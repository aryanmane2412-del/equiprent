package equipment

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/httpx"
)

// Repository is the persistence the equipment service needs.
type Repository interface {
	Categories(ctx context.Context, lang string) ([]Category, error)
	Makes(ctx context.Context) ([]Make, error)
	Models(ctx context.Context, categorySlug *string, makeID uuid.NullUUID) ([]Model, error)
	Cities(ctx context.Context) ([]City, error)

	Get(ctx context.Context, id uuid.UUID) (Listing, error)
	ListByOwner(ctx context.Context, owner uuid.UUID, status *string, only []uuid.UUID, k httpx.Keyset) ([]Summary, error)
	ListByStatus(ctx context.Context, status string, k httpx.Keyset) ([]Summary, error)
	Create(ctx context.Context, p db.CreateEquipmentListingParams) (uuid.UUID, error)
	Update(ctx context.Context, p db.UpdateEquipmentListingParams) (int32, error)
	Transition(ctx context.Context, id uuid.UUID, owner uuid.NullUUID, from []string, to string) (StatusChange, error)
	Retire(ctx context.Context, id, owner uuid.UUID) (bool, error)
	Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error)
	// OrgKYCStatus is the owner organisation's verification status.
	OrgKYCStatus(ctx context.Context, org uuid.UUID) (string, error)

	PricingPlans(ctx context.Context, id uuid.UUID) ([]PricingPlan, error)
	ReplacePricing(ctx context.Context, id uuid.UUID, plans []PricingPlan, mob *Mobilisation) error
	ReplaceSpecs(ctx context.Context, id uuid.UUID, specs []Spec) error
}

// PGRepository implements Repository on PostgreSQL.
type PGRepository struct {
	pool *pgxpool.Pool
	tx   database.Transactor
}

func NewRepository(pool *pgxpool.Pool, tx database.Transactor) *PGRepository {
	return &PGRepository{pool: pool, tx: tx}
}

func (r *PGRepository) q(ctx context.Context) *db.Queries { return database.Queries(ctx, r.pool) }

func (r *PGRepository) Categories(ctx context.Context, lang string) ([]Category, error) {
	rows, err := r.q(ctx).ListCategoriesWithCounts(ctx, db.ListCategoriesWithCountsParams{Lang: lang})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Category, 0, len(rows))
	for _, c := range rows {
		out = append(out, Category{ID: c.ID, ParentID: c.ParentID, Slug: c.Slug, Name: c.NameLocal, NameEn: c.NameEn,
			IconKey: c.IconKey, SortOrder: c.SortOrder, ListingCount: c.ListingCount, SpecSchema: c.SpecSchema})
	}
	return out, nil
}

func (r *PGRepository) Makes(ctx context.Context) ([]Make, error) {
	rows, err := r.q(ctx).ListMakes(ctx)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Make, 0, len(rows))
	for _, m := range rows {
		out = append(out, Make{ID: m.ID, Name: m.Name})
	}
	return out, nil
}

func (r *PGRepository) Models(ctx context.Context, categorySlug *string, makeID uuid.NullUUID) ([]Model, error) {
	rows, err := r.q(ctx).ListModels(ctx, db.ListModelsParams{CategorySlug: categorySlug, MakeID: makeID})
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]Model, 0, len(rows))
	for _, m := range rows {
		out = append(out, Model(m))
	}
	return out, nil
}

func (r *PGRepository) Cities(ctx context.Context) ([]City, error) {
	rows, err := r.q(ctx).ListCities(ctx)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]City, 0, len(rows))
	for _, c := range rows {
		out = append(out, City{ID: c.ID, Name: c.Name, StateCode: c.StateCode, IsLive: c.IsLive, Center: GeoPoint{Lat: c.Lat, Lng: c.Lng}})
	}
	return out, nil
}

func (r *PGRepository) Get(ctx context.Context, id uuid.UUID) (Listing, error) {
	q := r.q(ctx)
	e, err := q.GetEquipmentDetail(ctx, id)
	if err != nil {
		return Listing{}, database.MapError(err)
	}
	l := Listing{
		ID: e.ID, OwnerOrgID: e.OwnerOrgID, OwnerName: e.OwnerName, OwnerVerified: e.OwnerKycStatus == db.VerificationStatusVerified,
		ModelID: e.ModelID, ModelName: e.ModelName, MakeName: e.MakeName, CategoryID: e.CategoryID, CategorySlug: e.CategorySlug,
		Title: e.Title, Description: e.Description, ManufactureYear: e.ManufactureYear, RegistrationNo: e.RegistrationNo,
		CurrentHMR: e.CurrentHmr, BaseLocation: GeoPoint{Lat: e.Lat, Lng: e.Lng}, CityID: e.CityID,
		ServiceRadiusKm: e.ServiceRadiusKm, WetHire: e.WetHire, DryHire: e.DryHire, FuelPolicy: string(e.FuelPolicy),
		InstantBook: e.InstantBook, Status: string(e.Status), RatingAvg: e.RatingAvg, RatingCount: e.RatingCount,
		Version: e.Version, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt,
		CategoryName: e.CategoryName, CityName: e.CityName, LocationLabel: e.LocationLabel, Condition: string(e.Condition),
		DeliveryAvailable: e.DeliveryAvailable, SerialNo: e.SerialNo, ModerationNote: e.ModerationNote, ModeratedAt: e.ModeratedAt,
	}
	if l.Pricing, err = r.PricingPlans(ctx, id); err != nil {
		return Listing{}, err
	}
	if m, err := q.GetMobilisationRate(ctx, id); err == nil {
		l.Mobilisation = &Mobilisation{FixedPaise: m.FixedPaise, PerKmPaise: m.PerKmPaise, FreeKm: m.FreeKm, NeedsTrailer: m.NeedsTrailer}
	}
	specs, err := q.ListEquipmentSpecs(ctx, id)
	if err != nil {
		return Listing{}, database.MapError(err)
	}
	l.Specs = make([]Spec, 0, len(specs))
	for _, s := range specs {
		l.Specs = append(l.Specs, Spec{Key: s.SpecKey, ValueNumeric: s.ValueNumeric, ValueText: s.ValueText, Unit: s.Unit})
	}
	photos, err := q.ListEquipmentPhotos(ctx, id)
	if err != nil {
		return Listing{}, database.MapError(err)
	}
	l.Photos = make([]Photo, 0, len(photos))
	for _, p := range photos {
		l.Photos = append(l.Photos, Photo{ID: p.ID, ObjectKey: p.ObjectKey, ContentType: p.ContentType, Width: p.Width,
			Height: p.Height, Angle: p.Angle, IsCover: p.IsCover})
	}
	return l, nil
}

func (r *PGRepository) OrgKYCStatus(ctx context.Context, org uuid.UUID) (string, error) {
	st, err := r.q(ctx).GetOrgKYCStatus(ctx, org)
	return string(st), database.MapError(err)
}

func (r *PGRepository) ListByOwner(ctx context.Context, owner uuid.UUID, status *string, only []uuid.UUID, k httpx.Keyset) ([]Summary, error) {
	return r.Browse(ctx, db.BrowseListingsParams{
		OwnerOrgID: uuid.NullUUID{UUID: owner, Valid: true}, Status: status, OnlyIds: only, After: k.After, ResultLimit: k.Fetch(),
	})
}

func (r *PGRepository) ListByStatus(ctx context.Context, status string, k httpx.Keyset) ([]Summary, error) {
	return r.Browse(ctx, db.BrowseListingsParams{Status: &status, After: k.After, ResultLimit: k.Fetch()})
}

func (r *PGRepository) Create(ctx context.Context, p db.CreateEquipmentListingParams) (uuid.UUID, error) {
	id, err := r.q(ctx).CreateEquipmentListing(ctx, p)
	return id, database.MapError(err)
}

func (r *PGRepository) Update(ctx context.Context, p db.UpdateEquipmentListingParams) (int32, error) {
	row, err := r.q(ctx).UpdateEquipmentListing(ctx, p)
	return row.Version, database.MapError(err)
}

func (r *PGRepository) Transition(ctx context.Context, id uuid.UUID, owner uuid.NullUUID, from []string, to string) (StatusChange, error) {
	row, err := r.q(ctx).TransitionEquipmentStatus(ctx, db.TransitionEquipmentStatusParams{
		ID: id, OwnerOrgID: owner, FromStatuses: from, ToStatus: db.ListingStatus(to),
	})
	if err != nil {
		return StatusChange{}, database.MapError(err)
	}
	return StatusChange{ID: row.ID, Status: string(row.Status), Version: row.Version}, nil
}

func (r *PGRepository) Retire(ctx context.Context, id, owner uuid.UUID) (bool, error) {
	n, err := r.q(ctx).RetireOwnerEquipment(ctx, db.RetireOwnerEquipmentParams{ID: id, OwnerOrgID: owner})
	return n == 1, database.MapError(err)
}

func (r *PGRepository) Owner(ctx context.Context, id uuid.UUID) (uuid.UUID, string, error) {
	row, err := r.q(ctx).EquipmentOwner(ctx, id)
	return row.OwnerOrgID, string(row.Status), database.MapError(err)
}

func (r *PGRepository) PricingPlans(ctx context.Context, id uuid.UUID) ([]PricingPlan, error) {
	rows, err := r.q(ctx).ListPricingPlans(ctx, id)
	if err != nil {
		return nil, database.MapError(err)
	}
	out := make([]PricingPlan, 0, len(rows))
	for _, p := range rows {
		out = append(out, PricingPlan{
			ID: p.ID, Unit: string(p.Unit), WithOperator: p.WithOperator, RatePaise: p.RatePaise, MinUnits: p.MinUnits,
			IncludedHours: p.IncludedHours, ExtraHourRatePaise: p.ExtraHourRatePaise, OperatorBataPerDayPaise: p.OperatorBataPerDayPaise,
		})
	}
	return out, nil
}

// ReplacePricing deactivates the current plans and inserts the new set
// atomically (old plans stay for bookings that reference their snapshot).
func (r *PGRepository) ReplacePricing(ctx context.Context, id uuid.UUID, plans []PricingPlan, mob *Mobilisation) error {
	return database.MapError(r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		if err := q.DeactivatePricingPlans(ctx, id); err != nil {
			return err
		}
		for _, p := range plans {
			if _, err := q.CreatePricingPlan(ctx, db.CreatePricingPlanParams{
				EquipmentID: id, Unit: db.PriceUnit(p.Unit), WithOperator: p.WithOperator, RatePaise: p.RatePaise,
				MinUnits: max(p.MinUnits, 1), IncludedHours: p.IncludedHours, ExtraHourRatePaise: p.ExtraHourRatePaise,
				OperatorBataPerDayPaise: p.OperatorBataPerDayPaise,
			}); err != nil {
				return err
			}
		}
		if mob != nil {
			return q.UpsertMobilisationRate(ctx, db.UpsertMobilisationRateParams{
				EquipmentID: id, FixedPaise: mob.FixedPaise, PerKmPaise: mob.PerKmPaise, FreeKm: mob.FreeKm, NeedsTrailer: mob.NeedsTrailer,
			})
		}
		return nil
	}))
}

func (r *PGRepository) ReplaceSpecs(ctx context.Context, id uuid.UUID, specs []Spec) error {
	return database.MapError(r.tx.InTx(ctx, func(ctx context.Context) error {
		q := r.q(ctx)
		for _, s := range specs {
			if err := q.UpsertEquipmentSpec(ctx, db.UpsertEquipmentSpecParams{
				EquipmentID: id, SpecKey: s.Key, ValueNumeric: s.ValueNumeric, ValueText: s.ValueText, Unit: s.Unit,
			}); err != nil {
				return err
			}
		}
		return nil
	}))
}
