// Package ledger posts double-entry transactions to the financial ledger.
//
// Every business event (a captured payment, a processed refund, a settlement,
// a payout) is posted exactly once: ledger_transactions has a unique
// (kind, reference_type, reference_id), so a replayed webhook or a re-run job
// finds the posting already there. Rows are append-only (database triggers)
// and a deferred constraint trigger rejects any transaction whose debits and
// credits differ at COMMIT. Corrections are new 'adjustment' postings.
//
// Accounts (all INR paise):
//
//	gateway_clearing                  asset      money held by the gateway for us
//	customer_funds_held               liability  rent paid in, not yet earned/settled
//	customer_deposits                 liability  refundable security deposits
//	owner_payable:<org>               liability  owed to an owner
//	platform_revenue:commission       revenue    owner commission
//	platform_revenue:convenience_fee  revenue    customer platform fee
//	gst_output                        liability  GST on platform fee and commission
//	tcs_payable / tds_194o_payable    liability  withheld from owners
package ledger

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
)

// Account codes.
const (
	GatewayClearing   = "gateway_clearing"
	CustomerFunds     = "customer_funds_held"
	CustomerDeposits  = "customer_deposits"
	CommissionRevenue = "platform_revenue:commission"
	FeeRevenue        = "platform_revenue:convenience_fee"
	GSTOutput         = "gst_output"
	TCSPayable        = "tcs_payable"
	TDSPayable        = "tds_194o_payable"
)

// OwnerPayable is an owner's payable account code.
func OwnerPayable(org uuid.UUID) string { return "owner_payable:" + org.String() }

// Transaction kinds (ledger_transactions.kind).
const (
	KindPaymentCaptured = "payment_captured"
	KindSettlement      = "commission"
	KindRefund          = "refund"
	KindPayout          = "payout"
	KindDepositRelease  = "deposit_release"
)

// Leg is one side of a posting. Zero amounts are skipped.
type Leg struct {
	Account string
	OrgID   uuid.UUID // set for per-owner accounts
	Debit   bool
	Paise   int64
}

// Dr and Cr build legs.
func Dr(account string, paise int64) Leg { return Leg{Account: account, Debit: true, Paise: paise} }
func Cr(account string, paise int64) Leg { return Leg{Account: account, Paise: paise} }

// CrOwner credits an owner's payable account.
func CrOwner(org uuid.UUID, paise int64) Leg {
	return Leg{Account: OwnerPayable(org), OrgID: org, Paise: paise}
}

// DrOwner debits an owner's payable account.
func DrOwner(org uuid.UUID, paise int64) Leg {
	return Leg{Account: OwnerPayable(org), OrgID: org, Debit: true, Paise: paise}
}

// Posting is one balanced business event.
type Posting struct {
	Kind          string
	BookingID     uuid.UUID
	ReferenceType string
	ReferenceID   uuid.UUID
	Memo          string
	CreatedBy     uuid.UUID
	Legs          []Leg
}

// ErrUnbalanced is a programming error: the legs do not balance.
var ErrUnbalanced = errors.New("ledger posting is unbalanced")

// Queries is what posting needs (generated queries in a transaction).
type Queries interface {
	EnsureLedgerAccount(ctx context.Context, arg db.EnsureLedgerAccountParams) (uuid.UUID, error)
	InsertLedgerTransaction(ctx context.Context, arg db.InsertLedgerTransactionParams) (uuid.UUID, error)
	InsertLedgerEntry(ctx context.Context, arg db.InsertLedgerEntryParams) error
}

// Check validates a posting without writing it.
func Check(p Posting) error {
	var d, c int64
	legs := 0
	for _, l := range p.Legs {
		if l.Paise < 0 {
			return fmt.Errorf("%w: negative leg %s", ErrUnbalanced, l.Account)
		}
		if l.Paise == 0 {
			continue
		}
		legs++
		if l.Debit {
			d += l.Paise
		} else {
			c += l.Paise
		}
	}
	if d != c || legs < 2 {
		return fmt.Errorf("%w: %s debit %d credit %d", ErrUnbalanced, p.Kind, d, c)
	}
	return nil
}

// Post writes the posting once. posted=false when the same event was already
// posted. It must run inside a database transaction (the balance check is a
// deferred constraint).
func Post(ctx context.Context, q Queries, p Posting) (posted bool, err error) {
	if err := Check(p); err != nil {
		return false, err
	}
	params := db.InsertLedgerTransactionParams{Kind: p.Kind, ReferenceType: &p.ReferenceType,
		ReferenceID: uuid.NullUUID{UUID: p.ReferenceID, Valid: true}, Memo: &p.Memo}
	if p.BookingID != uuid.Nil {
		params.BookingID = uuid.NullUUID{UUID: p.BookingID, Valid: true}
	}
	if p.CreatedBy != uuid.Nil {
		params.CreatedBy = uuid.NullUUID{UUID: p.CreatedBy, Valid: true}
	}
	txID, err := q.InsertLedgerTransaction(ctx, params)
	if err != nil {
		if errors.Is(database.MapError(err), apperror.ErrNotFound) {
			return false, nil // already posted
		}
		return false, err
	}
	for _, l := range p.Legs {
		if l.Paise == 0 {
			continue
		}
		acc := db.EnsureLedgerAccountParams{Code: l.Account, Type: accountType(l.Account)}
		if l.OrgID != uuid.Nil {
			acc.OrgID = uuid.NullUUID{UUID: l.OrgID, Valid: true}
		}
		accID, err := q.EnsureLedgerAccount(ctx, acc)
		if err != nil {
			return false, err
		}
		dir := "C"
		if l.Debit {
			dir = "D"
		}
		if err := q.InsertLedgerEntry(ctx, db.InsertLedgerEntryParams{TransactionID: txID, AccountID: accID, Direction: dir, AmountPaise: l.Paise}); err != nil {
			return false, err
		}
	}
	return true, nil
}

func accountType(code string) string {
	switch {
	case code == GatewayClearing:
		return "asset"
	case len(code) > 16 && code[:16] == "platform_revenue":
		return "revenue"
	case code == "gateway_fees":
		return "expense"
	}
	return "liability"
}
