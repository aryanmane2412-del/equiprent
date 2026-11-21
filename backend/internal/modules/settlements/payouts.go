package settlements

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/ledger"
)

// AccountKind says which payout accounts a gateway may pay: accounts verified by
// the same provider in the same mode. A simulated or test-mode account is never
// sent live money (a real payout to a fake fund account would only fail), so after
// switching from test to live keys owners must verify their account again.
func AccountKind(gw PayoutGateway) (provider, mode string) {
	switch {
	case gw == nil:
		return "", ""
	case gw.Sandbox():
		return "simulated", "simulated"
	}
	if m, ok := gw.(interface{ ModeName() string }); ok {
		return "razorpay", m.ModeName()
	}
	return "razorpay", "live"
}

// RetryBackoff is how long to wait after the n-th failed attempt before trying the
// same money again: 30m, 1h, 2h, 4h, then 6h. A persistent problem (an empty
// RazorpayX balance, a closed account) is not hammered every 15 minutes.
func RetryBackoff(attempts int32) time.Duration {
	d := 30 * time.Minute << min(max(attempts-1, 0), 4)
	return min(d, 6*time.Hour)
}

// live reports whether the gateway moves real money (asynchronously).
func (s *Service) live() bool { return s.Gateway != nil && !s.Gateway.Sandbox() }

// RunPayouts creates one payout per owner for eligible settlements and submits
// it. The idempotency key hashes the owner and settlement set, so a crash and
// re-run cannot create a second payout for the same settlements.
//
// Simulated gateway: the transfer completes at once (development only).
// Live gateway: the payout stays "processing" until SettleProcessing sees the
// bank's final answer; payouts whose submission outcome is unknown are re-sent
// by SubmitPending with the same key, which the gateway de-duplicates.
func (s *Service) RunPayouts(ctx context.Context, limit int32) error {
	if s.Gateway == nil {
		return ErrNoGateway
	}
	if s.Gateway.Sandbox() && s.ForbidSimulated {
		return ErrSimulatedForbidden
	}
	var rows []db.PayableSettlementsRow
	if err := s.Tx.InTx(ctx, func(ctx context.Context) (err error) {
		provider, mode := AccountKind(s.Gateway)
		rows, err = s.q(ctx).PayableSettlements(ctx, db.PayableSettlementsParams{CooldownSecs: s.BankCooldown.Seconds(), Provider: provider, Mode: mode, Lim: limit})
		return
	}); err != nil {
		return err
	}
	byOwner := map[uuid.UUID][]db.PayableSettlementsRow{}
	for _, r := range rows {
		byOwner[r.OwnerOrgID] = append(byOwner[r.OwnerOrgID], r)
	}
	for owner, group := range byOwner {
		if err := s.payOwner(ctx, owner, s.withinCap(owner, group)); err != nil {
			s.Log.Error("payout failed", "owner", owner, "err", err)
		}
	}
	if s.live() {
		return s.SubmitPending(ctx, limit)
	}
	return nil
}

// withinCap keeps the oldest settlements whose sum fits the per-payout cap; a
// single settlement above the cap is left for a person to release.
func (s *Service) withinCap(owner uuid.UUID, group []db.PayableSettlementsRow) []db.PayableSettlementsRow {
	if !s.live() || s.MaxPaise <= 0 {
		return group
	}
	var out []db.PayableSettlementsRow
	var sum int64
	for _, g := range group {
		if g.NetPayablePaise > s.MaxPaise {
			s.Log.Error("ALERT: settlement above the payout cap needs a manual release", "owner", owner, "settlement", g.ID, "paise", g.NetPayablePaise, "cap", s.MaxPaise)
			continue
		}
		if sum+g.NetPayablePaise > s.MaxPaise {
			continue // paid in a later run
		}
		sum += g.NetPayablePaise
		out = append(out, g)
	}
	return out
}

func (s *Service) payOwner(ctx context.Context, owner uuid.UUID, group []db.PayableSettlementsRow) error {
	if len(group) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(group))
	var total int64
	for i, g := range group {
		ids[i] = g.ID
		total += g.NetPayablePaise
	}
	base := IdemKey(owner, ids)
	key := base
	var prior db.FailedPayoutAttemptsRow
	if err := s.Tx.InTx(ctx, func(ctx context.Context) (err error) {
		prior, err = s.q(ctx).FailedPayoutAttempts(ctx, db.FailedPayoutAttemptsParams{OwnerOrgID: owner, BaseKey: base})
		return
	}); err != nil {
		return err
	}
	if prior.Attempts > 0 {
		if wait := RetryBackoff(prior.Attempts); time.Duration(prior.SecsSinceLast*float64(time.Second)) < wait {
			s.Log.Debug("payout retry is waiting out its backoff", "owner", owner, "attempts", prior.Attempts, "wait", wait)
			return nil
		}
		key = fmt.Sprintf("%s:%d", base, prior.Attempts) // a retry is a new payout with its own key
	}
	var payoutID uuid.UUID
	err := s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payout"}, func(ctx context.Context) error {
		q := s.q(ctx)
		id, err := q.InsertPayoutIdem(ctx, db.InsertPayoutIdemParams{OwnerOrgID: owner,
			BankAccountID:   uuid.NullUUID{UUID: group[0].BankAccountID, Valid: true},
			LinkedAccountID: uuid.NullUUID{UUID: group[0].LinkedID, Valid: true}, AmountPaise: total, IdempotencyKey: &key})
		if err != nil {
			if errors.Is(database.MapError(err), apperror.ErrNotFound) {
				return nil // already recorded: never send twice
			}
			return err
		}
		payoutID = id
		for _, g := range group {
			if err := q.InsertPayoutItem(ctx, db.InsertPayoutItemParams{PayoutID: id, SettlementID: g.ID, AmountPaise: g.NetPayablePaise}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil || payoutID == uuid.Nil {
		return err
	}
	tid, terr := s.Gateway.Transfer(ctx, key, group[0].AccountID, total)
	if s.Gateway.Sandbox() {
		if terr != nil {
			return s.failPayout(ctx, payoutID, terr.Error())
		}
		return s.recordSubmitted(ctx, payoutID, tid, owner, total, true)
	}
	return s.afterLiveTransfer(ctx, payoutID, tid, terr, owner, total)
}

// afterLiveTransfer records the result of submitting a live payout.
func (s *Service) afterLiveTransfer(ctx context.Context, payoutID uuid.UUID, tid string, terr error, owner uuid.UUID, total int64) error {
	switch {
	case terr == nil:
		return s.recordSubmitted(ctx, payoutID, tid, owner, total, false)
	case errors.Is(terr, ErrTransferRejected):
		s.Log.Error("ALERT: payout rejected by the gateway; settlements released for a retry", "payout", payoutID, "owner", owner, "err", terr)
		return s.failPayout(ctx, payoutID, terr.Error())
	default:
		// Unknown outcome (timeout, 5xx, anything unrecognised): the money may have left.
		// Keep the payout "processing"; SubmitPending re-sends with the same key.
		s.Log.Warn("payout submission outcome unknown; will re-send with the same idempotency key", "payout", payoutID, "err", terr)
		return nil
	}
}

// recordSubmitted stores the gateway's transfer id. With complete=true (the
// simulator) the payout is also finished as paid.
func (s *Service) recordSubmitted(ctx context.Context, payoutID uuid.UUID, tid string, owner uuid.UUID, total int64, complete bool) error {
	return s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payout"}, func(ctx context.Context) error {
		q := s.q(ctx)
		if err := q.SetPayoutGatewayID(ctx, db.SetPayoutGatewayIDParams{ID: payoutID, GatewayTransferID: &tid}); err != nil {
			return err
		}
		if !complete {
			return nil
		}
		return s.finishPaid(ctx, q, payoutID, owner, total)
	})
}

// finishPaid marks a payout paid, its settlements paid, and posts the ledger
// entry. A payout that is no longer "processing" is left alone (never twice).
func (s *Service) finishPaid(ctx context.Context, q *db.Queries, payoutID, owner uuid.UUID, total int64) error {
	if _, err := q.FinishPayout(ctx, db.FinishPayoutParams{ID: payoutID, Status: db.PayoutStatusPaid}); err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return nil
		}
		return err
	}
	items, err := q.PayoutSettlements(ctx, payoutID)
	if err != nil {
		return err
	}
	// The money has left, so the settlements are paid whatever state they are in now. A dispute
	// opened after the payout was created moves a settlement to on_hold; insisting on "eligible"
	// would roll everything back and leave a paid payout "processing" forever.
	for _, it := range items {
		_, err := q.SetSettlementStatus(ctx, db.SetSettlementStatusParams{ID: it.ID, Status: db.SettlementStatusPaid,
			FromStatuses: []string{string(db.SettlementStatusEligible), string(db.SettlementStatusOnHold), string(db.SettlementStatusPending)}})
		if err != nil && !errors.Is(database.MapError(err), apperror.ErrNotFound) { // not found = already paid
			return err
		}
	}
	_, err = ledger.Post(ctx, q, ledger.Posting{Kind: ledger.KindPayout, ReferenceType: "payout", ReferenceID: payoutID, Memo: "Owner payout",
		Legs: []ledger.Leg{ledger.DrOwner(owner, total), ledger.Cr(ledger.GatewayClearing, total)}})
	return err
}

// failPayout closes a payout that did not complete and frees its settlements
// for a later retry. No ledger entry: no money left.
func (s *Service) failPayout(ctx context.Context, payoutID uuid.UUID, reason string) error {
	return s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payout"}, func(ctx context.Context) error {
		q := s.q(ctx)
		if _, err := q.FinishPayout(ctx, db.FinishPayoutParams{ID: payoutID, Status: db.PayoutStatusFailed, FailureReason: &reason}); err != nil {
			if errors.Is(database.MapError(err), apperror.ErrNotFound) {
				return nil
			}
			return err
		}
		return q.DeactivatePayoutItems(ctx, payoutID)
	})
}

// SubmitPending re-sends live payouts that were recorded but never confirmed as
// submitted. The same idempotency key makes the gateway return the original
// payout if it did go through, so this can never pay twice.
func (s *Service) SubmitPending(ctx context.Context, limit int32) error {
	if !s.live() {
		return nil
	}
	var rows []db.PayoutsToSubmitRow
	if err := s.Tx.InTx(ctx, func(ctx context.Context) (err error) {
		provider, mode := AccountKind(s.Gateway)
		rows, err = s.q(ctx).PayoutsToSubmit(ctx, db.PayoutsToSubmitParams{MinAgeSecs: 30, Provider: provider, Mode: mode, Lim: limit})
		return
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if r.IdempotencyKey == nil {
			continue
		}
		tid, terr := s.Gateway.Transfer(ctx, *r.IdempotencyKey, r.AccountID, r.AmountPaise)
		if err := s.afterLiveTransfer(ctx, r.ID, tid, terr, r.OwnerOrgID, r.AmountPaise); err != nil {
			s.Log.Error("re-sending payout failed", "payout", r.ID, "err", err)
		}
	}
	return nil
}

// SettleProcessing asks the gateway for the bank's final answer on every live
// payout still "processing", and finishes it: paid (settlements paid, ledger
// posted) or failed (settlements freed). A payout is never marked paid unless
// the gateway says processed AND the amount matches ours.
func (s *Service) SettleProcessing(ctx context.Context, limit int32) error {
	if !s.live() {
		return nil
	}
	var rows []db.PayoutsToSettleRow
	if err := s.Tx.InTx(ctx, func(ctx context.Context) (err error) {
		rows, err = s.q(ctx).PayoutsToSettle(ctx, limit)
		return
	}); err != nil {
		return err
	}
	for _, r := range rows {
		if r.GatewayTransferID == nil {
			continue
		}
		st, amt, err := s.Gateway.Status(ctx, *r.GatewayTransferID)
		if err != nil {
			s.Log.Warn("payout status unavailable", "payout", r.ID, "err", err)
			continue
		}
		switch ClassifyStatus(st) {
		case OutcomePaid:
			if amt != r.AmountPaise {
				s.Log.Error("ALERT: gateway payout amount differs from ours; not marking paid", "payout", r.ID, "ours", r.AmountPaise, "gateway", amt)
				note := fmt.Sprintf("ours: %d; gateway: %s %d", r.AmountPaise, st, amt)
				_ = s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "reconcile"}, func(ctx context.Context) error {
					return s.q(ctx).InsertReconciliation(ctx, db.InsertReconciliationParams{PayoutID: r.ID, GatewayStatus: st, GatewayPaise: amt, Matches: false, Note: &note})
				})
				continue
			}
			err = s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payout"}, func(ctx context.Context) error {
				return s.finishPaid(ctx, s.q(ctx), r.ID, r.OwnerOrgID, r.AmountPaise)
			})
		case OutcomeFailed:
			s.Log.Error("ALERT: payout failed at the gateway; settlements released for a retry", "payout", r.ID, "status", st)
			err = s.failPayout(ctx, r.ID, "gateway status: "+st)
		default:
			continue
		}
		if err != nil {
			s.Log.Error("settling payout failed", "payout", r.ID, "err", err)
		}
	}
	return nil
}

// Reconcile compares each payout with the gateway's view and records the result;
// mismatches surface to finance for a person to resolve. Payouts still on their
// way are left to SettleProcessing.
func (s *Service) Reconcile(ctx context.Context, limit int32) error {
	if s.Gateway == nil {
		return nil
	}
	return s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "reconcile"}, func(ctx context.Context) error {
		q := s.q(ctx)
		ps, err := q.PayoutsToReconcile(ctx, limit)
		if err != nil {
			return err
		}
		for _, p := range ps {
			gid, err := q.GetPayoutGatewayID(ctx, p.ID)
			if err != nil || gid == nil {
				continue
			}
			st, amt, err := s.Gateway.Status(ctx, *gid)
			if err != nil {
				continue
			}
			if p.Status == "processing" && ClassifyStatus(st) == OutcomePending {
				continue
			}
			if amt < 0 {
				amt = p.AmountPaise
			}
			ok := ClassifyStatus(st) == OutcomePaid && amt == p.AmountPaise && p.Status == "paid"
			note := ""
			if !ok {
				note = fmt.Sprintf("ours: %s %d; gateway: %s %d", p.Status, p.AmountPaise, st, amt)
			}
			if err := q.InsertReconciliation(ctx, db.InsertReconciliationParams{PayoutID: p.ID, GatewayStatus: st, GatewayPaise: amt, Matches: ok, Note: &note}); err != nil {
				return err
			}
		}
		return nil
	})
}
