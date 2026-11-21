package tests

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/access"
	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
)

// H7: an owner can only see and manage the machines and trackers of their own organisation.
func TestOwnerCannotManageAnotherOrganisationsEquipment(t *testing.T) {
	e := newEnv(t)
	kyc, ops := e.kycAdmin(), e.opsAdmin()
	a := e.verifiedOwner(kyc, "Owner A "+randomLetters(4))
	b := e.verifiedOwner(kyc, "Owner B "+randomLetters(4))
	eqA := e.publish(a, ops, listingBody(map[string]any{"title": "Owner A loader " + randomLetters(5)}))
	eqB := e.publish(b, ops, listingBody(map[string]any{"title": "Owner B loader " + randomLetters(5)}))

	// Machine endpoints answer 404 for a rival's machine (existence is not disclosed).
	must(t, a.do(http.MethodGet, "/owner/equipment/"+eqB, nil), 404, "A reads B's machine")
	must(t, a.do(http.MethodGet, "/owner/equipment/"+eqA, nil), 200, "A reads own machine")

	// Trackers: list, create, revoke across organisations.
	must(t, a.do(http.MethodGet, "/owner/equipment/"+eqB+"/trackers", nil), 404, "A lists B's trackers")
	must(t, a.do(http.MethodPost, "/owner/equipment/"+eqB+"/trackers", map[string]any{"provider": "simulated", "external_id": "A-" + randomLetters(6)}), 404, "A fits a tracker to B's machine")
	dev := must(t, b.do(http.MethodPost, "/owner/equipment/"+eqB+"/trackers", map[string]any{"provider": "simulated", "external_id": "B-" + randomLetters(6)}), 201, "B fits own tracker")
	devID := dev.str("device.id")
	must(t, a.do(http.MethodDelete, "/owner/equipment/"+eqB+"/trackers/"+devID, nil), 404, "A revokes B's tracker via B's machine")
	must(t, a.do(http.MethodDelete, "/owner/equipment/"+eqA+"/trackers/"+devID, nil), 404, "A revokes B's tracker via own machine")
	if st := e.query1(`SELECT status FROM telematics_devices WHERE id = $1`, devID); st != "active" {
		t.Fatalf("B's tracker status = %s", st)
	}
	if n := e.query1(`SELECT count(*) FROM telematics_devices WHERE equipment_id = $1`, eqB); n != "1" {
		t.Fatalf("trackers on B's machine = %s", n)
	}
	must(t, b.do(http.MethodGet, "/owner/equipment/"+eqB+"/trackers", nil), 200, "B lists own trackers")

	// The checker itself: a role in one organisation never reaches another organisation's machine.
	chk := access.NewChecker(e.pool)
	orgA, orgB := uuid.MustParse(a.org), uuid.MustParse(b.org)
	pA := &httpx.Principal{UserID: uuid.MustParse(a.id), ActingOrg: orgA, Roles: []httpx.Role{{Code: httpx.RoleOwner, OrgID: orgA}}}
	if err := chk.CanManage(context.Background(), pA, orgA, uuid.MustParse(eqB)); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("CanManage(A, B's machine) = %v", err)
	}
	if err := chk.CanManage(context.Background(), pA, orgB, uuid.MustParse(eqB)); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("CanManage(A claiming org B) = %v", err)
	}
	if err := chk.CanManage(context.Background(), pA, orgA, uuid.MustParse(eqA)); err != nil {
		t.Fatalf("CanManage(A, own machine) = %v", err)
	}
}

// H8: operators join only by accepting an invitation; existing data is never overwritten.
func TestOperatorInvitationWorkflow(t *testing.T) {
	e := newEnv(t)
	kyc := e.kycAdmin()
	o1 := e.verifiedOwner(kyc, "Invite One "+randomLetters(4))
	o2 := e.verifiedOwner(kyc, "Invite Two "+randomLetters(4))

	phone := randomPhone()
	inv := must(t, o1.do(http.MethodPost, "/orgs/"+o1.org+"/members", map[string]any{"phone": phone, "role": "operator", "full_name": "Ramesh"}), 201, "invite")
	if inv.str("status") != "invited" || inv.str("invitation_id") == "" {
		t.Fatalf("invitation = %s", inv.raw)
	}
	op := e.login(phone)
	if n := e.query1(`SELECT count(*) FROM user_roles WHERE user_id = $1 AND role_code = 'operator' AND revoked_at IS NULL`, op.id); n != "0" {
		t.Fatalf("operator role granted before acceptance: %s", n)
	}
	if n := e.query1(`SELECT count(*) FROM operator_profiles WHERE user_id = $1`, op.id); n != "0" {
		t.Fatalf("operator profile created before acceptance: %s", n)
	}
	must(t, o1.do(http.MethodPost, "/orgs/"+o1.org+"/members", map[string]any{"phone": phone, "role": "operator"}), 409, "duplicate pending invitation")

	// Only the invitee can answer.
	must(t, o2.do(http.MethodPost, "/invitations/"+inv.str("invitation_id")+"/accept", nil), 404, "someone else accepts")
	must(t, o1.do(http.MethodPost, "/invitations/"+inv.str("invitation_id")+"/accept", nil), 404, "the inviter accepts")
	list := must(t, op.do(http.MethodGet, "/me/invitations", nil), 200, "my invitations")
	if len(list.list("data")) != 1 {
		t.Fatalf("invitations = %s", list.raw)
	}
	must(t, op.do(http.MethodPost, "/invitations/"+inv.str("invitation_id")+"/accept", nil), 204, "accept")
	must(t, op.do(http.MethodPost, "/invitations/"+inv.str("invitation_id")+"/accept", nil), 409, "accept twice")
	if n := e.query1(`SELECT count(*) FROM user_roles WHERE user_id = $1 AND role_code = 'operator' AND organization_id = $2 AND revoked_at IS NULL`, op.id, o1.org); n != "1" {
		t.Fatalf("operator role after acceptance: %s", n)
	}

	// The operator now has licence details and a verification; a second organisation invites them.
	e.exec(`UPDATE operator_profiles SET licence_class = 'HMV', licence_last4 = 'AB12', experience_years = 7, verified_at = now() WHERE user_id = $1`, op.id)
	snap := func() string {
		return e.query1(`SELECT concat_ws('|', licence_class, licence_last4, experience_years, verified_at IS NOT NULL, employer_organization_id) FROM operator_profiles WHERE user_id = $1`, op.id)
	}
	before := snap()
	inv2 := must(t, o2.do(http.MethodPost, "/orgs/"+o2.org+"/members", map[string]any{"phone": phone, "role": "operator", "full_name": "Other Name"}), 201, "second organisation invites")
	if snap() != before {
		t.Fatalf("inviting changed the operator profile: %q -> %q", before, snap())
	}
	if n := e.query1(`SELECT count(*) FROM user_roles WHERE user_id = $1 AND role_code = 'operator' AND organization_id = $2`, op.id, o2.org); n != "0" {
		t.Fatalf("role granted by an invitation alone: %s", n)
	}
	// No transfer while still employed elsewhere.
	must(t, op.do(http.MethodPost, "/invitations/"+inv2.str("invitation_id")+"/accept", nil), 409, "accept while employed by another organisation")
	if snap() != before {
		t.Fatalf("a refused transfer changed the profile: %q", snap())
	}
	// The first organisation releases the operator; now the move is allowed, licence data intact.
	must(t, o1.do(http.MethodDelete, "/orgs/"+o1.org+"/members/"+op.id+"?role=operator", nil), 204, "release operator")
	must(t, op.do(http.MethodPost, "/invitations/"+inv2.str("invitation_id")+"/accept", nil), 204, "accept after release")
	after := e.query1(`SELECT concat_ws('|', licence_class, licence_last4, experience_years, verified_at IS NOT NULL) FROM operator_profiles WHERE user_id = $1`, op.id)
	if after != "HMV|AB12|7|t" {
		t.Fatalf("licence data changed: %s", after)
	}
	if emp := e.query1(`SELECT employer_organization_id::text FROM operator_profiles WHERE user_id = $1`, op.id); emp != o2.org {
		t.Fatalf("employer = %s, want %s", emp, o2.org)
	}

	// Declining and withdrawing leave no role.
	p3 := randomPhone()
	d := must(t, o1.do(http.MethodPost, "/orgs/"+o1.org+"/members", map[string]any{"phone": p3, "role": "operator"}), 201, "invite 3")
	u3 := e.login(p3)
	must(t, u3.do(http.MethodPost, "/invitations/"+d.str("invitation_id")+"/decline", nil), 204, "decline")
	must(t, u3.do(http.MethodPost, "/invitations/"+d.str("invitation_id")+"/accept", nil), 409, "accept a declined invitation")
	w := must(t, o1.do(http.MethodPost, "/orgs/"+o1.org+"/members", map[string]any{"phone": p3, "role": "operator"}), 201, "invite again")
	must(t, o1.do(http.MethodDelete, "/orgs/"+o1.org+"/invitations/"+w.str("invitation_id"), nil), 204, "withdraw")
	must(t, u3.do(http.MethodPost, "/invitations/"+w.str("invitation_id")+"/accept", nil), 409, "accept a withdrawn invitation")
	if n := e.query1(`SELECT count(*) FROM user_roles WHERE user_id = $1 AND role_code = 'operator'`, u3.id); n != "0" {
		t.Fatalf("operator role for a declined/withdrawn invitation: %s", n)
	}
	// Expired invitations cannot be accepted.
	x := must(t, o1.do(http.MethodPost, "/orgs/"+o1.org+"/members", map[string]any{"phone": p3, "role": "operator"}), 201, "invite 4")
	e.exec(`UPDATE org_invitations SET expires_at = now() - interval '1 minute' WHERE id = $1`, x.str("invitation_id"))
	must(t, u3.do(http.MethodPost, "/invitations/"+x.str("invitation_id")+"/accept", nil), 409, "accept expired")
}

// H9: email codes are bound to the address, limited to five attempts, expire and cannot be replayed.
func TestEmailVerificationTokenSecurity(t *testing.T) {
	e := newEnv(t)
	register := func() (email, token string) {
		email = randomEmail()
		reg := must(t, e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
			"full_name": "Asha Kulkarni", "email": email, "phone": randomPhone(), "password": goodPassword, "account_type": "customer", "accept_terms": true,
		}, nil), 202, "register")
		return email, reg.str("dev_email_token")
	}
	verify := func(email, token string) resp {
		return e.call(http.MethodPost, "/auth/email/verify", "", "", map[string]any{"email": email, "token": token}, nil)
	}
	wrong := func(token string) string {
		if token == "000000" {
			return "000001"
		}
		return "000000"
	}
	emailA, tokenA := register()
	emailB, tokenB := register()

	// Bound to the address: another user's address, or an unknown one, is refused with the same answer.
	r1 := verify(emailB, tokenA)
	r2 := verify("nobody-"+randomEmail(), tokenA)
	if r1.code != 400 || r2.code != 400 || r1.str("code") != "TOKEN_INVALID" || r1.str("code") != r2.str("code") || r1.str("detail") != r2.str("detail") {
		t.Fatalf("cross-address = %d %s / unknown = %d %s", r1.code, r1.raw, r2.code, r2.raw)
	}
	// B's real token still works after those failed attempts on A's code (attempts are per address).
	if n := e.query1(`SELECT attempts::text FROM auth_tokens WHERE email = $1 AND purpose = 'email_verify'`, emailB); n != "1" {
		t.Fatalf("B's attempts = %s", n)
	}

	// Five wrong codes burn the token: even the right code then fails.
	for i := range 5 {
		must(t, verify(emailA, wrong(tokenA)), 400, "wrong code")
		_ = i
	}
	must(t, verify(emailA, tokenA), 400, "correct code after five failures")
	if e.query1(`SELECT (email_verified_at IS NULL)::text FROM users WHERE email = $1`, emailA) != "true" {
		t.Fatal("email verified despite a burned token")
	}

	// B verifies once; the same code cannot be replayed.
	must(t, verify(emailB, tokenB), 200, "verify B")
	must(t, verify(emailB, tokenB), 400, "replay")

	// Expired tokens fail even with the right code.
	emailC, tokenC := register()
	e.exec(`UPDATE auth_tokens SET created_at = now() - interval '2 days', expires_at = now() - interval '1 day' WHERE email = $1 AND purpose = 'email_verify'`, emailC)
	must(t, verify(emailC, tokenC), 400, "expired")

	// A new code replaces the old one (resend), which then no longer works.
	emailD, tokenD := register()
	e.exec(`INSERT INTO auth_tokens (user_id, purpose, token_hash, email, expires_at)
	        SELECT user_id, purpose, sha256(convert_to(gen_random_uuid()::text, 'UTF8')), email, now() + interval '1 hour' FROM auth_tokens WHERE email = $1`, emailD)
	must(t, verify(emailD, tokenD), 400, "superseded code")
}
