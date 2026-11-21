package config

import (
	"strings"
	"testing"
)

func TestPayoutsAreOffByDefault(t *testing.T) {
	cfg, err := Load(lookupFrom(productionEnv(nil)))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Payout.Live() || cfg.Payout.Provider != "" {
		t.Fatalf("automatic payouts must be opt-in, got %+v", cfg.Payout)
	}
	if cfg.Payout.Mode != "IMPS" || cfg.Payout.MaxPaise != 20_000_000 || cfg.Payout.BankCooldown.Hours() != 24 {
		t.Fatalf("safe defaults expected, got %+v", cfg.Payout)
	}
}

func TestLivePayoutsNeedRealKeysAndAnAccountNumber(t *testing.T) {
	ok, err := Load(lookupFrom(productionEnv(map[string]string{"PAYOUT_PROVIDER": "razorpayx", "RAZORPAYX_ACCOUNT_NUMBER": "2323230012345678"})))
	if err != nil || !ok.Payout.Live() || ok.Payout.AccountNumber == "" {
		t.Fatalf("valid live payout config rejected: %v %+v", err, ok.Payout)
	}
	cases := map[string]struct {
		extra map[string]string
		want  string
	}{
		"missing account number": {map[string]string{"PAYOUT_PROVIDER": "razorpayx"}, "RAZORPAYX_ACCOUNT_NUMBER"},
		"unknown provider":       {map[string]string{"PAYOUT_PROVIDER": "paypal"}, "PAYOUT_PROVIDER"},
		"unknown mode":           {map[string]string{"PAYOUT_MODE": "CARRIER_PIGEON"}, "PAYOUT_MODE"},
		"cool-down too short in production": {map[string]string{
			"PAYOUT_PROVIDER": "razorpayx", "RAZORPAYX_ACCOUNT_NUMBER": "1", "PAYOUT_BANK_COOLDOWN": "1m"}, "PAYOUT_BANK_COOLDOWN"},
		"absurd cap": {map[string]string{"PAYOUT_MAX_PAISE": "5"}, "PAYOUT_MAX_PAISE"},
	}
	for name, c := range cases {
		_, err := Load(lookupFrom(productionEnv(c.extra)))
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want an error mentioning %s, got %v", name, c.want, err)
		}
	}
}

func TestLivePayoutsRefuseSandboxPaymentKeys(t *testing.T) {
	// Development with no payment keys (sandbox) must not accept live payouts.
	_, err := Load(lookupFrom(map[string]string{
		"APP_ENV": "development", "DATABASE_URL": "postgres://u:p@localhost:5434/x",
		"PAYOUT_PROVIDER": "razorpayx", "RAZORPAYX_ACCOUNT_NUMBER": "1",
	}))
	if err == nil || !strings.Contains(err.Error(), "PAYOUT_PROVIDER") {
		t.Fatalf("live payouts without real payment keys must be refused, got %v", err)
	}
}
