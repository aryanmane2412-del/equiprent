// Package invoices issues the owner's rental invoice for a booking and
// credit notes against it.
//
//   - Numbers are gap-free per supplier, series prefix and Indian financial
//     year (the series row is locked until commit): ER/2627/00001 for
//     invoices, CN/2627/00001 for credit notes.
//   - GST is configuration: each line is taxed by the rules frozen in the
//     booking's estimate (tax_rates, with SAC codes and a review record),
//     split into CGST+SGST within the state and IGST across states by the
//     place of supply (the work site's state).
//   - A supplier without a GSTIN gets a bill of supply without GST.
//   - Issued invoices are immutable (database trigger). Parties, equipment,
//     rental dates and payment references are frozen into the invoice's
//     details at issue; corrections and refunds are credit notes.
package invoices

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/modules/bookings"
)

// SACRental is the default SAC (leasing or rental of construction machinery
// with or without operator) when a tax rule names none.
const SACRental = "997313"

// Series prefixes (PREFIX/FY/NNNNN).
const (
	SeriesPrefix     = "ER"
	CreditNotePrefix = "CN"
)

var ist = time.FixedZone("IST", 5*3600+1800)

// FinancialYear returns the Indian financial year code (April–March) of t,
// e.g. 2026-11-12 → "2627".
func FinancialYear(t time.Time) string {
	t = t.In(ist)
	start := t.Year()
	if t.Month() < time.April {
		start--
	}
	return fmt.Sprintf("%02d%02d", start%100, (start+1)%100)
}

// Tax is the GST split of a taxable amount.
type Tax struct {
	SupplyType string
	CGST, SGST int64
	IGST       int64
}

// SplitGST applies rateBps to subtotal: intra-state supplies split equally
// into CGST and SGST, inter-state supplies pay IGST.
func SplitGST(subtotal int64, rateBps int, intra bool) Tax {
	if intra {
		half := (subtotal*int64(rateBps)/2 + 5000) / 10000
		return Tax{SupplyType: "intra", CGST: half, SGST: half}
	}
	return Tax{SupplyType: "inter", IGST: (subtotal*int64(rateBps) + 5000) / 10000}
}

// Line is an invoice line.
type Line struct {
	LineNo       int16   `json:"line_no"`
	Description  string  `json:"description"`
	SACCode      string  `json:"sac_code"`
	Quantity     float64 `json:"quantity"`
	Unit         string  `json:"unit"`
	RatePaise    int64   `json:"rate_paise"`
	TaxablePaise int64   `json:"taxable_paise"`
	GSTRateBps   int32   `json:"gst_rate_bps"`
	TaxCode      string  `json:"tax_code" doc:"The configured tax rule(s) applied, e.g. gst"`
	Component    string  `json:"-"`
}

// Party is a supplier or recipient as printed.
type Party struct {
	LegalName    string `json:"legal_name"`
	DisplayName  string `json:"display_name,omitempty"`
	GSTIN        string `json:"gstin,omitempty"`
	Address      string `json:"address,omitempty"`
	StateCode    string `json:"state_code,omitempty"`
	ContactName  string `json:"contact_name,omitempty"`
	ContactPhone string `json:"contact_phone,omitempty"`
}

// Equipment identifies the machine.
type Equipment struct {
	Title          string `json:"title"`
	Make           string `json:"make,omitempty"`
	Model          string `json:"model,omitempty"`
	Category       string `json:"category,omitempty"`
	RegistrationNo string `json:"registration_no,omitempty"`
	SerialNo       string `json:"serial_no,omitempty"`
	ListingID      string `json:"listing_id,omitempty"`
}

// Rental is the hire period and site.
type Rental struct {
	Start    time.Time `json:"start"`
	End      time.Time `json:"end"`
	TimeZone string    `json:"time_zone"`
	Site     string    `json:"site,omitempty"`
	WithOp   bool      `json:"with_operator"`
}

// PaymentRef is a captured payment for the booking at the time of issue.
type PaymentRef struct {
	Purpose          string     `json:"purpose"`
	AmountPaise      int64      `json:"amount_paise"`
	ReceiptNumber    string     `json:"receipt_number,omitempty"`
	GatewayPaymentID string     `json:"gateway_payment_id,omitempty"`
	Method           string     `json:"method,omitempty"`
	CapturedAt       *time.Time `json:"captured_at,omitempty"`
}

// Details is frozen into the invoice at issue.
type Details struct {
	BookingReference string       `json:"booking_reference"`
	Supplier         Party        `json:"supplier"`
	Recipient        Party        `json:"recipient"`
	Equipment        Equipment    `json:"equipment"`
	Rental           Rental       `json:"rental"`
	Basis            string       `json:"basis,omitempty" doc:"final (settled charges) or estimate"`
	Payments         []PaymentRef `json:"payments"`
	Notes            []string     `json:"notes"`
	OriginalNumber   string       `json:"original_invoice_number,omitempty"`
}

// Adjustment is a refund or credit note shown with an invoice.
type Adjustment struct {
	Kind        string     `json:"kind" doc:"refund or credit_note"`
	Reference   string     `json:"reference,omitempty"`
	AmountPaise int64      `json:"amount_paise"`
	Status      string     `json:"status"`
	Reason      string     `json:"reason,omitempty"`
	At          *time.Time `json:"at,omitempty"`
	ID          uuid.UUID  `json:"id"`
}

// Invoice is a tax invoice, bill of supply or credit note.
type Invoice struct {
	ID                uuid.UUID     `json:"id"`
	Number            string        `json:"invoice_number"`
	Title             string        `json:"title" doc:"Tax invoice, Bill of supply or Credit note"`
	Type              string        `json:"invoice_type"`
	Status            string        `json:"status"`
	IssuerOrgID       uuid.NullUUID `json:"issuer_org_id"`
	RecipientOrgID    uuid.UUID     `json:"recipient_org_id"`
	BookingID         uuid.NullUUID `json:"booking_id"`
	OriginalInvoiceID uuid.NullUUID `json:"original_invoice_id"`
	Reason            *string       `json:"reason,omitempty"`
	PlaceOfSupply     string        `json:"place_of_supply"`
	SupplyType        string        `json:"supply_type"`
	SACCode           string        `json:"sac_code"`
	SubtotalPaise     int64         `json:"subtotal_paise"`
	CGSTPaise         int64         `json:"cgst_paise"`
	SGSTPaise         int64         `json:"sgst_paise"`
	IGSTPaise         int64         `json:"igst_paise"`
	TotalPaise        int64         `json:"total_paise"`
	Currency          string        `json:"currency"`
	IssuedAt          *time.Time    `json:"issued_at,omitempty"`
	Details           *Details      `json:"details,omitempty"`
	Lines             []Line        `json:"lines,omitempty"`
	// On the detail view of a rental invoice: refunds on the booking and
	// credit notes issued against it.
	Adjustments   []Adjustment `json:"adjustments,omitempty"`
	CreditedPaise int64        `json:"credited_paise"`
	NetPaise      int64        `json:"net_paise" doc:"Total less issued credit notes"`
}

type InvoiceList struct {
	Data       []Invoice `json:"data"`
	NextCursor string    `json:"next_cursor,omitempty"`
}

// CreditNoteRequest credits part or all of an invoice's taxable value.
type CreditNoteRequest struct {
	AmountPaise int64  `json:"amount_paise" binding:"required,min=1" doc:"Taxable value credited (tax is added in proportion)"`
	Reason      string `json:"reason" binding:"required,min=5,max=300"`
}

// BuildRentalLines turns an older price snapshot (no itemised charges) into
// taxable lines at gstBps.
func BuildRentalLines(p bookings.Price, gstBps int) []Line {
	var lines []Line
	add := func(desc string, qty float64, unit string, rate, taxable int64) {
		if taxable <= 0 {
			return
		}
		lines = append(lines, Line{
			LineNo: int16(len(lines) + 1), Description: desc, SACCode: SACRental, Quantity: qty, Unit: unit,
			RatePaise: rate, TaxablePaise: taxable, GSTRateBps: int32(gstBps), TaxCode: "gst",
		})
	}
	if p.Version >= 2 {
		for _, l := range p.Lines {
			add("Equipment rental", float64(l.Count), l.Unit, l.RatePaise, l.Paise)
		}
		if p.ExtraHoursPaise > 0 {
			add("Extra hours", p.ExtraHours, "hour", p.ExtraHoursPaise/int64(max(p.ExtraHours, 1)), p.ExtraHoursPaise)
		}
		add("Delivery to site", 1, "trip", p.DeliveryPaise, p.DeliveryPaise)
		add("Pickup from site", 1, "trip", p.PickupPaise, p.PickupPaise)
		if p.OperatorBataPaise > 0 {
			add("Operator bata", float64(p.Days), "day", p.OperatorBataPaise/max(p.Days, 1), p.OperatorBataPaise)
		}
		if p.FuelPaise > 0 {
			add("Fuel", p.FuelHours, "hour", p.FuelPaise/int64(max(p.FuelHours, 1)), p.FuelPaise)
		}
		return lines
	}
	base := p.RatePaise * p.Units
	add("Equipment rental", float64(p.Units), p.Unit, p.RatePaise, base)
	if extra := p.RentalPaise - base; extra > 0 {
		add("Extra hours", p.ExtraHours, "hour", extra/int64(max(p.ExtraHours, 1)), extra)
	}
	add("Mobilisation and demobilisation", 1, "trip", p.MobilisationPaise, p.MobilisationPaise)
	if p.OperatorBataPaise > 0 {
		add("Operator bata", float64(p.Days), "day", p.OperatorBataPaise/max(p.Days, 1), p.OperatorBataPaise)
	}
	return lines
}

// ---- repository --------------------------------------------------------------------

type Repository interface {
	Context(ctx context.Context, bookingID uuid.UUID) (db.AgreementContextRow, error)
	HasInvoice(ctx context.Context, bookingID uuid.UUID, kind db.InvoiceType) (bool, error)
	Payments(ctx context.Context, bookingID uuid.UUID) ([]db.Payment, error)
	Refunds(ctx context.Context, bookingID uuid.UUID) ([]db.ListBookingRefundsRow, error)
	UnreviewedTaxRules(ctx context.Context) (int32, error)
	Issue(ctx context.Context, issuer uuid.UUID, prefix, fy string, inv db.CreateInvoiceV2Params, lines []Line) (uuid.UUID, error)
	Get(ctx context.Context, id uuid.UUID) (Invoice, error)
	CreditNotes(ctx context.Context, originalID uuid.UUID) ([]Invoice, error)
	ForBooking(ctx context.Context, bookingID uuid.UUID) ([]Invoice, error)
	ForOrg(ctx context.Context, org uuid.UUID, k httpx.Keyset) ([]Invoice, error)
	Audit(ctx context.Context, p db.InsertAuditLogParams) error
}

type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Context(ctx context.Context, bookingID uuid.UUID) (db.AgreementContextRow, error) {
	row, err := r.q(ctx).AgreementContext(ctx, bookingID)
	return row, database.MapError(err)
}

func (r *PGRepository) HasInvoice(ctx context.Context, bookingID uuid.UUID, kind db.InvoiceType) (bool, error) {
	ok, err := r.q(ctx).HasIssuedInvoice(ctx, db.HasIssuedInvoiceParams{BookingID: uuid.NullUUID{UUID: bookingID, Valid: true}, InvoiceType: kind})
	return ok, database.MapError(err)
}

func (r *PGRepository) Payments(ctx context.Context, bookingID uuid.UUID) ([]db.Payment, error) {
	rows, err := r.q(ctx).ListBookingPayments(ctx, bookingID)
	return rows, database.MapError(err)
}

func (r *PGRepository) Refunds(ctx context.Context, bookingID uuid.UUID) ([]db.ListBookingRefundsRow, error) {
	rows, err := r.q(ctx).ListBookingRefunds(ctx, bookingID)
	return rows, database.MapError(err)
}

func (r *PGRepository) UnreviewedTaxRules(ctx context.Context) (int32, error) {
	n, err := r.q(ctx).CountUnreviewedTaxRates(ctx)
	return n, database.MapError(err)
}

func (r *PGRepository) Audit(ctx context.Context, p db.InsertAuditLogParams) error {
	return database.MapError(r.q(ctx).InsertAuditLog(ctx, p))
}

// Issue numbers, inserts and issues the document in one transaction. The
// series row stays locked until commit, so numbers are gap-free.
func (r *PGRepository) Issue(ctx context.Context, issuer uuid.UUID, prefix, fy string, inv db.CreateInvoiceV2Params, lines []Line) (uuid.UUID, error) {
	var id uuid.UUID
	err := r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		issuerID := uuid.NullUUID{UUID: issuer, Valid: true}
		if err := q.EnsureInvoiceSeries(ctx, db.EnsureInvoiceSeriesParams{IssuerOrgID: issuerID, FinancialYear: fy, Prefix: prefix}); err != nil {
			return err
		}
		series, err := q.GetInvoiceSeries(ctx, db.GetInvoiceSeriesParams{IssuerOrgID: issuerID, FinancialYear: fy, Prefix: prefix})
		if err != nil {
			return err
		}
		number, err := q.NextInvoiceNumber(ctx, series)
		if err != nil {
			return err
		}
		inv.SeriesID, inv.InvoiceNumber = series, number
		row, err := q.CreateInvoiceV2(ctx, inv)
		if err != nil {
			return err
		}
		for _, l := range lines {
			if err := q.AddInvoiceLineV2(ctx, db.AddInvoiceLineV2Params{
				InvoiceID: row.ID, LineNo: l.LineNo, Description: l.Description, SacCode: l.SACCode, Quantity: l.Quantity,
				Unit: l.Unit, RatePaise: l.RatePaise, TaxablePaise: l.TaxablePaise, GstRateBps: l.GSTRateBps, TaxCode: l.TaxCode,
			}); err != nil {
				return err
			}
		}
		if _, err := q.IssueInvoice(ctx, row.ID); err != nil {
			return err
		}
		id = row.ID
		return nil
	})
	return id, database.MapError(err)
}

func toInvoice(i db.Invoice) Invoice {
	out := Invoice{
		ID: i.ID, Number: i.InvoiceNumber, Title: i.Title, Type: string(i.InvoiceType), Status: string(i.Status), IssuerOrgID: i.IssuerOrgID,
		RecipientOrgID: i.RecipientOrgID, BookingID: i.BookingID, OriginalInvoiceID: i.OriginalInvoiceID, Reason: i.Reason,
		PlaceOfSupply: i.PlaceOfSupply, SupplyType: i.SupplyType, SACCode: i.SacCode, SubtotalPaise: i.SubtotalPaise,
		CGSTPaise: i.CgstPaise, SGSTPaise: i.SgstPaise, IGSTPaise: i.IgstPaise, TotalPaise: i.TotalPaise, Currency: i.Currency,
		IssuedAt: i.IssuedAt, NetPaise: i.TotalPaise,
	}
	var d Details
	if len(i.Details) > 2 && json.Unmarshal(i.Details, &d) == nil {
		out.Details = &d
	}
	return out
}

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (Invoice, error) {
	q := r.q(ctx)
	i, err := q.GetInvoice(ctx, id)
	if err != nil {
		return Invoice{}, database.MapError(err)
	}
	out := toInvoice(i)
	lines, err := q.ListInvoiceLines(ctx, id)
	if err != nil {
		return Invoice{}, database.MapError(err)
	}
	for _, l := range lines {
		out.Lines = append(out.Lines, Line{
			LineNo: l.LineNo, Description: l.Description, SACCode: l.SacCode, Quantity: l.Quantity, Unit: l.Unit,
			RatePaise: l.RatePaise, TaxablePaise: l.TaxablePaise, GSTRateBps: l.GstRateBps, TaxCode: l.TaxCode,
		})
	}
	return out, nil
}

func (r *PGRepository) CreditNotes(ctx context.Context, originalID uuid.UUID) ([]Invoice, error) {
	rows, err := r.q(ctx).ListCreditNotes(ctx, uuid.NullUUID{UUID: originalID, Valid: true})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Invoice, 0, len(rows))
	for _, i := range rows {
		out = append(out, toInvoice(i))
	}
	return out, nil
}

func (r *PGRepository) ForBooking(ctx context.Context, bookingID uuid.UUID) ([]Invoice, error) {
	rows, err := r.q(ctx).ListBookingInvoices(ctx, uuid.NullUUID{UUID: bookingID, Valid: true})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Invoice, 0, len(rows))
	for _, i := range rows {
		x := toInvoice(i)
		x.Details = nil // list view
		out = append(out, x)
	}
	return out, nil
}

func (r *PGRepository) ForOrg(ctx context.Context, org uuid.UUID, k httpx.Keyset) ([]Invoice, error) {
	rows, err := r.q(ctx).ListOrgInvoices(ctx, db.ListOrgInvoicesParams{
		OrgID: uuid.NullUUID{UUID: org, Valid: true}, After: k.After, ResultLimit: k.Fetch(),
	})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Invoice, 0, len(rows))
	for _, i := range rows {
		x := toInvoice(i)
		x.Details = nil
		out = append(out, x)
	}
	return out, nil
}

// ---- service -----------------------------------------------------------------------

type Service struct {
	repo     Repository
	bookings *bookings.Service
	cfg      config.BookingConfig
	now      func() time.Time
	// requireReviewed refuses to issue while an active tax rule has no
	// recorded compliance review (staging and production).
	requireReviewed bool
}

func NewService(repo Repository, bk *bookings.Service, cfg config.BookingConfig) *Service {
	return &Service{repo: repo, bookings: bk, cfg: cfg, now: time.Now}
}

// RequireReviewedTaxRules gates invoice issue on reviewed tax rules.
func (s *Service) RequireReviewedTaxRules(on bool) *Service { s.requireReviewed = on; return s }

func deref(p *string) string {
	if p == nil {
		return ""
	}
	return strings.TrimSpace(*p)
}

// supplierState is the owner's registered state (GSTIN prefix), falling
// back to the site's state.
func supplierState(c db.AgreementContextRow) string {
	if c.OwnerStateCode != nil && *c.OwnerStateCode != "" {
		return *c.OwnerStateCode
	}
	if g := deref(c.OwnerGstin); len(g) >= 2 {
		return g[:2]
	}
	return c.SiteStateCode
}

// IssueRental issues the owner's invoice to the customer once the machine is
// back (one per booking).
func (s *Service) IssueRental(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (Invoice, error) {
	_, side, err := s.bookings.Access(ctx, p, bookingID)
	if err != nil {
		return Invoice{}, err
	}
	if !side.Has(bookings.SideOwner) {
		return Invoice{}, apperror.ErrForbidden.WithDetail("the owner issues the rental invoice")
	}
	c, err := s.repo.Context(ctx, bookingID)
	if err != nil {
		return Invoice{}, err
	}
	switch string(c.Status) {
	case bookings.StatusReturned, bookings.StatusCompleted, bookings.StatusClosed:
	default:
		return Invoice{}, apperror.ErrInvalidTransition.WithDetail("invoice once the machine is returned")
	}
	if has, err := s.repo.HasInvoice(ctx, bookingID, db.InvoiceTypeRental); err != nil {
		return Invoice{}, err
	} else if has {
		return Invoice{}, apperror.ErrConflict.WithDetail("a rental invoice already exists for this booking")
	}
	if s.requireReviewed {
		n, err := s.repo.UnreviewedTaxRules(ctx)
		if err != nil {
			return Invoice{}, err
		}
		if n > 0 {
			return Invoice{}, apperror.ErrServiceUnavailable.WithDetail("tax rules in force have no recorded compliance review; invoices are paused")
		}
	}
	var price bookings.Price
	if err := json.Unmarshal(c.PriceSnapshot, &price); err != nil {
		return Invoice{}, err
	}
	var final *bookings.FinalCharges
	if len(c.FinalCharges) > 2 {
		var fc bookings.FinalCharges
		if json.Unmarshal(c.FinalCharges, &fc) == nil {
			final = &fc
		}
	}
	registered := deref(c.OwnerGstin) != ""
	built := BuildLines(price, final, registered)
	if len(built.Lines) == 0 {
		return Invoice{}, apperror.ErrBusinessRule.WithDetail("there is nothing to invoice for this booking")
	}
	tot := Sum(built.Lines, supplierState(c) == c.SiteStateCode)
	details, err := s.details(ctx, c, built)
	if err != nil {
		return Invoice{}, err
	}
	raw, _ := json.Marshal(details)
	title := "Tax invoice"
	if !registered {
		title = "Bill of supply"
	}
	sac := SACRental
	if len(built.Lines) > 0 {
		sac = built.Lines[0].SACCode
	}
	id, err := s.repo.Issue(ctx, c.OwnerOrgID, SeriesPrefix, FinancialYear(s.now()), db.CreateInvoiceV2Params{
		IssuerOrgID: uuid.NullUUID{UUID: c.OwnerOrgID, Valid: true}, RecipientOrgID: c.CustomerOrgID,
		BookingID: uuid.NullUUID{UUID: bookingID, Valid: true}, InvoiceType: db.InvoiceTypeRental,
		PlaceOfSupply: c.SiteStateCode, SupplyType: tot.SupplyType, SacCode: sac,
		SubtotalPaise: tot.Subtotal, CgstPaise: tot.CGST, SgstPaise: tot.SGST, IgstPaise: tot.IGST, TotalPaise: tot.Total(),
		Title: title, Details: raw,
	}, built.Lines)
	if err != nil {
		return Invoice{}, err
	}
	return s.repo.Get(ctx, id)
}

// details freezes the facts printed on the invoice.
func (s *Service) details(ctx context.Context, c db.AgreementContextRow, built Built) (Details, error) {
	pays, err := s.repo.Payments(ctx, c.BookingID)
	if err != nil {
		return Details{}, err
	}
	d := Details{
		BookingReference: c.Reference, Basis: built.Basis, Notes: append([]string{}, built.Notes...), Payments: []PaymentRef{},
		Supplier: Party{LegalName: c.OwnerLegalName, DisplayName: c.OwnerDisplayName, GSTIN: deref(c.OwnerGstin), Address: c.OwnerAddress,
			StateCode: supplierState(c), ContactName: deref(c.OwnerContactName), ContactPhone: deref(c.OwnerContactPhone)},
		Recipient: Party{LegalName: c.CustomerLegalName, DisplayName: c.CustomerDisplayName, GSTIN: deref(c.CustomerGstin),
			Address: c.CustomerAddress, StateCode: deref(c.CustomerStateCode), ContactName: deref(c.CustomerContactName)},
		Equipment: Equipment{Title: c.EquipmentTitle, Make: c.MakeName, Model: c.ModelName, Category: c.CategoryName,
			RegistrationNo: deref(c.RegistrationNo), SerialNo: deref(c.SerialNo), ListingID: c.EquipmentID.String()},
		Rental: Rental{Start: c.StartsAt.UTC(), End: c.EndsAt.UTC(), TimeZone: c.EquipmentTimezone, Site: c.SiteAddress, WithOp: c.WithOperator},
	}
	for _, p := range pays {
		if p.CapturedAt == nil {
			continue
		}
		ref := PaymentRef{Purpose: string(p.Purpose), AmountPaise: p.AmountPaise, ReceiptNumber: deref(p.ReceiptNumber),
			GatewayPaymentID: deref(p.GatewayPaymentID), CapturedAt: p.CapturedAt}
		if p.Method.Valid {
			ref.Method = string(p.Method.PaymentMethod)
		}
		d.Payments = append(d.Payments, ref)
	}
	if c.PaymentMode == "offline" {
		d.Notes = append(d.Notes, "Payment for this booking is made directly between the parties (offline).")
	}
	return d, nil
}

// CreateCreditNote credits part or all of a rental invoice (e.g. for a
// refund or a corrected charge).
func (s *Service) CreateCreditNote(ctx context.Context, p *httpx.Principal, invoiceID uuid.UUID, req CreditNoteRequest) (Invoice, error) {
	orig, err := s.repo.Get(ctx, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	issuer := orig.IssuerOrgID.Valid && p.HasOrgRole(orig.IssuerOrgID.UUID, httpx.RoleOwner, httpx.RoleOrgAdmin)
	switch {
	case !issuer && !p.HasPlatformRole(httpx.RoleAdminFinance):
		if orig.IssuerOrgID.Valid && (p.IsMember(orig.IssuerOrgID.UUID) || p.IsMember(orig.RecipientOrgID)) {
			return Invoice{}, apperror.ErrForbidden.WithDetail("the supplier or EQUIPRENT finance issues credit notes")
		}
		return Invoice{}, apperror.ErrNotFound
	case orig.Type != string(db.InvoiceTypeRental) || orig.Status != "issued":
		return Invoice{}, apperror.ErrBusinessRule.WithDetail("credit notes are issued against an issued rental invoice")
	}
	notes, err := s.repo.CreditNotes(ctx, invoiceID)
	if err != nil {
		return Invoice{}, err
	}
	var credSub, credTax int64
	for _, n := range notes {
		if n.Status == "issued" {
			credSub += n.SubtotalPaise
			credTax += n.TotalPaise - n.SubtotalPaise
		}
	}
	if left := orig.SubtotalPaise - credSub; req.AmountPaise > left {
		return Invoice{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "amount_paise", Code: "max",
			Message: fmt.Sprintf("at most %s of taxable value is left to credit", rupees(left))})
	}
	tot := CreditTax(orig, credSub, credTax, req.AmountPaise)
	rate := int32(0)
	if orig.SubtotalPaise > 0 {
		rate = int32((orig.CGSTPaise + orig.SGSTPaise + orig.IGSTPaise) * 10000 / orig.SubtotalPaise)
	}
	d := Details{Notes: []string{}, Payments: []PaymentRef{}}
	if orig.Details != nil {
		d = *orig.Details
		d.Payments, d.Notes, d.Basis = []PaymentRef{}, []string{}, ""
	}
	d.OriginalNumber = orig.Number
	raw, _ := json.Marshal(d)
	reason := strings.TrimSpace(req.Reason)
	id, err := s.repo.Issue(ctx, orig.IssuerOrgID.UUID, CreditNotePrefix, FinancialYear(s.now()), db.CreateInvoiceV2Params{
		IssuerOrgID: orig.IssuerOrgID, RecipientOrgID: orig.RecipientOrgID, BookingID: orig.BookingID, InvoiceType: db.InvoiceTypeCreditNote,
		OriginalInvoiceID: uuid.NullUUID{UUID: orig.ID, Valid: true}, PlaceOfSupply: orig.PlaceOfSupply, SupplyType: orig.SupplyType,
		SacCode: orig.SACCode, SubtotalPaise: tot.Subtotal, CgstPaise: tot.CGST, SgstPaise: tot.SGST, IgstPaise: tot.IGST,
		TotalPaise: tot.Total(), Title: "Credit note", Details: raw, Reason: &reason,
	}, []Line{{LineNo: 1, Description: "Credit against invoice " + orig.Number + ": " + reason, SACCode: orig.SACCode, Quantity: 1,
		Unit: "credit", RatePaise: req.AmountPaise, TaxablePaise: req.AmountPaise, GSTRateBps: rate, TaxCode: "credit"}})
	if err != nil {
		return Invoice{}, err
	}
	return s.repo.Get(ctx, id)
}

// ForBooking lists a booking's invoices (parties).
func (s *Service) ForBooking(ctx context.Context, p *httpx.Principal, bookingID uuid.UUID) (InvoiceList, error) {
	if _, _, err := s.bookings.Access(ctx, p, bookingID); err != nil {
		return InvoiceList{}, err
	}
	list, err := s.repo.ForBooking(ctx, bookingID)
	return InvoiceList{Data: list}, err
}

// canRead: the issuer, the recipient and finance admins.
func canRead(p *httpx.Principal, inv Invoice) bool {
	issuerMember := inv.IssuerOrgID.Valid && p.IsMember(inv.IssuerOrgID.UUID)
	return issuerMember || p.IsMember(inv.RecipientOrgID) || p.HasPlatformRole(httpx.RoleAdminFinance)
}

// Get returns an invoice with its adjustments to the parties and finance.
func (s *Service) Get(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Invoice, error) {
	inv, err := s.repo.Get(ctx, id)
	if err != nil {
		return Invoice{}, err
	}
	if !canRead(p, inv) || !s.inScope(ctx, p, inv) {
		return Invoice{}, apperror.ErrNotFound
	}
	return s.withAdjustments(ctx, inv)
}

// fleetOnly: the caller acts in org only as a fleet manager.
func fleetOnly(p *httpx.Principal, org uuid.UUID) bool {
	return p.HasOrgRole(org, httpx.RoleFleetManager) && !p.HasOrgRole(org, httpx.RoleOwner, httpx.RoleCustomer, httpx.RoleOrgAdmin)
}

// inScope limits a fleet manager to invoices of bookings on machines they
// manage (the booking's access rules decide).
func (s *Service) inScope(ctx context.Context, p *httpx.Principal, inv Invoice) bool {
	if !inv.IssuerOrgID.Valid || !fleetOnly(p, inv.IssuerOrgID.UUID) || p.HasPlatformRole(httpx.RoleAdminFinance) {
		return true
	}
	if !inv.BookingID.Valid {
		return false
	}
	_, side, err := s.bookings.Access(ctx, p, inv.BookingID.UUID)
	return err == nil && side.Has(bookings.SideOwner)
}

// withAdjustments adds the booking's refunds and the credit notes issued
// against a rental invoice.
func (s *Service) withAdjustments(ctx context.Context, inv Invoice) (Invoice, error) {
	inv.Adjustments = []Adjustment{}
	if inv.Type != string(db.InvoiceTypeRental) {
		return inv, nil
	}
	notes, err := s.repo.CreditNotes(ctx, inv.ID)
	if err != nil {
		return Invoice{}, err
	}
	for _, n := range notes {
		inv.Adjustments = append(inv.Adjustments, Adjustment{Kind: "credit_note", ID: n.ID, Reference: n.Number, AmountPaise: n.TotalPaise,
			Status: n.Status, Reason: deref(n.Reason), At: n.IssuedAt})
		if n.Status == "issued" {
			inv.CreditedPaise += n.TotalPaise
		}
	}
	inv.NetPaise = inv.TotalPaise - inv.CreditedPaise
	if inv.BookingID.Valid {
		refunds, err := s.repo.Refunds(ctx, inv.BookingID.UUID)
		if err != nil {
			return Invoice{}, err
		}
		for _, r := range refunds {
			at := r.ProcessedAt
			if at == nil {
				at = &r.CreatedAt
			}
			inv.Adjustments = append(inv.Adjustments, Adjustment{Kind: "refund", ID: r.ID, AmountPaise: r.AmountPaise, Status: r.Status,
				Reason: r.Reason, At: at})
		}
	}
	return inv, nil
}

// ForOrg pages invoices issued by or to the acting organisation.
func (s *Service) ForOrg(ctx context.Context, p *httpx.Principal, org uuid.UUID, q httpx.PageQuery) (InvoiceList, error) {
	k, err := q.Keyset()
	if err != nil {
		return InvoiceList{}, err
	}
	rows, err := s.repo.ForOrg(ctx, org, k)
	if err != nil {
		return InvoiceList{}, err
	}
	page := httpx.Paginate(rows, k, func(i Invoice) uuid.UUID { return i.ID })
	if p != nil && fleetOnly(p, org) {
		kept := page.Data[:0]
		for _, inv := range page.Data {
			if s.inScope(ctx, p, inv) {
				kept = append(kept, inv)
			}
		}
		page.Data = kept
	}
	return InvoiceList{Data: page.Data, NextCursor: page.NextCursor}, nil
}

// audit records a document event (downloads are part of the history).
func (s *Service) audit(ctx context.Context, user uuid.UUID, inv Invoice, action, reason string) error {
	reqID := httpx.RequestIDFromContext(ctx)
	p := db.InsertAuditLogParams{Action: action, EntityType: "invoice", EntityID: uuid.NullUUID{UUID: inv.ID, Valid: true},
		Reason: &reason, RequestID: &reqID}
	if user != uuid.Nil {
		p.ActorUserID = uuid.NullUUID{UUID: user, Valid: true}
	}
	return s.repo.Audit(ctx, p)
}

// ---- handler -----------------------------------------------------------------------

type Handler struct {
	svc   *Service
	links *Links
}

func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// WithLinks enables signed download links (GET /invoices/:id/link).
func (h *Handler) WithLinks(l *Links) *Handler { h.links = l; return h }

func (h *Handler) Name() string { return "invoices" }

func (h *Handler) Routes(r *httpx.Router) {
	a := r.Tag("invoices").Authed()
	a.POST("/owner/bookings/:id/invoices", httpx.Handle(201, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.IssueRental(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Issue the rental invoice (tax invoice, or bill of supply without a GSTIN)", Response: Invoice{}, Status: 201,
		Description: "Lines come from the settled final charges (else the estimate), each taxed by the rules frozen with the booking."})
	a.POST("/invoices/:id/credit-notes", httpx.HandleJSON(201, func(c *gin.Context, p *httpx.Principal, req CreditNoteRequest) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.CreateCreditNote(c.Request.Context(), p, id, req)
	}), httpx.Doc{Summary: "Issue a credit note against a rental invoice", Request: CreditNoteRequest{}, Response: Invoice{}, Status: 201,
		Description: "Supplier (owner) or EQUIPRENT finance. The tax is credited in proportion; at most the invoice's remaining taxable value."})
	a.GET("/bookings/:id/invoices", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.ForBooking(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Invoices and credit notes of a booking", Response: InvoiceList{}})
	a.GET("/invoices", httpx.OrgHandle(200, nil, func(c *gin.Context, s httpx.Scope) (any, error) {
		var q httpx.PageQuery
		if err := httpx.BindQ(c, &q); err != nil {
			return nil, err
		}
		return h.svc.ForOrg(c.Request.Context(), s.P, s.Org, q)
	}), httpx.Doc{Summary: "Invoices issued by or to my organisation", Query: httpx.PageQuery{}, Response: InvoiceList{}, OrgScoped: true})
	a.GET("/invoices/:id", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return h.svc.Get(c.Request.Context(), p, id)
	}), httpx.Doc{Summary: "Invoice with lines, frozen details, refunds and credit notes", Response: Invoice{}})
	a.GET("/invoices/:id/pdf", h.pdf, httpx.Doc{Summary: "Download the invoice as a PDF",
		Description: "application/pdf attachment; parties to the invoice and finance only. Each download is audited."})
	if h.links != nil {
		a.GET("/invoices/:id/link", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
			id, err := httpx.Param(c, "id")
			if err != nil {
				return nil, err
			}
			inv, err := h.svc.Get(c.Request.Context(), p, id)
			if err != nil {
				return nil, err
			}
			if err := h.svc.audit(c.Request.Context(), p.UserID, inv, "document.url_issued", "invoice "+inv.Number+" download link"); err != nil {
				return nil, err
			}
			return h.links.Sign(id), nil
		}), httpx.Doc{Summary: "Short-lived download link for the invoice PDF", Response: DownloadLink{},
			Description: "For browsers and apps that open the PDF outside the API client (no Authorization header). Issuing the link is audited."})
		r.Tag("invoices").GET("/invoices/:id/download", h.signedPDF, httpx.Doc{Summary: "Invoice PDF through a signed link",
			Description: "Needs the exp and sig query parameters from GET /invoices/{id}/link."})
	}
}
