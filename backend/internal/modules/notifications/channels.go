package notifications

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

// Sender delivers one email or SMS. It returns the provider's message id.
// Errors wrapped in Permanent are not retried.
type Sender interface {
	Provider() string
	Send(ctx context.Context, to string, m Message) (ref string, err error)
}

// PermanentError marks a failure that a retry cannot fix (bad address,
// rejected credentials, refused template).
type PermanentError struct{ Err error }

func (e PermanentError) Error() string { return e.Err.Error() }
func (e PermanentError) Unwrap() error { return e.Err }

func Permanent(err error) error { return PermanentError{Err: err} }

func IsPermanent(err error) bool {
	var p PermanentError
	return errors.As(err, &p)
}

func randomRef(prefix string) string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

// ---- development simulation -----------------------------------------------------------------

// Simulator records instead of sending: the delivery is stored with status
// "simulated" and appears in the delivery log, so email and SMS flows can be
// exercised without provider accounts. Refused in production by config.
type Simulator struct {
	Channel string
	Log     *slog.Logger
}

func (s Simulator) Provider() string { return "simulator" }

func (s Simulator) Send(ctx context.Context, to string, m Message) (string, error) {
	if s.Log != nil {
		s.Log.InfoContext(ctx, "simulated "+s.Channel+" (not sent)", "to", maskContact(to), "type", m.Type, "title", m.Title)
	}
	return randomRef("sim_"), nil
}

// maskContact keeps logs free of full phone numbers and addresses.
func maskContact(s string) string {
	if i := strings.IndexByte(s, '@'); i > 1 {
		return s[:1] + "***" + s[i:]
	}
	if len(s) > 4 {
		return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
	}
	return "***"
}

// ---- email: SMTP ------------------------------------------------------------------------------

// SMTPEmail sends plain-text email through an SMTP relay (SES, SendGrid,
// Postmark, Zoho, ... all offer one). STARTTLS is required unless the host
// is local; port 465 uses implicit TLS.
type SMTPEmail struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string // "EQUIPRENT <no-reply@equiprent.in>"
	AppURL   string // footer link to the app
	Timeout  time.Duration
}

func (s SMTPEmail) Provider() string { return "smtp" }

func local(host string) bool { return host == "localhost" || host == "127.0.0.1" || host == "::1" }

func (s SMTPEmail) Send(ctx context.Context, to string, m Message) (string, error) {
	if strings.ContainsAny(to, "\r\n") || !strings.Contains(to, "@") {
		return "", Permanent(fmt.Errorf("invalid recipient"))
	}
	from := s.From
	addr, err := mailAddress(from)
	if err != nil {
		return "", Permanent(fmt.Errorf("NOTIFY_EMAIL_FROM: %w", err))
	}
	ref := randomRef("") + "@equiprent"
	var msg bytes.Buffer
	fmt.Fprintf(&msg, "From: %s\r\nTo: %s\r\nSubject: %s\r\nMessage-ID: <%s>\r\nDate: %s\r\n",
		from, to, mime.QEncoding.Encode("utf-8", m.Title), ref, time.Now().UTC().Format(time.RFC1123Z))
	msg.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: 8bit\r\n\r\n")
	body := m.Body
	if s.AppURL != "" {
		body += "\n\nOpen EQUIPRENT: " + s.AppURL + "\n\nYou can change which emails you receive in the app under Notifications."
	}
	msg.WriteString(strings.ReplaceAll(body, "\n", "\r\n"))
	msg.WriteString("\r\n")

	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	if dl, ok := ctx.Deadline(); ok && time.Until(dl) < timeout {
		timeout = time.Until(dl)
	}
	hostport := net.JoinHostPort(s.Host, fmt.Sprint(s.Port))
	dialer := &net.Dialer{Timeout: timeout}
	var conn net.Conn
	if s.Port == 465 {
		conn, err = tls.DialWithDialer(dialer, "tcp", hostport, &tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12})
	} else {
		conn, err = dialer.DialContext(ctx, "tcp", hostport)
	}
	if err != nil {
		return "", err
	}
	_ = conn.SetDeadline(time.Now().Add(timeout))
	c, err := smtp.NewClient(conn, s.Host)
	if err != nil {
		conn.Close()
		return "", err
	}
	defer c.Close()
	if s.Port != 465 {
		if ok, _ := c.Extension("STARTTLS"); ok {
			if err := c.StartTLS(&tls.Config{ServerName: s.Host, MinVersion: tls.VersionTLS12}); err != nil {
				return "", err
			}
		} else if !local(s.Host) {
			return "", Permanent(errors.New("SMTP server does not offer STARTTLS"))
		}
	}
	if s.Username != "" {
		if err := c.Auth(smtp.PlainAuth("", s.Username, s.Password, s.Host)); err != nil {
			return "", Permanent(fmt.Errorf("SMTP authentication: %w", err))
		}
	}
	if err := c.Mail(addr); err != nil {
		return "", classifySMTP(err)
	}
	if err := c.Rcpt(to); err != nil {
		return "", classifySMTP(err)
	}
	w, err := c.Data()
	if err != nil {
		return "", classifySMTP(err)
	}
	if _, err := w.Write(msg.Bytes()); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", classifySMTP(err)
	}
	_ = c.Quit()
	return ref, nil
}

// classifySMTP treats 5xx replies (unknown mailbox, policy refusal) as
// permanent and 4xx (try later) as retryable.
func classifySMTP(err error) error {
	var te *textproto.Error
	if errors.As(err, &te) && te.Code >= 500 {
		return Permanent(err)
	}
	return err
}

func mailAddress(from string) (string, error) {
	if i, j := strings.LastIndexByte(from, '<'), strings.LastIndexByte(from, '>'); i >= 0 && j > i {
		from = from[i+1 : j]
	}
	from = strings.TrimSpace(from)
	if !strings.Contains(from, "@") || strings.ContainsAny(from, "\r\n ") {
		return "", errors.New("not an email address")
	}
	return from, nil
}

// ---- SMS --------------------------------------------------------------------------------------

// smsText is the SMS body: title and text in one short message.
func smsText(m Message) string {
	t := "EQUIPRENT: " + m.Title
	if m.Body != "" {
		t += " - " + m.Body
	}
	if r := []rune(t); len(r) > 300 {
		t = string(r[:297]) + "..."
	}
	return t
}

func httpError(provider string, res *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(res.Body, 300))
	err := fmt.Errorf("%s: HTTP %d: %s", provider, res.StatusCode, strings.TrimSpace(string(b)))
	if res.StatusCode >= 400 && res.StatusCode < 500 && res.StatusCode != http.StatusTooManyRequests {
		return Permanent(err)
	}
	return err
}

// TwilioSMS sends through Twilio's Messages API.
type TwilioSMS struct {
	AccountSID string
	AuthToken  string
	From       string // a Twilio number or Messaging Service SID (MG...)
	BaseURL    string // tests
	Client     *http.Client
}

func (t TwilioSMS) Provider() string { return "twilio" }

func (t TwilioSMS) Send(ctx context.Context, to string, m Message) (string, error) {
	base := t.BaseURL
	if base == "" {
		base = "https://api.twilio.com"
	}
	form := url.Values{"To": {to}, "Body": {smsText(m)}}
	if strings.HasPrefix(t.From, "MG") {
		form.Set("MessagingServiceSid", t.From)
	} else {
		form.Set("From", t.From)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/2010-04-01/Accounts/"+url.PathEscape(t.AccountSID)+"/Messages.json",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.SetBasicAuth(t.AccountSID, t.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	res, err := client(t.Client).Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", httpError("twilio", res)
	}
	var out struct {
		SID string `json:"sid"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out)
	return out.SID, nil
}

// MSG91SMS sends through MSG91's Flow API. Indian SMS needs a DLT-registered
// template; the template must take the variables "title" and "body".
type MSG91SMS struct {
	AuthKey    string
	TemplateID string
	BaseURL    string
	Client     *http.Client
}

func (g MSG91SMS) Provider() string { return "msg91" }

func (g MSG91SMS) Send(ctx context.Context, to string, m Message) (string, error) {
	base := g.BaseURL
	if base == "" {
		base = "https://control.msg91.com"
	}
	body, _ := json.Marshal(map[string]any{
		"template_id": g.TemplateID,
		"short_url":   "0",
		"recipients":  []map[string]string{{"mobiles": strings.TrimPrefix(to, "+"), "title": m.Title, "body": m.Body}},
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, base+"/api/v5/flow/", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("authkey", g.AuthKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := client(g.Client).Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		return "", httpError("msg91", res)
	}
	var out struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	}
	_ = json.NewDecoder(io.LimitReader(res.Body, 1<<16)).Decode(&out)
	if out.Type == "error" {
		return "", Permanent(fmt.Errorf("msg91: %s", out.Message))
	}
	return out.Message, nil // the request id
}

func client(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{Timeout: 15 * time.Second}
}
