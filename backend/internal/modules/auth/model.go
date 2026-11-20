// Package auth implements EQUIPRENT authentication: registration with
// password (Argon2id), password and mobile-OTP login, email and phone
// verification, password reset and change, JWT access tokens, rotating
// refresh tokens with reuse detection, session/device management, the
// request authentication middleware, lockout and audit logging.
//
// Security properties (docs/SECURITY.md):
//   - Roles never come from the client: registration only accepts the two
//     self-service account types, and every request reloads roles from the DB.
//   - Account enumeration: register, login, OTP request and password-reset
//     request answer identically whether or not the account exists.
//   - Brute force: per-identifier lockout plus per-IP rate limits; OTPs allow 5
//     attempts; unknown accounts cost the same Argon2id work as known ones.
package auth

import (
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/httpx"
)

// Domain models -------------------------------------------------------------------

// Account is a user as the auth service sees it.
type Account struct {
	ID              uuid.UUID
	Phone           string
	Email           *string
	FullName        string
	Language        string
	Status          string
	PasswordHash    *string
	PhoneVerifiedAt *time.Time
	EmailVerifiedAt *time.Time
}

// HasPassword reports whether a password is set.
func (a Account) HasPassword() bool { return a.PasswordHash != nil && *a.PasswordHash != "" }

// User is the identity returned after login.
type User struct {
	ID                uuid.UUID `json:"id"`
	Phone             string    `json:"phone"`
	Email             *string   `json:"email,omitempty"`
	FullName          string    `json:"full_name"`
	PreferredLanguage string    `json:"preferred_language"`
	Status            string    `json:"status"`
	PhoneVerified     bool      `json:"phone_verified"`
	EmailVerified     bool      `json:"email_verified"`
	HasPassword       bool      `json:"has_password"`
}

// Challenge is a stored OTP challenge.
type Challenge struct {
	ID        uuid.UUID
	CodeHash  []byte
	Attempts  int
	ExpiresAt time.Time
}

// Session is a refresh-token session (one per device login).
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	FamilyID  uuid.UUID
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// SessionInfo is a session as shown to its owner.
type SessionInfo struct {
	ID         uuid.UUID  `json:"id"`
	DeviceID   *string    `json:"device_id,omitempty"`
	Platform   *string    `json:"platform,omitempty"`
	UserAgent  *string    `json:"user_agent,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
	LastUsedAt *time.Time `json:"last_used_at,omitempty"`
	ExpiresAt  time.Time  `json:"expires_at"`
	Current    bool       `json:"current"`
}

// SessionState is what the middleware checks on every request.
type SessionState struct {
	UserID     uuid.UUID
	UserActive bool
	Revoked    bool
	ExpiresAt  time.Time
}

// NewSession describes a session to create.
type NewSession struct {
	UserID      uuid.UUID
	FamilyID    uuid.UUID
	RefreshHash []byte
	DeviceID    string
	Platform    string
	UserAgent   string
	ExpiresAt   time.Time
}

// NewAccount is a password registration ready to persist.
type NewAccount struct {
	FullName                string
	Phone                   string
	Email                   string
	PasswordHash            string
	AccountType             string
	OrgName                 string
	InspectionPolicyVersion string
}

// AuditEvent is a security event written to audit_logs.
type AuditEvent struct {
	Action string // auth.<event>
	UserID uuid.UUID
	Actor  uuid.UUID
	Reason string
	IP     string
	Data   map[string]any
}

// Public account types. Fleet manager and admin privileges are provisioned
// privately and are never granted by public registration.
const (
	AccountTypeCustomer       = "customer"
	AccountTypeEquipmentOwner = "equipment_owner"
	AccountTypeDriver         = "driver"
)

// DTOs ----------------------------------------------------------------------------

// RequestOTPRequest starts a mobile OTP login (or phone verification).
type RequestOTPRequest struct {
	Phone    string `json:"phone" binding:"required,e164" doc:"E.164 phone number, e.g. +919876543210"`
	Purpose  string `json:"purpose" binding:"omitempty,oneof=login" doc:"Only login is supported for now"`
	DeviceID string `json:"device_id" binding:"omitempty,max=128"`
}

// RequestOTPResponse acknowledges the OTP. It looks the same for every
// phone number.
type RequestOTPResponse struct {
	ExpiresAt         time.Time `json:"expires_at"`
	ResendAfterSecond int       `json:"resend_after_seconds"`
	DevCode           string    `json:"dev_code,omitempty" doc:"Development simulation only: the OTP, instead of an SMS"`
}

// VerifyOTPRequest completes an OTP login and proves phone ownership.
type VerifyOTPRequest struct {
	Phone    string `json:"phone" binding:"required,e164"`
	Code     string `json:"code" binding:"required,len=6,numeric"`
	DeviceID string `json:"device_id" binding:"omitempty,max=128"`
	Platform string `json:"platform" binding:"omitempty,oneof=android ios web"`
}

// RegisterRequest creates a password account. There is deliberately no
// role field: account_type selects a server-controlled setup.
type RegisterRequest struct {
	FullName                string `json:"full_name" binding:"required,min=2,max=100"`
	Email                   string `json:"email" binding:"required,email,max=254"`
	Phone                   string `json:"phone" binding:"required,e164"`
	Password                string `json:"password" binding:"required,min=10,max=128"`
	AccountType             string `json:"account_type" binding:"required,oneof=customer equipment_owner driver" doc:"customer, equipment_owner or driver; fleet managers and admins are provisioned privately"`
	OrganizationName        string `json:"organization_name" binding:"required_if=AccountType equipment_owner,omitempty,min=2,max=100" doc:"Business name (equipment owners)"`
	AcceptTerms             bool   `json:"accept_terms" binding:"required" doc:"Must be true"`
	InspectionPolicyVersion string `json:"inspection_policy_version" binding:"required,max=40"`
	AcceptInspectionPolicy  bool   `json:"accept_inspection_policy" binding:"required"`
}

const EquipmentInspectionPolicyVersion = "2026-11-18-v1"

// RegisterResponse is identical for new and already-registered details.
type RegisterResponse struct {
	Status        string `json:"status" doc:"Always verification_required"`
	Message       string `json:"message"`
	DevOTP        string `json:"dev_otp,omitempty" doc:"Development simulation only"`
	DevEmailToken string `json:"dev_email_token,omitempty" doc:"Development simulation only"`
	// DevNotice explains why no code was issued (details already registered).
	// Development simulation only: production never reveals this.
	DevNotice string `json:"dev_notice,omitempty" doc:"Development simulation only"`
}

// LoginRequest signs in with a password. identifier is an email address or
// an E.164 phone number (a 10-digit Indian number gets +91).
type LoginRequest struct {
	Identifier string `json:"identifier" binding:"required,min=5,max=254"`
	Password   string `json:"password" binding:"required,max=128"`
	DeviceID   string `json:"device_id" binding:"omitempty,max=128"`
	Platform   string `json:"platform" binding:"omitempty,oneof=android ios web"`
}

// TokenResponse is returned by every successful sign-in and refresh.
type TokenResponse struct {
	AccessToken      string       `json:"access_token"`
	AccessExpiresAt  time.Time    `json:"access_expires_at"`
	RefreshToken     string       `json:"refresh_token"`
	RefreshExpiresAt time.Time    `json:"refresh_expires_at"`
	TokenType        string       `json:"token_type"`
	User             User         `json:"user"`
	Roles            []httpx.Role `json:"roles"`
	AppRoles         []string     `json:"app_roles" doc:"CUSTOMER, EQUIPMENT_OWNER, OPERATOR, FLEET_MANAGER, ADMIN (derived server-side)"`
	IsNew            bool         `json:"is_new"`
}

// RefreshRequest rotates a refresh token.
type RefreshRequest struct {
	RefreshToken string `json:"refresh_token" binding:"required,min=20,max=200"`
}

// SessionsResponse lists active sessions.
type SessionsResponse struct {
	Data []SessionInfo `json:"data"`
}

// ForgotPasswordRequest starts a reset by email link or SMS code.
type ForgotPasswordRequest struct {
	Identifier string `json:"identifier" binding:"required,min=5,max=254"`
}

// ForgotPasswordResponse is identical for known and unknown identifiers.
type ForgotPasswordResponse struct {
	Message  string `json:"message"`
	DevToken string `json:"dev_token,omitempty" doc:"Development simulation only (email reset)"`
	DevCode  string `json:"dev_code,omitempty" doc:"Development simulation only (SMS reset)"`
}

// ResetPasswordRequest completes a reset with the emailed token or the SMS code.
type ResetPasswordRequest struct {
	Token       string `json:"token" binding:"required_without=Phone,omitempty,min=20,max=200"`
	Phone       string `json:"phone" binding:"required_without=Token,omitempty,e164"`
	Code        string `json:"code" binding:"required_with=Phone,omitempty,len=6,numeric"`
	NewPassword string `json:"new_password" binding:"required,min=10,max=128"`
}

// ChangePasswordRequest changes (or first sets) the password.
type ChangePasswordRequest struct {
	CurrentPassword string `json:"current_password" binding:"max=128" doc:"Required when a password is already set"`
	NewPassword     string `json:"new_password" binding:"required,min=10,max=128"`
}

// VerifyEmailRequest proves email ownership.
type VerifyEmailRequest struct {
	Token string `json:"token" binding:"required,min=6,max=200"`
}

// EmailVerificationResponse acknowledges a sent verification link.
type EmailVerificationResponse struct {
	Sent     bool   `json:"sent"`
	DevToken string `json:"dev_token,omitempty" doc:"Development simulation only"`
}

// StatusResponse is a simple acknowledgement.
type StatusResponse struct {
	Status string `json:"status"`
}

// Capabilities tells clients which sign-in methods are available.
type Capabilities struct {
	PasswordLogin     bool `json:"password_login"`
	MobileOTP         bool `json:"mobile_otp"`
	EmailVerification bool `json:"email_verification"`
	DevSimulation     bool `json:"dev_simulation" doc:"OTPs and email tokens are returned in responses (never in production)"`
	PasswordMinLength int  `json:"password_min_length"`
}
