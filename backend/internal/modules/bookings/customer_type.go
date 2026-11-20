package bookings

import (
	"encoding/json"
	"regexp"
	"strings"

	"github.com/equiprent/backend/internal/apperror"
)

// Customer rental types stored on bookings.customer_type.
const (
	CustomerIndividual = "individual"
	CustomerCompany    = "company"
)

// CustomerDetails is what the customer entered for this booking. Individuals
// give personal contact details (the work site is the booking's site address);
// companies also give business details. Stored in bookings.customer_details.
type CustomerDetails struct {
	Name        string `json:"name,omitempty" binding:"max=120" doc:"Individual: customer name"`
	CompanyName string `json:"company_name,omitempty" binding:"max=160"`
	ContactName string `json:"contact_name,omitempty" binding:"max=120" doc:"Company: contact person"`
	Phone       string `json:"phone,omitempty" binding:"max=20"`
	Email       string `json:"email,omitempty" binding:"max=160"`
	Address     string `json:"address,omitempty" binding:"max=300" doc:"Company: business address"`
	City        string `json:"city,omitempty" binding:"max=80"`
	State       string `json:"state,omitempty" binding:"max=80"`
	PinCode     string `json:"pin_code,omitempty" binding:"max=6"`
	GSTIN       string `json:"gstin,omitempty" binding:"max=15"`
}

var (
	phoneRE = regexp.MustCompile(`^[6-9][0-9]{9}$`)
	emailRE = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
	pinRE   = regexp.MustCompile(`^[1-9][0-9]{5}$`)
	gstinRE = regexp.MustCompile(`^[0-9]{2}[A-Z]{5}[0-9]{4}[A-Z][1-9A-Z]Z[0-9A-Z]$`)
)

// normalizeCustomer validates the customer block for the chosen type and
// returns the JSON to store. Both empty = legacy client: stored as an
// individual with no details (the owner view falls back to the account contact).
func normalizeCustomer(kind string, d *CustomerDetails) (string, []byte, error) {
	if kind == "" && d == nil {
		return CustomerIndividual, []byte("{}"), nil
	}
	if kind != CustomerIndividual && kind != CustomerCompany {
		return "", nil, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "customer_type", Code: "required", Message: "choose individual or company"})
	}
	if d == nil {
		d = &CustomerDetails{}
	}
	c := *d
	for _, f := range []*string{&c.Name, &c.CompanyName, &c.ContactName, &c.Phone, &c.Email, &c.Address, &c.City, &c.State, &c.PinCode, &c.GSTIN} {
		*f = strings.TrimSpace(*f)
	}
	c.GSTIN = strings.ToUpper(c.GSTIN)
	var errs []apperror.FieldError
	need := func(field, v, msg string) {
		if v == "" {
			errs = append(errs, apperror.FieldError{Field: "customer." + field, Code: "required", Message: msg + " is required"})
		}
	}
	bad := func(field, msg string) {
		errs = append(errs, apperror.FieldError{Field: "customer." + field, Code: "invalid", Message: msg})
	}
	if kind == CustomerIndividual {
		need("name", c.Name, "customer name")
		c.CompanyName, c.ContactName, c.Address, c.City, c.State, c.PinCode, c.GSTIN = "", "", "", "", "", "", ""
	} else {
		need("company_name", c.CompanyName, "company name")
		need("contact_name", c.ContactName, "contact person")
		need("address", c.Address, "business address")
		need("city", c.City, "city")
		need("state", c.State, "state")
		need("pin_code", c.PinCode, "PIN code")
		c.Name = ""
		if c.PinCode != "" && !pinRE.MatchString(c.PinCode) {
			bad("pin_code", "enter a 6-digit PIN code")
		}
		if c.GSTIN != "" && !gstinRE.MatchString(c.GSTIN) {
			bad("gstin", "enter a valid 15-character GSTIN")
		}
	}
	need("phone", c.Phone, "mobile number")
	need("email", c.Email, "email")
	if c.Phone != "" && !phoneRE.MatchString(c.Phone) {
		bad("phone", "enter a valid 10-digit mobile number")
	}
	if c.Email != "" && !emailRE.MatchString(c.Email) {
		bad("email", "enter a valid email address")
	}
	if len(errs) > 0 {
		return "", nil, apperror.ErrValidation.WithFields(errs...)
	}
	raw, err := json.Marshal(c)
	return kind, raw, err
}
