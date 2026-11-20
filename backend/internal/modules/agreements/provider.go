package agreements

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"
)

// Provider is how a party signs. The built-in provider records a typed-name
// signature directly. An external provider (Aadhaar eSign through an e-sign
// service provider, DocuSign, …) sends the signer to its own page and
// reports back by webhook; Begin and Webhook are the only integration points.
type Provider interface {
	Name() string
	// External reports whether signing happens at the provider.
	External() bool
	// Begin starts a signing session for the document.
	Begin(ctx context.Context, req BeginRequest) (BeginResult, error)
	// Webhook authenticates and parses a provider callback.
	Webhook(header http.Header, body []byte) (WebhookEvent, error)
}

// BeginRequest describes one signer.
type BeginRequest struct {
	SessionID  uuid.UUID
	Reference  string
	Party      string
	SignerName string
	Document   []byte // the PDF to sign
}

// BeginResult tells the client where to go.
type BeginResult struct {
	ProviderReference string
	RedirectURL       string
	// Sandbox is filled by the development provider only: the webhook a
	// real provider would send, so the flow can be completed without one.
	Sandbox *SandboxCompletion
}

// SandboxCompletion is a ready-to-send provider webhook (development).
type SandboxCompletion struct {
	WebhookPath string          `json:"webhook_path"`
	Signature   string          `json:"signature" doc:"Send as X-ESign-Signature"`
	Body        json.RawMessage `json:"body" swaggertype:"object"`
}

// WebhookEvent is a parsed provider callback.
type WebhookEvent struct {
	ProviderReference string
	Status            string // signed | declined | expired
	SignedDocument    []byte // the provider's signed PDF, when it returns one
}

// ErrBadWebhook is returned for unauthenticated or malformed callbacks.
var ErrBadWebhook = errors.New("invalid e-signature webhook")

// Builtin records typed-name signatures on EQUIPRENT itself.
type Builtin struct{}

func (Builtin) Name() string   { return "equiprent" }
func (Builtin) External() bool { return false }
func (Builtin) Begin(context.Context, BeginRequest) (BeginResult, error) {
	return BeginResult{}, errors.New("the built-in provider signs directly")
}
func (Builtin) Webhook(http.Header, []byte) (WebhookEvent, error) {
	return WebhookEvent{}, ErrBadWebhook
}

// Sandbox simulates an external provider for development: Begin returns a
// redirect URL and the HMAC-signed webhook the provider would send once the
// signer finishes.
type Sandbox struct{ Secret string }

func (Sandbox) Name() string   { return "sandbox" }
func (Sandbox) External() bool { return true }

type sandboxHook struct {
	Reference string `json:"reference"`
	Status    string `json:"status"`
}

func (s Sandbox) sign(body []byte) string {
	m := hmac.New(sha256.New, []byte(s.Secret))
	m.Write(body)
	return hex.EncodeToString(m.Sum(nil))
}

func (s Sandbox) Begin(_ context.Context, req BeginRequest) (BeginResult, error) {
	ref := "sbx_" + strings.ReplaceAll(req.SessionID.String(), "-", "")
	body, _ := json.Marshal(sandboxHook{Reference: ref, Status: "signed"})
	return BeginResult{
		ProviderReference: ref,
		RedirectURL:       "https://esign.sandbox.invalid/sign/" + ref,
		Sandbox:           &SandboxCompletion{WebhookPath: "/api/v1/webhooks/esign/sandbox", Signature: s.sign(body), Body: body},
	}, nil
}

func (s Sandbox) Webhook(h http.Header, body []byte) (WebhookEvent, error) {
	want := s.sign(body)
	if !hmac.Equal([]byte(want), []byte(strings.ToLower(h.Get("X-ESign-Signature")))) {
		return WebhookEvent{}, ErrBadWebhook
	}
	var hook sandboxHook
	if err := json.Unmarshal(body, &hook); err != nil || hook.Reference == "" {
		return WebhookEvent{}, ErrBadWebhook
	}
	switch hook.Status {
	case "signed", "declined", "expired":
	default:
		return WebhookEvent{}, ErrBadWebhook
	}
	return WebhookEvent{ProviderReference: hook.Reference, Status: hook.Status}, nil
}
