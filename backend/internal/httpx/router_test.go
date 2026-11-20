package httpx

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

// Every authenticated per-machine owner route gets the equipment guard,
// whichever module registers it; other routes do not.
func TestEquipmentGuardAttachedByPrefix(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	guarded := map[string]bool{}
	r := NewRouter(engine.Group("/api/v1"), nil, Guards{
		Authenticate:    func(c *gin.Context) { c.Next() },
		EquipmentAccess: func(c *gin.Context) { guarded[c.FullPath()] = true; c.Next() },
	})
	ok := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	a := r.Authed()
	a.GET("/owner/equipment/:id", ok, Doc{})
	a.GET("/owner/equipment/:id/calendar", ok, Doc{})
	a.GET("/owner/equipment/:id/maintenance/plans", ok, Doc{})
	a.GET("/owner/equipment", ok, Doc{})
	a.GET("/owner/bookings/:id", ok, Doc{})
	r.GET("/equipment/:id", ok, Doc{}) // public: no principal, no guard

	for _, p := range []string{"/owner/equipment/x", "/owner/equipment/x/calendar", "/owner/equipment/x/maintenance/plans",
		"/owner/equipment", "/owner/bookings/x", "/equipment/x"} {
		engine.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1"+p, nil))
	}
	want := map[string]bool{
		"/api/v1/owner/equipment/:id": true, "/api/v1/owner/equipment/:id/calendar": true,
		"/api/v1/owner/equipment/:id/maintenance/plans": true,
	}
	if len(guarded) != len(want) {
		t.Fatalf("guarded = %v", guarded)
	}
	for p := range want {
		if !guarded[p] {
			t.Errorf("%s is not guarded", p)
		}
	}
}

func TestAdminRoutesAreReadOnly(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	r := NewRouter(engine.Group("/api/v1"), nil, Guards{
		Authenticate: func(c *gin.Context) {
			SetPrincipal(c, &Principal{Roles: []Role{{Code: RoleAdminOps}}})
			c.Next()
		},
		RequirePlatformRole: func(...string) gin.HandlerFunc {
			return func(c *gin.Context) { c.Next() }
		},
	})
	admin := r.Admin()
	handler := func(c *gin.Context) { c.Status(http.StatusNoContent) }
	admin.GET("/admin/report", handler, Doc{})
	admin.POST("/admin/report", handler, Doc{})
	review := admin.AdminWrites("POST /admin/review")
	review.POST("/admin/review", handler, Doc{})
	review.POST("/admin/unlisted", handler, Doc{})

	get := httptest.NewRecorder()
	engine.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/api/v1/admin/report", nil))
	if get.Code != http.StatusNoContent {
		t.Fatalf("GET admin report status = %d", get.Code)
	}
	post := httptest.NewRecorder()
	engine.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/api/v1/admin/report", nil))
	if post.Code != http.StatusForbidden {
		t.Fatalf("POST admin report status = %d, want %d", post.Code, http.StatusForbidden)
	}
	reviewResponse := httptest.NewRecorder()
	engine.ServeHTTP(reviewResponse, httptest.NewRequest(http.MethodPost, "/api/v1/admin/review", nil))
	if reviewResponse.Code != http.StatusNoContent {
		t.Fatalf("opted-in review POST status = %d, want %d", reviewResponse.Code, http.StatusNoContent)
	}
	unlisted := httptest.NewRecorder()
	engine.ServeHTTP(unlisted, httptest.NewRequest(http.MethodPost, "/api/v1/admin/unlisted", nil))
	if unlisted.Code != http.StatusForbidden {
		t.Fatalf("unlisted admin POST status = %d, want %d", unlisted.Code, http.StatusForbidden)
	}
}

func TestAdminWriteOptInStillRequiresPlatformRole(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	r := NewRouter(engine.Group("/api/v1"), nil, Guards{
		Authenticate: func(c *gin.Context) { SetPrincipal(c, &Principal{Roles: []Role{{Code: RoleAdminSupport}}}); c.Next() },
		RequirePlatformRole: func(...string) gin.HandlerFunc {
			return func(c *gin.Context) { c.AbortWithStatus(http.StatusForbidden) }
		},
	})
	r.Admin(RoleAdminKYC).AdminWrites("POST /admin/review").POST("/admin/review", func(c *gin.Context) { c.Status(http.StatusNoContent) }, Doc{})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/admin/review", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("unauthorized admin write status = %d, want 403", rec.Code)
	}
}

func TestPlatformAdminCannotWriteThroughOtherAuthenticatedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()
	r := NewRouter(engine.Group("/api/v1"), nil, Guards{
		Authenticate: func(c *gin.Context) {
			SetPrincipal(c, &Principal{Roles: []Role{{Code: RoleAdminOps}}})
			c.Next()
		},
	})
	r.Authed().POST("/owner/equipment", func(c *gin.Context) { c.Status(http.StatusCreated) }, Doc{})
	rec := httptest.NewRecorder()
	engine.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/v1/owner/equipment", nil))
	if rec.Code != http.StatusForbidden {
		t.Fatalf("admin write status = %d, want %d", rec.Code, http.StatusForbidden)
	}
}
