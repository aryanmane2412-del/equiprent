package httpx

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
)

func TestKeysetRoundTrip(t *testing.T) {
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	k, err := PageQuery{Limit: 2}.Keyset()
	if err != nil || k.Fetch() != 3 {
		t.Fatalf("keyset = %+v %v", k, err)
	}
	page := Paginate(ids, k, func(id uuid.UUID) uuid.UUID { return id })
	if len(page.Data) != 2 || page.NextCursor == "" {
		t.Fatalf("page = %+v", page)
	}
	next, err := PageQuery{Limit: 2, Cursor: page.NextCursor}.Keyset()
	if err != nil || !next.After.Valid || next.After.UUID != ids[1] {
		t.Fatalf("cursor decodes to %+v %v", next, err)
	}
	last := Paginate(ids[:1], k, func(id uuid.UUID) uuid.UUID { return id })
	if last.NextCursor != "" {
		t.Fatal("last page must not have a cursor")
	}
	if empty := Paginate[uuid.UUID](nil, k, nil); empty.Data == nil {
		t.Fatal("empty pages serialise as []")
	}
	if _, err := (PageQuery{Cursor: "%%%"}).Keyset(); err == nil {
		t.Fatal("garbage cursor accepted")
	}
	if k, _ := (PageQuery{Limit: 1000}).Keyset(); k.Limit != MaxPageSize {
		t.Fatalf("limit not capped: %d", k.Limit)
	}
}

func TestPrincipalRoles(t *testing.T) {
	a, b := uuid.New(), uuid.New()
	p := &Principal{Roles: []Role{{Code: RoleOrgAdmin, OrgID: a}, {Code: RoleOperator, OrgID: b}}}
	if !p.HasOrgRole(a, RoleCustomer) || !p.HasOrgRole(a, RoleOrgAdmin) || p.HasOrgRole(b, RoleOwner) || !p.HasOrgRole(b, RoleOperator) {
		t.Error("org_admin implies customer; other roles are exact")
	}
	if p.HasOrgRole(a, RoleOwner) || p.HasOrgRole(a, RoleFleetManager, RoleOperator) {
		t.Error("org_admin must not imply supplier roles (privilege escalation)")
	}
	if p.IsAdmin() || p.HasPlatformRole(RoleAdminOps) {
		t.Error("no platform roles")
	}
	if len(p.Orgs()) != 2 || !p.IsMember(a) || p.IsMember(uuid.New()) || p.IsMember(uuid.Nil) {
		t.Error("membership")
	}
	super := &Principal{Roles: []Role{{Code: RoleAdminSuper}}}
	if !super.HasPlatformRole(RoleAdminFinance) || !super.HasPlatformRole() || !super.IsAdmin() {
		t.Error("admin_super holds every platform role")
	}
	ops := &Principal{Roles: []Role{{Code: RoleAdminOps}}}
	if ops.HasPlatformRole() || !ops.HasPlatformRole(RoleAdminOps) {
		t.Error("only super passes an empty role list")
	}
}

func TestBindErrors(t *testing.T) {
	SetupValidator()
	gin.SetMode(gin.TestMode)
	type req struct {
		Phone string `json:"phone" binding:"required,e164"`
		Pin   string `json:"pin" binding:"omitempty,pincode"`
		Key   string `json:"key" binding:"omitempty,objkey"`
	}
	run := func(body string) *apperror.Error {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(body))
		c.Request.Header.Set("Content-Type", "application/json")
		var r req
		if BindJSON(c, &r) {
			return nil
		}
		return apperror.From(c.Errors.Last().Err)
	}
	if e := run(`{"phone":"+919876543210","pin":"411057","key":"equipment/a/b.jpg"}`); e != nil {
		t.Fatalf("valid body rejected: %v", e)
	}
	e := run(`{"phone":"98765","pin":"011057","key":"../etc/passwd"}`)
	if e == nil || e.Code != "VALIDATION_FAILED" || len(e.Fields) != 3 || e.Fields[0].Field != "phone" {
		t.Fatalf("field errors = %+v", e)
	}
	if e := run(`{"phone":`); e == nil || e.Code != "BAD_REQUEST" {
		t.Fatalf("malformed JSON = %+v", e)
	}
	if e := run(``); e == nil || !strings.Contains(e.Detail, "required") {
		t.Fatalf("empty body = %+v", e)
	}
	if e := run(`{"phone": 5}`); e == nil || e.Fields[0].Code != "type" {
		t.Fatalf("type error = %+v", e)
	}
}
