package tests

import "testing"

// ackInspection records the customer's and the equipment side's clean inspection
// acknowledgements, which the start gate requires (setup shortcut; the API path is covered elsewhere).
func (u *user) ackInspection(t *testing.T, bookingID, stage string) {
	t.Helper()
	u.e.exec(`INSERT INTO booking_inspection_acknowledgements (booking_id, stage, user_id, role)
SELECT b.id, $2, b.requested_by, 'customer' FROM bookings b WHERE b.id = $1
UNION ALL
SELECT b.id, $2, COALESCE(a.operator_user_id, (SELECT ur.user_id FROM user_roles ur WHERE ur.organization_id = b.owner_org_id AND ur.role_code = 'owner' AND ur.revoked_at IS NULL LIMIT 1)),
       CASE WHEN b.with_operator THEN 'operator' ELSE 'owner' END
FROM bookings b LEFT JOIN operator_assignments a ON a.booking_id = b.id AND a.status IN ('assigned', 'accepted') WHERE b.id = $1
ON CONFLICT DO NOTHING`, bookingID, stage)
}
