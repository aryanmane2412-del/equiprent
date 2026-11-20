package equipment

import (
	"context"
	"errors"
	"math"
	"regexp"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

var (
	registrationPattern = regexp.MustCompile(`^[A-Z]{2}[0-9]{1,2}[A-Z]{0,3}[0-9]{1,4}$`)
	specKeyPattern      = regexp.MustCompile(`^[a-z0-9_]+$`)
)

// Service implements catalogue and listing use cases.
type Service struct {
	repo     Repository
	ext      ListingStore // nil in unit tests with a minimal fake
	media    Media        // nil until WithMedia
	mediaOpt MediaOptions
}

func NewService(repo Repository) *Service {
	s := &Service{repo: repo}
	s.ext, _ = repo.(ListingStore)
	return s
}

// ---- catalogue (public) ------------------------------------------------------------

func (s *Service) Categories(ctx context.Context, lang string) (CategoryList, error) {
	if lang == "" {
		lang = "en"
	}
	list, err := s.repo.Categories(ctx, lang)
	return CategoryList{Data: list}, err
}

func (s *Service) Makes(ctx context.Context) (MakeList, error) {
	list, err := s.repo.Makes(ctx)
	return MakeList{Data: list}, err
}

func (s *Service) Models(ctx context.Context, q ModelsQuery) (ModelList, error) {
	var cat *string
	if q.Category != "" {
		cat = &q.Category
	}
	var makeID uuid.NullUUID
	if q.MakeID != "" {
		makeID = uuid.NullUUID{UUID: uuid.MustParse(q.MakeID), Valid: true}
	}
	list, err := s.repo.Models(ctx, cat, makeID)
	return ModelList{Data: list}, err
}

func (s *Service) Cities(ctx context.Context) (CityList, error) {
	list, err := s.repo.Cities(ctx)
	return CityList{Data: list}, err
}

// Public returns a listing visible to anyone (active, or any status for its
// owner and admins).
func (s *Service) Public(ctx context.Context, p *httpx.Principal, id uuid.UUID) (Listing, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	privileged := p != nil && (p.IsMember(l.OwnerOrgID) || p.IsAdmin())
	if l.Status != StatusActive && !privileged {
		return Listing{}, apperror.ErrNotFound
	}
	if p == nil || !p.IsMember(l.OwnerOrgID) {
		l.RegistrationNo = nil // owner details stay private until a booking is confirmed
		l.SerialNo = nil
	}
	if !privileged {
		l.ModerationNote, l.ModeratedAt = nil, nil
	}
	if p == nil || !p.IsMember(l.OwnerOrgID) {
		// Customers see only the approximate public point (same ~1 km grid
		// as the equipment_public_point SQL function), never the yard.
		l.BaseLocation = PublicPoint(l.BaseLocation)
	}
	s.decorate(&l)
	return l, nil
}

// PublicPoint is the centre of the 0.01 degree grid cell holding g. It must
// match equipment_public_point() in migration 000016.
func PublicPoint(g GeoPoint) GeoPoint {
	cell := func(v float64) float64 { return math.Round((math.Floor(v/0.01)*0.01+0.005)*1e4) / 1e4 }
	return GeoPoint{Lat: cell(g.Lat), Lng: cell(g.Lng)}
}

// ---- owner listings ----------------------------------------------------------------

// OwnerRoles may manage listings.
var OwnerRoles = []string{httpx.RoleOwner, httpx.RoleFleetManager}

// ListMine lists the organisation's machines; only, when not nil, restricts
// the list to those ids (a fleet manager's authorised machines).
func (s *Service) ListMine(ctx context.Context, owner uuid.UUID, q ListQuery, only []uuid.UUID) (SummaryPage, error) {
	k, err := httpx.PageQuery{Limit: q.Limit, Cursor: q.Cursor}.Keyset()
	if err != nil {
		return SummaryPage{}, err
	}
	var status *string
	if q.Status != "" {
		status = &q.Status
	}
	rows, err := s.repo.ListByOwner(ctx, owner, status, only, k)
	if err != nil {
		return SummaryPage{}, err
	}
	page := httpx.Paginate(s.cards(rows), k, func(s Summary) uuid.UUID { return s.ID })
	return SummaryPage{Data: page.Data, NextCursor: page.NextCursor}, nil
}

// Mine returns one of the organisation's listings.
func (s *Service) Mine(ctx context.Context, owner, id uuid.UUID) (Listing, error) {
	l, err := s.repo.Get(ctx, id)
	if err != nil {
		return Listing{}, err
	}
	if l.OwnerOrgID != owner {
		return Listing{}, apperror.ErrNotFound
	}
	return l, nil
}

// ownerView is Mine with signed photo links and the documents.
func (s *Service) ownerView(ctx context.Context, owner, id uuid.UUID) (Listing, error) {
	l, err := s.Mine(ctx, owner, id)
	if err != nil {
		return Listing{}, err
	}
	s.decorate(&l)
	return l, s.withDocuments(ctx, &l)
}

// OwnerView returns one of the organisation's listings with documents.
func (s *Service) OwnerView(ctx context.Context, owner, id uuid.UUID) (Listing, error) {
	return s.ownerView(ctx, owner, id)
}

// Create adds a draft listing.
func (s *Service) Create(ctx context.Context, owner uuid.UUID, req CreateRequest) (Listing, error) {
	reg, err := normaliseRegistration(req.RegistrationNo)
	if err != nil {
		return Listing{}, err
	}
	wet := true
	if req.WetHire != nil {
		wet = *req.WetHire
	}
	if !wet && !req.DryHire {
		return Listing{}, apperror.ErrValidation.WithDetail("offer the machine with an operator (wet hire), without (dry hire), or both")
	}
	radius := req.ServiceRadiusKm
	if radius == 0 {
		radius = 50
	}
	fuel := req.FuelPolicy
	if fuel == "" {
		fuel = "customer"
	}
	modelID, err := s.resolveModel(ctx, req.ModelID, req.Category, req.Manufacturer, req.ModelName)
	if err != nil {
		return Listing{}, err
	}
	if modelID == uuid.Nil {
		return Listing{}, apperror.ErrValidation.WithDetail("choose a catalogue model, or give category, manufacturer and model")
	}
	condition := req.Condition
	if condition == "" {
		condition = "good"
	}
	delivery := true
	if req.DeliveryAvailable != nil {
		delivery = *req.DeliveryAvailable
	}
	id, err := s.repo.Create(ctx, db.CreateEquipmentListingParams{
		OwnerOrgID: owner, ModelID: modelID, Title: strings.TrimSpace(req.Title), Description: strings.TrimSpace(req.Description),
		ManufactureYear: req.ManufactureYear, RegistrationNo: reg, SerialNo: req.SerialNo, CurrentHmr: req.CurrentHMR,
		Lat: req.BaseLocation.Lat, Lng: req.BaseLocation.Lng, BaseAddressID: req.BaseAddressID, CityID: req.CityID,
		ServiceRadiusKm: radius, WetHire: wet, DryHire: req.DryHire, FuelPolicy: db.FuelPolicy(fuel), InstantBook: req.InstantBook,
		Condition: db.EquipmentCondition(condition), DeliveryAvailable: delivery, LocationLabel: trimmed(req.LocationLabel),
	})
	if err != nil {
		return Listing{}, err
	}
	return s.ownerView(ctx, owner, id)
}

// Update edits a listing with optimistic locking: a stale version gets 412.
func (s *Service) Update(ctx context.Context, owner, id uuid.UUID, req UpdateRequest) (Listing, error) {
	cur, err := s.Mine(ctx, owner, id)
	if err != nil {
		return Listing{}, err
	}
	if cur.Status == StatusRemoved {
		return Listing{}, apperror.ErrInvalidTransition.WithDetail("this listing was removed by a moderator and cannot be edited")
	}
	reg, err := normaliseRegistration(req.RegistrationNo)
	if err != nil {
		return Listing{}, err
	}
	modelID, err := s.resolveModel(ctx, req.ModelID, req.Category, req.Manufacturer, req.ModelName)
	if err != nil {
		return Listing{}, err
	}
	p := db.UpdateEquipmentListingParams{
		ID: id, OwnerOrgID: owner, Version: req.Version, Title: trimmed(req.Title), Description: req.Description,
		CurrentHmr: req.CurrentHMR, ServiceRadiusKm: req.ServiceRadiusKm, WetHire: req.WetHire, DryHire: req.DryHire,
		InstantBook: req.InstantBook, ManufactureYear: req.ManufactureYear, RegistrationNo: reg,
		DeliveryAvailable: req.DeliveryAvailable, LocationLabel: trimmed(req.LocationLabel),
	}
	if modelID != uuid.Nil {
		p.ModelID = uuid.NullUUID{UUID: modelID, Valid: true}
	}
	if req.FuelPolicy != nil {
		p.FuelPolicy = db.NullFuelPolicy{FuelPolicy: db.FuelPolicy(*req.FuelPolicy), Valid: true}
	}
	if req.Condition != nil {
		p.Condition = db.NullEquipmentCondition{EquipmentCondition: db.EquipmentCondition(*req.Condition), Valid: true}
	}
	if req.CityID != nil {
		p.CityID = uuid.NullUUID{UUID: *req.CityID, Valid: true}
	}
	if req.BaseLocation != nil {
		p.Lat, p.Lng = &req.BaseLocation.Lat, &req.BaseLocation.Lng
	}
	wet, dry := cur.WetHire, cur.DryHire
	if req.WetHire != nil {
		wet = *req.WetHire
	}
	if req.DryHire != nil {
		dry = *req.DryHire
	}
	if !wet && !dry {
		return Listing{}, apperror.ErrValidation.WithDetail("offer the machine with an operator (wet hire), without (dry hire), or both")
	}
	if _, err := s.repo.Update(ctx, p); err != nil {
		if errors.Is(err, apperror.ErrNotFound) {
			return Listing{}, apperror.ErrVersionMismatch
		}
		return Listing{}, err
	}
	// What customers rely on changed: a live listing goes back to review.
	identity := (req.Title != nil && strings.TrimSpace(*req.Title) != cur.Title) ||
		(req.Description != nil && *req.Description != cur.Description) ||
		(modelID != uuid.Nil && modelID != cur.ModelID) ||
		(req.ManufactureYear != nil && (cur.ManufactureYear == nil || *req.ManufactureYear != *cur.ManufactureYear)) ||
		(reg != nil && (cur.RegistrationNo == nil || *reg != *cur.RegistrationNo))
	if identity {
		if err := s.contentChanged(ctx, id); err != nil {
			return Listing{}, err
		}
	}
	return s.ownerView(ctx, owner, id)
}

// Retire removes a listing that holds no active bookings.
func (s *Service) Retire(ctx context.Context, owner, id uuid.UUID) error {
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return err
	}
	ok, err := s.repo.Retire(ctx, id, owner)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrConflict.WithDetail("the machine has active bookings; finish or cancel them first")
	}
	return nil
}

// Act runs a lifecycle action. owner is uuid.Nil for admin actions.
func (s *Service) Act(ctx context.Context, owner, id uuid.UUID, action Action) (StatusChange, error) {
	t, ok := Transitions[action]
	if !ok {
		return StatusChange{}, apperror.ErrNotFound
	}
	ownerFilter := uuid.NullUUID{}
	if owner != uuid.Nil {
		if !t.ownerOK {
			return StatusChange{}, apperror.ErrForbidden
		}
		if _, err := s.Mine(ctx, owner, id); err != nil {
			return StatusChange{}, err
		}
		ownerFilter = uuid.NullUUID{UUID: owner, Valid: true}
	}
	if action == ActionSubmit {
		if owner != uuid.Nil {
			kyc, err := s.repo.OrgKYCStatus(ctx, owner)
			if err != nil {
				return StatusChange{}, err
			}
			if kyc != "verified" {
				return StatusChange{}, apperror.ErrKYCRequired.WithDetail("your business verification must be approved before listings go live")
			}
		}
		plans, err := s.repo.PricingPlans(ctx, id)
		if err != nil {
			return StatusChange{}, err
		}
		if len(plans) == 0 {
			return StatusChange{}, apperror.ErrBusinessRule.WithDetail("set at least one price before submitting for review")
		}
		if s.ext != nil {
			n, err := s.ext.PhotoCount(ctx, id)
			if err != nil {
				return StatusChange{}, err
			}
			if n == 0 {
				return StatusChange{}, apperror.ErrBusinessRule.WithDetail("add at least one photo before submitting for review")
			}
			documents, err := s.ext.Documents(ctx, id)
			if err != nil {
				return StatusChange{}, err
			}
			if len(documents) == 0 {
				return StatusChange{}, apperror.ErrBusinessRule.WithDetail("attach at least one equipment document before submitting for review")
			}
		}
	}
	change, err := s.repo.Transition(ctx, id, ownerFilter, t.from, t.to)
	if errors.Is(err, apperror.ErrNotFound) {
		_, current, ownerErr := s.repo.Owner(ctx, id)
		if ownerErr != nil {
			return StatusChange{}, ownerErr
		}
		return StatusChange{}, apperror.ErrInvalidTransition.WithDetail(
			"cannot " + string(action) + " a listing that is " + current)
	}
	return change, err
}

// SetPricing replaces the price list.
func (s *Service) SetPricing(ctx context.Context, owner, id uuid.UUID, req PricingRequest) (Listing, error) {
	l, err := s.Mine(ctx, owner, id)
	if err != nil {
		return Listing{}, err
	}
	seen := map[string]bool{}
	for i, p := range req.Plans {
		key := p.Unit + "/" + map[bool]string{true: "wet", false: "dry"}[p.WithOperator]
		if seen[key] {
			return Listing{}, apperror.ErrValidation.WithDetail("duplicate price for " + key)
		}
		seen[key] = true
		if p.WithOperator && !l.WetHire || !p.WithOperator && !l.DryHire {
			return Listing{}, apperror.ErrValidation.WithFields(apperror.FieldError{
				Field: "plans[" + itoa(i) + "].with_operator", Code: "hire_mode", Message: "the listing does not offer this hire mode",
			})
		}
		if !p.WithOperator && p.OperatorBataPerDayPaise != 0 {
			return Listing{}, apperror.ErrValidation.WithDetail("operator bata applies only to wet hire")
		}
	}
	if err := s.repo.ReplacePricing(ctx, id, req.Plans, req.Mobilisation); err != nil {
		return Listing{}, err
	}
	return s.ownerView(ctx, owner, id)
}

// SetSpecs upserts specification values.
func (s *Service) SetSpecs(ctx context.Context, owner, id uuid.UUID, req SpecsRequest) (Listing, error) {
	if _, err := s.Mine(ctx, owner, id); err != nil {
		return Listing{}, err
	}
	for _, sp := range req.Specs {
		if !specKeyPattern.MatchString(sp.Key) {
			return Listing{}, apperror.ErrValidation.WithDetail("spec keys use lower_snake_case: " + sp.Key)
		}
		if sp.ValueNumeric == nil && sp.ValueText == nil {
			return Listing{}, apperror.ErrValidation.WithDetail("spec " + sp.Key + " needs a value")
		}
	}
	if err := s.repo.ReplaceSpecs(ctx, id, req.Specs); err != nil {
		return Listing{}, err
	}
	return s.ownerView(ctx, owner, id)
}

// ModerationQueue lists listings by status for admins.
func (s *Service) ModerationQueue(ctx context.Context, status string, page httpx.PageQuery) (SummaryPage, error) {
	if !slices.Contains([]string{StatusPendingReview, StatusActive, StatusPaused, StatusSuspended, StatusRejected, StatusRemoved}, status) {
		status = StatusPendingReview
	}
	k, err := page.Keyset()
	if err != nil {
		return SummaryPage{}, err
	}
	rows, err := s.repo.ListByStatus(ctx, status, k)
	if err != nil {
		return SummaryPage{}, err
	}
	pg := httpx.Paginate(s.cards(rows), k, func(s Summary) uuid.UUID { return s.ID })
	return SummaryPage{Data: pg.Data, NextCursor: pg.NextCursor}, nil
}

func trimmed(v *string) *string {
	if v == nil {
		return nil
	}
	t := strings.TrimSpace(*v)
	return &t
}

func normaliseRegistration(raw *string) (*string, error) {
	if raw == nil {
		return nil, nil
	}
	reg := strings.ToUpper(strings.NewReplacer(" ", "", "-", "").Replace(*raw))
	if reg == "" {
		return nil, nil
	}
	if !registrationPattern.MatchString(reg) {
		return nil, apperror.ErrValidation.WithFields(apperror.FieldError{
			Field: "registration_no", Code: "invalid", Message: "must look like MH12KX4471",
		})
	}
	return &reg, nil
}

func itoa(i int) string {
	const digits = "0123456789"
	if i < 10 {
		return digits[i : i+1]
	}
	return itoa(i/10) + digits[i%10:i%10+1]
}
