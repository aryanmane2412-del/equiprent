// Package payments collects advance, deposit and balance payments through
// the payment gateway, verifies checkout callbacks and webhooks (HMAC), and
// confirms bookings once the advance is captured. The webhook is the source
// of truth; it is stored in webhook_events and processed exactly once.
package payments

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/events"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/ledger"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// ---- domain ------------------------------------------------------------------------

// Payment is a payment as shown to the parties.
type Payment struct {
	ID               uuid.UUID  `json:"id"`
	BookingID        uuid.UUID  `json:"booking_id"`
	Purpose          string     `json:"purpose"`
	AmountPaise      int64      `json:"amount_paise"`
	Currency         string     `json:"currency"`
	Status           string     `json:"status"`
	Gateway          string     `json:"gateway"`
	GatewayOrderID   *string    `json:"gateway_order_id,omitempty"`
	GatewayPaymentID *string    `json:"gateway_payment_id,omitempty"`
	Method           *string    `json:"method,omitempty"`
	FailureReason    *string    `json:"failure_reason,omitempty"`
	ReceiptNumber    *string    `json:"receipt_number,omitempty"`
	CapturedAt       *time.Time `json:"captured_at,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
}

// Checkout is returned when a payment is created.
type Checkout struct {
	Payment  Payment        `json:"payment"`
	Checkout map[string]any `json:"checkout" doc:"Pass to the gateway's checkout SDK"`
	// Sandbox is set only when no gateway credentials are configured: post
	// it to /payments/{id}/verify to simulate a successful payment.
	Sandbox *VerifyRequest `json:"sandbox,omitempty"`
}

type CreateRequest struct {
	Purpose string `json:"purpose" binding:"required,oneof=advance deposit balance"`
}

type VerifyRequest struct {
	GatewayPaymentID string `json:"gateway_payment_id" binding:"required,max=64"`
	Signature        string `json:"signature" binding:"required,len=64,hexadecimal"`
}

type PaymentList struct {
	Data []Payment `json:"data"`
}

type WebhookAck struct {
	Status string `json:"status" doc:"processed, duplicate or ignored"`
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Create(ctx context.Context, p db.CreatePaymentParams) (Payment, error)
	ByIdempotencyKey(ctx context.Context, key string) (Payment, error)
	Get(ctx context.Context, id uuid.UUID) (Payment, error)
	ByOrderID(ctx context.Context, orderID string) (Payment, error)
	ForBooking(ctx context.Context, bookingID uuid.UUID) ([]Payment, error)
	Captured(ctx context.Context, bookingID uuid.UUID) (int64, error)
	Capture(ctx context.Context, id uuid.UUID, gatewayPaymentID, method string) (Payment, bool, error)
	Fail(ctx context.Context, id uuid.UUID, code, reason string) error
	RecordWebhook(ctx context.Context, p db.ClaimWebhookEventParams) (uuid.UUID, bool, error)
	MarkWebhook(ctx context.Context, id uuid.UUID, errMsg *string) error
	RetryableWebhooks(ctx context.Context, max, batch int32) ([]db.ListRetryableWebhooksRow, error)
	ClaimWebhook(ctx context.Context, id uuid.UUID) ([]byte, bool, error)
	OpenFor(ctx context.Context, bookingID uuid.UUID, purpose string) (Payment, bool, error)
	OtherCaptured(ctx context.Context, bookingID uuid.UUID, purpose string, exclude uuid.UUID) (int, error)
	Lock(ctx context.Context, key string) error
	Emit(ctx context.Context, paymentID uuid.UUID, eventType string, p events.Payload) error
}

// refundQueries is the refund worker's view of the database (generated
// queries, run in the job's transaction).
type refundQueries interface {
	ClaimRefundsForSubmit(ctx context.Context, arg db.ClaimRefundsForSubmitParams) ([]db.ClaimRefundsForSubmitRow, error)
	MarkRefundSubmitted(ctx context.Context, arg db.MarkRefundSubmittedParams) error
	MarkRefundRetry(ctx context.Context, arg db.MarkRefundRetryParams) error
	SubmittedRefundsToCheck(ctx context.Context, batchSize int32) ([]db.SubmittedRefundsToCheckRow, error)
	TouchRefundCheck(ctx context.Context, id uuid.UUID) error
	GetRefundLedgerInfo(ctx context.Context, id uuid.UUID) (db.GetRefundLedgerInfoRow, error)
	MarkRefundProcessed(ctx context.Context, arg db.MarkRefundProcessedParams) (uuid.UUID, error)
	MarkRefundFailed(ctx context.Context, arg db.MarkRefundFailedParams) (uuid.UUID, error)
	GetRefundByGatewayID(ctx context.Context, gatewayRefundID *string) (db.GetRefundByGatewayIDRow, error)
	FinishRefundedBookings(ctx context.Context, batchSize int32) ([]db.FinishRefundedBookingsRow, error)
	InsertOutboxEvent(ctx context.Context, arg db.InsertOutboxEventParams) error
	CreateRefund(ctx context.Context, arg db.CreateRefundParams) (uuid.UUID, error)
	// ledger
	EnsureLedgerAccount(ctx context.Context, arg db.EnsureLedgerAccountParams) (uuid.UUID, error)
	InsertLedgerTransaction(ctx context.Context, arg db.InsertLedgerTransactionParams) (uuid.UUID, error)
	InsertLedgerEntry(ctx context.Context, arg db.InsertLedgerEntryParams) error
	// security deposits
	InsertSecurityDeposit(ctx context.Context, arg db.InsertSecurityDepositParams) error
	GetDepositForUpdate(ctx context.Context, id uuid.UUID) (db.SecurityDeposit, error)
	ResolveDeposit(ctx context.Context, arg db.ResolveDepositParams) (db.SecurityDeposit, error)
	DepositsToRelease(ctx context.Context, arg db.DepositsToReleaseParams) ([]db.SecurityDeposit, error)
	ApprovedDamageDeductions(ctx context.Context, arg db.ApprovedDamageDeductionsParams) (int64, error)
	HeldDepositsOfCancelled(ctx context.Context, lim int32) ([]db.SecurityDeposit, error)
	ProcessedRefundTotal(ctx context.Context, paymentID uuid.UUID) (int64, error)
	DepositRefundedOnCancel(ctx context.Context, arg db.DepositRefundedOnCancelParams) error
	RefundablePayment(ctx context.Context, id uuid.UUID) (db.RefundablePaymentRow, error)
}

type PGRepository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *PGRepository { return &PGRepository{pool: pool} }

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func toPayment(p db.Payment) Payment {
	out := Payment{
		ID: p.ID, BookingID: p.BookingID, Purpose: string(p.Purpose), AmountPaise: p.AmountPaise, Currency: p.Currency,
		Status: string(p.Status), Gateway: p.Gateway, GatewayOrderID: p.GatewayOrderID, GatewayPaymentID: p.GatewayPaymentID,
		FailureReason: p.FailureReason, ReceiptNumber: p.ReceiptNumber, CapturedAt: p.CapturedAt, CreatedAt: p.CreatedAt,
	}
	if p.Method.Valid {
		m := string(p.Method.PaymentMethod)
		out.Method = &m
	}
	return out
}

func (r *PGRepository) Create(ctx context.Context, p db.CreatePaymentParams) (Payment, error) {
	row, err := r.q(ctx).CreatePayment(ctx, p)
	return toPayment(row), database.MapError(err)
}

func (r *PGRepository) ByIdempotencyKey(ctx context.Context, key string) (Payment, error) {
	row, err := r.q(ctx).GetPaymentByIdempotencyKey(ctx, key)
	return toPayment(row), database.MapError(err)
}

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (Payment, error) {
	row, err := r.q(ctx).GetPayment(ctx, id)
	return toPayment(row), database.MapError(err)
}

func (r *PGRepository) ByOrderID(ctx context.Context, orderID string) (Payment, error) {
	row, err := r.q(ctx).GetPaymentByOrderID(ctx, &orderID)
	return toPayment(row), database.MapError(err)
}

func (r *PGRepository) ForBooking(ctx context.Context, bookingID uuid.UUID) ([]Payment, error) {
	rows, err := r.q(ctx).ListBookingPayments(ctx, bookingID)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Payment, 0, len(rows))
	for _, p := range rows {
		out = append(out, toPayment(p))
	}
	return out, nil
}

func (r *PGRepository) Captured(ctx context.Context, bookingID uuid.UUID) (int64, error) {
	n, err := r.q(ctx).SumCapturedPayments(ctx, bookingID)
	return n, database.MapError(err)
}

// Capture marks a payment captured; changed=false when it already was.
func (r *PGRepository) Capture(ctx context.Context, id uuid.UUID, gatewayPaymentID, method string) (Payment, bool, error) {
	params := db.CapturePaymentParams{ID: id, GatewayPaymentID: &gatewayPaymentID}
	if m := db.PaymentMethod(method); m.Valid() && method != "offline_cash" && method != "offline_bank_transfer" {
		params.Method = db.NullPaymentMethod{PaymentMethod: m, Valid: true}
	}
	row, err := r.q(ctx).CapturePayment(ctx, params)
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			p, gerr := r.Get(ctx, id)
			return p, false, gerr
		}
		return Payment{}, false, err
	}
	return toPayment(row), true, nil
}

func (r *PGRepository) Fail(ctx context.Context, id uuid.UUID, code, reason string) error {
	_, err := r.q(ctx).FailPayment(ctx, db.FailPaymentParams{ID: id, FailureCode: &code, FailureReason: &reason})
	if errors.Is(database.MapError(err), apperror.ErrNotFound) {
		return nil // already final
	}
	return database.MapError(err)
}

// RecordWebhook claims a delivery for processing. isNew=false when it is already
// processed or being processed (a failed or stale one is claimed again).
func (r *PGRepository) RecordWebhook(ctx context.Context, p db.ClaimWebhookEventParams) (uuid.UUID, bool, error) {
	id, err := r.q(ctx).ClaimWebhookEvent(ctx, p)
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, err
	}
	return id, true, nil
}

// MarkWebhook records the outcome: processed when errMsg is nil, else failed (retryable).
func (r *PGRepository) MarkWebhook(ctx context.Context, id uuid.UUID, errMsg *string) error {
	if errMsg == nil {
		return database.MapError(r.q(ctx).MarkWebhookProcessed(ctx, id))
	}
	return database.MapError(r.q(ctx).MarkWebhookFailed(ctx, db.MarkWebhookFailedParams{ID: id, Error: errMsg}))
}

func (r *PGRepository) RetryableWebhooks(ctx context.Context, max, batch int32) ([]db.ListRetryableWebhooksRow, error) {
	rows, err := r.q(ctx).ListRetryableWebhooks(ctx, db.ListRetryableWebhooksParams{MaxAttempts: max, Batch: batch})
	return rows, database.MapError(err)
}

func (r *PGRepository) ClaimWebhook(ctx context.Context, id uuid.UUID) ([]byte, bool, error) {
	body, err := r.q(ctx).ClaimWebhookByID(ctx, id)
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return body, true, nil
}

// OpenFor returns the unpaid (created/authorized) payment for a booking and purpose.
func (r *PGRepository) OpenFor(ctx context.Context, bookingID uuid.UUID, purpose string) (Payment, bool, error) {
	row, err := r.q(ctx).GetOpenBookingPayment(ctx, db.GetOpenBookingPaymentParams{BookingID: bookingID, Purpose: db.PaymentPurpose(purpose)})
	if err != nil {
		err = database.MapError(err)
		if errors.Is(err, apperror.ErrNotFound) {
			return Payment{}, false, nil
		}
		return Payment{}, false, err
	}
	return toPayment(row), true, nil
}

func (r *PGRepository) OtherCaptured(ctx context.Context, bookingID uuid.UUID, purpose string, exclude uuid.UUID) (int, error) {
	n, err := r.q(ctx).CountOtherCapturedPayments(ctx, db.CountOtherCapturedPaymentsParams{
		BookingID: bookingID, Purpose: db.PaymentPurpose(purpose), ExcludeID: exclude})
	return int(n), database.MapError(err)
}

// Lock serialises concurrent work on one key until the transaction ends.
func (r *PGRepository) Lock(ctx context.Context, key string) error {
	return database.MapError(r.q(ctx).LockPaymentKey(ctx, key))
}

func (r *PGRepository) Emit(ctx context.Context, id uuid.UUID, eventType string, p events.Payload) error {
	return database.MapError(events.Emit(ctx, r.q(ctx), "payment", id, eventType, p))
}

// ---- service -----------------------------------------------------------------------

// Service implements payment use cases.
type Service struct {
	repo     Repository
	bookings *bookings.Service
	gateway  Gateway
	tx       database.Transactor
	log      *slog.Logger
	refunds  func(ctx context.Context) refundQueries
}

func NewService(repo Repository, bk *bookings.Service, gw Gateway, tx database.Transactor, log *slog.Logger) *Service {
	s := &Service{repo: repo, bookings: bk, gateway: gw, tx: tx, log: log}
	if pg, ok := repo.(*PGRepository); ok {
		s.refunds = func(ctx context.Context) refundQueries { return pg.q(ctx) }
	}
	return s
}

// amountFor works out what a purpose costs from the booking's price.
func amountFor(b bookings.Booking, purpose string, captured int64) (int64, error) {
	var price bookings.Price
	if err := json.Unmarshal(b.Price, &price); err != nil {
		return 0, err
	}
	switch purpose {
	case "advance":
		if !bookings.CanPayAdvance(b.Status) {
			return 0, apperror.ErrInvalidTransition.WithDetail("the advance is paid after the owner accepts and before the booking is confirmed")
		}
		due := price.DueOnConfirmationPaise
		if due == 0 {
			due = price.AdvanceDuePaise + price.DepositPaise
		}
		return due, nil
	case "deposit":
		return price.DepositPaise, nil
	case "balance":
		switch b.Status {
		case bookings.StatusReturned, bookings.StatusCompleted, bookings.StatusClosed:
		default:
			return 0, apperror.ErrInvalidTransition.WithDetail("the final balance is due after the machine is returned and work is complete")
		}
		if b.FinalizedAt == nil || b.FinalTotal == nil {
			return 0, apperror.ErrInvalidTransition.WithDetail("the owner must settle the final bill before the balance can be paid")
		}
		total := b.EstimatedTotal
		if b.FinalTotal != nil {
			total = *b.FinalTotal
		}
		return total + price.DepositPaise - captured, nil
	}
	return 0, apperror.ErrValidation
}

// Create starts a payment for a booking (customer side). The idempotency key
// makes retries return the same order instead of charging twice.
func (s *Service) Create(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID, key string, req CreateRequest) (Checkout, error) {
	if existing, err := s.repo.ByIdempotencyKey(ctx, key); err == nil {
		return s.checkout(existing), nil
	}
	b, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Checkout{}, err
	}
	if !side.Has(bookings.SideCustomer) {
		return Checkout{}, apperror.ErrForbidden.WithDetail("only the customer pays for a booking")
	}
	if b.PaymentMode != "online" {
		return Checkout{}, apperror.ErrBusinessRule.WithDetail("this booking is paid offline")
	}
	// One unpaid order per advance/deposit: a retry with a new key reuses it.
	once := req.Purpose == "advance" || req.Purpose == "deposit"
	if once {
		if open, ok, err := s.repo.OpenFor(ctx, b.ID, req.Purpose); err != nil {
			return Checkout{}, err
		} else if ok {
			return s.checkout(open), nil
		}
		if n, err := s.repo.OtherCaptured(ctx, b.ID, req.Purpose, uuid.Nil); err != nil {
			return Checkout{}, err
		} else if n > 0 {
			return Checkout{}, apperror.ErrBusinessRule.WithDetail("the " + req.Purpose + " is already paid")
		}
		// The advance already includes the deposit, so a captured advance covers it.
		if req.Purpose == "deposit" {
			if n, err := s.repo.OtherCaptured(ctx, b.ID, "advance", uuid.Nil); err != nil {
				return Checkout{}, err
			} else if n > 0 {
				return Checkout{}, apperror.ErrBusinessRule.WithDetail("the deposit is already paid with the advance")
			}
		}
	}
	captured, err := s.repo.Captured(ctx, b.ID)
	if err != nil {
		return Checkout{}, err
	}
	amount, err := amountFor(b, req.Purpose, captured)
	if err != nil {
		return Checkout{}, err
	}
	if amount <= 0 {
		return Checkout{}, apperror.ErrBusinessRule.WithDetail("nothing is due for " + req.Purpose)
	}
	order, err := s.gateway.CreateOrder(ctx, amount, b.Reference+"-"+req.Purpose, map[string]string{
		"booking_id": b.ID.String(), "purpose": req.Purpose,
	})
	if err != nil {
		return Checkout{}, apperror.ErrServiceUnavailable.WithDetail("the payment gateway is unavailable; try again").Wrap(err)
	}
	// The payment row and the booking's move to payment_processing commit
	// together; a deadline that passes meanwhile still expires the booking.
	var pay Payment
	raced := false
	err = s.tx.InTxAs(ctx, database.Actor{UserID: p.UserID, Role: "customer", Reason: "checkout started"}, func(ctx context.Context) error {
		if err := s.repo.Lock(ctx, "pay:"+b.ID.String()+":"+req.Purpose); err != nil {
			return err
		}
		if once { // lost a race with a concurrent request: reuse its order
			if open, ok, err := s.repo.OpenFor(ctx, b.ID, req.Purpose); err != nil {
				return err
			} else if ok {
				pay, raced = open, true
				return nil
			}
		}
		var err error
		pay, err = s.repo.Create(ctx, db.CreatePaymentParams{
			BookingID: b.ID, PayerOrgID: b.CustomerOrgID, Purpose: db.PaymentPurpose(req.Purpose), AmountPaise: amount,
			Gateway: s.gateway.Name(), GatewayOrderID: &order.ID, IdempotencyKey: key,
		})
		if err != nil || req.Purpose != "advance" {
			return err
		}
		return s.bookings.PaymentStarted(ctx, b.ID)
	})
	if err != nil {
		return Checkout{}, err
	}
	if raced {
		return s.checkout(pay), nil
	}
	out := Checkout{Payment: pay, Checkout: order.Checkout}
	if sb, ok := s.gateway.(*Razorpay); ok && sb.Sandbox() {
		pid, sig := sb.SandboxPayment(order.ID)
		out.Sandbox = &VerifyRequest{GatewayPaymentID: pid, Signature: sig}
	}
	return out, nil
}

func (s *Service) checkout(p Payment) Checkout {
	out := Checkout{Payment: p, Checkout: map[string]any{"amount": p.AmountPaise, "currency": p.Currency}}
	if p.GatewayOrderID != nil {
		out.Checkout["order_id"] = *p.GatewayOrderID
	}
	return out
}

// List returns a booking's payments (parties).
func (s *Service) List(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (PaymentList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return PaymentList{}, err
	}
	list, err := s.repo.ForBooking(ctx, bookingID)
	return PaymentList{Data: list}, err
}

// Verify handles the checkout callback: a valid signature captures the
// payment (the webhook later finds it already captured).
func (s *Service) Verify(ctx context.Context, p *httpx.Principal, paymentID uuid.UUID, req VerifyRequest) (Payment, error) {
	pay, err := s.repo.Get(ctx, paymentID)
	if err != nil {
		return Payment{}, err
	}
	if _, side, err := s.bookings.Access(ctx, p, pay.BookingID); err != nil {
		return Payment{}, err
	} else if !side.Has(bookings.SideCustomer) {
		return Payment{}, apperror.ErrNotFound
	}
	if pay.GatewayOrderID == nil || !s.gateway.VerifyPayment(*pay.GatewayOrderID, req.GatewayPaymentID, req.Signature) {
		return Payment{}, apperror.New(400, "PAYMENT_SIGNATURE_INVALID", "The payment could not be verified")
	}
	// The signature only proves the browser callback is genuine. Live payments are confirmed with
	// the gateway itself; money counts only when the gateway says captured and order and amount
	// match. Otherwise the payment stays as it is and the verified webhook finishes it.
	if !s.gateway.Sandbox() && !strings.HasPrefix(req.GatewayPaymentID, "pay_sbx_") {
		gp, err := s.gateway.FetchPayment(ctx, req.GatewayPaymentID)
		if err != nil {
			s.log.WarnContext(ctx, "could not confirm a payment with the gateway", "payment_id", pay.ID, "error", err)
			return pay, nil // the webhook will confirm it
		}
		if gp.OrderID != *pay.GatewayOrderID || gp.AmountPaise != pay.AmountPaise {
			s.log.ErrorContext(ctx, "ALERT: gateway payment does not match our order", "payment_id", pay.ID, "gateway_payment_id", gp.ID)
			return Payment{}, apperror.New(400, "PAYMENT_MISMATCH", "The payment does not match this order")
		}
		if gp.Status != "captured" {
			return pay, nil
		}
	}
	return s.capture(ctx, pay, req.GatewayPaymentID, "")
}

// capture marks the payment captured and, for an advance, confirms the
// booking — in one transaction, attributed to the system.
func (s *Service) capture(ctx context.Context, pay Payment, gatewayPaymentID, method string) (Payment, error) {
	var out Payment
	err := s.tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payment captured"}, func(ctx context.Context) error {
		if err := s.repo.Lock(ctx, "pay:"+pay.BookingID.String()+":"+pay.Purpose); err != nil {
			return err
		}
		captured, changed, err := s.repo.Capture(ctx, pay.ID, gatewayPaymentID, method)
		if err != nil {
			return err
		}
		out = captured
		if !changed {
			switch captured.Status {
			case "captured", "partially_refunded", "refunded":
				if captured.GatewayPaymentID != nil && *captured.GatewayPaymentID != gatewayPaymentID {
					s.log.ErrorContext(ctx, "ALERT: second gateway payment captured for an already paid order; refund it in the gateway",
						"payment_id", pay.ID, "gateway_payment_id", gatewayPaymentID)
				}
			default:
				s.log.ErrorContext(ctx, "ALERT: captured payment updated no payment record",
					"payment_id", pay.ID, "status", captured.Status, "gateway_payment_id", gatewayPaymentID)
				return errors.New("captured payment could not be applied to payment " + pay.ID.String() + " in status " + captured.Status)
			}
			return nil
		}
		if pay.Purpose == "advance" || pay.Purpose == "deposit" {
			if n, err := s.repo.OtherCaptured(ctx, pay.BookingID, pay.Purpose, pay.ID); err != nil {
				return err
			} else if n > 0 {
				return s.refundDuplicate(ctx, captured)
			}
		}
		if err := s.recordCapture(ctx, captured); err != nil {
			return err
		}
		pl := events.Payload{}
		if b, err := s.bookings.Load(ctx, pay.BookingID); err == nil {
			pl = bookings.EventPayload(b, uuid.Nil)
		} else {
			return err
		}
		pl.Extra = map[string]any{"booking_id": pay.BookingID, "amount_paise": pay.AmountPaise, "purpose": pay.Purpose}
		if err := s.repo.Emit(ctx, pay.ID, events.PaymentCaptured, pl); err != nil {
			return err
		}
		if pay.Purpose == "advance" {
			return s.bookings.ConfirmPaid(ctx, pay.BookingID)
		}
		return nil
	})
	return out, err
}

// refundDuplicate queues a full refund of a captured payment that duplicates an
// earlier captured one for the same booking and purpose (the worker sends it).
func (s *Service) refundDuplicate(ctx context.Context, pay Payment) error {
	s.log.ErrorContext(ctx, "ALERT: duplicate captured payment; refunding", "payment_id", pay.ID,
		"booking_id", pay.BookingID, "purpose", pay.Purpose, "amount_paise", pay.AmountPaise)
	if s.refunds == nil {
		return errors.New("duplicate payment " + pay.ID.String() + " cannot be refunded: refunds are not configured")
	}
	_, err := s.refunds(ctx).CreateRefund(ctx, db.CreateRefundParams{
		PaymentID: pay.ID, BookingID: pay.BookingID, AmountPaise: pay.AmountPaise,
		Reason: "Duplicate " + pay.Purpose + " payment", IdempotencyKey: "duplicate:" + pay.ID.String(),
	})
	if err != nil && !errors.Is(database.MapError(err), apperror.ErrNotFound) {
		return database.MapError(err)
	}
	return nil
}

// razorpayEvent is the subset of a Razorpay webhook we use.
type razorpayEvent struct {
	Event   string `json:"event"`
	Payload struct {
		Payment struct {
			Entity struct {
				ID          string `json:"id"`
				OrderID     string `json:"order_id"`
				Amount      int64  `json:"amount"`
				Method      string `json:"method"`
				ErrorCode   string `json:"error_code"`
				Description string `json:"error_description"`
			} `json:"entity"`
		} `json:"payment"`
		Refund struct {
			Entity struct {
				ID        string `json:"id"`
				PaymentID string `json:"payment_id"`
			} `json:"entity"`
		} `json:"refund"`
	} `json:"payload"`
}

// ErrBadSignature rejects unsigned / forged webhooks.
var ErrBadSignature = apperror.New(401, "WEBHOOK_SIGNATURE_INVALID", "Invalid webhook signature")

// Webhook processes a gateway event exactly once.
func (s *Service) Webhook(ctx context.Context, provider string, body []byte, signature, eventID string) (WebhookAck, error) {
	if provider != s.gateway.Name() {
		return WebhookAck{}, apperror.ErrNotFound
	}
	if !s.gateway.VerifyWebhook(body, signature) {
		s.log.WarnContext(ctx, "payment webhook with invalid signature rejected", "provider", provider)
		return WebhookAck{}, ErrBadSignature
	}
	var ev razorpayEvent
	if err := json.Unmarshal(body, &ev); err != nil || ev.Event == "" {
		return WebhookAck{}, apperror.ErrBadRequest.WithDetail("unrecognised webhook body")
	}
	if eventID == "" {
		sum := sha256.Sum256(body)
		eventID = "sha256:" + hex.EncodeToString(sum[:])
	}
	id, isNew, err := s.repo.RecordWebhook(ctx, db.ClaimWebhookEventParams{
		Provider: provider, EventID: eventID, EventType: ev.Event, Payload: body, SignatureValid: true,
	})
	if err != nil {
		return WebhookAck{}, err
	}
	if !isNew {
		return WebhookAck{Status: "duplicate"}, nil
	}
	return s.finish(ctx, id, ev)
}

// finish applies a claimed event and records the outcome. The event is marked
// processed only after the database changes committed; any failure marks it
// failed (kept for retry) and is reported so the gateway redelivers.
func (s *Service) finish(ctx context.Context, id uuid.UUID, ev razorpayEvent) (WebhookAck, error) {
	status, procErr := s.apply(ctx, ev)
	if procErr != nil {
		m := procErr.Error()
		s.log.ErrorContext(ctx, "payment webhook failed; will be retried", "webhook_id", id, "event", ev.Event, "error", procErr)
		if err := s.repo.MarkWebhook(ctx, id, &m); err != nil {
			return WebhookAck{}, err
		}
		return WebhookAck{}, apperror.ErrServiceUnavailable.WithDetail("webhook processing failed; retry later").Wrap(procErr)
	}
	if err := s.repo.MarkWebhook(ctx, id, nil); err != nil {
		return WebhookAck{}, err
	}
	return WebhookAck{Status: status}, nil
}

// MaxWebhookAttempts bounds automatic retries of a failed webhook event.
const MaxWebhookAttempts = 10

// ReconcileWebhooks retries failed (or abandoned) webhook events from their
// stored payload. Each event commits or fails on its own transaction.
func (s *Service) ReconcileWebhooks(ctx context.Context) error {
	rows, err := s.repo.RetryableWebhooks(ctx, MaxWebhookAttempts, 20)
	if err != nil {
		return err
	}
	for _, r := range rows {
		ectx := database.WithoutTx(ctx)
		body, ok, err := s.repo.ClaimWebhook(ectx, r.ID)
		if err != nil || !ok {
			continue
		}
		var ev razorpayEvent
		if err := json.Unmarshal(body, &ev); err != nil {
			m := "stored payload is unreadable: " + err.Error()
			if err := s.repo.MarkWebhook(ectx, r.ID, &m); err != nil {
				s.log.ErrorContext(ctx, "could not mark unreadable webhook", "webhook_id", r.ID, "error", err)
			}
			continue
		}
		if _, err := s.finish(ectx, r.ID, ev); err != nil && r.Attempts+1 >= MaxWebhookAttempts {
			s.log.ErrorContext(ctx, "ALERT: payment webhook exhausted its retries; needs manual reconciliation", "webhook_id", r.ID)
		}
	}
	return nil
}

func (s *Service) apply(ctx context.Context, ev razorpayEvent) (string, error) {
	ent := ev.Payload.Payment.Entity
	switch ev.Event {
	case "payment.captured", "order.paid":
		pay, err := s.repo.ByOrderID(ctx, ent.OrderID)
		if errors.Is(err, apperror.ErrNotFound) {
			s.log.ErrorContext(ctx, "ALERT: captured payment matched no payment record (another environment, or an order we lost)",
				"gateway_order_id", ent.OrderID, "gateway_payment_id", ent.ID, "amount_paise", ent.Amount)
			return "ignored", nil
		}
		if err != nil {
			return "", err
		}
		if ent.Amount != pay.AmountPaise {
			return "", errors.New("captured amount does not match the payment; needs reconciliation")
		}
		_, err = s.capture(ctx, pay, ent.ID, ent.Method)
		return "processed", err
	case "payment.failed":
		pay, err := s.repo.ByOrderID(ctx, ent.OrderID)
		if errors.Is(err, apperror.ErrNotFound) {
			return "ignored", nil
		}
		if err != nil {
			return "", err
		}
		reason := ent.Description
		if reason == "" {
			reason = "payment failed"
		}
		return "processed", s.Failed(ctx, pay, ent.ErrorCode, reason)
	case "refund.processed", "refund.failed":
		ref := ev.Payload.Refund.Entity
		if ref.ID == "" || s.refunds == nil {
			return "ignored", nil
		}
		return s.refundOutcome(ctx, ref.ID, ev.Event == "refund.processed", "the gateway could not refund this payment")
	}
	return "ignored", nil
}

// Failed records a failed payment and returns the booking to awaiting
// payment, notifying the customer.
func (s *Service) Failed(ctx context.Context, pay Payment, code, reason string) error {
	return s.tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "payment failed"}, func(ctx context.Context) error {
		if err := s.repo.Fail(ctx, pay.ID, code, reason); err != nil {
			return err
		}
		if pay.Purpose != "advance" {
			return nil
		}
		if err := s.bookings.PaymentFailed(ctx, pay.BookingID); err != nil {
			return err
		}
		b, err := s.bookings.Load(ctx, pay.BookingID)
		if err != nil {
			return err
		}
		pl := bookings.EventPayload(b, uuid.Nil)
		pl.Extra = map[string]any{"booking_id": pay.BookingID, "amount_paise": pay.AmountPaise, "reason": reason}
		return s.repo.Emit(ctx, pay.ID, events.PaymentFailed, pl)
	})
}

// refundOutcome settles a refund the gateway answered asynchronously.
func (s *Service) refundOutcome(ctx context.Context, gatewayRefundID string, processed bool, failure string) (string, error) {
	err := s.tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "refund webhook"}, func(ctx context.Context) error {
		q := s.refunds(ctx)
		r, err := q.GetRefundByGatewayID(ctx, &gatewayRefundID)
		if err != nil {
			if errors.Is(database.MapError(err), apperror.ErrNotFound) {
				return nil
			}
			return database.MapError(err)
		}
		if r.Status != "pending" {
			return nil
		}
		if !processed {
			_, err := q.MarkRefundFailed(ctx, db.MarkRefundFailedParams{ID: r.ID, FailureReason: &failure})
			return database.MapError(err)
		}
		return s.refundDone(ctx, q, r.ID, r.BookingID, &gatewayRefundID)
	})
	return "processed", err
}

// refundDone marks a refund processed and tells the customer.
func (s *Service) refundDone(ctx context.Context, q refundQueries, id, bookingID uuid.UUID, gatewayID *string) error {
	if _, err := q.MarkRefundProcessed(ctx, db.MarkRefundProcessedParams{ID: id, GatewayRefundID: gatewayID}); err != nil {
		return database.MapError(err)
	}
	// Confirmed by the gateway: record it in the ledger (once) and tell the customer.
	info, err := q.GetRefundLedgerInfo(ctx, id)
	if err != nil {
		return database.MapError(err)
	}
	account := ledger.CustomerFunds
	if info.IsDeposit {
		account = ledger.CustomerDeposits
	}
	if _, err := ledger.Post(ctx, q, ledger.Posting{Kind: ledger.KindRefund, BookingID: bookingID, ReferenceType: "refund", ReferenceID: id,
		Memo: "Refund confirmed by the gateway", Legs: []ledger.Leg{ledger.Dr(account, info.AmountPaise), ledger.Cr(ledger.GatewayClearing, info.AmountPaise)}}); err != nil {
		return err
	}
	b, err := s.bookings.Load(ctx, bookingID)
	if err != nil {
		return err
	}
	return s.refundEvent(ctx, q, id, b)
}

func (s *Service) refundEvent(ctx context.Context, q refundQueries, refundID uuid.UUID, b bookings.Booking) error {
	pl := bookings.EventPayload(b, uuid.Nil)
	var amount int64
	if m, err := s.bookingsRefund(ctx, b.ID, refundID); err == nil {
		amount = m
	}
	pl.Extra = map[string]any{"booking_id": b.ID, "refund_id": refundID, "amount_paise": amount}
	return events.Emit(ctx, q, "refund", refundID, events.RefundProcessed, pl)
}

// bookingsRefund finds a refund's amount in the booking's refund list.
func (s *Service) bookingsRefund(ctx context.Context, bookingID, refundID uuid.UUID) (int64, error) {
	list, err := s.bookings.RefundList(ctx, bookingID)
	if err != nil {
		return 0, err
	}
	for _, r := range list {
		if r.ID == refundID {
			return r.AmountPaise, nil
		}
	}
	return 0, apperror.ErrNotFound
}

// MaxRefundAttempts is how often a refund the gateway rejected is retried
// before it is marked failed for finance. An uncertain outcome is never marked
// failed: it is looked up at the gateway again and again.
const MaxRefundAttempts = 10

func refundBackoff(attempts int32) float64 {
	d := 60.0
	for i := int32(1); i < attempts && d < 6*3600; i++ {
		d *= 2
	}
	return min(d, 6*3600)
}

// ProcessRefunds sends queued refunds to the gateway and closes bookings whose
// refunds are all settled (refund_pending → refunded). Every database step is its
// own short transaction: a refund is claimed (marked submitting) and committed
// BEFORE the gateway is called, and the answer is recorded afterwards, so no
// transaction stays open across a network call and a crash is recoverable.
func (s *Service) ProcessRefunds(ctx context.Context) error {
	if s.refunds == nil {
		return nil
	}
	dctx := database.WithoutTx(ctx)
	rows, err := s.refunds(dctx).ClaimRefundsForSubmit(dctx, db.ClaimRefundsForSubmitParams{MaxAttempts: MaxRefundAttempts, BatchSize: 20})
	if err != nil {
		return err
	}
	for _, r := range rows {
		if err := s.submitRefund(dctx, r); err != nil {
			s.log.ErrorContext(ctx, "refund submission could not be recorded", "refund_id", r.ID, "error", err)
		}
	}
	if err := s.reconcileSubmittedRefunds(dctx); err != nil {
		return err
	}
	q := s.refunds(ctx)
	done, err := q.FinishRefundedBookings(ctx, 100)
	if err != nil {
		return err
	}
	for _, b := range done {
		if err := events.Emit(ctx, q, "booking", b.ID, events.BookingRefunded, events.Payload{
			Reference: b.Reference, CustomerOrgID: b.CustomerOrgID, OwnerOrgID: b.OwnerOrgID,
		}); err != nil {
			return err
		}
	}
	return nil
}

// submitRefund performs one claimed refund. The idempotency reference is the
// refund's stable key, so a repeat can never refund twice.
func (s *Service) submitRefund(ctx context.Context, r db.ClaimRefundsForSubmitRow) error {
	q := s.refunds(ctx)
	retry := func(msg string, uncertain bool) error {
		if !uncertain && int(r.Attempts) >= MaxRefundAttempts {
			s.log.ErrorContext(ctx, "ALERT: refund rejected by the gateway too many times; finance must handle it", "refund_id", r.ID, "reason", msg)
			_, err := q.MarkRefundFailed(ctx, db.MarkRefundFailedParams{ID: r.ID, FailureReason: &msg})
			return err
		}
		if uncertain && int(r.Attempts) >= MaxRefundAttempts {
			s.log.ErrorContext(ctx, "ALERT: refund outcome still unknown after many attempts", "refund_id", r.ID, "reason", msg)
		}
		return q.MarkRefundRetry(ctx, db.MarkRefundRetryParams{ID: r.ID, Uncertain: uncertain, FailureReason: &msg, DelaySeconds: refundBackoff(r.Attempts)})
	}
	if r.GatewayPaymentID == nil {
		msg := "the payment has no gateway reference"
		_, err := q.MarkRefundFailed(ctx, db.MarkRefundFailedParams{ID: r.ID, FailureReason: &msg})
		return err
	}
	var res RefundResult
	found := false
	if r.Attempts > 1 { // an earlier attempt may have reached the gateway
		var err error
		if res, found, err = s.gateway.FindRefund(ctx, *r.GatewayPaymentID, r.IdempotencyKey); err != nil {
			return retry("could not check the gateway for an earlier attempt: "+err.Error(), true)
		}
	}
	if !found {
		var err error
		if res, err = s.gateway.Refund(ctx, *r.GatewayPaymentID, r.AmountPaise, r.IdempotencyKey); err != nil {
			var re *RefundError
			definite := errors.As(err, &re) && re.Definite
			s.log.WarnContext(ctx, "refund attempt failed", "refund_id", r.ID, "definite", definite, "error", err)
			return retry(err.Error(), !definite)
		}
	}
	return s.settleRefund(ctx, r.ID, r.BookingID, res)
}

// settleRefund records what the gateway said. A refund is processed only when the
// gateway says so; accepted-but-pending stays pending until a webhook or the
// reconciliation poll confirms it.
func (s *Service) settleRefund(ctx context.Context, id, bookingID uuid.UUID, res RefundResult) error {
	q := s.refunds(ctx)
	if res.Failed {
		msg := "the gateway reports the refund failed"
		s.log.ErrorContext(ctx, "ALERT: gateway refund failed", "refund_id", id, "gateway_refund_id", res.ID)
		if _, err := q.MarkRefundFailed(ctx, db.MarkRefundFailedParams{ID: id, FailureReason: &msg}); err != nil && !errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return err
		}
		return nil
	}
	if err := q.MarkRefundSubmitted(ctx, db.MarkRefundSubmittedParams{ID: id, GatewayRefundID: &res.ID}); err != nil {
		return err
	}
	if !res.Processed {
		return nil
	}
	err := s.tx.InTxAs(ctx, database.Actor{Role: "system", Reason: "refund processed"}, func(ctx context.Context) error {
		return s.refundDone(ctx, s.refunds(ctx), id, bookingID, &res.ID)
	})
	if errors.Is(err, apperror.ErrNotFound) {
		return nil // a webhook settled it first
	}
	return err
}

// reconcileSubmittedRefunds polls the gateway for refunds it accepted but never confirmed.
func (s *Service) reconcileSubmittedRefunds(ctx context.Context) error {
	q := s.refunds(ctx)
	rows, err := q.SubmittedRefundsToCheck(ctx, 20)
	if err != nil {
		return err
	}
	for _, r := range rows {
		if r.GatewayRefundID == nil {
			continue
		}
		res, err := s.gateway.RefundStatus(ctx, *r.GatewayRefundID)
		if err != nil {
			s.log.WarnContext(ctx, "refund status check failed", "refund_id", r.ID, "error", err)
			_ = q.TouchRefundCheck(ctx, r.ID)
			continue
		}
		if !res.Processed && !res.Failed {
			_ = q.TouchRefundCheck(ctx, r.ID)
			continue
		}
		if err := s.settleRefund(ctx, r.ID, r.BookingID, RefundResult{ID: *r.GatewayRefundID, Processed: res.Processed, Failed: res.Failed}); err != nil {
			s.log.ErrorContext(ctx, "refund reconciliation failed", "refund_id", r.ID, "error", err)
		}
	}
	return nil
}

// ---- handler -----------------------------------------------------------------------

type Handler struct{ svc *Service }

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

func (h *Handler) Name() string { return "payments" }

func (h *Handler) Routes(r *httpx.Router) {
	r = r.Tag("payments")
	r.POST("/webhooks/payments/:provider", func(c *gin.Context) {
		body, err := c.GetRawData()
		if err != nil {
			httpx.Fail(c, httpx.BindError(err))
			return
		}
		ack, err := h.svc.Webhook(c.Request.Context(), c.Param("provider"), body,
			c.GetHeader("X-Razorpay-Signature"), c.GetHeader("X-Razorpay-Event-Id"))
		httpx.Reply(c, 200, ack, err)
	}, httpx.Doc{Summary: "Payment gateway webhook (HMAC-signed)", Response: WebhookAck{},
		Description: "Verifies X-Razorpay-Signature over the raw body, stores the event and processes it once (deduplicated by X-Razorpay-Event-Id)."})

	a := r.Authed()
	a.POST("/bookings/:id/payments", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req CreateRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		key := c.GetHeader("Idempotency-Key")
		if key == "" {
			return nil, apperror.ErrIdempotencyRequired
		}
		return h.svc.Create(c.Request.Context(), p, id, p.UserID.String()+":"+key, req)
	}), httpx.Doc{Summary: "Start a payment (advance, deposit or balance)", Request: CreateRequest{}, Response: Checkout{}, Status: 201, Idempotent: true})
	a.GET("/bookings/:id/payments", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.List(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Payments of a booking", Response: PaymentList{}})
	a.POST("/payments/:id/verify", httpx.HandleJSON(200, func(c *gin.Context, p *httpx.Principal, req VerifyRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Verify(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Verify the checkout callback signature", Request: VerifyRequest{}, Response: Payment{}})
}

// UseGateway swaps the payment gateway (tests inject a scripted one).
func (s *Service) UseGateway(g Gateway) { s.gateway = g }

// Gateway returns the configured gateway.
func (s *Service) Gateway() Gateway { return s.gateway }
