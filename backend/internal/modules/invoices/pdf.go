package invoices

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"

	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/pdfdoc"
)

// rupees formats paise for the PDF (the standard PDF fonts have no ₹ glyph).
func rupees(paise int64) string {
	neg := paise < 0
	if neg {
		paise = -paise
	}
	whole, frac := paise/100, paise%100
	digits := fmt.Sprint(whole)
	if len(digits) > 3 { // Indian grouping: 12,34,567
		head, tail := digits[:len(digits)-3], digits[len(digits)-3:]
		var groups []string
		for len(head) > 2 {
			groups = append([]string{head[len(head)-2:]}, groups...)
			head = head[:len(head)-2]
		}
		digits = strings.Join(append([]string{head}, groups...), ",") + "," + tail
	}
	out := fmt.Sprintf("Rs. %s.%02d", digits, frac)
	if neg {
		return "-" + out
	}
	return out
}

// BuildPDF renders an issued invoice, bill of supply or credit note. The
// output depends only on the stored invoice, so a re-download is identical.
func BuildPDF(inv Invoice) []byte {
	issued := time.Unix(0, 0).UTC()
	if inv.IssuedAt != nil {
		issued = *inv.IssuedAt
	}
	title := inv.Title
	if title == "" {
		title = "Tax invoice"
	}
	d := &pdfdoc.Doc{
		Title: title + " " + inv.Number, Author: "EQUIPRENT", Subject: "Equipment rental " + strings.ToLower(title), Created: issued,
		Header: "EQUIPRENT - " + title + " " + inv.Number, Footer: title + " " + inv.ID.String(),
	}
	if inv.Status != "issued" {
		d.Watermark = strings.ToUpper(inv.Status)
	}
	det := inv.Details
	if det == nil {
		det = &Details{}
	}
	d.Heading(title, 16)
	d.Field("Number", inv.Number)
	d.Field("Date", issued.In(ist).Format("02 Jan 2006"))
	if det.OriginalNumber != "" {
		d.Field("Against invoice", det.OriginalNumber)
	}
	if inv.Reason != nil {
		d.Field("Reason", *inv.Reason)
	}
	if det.BookingReference != "" {
		d.Field("Booking", det.BookingReference)
	}
	d.Field("Place of supply", inv.PlaceOfSupply+" (state code)")
	if inv.CGSTPaise+inv.SGSTPaise+inv.IGSTPaise > 0 {
		d.Field("Supply", map[string]string{"intra": "Intra-state (CGST + SGST)", "inter": "Inter-state (IGST)"}[inv.SupplyType])
	}
	d.Field("SAC", inv.SACCode)

	party := func(heading string, p Party) {
		d.Space(4)
		d.Heading(heading, 11)
		name := p.LegalName
		if p.DisplayName != "" && p.DisplayName != p.LegalName {
			name += " (" + p.DisplayName + ")"
		}
		d.Field("Name", orDash(name))
		d.Field("GSTIN", orDash(p.GSTIN))
		d.Field("Address", orDash(p.Address))
		if p.StateCode != "" {
			d.Field("State code", p.StateCode)
		}
		if p.ContactName != "" || p.ContactPhone != "" {
			d.Field("Contact", strings.TrimSpace(p.ContactName+" "+p.ContactPhone))
		}
	}
	d.Rule()
	party("Supplier (owner)", det.Supplier)
	party("Recipient (customer)", det.Recipient)

	if det.Equipment.Title != "" {
		d.Rule()
		d.Heading("Equipment and rental", 11)
		d.Field("Machine", det.Equipment.Title)
		if mm := strings.TrimSpace(det.Equipment.Make + " " + det.Equipment.Model); mm != "" {
			d.Field("Make and model", mm)
		}
		d.Field("Registration no.", orDash(det.Equipment.RegistrationNo))
		if det.Equipment.SerialNo != "" {
			d.Field("Serial no.", det.Equipment.SerialNo)
		}
		if !det.Rental.Start.IsZero() {
			loc, err := time.LoadLocation(det.Rental.TimeZone)
			if err != nil {
				loc = ist
			}
			d.Field("Rental period", det.Rental.Start.In(loc).Format("02 Jan 2006 15:04")+" to "+det.Rental.End.In(loc).Format("02 Jan 2006 15:04 MST"))
		}
		if det.Rental.Site != "" {
			d.Field("Site", det.Rental.Site)
		}
		if det.Basis == "estimate" {
			d.Field("Basis", "Accepted estimate")
		} else if det.Basis == "final" {
			d.Field("Basis", "Final charges after the rental")
		}
	}

	d.Rule()
	d.Heading("Lines", 11)
	for _, l := range inv.Lines {
		d.Para(fmt.Sprintf("%d. %s", l.LineNo, l.Description))
		tax := "no GST"
		if l.GSTRateBps > 0 {
			tax = fmt.Sprintf("GST %s%%", pct(l.GSTRateBps))
		}
		d.Small(fmt.Sprintf("SAC %s   %g %s x %s = %s taxable   %s", l.SACCode, l.Quantity, l.Unit, rupees(l.RatePaise), rupees(l.TaxablePaise), tax))
	}
	d.Rule()
	d.Field("Taxable value", rupees(inv.SubtotalPaise))
	switch {
	case inv.IGSTPaise > 0:
		d.Field("IGST", rupees(inv.IGSTPaise))
	case inv.CGSTPaise > 0:
		d.Field("CGST", rupees(inv.CGSTPaise))
		d.Field("SGST", rupees(inv.SGSTPaise))
	}
	d.Field("Total", rupees(inv.TotalPaise))
	if inv.CreditedPaise > 0 {
		d.Field("Credited", rupees(inv.CreditedPaise))
		d.Field("Net after credit notes", rupees(inv.NetPaise))
	}

	if len(det.Payments) > 0 {
		d.Rule()
		d.Heading("Payments received for this booking", 11)
		for _, p := range det.Payments {
			ref := p.ReceiptNumber
			if p.GatewayPaymentID != "" {
				ref = strings.TrimSpace(ref + " / " + p.GatewayPaymentID)
			}
			when := ""
			if p.CapturedAt != nil {
				when = p.CapturedAt.In(ist).Format("02 Jan 2006")
			}
			d.Field(strings.ReplaceAll(p.Purpose, "_", " "), strings.TrimSpace(fmt.Sprintf("%s  %s  %s %s", rupees(p.AmountPaise), when, p.Method, orDash(ref))))
		}
	}
	if len(inv.Adjustments) > 0 {
		d.Rule()
		d.Heading("Refunds and adjustments", 11)
		for _, a := range inv.Adjustments {
			label := "Refund"
			if a.Kind == "credit_note" {
				label = "Credit note " + a.Reference
			}
			d.Field(label, fmt.Sprintf("%s (%s) %s", rupees(a.AmountPaise), a.Status, a.Reason))
		}
	}
	for _, n := range det.Notes {
		d.Small(n)
	}
	d.Space(10)
	d.Small("This is a computer-generated document issued through EQUIPRENT on behalf of the supplier named above and needs no signature.")
	return d.Bytes()
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func pct(bps int32) string {
	if bps%100 == 0 {
		return fmt.Sprint(bps / 100)
	}
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.2f", float64(bps)/100), "0"), ".")
}

// pdf serves GET /invoices/:id/pdf to the issuer, the recipient and finance.
func (h *Handler) pdf(c *gin.Context) {
	p, ok := httpx.MustPrincipal(c)
	if !ok {
		return
	}
	id, err := httpx.Param(c, "id")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	inv, err := h.svc.Get(c.Request.Context(), p, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	if err := h.svc.audit(c.Request.Context(), p.UserID, inv, "document.downloaded", "invoice "+inv.Number+" PDF downloaded"); err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+fileSlug(inv)+`-`+strings.ReplaceAll(inv.Number, "/", "-")+`.pdf"`)
	c.Header("Cache-Control", "private, no-store")
	c.Data(200, "application/pdf", BuildPDF(inv))
}

// Links signs short-lived invoice download URLs (HMAC over id and expiry).
type Links struct {
	secret []byte
	base   string // e.g. https://api.equiprent.in/api/v1
	ttl    time.Duration
	now    func() time.Time
}

func NewLinks(secret, base string, ttl time.Duration) *Links {
	return &Links{secret: []byte("invoice-pdf\n" + secret), base: strings.TrimRight(base, "/"), ttl: ttl, now: time.Now}
}

// DownloadLink is a signed, expiring PDF URL.
type DownloadLink struct {
	URL       string    `json:"url"`
	ExpiresAt time.Time `json:"expires_at"`
}

func (l *Links) mac(id string, exp int64) string {
	m := hmac.New(sha256.New, l.secret)
	m.Write([]byte(id + "\n" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (l *Links) Sign(id uuid.UUID) DownloadLink {
	exp := l.now().Add(l.ttl).Unix()
	q := url.Values{"exp": {strconv.FormatInt(exp, 10)}, "sig": {l.mac(id.String(), exp)}}
	return DownloadLink{URL: l.base + "/invoices/" + id.String() + "/download?" + q.Encode(), ExpiresAt: time.Unix(exp, 0).UTC()}
}

func (l *Links) valid(id, expRaw, sig string) bool {
	exp, err := strconv.ParseInt(expRaw, 10, 64)
	if err != nil || l.now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(l.mac(id, exp)), []byte(sig))
}

// signedPDF serves GET /invoices/:id/download?exp=&sig= (no session).
func (h *Handler) signedPDF(c *gin.Context) {
	id, err := httpx.Param(c, "id")
	if err != nil || !h.links.valid(id.String(), c.Query("exp"), c.Query("sig")) {
		httpx.Fail(c, apperror.ErrNotFound)
		return
	}
	inv, err := h.svc.repo.Get(c.Request.Context(), id)
	if err == nil {
		inv, err = h.svc.withAdjustments(c.Request.Context(), inv)
	}
	if err == nil {
		err = h.svc.audit(c.Request.Context(), uuid.Nil, inv, "document.downloaded", "invoice "+inv.Number+" PDF downloaded through a signed link")
	}
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+fileSlug(inv)+`-`+strings.ReplaceAll(inv.Number, "/", "-")+`.pdf"`)
	c.Header("Cache-Control", "private, no-store")
	c.Data(200, "application/pdf", BuildPDF(inv))
}

// fileSlug names the download after the document kind.
func fileSlug(inv Invoice) string {
	if inv.Type == "credit_note" {
		return "credit-note"
	}
	return "invoice"
}
