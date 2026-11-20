package httpx

import (
	"net/http"
	"slices"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/apidoc"
	"github.com/equiprent/backend/internal/apperror"
)

// Doc documents a route; see apidoc.Op.
type Doc = apidoc.Op

// Guards are the cross-cutting middlewares modules attach to their routes.
// Unset guards are skipped (useful in unit tests).
type Guards struct {
	// Authenticate requires a valid bearer token and sets the Principal.
	Authenticate gin.HandlerFunc
	// Idempotency replays responses for repeated Idempotency-Key requests.
	Idempotency gin.HandlerFunc
	// RequirePlatformRole allows only users with one of the platform roles.
	RequirePlatformRole func(roles ...string) gin.HandlerFunc
	// EquipmentAccess runs on every authenticated route under
	// /owner/equipment/:id, so fleet managers can only reach machines an
	// owner authorised them for, whichever module serves the route.
	EquipmentAccess gin.HandlerFunc
}

// EquipmentScopedPrefix marks the owner-side machine routes guarded by
// Guards.EquipmentAccess.
const EquipmentScopedPrefix = "/owner/equipment/:id"

// Module is implemented by every feature module.
type Module interface {
	// Name is the module (and OpenAPI tag) name.
	Name() string
	// Routes registers the module's endpoints.
	Routes(r *Router)
}

// Router registers documented routes on a Gin group. Every route registered
// through it is added to the OpenAPI spec, which keeps docs and code in sync.
type Router struct {
	group  *gin.RouterGroup
	spec   *apidoc.Spec
	guards Guards
	tag    string
	auth   bool
	// adminOnly requires a platform role: one of admin, or admin_super
	// alone when admin is empty.
	adminOnly       bool
	admin           []string
	adminWritePaths map[string]struct{}
	extra           []gin.HandlerFunc
}

// NewRouter wraps group. spec may be nil (docs disabled).
func NewRouter(group *gin.RouterGroup, spec *apidoc.Spec, guards Guards) *Router {
	SetupValidator()
	return &Router{group: group, spec: spec, guards: guards}
}

func (r *Router) clone() *Router {
	c := *r
	c.extra = slices.Clone(r.extra)
	c.admin = slices.Clone(r.admin)
	if r.adminWritePaths != nil {
		c.adminWritePaths = make(map[string]struct{}, len(r.adminWritePaths))
		for path := range r.adminWritePaths {
			c.adminWritePaths[path] = struct{}{}
		}
	}
	return &c
}

// Tag returns a router whose routes are grouped under tag in the docs.
func (r *Router) Tag(tag string) *Router { c := r.clone(); c.tag = tag; return c }

// Authed returns a router whose routes require authentication.
func (r *Router) Authed() *Router { c := r.clone(); c.auth = true; return c }

// Admin returns a router whose routes require one of the platform roles
// (admin_super always passes). With no roles, only admin_super may call.
func (r *Router) Admin(roles ...string) *Router {
	c := r.clone()
	c.auth = true
	c.adminOnly = true
	c.admin = roles
	return c
}

// AdminWrites explicitly opts an admin router into only the named write
// routes. The platform-role guard remains mandatory.
func (r *Router) AdminWrites(paths ...string) *Router {
	c := r.clone()
	if c.adminWritePaths == nil {
		c.adminWritePaths = make(map[string]struct{})
	}
	for _, path := range paths {
		c.adminWritePaths[path] = struct{}{}
	}
	return c
}

func (r *Router) allowsAdminWrite(method, path string) bool {
	_, ok := r.adminWritePaths[method+" "+path]
	return ok
}

// Use returns a router that runs mw before the handlers of its routes.
func (r *Router) Use(mw ...gin.HandlerFunc) *Router {
	c := r.clone()
	c.extra = append(c.extra, mw...)
	return c
}

func (r *Router) GET(path string, h gin.HandlerFunc, doc Doc) {
	r.Handle(http.MethodGet, path, h, doc)
}
func (r *Router) POST(path string, h gin.HandlerFunc, doc Doc) {
	r.Handle(http.MethodPost, path, h, doc)
}
func (r *Router) PUT(path string, h gin.HandlerFunc, doc Doc) {
	r.Handle(http.MethodPut, path, h, doc)
}
func (r *Router) PATCH(path string, h gin.HandlerFunc, doc Doc) {
	r.Handle(http.MethodPatch, path, h, doc)
}
func (r *Router) DELETE(path string, h gin.HandlerFunc, doc Doc) {
	r.Handle(http.MethodDelete, path, h, doc)
}

// Handle registers the route and documents it.
func (r *Router) Handle(method, path string, h gin.HandlerFunc, doc Doc) {
	adminWrite := r.adminOnly && r.allowsAdminWrite(method, path)
	var chain []gin.HandlerFunc
	if r.auth && r.guards.Authenticate != nil {
		chain = append(chain, r.guards.Authenticate)
	}
	if r.auth && !adminWrite && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions && path != "/auth/logout" {
		chain = append(chain, func(c *gin.Context) {
			p, ok := MustPrincipal(c)
			if !ok {
				c.Abort()
				return
			}
			if p.IsAdmin() {
				c.Status(http.StatusForbidden)
				Fail(c, apperror.ErrForbidden.WithDetail("platform administrator accounts are read-only"))
				return
			}
			c.Next()
		})
	}
	if r.adminOnly {
		if r.guards.RequirePlatformRole == nil {
			panic("httpx: admin route " + path + " registered without a RequirePlatformRole guard")
		}
		chain = append(chain, r.guards.RequirePlatformRole(r.admin...))
		if !adminWrite && method != http.MethodGet && method != http.MethodHead && method != http.MethodOptions {
			chain = append(chain, func(c *gin.Context) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
					"type": "about:blank", "title": "Forbidden", "status": http.StatusForbidden,
					"code": "FORBIDDEN", "detail": "platform administrator access is read-only",
				})
			})
		}
	}
	if r.auth && r.guards.EquipmentAccess != nil && strings.HasPrefix(path, EquipmentScopedPrefix) {
		chain = append(chain, r.guards.EquipmentAccess)
	}
	if doc.Idempotent && r.guards.Idempotency != nil {
		chain = append(chain, r.guards.Idempotency)
	}
	chain = append(chain, r.extra...)
	chain = append(chain, h)
	r.group.Handle(method, path, chain...)

	if r.spec != nil {
		doc.Public = !r.auth
		if r.tag != "" && len(doc.Tags) == 0 {
			doc.Tags = []string{r.tag}
		}
		r.spec.Add(method, r.group.BasePath()+path, doc)
	}
}
