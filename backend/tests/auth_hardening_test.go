package tests

import (
	"net/http"
	"strings"
	"testing"
)

// Expired and used verification tokens are removed by the cleanup job; live ones stay.
func TestExpiredEmailTokensAreCleanedUp(t *testing.T) {
	e := newEnv(t)
	live, old := randomEmail(), randomEmail()
	for _, em := range []string{live, old} {
		must(t, e.call(http.MethodPost, "/auth/register", "", "", map[string]any{
			"full_name": "Asha Kulkarni", "email": em, "phone": randomPhone(), "password": goodPassword, "account_type": "customer", "accept_terms": true,
		}, nil), 202, "register")
	}
	e.exec(`UPDATE auth_tokens SET created_at = now() - interval '5 days', expires_at = now() - interval '4 days' WHERE email = $1`, old)
	e.runJob(t, "cleanup.expired_keys")
	if n := e.query1(`SELECT count(*) FROM auth_tokens WHERE email = $1`, old); n != "0" {
		t.Fatalf("expired token rows = %s", n)
	}
	if n := e.query1(`SELECT count(*) FROM auth_tokens WHERE email = $1`, live); n != "1" {
		t.Fatalf("live token rows = %s", n)
	}
}

// Inviting an existing account and an unknown number answer the same way and never reveal the
// stored name of a registered user.
func TestInvitationResponsesDoNotRevealRegisteredUsers(t *testing.T) {
	e := newEnv(t)
	owner := e.verifiedOwner(e.kycAdmin(), "Reveal Rentals "+randomLetters(4))
	existing := e.registerAndVerify("customer", "")
	e.exec(`UPDATE users SET full_name = 'Priya Secret Name' WHERE id = $1`, existing.id)
	phone := e.query1(`SELECT phone FROM users WHERE id = $1`, existing.id)

	for _, role := range []string{"operator", "fleet_manager"} {
		known := must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": phone, "role": role, "full_name": "Typed Name"}), 201, "invite existing")
		unknown := must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": randomPhone(), "role": role, "full_name": "Typed Name"}), 201, "invite unknown")
		if strings.Contains(known.raw, "Priya") || known.str("full_name") != "Typed Name" || unknown.str("full_name") != "Typed Name" {
			t.Fatalf("%s: existing = %s", role, known.raw)
		}
		for k := range known.body {
			if _, ok := unknown.body[k]; !ok {
				t.Fatalf("%s: field %q differs between existing and unknown numbers", role, k)
			}
		}
		// No name supplied: nothing from the stored account is echoed either.
		again := must(t, owner.do(http.MethodPost, "/orgs/"+owner.org+"/members", map[string]any{"phone": phone, "role": "customer"}), 201, "add without a name")
		if strings.Contains(again.raw, "Priya") {
			t.Fatalf("stored name leaked: %s", again.raw)
		}
		break
	}
}
