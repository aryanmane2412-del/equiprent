package notifications

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// ServiceAccount is the subset of a Google service-account key file used to
// call FCM (Firebase console → Project settings → Service accounts).
type ServiceAccount struct {
	ProjectID   string `json:"project_id"`
	ClientEmail string `json:"client_email"`
	PrivateKey  string `json:"private_key"`
	TokenURI    string `json:"token_uri"`
}

// LoadServiceAccount reads a key from a file path or from inline JSON.
func LoadServiceAccount(fileOrJSON string) (ServiceAccount, error) {
	raw := []byte(strings.TrimSpace(fileOrJSON))
	if len(raw) > 0 && raw[0] != '{' {
		b, err := os.ReadFile(fileOrJSON)
		if err != nil {
			return ServiceAccount{}, fmt.Errorf("read FCM credentials: %w", err)
		}
		raw = b
	}
	var sa ServiceAccount
	if err := json.Unmarshal(raw, &sa); err != nil {
		return ServiceAccount{}, fmt.Errorf("parse FCM credentials: %w", err)
	}
	if sa.ClientEmail == "" || sa.PrivateKey == "" {
		return ServiceAccount{}, errors.New("FCM credentials need client_email and private_key")
	}
	if sa.TokenURI == "" {
		sa.TokenURI = "https://oauth2.googleapis.com/token"
	}
	return sa, nil
}

// DeviceStore is what the FCM sender needs from the device registry.
type DeviceStore interface {
	PushTokens(ctx context.Context, userID uuid.UUID) ([]string, error)
	MarkPushed(ctx context.Context, token string) error
	MarkFailed(ctx context.Context, token, reason string) error
	DropToken(ctx context.Context, token string) error
}

// FCMPusher sends push notifications through the Firebase Cloud Messaging
// HTTP v1 API, authenticating with a service account (OAuth 2.0 JWT bearer
// grant, RS256). Tokens that FCM reports as unregistered are deleted.
type FCMPusher struct {
	ProjectID string
	Account   ServiceAccount
	Devices   DeviceStore
	Client    *http.Client
	Log       *slog.Logger
	// Endpoint overrides https://fcm.googleapis.com (tests).
	Endpoint string
	// AppURL is opened by web pushes (fcm_options.link), e.g. https://app.equiprent.in
	AppURL string

	key      *rsa.PrivateKey
	mu       sync.Mutex
	token    string
	tokenExp time.Time
	now      func() time.Time
}

// NewFCMPusher validates the key and builds the sender.
func NewFCMPusher(sa ServiceAccount, projectID string, devices DeviceStore, log *slog.Logger) (*FCMPusher, error) {
	key, err := parseRSAKey(sa.PrivateKey)
	if err != nil {
		return nil, err
	}
	if projectID == "" {
		projectID = sa.ProjectID
	}
	if projectID == "" {
		return nil, errors.New("FCM project id is missing (FCM_PROJECT_ID or project_id in the key)")
	}
	return &FCMPusher{
		ProjectID: projectID, Account: sa, Devices: devices, Log: log, key: key,
		Client: &http.Client{Timeout: 10 * time.Second}, Endpoint: "https://fcm.googleapis.com", now: time.Now,
	}, nil
}

func parseRSAKey(pemText string) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode([]byte(strings.ReplaceAll(pemText, `\n`, "\n")))
	if block == nil {
		return nil, errors.New("FCM private_key is not PEM")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		if rk, ok := k.(*rsa.PrivateKey); ok {
			return rk, nil
		}
		return nil, errors.New("FCM private_key is not an RSA key")
	}
	k, err := x509.ParsePKCS1PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("FCM private_key: %w", err)
	}
	return k, nil
}

// accessToken returns a cached OAuth token, refreshing it a minute early.
func (f *FCMPusher) accessToken(ctx context.Context) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := f.now()
	if f.token != "" && now.Before(f.tokenExp.Add(-time.Minute)) {
		return f.token, nil
	}
	assertion, err := f.signJWT(now)
	if err != nil {
		return "", err
	}
	form := url.Values{"grant_type": {"urn:ietf:params:oauth:grant-type:jwt-bearer"}, "assertion": {assertion}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.Account.TokenURI, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := f.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("FCM token: %w", err)
	}
	defer resp.Body.Close()
	var body struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		Error       string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&body); err != nil || resp.StatusCode != http.StatusOK || body.AccessToken == "" {
		return "", fmt.Errorf("FCM token: status %d %s", resp.StatusCode, body.Error)
	}
	f.token = body.AccessToken
	f.tokenExp = now.Add(time.Duration(body.ExpiresIn) * time.Second)
	return f.token, nil
}

func (f *FCMPusher) signJWT(now time.Time) (string, error) {
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"RS256","typ":"JWT"}`))
	claims, _ := json.Marshal(map[string]any{
		"iss": f.Account.ClientEmail, "scope": "https://www.googleapis.com/auth/firebase.messaging",
		"aud": f.Account.TokenURI, "iat": now.Unix(), "exp": now.Add(time.Hour).Unix(),
	})
	signing := header + "." + enc.EncodeToString(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, f.key, crypto.SHA256, sum[:])
	if err != nil {
		return "", err
	}
	return signing + "." + enc.EncodeToString(sig), nil
}

// fcmMessage is the HTTP v1 message body.
type fcmMessage struct {
	Message struct {
		Token        string            `json:"token"`
		Notification map[string]string `json:"notification"`
		Data         map[string]string `json:"data,omitempty"`
		Android      map[string]any    `json:"android"`
		APNS         map[string]any    `json:"apns"`
		Webpush      map[string]any    `json:"webpush,omitempty"`
	} `json:"message"`
}

// Push sends m to every registered device of the user. One failing device
// does not stop the others; the in-app notification is the record anyway.
func (f *FCMPusher) Push(ctx context.Context, userID uuid.UUID, m Message) error {
	tokens, err := f.Devices.PushTokens(ctx, userID)
	if err != nil || len(tokens) == 0 {
		return err
	}
	access, err := f.accessToken(ctx)
	if err != nil {
		return err
	}
	var errs []error
	for _, tok := range tokens {
		if err := f.send(ctx, access, tok, m); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// PushData converts notification data to FCM's string-only data map.
func PushData(m Message) map[string]string {
	data := map[string]string{"type": m.Type}
	for k, v := range m.Data {
		switch x := v.(type) {
		case string:
			data[k] = x
		case nil:
		default:
			if b, err := json.Marshal(x); err == nil {
				data[k] = strings.Trim(string(b), `"`)
			}
		}
	}
	return data
}

func (f *FCMPusher) send(ctx context.Context, access, token string, m Message) error {
	var msg fcmMessage
	msg.Message.Token = token
	msg.Message.Notification = map[string]string{"title": m.Title, "body": m.Body}
	msg.Message.Data = PushData(m)
	collapse := m.Type
	if id, ok := m.Data["conversation_id"]; ok {
		collapse = fmt.Sprint(id) // one stacked notification per chat
	}
	msg.Message.Android = map[string]any{"priority": "high", "collapse_key": collapse,
		"notification": map[string]any{"channel_id": "equiprent_default", "tag": collapse}}
	msg.Message.APNS = map[string]any{"headers": map[string]string{"apns-collapse-id": trimTo(collapse, 64)},
		"payload": map[string]any{"aps": map[string]any{"sound": "default", "thread-id": collapse}}}
	if f.AppURL != "" {
		msg.Message.Webpush = map[string]any{"fcm_options": map[string]string{"link": f.AppURL}}
	}
	body, _ := json.Marshal(msg)
	endpoint := strings.TrimRight(f.Endpoint, "/") + "/v1/projects/" + url.PathEscape(f.ProjectID) + "/messages:send"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Content-Type", "application/json")
	resp, err := f.Client.Do(req)
	if err != nil {
		_ = f.Devices.MarkFailed(ctx, token, trimTo(err.Error(), 300))
		return fmt.Errorf("FCM send: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusOK {
		return f.Devices.MarkPushed(ctx, token)
	}
	var e struct {
		Error struct {
			Status  string `json:"status"`
			Message string `json:"message"`
			Details []struct {
				ErrorCode string `json:"errorCode"`
			} `json:"details"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 1<<16)).Decode(&e)
	code := e.Error.Status
	for _, d := range e.Error.Details {
		if d.ErrorCode != "" {
			code = d.ErrorCode
		}
	}
	// The app was uninstalled or the token rotated: forget the device.
	if code == "UNREGISTERED" || resp.StatusCode == http.StatusNotFound ||
		(code == "INVALID_ARGUMENT" && strings.Contains(strings.ToLower(e.Error.Message), "registration token")) {
		f.Log.InfoContext(ctx, "dropping invalid push token", "code", code)
		return f.Devices.DropToken(ctx, token)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		f.mu.Lock()
		f.token = "" // refresh on the next push
		f.mu.Unlock()
	}
	_ = f.Devices.MarkFailed(ctx, token, trimTo(fmt.Sprintf("%d %s", resp.StatusCode, code), 300))
	return fmt.Errorf("FCM send: status %d %s", resp.StatusCode, code)
}

func trimTo(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
