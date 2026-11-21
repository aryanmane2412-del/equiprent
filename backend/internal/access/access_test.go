package access

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/httpx"
)

// fakeEquipment owns machines by organisation, like the database does.
type fakeEquipment struct{ owner map[uuid.UUID]uuid.UUID }

func (f fakeEquipment) CanManage(_ context.Context, p *httpx.Principal, org, eq uuid.UUID) error {
	if !p.HasOrgRole(org, httpx.RoleOwner, httpx.RoleFleetManager) {
		return apperror.ErrForbidden
	}
	if f.owner[eq] != org {
		return apperror.ErrNotFound
	}
	return nil
}
func (fakeEquipment) Scope(context.Context, *httpx.Principal, uuid.UUID) (bool, map[uuid.UUID]bool, error) {
	return true, nil, nil
}
func (fakeEquipment) Grant(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error { return nil }
func (fakeEquipment) ManagementBlocked(context.Context, uuid.UUID) (bool, error)   { return false, nil }

func call(t *testing.T, eq Equipment, p *httpx.Principal, machine uuid.UUID) int {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(func(c *gin.Context) {
		httpx.SetPrincipal(c, p)
		c.Next()
		if len(c.Errors) > 0 { // what the real error middleware does
			c.Status(apperror.From(c.Errors.Last().Err).Status)
		}
	})
	r.GET("/machines/:id", Guard(eq), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/machines/"+machine.String(), nil)
	r.ServeHTTP(w, req)
	return w.Code
}

func TestGuardRefusesAnotherOrganisationsMachine(t *testing.T) {
	orgA, orgB := uuid.New(), uuid.New()
	machineA, machineB := uuid.New(), uuid.New()
	eq := fakeEquipment{owner: map[uuid.UUID]uuid.UUID{machineA: orgA, machineB: orgB}}
	ownerA := &httpx.Principal{UserID: uuid.New(), ActingOrg: orgA, Roles: []httpx.Role{{Code: httpx.RoleOwner, OrgID: orgA}}}

	if got := call(t, eq, ownerA, machineA); got != http.StatusNoContent {
		t.Fatalf("own machine = %d", got)
	}
	if got := call(t, eq, ownerA, machineB); got != http.StatusNotFound {
		t.Fatalf("another organisation's machine = %d, want 404", got)
	}
}

func TestGuardLetsNonOwnerRolesFallThrough(t *testing.T) {
	org, machine := uuid.New(), uuid.New()
	eq := fakeEquipment{owner: map[uuid.UUID]uuid.UUID{}}
	customer := &httpx.Principal{UserID: uuid.New(), ActingOrg: org, Roles: []httpx.Role{{Code: httpx.RoleCustomer, OrgID: org}}}
	// The handler's own role check answers; the guard does not widen access.
	if got := call(t, eq, customer, machine); got != http.StatusNoContent {
		t.Fatalf("customer = %d", got)
	}
}

func TestFilterReturnsNilWhenAllMachinesVisible(t *testing.T) {
	ids, err := Filter(context.Background(), fakeEquipment{}, &httpx.Principal{}, uuid.New())
	if err != nil || ids != nil {
		t.Fatalf("filter = %v, %v", ids, err)
	}
}
