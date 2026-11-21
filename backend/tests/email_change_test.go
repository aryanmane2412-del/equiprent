package tests

import (
	"net/http"
	"strings"
	"testing"
)

// A stolen session must not be able to re-point account recovery: replacing a
// VERIFIED email needs the current password, and wrong guesses lock out.
func TestReplacingVerifiedEmailNeedsPassword(t *testing.T) {
	e := newEnv(t)
	u := e.registerAndVerify("customer", "")
	me := func() resp { return must(t, u.do(http.MethodGet, "/me", nil), 200, "me") }
	first := me().str("email")

	// An unverified address can still be corrected freely (onboarding).
	typo := strings.ToLower(randomEmail())
	must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": typo}), 200, "fix unverified email")

	// Once verified, replacing it requires the password.
	e.exec(`UPDATE users SET email_verified_at = now() WHERE id = $1`, u.id)
	other := strings.ToLower(randomEmail())
	must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": other}), 400, "no password")
	wrong := must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": other, "current_password": "Wrong-Password-9"}), 400, "wrong password")
	if wrong.str("code") != "CURRENT_PASSWORD_INCORRECT" {
		t.Fatalf("code = %s", wrong.str("code"))
	}
	if got := me().str("email"); got != typo {
		t.Fatalf("email changed without re-authentication: %s", got)
	}

	// The app resends the unchanged email on every save: no password needed.
	must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": strings.ToUpper(typo), "full_name": "Renamed User"}), 200, "unchanged email")

	// Correct password: the change goes through and must be re-verified.
	ok := must(t, u.do(http.MethodPatch, "/me", map[string]any{"email": other, "current_password": goodPassword}), 200, "correct password")
	if ok.str("email") != other || ok.str("email_verified") != "false" {
		t.Fatalf("after change: email=%s verified=%s (was %s)", ok.str("email"), ok.str("email_verified"), first)
	}

	// Guessing the password through this endpoint is locked out.
	e.exec(`UPDATE users SET email_verified_at = now() WHERE id = $1`, u.id)
	var last resp
	for range 12 {
		last = u.do(http.MethodPatch, "/me", map[string]any{"email": strings.ToLower(randomEmail()), "current_password": "Guess-Password-1"})
	}
	if last.code != http.StatusTooManyRequests {
		t.Fatalf("repeated wrong passwords: last status %d, want 429", last.code)
	}
}
