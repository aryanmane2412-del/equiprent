// Package equipment owns the catalogue (categories, makes, models, cities)
// and equipment listings: creation, editing with optimistic locking, the
// listing lifecycle (draft → review → active ⇄ paused), pricing,
// mobilisation rates, specifications and photographs.
package equipment

import (
	"encoding/json"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/storage"
)

// Listing statuses.
const (
	StatusDraft         = "draft"
	StatusPendingReview = "pending_review"
	StatusActive        = "active"
	StatusPaused        = "paused"
	StatusRejected      = "rejected"
	StatusSuspended     = "suspended"
	StatusRetired       = "retired"
	// StatusRemoved is set by admins for prohibited or fraudulent listings;
	// it is final.
	StatusRemoved = "removed"
)

// Conditions an owner can declare.
var Conditions = []string{"excellent", "good", "fair", "needs_repair"}

// Action is a listing lifecycle action.
type Action string

const (
	ActionSubmit    Action = "submit"
	ActionPause     Action = "pause"
	ActionResume    Action = "resume"
	ActionApprove   Action = "approve"
	ActionReject    Action = "reject"
	ActionSuspend   Action = "suspend"
	ActionReinstate Action = "reinstate"
	ActionRemove    Action = "remove"
)

// transition is one allowed lifecycle edge set.
type transition struct {
	from    []string
	to      string
	ownerOK bool // owners may perform it; otherwise admin only
}

// Transitions is the listing state machine.
var Transitions = map[Action]transition{
	ActionSubmit:    {from: []string{StatusDraft, StatusRejected}, to: StatusActive, ownerOK: true},
	ActionPause:     {from: []string{StatusActive}, to: StatusPaused, ownerOK: true},
	ActionResume:    {from: []string{StatusPaused}, to: StatusActive, ownerOK: true},
	ActionApprove:   {from: []string{StatusPendingReview}, to: StatusActive},
	ActionReject:    {from: []string{StatusPendingReview}, to: StatusRejected},
	ActionSuspend:   {from: []string{StatusActive, StatusPaused, StatusPendingReview}, to: StatusSuspended},
	ActionReinstate: {from: []string{StatusSuspended}, to: StatusActive},
	ActionRemove: {from: []string{StatusDraft, StatusPendingReview, StatusActive, StatusPaused, StatusRejected, StatusSuspended},
		to: StatusRemoved},
}

// GeoPoint is a WGS84 coordinate.
type GeoPoint struct {
	Lat float64 `json:"lat" binding:"latitude"`
	Lng float64 `json:"lng" binding:"longitude"`
}

// Category is a catalogue category.
type Category struct {
	ID        uuid.UUID     `json:"id"`
	ParentID  uuid.NullUUID `json:"parent_id"`
	Slug      string        `json:"slug"`
	Name      string        `json:"name"`
	NameEn    string        `json:"name_en"`
	IconKey   *string       `json:"icon_key,omitempty"`
	SortOrder int32         `json:"sort_order"`
	// ListingCount is the number of approved listings in the category.
	ListingCount int32 `json:"listing_count"`
	// SpecSchema suggests specification keys and types for the category.
	SpecSchema json.RawMessage `json:"spec_schema" swaggertype:"object"`
}

type Make struct {
	ID   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type Model struct {
	ID           uuid.UUID `json:"id"`
	Name         string    `json:"name"`
	MakeName     string    `json:"make_name"`
	CategorySlug string    `json:"category_slug"`
}

type City struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	StateCode string    `json:"state_code"`
	IsLive    bool      `json:"is_live"`
	Center    GeoPoint  `json:"center"`
}

// PricingPlan is the price for one unit (hour, shift, day…) and hire mode.
type PricingPlan struct {
	ID                      uuid.UUID `json:"id,omitempty"`
	Unit                    string    `json:"unit" binding:"required,oneof=hour shift day week month"`
	WithOperator            bool      `json:"with_operator"`
	RatePaise               int64     `json:"rate_paise" binding:"required,min=100"`
	MinUnits                int32     `json:"min_units" binding:"omitempty,min=1,max=365"`
	IncludedHours           *float64  `json:"included_hours,omitempty" binding:"omitempty,gt=0,max=744"`
	ExtraHourRatePaise      *int64    `json:"extra_hour_rate_paise,omitempty" binding:"omitempty,min=0"`
	OperatorBataPerDayPaise int64     `json:"operator_bata_per_day_paise" binding:"omitempty,min=0"`
}

// Mobilisation prices moving the machine to site.
type Mobilisation struct {
	FixedPaise   int64 `json:"fixed_paise" binding:"min=0"`
	PerKmPaise   int64 `json:"per_km_paise" binding:"min=0"`
	FreeKm       int32 `json:"free_km" binding:"min=0,max=500"`
	NeedsTrailer bool  `json:"needs_trailer"`
}

type Spec struct {
	Key          string   `json:"key" binding:"required,max=60"`
	ValueNumeric *float64 `json:"value_numeric,omitempty"`
	ValueText    *string  `json:"value_text,omitempty" binding:"omitempty,max=200"`
	Unit         *string  `json:"unit,omitempty" binding:"omitempty,max=20"`
}

// Photo is a listing photograph. URL is a signed, time-limited link;
// storage keys are never exposed.
type Photo struct {
	ID          uuid.UUID `json:"id"`
	URL         string    `json:"url"`
	ContentType string    `json:"content_type"`
	Width       *int32    `json:"width,omitempty"`
	Height      *int32    `json:"height,omitempty"`
	Angle       *string   `json:"angle,omitempty"`
	IsCover     bool      `json:"is_cover"`
	ObjectKey   string    `json:"-"`
}

// Document is a machine document (RC, insurance…). Only the owner and
// moderators see documents; Download is filled for moderators.
type Document struct {
	ID             uuid.UUID          `json:"id"`
	DocType        string             `json:"doc_type"`
	DocumentNumber *string            `json:"document_number,omitempty"`
	ContentType    string             `json:"content_type"`
	ValidFrom      *string            `json:"valid_from,omitempty"`
	ValidUntil     *string            `json:"valid_until,omitempty"`
	Status         string             `json:"status"`
	CreatedAt      time.Time          `json:"created_at"`
	Download       *storage.SignedURL `json:"download,omitempty"`
	ObjectKey      string             `json:"-"`
}

// Listing is the full equipment view.
type Listing struct {
	ID              uuid.UUID     `json:"id"`
	OwnerOrgID      uuid.UUID     `json:"owner_org_id"`
	OwnerName       string        `json:"owner_name"`
	OwnerVerified   bool          `json:"owner_verified"`
	ModelID         uuid.UUID     `json:"model_id"`
	ModelName       string        `json:"model_name"`
	MakeName        string        `json:"make_name"`
	CategoryID      uuid.UUID     `json:"category_id"`
	CategorySlug    string        `json:"category_slug"`
	CategoryName    string        `json:"category_name"`
	Title           string        `json:"title"`
	Description     string        `json:"description"`
	ManufactureYear *int16        `json:"manufacture_year,omitempty"`
	RegistrationNo  *string       `json:"registration_no,omitempty"`
	CurrentHMR      float64       `json:"current_hmr"`
	BaseLocation    GeoPoint      `json:"base_location"`
	CityID          uuid.NullUUID `json:"city_id"`
	CityName        string        `json:"city_name,omitempty"`
	LocationLabel   *string       `json:"location_label,omitempty"`
	ServiceRadiusKm int32         `json:"service_radius_km"`
	Condition       string        `json:"condition" doc:"excellent, good, fair or needs_repair"`
	// DeliveryAvailable: the owner transports the machine to site (priced
	// by the mobilisation rates); otherwise the customer collects it.
	DeliveryAvailable bool    `json:"delivery_available"`
	SerialNo          *string `json:"serial_no,omitempty"`
	// ModerationNote is the reviewer's reason (rejection, suspension,
	// removal), shown to the owner and admins only.
	ModerationNote *string       `json:"moderation_note,omitempty"`
	ModeratedAt    *time.Time    `json:"moderated_at,omitempty"`
	WetHire        bool          `json:"wet_hire"`
	DryHire        bool          `json:"dry_hire"`
	FuelPolicy     string        `json:"fuel_policy"`
	InstantBook    bool          `json:"instant_book"`
	Status         string        `json:"status"`
	RatingAvg      *float64      `json:"rating_avg,omitempty"`
	RatingCount    int32         `json:"rating_count"`
	Version        int32         `json:"version"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
	Pricing        []PricingPlan `json:"pricing"`
	Mobilisation   *Mobilisation `json:"mobilisation,omitempty"`
	Specs          []Spec        `json:"specs"`
	Photos         []Photo       `json:"photos"`
	// Documents are included for the owner and moderators only.
	Documents []Document `json:"documents,omitempty"`
}

// Summary is a listing card in lists (browse, my listings, moderation).
type Summary struct {
	ID                uuid.UUID `json:"id"`
	Title             string    `json:"title"`
	Status            string    `json:"status"`
	Version           int32     `json:"version"`
	InstantBook       bool      `json:"instant_book"`
	RatingAvg         *float64  `json:"rating_avg,omitempty"`
	RatingCount       int32     `json:"rating_count"`
	ModelName         string    `json:"model_name"`
	MakeName          string    `json:"make_name"`
	CategorySlug      string    `json:"category_slug"`
	CategoryName      string    `json:"category_name"`
	ManufactureYear   *int16    `json:"manufacture_year,omitempty"`
	Condition         string    `json:"condition"`
	LocationLabel     *string   `json:"location_label,omitempty"`
	CityName          string    `json:"city_name,omitempty"`
	DeliveryAvailable bool      `json:"delivery_available"`
	WetHire           bool      `json:"wet_hire"`
	DryHire           bool      `json:"dry_hire"`
	OwnerOrgID        uuid.UUID `json:"owner_org_id"`
	OwnerName         string    `json:"owner_name"`
	OwnerVerified     bool      `json:"owner_verified"`
	CoverURL          string    `json:"cover_url,omitempty" doc:"Signed, time-limited photo link"`
	PhotoCount        int32     `json:"photo_count"`
	PricePaise        int64     `json:"price_paise,omitempty" doc:"Lowest-unit price shown on cards (day preferred)"`
	PriceUnit         string    `json:"price_unit,omitempty"`
	ModerationNote    *string   `json:"moderation_note,omitempty"`
	CreatedAt         time.Time `json:"created_at"`
	CoverObjectKey    string    `json:"-"`
}

// StatusChange is the result of a lifecycle action.
type StatusChange struct {
	ID      uuid.UUID `json:"id"`
	Status  string    `json:"status"`
	Version int32     `json:"version"`
}

// Requests ----------------------------------------------------------------------------

// CreateRequest adds a draft listing. Identify the machine either by a
// catalogue model_id, or by category + manufacturer + model (new makes and
// models are added to the catalogue).
type CreateRequest struct {
	ModelID           *uuid.UUID    `json:"model_id"`
	Category          string        `json:"category" binding:"omitempty,max=60" doc:"Category slug, with manufacturer and model"`
	Manufacturer      string        `json:"manufacturer" binding:"omitempty,min=2,max=60"`
	ModelName         string        `json:"model" binding:"omitempty,min=1,max=80"`
	Condition         string        `json:"condition" binding:"omitempty,oneof=excellent good fair needs_repair"`
	DeliveryAvailable *bool         `json:"delivery_available" doc:"Default true"`
	LocationLabel     *string       `json:"location_label" binding:"omitempty,min=2,max=120" doc:"Area shown to customers, e.g. Hinjewadi, Pune"`
	Title             string        `json:"title" binding:"required,min=3,max=140"`
	Description       string        `json:"description" binding:"max=4000"`
	ManufactureYear   *int16        `json:"manufacture_year" binding:"omitempty,min=1980,max=2100"`
	RegistrationNo    *string       `json:"registration_no" binding:"omitempty,max=12" doc:"Without spaces, e.g. MH12KX4471"`
	SerialNo          *string       `json:"serial_no" binding:"omitempty,max=60"`
	CurrentHMR        float64       `json:"current_hmr" binding:"min=0"`
	BaseLocation      GeoPoint      `json:"base_location" binding:"required"`
	BaseAddressID     uuid.NullUUID `json:"base_address_id"`
	CityID            uuid.NullUUID `json:"city_id"`
	ServiceRadiusKm   int32         `json:"service_radius_km" binding:"omitempty,min=1,max=500"`
	WetHire           *bool         `json:"wet_hire" doc:"Hire with operator (default true)"`
	DryHire           bool          `json:"dry_hire"`
	FuelPolicy        string        `json:"fuel_policy" binding:"omitempty,oneof=customer owner shared"`
	InstantBook       bool          `json:"instant_book"`
}

type UpdateRequest struct {
	Version           int32      `json:"version" binding:"required,min=1" doc:"The version you last read (or send If-Match)"`
	ModelID           *uuid.UUID `json:"model_id"`
	Category          string     `json:"category" binding:"omitempty,max=60"`
	Manufacturer      string     `json:"manufacturer" binding:"omitempty,min=2,max=60"`
	ModelName         string     `json:"model" binding:"omitempty,min=1,max=80"`
	ManufactureYear   *int16     `json:"manufacture_year" binding:"omitempty,min=1980,max=2100"`
	RegistrationNo    *string    `json:"registration_no" binding:"omitempty,max=12"`
	FuelPolicy        *string    `json:"fuel_policy" binding:"omitempty,oneof=customer owner shared"`
	Condition         *string    `json:"condition" binding:"omitempty,oneof=excellent good fair needs_repair"`
	DeliveryAvailable *bool      `json:"delivery_available"`
	LocationLabel     *string    `json:"location_label" binding:"omitempty,min=2,max=120"`
	CityID            *uuid.UUID `json:"city_id"`
	Title             *string    `json:"title" binding:"omitempty,min=3,max=140"`
	Description       *string    `json:"description" binding:"omitempty,max=4000"`
	CurrentHMR        *float64   `json:"current_hmr" binding:"omitempty,min=0"`
	ServiceRadiusKm   *int32     `json:"service_radius_km" binding:"omitempty,min=1,max=500"`
	WetHire           *bool      `json:"wet_hire"`
	DryHire           *bool      `json:"dry_hire"`
	InstantBook       *bool      `json:"instant_book"`
	BaseLocation      *GeoPoint  `json:"base_location"`
}

type PricingRequest struct {
	Plans        []PricingPlan `json:"plans" binding:"required,min=1,max=10,dive"`
	Mobilisation *Mobilisation `json:"mobilisation"`
}

type SpecsRequest struct {
	Specs []Spec `json:"specs" binding:"required,max=50,dive"`
}

// AddPhotoRequest attaches an upload (purpose equipment_photo).
type AddPhotoRequest struct {
	UploadID uuid.UUID `json:"upload_id" binding:"required" doc:"From POST /uploads with purpose equipment_photo"`
	Angle    *string   `json:"angle" binding:"omitempty,oneof=front rear left right cabin hour_meter attachment other"`
	IsCover  bool      `json:"is_cover"`
}

// DocumentRequest attaches an upload (purpose equipment_document).
type DocumentRequest struct {
	DocType        string    `json:"doc_type" binding:"required,oneof=rc insurance fitness puc invoice permit load_test other"`
	UploadID       uuid.UUID `json:"upload_id" binding:"required"`
	DocumentNumber *string   `json:"document_number" binding:"omitempty,min=2,max=40"`
	ValidFrom      *string   `json:"valid_from" binding:"omitempty,datetime=2006-01-02"`
	ValidUntil     *string   `json:"valid_until" binding:"omitempty,datetime=2006-01-02"`
}

// BrowseQuery filters the public catalogue of approved listings.
type BrowseQuery struct {
	Category     string `form:"category" binding:"omitempty,max=60"`
	CityID       string `form:"city_id" binding:"omitempty,uuid"`
	Q            string `form:"q" binding:"omitempty,max=80"`
	Condition    string `form:"condition" binding:"omitempty,oneof=excellent good fair needs_repair"`
	DeliveryOnly bool   `form:"delivery_only"`
	VerifiedOnly bool   `form:"verified_only"`
	Limit        int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor       string `form:"cursor"`
}

// AdminCategory is a category as administrators see it.
type AdminCategory struct {
	Category
	NameHi     string `json:"name_hi,omitempty"`
	IsActive   bool   `json:"is_active"`
	ModelCount int32  `json:"model_count"`
}

type AdminCategoryList struct {
	Data []AdminCategory `json:"data"`
}

// CategoryRequest creates or edits a category.
type CategoryRequest struct {
	Slug       string          `json:"slug" binding:"omitempty,min=2,max=40" doc:"Create only; lower-case words joined by hyphens"`
	NameEn     string          `json:"name_en" binding:"required,min=2,max=60"`
	NameHi     string          `json:"name_hi" binding:"omitempty,max=60"`
	SortOrder  int32           `json:"sort_order" binding:"min=0,max=100000"`
	IsActive   *bool           `json:"is_active"`
	IconKey    *string         `json:"icon_key" binding:"omitempty,max=40"`
	SpecSchema json.RawMessage `json:"spec_schema" swaggertype:"object" doc:"Suggested spec keys: {\"operating_weight_kg\": \"number\"}"`
}

type ListQuery struct {
	Status string `form:"status" binding:"omitempty,oneof=draft pending_review active paused rejected suspended removed"`
	Limit  int    `form:"limit" binding:"omitempty,min=1,max=100"`
	Cursor string `form:"cursor"`
}

type CategoriesQuery struct {
	Lang string `form:"lang" binding:"omitempty,oneof=en hi mr ta te kn gu bn"`
}

type ModelsQuery struct {
	Category string `form:"category" binding:"omitempty,max=60"`
	MakeID   string `form:"make_id" binding:"omitempty,uuid"`
}

// List envelopes.
type CategoryList struct {
	Data []Category `json:"data"`
}
type MakeList struct {
	Data []Make `json:"data"`
}
type ModelList struct {
	Data []Model `json:"data"`
}
type CityList struct {
	Data []City `json:"data"`
}
type SummaryPage struct {
	Data       []Summary `json:"data"`
	NextCursor string    `json:"next_cursor,omitempty"`
}
