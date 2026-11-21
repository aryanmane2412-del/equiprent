package app

import (
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/modules/settlements"
	"github.com/equiprent/backend/internal/modules/verification"
	"github.com/equiprent/backend/internal/payouts/razorpayx"
)

// newPayoutProvider chooses how an owner's bank account is verified:
// RazorpayX penny drop when live payouts are configured, the simulator in
// development (no payment keys), and a refusal otherwise (never "verified"
// without a real check).
func newPayoutProvider(cfg config.Config) verification.PayoutProvider {
	switch {
	case cfg.Payout.Live():
		return razorpayx.New(cfg.Payments.KeyID, cfg.Payments.KeySecret, cfg.Payout.AccountNumber, cfg.Payout.Mode)
	case cfg.Payments.Sandbox():
		return verification.SandboxPayouts{}
	default:
		return verification.UnconfiguredPayouts{}
	}
}

// PayoutGateway returns the gateway that sends owner payouts, or nil when
// automatic payouts are off (owners are then paid manually). The simulator is
// returned only where it is allowed: it records owners as paid without moving
// money, so it never runs with real keys or in production.
func PayoutGateway(cfg config.Config) settlements.PayoutGateway {
	switch {
	case cfg.Payout.Live():
		return razorpayx.New(cfg.Payments.KeyID, cfg.Payments.KeySecret, cfg.Payout.AccountNumber, cfg.Payout.Mode)
	case settlements.SimulatedPayoutsAllowed(cfg.Payments.Sandbox(), cfg.Env.IsProduction()):
		return settlements.SandboxGateway{}
	default:
		return nil
	}
}
