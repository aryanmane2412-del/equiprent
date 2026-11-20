package equipment

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

type memRepo struct {
	listings map[uuid.UUID]*Listing
	plans    map[uuid.UUID][]PricingPlan
	created  []db.CreateEquipmentListingParams
	kyc      string // "" means verified
}

func (m *memRepo) OrgKYCStatus(context.Context, uuid.UUID) (string, error) {
	if m.kyc == "" {
		return "verified", nil
	}
	return m.kyc, nil
}

func newMem() *memRepo {
	return &memRepo{listings: map[uuid.UUID]*Listing{}, plans: map[uuid.UUID][]PricingPlan{}}
}

func (m *memRepo) Categories(context.Context, string) ([]Category, error) { return nil, nil }
func (m *memRepo) Makes(context.Context) ([]Make, error)                  { return nil, nil }
func (m *memRepo) Models(context.Context, *string, uuid.NullUUID) ([]Model, error) {
	return nil, nil
}
func (m *memRepo) Cities(context.Context) ([]City, error) { return nil, nil }
func (m *memRepo) Get(_ context.Context, id uuid.UUID) (Listing, error) {
	l, ok := m.listings[id]
	if !ok {
		return Listing{}, apperror.ErrNotFound
	}
	return *l, nil
}
func (m *memRepo) ListByOwner(context.Context, uuid.UUID, *string, []uuid.UUID, httpx.Keyset) ([]Summary, error) {
	return nil, nil
}
func (m *memRepo) ListByStatus(context.Context, string, httpx.Keyset) ([]Summary, error) {
	return nil, nil
}
func (m *memRepo) Create(_ context.Context, p db.CreateEquipmentListingParams) (uuid.UUID, error) {
	m.created = append(m.created, p)
	id := uuid.New()
	m.listings[id] = &Listing{ID: id, OwnerOrgID: p.OwnerOrgID, Status: StatusDraft, WetHire: p.WetHire, DryHire: p.DryHire}
	return id, nil
}
func (m *memRepo) Update(context.Context, db.UpdateEquipmentListingParams) (int32, error) {
	return 0, apperror.ErrNotFound // simulate a stale version
}
func (m *memRepo) Transition(_ context.Context, id uuid.UUID, owner uuid.NullUUID, from []string, to string) (StatusChange, error) {
	l := m.listings[id]
	if !slices.Contains(from, l.Status) || (owner.Valid && owner.UUID != l.OwnerOrgID) {
		return StatusChange{}, apperror.ErrNotFound
	}
	l.Status = to
	return StatusChange{ID: id, Status: to}, nil
}
func (m *memRepo) Retire(context.Context, uuid.UUID, uuid.UUID) (bool, error) { return false, nil }
func (m *memRepo) Owner(_ context.Context, id uuid.UUID) (uuid.UUID, string, error) {
	return m.listings[id].OwnerOrgID, m.listings[id].Status, nil
}
func (m *memRepo) PricingPlans(_ context.Context, id uuid.UUID) ([]PricingPlan, error) {
	return m.plans[id], nil
}
func (m *memRepo) ReplacePricing(_ context.Context, id uuid.UUID, plans []PricingPlan, _ *Mobilisation) error {
	m.plans[id] = plans
	return nil
}
func (m *memRepo) ReplaceSpecs(context.Context, uuid.UUID, []Spec) error { return nil }

var org, model = uuid.New(), uuid.New()

func create(t *testing.T, svc *Service, req CreateRequest) Listing {
	t.Helper()
	if req.Title == "" {
		req.Title = "JCB 3DX"
	}
	if req.ModelID == nil {
		req.ModelID = &model
	}
	l, err := svc.Create(context.Background(), org, req)
	if err != nil {
		t.Fatal(err)
	}
	return l
}

func TestCreateDefaultsAndRegistration(t *testing.T) {
	repo := newMem()
	svc := NewService(repo)
	reg := " mh-12 kx 4471 "
	create(t, svc, CreateRequest{RegistrationNo: &reg})
	p := repo.created[0]
	if *p.RegistrationNo != "MH12KX4471" || !p.WetHire || p.ServiceRadiusKm != 50 || p.FuelPolicy != "customer" {
		t.Fatalf("params = %+v", p)
	}
	bad := "12345"
	if _, err := svc.Create(context.Background(), org, CreateRequest{Title: "x y z", ModelID: &model, RegistrationNo: &bad}); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("bad registration: %v", err)
	}
	no := false
	if _, err := svc.Create(context.Background(), org, CreateRequest{Title: "x y z", ModelID: &model, WetHire: &no}); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("no hire mode: %v", err)
	}
}

func TestLifecycleRules(t *testing.T) {
	ctx := context.Background()
	repo := newMem()
	svc := NewService(repo)
	l := create(t, svc, CreateRequest{})

	if _, err := svc.Act(ctx, org, l.ID, ActionSubmit); !errors.Is(err, apperror.ErrBusinessRule) {
		t.Fatalf("submit without price: %v", err)
	}
	if _, err := svc.SetPricing(ctx, org, l.ID, PricingRequest{Plans: []PricingPlan{{Unit: "day", WithOperator: true, RatePaise: 100000}}}); err != nil {
		t.Fatal(err)
	}
	if ch, err := svc.Act(ctx, org, l.ID, ActionSubmit); err != nil || ch.Status != StatusActive {
		t.Fatalf("submit goes live: %+v %v", ch, err)
	}
	if _, err := svc.Act(ctx, org, l.ID, ActionApprove); !errors.Is(err, apperror.ErrForbidden) {
		t.Fatalf("owner approving own listing: %v", err)
	}
	if ch, err := svc.Act(ctx, org, l.ID, ActionPause); err != nil || ch.Status != StatusPaused {
		t.Fatalf("owner pauses a live listing: %+v %v", ch, err)
	}
	if _, err := svc.Act(ctx, uuid.New(), l.ID, ActionPause); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatalf("another org pausing: %v", err)
	}
}

func TestPricingValidation(t *testing.T) {
	ctx := context.Background()
	svc := NewService(newMem())
	l := create(t, svc, CreateRequest{}) // wet hire only
	cases := map[string][]PricingPlan{
		"duplicate unit":  {{Unit: "day", WithOperator: true, RatePaise: 1}, {Unit: "day", WithOperator: true, RatePaise: 2}},
		"dry not offered": {{Unit: "day", WithOperator: false, RatePaise: 1}},
	}
	for name, plans := range cases {
		if _, err := svc.SetPricing(ctx, org, l.ID, PricingRequest{Plans: plans}); !errors.Is(err, apperror.ErrValidation) {
			t.Errorf("%s: %v", name, err)
		}
	}
}

func TestStaleUpdateIs412(t *testing.T) {
	svc := NewService(newMem())
	l := create(t, svc, CreateRequest{})
	if _, err := svc.Update(context.Background(), org, l.ID, UpdateRequest{Version: 7}); !errors.Is(err, apperror.ErrVersionMismatch) {
		t.Fatalf("stale update: %v", err)
	}
}

func TestPublicHidesDraftsAndRegistration(t *testing.T) {
	repo := newMem()
	svc := NewService(repo)
	reg := "MH12KX4471"
	l := create(t, svc, CreateRequest{RegistrationNo: &reg})
	repo.listings[l.ID].RegistrationNo = &reg
	if _, err := svc.Public(context.Background(), nil, l.ID); !errors.Is(err, apperror.ErrNotFound) {
		t.Fatal("drafts are not public")
	}
	ownerP := &httpx.Principal{Roles: []httpx.Role{{Code: "owner", OrgID: org}}}
	if got, err := svc.Public(context.Background(), ownerP, l.ID); err != nil || got.RegistrationNo == nil {
		t.Fatalf("owner preview: %v", err)
	}
	repo.listings[l.ID].Status = StatusActive
	if got, _ := svc.Public(context.Background(), nil, l.ID); got.RegistrationNo != nil {
		t.Fatal("registration number must be hidden from the public")
	}
}

func TestCreateNeedsAModel(t *testing.T) {
	svc := NewService(newMem())
	_, err := svc.Create(context.Background(), org, CreateRequest{Title: "Loader for hire"})
	if !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("no model: %v", err)
	}
	_, err = svc.Create(context.Background(), org, CreateRequest{Title: "Loader for hire", Category: "wheel-loader", Manufacturer: "Liugong"})
	if !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("partial model: %v", err)
	}
}

func TestModerationNeedsReasons(t *testing.T) {
	svc := NewService(newMem())
	l := create(t, svc, CreateRequest{})
	moderator := &httpx.Principal{UserID: uuid.New(), Roles: []httpx.Role{{Code: httpx.RoleAdminOps}}}
	for _, a := range []Action{ActionReject, ActionSuspend, ActionRemove} {
		_, err := svc.Moderate(context.Background(), moderator, l.ID, a, "bad")
		if a != ActionReject && !errors.Is(err, apperror.ErrForbidden) {
			t.Errorf("%s is unavailable in equipment verification: %v", a, err)
		} else if a == ActionReject && !errors.Is(err, apperror.ErrValidation) {
			t.Errorf("%s without a proper reason: %v", a, err)
		}
	}
	if _, err := svc.Moderate(context.Background(), moderator, l.ID, ActionSubmit, ""); !errors.Is(err, apperror.ErrForbidden) {
		t.Errorf("owner action through moderation: %v", err)
	}
	if _, err := svc.Moderate(context.Background(), &httpx.Principal{}, l.ID, ActionReject, "Reason is long enough"); !errors.Is(err, apperror.ErrForbidden) {
		t.Errorf("unauthorized moderator: %v", err)
	}
}
