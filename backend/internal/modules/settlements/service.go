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

// ErrSimulatedForbidden is returned when the simulated gateway is wired where it
// must not run: it records owners as paid without moving any money.
var ErrSimulatedForbidden = errors.New("the simulated payout gateway must not run here")

// ErrNoGateway is returned when no payout gateway is configured.
var ErrNoGateway = errors.New("no payout gateway configured")

// PayoutGateway moves money to an owner. SandboxGateway simulates; a live
// gateway (internal/payouts/razorpayx) is asynchronous: Transfer only submits
// the payout, and Status later reports whether the bank completed it.
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

	// MaxPaise caps one live payout; bigger settlements wait for a person.
	MaxPaise int64
	// BankCooldown holds payouts after the owner's payout account changed.
	BankCooldown time.Duration
	// ForbidSimulated refuses to run the simulated gateway at all (production).
	ForbidSimulated bool
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
