package settlements

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/ledger"
)

// ErrLiveTransfersDisabled is returned when a non-sandbox payout gateway is wired
// before payout verification and reconciliation have been tested.
var ErrLiveTransfersDisabled = errors.New("live payout transfers are disabled until verification and reconciliation are tested")

// PayoutGateway moves money to an owner. Only SandboxGateway exists: no real
// transfer code ships until reconciliation has been tested.
type PayoutGateway interface {
	Sandbox() bool
	Transfer(ctx context.Context, idempotencyKey, linkedAccountID string, paise int64) (transferID string, err error)
	// Status returns the gateway's state and amount; amount -1 means "as sent".
	Status(ctx context.Context, transferID string) (status string, paise int64, err error)
}

// SandboxGateway simulates transfers; linked account ids ending in "0000" fail.
type SandboxGateway struct{}

func (SandboxGateway) Sandbox() bool { return true }
func (SandboxGateway) Transfer(_ context.Context, key, acct string, _ int64) (string, error) {
	if len(acct) >= 4 && acct[len(acct)-4:] == "0000" {
		return "", errors.New("sandbox: transfer rejected")
	}
	return "sbx_" + key[:16], nil
}
func (SandboxGateway) Status(context.Context, string) (string, int64, error) {
	return "processed", -1, nil
}

type Service struct {
	Pool    *pgxpool.Pool
	Tx      database.Transactor
	Gateway PayoutGateway
	Hold    time.Duration // completion -> eligible (BOOKING_SETTLEMENT_HOLD)
	Log     *slog.Logger
}

func (s *Service) q(ctx context.Context) *db.Queries { return database.Queries(ctx, s.Pool) }

func (s *Service) rate(ctx context.Context, q *db.Queries, code string, at time.Time) (int, error) {
	v, err := q.CurrentTaxRate(ctx, db.CurrentTaxRateParams{Code: code, At: at})
	if err != nil {
		return 0, fmt.Errorf("tax rate %s: %w", code, err)
	}
	return int(v), nil
}

// CreateSettlements settles finished bookings. Idempotent: settlements.booking_id is
// unique, the ledger posting is unique per settlement, and each booking commits on its
// own, so a crash mid-run loses nothing and a re-run repeats nothing.
func (s *Service) CreateSettlements(ctx context.Context, limit int32) (int, error) {
	var ids []uuid.UUID
	if err := s.Tx.InTx(ctx, func(ctx context.Context) (err error) {
		ids, err = s.q(ctx).SettlementCandidates(ctx, limit)
		return
	}); err != nil {
		return 0, err
	}
	n := 0
	for _, id := range ids {
		err := s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "settlement"}, func(ctx context.Context) error {
			made, err := s.settle(ctx, id)
			if made {
				n++
			}
			return err
		})
		if err != nil {
			s.Log.Error("settlement failed", "booking", id, "err", err)
		}
	}
	return n, nil
}

func (s *Service) settle(ctx context.Context, bookingID uuid.UUID) (bool, error) {
	q := s.q(ctx)
	in, err := q.SettlementInputs(ctx, bookingID)
	if err != nil {
		return false, err
	}
	var p struct {
		Rental   int64 `json:"rental_paise"`
		Delivery int64 `json:"mobilisation_paise"`
		Fee      int64 `json:"platform_fee_paise"`
		Total    int64 `json:"estimated_total_paise"`
	}
	if err := json.Unmarshal(in.Price, &p); err != nil {
		return false, err
	}
	at := in.StatusChangedAt
	gst, err := s.rate(ctx, q, "gst_platform", at)
	if err != nil {
		return false, err
	}
	tcs, err := s.rate(ctx, q, "tcs_gst_52", at)
	if err != nil {
		return false, err
	}
	tdsCode := "tds_194o"
	if !in.OwnerPan {
		tdsCode = "tds_194o_no_pan"
	}
	tds, err := s.rate(ctx, q, tdsCode, at)
	if err != nil {
		return false, err
	}
	cr, err := q.CommissionForBooking(ctx, bookingID)
	if err != nil {
		return false, fmt.Errorf("commission rule: %w", err)
	}
	rows, err := q.NetCapturedByPurpose(ctx, bookingID)
	if err != nil {
		return false, err
	}
	var paid, refunded int64
	for _, r := range rows {
		if r.Purpose != "deposit" {
			paid += r.CapturedPaise
			refunded += r.RefundedPaise
		}
	}
	refunded = min(refunded, paid)
	feeTax := int64(gst) * p.Fee / 10000
	res, err := Compute(Input{PaidPaise: paid, RefundedPaise: refunded, EstTotalPaise: p.Total, EstRentalPaise: p.Rental,
		EstDeliveryPaise: p.Delivery, EstFeePaise: p.Fee, EstFeeTaxPaise: feeTax, OwnerGST: in.OwnerGst,
		Rates: Rates{CommissionBps: int(cr.CommissionBps), GSTBps: gst, TCSBps: tcs, TDSBps: tds}})
	if err != nil {
		return false, err
	}
	st, err := q.InsertSettlement(ctx, db.InsertSettlementParams{BookingID: bookingID, OwnerOrgID: in.OwnerOrgID,
		CommissionRuleID: uuid.NullUUID{UUID: cr.ID, Valid: true}, CommissionBps: cr.CommissionBps,
		GrossPaise: res.Gross, CommissionPaise: res.Commission, GstOnCommissionPaise: res.CommissionGST,
		TcsPaise: res.TCS, TdsPaise: res.TDS, NetPayablePaise: res.Net, EligibleAt: time.Now().Add(s.Hold), Status: db.SettlementStatusPending,
		CustomerPaidPaise: res.Customer, RentalPaise: res.Rental, DeliveryPaise: res.Delivery, OwnerTaxPaise: res.OwnerTax,
		PlatformFeePaise: res.Fee, PlatformFeeTaxPaise: res.FeeTax, CommissionBasePaise: res.Base, RefundedPaise: refunded})
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return false, nil // already settled
		}
		return false, err
	}
	// Customer funds -> owner payable and platform fee; then commission, GST and withholding out of owner payable.
	posted, err := ledger.Post(ctx, q, ledger.Posting{Kind: ledger.KindSettlement, BookingID: bookingID, ReferenceType: "settlement",
		ReferenceID: st.ID, Memo: "Owner settlement", Legs: []ledger.Leg{
			ledger.Dr(ledger.CustomerFunds, res.Customer),
			ledger.CrOwner(in.OwnerOrgID, res.Customer-res.Fee-res.FeeTax),
			ledger.Cr(ledger.FeeRevenue, res.Fee),
			ledger.Cr(ledger.GSTOutput, res.FeeTax+res.CommissionGST),
			ledger.DrOwner(in.OwnerOrgID, res.Commission+res.CommissionGST+res.TCS+res.TDS),
			ledger.Cr(ledger.CommissionRevenue, res.Commission),
			ledger.Cr(ledger.TCSPayable, res.TCS),
			ledger.Cr(ledger.TDSPayable, res.TDS),
		}})
	return posted, err
}

// ReleaseDue holds disputed settlements, moves settlements past their hold window to
// eligible, and closes zero-value ones.
func (s *Service) ReleaseDue(ctx context.Context) error {
	return s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "settlement release"}, func(ctx context.Context) error {
		q := s.q(ctx)
		if _, err := q.HoldDisputedSettlements(ctx); err != nil {
			return err
		}
		if _, err := q.ReleaseDueSettlements(ctx); err != nil {
			return err
		}
		_, err := q.ZeroSettlementsPaid(ctx)
		return err
	})
}

// IdemKey identifies a payout by owner and settlement set.
func IdemKey(owner uuid.UUID, ids []uuid.UUID) string {
	ss := make([]string, len(ids))
	for i, id := range ids {
		ss[i] = id.String()
	}
	sort.Strings(ss)
	h := sha256.New()
	h.Write([]byte(owner.String()))
	for _, x := range ss {
		h.Write([]byte(x))
	}
	return hex.EncodeToString(h.Sum(nil))
}

// RunPayouts pays eligible settlements, one payout per owner. The key hashes the owner and
// settlement set, so a crash and re-run cannot create a second payout for the same settlements.
// It refuses to run against a non-sandbox gateway.
func (s *Service) RunPayouts(ctx context.Context, limit int32) error {
	if s.Gateway == nil || !s.Gateway.Sandbox() {
		return ErrLiveTransfersDisabled
	}
	var rows []db.PayableSettlementsRow
	if err := s.Tx.InTx(ctx, func(ctx context.Context) (err error) {
		rows, err = s.q(ctx).PayableSettlements(ctx, limit)
		return
	}); err != nil {
		return err
	}
	byOwner := map[uuid.UUID][]db.PayableSettlementsRow{}
	for _, r := range rows {
		byOwner[r.OwnerOrgID] = append(byOwner[r.OwnerOrgID], r)
	}
	for owner, group := range byOwner {
		if err := s.payOwner(ctx, owner, group); err != nil {
			s.Log.Error("payout failed", "owner", owner, "err", err)
		}
	}
	return nil
}

func (s *Service) payOwner(ctx context.Context, owner uuid.UUID, group []db.PayableSettlementsRow) error {
	ids := make([]uuid.UUID, len(group))
	var total int64
	for i, g := range group {
		ids[i] = g.ID
		total += g.NetPayablePaise
	}
	key := IdemKey(owner, ids)
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
	return s.Tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payout"}, func(ctx context.Context) error {
		q := s.q(ctx)
		if terr != nil {
			reason := terr.Error()
			if _, err := q.FinishPayout(ctx, db.FinishPayoutParams{ID: payoutID, Status: db.PayoutStatusFailed, FailureReason: &reason}); err != nil {
				return err
			}
			return q.DeactivatePayoutItems(ctx, payoutID) // frees the settlements for a retry
		}
		if err := q.SetPayoutGatewayID(ctx, db.SetPayoutGatewayIDParams{ID: payoutID, GatewayTransferID: &tid}); err != nil {
			return err
		}
		if _, err := q.FinishPayout(ctx, db.FinishPayoutParams{ID: payoutID, Status: db.PayoutStatusPaid}); err != nil {
			return err
		}
		for _, g := range group {
			if _, err := q.SetSettlementStatus(ctx, db.SetSettlementStatusParams{ID: g.ID, Status: db.SettlementStatusPaid,
				FromStatuses: []db.SettlementStatus{db.SettlementStatusEligible}}); err != nil {
				return err
			}
		}
		_, err := ledger.Post(ctx, q, ledger.Posting{Kind: ledger.KindPayout, ReferenceType: "payout", ReferenceID: payoutID, Memo: "Owner payout",
			Legs: []ledger.Leg{ledger.DrOwner(owner, total), ledger.Cr(ledger.GatewayClearing, total)}})
		return err
	})
}

// Reconcile compares each unreconciled payout with the gateway's view and records the result;
// mismatches surface to finance for a person to resolve.
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
			if amt < 0 {
				amt = p.AmountPaise
			}
			ok := st == "processed" && amt == p.AmountPaise && p.Status == "paid"
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
