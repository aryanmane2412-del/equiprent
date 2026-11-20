package reviews

import (
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/modules/bookings"
)

func TestSubjectRules(t *testing.T) {
	b := bookings.Booking{EquipmentID: uuid.New(), OwnerOrgID: uuid.New(), CustomerOrgID: uuid.New()}
	cases := []struct {
		side    bookings.Side
		subject string
		org, id uuid.UUID
		err     error
	}{
		{bookings.SideCustomer, "equipment", b.CustomerOrgID, b.EquipmentID, nil},
		{bookings.SideCustomer, "owner", b.CustomerOrgID, b.OwnerOrgID, nil},
		{bookings.SideOwner, "customer", b.OwnerOrgID, b.CustomerOrgID, nil},
		{bookings.SideOwner, "equipment", uuid.Nil, uuid.Nil, apperror.ErrForbidden},
		{bookings.SideCustomer, "customer", uuid.Nil, uuid.Nil, apperror.ErrForbidden},
		{bookings.SideCustomer, "operator", uuid.Nil, uuid.Nil, apperror.ErrBusinessRule},
	}
	for _, tc := range cases {
		org, id, err := Subject(b, tc.side, tc.subject)
		if !errors.Is(err, tc.err) || (tc.err == nil && (org != tc.org || id != tc.id)) {
			t.Errorf("side %b subject %s: org=%v id=%v err=%v", tc.side, tc.subject, org, id, err)
		}
	}
	op := uuid.New()
	b.Operator = &bookings.Assignment{UserID: op}
	if _, id, err := Subject(b, bookings.SideCustomer, "operator"); err != nil || id != op {
		t.Fatalf("operator review: %v %v", id, err)
	}
}
