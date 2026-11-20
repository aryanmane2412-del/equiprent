package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/password"
)

const (
	maxAttempts         = 5
	resendCooldown      = 30 * time.Second
	otpPerPhoneLimit    = 5 // per hour
	emailResendCooldown = time.Minute
	resetRequestLimit   = 5 // per identifier per hour

	purposeLogin       = "login"
	purposeReset       = "password_reset"
	tokenEmailVerify   = "email_verify"
	tokenPasswordReset = "password_reset"
)

// API errors. Messages never reveal whether an account exists.
var (
	errInvalidCode        = apperror.New(401, "OTP_INVALID", "The code is incorrect or has expired")
	errTooManyTries       = apperror.New(429, "OTP_ATTEMPTS_EXCEEDED", "Too many wrong codes; request a new one")
	errResendTooSoon      = apperror.New(429, "OTP_RESEND_TOO_SOON", "Please wait before requesting another code")
	errOTPUnavailable     = apperror.New(503, "OTP_UNAVAILABLE", "Mobile OTP is not available yet; sign in with your email and password")
	errEmailUnavailable   = apperror.New(503, "EMAIL_UNAVAILABLE", "Email delivery is not available yet")
	errInvalidCredentials = apperror.New(401, "INVALID_CREDENTIALS", "The email/phone or password is incorrect")
	errLocked             = apperror.New(429, "ACCOUNT_TEMPORARILY_LOCKED", "Too many sign-in attempts; try again later")
	errNotVerified        = apperror.New(403, "VERIFICATION_REQUIRED", "Verify this email address or phone number before signing in with it")
	errSuspended          = apperror.New(403, "ACCOUNT_SUSPENDED", "This account is suspended; contact support")
	errTokenInvalid       = apperror.New(400, "TOKEN_INVALID", "The link or code is invalid or has expired")
	errWrongPassword      = apperror.New(400, "CURRENT_PASSWORD_INCORRECT", "The current password is incorrect")
	errSessionEnded       = apperror.ErrUnauthenticated.WithDetail("the session has ended; sign in again")
)

// Service implements the auth use cases.
type Service struct {
	repo   Repository
	tokens *Tokens
	store  cache.Store
	sms    SMSSender
	email  EmailSender
	cfg    config.AuthConfig
	log    *slog.Logger
	hash   password.Params
	now    func() time.Time
}

// NewService wires the auth service. Pass DevSimulator senders only when
// cfg.DevSimulation is on; otherwise the Disabled* senders (until a real
// provider is integrated).
func NewService(repo Repository, tokens *Tokens, store cache.Store, sms SMSSender, email EmailSender, cfg config.AuthConfig, log *slog.Logger) *Service {
	return &Service{repo: repo, tokens: tokens, store: store, sms: sms, email: email, cfg: cfg, log: log, hash: password.Default, now: time.Now}
}

// Capabilities reports the available sign-in methods.
func (s *Service) Capabilities() Capabilities {
	return Capabilities{
		PasswordLogin: true, MobileOTP: s.cfg.OTPEnabled(), EmailVerification: s.cfg.EmailEnabled(),
		DevSimulation: s.cfg.DevSimulation, PasswordMinLength: password.MinLength,
	}
}

// ClientInfo describes the calling device.
type ClientInfo struct {
	DeviceID  string
	Platform  string
	UserAgent string
	IP        string
}

func (s *Service) audit(ctx context.Context, e AuditEvent) {
	if e.Actor == uuid.Nil {
		e.Actor = e.UserID
	}
	if err := s.repo.Audit(ctx, e); err != nil {
		s.log.ErrorContext(ctx, "writing auth audit event failed", "action", e.Action, "error", err)
	}
}

// ---- OTP -----------------------------------------------------------------------------

// issueOTP throttles, stores and sends a code. The returned code is only
// used for dev simulation.
func (s *Service) issueOTP(ctx context.Context, phone, purpose, deviceID string) (Challenge, string, error) {
	if !s.cfg.OTPEnabled() {
		return Challenge{}, "", errOTPUnavailable
	}
	ok, err := s.store.SetNX(ctx, "otp:cooldown:"+purpose+":"+phone, []byte{1}, resendCooldown)
	if err != nil {
		return Challenge{}, "", err
	}
	if !ok {
		return Challenge{}, "", errResendTooSoon.WithRetryAfter(resendCooldown)
	}
	n, ttl, err := s.store.Incr(ctx, "otp:hourly:"+phone, time.Hour)
	if err != nil {
		return Challenge{}, "", err
	}
	if n > otpPerPhoneLimit {
		return Challenge{}, "", apperror.ErrRateLimited.WithDetail("too many codes requested for this number").WithRetryAfter(ttl)
	}
	code, err := NewOTP()
	if err != nil {
		return Challenge{}, "", err
	}
	ch, err := s.repo.CreateChallenge(ctx, phone, purpose, HashOTP([]byte(s.cfg.OTPSecret), phone, purpose, code), deviceID, s.now().Add(s.cfg.OTPTTL))
	if err != nil {
		return Challenge{}, "", err
	}
	if err := s.sms.SendOTP(ctx, phone, code, purpose); err != nil {
		return Challenge{}, "", apperror.ErrServiceUnavailable.WithDetail("could not send the code; try again").Wrap(err)
	}
	return ch, code, nil
}

// checkOTP verifies and consumes the latest code for phone + purpose.
func (s *Service) checkOTP(ctx context.Context, phone, purpose, code string) error {
	if !s.cfg.OTPEnabled() {
		return errOTPUnavailable
	}
	ch, err := s.repo.LatestChallenge(ctx, phone, purpose)
	if errors.Is(err, apperror.ErrNotFound) {
		return errInvalidCode
	}
	if err != nil {
		return err
	}
	if ch.Attempts >= maxAttempts {
		return errTooManyTries
	}
	if !hmac.Equal(HashOTP([]byte(s.cfg.OTPSecret), phone, purpose, code), ch.CodeHash) {
		attempts, err := s.repo.IncrementAttempts(ctx, ch.ID)
		if err != nil {
			return err
		}
		if attempts >= maxAttempts {
			return errTooManyTries
		}
		return errInvalidCode
	}
	consumed, err := s.repo.ConsumeChallenge(ctx, ch.ID)
	if err != nil {
		return err
	}
	if !consumed { // a concurrent verify won the race
		return errInvalidCode
	}
	return nil
}

// RequestOTP sends a login code. The answer is the same for every number.
func (s *Service) RequestOTP(ctx context.Context, req RequestOTPRequest) (RequestOTPResponse, error) {
	ch, code, err := s.issueOTP(ctx, req.Phone, purposeLogin, req.DeviceID)
	if err != nil {
		return RequestOTPResponse{}, err
	}
	resp := RequestOTPResponse{ExpiresAt: ch.ExpiresAt, ResendAfterSecond: int(resendCooldown.Seconds())}
	if s.cfg.DevSimulation {
		resp.DevCode = code
	}
	return resp, nil
}

// VerifyOTP signs in with a phone code (creating a customer account for a
// new number) and marks the phone verified.
func (s *Service) VerifyOTP(ctx context.Context, req VerifyOTPRequest, client ClientInfo) (TokenResponse, error) {
	if err := s.checkOTP(ctx, req.Phone, purposeLogin, req.Code); err != nil {
		s.audit(ctx, AuditEvent{Action: "auth.otp_failed", IP: client.IP, Reason: apperror.From(err).Code, Data: map[string]any{"phone": MaskPhone(req.Phone)}})
		return TokenResponse{}, err
	}
	isNew := false
	acct, err := s.repo.AccountByPhone(ctx, req.Phone)
	if errors.Is(err, apperror.ErrNotFound) {
		acct, err = s.repo.CreateUserWithPersonalOrg(ctx, req.Phone)
		isNew = true
	}
	if err != nil {
		return TokenResponse{}, err
	}
	if acct.Status != "active" {
		s.audit(ctx, AuditEvent{Action: "auth.login_blocked", UserID: acct.ID, IP: client.IP, Reason: acct.Status})
		return TokenResponse{}, errSuspended
	}
	if err := s.repo.MarkPhoneVerified(ctx, acct.ID); err != nil {
		return TokenResponse{}, err
	}
	now := s.now()
	acct.PhoneVerifiedAt = &now
	if client.DeviceID == "" {
		client.DeviceID = req.DeviceID
	}
	if req.Platform != "" {
		client.Platform = req.Platform
	}
	resp, err := s.startSession(ctx, acct, uuid.Must(uuid.NewV7()), client)
	if err != nil {
		return TokenResponse{}, err
	}
	resp.IsNew = isNew
	s.audit(ctx, AuditEvent{Action: "auth.login", UserID: acct.ID, IP: client.IP, Data: map[string]any{"method": "otp", "new_account": isNew}})
	return resp, nil
}

// ---- registration ----------------------------------------------------------------------

const registerMessage = "If these details are new, your account has been created: we sent a code to your phone and a verification link to your email. If you already have an account, sign in or reset your password."

// Register creates a password account. To prevent account enumeration the
// response is identical when the phone or email is already registered (the
// existing owner gets a notice instead), and the same password hashing work
// is done either way.
func (s *Service) Register(ctx context.Context, req RegisterRequest, client ClientInfo) (RegisterResponse, error) {
	if !req.AcceptInspectionPolicy || req.InspectionPolicyVersion != EquipmentInspectionPolicyVersion {
		return RegisterResponse{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "accept_inspection_policy", Code: "required", Message: "read and accept the current equipment inspection policy"})
	}
	if req.AccountType != AccountTypeCustomer && req.AccountType != AccountTypeEquipmentOwner && req.AccountType != AccountTypeDriver {
		return RegisterResponse{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "account_type", Code: "invalid",
			Message: "choose customer, equipment_owner or driver"})
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	name := strings.TrimSpace(req.FullName)
	if err := password.Check(req.Password, name, strings.SplitN(email, "@", 2)[0], req.Phone[3:]); err != nil {
		return RegisterResponse{}, apperror.ErrValidation.WithFields(apperror.FieldError{Field: "password", Code: "weak", Message: err.Error()})
	}
	hash, err := password.Hash(req.Password, s.hash)
	if err != nil {
		return RegisterResponse{}, err
	}
	generic := RegisterResponse{Status: "verification_required", Message: registerMessage}

	byPhone, perr := s.repo.AccountByPhone(ctx, req.Phone)
	byEmail, eerr := s.repo.AccountByEmail(ctx, email)
	if perr == nil || eerr == nil {
		if perr == nil {
			_ = s.sms.SendNotice(ctx, byPhone.Phone, "Someone tried to create an EQUIPRENT account with your phone number. If it was you, sign in or reset your password.")
		}
		if eerr == nil && byEmail.Email != nil {
			_ = s.email.Send(ctx, *byEmail.Email, "EQUIPRENT: sign-up attempt", "Someone tried to create an account with this email address. If it was you, sign in or reset your password.")
		}
		s.audit(ctx, AuditEvent{Action: "auth.register_duplicate", UserID: firstID(byPhone, byEmail), IP: client.IP})
		if s.cfg.DevSimulation {
			generic.DevNotice = duplicateNotice(perr == nil, eerr == nil)
		}
		return generic, nil
	}
	if !errors.Is(perr, apperror.ErrNotFound) {
		return RegisterResponse{}, perr
	}
	if !errors.Is(eerr, apperror.ErrNotFound) {
		return RegisterResponse{}, eerr
	}

	acct, err := s.repo.Register(ctx, NewAccount{
		FullName: name, Phone: req.Phone, Email: email, PasswordHash: hash,
		AccountType: req.AccountType, OrgName: strings.TrimSpace(req.OrganizationName), InspectionPolicyVersion: req.InspectionPolicyVersion,
	})
	if errors.Is(err, apperror.ErrConflict) { // registered concurrently
		return generic, nil
	}
	if err != nil {
		return RegisterResponse{}, err
	}
	s.audit(ctx, AuditEvent{Action: "auth.register", UserID: acct.ID, IP: client.IP, Data: map[string]any{"account_type": req.AccountType}})

	out := generic
	if _, code, err := s.issueOTP(ctx, req.Phone, purposeLogin, client.DeviceID); err == nil && s.cfg.DevSimulation {
		out.DevOTP = code
	}
	if token, err := s.sendEmailToken(ctx, acct.ID, email); err == nil && s.cfg.DevSimulation {
		out.DevEmailToken = token
	}
	return out, nil
}

// duplicateNotice tells a developer why registration issued no code. Only
// returned in development simulation, where account enumeration is harmless.
func duplicateNotice(phone, email bool) string {
	what := "This mobile number and email are"
	switch {
	case phone && !email:
		what = "This mobile number is"
	case email && !phone:
		what = "This email address is"
	}
	return what + " already registered, so no new account was created and no code was sent. Sign in with that account's password, use \"Forgot password\", or register with different details."
}

func firstID(a, b Account) uuid.UUID {
	if a.ID != uuid.Nil {
		return a.ID
	}
	return b.ID
}

// ---- password login ------------------------------------------------------------------------

var tenDigits = regexp.MustCompile(`^[6-9][0-9]{9}$`)

// normaliseIdentifier returns ("email", address) or ("phone", E.164).
func normaliseIdentifier(raw string) (string, string, error) {
	id := strings.TrimSpace(raw)
	if strings.Contains(id, "@") {
		return "email", strings.ToLower(id), nil
	}
	digits := strings.NewReplacer(" ", "", "-", "").Replace(id)
	if tenDigits.MatchString(digits) {
		digits = "+91" + digits
	}
	if !httpx.E164(digits) {
		return "", "", apperror.ErrValidation.WithFields(apperror.FieldError{Field: "identifier", Code: "identifier", Message: "enter an email address or mobile number"})
	}
	return "phone", digits, nil
}

func lockKey(identifier string) string {
	sum := sha256.Sum256([]byte(identifier))
	return "auth:attempts:" + hex.EncodeToString(sum[:16])
}

// Login signs in with a password. Unknown accounts, accounts without a
// password and wrong passwords all fail with the same error after the same
// amount of hashing work. Each identifier gets LoginMaxAttempts tries per
// LoginWindow (counted whether or not the account exists).
func (s *Service) Login(ctx context.Context, req LoginRequest, client ClientInfo) (TokenResponse, error) {
	kind, id, err := normaliseIdentifier(req.Identifier)
	if err != nil {
		return TokenResponse{}, err
	}
	n, ttl, err := s.store.Incr(ctx, lockKey(id), s.cfg.LoginWindow)
	if err != nil {
		return TokenResponse{}, err
	}
	if n > int64(s.cfg.LoginMaxAttempts) {
		if n == int64(s.cfg.LoginMaxAttempts)+1 {
			s.audit(ctx, AuditEvent{Action: "auth.lockout", IP: client.IP, Data: map[string]any{"identifier_kind": kind}})
		}
		return TokenResponse{}, errLocked.WithRetryAfter(ttl)
	}

	var acct Account
	if kind == "email" {
		acct, err = s.repo.AccountByEmail(ctx, id)
	} else {
		acct, err = s.repo.AccountByPhone(ctx, id)
	}
	if err != nil && !errors.Is(err, apperror.ErrNotFound) {
		return TokenResponse{}, err
	}
	if err != nil || !acct.HasPassword() {
		password.VerifyDummy(req.Password)
		s.audit(ctx, AuditEvent{Action: "auth.login_failed", UserID: acct.ID, IP: client.IP, Reason: "unknown account or no password"})
		return TokenResponse{}, errInvalidCredentials
	}
	ok, err := password.Verify(req.Password, *acct.PasswordHash)
	if err != nil || !ok {
		s.audit(ctx, AuditEvent{Action: "auth.login_failed", UserID: acct.ID, IP: client.IP, Reason: "wrong password"})
		return TokenResponse{}, errInvalidCredentials
	}
	// The password is correct: account state can be disclosed now.
	if acct.Status != "active" {
		s.audit(ctx, AuditEvent{Action: "auth.login_blocked", UserID: acct.ID, IP: client.IP, Reason: acct.Status})
		return TokenResponse{}, errSuspended
	}
	if (kind == "email" && acct.EmailVerifiedAt == nil) || (kind == "phone" && acct.PhoneVerifiedAt == nil) {
		s.audit(ctx, AuditEvent{Action: "auth.login_blocked", UserID: acct.ID, IP: client.IP, Reason: "unverified " + kind})
		return TokenResponse{}, errNotVerified
	}
	_ = s.store.Delete(ctx, lockKey(id))
	if password.NeedsRehash(*acct.PasswordHash, s.hash) {
		if h, err := password.Hash(req.Password, s.hash); err == nil {
			_ = s.repo.SetPassword(ctx, acct.ID, h)
		}
	}
	client.DeviceID, client.Platform = firstNonEmpty(req.DeviceID, client.DeviceID), firstNonEmpty(req.Platform, client.Platform)
	resp, err := s.startSession(ctx, acct, uuid.Must(uuid.NewV7()), client)
	if err != nil {
		return TokenResponse{}, err
	}
	s.audit(ctx, AuditEvent{Action: "auth.login", UserID: acct.ID, IP: client.IP, Data: map[string]any{"method": "password", "identifier_kind": kind}})
	return resp, nil
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ---- sessions ---------------------------------------------------------------------------

func (s *Service) startSession(ctx context.Context, acct Account, family uuid.UUID, client ClientInfo) (TokenResponse, error) {
	if err := s.repo.MarkLoggedIn(ctx, acct.ID); err != nil {
		return TokenResponse{}, err
	}
	refresh, hash, err := NewRefreshToken()
	if err != nil {
		return TokenResponse{}, err
	}
	sess, err := s.repo.CreateSession(ctx, NewSession{
		UserID: acct.ID, FamilyID: family, RefreshHash: hash,
		DeviceID: client.DeviceID, Platform: validPlatform(client.Platform), UserAgent: client.UserAgent,
		ExpiresAt: s.now().Add(s.cfg.RefreshTokenTTL),
	})
	if err != nil {
		return TokenResponse{}, err
	}
	access, accessExp, err := s.tokens.Issue(acct.ID, sess.ID)
	if err != nil {
		return TokenResponse{}, err
	}
	roles, err := s.repo.Roles(ctx, acct.ID)
	if err != nil {
		return TokenResponse{}, err
	}
	return TokenResponse{
		AccessToken: access, AccessExpiresAt: accessExp, RefreshToken: refresh, RefreshExpiresAt: sess.ExpiresAt,
		TokenType: "Bearer", User: ToUser(acct), Roles: roles, AppRoles: httpx.AppRoles(roles),
	}, nil
}

func validPlatform(p string) string {
	switch p {
	case "android", "ios", "web":
		return p
	}
	return ""
}

// ToUser builds the public user view.
func ToUser(a Account) User {
	return User{
		ID: a.ID, Phone: a.Phone, Email: a.Email, FullName: a.FullName, PreferredLanguage: a.Language, Status: a.Status,
		PhoneVerified: a.PhoneVerifiedAt != nil, EmailVerified: a.EmailVerifiedAt != nil, HasPassword: a.HasPassword(),
	}
}

// Refresh rotates a refresh token. Presenting an already-rotated token is
// treated as theft: the whole session family is revoked.
func (s *Service) Refresh(ctx context.Context, token string, client ClientInfo) (TokenResponse, error) {
	sess, err := s.repo.SessionByRefreshHash(ctx, HashRefreshToken(token))
	if errors.Is(err, apperror.ErrNotFound) {
		return TokenResponse{}, errSessionEnded
	}
	if err != nil {
		return TokenResponse{}, err
	}
	if sess.RevokedAt != nil {
		s.log.WarnContext(ctx, "refresh token reuse detected; revoking session family", "user_id", sess.UserID, "family_id", sess.FamilyID)
		if err := s.repo.RevokeFamily(ctx, sess.FamilyID); err != nil {
			return TokenResponse{}, err
		}
		s.audit(ctx, AuditEvent{Action: "auth.refresh_reuse", UserID: sess.UserID, IP: client.IP, Data: map[string]any{"family_id": sess.FamilyID}})
		return TokenResponse{}, errSessionEnded
	}
	if !s.now().Before(sess.ExpiresAt) {
		return TokenResponse{}, errSessionEnded
	}
	revoked, err := s.repo.RevokeSession(ctx, sess.ID)
	if err != nil {
		return TokenResponse{}, err
	}
	if !revoked { // concurrent refresh with the same token
		return TokenResponse{}, errSessionEnded
	}
	acct, err := s.repo.AccountByID(ctx, sess.UserID)
	if err != nil {
		return TokenResponse{}, errSessionEnded
	}
	if acct.Status != "active" {
		return TokenResponse{}, errSuspended
	}
	return s.startSession(ctx, acct, sess.FamilyID, client)
}

// Logout revokes the caller's current session.
func (s *Service) Logout(ctx context.Context, p *httpx.Principal, client ClientInfo) error {
	if _, err := s.repo.RevokeSession(ctx, p.SessionID); err != nil {
		return err
	}
	s.audit(ctx, AuditEvent{Action: "auth.logout", UserID: p.UserID, IP: client.IP})
	return nil
}

// Sessions lists the caller's active sessions.
func (s *Service) Sessions(ctx context.Context, p *httpx.Principal) ([]SessionInfo, error) {
	list, err := s.repo.ListSessions(ctx, p.UserID)
	for i := range list {
		list[i].Current = list[i].ID == p.SessionID
	}
	return list, err
}

// RevokeSession signs out one of the caller's devices.
func (s *Service) RevokeSession(ctx context.Context, p *httpx.Principal, id uuid.UUID, client ClientInfo) error {
	ok, err := s.repo.RevokeUserSession(ctx, id, p.UserID)
	if err != nil {
		return err
	}
	if !ok {
		return apperror.ErrNotFound.WithDetail("no active session with this id")
	}
	s.audit(ctx, AuditEvent{Action: "auth.session_revoked", UserID: p.UserID, IP: client.IP, Data: map[string]any{"session_id": id}})
	return nil
}

// RevokeOtherSessions signs out every device except the current one.
func (s *Service) RevokeOtherSessions(ctx context.Context, p *httpx.Principal, client ClientInfo) error {
	if err := s.repo.RevokeOtherSessions(ctx, p.UserID, p.SessionID); err != nil {
		return err
	}
	s.audit(ctx, AuditEvent{Action: "auth.sessions_revoked", UserID: p.UserID, IP: client.IP, Reason: "sign out other devices"})
	return nil
}

// Authenticate validates an access token, checks its session is still live
// and the account active, and loads the caller's roles from the database.
func (s *Service) Authenticate(ctx context.Context, token string) (*httpx.Principal, error) {
	userID, sessionID, err := s.tokens.Parse(token)
	if err != nil {
		return nil, apperror.ErrUnauthenticated.WithDetail("the access token is invalid or expired")
	}
	state, err := s.repo.SessionState(ctx, sessionID)
	if errors.Is(err, apperror.ErrNotFound) {
		return nil, errSessionEnded
	}
	if err != nil {
		return nil, err
	}
	if state.UserID != userID || state.Revoked || !s.now().Before(state.ExpiresAt) {
		return nil, errSessionEnded
	}
	if !state.UserActive {
		return nil, errSuspended
	}
	roles, err := s.repo.Roles(ctx, userID)
	if err != nil {
		return nil, err
	}
	return &httpx.Principal{UserID: userID, SessionID: sessionID, Roles: roles}, nil
}

// ---- password reset / change ------------------------------------------------------------

const forgotMessage = "If an account matches, we have sent password reset instructions to it."

// ForgotPassword sends a reset link (verified email) or code (verified
// phone). The response never reveals whether an account matched.
func (s *Service) ForgotPassword(ctx context.Context, req ForgotPasswordRequest, client ClientInfo) (ForgotPasswordResponse, error) {
	kind, id, err := normaliseIdentifier(req.Identifier)
	if err != nil {
		return ForgotPasswordResponse{}, err
	}
	out := ForgotPasswordResponse{Message: forgotMessage}
	n, _, err := s.store.Incr(ctx, "auth:reset:"+lockKey(id), time.Hour)
	if err != nil {
		return ForgotPasswordResponse{}, err
	}
	if n > resetRequestLimit {
		return out, nil // silently throttled: same answer
	}
	var acct Account
	if kind == "email" {
		acct, err = s.repo.AccountByEmail(ctx, id)
	} else {
		acct, err = s.repo.AccountByPhone(ctx, id)
	}
	if err != nil || acct.Status != "active" {
		if err != nil && !errors.Is(err, apperror.ErrNotFound) {
			return ForgotPasswordResponse{}, err
		}
		return out, nil
	}
	switch {
	case kind == "email" && acct.EmailVerifiedAt != nil && s.cfg.EmailEnabled():
		if err := s.repo.InvalidateTokens(ctx, acct.ID, tokenPasswordReset); err != nil {
			return ForgotPasswordResponse{}, err
		}
		token, hash, err := newURLToken()
		if err != nil {
			return ForgotPasswordResponse{}, err
		}
		if err := s.repo.CreateToken(ctx, acct.ID, tokenPasswordReset, hash, id, s.now().Add(s.cfg.ResetTokenTTL)); err != nil {
			return ForgotPasswordResponse{}, err
		}
		if err := s.email.Send(ctx, id, "Reset your EQUIPRENT password", "Use this code to reset your password: "+token); err == nil && s.cfg.DevSimulation {
			out.DevToken = token
		}
	case kind == "phone" && acct.PhoneVerifiedAt != nil && s.cfg.OTPEnabled():
		if _, code, err := s.issueOTP(ctx, id, purposeReset, ""); err == nil && s.cfg.DevSimulation {
			out.DevCode = code
		}
	}
	s.audit(ctx, AuditEvent{Action: "auth.password_reset_requested", UserID: acct.ID, IP: client.IP, Data: map[string]any{"via": kind}})
	return out, nil
}

// ResetPassword sets a new password with an emailed token or an SMS code,
// then signs the account out everywhere.
func (s *Service) ResetPassword(ctx context.Context, req ResetPasswordRequest, client ClientInfo) (StatusResponse, error) {
	var userID uuid.UUID
	via := "email"
	if req.Token != "" {
		id, _, err := s.repo.ConsumeToken(ctx, HashRefreshToken(req.Token), tokenPasswordReset)
		if errors.Is(err, apperror.ErrNotFound) {
			return StatusResponse{}, errTokenInvalid
		}
		if err != nil {
			return StatusResponse{}, err
		}
		userID = id
	} else {
		via = "phone"
		if err := s.checkOTP(ctx, req.Phone, purposeReset, req.Code); err != nil {
			return StatusResponse{}, err
		}
		acct, err := s.repo.AccountByPhone(ctx, req.Phone)
		if err != nil {
			return StatusResponse{}, errTokenInvalid
		}
		userID = acct.ID
	}
	acct, err := s.repo.AccountByID(ctx, userID)
	if err != nil || acct.Status != "active" {
		return StatusResponse{}, errTokenInvalid
	}
	if err := s.setPassword(ctx, acct, req.NewPassword); err != nil {
		return StatusResponse{}, err
	}
	if err := s.repo.RevokeAllSessions(ctx, acct.ID); err != nil {
		return StatusResponse{}, err
	}
	_ = s.repo.InvalidateTokens(ctx, acct.ID, tokenPasswordReset)
	if acct.Email != nil {
		_ = s.store.Delete(ctx, lockKey(*acct.Email))
		_ = s.email.Send(ctx, *acct.Email, "Your EQUIPRENT password was changed", "Your password was reset and all devices were signed out. If this wasn't you, contact support immediately.")
	}
	_ = s.store.Delete(ctx, lockKey(acct.Phone))
	s.audit(ctx, AuditEvent{Action: "auth.password_reset", UserID: acct.ID, IP: client.IP, Data: map[string]any{"via": via}})
	return StatusResponse{Status: "password_reset"}, nil
}

// ChangePassword changes (or first sets) the caller's password and signs
// out their other devices.
func (s *Service) ChangePassword(ctx context.Context, p *httpx.Principal, req ChangePasswordRequest, client ClientInfo) (StatusResponse, error) {
	acct, err := s.repo.AccountByID(ctx, p.UserID)
	if err != nil {
		return StatusResponse{}, err
	}
	if acct.HasPassword() {
		n, ttl, err := s.store.Incr(ctx, "auth:change:"+acct.ID.String(), s.cfg.LoginWindow)
		if err != nil {
			return StatusResponse{}, err
		}
		if n > int64(s.cfg.LoginMaxAttempts) {
			return StatusResponse{}, errLocked.WithRetryAfter(ttl)
		}
		if ok, _ := password.Verify(req.CurrentPassword, *acct.PasswordHash); !ok {
			s.audit(ctx, AuditEvent{Action: "auth.password_change_failed", UserID: acct.ID, IP: client.IP})
			return StatusResponse{}, errWrongPassword
		}
		_ = s.store.Delete(ctx, "auth:change:"+acct.ID.String())
	}
	if err := s.setPassword(ctx, acct, req.NewPassword); err != nil {
		return StatusResponse{}, err
	}
	if err := s.repo.RevokeOtherSessions(ctx, acct.ID, p.SessionID); err != nil {
		return StatusResponse{}, err
	}
	if acct.Email != nil {
		_ = s.email.Send(ctx, *acct.Email, "Your EQUIPRENT password was changed", "Your password was changed and your other devices were signed out.")
	}
	s.audit(ctx, AuditEvent{Action: "auth.password_changed", UserID: acct.ID, IP: client.IP})
	return StatusResponse{Status: "password_changed"}, nil
}

func (s *Service) setPassword(ctx context.Context, acct Account, plain string) error {
	personal := []string{acct.FullName, acct.Phone[min(3, len(acct.Phone)):]}
	if acct.Email != nil {
		personal = append(personal, strings.SplitN(*acct.Email, "@", 2)[0])
	}
	if err := password.Check(plain, personal...); err != nil {
		return apperror.ErrValidation.WithFields(apperror.FieldError{Field: "new_password", Code: "weak", Message: err.Error()})
	}
	hash, err := password.Hash(plain, s.hash)
	if err != nil {
		return err
	}
	return s.repo.SetPassword(ctx, acct.ID, hash)
}

// ---- email verification -----------------------------------------------------------------

// sendEmailToken issues a verification token for the address and sends it.
func (s *Service) sendEmailToken(ctx context.Context, userID uuid.UUID, email string) (string, error) {
	if !s.cfg.EmailEnabled() {
		return "", errEmailUnavailable
	}
	if err := s.repo.InvalidateTokens(ctx, userID, tokenEmailVerify); err != nil {
		return "", err
	}
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", err
	}
	token := fmt.Sprintf("%06d", n.Int64())
	hash := HashRefreshToken(token)
	if err := s.repo.CreateToken(ctx, userID, tokenEmailVerify, hash, email, s.now().Add(s.cfg.EmailTokenTTL)); err != nil {
		return "", err
	}
	if err := s.email.Send(ctx, email, "Verify your email for EQUIPRENT", "Your EQUIPRENT email verification code is "+token+". It expires soon. Never share this code."); err != nil {
		return "", errEmailUnavailable.Wrap(err)
	}
	return token, nil
}

// SendEmailVerification (re)sends the verification link to the caller.
func (s *Service) SendEmailVerification(ctx context.Context, p *httpx.Principal, client ClientInfo) (EmailVerificationResponse, error) {
	acct, err := s.repo.AccountByID(ctx, p.UserID)
	if err != nil {
		return EmailVerificationResponse{}, err
	}
	if acct.Email == nil {
		return EmailVerificationResponse{}, apperror.ErrBusinessRule.WithDetail("add an email address to your profile first")
	}
	if acct.EmailVerifiedAt != nil {
		return EmailVerificationResponse{}, apperror.ErrConflict.WithDetail("this email address is already verified")
	}
	ok, err := s.store.SetNX(ctx, "auth:emailverify:"+acct.ID.String(), []byte{1}, emailResendCooldown)
	if err != nil {
		return EmailVerificationResponse{}, err
	}
	if !ok {
		return EmailVerificationResponse{}, apperror.ErrRateLimited.WithDetail("wait a minute before requesting another email").WithRetryAfter(emailResendCooldown)
	}
	token, err := s.sendEmailToken(ctx, acct.ID, *acct.Email)
	if err != nil {
		return EmailVerificationResponse{}, err
	}
	s.audit(ctx, AuditEvent{Action: "auth.email_verification_sent", UserID: acct.ID, IP: client.IP})
	out := EmailVerificationResponse{Sent: true}
	if s.cfg.DevSimulation {
		out.DevToken = token
	}
	return out, nil
}

// VerifyEmail confirms an address with its token (no sign-in required, so
// the link works on any device).
func (s *Service) VerifyEmail(ctx context.Context, req VerifyEmailRequest, client ClientInfo) (StatusResponse, error) {
	userID, email, err := s.repo.ConsumeToken(ctx, HashRefreshToken(req.Token), tokenEmailVerify)
	if errors.Is(err, apperror.ErrNotFound) {
		return StatusResponse{}, errTokenInvalid
	}
	if err != nil {
		return StatusResponse{}, err
	}
	ok, err := s.repo.MarkEmailVerified(ctx, userID, email)
	if err != nil {
		return StatusResponse{}, err
	}
	if !ok { // the address changed since the token was sent
		return StatusResponse{}, errTokenInvalid
	}
	s.audit(ctx, AuditEvent{Action: "auth.email_verified", UserID: userID, IP: client.IP})
	return StatusResponse{Status: "email_verified"}, nil
}

// newURLToken returns a random token and its SHA-256 (stored).
func newURLToken() (string, []byte, error) {
	return NewRefreshToken()
}
