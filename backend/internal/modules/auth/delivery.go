package auth

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// ErrDeliveryUnavailable is returned by senders that are not configured.
var ErrDeliveryUnavailable = errors.New("delivery provider not configured")

// SMSSender delivers OTP codes and security notices by SMS. Production needs
// a DLT-registered provider; none is integrated yet (see config.SMSProvider).
type SMSSender interface {
	SendOTP(ctx context.Context, phone, code, purpose string) error
	SendNotice(ctx context.Context, phone, text string) error
}

// EmailSender delivers verification / reset links and security notices.
type EmailSender interface {
	Send(ctx context.Context, to, subject, body string) error
}

// DevSimulator stands in for SMS and email in development: nothing is sent,
// recipients are logged masked, and the secret itself is never logged (it is
// returned to the caller as dev_code / dev_token instead).
type DevSimulator struct{ Log *slog.Logger }

func (d DevSimulator) SendOTP(ctx context.Context, phone, _, purpose string) error {
	d.Log.InfoContext(ctx, "DEV SIMULATION: OTP not sent; returned as dev_code", "phone", MaskPhone(phone), "purpose", purpose)
	return nil
}

func (d DevSimulator) SendNotice(ctx context.Context, phone, text string) error {
	d.Log.InfoContext(ctx, "DEV SIMULATION: SMS notice not sent", "phone", MaskPhone(phone), "text", text)
	return nil
}

func (d DevSimulator) Send(ctx context.Context, to, subject, _ string) error {
	d.Log.InfoContext(ctx, "DEV SIMULATION: email not sent", "to", MaskEmail(to), "subject", subject)
	return nil
}

// DisabledSMS refuses delivery: production OTP stays off until a provider
// with credentials is configured.
type DisabledSMS struct{}

func (DisabledSMS) SendOTP(context.Context, string, string, string) error {
	return ErrDeliveryUnavailable
}
func (DisabledSMS) SendNotice(context.Context, string, string) error { return ErrDeliveryUnavailable }

// DisabledEmail refuses delivery until an email provider is configured.
type DisabledEmail struct{}

func (DisabledEmail) Send(context.Context, string, string, string) error {
	return ErrDeliveryUnavailable
}

// HTTPJSON is a configurable HTTPS JSON provider adapter. SMS endpoints
// receive {to,message}; email endpoints receive {from,to,subject,body}.
type HTTPJSON struct {
	Endpoint string
	Token    string
	Client   *http.Client
}

func (p HTTPJSON) send(ctx context.Context, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.Endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.Token)
	client := p.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("delivery provider returned HTTP %d", resp.StatusCode)
	}
	return nil
}

type SMSHTTP struct{ HTTPJSON }

func (p SMSHTTP) SendOTP(ctx context.Context, phone, code, purpose string) error {
	return p.send(ctx, map[string]string{"to": phone, "message": fmt.Sprintf("Your EQUIPRENT %s code is %s. It expires soon. Never share it.", purpose, code)})
}
func (p SMSHTTP) SendNotice(ctx context.Context, phone, text string) error {
	return p.send(ctx, map[string]string{"to": phone, "message": text})
}

type EmailHTTP struct {
	HTTPJSON
	From string
}

func (p EmailHTTP) Send(ctx context.Context, to, subject, body string) error {
	return p.send(ctx, map[string]string{"from": p.From, "to": to, "subject": subject, "body": body})
}

// MaskPhone keeps the country code and last two digits: +91******10.
func MaskPhone(p string) string {
	if len(p) < 6 {
		return "***"
	}
	return p[:3] + strings.Repeat("*", len(p)-5) + p[len(p)-2:]
}

// MaskEmail keeps the first character and the domain: r***@example.com.
func MaskEmail(e string) string {
	at := strings.LastIndexByte(e, '@')
	if at < 1 {
		return "***"
	}
	return e[:1] + "***" + e[at:]
}
