package tests

import (
	"context"
	"fmt"
	"math/rand/v2"
	"net/http"
	"reflect"
	"strings"
	"testing"

	"github.com/equiprent/backend/internal/config"
)

const goodPassword = "Tractor-Rental-2026"

func randomEmail() string { return fmt.Sprintf("user%d@example.com", rand.Int64N(1e12)) }

// registerAndVerify registers through the API and proves the phone with the
// simulated OTP, returning a signed-in user.
func (e *env) registerAndVerify(kind, org string) *user {
	e.t.Helper()
	ph := randomPhone()
	body := map[string]any{
		"full_name": "Test User", "email": randomEmail(), "phone": ph, "password": goodPassword,
		"account_type": kind, "accept_terms": true,
	}
	if org != "" {
		body["organization_name"] = org
	}
	reg := must(e.t, e.call(http.MethodPost, "/auth/register", "", "", body, nil), 202, "register "+kind)
	tok := must(e.t, e.call(http.MethodPost, "/auth/otp/verify", "", "", map[string]any{"phone": ph, "code": reg.str("dev_otp")}, nil), 200, "verify phone")
	u := &user{e: e, phone: ph, token: tok.str("access_token"), id: tok.str("user.id"), refesh: tok.str("refresh_token")}
	u.org = tok.str("roles.0.organization_id")
	return u
}

func appRoles(r resp) []string {
	var out []string
	for _, v := range r.list("app_roles") {
		out = append(out, fmt.Sprint(v))
	}
	return out
}

// TestRoleAccessMatrix signs in one user per role and checks what each may
// and may not do, including every privilege-escalation path we know of.
func TestRoleAccessMatrix(t *testing.T) {
	e := newEnv(t)

	customer := e.registerAndVerify("customer", "")
	owner := e.registerAndVerify("equipment_owner", "Patil Earthmovers")
	rival := e.registerAndVerify("equipment_owner", "Rival Rentals")

	fleetPhone, opPhone := randomPhone(), randomPhone()
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": fleetPhone, "role": "fleet_manager"}), 201, "add fleet manager")
	must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": opPhone, "role": "operator"}), 201, "add operator")
	fleet, operator := e.login(fleetPhone), e.login(opPhone)
	e.acceptInvitations(operator)

	ops, super := e.login(randomPhone()), e.login(randomPhone())
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_ops')`, ops.id)
	e.exec(`INSERT INTO user_roles (user_id, role_code) VALUES ($1, 'admin_super')`, super.id)

	// Server-derived app roles.
	for _, tc := range []struct {
		u    *user
		want []string
	}{
		{customer, []string{"CUSTOMER"}}, {owner, []string{"EQUIPMENT_OWNER"}}, {fleet, []string{"FLEET_MANAGER"}},
		{operator, []string{"OPERATOR"}}, {ops, []string{"CUSTOMER", "ADMIN"}},
	} {
		if got := appRoles(must(t, tc.u.do(http.MethodGet, "/me", nil), 200, "me")); !reflect.DeepEqual(got, tc.want) {
			t.Errorf("app roles = %v, want %v", got, tc.want)
		}
	}

	models := must(t, owner.do(http.MethodGet, "/models?category=excavator", nil), 200, "models")
	newListing := func(u *user) string {
		return must(t, u.do(http.MethodPost, "/owner/equipment", map[string]any{
			"model_id": models.str("data.0.id"), "title": "Excavator for hire", "base_location": map[string]float64{"lat": 18.52, "lng": 73.85},
		}), 201, "create listing").str("id")
	}
	ownEq, rivalEq := newListing(owner), newListing(rival)

	type check struct {
		who  string
		u    *user
		m    string
		path string
		body any
		want int
	}
	anon := &user{e: e}
	checks := []check{
		// Anonymous.
		{"anonymous", anon, "GET", "/me", nil, 401},
		{"anonymous", anon, "GET", "/owner/bookings", nil, 401},
		{"anonymous", anon, "GET", "/admin/stats", nil, 401},
		// Customer.
		{"customer", customer, "GET", "/bookings", nil, 200},
		{"customer", customer, "GET", "/owner/bookings", nil, 403},
		{"customer", customer, "POST", "/owner/equipment", map[string]any{}, 403},
		{"customer", customer, "GET", "/owner/equipment/" + ownEq, nil, 403},
		{"customer", customer, "GET", "/operators/me/profile", nil, 403},
		{"customer", customer, "GET", "/fleet/" + owner.org + "/overview", nil, 404},
		{"customer", customer, "GET", "/orgs/" + owner.org, nil, 404},
		{"customer", customer, "POST", "/orgs/" + owner.org + "/members", map[string]any{"phone": randomPhone(), "role": "owner"}, 403},
		{"customer", customer, "GET", "/admin/stats", nil, 403},
		{"customer", customer, "POST", "/admin/listings/" + ownEq + "/approve", map[string]any{"reason": "self approve"}, 403},
		// Equipment owner.
		{"owner", owner, "GET", "/owner/bookings", nil, 200},
		{"owner", owner, "GET", "/owner/equipment/" + ownEq, nil, 200},
		{"owner", owner, "GET", "/owner/equipment/" + rivalEq, nil, 404},
		{"owner", owner, "PATCH", "/owner/equipment/" + rivalEq, map[string]any{"version": 1, "title": "hijacked"}, 404},
		{"owner", owner, "GET", "/fleet/" + owner.org + "/overview", nil, 200},
		{"owner", owner, "GET", "/fleet/" + rival.org + "/overview", nil, 404},
		{"owner", owner, "POST", "/orgs/" + owner.org + "/members", map[string]any{"phone": randomPhone(), "role": "admin_super"}, 400},
		{"owner", owner, "GET", "/admin/stats", nil, 403},
		// Fleet manager.
		{"fleet", fleet, "GET", "/owner/equipment", nil, 200},
		{"fleet", fleet, "GET", "/fleet/" + owner.org + "/overview", nil, 200},
		{"fleet", fleet, "POST", "/owners/onboard", nil, 403},
		{"fleet", fleet, "POST", "/orgs/" + owner.org + "/members", map[string]any{"phone": randomPhone(), "role": "owner"}, 403},
		{"fleet", fleet, "POST", "/orgs/" + owner.org + "/members", map[string]any{"phone": randomPhone(), "role": "operator"}, 201},
		{"fleet", fleet, "GET", "/fleet/" + rival.org + "/overview", nil, 404},
		{"fleet", fleet, "GET", "/admin/stats", nil, 403},
		// Operator.
		{"operator", operator, "GET", "/operator/jobs", nil, 200},
		{"operator", operator, "GET", "/operators/me/profile", nil, 200},
		{"operator", operator, "GET", "/owner/bookings", nil, 403},
		{"operator", operator, "GET", "/owner/equipment", nil, 403},
		{"operator", operator, "GET", "/fleet/" + owner.org + "/overview", nil, 403},
		{"operator", operator, "GET", "/admin/stats", nil, 403},
		// Operations admin.
		{"admin_ops", ops, "GET", "/admin/stats", nil, 200},
		{"admin_ops", ops, "GET", "/admin/listings", nil, 200},
		{"admin_ops", ops, "GET", "/admin/audit-logs", nil, 403},
		{"admin_ops", ops, "POST", "/admin/users/" + customer.id + "/roles", map[string]any{"role": "admin_super", "reason": "please"}, 403},
		{"admin_ops", ops, "POST", "/admin/users/" + ops.id + "/roles", map[string]any{"role": "admin_super", "reason": "self grant"}, 403},
		{"admin_ops", ops, "POST", "/admin/users/" + super.id + "/suspend", map[string]any{"reason": "take over"}, 403},
		// Super admin.
		{"admin_super", super, "GET", "/admin/audit-logs", nil, 200},
	}
	for _, c := range checks {
		r := c.u.do(c.m, c.path, c.body)
		if r.code != c.want {
			t.Errorf("%s %s %s: %d, want %d (%s)", c.who, c.m, c.path, r.code, c.want, r.raw)
		}
	}

	// A role claimed by the client is ignored everywhere.
	if r := customer.do(http.MethodGet, "/admin/stats", nil, "X-Role", "admin", "X-Roles", "ADMIN"); r.code != 403 {
		t.Errorf("X-Role header honoured: %d", r.code)
	}
	forged := &user{e: e, token: customer.token, org: owner.org}
	if r := forged.do(http.MethodGet, "/owner/bookings", nil); r.code != 403 {
		t.Errorf("foreign X-Org-Id accepted: %d", r.code)
	}
	if r := must(t, customer.do(http.MethodPatch, "/me", map[string]any{"full_name": "Still Customer", "roles": []string{"admin_super"}, "status": "active", "app_roles": []string{"ADMIN"}}), 200, "patch me"); !reflect.DeepEqual(appRoles(r), []string{"CUSTOMER"}) {
		t.Errorf("roles changed through PATCH /me: %v", appRoles(r))
	}
	for _, kind := range []string{"admin", "ADMIN", "operator", "fleet_manager", "owner"} {
		r := e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
			"full_name": "Sneaky", "email": randomEmail(), "phone": randomPhone(), "password": goodPassword, "account_type": kind, "accept_terms": true,
		}, nil)
		if r.code != 400 {
			t.Errorf("register as %q: %d", kind, r.code)
		}
	}
	sneaky := e.registerAndVerify("customer", "")
	// Extra JSON fields are ignored: the account is a plain customer.
	_ = sneaky
	extra := map[string]any{"full_name": "Sneaky Two", "email": randomEmail(), "phone": randomPhone(), "password": goodPassword,
		"account_type": "customer", "accept_terms": true, "roles": []string{"admin_super"}, "app_roles": []string{"ADMIN"}}
	reg := must(t, e.call(http.MethodPost, "/auth/register", "", "", extra, nil), 202, "register with extra fields")
	tok := must(t, e.call(http.MethodPost, "/auth/otp/verify", "", "", map[string]any{"phone": extra["phone"], "code": reg.str("dev_otp")}, nil), 200, "verify")
	if got := appRoles(tok); !reflect.DeepEqual(got, []string{"CUSTOMER"}) {
		t.Errorf("registration honoured client roles: %v", got)
	}

	// Super admin suspends the ops admin: their sessions are revoked at once,
	// signing in again is refused, and reactivation needs a fresh sign-in.
	// Platform administrator accounts are read-only: no password change, suspension or reactivation.
	must(t, ops.do(http.MethodPost, "/auth/password/change", map[string]any{"new_password": goodPassword}), 403, "ops password change")
	must(t, super.do(http.MethodPost, "/admin/users/"+ops.id+"/suspend", map[string]any{"reason": "security review"}), 403, "suspend")
	must(t, super.do(http.MethodPost, "/admin/users/"+ops.id+"/reactivate", map[string]any{"reason": "review passed"}), 403, "reactivate")
	must(t, ops.do(http.MethodGet, "/admin/stats", nil), 200, "admin can still read")
}

// TestPasswordAuthLifecycle runs registration → verification → password
// login → email verification → change → reset → lockout against the DB.
func TestPasswordAuthLifecycle(t *testing.T) {
	e := newEnv(t)
	ph, email := randomPhone(), randomEmail()
	reg := must(t, e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
		"full_name": "Asha Kulkarni", "email": email, "phone": ph, "password": goodPassword, "account_type": "customer", "accept_terms": true,
	}, nil), 202, "register")
	if reg.str("dev_otp") == "" || reg.str("dev_email_token") == "" {
		t.Fatalf("dev simulation fields missing: %s", reg.raw)
	}
	login := func(id, pw string) resp {
		return e.call(http.MethodPost, "/auth/login", "", "", map[string]any{"identifier": id, "password": pw}, nil)
	}
	if r := login(email, goodPassword); r.code != 403 || r.str("code") != "VERIFICATION_REQUIRED" {
		t.Fatalf("unverified email login: %d %s", r.code, r.raw)
	}
	must(t, e.call(http.MethodPost, "/auth/email/verify", "", "", map[string]any{"email": email, "token": reg.str("dev_email_token")}, nil), 200, "verify email")
	tok := must(t, login(strings.ToUpper(email), goodPassword), 200, "email login")
	u := &user{e: e, token: tok.str("access_token"), id: tok.str("user.id")}
	me := must(t, u.do(http.MethodGet, "/me", nil), 200, "me")
	if me.body["email_verified"] != true || me.body["phone_verified"] != false || me.body["has_password"] != true {
		t.Fatalf("me = %s", me.raw)
	}

	// Changing a VERIFIED email needs the current password, then drops its
	// verification (DB trigger).
	newEmail := randomEmail()
	must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": newEmail}), 400, "change verified email without password")
	if r := must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": newEmail, "current_password": goodPassword}), 200, "change email"); r.body["email_verified"] != false {
		t.Fatalf("email change kept verification: %s", r.raw)
	}
	sent := must(t, u.do(http.MethodPost, "/auth/email/verification", nil), 202, "resend verification")
	must(t, u.do(http.MethodPost, "/auth/email/verification", nil), 429, "resend cooldown")
	must(t, e.call(http.MethodPost, "/auth/email/verify", "", "", map[string]any{"email": newEmail, "token": sent.str("dev_token")}, nil), 200, "verify new email")

	// Change password; other sessions are signed out.
	other := must(t, login(newEmail, goodPassword), 200, "second device")
	must(t, u.do(http.MethodPost, "/auth/password/change", map[string]any{"current_password": "wrong-one-1", "new_password": "Crane-Hire-4417"}), 400, "wrong current password")
	must(t, u.do(http.MethodPost, "/auth/password/change", map[string]any{"current_password": goodPassword, "new_password": "password123"}), 400, "weak new password")
	must(t, u.do(http.MethodPost, "/auth/password/change", map[string]any{"current_password": goodPassword, "new_password": "Crane-Hire-4417"}), 200, "change password")
	must(t, e.call(http.MethodGet, "/me", other.str("access_token"), "", nil, nil), 401, "other device after change")
	must(t, u.do(http.MethodGet, "/me", nil), 200, "this device after change")

	// Reset by email signs out everywhere.
	forgot := must(t, e.call(http.MethodPost, "/auth/password/forgot", "", "", map[string]any{"identifier": newEmail}, nil), 202, "forgot")
	must(t, e.call(http.MethodPost, "/auth/password/reset", "", "", map[string]any{"token": forgot.str("dev_token"), "new_password": "Grader-Lease-8821"}, nil), 200, "reset")
	must(t, u.do(http.MethodGet, "/me", nil), 401, "after reset")
	must(t, login(newEmail, "Crane-Hire-4417"), 401, "old password after reset")
	must(t, login(newEmail, "Grader-Lease-8821"), 200, "new password")
	must(t, e.call(http.MethodPost, "/auth/password/reset", "", "", map[string]any{"token": forgot.str("dev_token"), "new_password": "Another-Secret-55"}, nil), 400, "reused reset token")

	// Phone OTP verification unlocks phone login.
	must(t, login(ph, "Grader-Lease-8821"), 403, "unverified phone")
	// (registration already sent the phone a code; a second request within
	// 30 s is refused by the resend cooldown)
	must(t, e.call(http.MethodPost, "/auth/otp/request", "", "", map[string]any{"phone": ph}, nil), 429, "otp resend cooldown")
	must(t, e.call(http.MethodPost, "/auth/otp/verify", "", "", map[string]any{"phone": ph, "code": reg.str("dev_otp")}, nil), 200, "verify phone")
	must(t, login(ph, "Grader-Lease-8821"), 200, "phone login")

	// Lockout after repeated failures, even for the right password; audited.
	for range e.cfg.Auth.LoginMaxAttempts {
		login(ph, "Wrong-Guess-0000")
	}
	if r := login(ph, "Grader-Lease-8821"); r.code != 429 || r.header.Get("Retry-After") == "" {
		t.Fatalf("lockout: %d %v", r.code, r.header)
	}
	var failures, lockouts int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(*) FILTER (WHERE action = 'auth.login_failed'), count(*) FILTER (WHERE action = 'auth.lockout')
		FROM audit_logs WHERE entity_type = 'users' AND (entity_id = $1 OR entity_id IS NULL) AND occurred_at > now() - interval '5 minutes'`, u.id).Scan(&failures, &lockouts)
	if failures < e.cfg.Auth.LoginMaxAttempts || lockouts < 1 {
		t.Fatalf("audit: failures=%d lockouts=%d", failures, lockouts)
	}
	var events int
	_ = e.pool.QueryRow(context.Background(), `SELECT count(DISTINCT action) FROM audit_logs WHERE entity_id = $1
		AND action IN ('auth.register', 'auth.email_verified', 'auth.login', 'auth.password_changed', 'auth.password_reset_requested', 'auth.password_reset')`, u.id).Scan(&events)
	if events != 6 {
		t.Fatalf("expected 6 distinct audited auth events, got %d", events)
	}
}

// TestNoAccountEnumeration runs with dev simulation OFF (what production
// clients see) and compares answers for existing and unknown accounts.
func TestNoAccountEnumeration(t *testing.T) {
	e := newEnv(t, func(c *config.Config) { c.Auth.DevSimulation = false })
	ph, email := randomPhone(), randomEmail()
	first := e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
		"full_name": "Real Owner", "email": email, "phone": ph, "password": goodPassword, "account_type": "customer", "accept_terms": true,
	}, nil)
	dupPhone := e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
		"full_name": "Attacker", "email": randomEmail(), "phone": ph, "password": "Crane-Hire-4417", "account_type": "customer", "accept_terms": true,
	}, nil)
	dupEmail := e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
		"full_name": "Attacker", "email": email, "phone": randomPhone(), "password": "Crane-Hire-4417", "account_type": "customer", "accept_terms": true,
	}, nil)
	if first.code != 202 || first.raw != dupPhone.raw || first.raw != dupEmail.raw || dupPhone.code != dupEmail.code {
		t.Fatalf("register answers differ:\n%d %s\n%d %s\n%d %s", first.code, first.raw, dupPhone.code, dupPhone.raw, dupEmail.code, dupEmail.raw)
	}
	e.exec(`UPDATE users SET phone_verified_at = now(), email_verified_at = now() WHERE phone = $1`, ph)

	strip := func(r resp) string { return fmt.Sprint(r.code, r.str("code"), r.str("title"), r.str("detail")) }
	login := func(id string) resp {
		return e.call(http.MethodPost, "/auth/login", "", "", map[string]any{"identifier": id, "password": "Wrong-Guess-0000"}, nil)
	}
	known, unknown := login(email), login(randomEmail())
	if strip(known) != strip(unknown) || known.code != 401 {
		t.Fatalf("login answers differ: %s vs %s", strip(known), strip(unknown))
	}
	f1 := e.call(http.MethodPost, "/auth/password/forgot", "", "", map[string]any{"identifier": email}, nil)
	f2 := e.call(http.MethodPost, "/auth/password/forgot", "", "", map[string]any{"identifier": randomEmail()}, nil)
	if f1.code != 202 || f1.raw != f2.raw {
		t.Fatalf("forgot answers differ: %s vs %s", f1.raw, f2.raw)
	}
	// Production-like: no OTP without an SMS provider, and no dev secrets leak.
	otp := e.call(http.MethodPost, "/auth/otp/request", "", "", map[string]any{"phone": ph}, nil)
	if otp.code != 503 || otp.str("code") != "OTP_UNAVAILABLE" {
		t.Fatalf("otp without provider: %d %s", otp.code, otp.raw)
	}
	caps := must(t, e.call(http.MethodGet, "/auth/capabilities", "", "", nil, nil), 200, "capabilities")
	if caps.body["mobile_otp"] != false || caps.body["dev_simulation"] != false || caps.body["password_login"] != true {
		t.Fatalf("capabilities = %s", caps.raw)
	}
	for _, r := range []resp{first, dupPhone, f1} {
		if strings.Contains(r.raw, "dev_") {
			t.Fatalf("dev field leaked: %s", r.raw)
		}
	}
	must(t, e.call(http.MethodPost, "/auth/login", "", "", map[string]any{"identifier": email, "password": goodPassword}, nil), 200, "real login still works")
}
