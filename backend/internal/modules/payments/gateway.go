package payments

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Order is a gateway order the client pays with the checkout SDK.
type Order struct {
	ID       string         `json:"order_id"`
	Checkout map[string]any `json:"checkout"`
}

// Gateway is a payment provider. Razorpay is the first implementation; the
// interface keeps Cashfree/PayU possible (payments.gateway CHECK allows them).
type Gateway interface {
	Name() string
	CreateOrder(ctx context.Context, amountPaise int64, receipt string, notes map[string]string) (Order, error)
	// VerifyPayment checks the checkout callback signature.
	VerifyPayment(orderID, paymentID, signature string) bool
	// VerifyWebhook checks a webhook body signature.
	VerifyWebhook(body []byte, signature string) bool
	// Sandbox reports whether orders are simulated locally.
	Sandbox() bool
	// Refund returns part or all of a captured payment. Processed is false
	// when the gateway settles it later (a refund webhook follows).
	Refund(ctx context.Context, gatewayPaymentID string, amountPaise int64, receipt string) (RefundResult, error)
}

// RefundResult is the gateway's answer to a refund.
type RefundResult struct {
	ID        string
	Processed bool
}

// Refund calls POST /payments/{id}/refund. In sandbox mode (or for a
// sandbox payment) the refund is processed at once.
func (r *Razorpay) Refund(ctx context.Context, gatewayPaymentID string, amountPaise int64, receipt string) (RefundResult, error) {
	if r.Sandbox() || strings.HasPrefix(gatewayPaymentID, "pay_sbx_") {
		return RefundResult{ID: "rfnd_sbx_" + randomHex(7), Processed: true}, nil
	}
	body, _ := json.Marshal(map[string]any{"amount": amountPaise, "speed": "normal", "receipt": receipt})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+"/payments/"+gatewayPaymentID+"/refund", bytes.NewReader(body))
	if err != nil {
		return RefundResult{}, err
	}
	req.SetBasicAuth(r.KeyID, r.KeySecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return RefundResult{}, fmt.Errorf("razorpay refund: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return RefundResult{}, fmt.Errorf("razorpay refund: status %d", resp.StatusCode)
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" {
		return RefundResult{}, fmt.Errorf("razorpay refund: unexpected response")
	}
	return RefundResult{ID: out.ID, Processed: out.Status == "processed"}, nil
}

// Razorpay implements Gateway against the Razorpay Orders API. With empty
// credentials it runs in sandbox mode: orders are created locally and the
// payment signature uses the webhook secret, so the flow can be exercised
// end-to-end without network access (refused in production by config).
type Razorpay struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
	BaseURL       string
	HTTP          *http.Client
}

// NewRazorpay builds the gateway.
func NewRazorpay(keyID, keySecret, webhookSecret string) *Razorpay {
	return &Razorpay{
		KeyID: keyID, KeySecret: keySecret, WebhookSecret: webhookSecret,
		BaseURL: "https://api.razorpay.com/v1", HTTP: &http.Client{Timeout: 10 * time.Second},
	}
}

func (r *Razorpay) Name() string  { return "razorpay" }
func (r *Razorpay) Sandbox() bool { return r.KeyID == "" || r.KeySecret == "" }

func (r *Razorpay) CreateOrder(ctx context.Context, amountPaise int64, receipt string, notes map[string]string) (Order, error) {
	checkout := map[string]any{"amount": amountPaise, "currency": "INR", "name": "EQUIPRENT", "receipt": receipt}
	if r.Sandbox() {
		id := "order_sbx_" + randomHex(7)
		checkout["key"] = "rzp_test_sandbox"
		checkout["order_id"] = id
		return Order{ID: id, Checkout: checkout}, nil
	}

	body, _ := json.Marshal(map[string]any{"amount": amountPaise, "currency": "INR", "receipt": receipt, "notes": notes})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.BaseURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return Order{}, err
	}
	req.SetBasicAuth(r.KeyID, r.KeySecret)
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.HTTP.Do(req)
	if err != nil {
		return Order{}, fmt.Errorf("razorpay create order: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return Order{}, fmt.Errorf("razorpay create order: status %d", resp.StatusCode)
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID == "" {
		return Order{}, fmt.Errorf("razorpay create order: unexpected response")
	}
	checkout["key"] = r.KeyID
	checkout["order_id"] = out.ID
	return Order{ID: out.ID, Checkout: checkout}, nil
}

// VerifyPayment: hex(HMAC_SHA256(order_id + "|" + payment_id, key_secret)).
func (r *Razorpay) VerifyPayment(orderID, paymentID, signature string) bool {
	return hmac.Equal([]byte(Sign(r.paymentSecret(), orderID+"|"+paymentID)), []byte(signature))
}

// VerifyWebhook: hex(HMAC_SHA256(raw body, webhook_secret)).
func (r *Razorpay) VerifyWebhook(body []byte, signature string) bool {
	if r.WebhookSecret == "" {
		return false
	}
	return hmac.Equal([]byte(Sign(r.WebhookSecret, string(body))), []byte(signature))
}

func (r *Razorpay) paymentSecret() string {
	if r.Sandbox() {
		return r.WebhookSecret
	}
	return r.KeySecret
}

// SandboxPayment simulates the checkout result for a sandbox order.
func (r *Razorpay) SandboxPayment(orderID string) (paymentID, signature string) {
	paymentID = "pay_sbx_" + randomHex(7)
	return paymentID, Sign(r.paymentSecret(), orderID+"|"+paymentID)
}

// Sign returns hex(HMAC_SHA256(msg, secret)).
func Sign(secret, msg string) string {
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(msg))
	return hex.EncodeToString(m.Sum(nil))
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
