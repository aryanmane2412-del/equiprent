package discovery

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/equiprent/backend/internal/httpx"
)

// Handler exposes the discovery API.
type Handler struct {
	svc      *Service
	optional gin.HandlerFunc
}

// NewHandler builds the handler. optionalAuth identifies signed-in users on
// public routes (to record search history) and never rejects.
func NewHandler(svc *Service, optionalAuth gin.HandlerFunc) *Handler {
	return &Handler{svc: svc, optional: optionalAuth}
}

func (h *Handler) Name() string { return "discovery" }

type suggestQuery struct {
	Q string `form:"q" binding:"omitempty,max=80"`
}

type compareQuery struct {
	IDs string `form:"ids" binding:"required,max=200" doc:"2 to 4 listing ids, comma separated"`
}

type reverseLocationQuery struct {
	Lat float64 `form:"lat" binding:"required,latitude"`
	Lng float64 `form:"lng" binding:"required,longitude"`
}

func (h *Handler) Routes(r *httpx.Router) {
	pub := r.Tag("discovery")
	if h.optional != nil {
		pub = pub.Use(h.optional)
	}
	pub.GET("/discover/equipment", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q Query) (any, error) {
		return h.svc.Search(c.Request.Context(), p, q)
	}), httpx.Doc{
		Summary: "Search approved equipment", Query: Query{}, Response: Results{},
		Description: "Full-text search over name, manufacturer, model, category, area and city, with distance from a point, " +
			"PIN code or city, map bounds, filters and sorting. Only active, approved listings are returned, and locations " +
			"are approximate (about 1 km). Signed-in users' searches are added to their search history.",
	})
	pub.GET("/discover/suggest", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q suggestQuery) (any, error) {
		return h.svc.Suggest(c.Request.Context(), q.Q)
	}), httpx.Doc{Summary: "Search suggestions (categories, makes, models, cities, PIN codes)", Query: suggestQuery{}, Response: SuggestionList{}})
	pub.GET("/discover/locations", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q suggestQuery) (any, error) {
		return h.svc.Locations(c.Request.Context(), q.Q)
	}), httpx.Doc{Summary: "Cities and PIN codes for choosing a search location", Query: suggestQuery{}, Response: LocationList{}})
	pub.GET("/discover/locations/pincode/:pin", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.LookupPincode(c.Request.Context(), c.Param("pin"))
	}), httpx.Doc{Summary: "Resolve an Indian PIN code to its postal location", Response: LocationDetails{}})
	pub.GET("/discover/locations/reverse", httpx.HandleQuery(200, func(c *gin.Context, _ *httpx.Principal, q reverseLocationQuery) (any, error) {
		return h.svc.ReverseGeocode(c.Request.Context(), q.Lat, q.Lng)
	}), httpx.Doc{Summary: "Reverse geocode a device coordinate into a postal address", Query: reverseLocationQuery{}, Response: LocationDetails{}})
	pub.GET("/discover/compare", httpx.HandleQuery(200, func(c *gin.Context, p *httpx.Principal, q compareQuery) (any, error) {
		return h.svc.Compare(c.Request.Context(), p, q.IDs)
	}), httpx.Doc{Summary: "Compare 2 to 4 listings", Query: compareQuery{}, Response: Comparison{}})
	pub.GET("/discover/maps", httpx.Handle(200, func(c *gin.Context, _ *httpx.Principal) (any, error) {
		return h.svc.Maps(), nil
	}), httpx.Doc{Summary: "Which map provider is configured", Response: MapsInfo{}})

	me := r.Tag("discovery").Authed()
	me.GET("/me/saved-equipment", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.Saved(c.Request.Context(), p.UserID)
	}), httpx.Doc{Summary: "Saved equipment", Response: SavedList{}})
	me.GET("/me/saved-equipment/ids", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.SavedIDs(c.Request.Context(), p.UserID)
	}), httpx.Doc{Summary: "Ids of saved equipment (to mark results)", Response: SavedIDs{}})
	me.PUT("/me/saved-equipment/:id", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.Save(c.Request.Context(), p.UserID, id)
	}), httpx.Doc{Summary: "Save a listing (idempotent)"})
	me.DELETE("/me/saved-equipment/:id", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.Unsave(c.Request.Context(), p.UserID, id)
	}), httpx.Doc{Summary: "Remove a saved listing (idempotent)"})
	me.GET("/me/search-history", httpx.Handle(200, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return h.svc.History(c.Request.Context(), p.UserID)
	}), httpx.Doc{Summary: "Recent searches (newest 30)", Response: HistoryList{}})
	me.DELETE("/me/search-history/:id", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		id, err := httpx.Param(c, "id")
		if err != nil {
			return nil, err
		}
		return nil, h.svc.DeleteHistory(c.Request.Context(), p.UserID, id)
	}), httpx.Doc{Summary: "Forget one recent search"})
	me.DELETE("/me/search-history", httpx.Handle(204, func(c *gin.Context, p *httpx.Principal) (any, error) {
		return nil, h.svc.ClearHistory(c.Request.Context(), p.UserID)
	}), httpx.Doc{Summary: "Clear search history"})
}

// ---- Google geocoding -------------------------------------------------------------------------

// GoogleGeocoder resolves Indian PIN codes with the Geocoding API.
type GoogleGeocoder struct {
	Key    string
	Client *http.Client
	// Endpoint is overridable in tests.
	Endpoint string
}

func NewGoogleGeocoder(key string) *GoogleGeocoder {
	return &GoogleGeocoder{Key: key, Client: &http.Client{Timeout: 5 * time.Second}, Endpoint: "https://maps.googleapis.com/maps/api/geocode/json"}
}

func (g *GoogleGeocoder) Pincode(ctx context.Context, pin string) (float64, float64, string, error) {
	location, err := g.PincodeDetails(ctx, pin)
	if err != nil {
		return 0, 0, "", err
	}
	label := location.Area
	if label == "" {
		label = location.City
	}
	return location.Lat, location.Lng, label, nil
}
