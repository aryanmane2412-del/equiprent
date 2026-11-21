// Package razorpayx pays equipment owners through RazorpayX and verifies their
// bank accounts with its Fund Account Validation (a penny drop that confirms
// the account exists and is active and returns the name the bank holds).
//
// One Client implements both verification.PayoutProvider (bank account
// verification) and settlements.PayoutGateway (transfers). Money only moves
// to a fund account that passed validation, every payout carries an
// idempotency key so a retry can never pay twice, and a transfer whose outcome
// is unknown is reported as settlements.ErrTransferUncertain, never as failed.
package razorpayx

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/modules/settlements"
	"github.com/equiprent/backend/internal/modules/verification"
)

// Client talks to the RazorpayX API.
type Client struct {
	KeyID         string
	KeySecret     string
	AccountNumber string // the RazorpayX account the payouts are debited from
	Mode          string // IMPS | NEFT | RTGS | UPI
	BaseURL       string
	HTTP          *http.Client

	// PollEvery / PollFor bound how long VerifyAccount waits for the bank. The whole call must
	// finish inside the app's 20 s request timeout, so a slow bank is resumed on the next try.
	PollEvery time.Duration
	PollFor   time.Duration

	mu      sync.Mutex
	pending map[string]pendingValidation
}

// pendingValidation is a penny drop the bank has not answered yet, remembered so that
// the owner's retry resumes it instead of starting (and paying for) a new one.
type pendingValidation struct {
	fundID, valID string
	expires       time.Time
}

const pendingTTL = 10 * time.Minute

// New builds a client for the live API.
func New(keyID, keySecret, accountNumber, mode string) *Client {
	return &Client{
		KeyID: keyID, KeySecret: keySecret, AccountNumber: accountNumber, Mode: mode,
		BaseURL: "https://api.razorpay.com/v1", HTTP: &http.Client{Timeout: 15 * time.Second},
		PollEvery: time.Second, PollFor: 12 * time.Second,
	}
}

// Sandbox is always false: this client moves real money (or test-mode money
// when the keys are rzp_test_ keys).
func (c *Client) Sandbox() bool { return false }

// ModeName is "live" for rzp_live_ keys and "test" otherwise.
func (c *Client) ModeName() string {
	if strings.HasPrefix(c.KeyID, "rzp_live_") {
		return "live"
	}
	return "test"
}

// apiError is what RazorpayX answers on failure.
type apiError struct {
	Status      int
	Code        string
	Description string
}

func (e *apiError) Error() string {
	return fmt.Sprintf("razorpayx: %d %s: %s", e.Status, e.Code, e.Description)
}

// call performs one request. Network failures return a plain error; HTTP
// failures return *apiError; success returns the body.
func (c *Client) call(ctx context.Context, method, path string, body any, header map[string]string) ([]byte, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, rd)
	if err != nil {
		return nil, err
	}
	req.SetBasicAuth(c.KeyID, c.KeySecret)
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return raw, nil
	}
	var e struct {
		Error struct {
			Code        string `json:"code"`
			Description string `json:"description"`
		} `json:"error"`
	}
	_ = json.Unmarshal(raw, &e)
	return nil, &apiError{Status: resp.StatusCode, Code: e.Error.Code, Description: e.Error.Description}
}

// definite reports whether an API error means the request was refused before
// anything happened. 5xx, rate limiting and conflicts are NOT definite.
func definite(err error) bool {
	var ae *apiError
	if !errors.As(err, &ae) {
		return false
	}
	return ae.Status >= 400 && ae.Status < 500 && ae.Status != http.StatusTooManyRequests && ae.Status != http.StatusConflict && ae.Status != http.StatusRequestTimeout
}

// ---- bank account verification ---------------------------------------------------------------

// VerifyAccount creates a contact and a bank fund account, runs the penny-drop
// validation and waits (bounded) for the bank's answer. If the bank is slower than
// the wait, the validation is remembered and the owner's retry resumes it.
func (c *Client) VerifyAccount(ctx context.Context, a verification.PayoutAccount) (verification.PayoutResult, error) {
	ctx, cancel := context.WithTimeout(ctx, c.PollFor+4*time.Second)
	defer cancel()
	unavailable := func(err error) (verification.PayoutResult, error) {
		return verification.PayoutResult{}, apperror.ErrServiceUnavailable.
			WithDetail("bank account verification is temporarily unavailable; please try again shortly").Wrap(err)
	}
	// A 400 on the owner's own details is theirs to fix. Anything else (bad credentials, an empty
	// RazorpayX balance, rate limits) is OUR problem and must not be blamed on the owner.
	userFault := func(err error) (verification.PayoutResult, bool) {
		var ae *apiError
		if errors.As(err, &ae) && definite(err) && ae.Status != http.StatusUnauthorized && ae.Status != http.StatusForbidden && ae.Status != http.StatusNotFound {
			return verification.PayoutResult{Verified: false, Reason: firstNonEmpty(ae.Description, "the bank details were not accepted")}, true
		}
		return verification.PayoutResult{}, false
	}

	key := verifyKey(a)
	fundID, valID := c.takePending(key)
	if valID == "" {
		var contact struct {
			ID string `json:"id"`
		}
		raw, err := c.call(ctx, http.MethodPost, "/contacts", map[string]any{
			"name": a.HolderName, "type": "vendor", "reference_id": "owner-" + randomHex(8),
		}, nil)
		if err != nil {
			if r, ok := userFault(err); ok {
				return r, nil
			}
			return unavailable(err)
		}
		if json.Unmarshal(raw, &contact) != nil || contact.ID == "" {
			return unavailable(errors.New("unexpected contact response"))
		}

		var fund struct {
			ID string `json:"id"`
		}
		raw, err = c.call(ctx, http.MethodPost, "/fund_accounts", map[string]any{
			"contact_id": contact.ID, "account_type": "bank_account",
			"bank_account": map[string]any{"name": a.HolderName, "ifsc": a.IFSC, "account_number": a.AccountNumber},
		}, nil)
		if err != nil {
			if r, ok := userFault(err); ok {
				return r, nil
			}
			return unavailable(err)
		}
		if json.Unmarshal(raw, &fund) != nil || fund.ID == "" {
			return unavailable(errors.New("unexpected fund account response"))
		}
		fundID = fund.ID

		var created validation
		raw, err = c.call(ctx, http.MethodPost, "/fund_accounts/validations", map[string]any{
			"account_number": c.AccountNumber, "fund_account": map[string]any{"id": fundID},
			"amount": 100, "currency": "INR", "reference_id": "val-" + randomHex(8),
			"notes": map[string]any{"purpose": "EQUIPRENT payout account"},
		}, nil)
		if err != nil {
			// The bank details were already accepted above, so a failure to START the check
			// (typically too little RazorpayX balance for the penny drop) is the platform's, not the owner's.
			return unavailable(err)
		}
		if json.Unmarshal(raw, &created) != nil || created.ID == "" {
			return unavailable(errors.New("unexpected validation response"))
		}
		valID = created.ID
	}

	var val validation
	deadline := time.Now().Add(c.PollFor)
	for {
		raw, err := c.call(ctx, http.MethodGet, "/fund_accounts/validations/"+url.PathEscape(valID), nil, nil)
		if err != nil {
			c.savePending(key, fundID, valID) // keep it: the retry can pick up where this stopped
			return unavailable(err)
		}
		if json.Unmarshal(raw, &val) != nil {
			return unavailable(errors.New("unexpected validation response"))
		}
		if val.Status != "created" && val.Status != "" {
			break
		}
		if time.Now().After(deadline) {
			c.savePending(key, fundID, valID)
			return verification.PayoutResult{}, apperror.ErrServiceUnavailable.
				WithDetail("the bank is still verifying this account; please try again in a minute")
		}
		select {
		case <-ctx.Done():
			c.savePending(key, fundID, valID)
			return unavailable(ctx.Err())
		case <-time.After(c.PollEvery):
		}
	}
	c.dropPending(key)

	res := verification.PayoutResult{Provider: "razorpay", Mode: c.ModeName()}
	switch {
	case val.Status == "completed" && strings.EqualFold(val.Results.AccountStatus, "active"):
		if !NamesMatch(a.HolderName, val.Results.RegisteredName) {
			res.Reason = "the account holder name does not match the name the bank holds for this account"
			return res, nil
		}
		res.Verified, res.Reference = true, fundID
	case val.Status == "completed":
		res.Reason = "the bank reports this account as " + firstNonEmpty(val.Results.AccountStatus, "not active")
	default:
		res.Reason = firstNonEmpty(val.StatusDetails.Description, "the bank could not validate this account")
	}
	return res, nil
}

// verifyKey identifies one verification attempt by what the owner typed.
func verifyKey(a verification.PayoutAccount) string {
	sum := sha256.Sum256([]byte(strings.ToLower(strings.TrimSpace(a.HolderName)) + "|" + a.AccountNumber + "|" + strings.ToUpper(a.IFSC)))
	return hex.EncodeToString(sum[:])
}

func (c *Client) takePending(key string) (fundID, valID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	p, ok := c.pending[key]
	if !ok || time.Now().After(p.expires) {
		delete(c.pending, key)
		return "", ""
	}
	return p.fundID, p.valID
}

func (c *Client) savePending(key, fundID, valID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.pending == nil {
		c.pending = map[string]pendingValidation{}
	}
	c.pending[key] = pendingValidation{fundID: fundID, valID: valID, expires: time.Now().Add(pendingTTL)}
}

func (c *Client) dropPending(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, key)
}

type validation struct {
	ID      string `json:"id"`
	Status  string `json:"status"`
	Results struct {
		AccountStatus  string `json:"account_status"`
		RegisteredName string `json:"registered_name"`
	} `json:"results"`
	StatusDetails struct {
		Description string `json:"description"`
	} `json:"status_details"`
}

// ---- transfers -----------------------------------------------------------------------------------

// Transfer sends paise to a validated fund account. The idempotency key makes a
// repeat of the same call return the same payout instead of paying again.
func (c *Client) Transfer(ctx context.Context, idempotencyKey, fundAccountID string, paise int64) (string, error) {
	if idempotencyKey == "" || fundAccountID == "" || paise <= 0 {
		return "", fmt.Errorf("%w: invalid transfer request", settlements.ErrTransferRejected)
	}
	// RazorpayX limits idempotency keys and reference ids to a short length, so send a
	// stable 32-character digest: the same key always gives the same value, different keys differ.
	sum := sha256.Sum256([]byte(idempotencyKey))
	ref := hex.EncodeToString(sum[:])[:32]
	raw, err := c.call(ctx, http.MethodPost, "/payouts", map[string]any{
		"account_number": c.AccountNumber, "fund_account_id": fundAccountID, "amount": paise, "currency": "INR",
		"mode": c.Mode, "purpose": "payout", "queue_if_low_balance": false,
		"reference_id": ref, "narration": "EQUIPRENT payout",
	}, map[string]string{"X-Payout-Idempotency": ref})
	if err != nil {
		if definite(err) {
			return "", fmt.Errorf("%w: %v", settlements.ErrTransferRejected, err)
		}
		return "", fmt.Errorf("%w: %v", settlements.ErrTransferUncertain, err)
	}
	var out struct {
		ID     string `json:"id"`
		Status string `json:"status"`
	}
	if json.Unmarshal(raw, &out) != nil || out.ID == "" {
		return "", fmt.Errorf("%w: unreadable response", settlements.ErrTransferUncertain)
	}
	if settlements.ClassifyStatus(out.Status) == settlements.OutcomeFailed {
		return out.ID, fmt.Errorf("%w: payout %s", settlements.ErrTransferRejected, out.Status)
	}
	return out.ID, nil
}

// Status returns the gateway's state of a payout and its amount in paise.
func (c *Client) Status(ctx context.Context, transferID string) (string, int64, error) {
	raw, err := c.call(ctx, http.MethodGet, "/payouts/"+url.PathEscape(transferID), nil, nil)
	if err != nil {
		return "", 0, err
	}
	var out struct {
		Status string `json:"status"`
		Amount int64  `json:"amount"`
	}
	if json.Unmarshal(raw, &out) != nil || out.Status == "" {
		return "", 0, errors.New("razorpayx: unreadable payout status")
	}
	return strings.ToLower(out.Status), out.Amount, nil
}

// ---- helpers ----------------------------------------------------------------------------------

var nameNoise = map[string]bool{
	"mr": true, "mrs": true, "ms": true, "shri": true, "smt": true, "sri": true, "m": true, "s": true, "messrs": true,
	"pvt": true, "private": true, "ltd": true, "limited": true, "llp": true, "and": true, "the": true, "co": true,
}

func nameTokens(s string) []string {
	var cur strings.Builder
	var out []string
	flush := func() {
		if cur.Len() >= 3 && !nameNoise[cur.String()] {
			out = append(out, cur.String())
		}
		cur.Reset()
	}
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' {
			cur.WriteRune(r)
		} else {
			flush()
		}
	}
	flush()
	return out
}

// NamesMatch reports whether the holder name the owner typed plausibly belongs
// to the account: they share at least one significant word. It guards against
// paying out to someone else's account; it is deliberately lenient about
// initials, honorifics and company suffixes. An empty bank-side name (some
// banks do not return one) is accepted.
func NamesMatch(typed, registered string) bool {
	if strings.TrimSpace(registered) == "" {
		return true
	}
	have := map[string]bool{}
	for _, t := range nameTokens(registered) {
		have[t] = true
	}
	for _, t := range nameTokens(typed) {
		if have[t] {
			return true
		}
	}
	return false
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
