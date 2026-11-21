// Package users manages the signed-in user's profile, organisations,
// organisation members and saved addresses (sites and base yards).
package users

import (
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/httpx"
)

// Profile is the caller's account.
type Profile struct {
	ID                uuid.UUID    `json:"id"`
	Phone             string       `json:"phone"`
	Email             *string      `json:"email,omitempty"`
	FullName          string       `json:"full_name"`
	PreferredLanguage string       `json:"preferred_language"`
	Status            string       `json:"status"`
	CreatedAt         time.Time    `json:"created_at"`
	PhoneVerified     bool         `json:"phone_verified"`
	EmailVerified     bool         `json:"email_verified"`
	HasPassword       bool         `json:"has_password"`
	Roles             []httpx.Role `json:"roles"`
	AppRoles          []string     `json:"app_roles" doc:"CUSTOMER, EQUIPMENT_OWNER, OPERATOR, FLEET_MANAGER, ADMIN (derived server-side)"`
}

// Organization is a customer or supplier business (or a person).
type Organization struct {
	ID           uuid.UUID `json:"id"`
	LegalName    string    `json:"legal_name"`
	DisplayName  string    `json:"display_name"`
	OrgType      string    `json:"org_type"`
	IsBuyer      bool      `json:"is_buyer"`
	IsSupplier   bool      `json:"is_supplier"`
	GSTIN        *string   `json:"gstin,omitempty"`
	GSTStateCode *string   `json:"gst_state_code,omitempty"`
	KYCStatus    string    `json:"kyc_status"`
	RatingAvg    *float64  `json:"rating_avg,omitempty"`
	RatingCount  int32     `json:"rating_count"`
	CreatedAt    time.Time `json:"created_at"`
}

// Member is a user's role in an organisation.
type Member struct {
	UserID    uuid.UUID `json:"user_id"`
	FullName  string    `json:"full_name"`
	Phone     string    `json:"phone"`
	Role      string    `json:"role"`
	GrantedAt time.Time `json:"granted_at"`
	// Status is "invited" for an operator invitation awaiting the person's acceptance.
	Status       string     `json:"status,omitempty"`
	InvitationID *uuid.UUID `json:"invitation_id,omitempty"`
}

// Invitation is an offer to join an organisation, answered by the invitee.
type Invitation struct {
	ID               uuid.UUID `json:"id"`
	OrganizationID   uuid.UUID `json:"organization_id"`
	OrganizationName string    `json:"organization_name"`
	Role             string    `json:"role"`
	CreatedAt        time.Time `json:"created_at"`
	ExpiresAt        time.Time `json:"expires_at"`
}

type InvitationList struct {
	Data []Invitation `json:"data"`
}

// OrgInvitation is a pending invitation as the organisation sees it.
type OrgInvitation struct {
	ID        uuid.UUID `json:"id"`
	UserID    uuid.UUID `json:"user_id"`
	FullName  string    `json:"full_name"`
	Phone     string    `json:"phone"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

type OrgInvitationList struct {
	Data []OrgInvitation `json:"data"`
}

// Address is a saved site or yard.
type Address struct {
	ID             uuid.UUID `json:"id"`
	OrganizationID uuid.UUID `json:"organization_id"`
	Label          string    `json:"label"`
	Line1          string    `json:"line1"`
	Line2          *string   `json:"line2,omitempty"`
	Locality       *string   `json:"locality,omitempty"`
	City           string    `json:"city"`
	District       *string   `json:"district,omitempty"`
	StateCode      string    `json:"state_code"`
	Pincode        string    `json:"pincode"`
	Location       *GeoPoint `json:"location,omitempty"`
	IsSite         bool      `json:"is_site"`
	CreatedAt      time.Time `json:"created_at"`
}

// GeoPoint is a WGS84 coordinate.
type GeoPoint struct {
	Lat float64 `json:"lat" binding:"latitude"`
	Lng float64 `json:"lng" binding:"longitude"`
}

// Requests --------------------------------------------------------------------------

// UpdateProfileRequest edits the profile. Changing the email clears its
// verification (DB trigger). Roles and status cannot be set here: unknown
// fields such as "roles" are ignored.
type UpdateProfileRequest struct {
	FullName          *string `json:"full_name" binding:"omitempty,min=2,max=100"`
	Email             *string `json:"email" binding:"omitempty,email,max=254" doc:"Changing it requires verifying the new address"`
	PreferredLanguage *string `json:"preferred_language" binding:"omitempty,oneof=en hi mr ta te kn gu bn"`
}

type CreateOrgRequest struct {
	LegalName   string  `json:"legal_name" binding:"required,min=2,max=200"`
	DisplayName string  `json:"display_name" binding:"required,min=2,max=100"`
	OrgType     string  `json:"org_type" binding:"required,oneof=individual proprietorship partnership llp private_limited public_limited government other"`
	GSTIN       *string `json:"gstin" binding:"omitempty,len=15" doc:"15-character GSTIN; the state code is derived from it"`
}

type UpdateOrgRequest struct {
	LegalName   *string `json:"legal_name" binding:"omitempty,min=2,max=200"`
	DisplayName *string `json:"display_name" binding:"omitempty,min=2,max=100"`
	GSTIN       *string `json:"gstin" binding:"omitempty,len=15"`
}

type AddMemberRequest struct {
	Phone    string `json:"phone" binding:"required,e164"`
	Role     string `json:"role" binding:"required,oneof=customer owner fleet_manager operator org_admin"`
	FullName string `json:"full_name" binding:"omitempty,max=100" doc:"Used when the phone has no account yet"`
}

type RemoveMemberQuery struct {
	Role string `form:"role" binding:"required,oneof=customer owner fleet_manager operator org_admin"`
}

type CreateAddressRequest struct {
	Label     string    `json:"label" binding:"required,min=1,max=60"`
	Line1     string    `json:"line1" binding:"required,min=3,max=200"`
	Line2     *string   `json:"line2" binding:"omitempty,max=200"`
	Locality  *string   `json:"locality" binding:"omitempty,max=100"`
	City      string    `json:"city" binding:"required,max=100"`
	District  *string   `json:"district" binding:"omitempty,max=100"`
	StateCode string    `json:"state_code" binding:"required,statecode"`
	Pincode   string    `json:"pincode" binding:"required,pincode"`
	Location  *GeoPoint `json:"location"`
	IsSite    bool      `json:"is_site" doc:"Work sites need a location (used for pricing and geofencing)"`
}

// List envelopes.
type OrgList struct {
	Data []OrgMembership `json:"data"`
}

// OrgMembership is an organisation with the caller's roles in it.
type OrgMembership struct {
	Organization
	Roles []string `json:"roles"`
}

type MemberList struct {
	Data []Member `json:"data"`
}

type AddressList struct {
	Data []Address `json:"data"`
}
