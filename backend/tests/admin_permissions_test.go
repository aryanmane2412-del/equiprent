package tests

import (
	"net/http"
	"strings"
	"testing"
)

// Each admin write is an explicit permission: only the listed admin roles may use it, other
// administrators stay read-only, and non-admins are refused.
func TestAdminWritePermissionMatrix(t *testing.T) {
	e := newEnv(t)
	ops, support, kyc, fin, super := e.opsAdmin(), e.supportAdmin(), e.kycAdmin(), e.financeAdmin(), e.superAdmin()
	owner := e.verifiedOwner(kyc, "Perm Rentals "+randomLetters(4))
	target := e.registerAndVerify("customer", "")
	slug := "perm-cat-" + strings.ToLower(randomLetters(5))

	// Categories: admin_ops only.
	body := map[string]any{"slug": slug, "name_en": "Permission test category"}
	for name, u := range map[string]*user{"support": support, "kyc": kyc, "finance": fin, "owner": owner, "customer": target} {
		must(t, u.do(http.MethodPost, "/admin/categories", body), 403, name+" adds a category")
	}
	cat := must(t, ops.do(http.MethodPost, "/admin/categories", body), 201, "ops adds a category")
	must(t, support.do(http.MethodPut, "/admin/categories/"+cat.str("id"), map[string]any{"name_en": "Renamed", "is_active": false}), 403, "support edits a category")
	must(t, ops.do(http.MethodPut, "/admin/categories/"+cat.str("id"), map[string]any{"name_en": "Renamed", "is_active": false}), 200, "ops edits a category")
	if n := e.query1(`SELECT count(*) FROM audit_logs WHERE entity_type = 'equipment_categories' AND entity_id = $1 AND actor_user_id = $2`, cat.str("id"), ops.id); n == "0" {
		t.Fatal("category changes are not audited with the acting admin")
	}

	// Restrictions: ops and support; finance, kyc and non-admins are refused.
	rb := map[string]any{"kind": "new_bookings", "reason": "Repeated no-shows"}
	for name, u := range map[string]*user{"kyc": kyc, "finance": fin, "owner": owner, "customer": target} {
		must(t, u.do(http.MethodPost, "/admin/users/"+target.id+"/restrictions", rb), 403, name+" restricts")
	}
	r1 := must(t, support.do(http.MethodPost, "/admin/users/"+target.id+"/restrictions", rb), 201, "support restricts")
	for name, u := range map[string]*user{"kyc": kyc, "finance": fin, "customer": target} {
		must(t, u.do(http.MethodPost, "/admin/restrictions/"+r1.str("id")+"/lift", map[string]any{"reason": "Cleared"}), 403, name+" lifts")
	}
	must(t, ops.do(http.MethodPost, "/admin/restrictions/"+r1.str("id")+"/lift", map[string]any{"reason": "Cleared"}), 200, "ops lifts")
	if n := e.query1(`SELECT count(*) FROM audit_logs WHERE entity_type = 'account_restrictions' AND actor_user_id IN ($1, $2)`, support.id, ops.id); n == "0" {
		t.Fatal("restrictions are not audited with the acting admin")
	}

	// admin_super holds every platform role.
	must(t, super.do(http.MethodPost, "/admin/users/"+target.id+"/restrictions", rb), 201, "super restricts")
}
