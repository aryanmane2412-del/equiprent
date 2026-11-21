package tests

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/equiprent/backend/internal/logger"
	"github.com/equiprent/backend/internal/modules/settlements"
)

// payoutGateway behaves like a live, asynchronous payout gateway: Transfer
// only submits, Status reports the bank's answer, and (like RazorpayX with an
// idempotency header) a repeated key returns the original payout.
type payoutGateway struct {
	mu           sync.Mutex
	idem         map[string]string // idempotency key -> transfer id
	calls        int               // every Transfer call, including repeats
	transferTo   func(key string) (string, error)
	status       map[string]string // transfer id -> gateway status
	amount       map[string]int64  // transfer id -> amount the gateway reports (0 = ours)
	paise        map[string]int64
	sendThenFail bool // record the payout, then report an unknown outcome (a timeout after sending)
	failWith     error
}

func newPayoutGateway() *payoutGateway {
	return &payoutGateway{idem: map[string]string{}, status: map[string]string{}, amount: map[string]int64{}, paise: map[string]int64{}}
}

func (g *payoutGateway) Sandbox() bool { return false }

// ModeName makes the gateway a razorpay "test" gateway, so it may only pay razorpay/test accounts.
func (g *payoutGateway) ModeName() string { return "test" }

func (g *payoutGateway) Transfer(_ context.Context, key, _ string, paise int64) (string, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.calls++
	if id, ok := g.idem[key]; ok {
		return id, nil // de-duplicated: the original payout
	}
	if g.failWith != nil && !g.sendThenFail {
		return "", g.failWith
	}
	id := "pout_" + strconv.Itoa(len(g.idem)+1)
	g.idem[key], g.paise[id], g.status[id] = id, paise, "processing"
	if g.sendThenFail {
		g.sendThenFail = false
		return "", fmt.Errorf("%w: timeout after sending", settlements.ErrTransferUncertain)
	}
	return id, nil
}

func (g *payoutGateway) Status(_ context.Context, id string) (string, int64, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	amt := g.amount[id]
	if amt == 0 {
		amt = g.paise[id]
	}
	return g.status[id], amt, nil
}

func (g *payoutGateway) payouts() int { g.mu.Lock(); defer g.mu.Unlock(); return len(g.idem) }
func (g *payoutGateway) setAll(status string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	for id := range g.status {
		g.status[id] = status
	}
}

// payoutFixture is an isolated database with one verified owner and a number
// of eligible settlements for it.
type payoutFixture struct {
	t   *testing.T
	e   *env
	org string
}

func newPayoutFixture(t *testing.T, amounts ...int64) *payoutFixture {
	t.Helper()
	e := newIsolatedEnv(t)
	m := newMarket(t, e, nil)
	f := &payoutFixture{t: t, e: e, org: m.owner.org}
	base := time.Now().Add(96 * time.Hour).UTC().Truncate(time.Hour)
	for i, amt := range amounts {
		m.start = base.Add(time.Duration(i) * 10 * 24 * time.Hour)
		m.end = m.start.Add(72 * time.Hour)
		booking := m.request(t, "online")
		e.exec(`INSERT INTO settlements (booking_id, owner_org_id, commission_bps, gross_paise, commission_paise, net_payable_paise, eligible_at, status)
		        VALUES ($1, $2, 0, $3, 0, $3, now() - interval '1 hour', 'eligible')`, booking, f.org, amt)
	}
	return f
}

func (f *payoutFixture) service(gw settlements.PayoutGateway, tweak func(*settlements.Service)) *settlements.Service {
	log := logger.Discard()
	if os.Getenv("TEST_LOG") != "" {
		log = slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelDebug}))
	}
	s := &settlements.Service{Pool: f.e.pool, Tx: f.e.app.Tx, Gateway: gw, Log: log}
	if tweak != nil {
		tweak(s)
	}
	return s
}

// ageAccount makes the owner's payout account (and every payout) old enough to
// pass the cool-down and re-send checks; the updated_at trigger is paused
// because it would otherwise reset the timestamp.
func (f *payoutFixture) ageAccount() {
	f.e.exec(`ALTER TABLE payout_linked_accounts DISABLE TRIGGER payout_linked_accounts_updated_at;
	          UPDATE payout_linked_accounts SET updated_at = now() - interval '2 days';
	          ALTER TABLE payout_linked_accounts ENABLE TRIGGER payout_linked_accounts_updated_at;
	          ALTER TABLE payouts DISABLE TRIGGER payouts_updated_at;
	          UPDATE payouts SET created_at = now() - interval '1 hour';
	          ALTER TABLE payouts ENABLE TRIGGER payouts_updated_at;`)
}

// liveAccount turns the owner's payout account into a RazorpayX test-mode account, as verifying
// with the live provider would. The simulated account the sandbox created is not payable by a live gateway.
func (f *payoutFixture) liveAccount() {
	f.e.exec(`UPDATE payout_linked_accounts SET provider = 'razorpay', mode = 'test'`)
}

func (f *payoutFixture) count(sql string, args ...any) int {
	f.t.Helper()
	n, err := strconv.Atoi(f.e.query1(sql, args...))
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *payoutFixture) payoutStatuses() string {
	return f.e.query1(`SELECT COALESCE(string_agg(status::text, ',' ORDER BY created_at), '') FROM payouts`)
}
func (f *payoutFixture) settlementStatuses() string {
	return f.e.query1(`SELECT COALESCE(string_agg(status::text, ',' ORDER BY net_payable_paise), '') FROM settlements`)
}
func (f *payoutFixture) ledgerPayouts() int {
	return f.count(`SELECT count(*) FROM ledger_transactions WHERE kind = 'payout'`)
}

var pctx = context.Background()

// Verifying a bank account is what makes an owner payable.
func TestVerifiedBankAccountBecomesThePayoutAccount(t *testing.T) {
	f := newPayoutFixture(t)
	if n := f.count(`SELECT count(*) FROM payout_linked_accounts WHERE owner_org_id = $1 AND activation_status = 'activated'`, f.org); n != 1 {
		t.Fatalf("a verified owner must have exactly one activated payout account, got %d", n)
	}
	if got := f.e.query1(`SELECT provider || '/' || mode FROM payout_linked_accounts WHERE owner_org_id = $1`, f.org); got != "simulated/simulated" {
		t.Fatalf("sandbox verification must record the simulator, got %s", got)
	}
}

// A live payout is only "paid" once the bank says so, and then exactly once.
func TestLivePayoutIsPaidOnlyWhenTheBankConfirms(t *testing.T) {
	f := newPayoutFixture(t, 500_000, 700_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, nil)

	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 1 || f.payoutStatuses() != "processing" {
		t.Fatalf("one payout for the owner, still processing: gateway=%d db=%s", gw.payouts(), f.payoutStatuses())
	}
	if f.settlementStatuses() != "eligible,eligible" || f.ledgerPayouts() != 0 {
		t.Fatalf("submitting must not mark anything paid: %s, ledger=%d", f.settlementStatuses(), f.ledgerPayouts())
	}
	// The bank is still working: nothing changes.
	if err := svc.SettleProcessing(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "processing" || f.ledgerPayouts() != 0 {
		t.Fatalf("a payout in flight stayed processing, got %s ledger=%d", f.payoutStatuses(), f.ledgerPayouts())
	}
	// Running payouts again must not create a second payout for the same money.
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 1 || f.count(`SELECT count(*) FROM payouts`) != 1 {
		t.Fatalf("money already in a payout was paid again: gateway=%d", gw.payouts())
	}
	// The bank confirms: paid once, settlements paid, ledger posted once.
	gw.setAll("processed")
	for range 3 { // repeated runs are harmless
		if err := svc.SettleProcessing(pctx, 50); err != nil {
			t.Fatal(err)
		}
	}
	if f.payoutStatuses() != "paid" || f.settlementStatuses() != "paid,paid" || f.ledgerPayouts() != 1 {
		t.Fatalf("after confirmation: payouts=%s settlements=%s ledger=%d", f.payoutStatuses(), f.settlementStatuses(), f.ledgerPayouts())
	}
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.count(`SELECT count(*) FROM payouts`) != 1 {
		t.Fatal("paid settlements were paid out again")
	}
}

// A gateway amount that differs from ours is never marked paid.
func TestPayoutWithADifferentGatewayAmountIsNotMarkedPaid(t *testing.T) {
	f := newPayoutFixture(t, 500_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, nil)
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	gw.setAll("processed")
	gw.amount["pout_1"] = 499_999
	if err := svc.SettleProcessing(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "processing" || f.ledgerPayouts() != 0 {
		t.Fatalf("a mismatched amount must not be paid: %s ledger=%d", f.payoutStatuses(), f.ledgerPayouts())
	}
	if f.count(`SELECT count(*) FROM payout_reconciliations WHERE NOT matches`) != 1 {
		t.Fatal("the mismatch must be recorded for finance")
	}
}

// A refused or failed payout frees the money for a retry and posts nothing.
func TestRejectedAndFailedPayoutsAreFreedForRetry(t *testing.T) {
	f := newPayoutFixture(t, 500_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, nil)

	gw.failWith = fmt.Errorf("%w: not enough balance", settlements.ErrTransferRejected)
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "failed" || f.settlementStatuses() != "eligible" || f.ledgerPayouts() != 0 {
		t.Fatalf("after a refusal: payouts=%s settlements=%s ledger=%d", f.payoutStatuses(), f.settlementStatuses(), f.ledgerPayouts())
	}
	// Retried immediately? No: a persistent problem (an empty balance) must not be hammered every run.
	gw.failWith = nil
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "failed" || gw.payouts() != 0 {
		t.Fatalf("a failed attempt must wait out its backoff before the same money is retried: %s", f.payoutStatuses())
	}
	// After the backoff (balance topped up): the same money is paid, with a new idempotency key.
	f.backdatePayouts(1)
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "failed,processing" {
		t.Fatalf("retry must create a new payout, got %s", f.payoutStatuses())
	}
	// The bank then fails it (e.g. account closed): freed again, nothing posted.
	gw.setAll("failed")
	if err := svc.SettleProcessing(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "failed,failed" || f.settlementStatuses() != "eligible" || f.ledgerPayouts() != 0 {
		t.Fatalf("after a bank failure: payouts=%s settlements=%s ledger=%d", f.payoutStatuses(), f.settlementStatuses(), f.ledgerPayouts())
	}
}

// If the outcome of sending is unknown, the payout is NOT freed and NOT
// duplicated: it is re-sent with the same key and the gateway de-duplicates.
func TestUnknownOutcomeIsNeverDuplicated(t *testing.T) {
	f := newPayoutFixture(t, 500_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, nil)

	gw.sendThenFail = true // the payout left, but we never heard back
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "processing" || f.settlementStatuses() != "eligible" {
		t.Fatalf("an unknown outcome must stay in flight: %s / %s", f.payoutStatuses(), f.settlementStatuses())
	}
	if f.e.query1(`SELECT COALESCE(gateway_transfer_id, '') FROM payouts`) != "" {
		t.Fatal("no gateway id is known yet")
	}
	// Too soon to re-send (the gateway may still be answering)...
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.calls != 1 {
		t.Fatalf("re-sent immediately: %d calls", gw.calls)
	}
	// ...later the same key is re-sent and returns the original payout.
	f.ageAccount()
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 1 || f.count(`SELECT count(*) FROM payouts`) != 1 {
		t.Fatalf("the money was paid twice: gateway=%d", gw.payouts())
	}
	if f.e.query1(`SELECT COALESCE(gateway_transfer_id, '')  FROM payouts`) != "pout_1" {
		t.Fatal("the original payout id must be recorded after the re-send")
	}
	gw.setAll("processed")
	if err := svc.SettleProcessing(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "paid" || f.ledgerPayouts() != 1 {
		t.Fatalf("final: %s ledger=%d", f.payoutStatuses(), f.ledgerPayouts())
	}
}

// A new or changed payout account waits out the cool-down; large settlements
// wait for a person.
func TestCooldownAndCap(t *testing.T) {
	f := newPayoutFixture(t, 300_000, 900_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, func(s *settlements.Service) { s.BankCooldown = 24 * time.Hour; s.MaxPaise = 500_000 })

	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 0 {
		t.Fatal("a payout account added moments ago must not be paid yet (account-change fraud)")
	}
	f.ageAccount()
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 1 || f.e.query1(`SELECT amount_paise::text FROM payouts`) != "300000" {
		t.Fatalf("only the settlement within the cap is paid, got %d payouts", gw.payouts())
	}
	if f.settlementStatuses() != "eligible,eligible" {
		t.Fatalf("the oversize settlement stays eligible for a person: %s", f.settlementStatuses())
	}
}

// The simulator records owners as paid without moving money: forbidden where
// it must not run, still available in development.
func TestSimulatedGatewayGuard(t *testing.T) {
	f := newPayoutFixture(t, 500_000)
	forbidden := f.service(settlements.SandboxGateway{}, func(s *settlements.Service) { s.ForbidSimulated = true })
	if err := forbidden.RunPayouts(pctx, 50); !errors.Is(err, settlements.ErrSimulatedForbidden) {
		t.Fatalf("got %v", err)
	}
	if f.count(`SELECT count(*) FROM payouts`) != 0 {
		t.Fatal("nothing may be recorded when the simulator is forbidden")
	}
	dev := f.service(settlements.SandboxGateway{}, nil)
	if err := dev.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "paid" || f.settlementStatuses() != "paid" || f.ledgerPayouts() != 1 {
		t.Fatalf("development simulation still completes at once: %s %s %d", f.payoutStatuses(), f.settlementStatuses(), f.ledgerPayouts())
	}
}

// A dispute opened while a payout is in flight moves the settlement to on_hold. When the bank then
// confirms, the money has left: the payout must still complete (it used to roll back and stay
// "processing" forever).
func TestDisputeOpenedWhilePayoutIsInFlightStillCompletes(t *testing.T) {
	f := newPayoutFixture(t, 500_000)
	gw := newPayoutGateway()
	f.liveAccount()
	svc := f.service(gw, nil)
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	f.e.exec(`UPDATE settlements SET status = 'on_hold', hold_reason = 'A dispute is open on this booking'`)
	gw.setAll("processed")
	if err := svc.SettleProcessing(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if f.payoutStatuses() != "paid" || f.settlementStatuses() != "paid" || f.ledgerPayouts() != 1 {
		t.Fatalf("money already sent must be recorded as paid: payouts=%s settlements=%s ledger=%d", f.payoutStatuses(), f.settlementStatuses(), f.ledgerPayouts())
	}
}

// A live gateway only pays accounts its own provider and mode verified: a simulated account (created
// in development/staging) is never sent real money, and neither is a test-mode account under live keys.
func TestGatewayOnlyPaysItsOwnKindOfAccount(t *testing.T) {
	f := newPayoutFixture(t, 500_000) // the sandbox verified the owner: a simulated account
	gw := newPayoutGateway()
	svc := f.service(gw, nil)
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 0 || f.count(`SELECT count(*) FROM payouts`) != 0 {
		t.Fatal("a simulated payout account must not be sent a live payout")
	}
	f.e.exec(`UPDATE payout_linked_accounts SET provider = 'razorpay', mode = 'live'`) // live keys would be mode live; this gateway is test
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 0 {
		t.Fatal("a live-mode account must not be paid by a test-mode gateway")
	}
	f.liveAccount()
	if err := svc.RunPayouts(pctx, 50); err != nil {
		t.Fatal(err)
	}
	if gw.payouts() != 1 {
		t.Fatalf("the matching account is paid: %d", gw.payouts())
	}
}

// Backoff grows and is capped.
func TestRetryBackoffGrowsAndIsCapped(t *testing.T) {
	got := []string{}
	for _, n := range []int32{1, 2, 3, 4, 5, 9} {
		got = append(got, settlements.RetryBackoff(n).String())
	}
	want := []string{"30m0s", "1h0m0s", "2h0m0s", "4h0m0s", "6h0m0s", "6h0m0s"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("backoff = %v, want %v", got, want)
		}
	}
}

// The runbook's SQL must compute the same reference id the RazorpayX client sends.
func TestRunbookReferenceSQLMatchesTheClient(t *testing.T) {
	e := newEnv(t)
	sum := sha256.Sum256([]byte("key-abc"))
	want := hex.EncodeToString(sum[:])[:32]
	if got := e.query1(`SELECT left(encode(sha256(convert_to($1::text, 'UTF8')), 'hex'), 32)`, "key-abc"); got != want {
		t.Fatalf("runbook SQL gives %s, the client sends %s", got, want)
	}
}
