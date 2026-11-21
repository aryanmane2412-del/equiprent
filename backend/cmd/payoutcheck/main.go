// Command payoutcheck exercises the RazorpayX integration against the REAL
// RazorpayX API in TEST mode, so the assumptions the unit tests could only
// mock are checked before any live money moves:
//
//   - a bank account can be verified (penny drop) and the holder name comes back
//   - a payout is accepted, and sending the SAME idempotency key twice returns the
//     SAME payout (this is what prevents paying an owner twice)
//   - the payout reaches a final status and the amount matches
//   - a payout the account cannot afford is refused (not "unknown")
//
// It refuses to run with live keys and never prints a key or a full account number.
//
//	go run ./cmd/payoutcheck -name "Test Owner" -account 1234567890 -ifsc HDFC0001234
//
// Needs, in ../.env or the environment: PAYMENT_KEY_ID / PAYMENT_KEY_SECRET (rzp_test_
// keys), PAYOUT_PROVIDER=razorpayx and RAZORPAYX_ACCOUNT_NUMBER (your RazorpayX TEST account).
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/equiprent/backend/internal/bootstrap"
	"github.com/equiprent/backend/internal/modules/settlements"
	"github.com/equiprent/backend/internal/modules/verification"
	"github.com/equiprent/backend/internal/payouts/razorpayx"
)

var failures int

func report(ok bool, name, detail string) {
	mark := "PASS"
	if !ok {
		mark = "FAIL"
		failures++
	}
	fmt.Printf("  [%s] %s", mark, name)
	if detail != "" {
		fmt.Printf("  -- %s", detail)
	}
	fmt.Println()
}

func main() {
	name := flag.String("name", "", "account holder name (as at the bank)")
	account := flag.String("account", "", "bank account number to verify and pay")
	ifsc := flag.String("ifsc", "", "IFSC of that account")
	paise := flag.Int64("amount-paise", 100, "test payout amount in paise (100 = INR 1)")
	wait := flag.Duration("wait", 2*time.Minute, "how long to wait for the payout's final status")
	overBalance := flag.Bool("over-balance", true, "also try a payout larger than the balance, which must be refused")
	flag.Parse()

	cfg, _, err := bootstrap.Init("payoutcheck")
	if err != nil {
		fail(err.Error())
	}
	switch {
	case !cfg.Payout.Live():
		fail("set PAYOUT_PROVIDER=razorpayx and RAZORPAYX_ACCOUNT_NUMBER (your RazorpayX TEST account) in ../.env first")
	case !strings.HasPrefix(cfg.Payments.KeyID, "rzp_test_"):
		fail("this checker only runs with rzp_test_ keys; it will not touch live keys")
	case *name == "" || *account == "" || *ifsc == "":
		fail("pass -name, -account and -ifsc of a test bank account")
	}
	client := razorpayx.New(cfg.Payments.KeyID, cfg.Payments.KeySecret, cfg.Payout.AccountNumber, cfg.Payout.Mode)
	ctx := context.Background()
	last4 := (*account)[max(0, len(*account)-4):]

	fmt.Printf("RazorpayX test-mode check  (mode=%s, rail=%s, account ending %s)\n\n", client.ModeName(), cfg.Payout.Mode, last4)

	// 1. Bank account verification (penny drop).
	fmt.Println("1. Bank account verification")
	res, err := client.VerifyAccount(ctx, verification.PayoutAccount{HolderName: *name, AccountNumber: *account, IFSC: strings.ToUpper(*ifsc)})
	switch {
	case err != nil:
		report(false, "verification ran", err.Error())
		summary()
	case !res.Verified:
		report(false, "account verified", res.Reason)
		fmt.Println("     (in test mode use a RazorpayX test bank account; a refusal here may be correct)")
		summary()
	}
	report(true, "account verified", "fund account "+res.Reference+", provider "+res.Provider+"/"+res.Mode)

	// 2. Payout, and the idempotency guarantee.
	fmt.Println("\n2. Payout and idempotency")
	key := "check-" + randomHex(8)
	id1, err := client.Transfer(ctx, key, res.Reference, *paise)
	if err != nil {
		report(false, "payout accepted", err.Error())
		summary()
	}
	report(true, "payout accepted", "payout "+id1)
	id2, err := client.Transfer(ctx, key, res.Reference, *paise)
	switch {
	case err != nil:
		report(false, "same key sent again returns the original payout", err.Error())
	case id2 != id1:
		report(false, "same key sent again returns the original payout",
			"got a DIFFERENT payout "+id2+": RazorpayX did not honour the idempotency key. DO NOT GO LIVE; an owner could be paid twice")
	default:
		report(true, "same key sent again returns the original payout", "no duplicate was created")
	}

	// 3. Final status and amount.
	fmt.Println("\n3. Payout status")
	deadline := time.Now().Add(*wait)
	var final string
	var amount int64
	seen := ""
	for time.Now().Before(deadline) {
		st, amt, err := client.Status(ctx, id1)
		if err != nil {
			report(false, "status readable", err.Error())
			break
		}
		if st != seen {
			fmt.Printf("     status: %s\n", st)
			seen = st
		}
		amount = amt
		if settlements.ClassifyStatus(st) != settlements.OutcomePending {
			final = st
			break
		}
		time.Sleep(3 * time.Second)
	}
	switch {
	case final == "":
		report(false, "reached a final status", "still '"+seen+"' after "+wait.String()+" (in test mode confirm the payout in the RazorpayX dashboard, then re-run with a longer -wait)")
	default:
		report(settlements.ClassifyStatus(final) == settlements.OutcomePaid, "payout completed", "final status "+final)
		report(amount == *paise, "gateway amount matches ours", fmt.Sprintf("sent %d paise, gateway says %d", *paise, amount))
	}

	// 4. A refusal must be classified as a refusal.
	if *overBalance {
		fmt.Println("\n4. Refusal handling")
		_, err := client.Transfer(ctx, "check-big-"+randomHex(6), res.Reference, 100_000_000_000)
		switch {
		case err == nil:
			report(false, "an unaffordable payout is refused", "it was ACCEPTED; check the balance and limits in RazorpayX")
		case errors.Is(err, settlements.ErrTransferRejected):
			report(true, "an unaffordable payout is refused as a definite rejection", "it will be retried safely, not treated as unknown")
		default:
			report(false, "an unaffordable payout is refused as a definite rejection", "classified as: "+err.Error()+" (an unknown outcome would stall the retry)")
		}
	}
	summary()
}

func summary() {
	fmt.Println()
	if failures > 0 {
		fmt.Printf("RESULT: %d check(s) FAILED. Send me this output and I will fix them. Do not enable live payouts yet.\n", failures)
		os.Exit(1)
	}
	fmt.Println("RESULT: all checks passed in test mode.")
	os.Exit(0)
}

func fail(msg string) {
	fmt.Fprintln(os.Stderr, "payoutcheck:", msg)
	os.Exit(2)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
