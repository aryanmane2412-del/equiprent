package verification

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strings"

	"github.com/equiprent/backend/internal/apperror"
)

// PayoutAccount is a bank account to be verified by the payment provider
// before payouts are sent to it.
type PayoutAccount struct {
	HolderName    string
	AccountNumber string
	IFSC          string
}

// PayoutResult is the provider's verdict.
type PayoutResult struct {
	Verified bool
	// Reference is the provider's fund-account id; payouts are made to this
	// reference, never to raw account numbers.
	Reference string
	Reason    string
	// Provider ("razorpay" | "simulated") and Mode ("live" | "test" |
	// "simulated") say who verified the account; they become the owner's
	// payout account record.
	Provider string
	Mode     string
}

// PayoutProvider verifies bank accounts through the approved payment
// provider (a penny-drop / fund-account validation). EQUIPRENT never moves
// money to an account the provider has not verified.
type PayoutProvider interface {
	VerifyAccount(ctx context.Context, a PayoutAccount) (PayoutResult, error)
}

// SandboxPayouts simulates the provider while no gateway credentials are
// configured (development/test only; production refuses to start in sandbox
// mode). Account numbers ending in 0000 fail verification so the failure path
// can be exercised.
type SandboxPayouts struct{}

func (SandboxPayouts) VerifyAccount(_ context.Context, a PayoutAccount) (PayoutResult, error) {
	if strings.HasSuffix(a.AccountNumber, "0000") {
		return PayoutResult{Verified: false, Reason: "the bank rejected this account (sandbox: numbers ending 0000 fail)"}, nil
	}
	b := make([]byte, 7)
	_, _ = rand.Read(b)
	return PayoutResult{Verified: true, Reference: "fa_sbx_" + hex.EncodeToString(b), Provider: "simulated", Mode: "simulated"}, nil
}

// UnconfiguredPayouts is used when gateway credentials are set but the
// provider's account-validation integration is not enabled. It refuses rather
// than pretending an account is verified.
type UnconfiguredPayouts struct{}

func (UnconfiguredPayouts) VerifyAccount(context.Context, PayoutAccount) (PayoutResult, error) {
	return PayoutResult{}, apperror.ErrServiceUnavailable.WithDetail("bank account verification with the payment provider is not enabled yet")
}
