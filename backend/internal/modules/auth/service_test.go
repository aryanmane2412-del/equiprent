package auth

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/config"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/logger"
	"github.com/equiprent/backend/internal/password"
)

// fakeRepo is an in-memory Repository.
type fakeRepo struct {
	mu         sync.Mutex
	challenges map[string][]*Challenge // key: phone|purpose
	consumed   map[uuid.UUID]bool
	accounts   map[uuid.UUID]*Account
	roles      map[uuid.UUID][]httpx.Role
	tokens     map[string]*fakeToken
	sessions   map[uuid.UUID]*fakeSession
	audits     []AuditEvent
}

type fakeToken struct {
	user    uuid.UUID
	purpose string
	email   string
	used    bool
	expires time.Time
}

type fakeSession struct {
	Session
	hash []byte
}

func newFakeRepo() *fakeRepo {
	return &fakeRepo{
		challenges: map[string][]*Challenge{}, consumed: map[uuid.UUID]bool{}, accounts: map[uuid.UUID]*Account{},
		roles: map[uuid.UUID][]httpx.Role{}, tokens: map[string]*fakeToken{}, sessions: map[uuid.UUID]*fakeSession{},
	}
}

func (f *fakeRepo) CreateChallenge(_ context.Context, phone, purpose string, hash []byte, _ string, exp time.Time) (Challenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c := &Challenge{ID: uuid.New(), CodeHash: hash, ExpiresAt: exp}
	f.challenges[phone+"|"+purpose] = append(f.challenges[phone+"|"+purpose], c)
	return *c, nil
}

func (f *fakeRepo) LatestChallenge(_ context.Context, phone, purpose string) (Challenge, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.challenges[phone+"|"+purpose]
	for i := len(list) - 1; i >= 0; i-- {
		if !f.consumed[list[i].ID] {
			return *list[i], nil
		}
	}
	return Challenge{}, apperror.ErrNotFound
}

func (f *fakeRepo) IncrementAttempts(_ context.Context, id uuid.UUID) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, list := range f.challenges {
		for _, c := range list {
			if c.ID == id {
				c.Attempts++
				return c.Attempts, nil
			}
		}
	}
	return 0, apperror.ErrNotFound
}

func (f *fakeRepo) ConsumeChallenge(_ context.Context, id uuid.UUID) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.consumed[id] {
		return false, nil
	}
	f.consumed[id] = true
	return true, nil
}

func (f *fakeRepo) find(match func(*Account) bool) (Account, error) {
	for _, a := range f.accounts {
		if match(a) {
			return *a, nil
		}
	}
	return Account{}, apperror.ErrNotFound
}

func (f *fakeRepo) AccountByPhone(_ context.Context, phone string) (Account, error) {
	return f.find(func(a *Account) bool { return a.Phone == phone })
}
func (f *fakeRepo) AccountByEmail(_ context.Context, email string) (Account, error) {
	return f.find(func(a *Account) bool { return a.Email != nil && strings.EqualFold(*a.Email, email) })
}
func (f *fakeRepo) AccountByID(_ context.Context, id uuid.UUID) (Account, error) {
	return f.find(func(a *Account) bool { return a.ID == id })
}

func (f *fakeRepo) CreateUserWithPersonalOrg(_ context.Context, phone string) (Account, error) {
	a := &Account{ID: uuid.New(), Phone: phone, Status: "active", Language: "en"}
	f.accounts[a.ID] = a
	f.roles[a.ID] = []httpx.Role{{Code: "customer", OrgID: uuid.New()}}
	return *a, nil
}

func (f *fakeRepo) Register(_ context.Context, n NewAccount) (Account, error) {
	email, hash := n.Email, n.PasswordHash
	a := &Account{ID: uuid.New(), Phone: n.Phone, Email: &email, FullName: n.FullName, Status: "active", PasswordHash: &hash}
	f.accounts[a.ID] = a
	org := uuid.New()
	if n.AccountType == AccountTypeEquipmentOwner {
		f.roles[a.ID] = []httpx.Role{{Code: "owner", OrgID: org}, {Code: "org_admin", OrgID: org}}
	} else if n.AccountType == AccountTypeDriver {
		f.roles[a.ID] = []httpx.Role{{Code: "operator", OrgID: org}, {Code: "org_admin", OrgID: org}}
	} else {
		f.roles[a.ID] = []httpx.Role{{Code: "customer", OrgID: org}, {Code: "org_admin", OrgID: org}}
	}
	return *a, nil
}

func (f *fakeRepo) MarkLoggedIn(context.Context, uuid.UUID) error { return nil }
func (f *fakeRepo) MarkPhoneVerified(_ context.Context, id uuid.UUID) error {
	now := time.Now()
	f.accounts[id].PhoneVerifiedAt = &now
	return nil
}
func (f *fakeRepo) MarkEmailVerified(_ context.Context, id uuid.UUID, email string) (bool, error) {
	a := f.accounts[id]
	if a.Email == nil || *a.Email != email {
		return false, nil
	}
	now := time.Now()
	a.EmailVerifiedAt = &now
	return true, nil
}
func (f *fakeRepo) SetPassword(_ context.Context, id uuid.UUID, hash string) error {
	f.accounts[id].PasswordHash = &hash
	return nil
}
func (f *fakeRepo) Roles(_ context.Context, id uuid.UUID) ([]httpx.Role, error) {
	return f.roles[id], nil
}

func (f *fakeRepo) CreateToken(_ context.Context, user uuid.UUID, purpose string, hash []byte, email string, exp time.Time) error {
	f.tokens[string(hash)] = &fakeToken{user: user, purpose: purpose, email: email, expires: exp}
	return nil
}
func (f *fakeRepo) ConsumeToken(_ context.Context, hash []byte, purpose string) (uuid.UUID, string, error) {
	t, ok := f.tokens[string(hash)]
	if !ok || t.used || t.purpose != purpose || time.Now().After(t.expires) {
		return uuid.Nil, "", apperror.ErrNotFound
	}
	t.used = true
	return t.user, t.email, nil
}
func (f *fakeRepo) InvalidateTokens(_ context.Context, user uuid.UUID, purpose string) error {
	for _, t := range f.tokens {
		if t.user == user && t.purpose == purpose {
			t.used = true
		}
	}
	return nil
}

func (f *fakeRepo) CreateSession(_ context.Context, s NewSession) (Session, error) {
	sess := Session{ID: uuid.New(), UserID: s.UserID, FamilyID: s.FamilyID, ExpiresAt: s.ExpiresAt}
	f.sessions[sess.ID] = &fakeSession{Session: sess, hash: s.RefreshHash}
	return sess, nil
}
func (f *fakeRepo) SessionByRefreshHash(_ context.Context, hash []byte) (Session, error) {
	for _, s := range f.sessions {
		if string(s.hash) == string(hash) {
			return s.Session, nil
		}
	}
	return Session{}, apperror.ErrNotFound
}
func (f *fakeRepo) revoke(match func(*fakeSession) bool) int {
	now, n := time.Now(), 0
	for _, s := range f.sessions {
		if s.RevokedAt == nil && match(s) {
			s.RevokedAt = &now
			n++
		}
	}
	return n
}
func (f *fakeRepo) RevokeSession(_ context.Context, id uuid.UUID) (bool, error) {
	return f.revoke(func(s *fakeSession) bool { return s.ID == id }) == 1, nil
}
func (f *fakeRepo) RevokeUserSession(_ context.Context, id, user uuid.UUID) (bool, error) {
	return f.revoke(func(s *fakeSession) bool { return s.ID == id && s.UserID == user }) == 1, nil
}
func (f *fakeRepo) RevokeFamily(_ context.Context, fam uuid.UUID) error {
	f.revoke(func(s *fakeSession) bool { return s.FamilyID == fam })
	return nil
}
func (f *fakeRepo) RevokeAllSessions(_ context.Context, user uuid.UUID) error {
	f.revoke(func(s *fakeSession) bool { return s.UserID == user })
	return nil
}
func (f *fakeRepo) RevokeOtherSessions(_ context.Context, user, keep uuid.UUID) error {
	f.revoke(func(s *fakeSession) bool { return s.UserID == user && s.ID != keep })
	return nil
}
func (f *fakeRepo) ListSessions(context.Context, uuid.UUID) ([]SessionInfo, error) { return nil, nil }
func (f *fakeRepo) SessionState(_ context.Context, id uuid.UUID) (SessionState, error) {
	s, ok := f.sessions[id]
	if !ok {
		return SessionState{}, apperror.ErrNotFound
	}
	return SessionState{UserID: s.UserID, UserActive: f.accounts[s.UserID].Status == "active", Revoked: s.RevokedAt != nil, ExpiresAt: s.ExpiresAt}, nil
}
func (f *fakeRepo) Audit(_ context.Context, e AuditEvent) error {
	f.audits = append(f.audits, e)
	return nil
}

func (f *fakeRepo) audited(action string) int {
	n := 0
	for _, a := range f.audits {
		if a.Action == action {
			n++
		}
	}
	return n
}

// ---- harness -----------------------------------------------------------------------

var fastHash = password.Params{MemoryKiB: 1024, Time: 1, Threads: 1, SaltLen: 16, KeyLen: 32}

func testConfig(devSim bool) config.AuthConfig {
	return config.AuthConfig{
		JWTSecret: "test-secret-test-secret-test-secret", Issuer: "equiprent",
		AccessTokenTTL: 15 * time.Minute, RefreshTokenTTL: 24 * time.Hour,
		OTPSecret: "otp-secret", OTPTTL: 5 * time.Minute, DevSimulation: devSim,
		LoginMaxAttempts: 5, LoginWindow: 15 * time.Minute, EmailTokenTTL: time.Hour, ResetTokenTTL: 30 * time.Minute,
	}
}

func newTestService(t *testing.T) (*Service, *fakeRepo) {
	t.Helper()
	return newServiceWith(t, true)
}

func newServiceWith(t *testing.T, devSim bool) (*Service, *fakeRepo) {
	t.Helper()
	repo := newFakeRepo()
	cfg := testConfig(devSim)
	var sms SMSSender = DisabledSMS{}
	var email EmailSender = DisabledEmail{}
	if devSim {
		sim := DevSimulator{Log: logger.Discard()}
		sms, email = sim, sim
	}
	svc := NewService(repo, NewTokens(cfg.JWTSecret, cfg.Issuer, cfg.AccessTokenTTL), cache.NewMemoryStore(), sms, email, cfg, logger.Discard())
	svc.hash = fastHash
	return svc, repo
}

const (
	phone = "+919876543210"
	pw    = "Tractor-Rental-2026"
)

var ctx = context.Background()

func login(t *testing.T, svc *Service) TokenResponse {
	t.Helper()
	otp, err := svc.RequestOTP(ctx, RequestOTPRequest{Phone: phone})
	if err != nil || len(otp.DevCode) != 6 {
		t.Fatalf("request otp: %+v %v", otp, err)
	}
	tok, err := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: otp.DevCode}, ClientInfo{Platform: "android"})
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	return tok
}

func register(t *testing.T, svc *Service, kind string) RegisterResponse {
	t.Helper()
	req := RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone, Password: pw, AccountType: kind, AcceptTerms: true, AcceptInspectionPolicy: true, InspectionPolicyVersion: EquipmentInspectionPolicyVersion}
	if kind == AccountTypeEquipmentOwner {
		req.OrganizationName = "Patil Earthmovers"
	}
	resp, err := svc.Register(ctx, req, ClientInfo{IP: "10.0.0.1"})
	if err != nil {
		t.Fatalf("register: %v", err)
	}
	return resp
}

func acceptedInspectionPolicy(req RegisterRequest) RegisterRequest {
	req.AcceptInspectionPolicy = true
	req.InspectionPolicyVersion = EquipmentInspectionPolicyVersion
	return req
}

func code(err error) string { return apperror.From(err).Code }

// ---- OTP -----------------------------------------------------------------------------

func TestOTPLoginCreatesCustomerAndVerifiesPhone(t *testing.T) {
	svc, repo := newTestService(t)
	tok := login(t, svc)
	if !tok.IsNew || !tok.User.PhoneVerified || tok.User.HasPassword || !reflect.DeepEqual(tok.AppRoles, []string{"CUSTOMER"}) {
		t.Fatalf("token = %+v", tok)
	}
	p, err := svc.Authenticate(ctx, tok.AccessToken)
	if err != nil || p.UserID != tok.User.ID {
		t.Fatalf("authenticate: %+v %v", p, err)
	}
	if repo.audited("auth.login") != 1 {
		t.Error("login not audited")
	}
}

func TestOTPCooldownAttemptsAndSingleUse(t *testing.T) {
	svc, _ := newTestService(t)
	otp, _ := svc.RequestOTP(ctx, RequestOTPRequest{Phone: phone})
	if _, err := svc.RequestOTP(ctx, RequestOTPRequest{Phone: phone}); code(err) != "OTP_RESEND_TOO_SOON" {
		t.Fatalf("cooldown: %v", err)
	}
	wrong := "000000"
	if otp.DevCode == wrong {
		wrong = "111111"
	}
	for i := 1; i <= 5; i++ {
		_, err := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: wrong}, ClientInfo{})
		want := map[bool]string{true: "OTP_ATTEMPTS_EXCEEDED", false: "OTP_INVALID"}[i == 5]
		if code(err) != want {
			t.Fatalf("attempt %d: %s", i, code(err))
		}
	}
	if _, err := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: otp.DevCode}, ClientInfo{}); code(err) != "OTP_ATTEMPTS_EXCEEDED" {
		t.Fatal("locked challenge accepted the right code")
	}
}

func TestProductionOTPStaysOffWithoutProvider(t *testing.T) {
	svc, _ := newServiceWith(t, false)
	if _, err := svc.RequestOTP(ctx, RequestOTPRequest{Phone: phone}); code(err) != "OTP_UNAVAILABLE" {
		t.Fatalf("request: %v", err)
	}
	if _, err := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: "123456"}, ClientInfo{}); code(err) != "OTP_UNAVAILABLE" {
		t.Fatalf("verify: %v", err)
	}
	if c := svc.Capabilities(); c.MobileOTP || c.EmailVerification || c.DevSimulation || !c.PasswordLogin {
		t.Fatalf("capabilities = %+v", c)
	}
}

// ---- registration ----------------------------------------------------------------------

func registrationField(err error) string {
	var appErr *apperror.Error
	if errors.As(err, &appErr) && len(appErr.Fields) > 0 {
		return appErr.Fields[0].Field
	}
	return ""
}

func TestRegisterPublicAccountTypesGetServerAssignedRoles(t *testing.T) {
	for kind, want := range map[string]string{AccountTypeCustomer: "CUSTOMER", AccountTypeEquipmentOwner: "EQUIPMENT_OWNER", AccountTypeDriver: "OPERATOR"} {
		svc, repo := newTestService(t)
		resp := register(t, svc, kind)
		if resp.DevOTP == "" || resp.DevEmailToken == "" || resp.Status != "verification_required" {
			t.Fatalf("%s: %+v", kind, resp)
		}
		tok, err := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})
		if err != nil || !reflect.DeepEqual(tok.AppRoles, []string{want}) || !tok.User.HasPassword || tok.IsNew {
			t.Fatalf("%s: %+v %v", kind, tok, err)
		}
		if repo.audited("auth.register") != 1 {
			t.Errorf("%s: registration not audited", kind)
		}
	}
}

func TestRegisterRequiresCurrentInspectionPolicyAcceptance(t *testing.T) {
	svc, repo := newTestService(t)
	base := RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone, Password: pw, AccountType: AccountTypeCustomer, AcceptTerms: true}
	for _, req := range []RegisterRequest{base, acceptedInspectionPolicy(base)} {
		if req.AcceptInspectionPolicy {
			req.InspectionPolicyVersion = "obsolete"
		}
		if _, err := svc.Register(ctx, req, ClientInfo{}); registrationField(err) != "accept_inspection_policy" {
			t.Fatalf("expected policy acceptance error, got %v", err)
		}
	}
	if len(repo.accounts) != 0 {
		t.Fatalf("created %d accounts without current consent", len(repo.accounts))
	}
}

func TestRegisterRejectsPrivilegedOrUnknownAccountTypes(t *testing.T) {
	svc, _ := newTestService(t)
	for _, kind := range []string{"admin", "fleet_manager", "operator", "unexpected"} {
		_, err := svc.Register(ctx, acceptedInspectionPolicy(RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone,
			Password: pw, AccountType: kind, AcceptTerms: true}), ClientInfo{})
		if registrationField(err) != "account_type" {
			t.Errorf("%q: expected account_type validation error, got %v", kind, err)
		}
	}
}

func TestRegisterDoesNotRevealExistingAccounts(t *testing.T) {
	svc, repo := newServiceWith(t, false) // no dev fields: what production clients see
	first, err := svc.Register(ctx, acceptedInspectionPolicy(RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone, Password: pw, AccountType: "customer", AcceptTerms: true}), ClientInfo{})
	if err != nil {
		t.Fatal(err)
	}
	for _, dup := range []RegisterRequest{
		acceptedInspectionPolicy(RegisterRequest{FullName: "Someone Else", Email: "other@example.com", Phone: phone, Password: "Crane-Hire-4417", AccountType: "customer", AcceptTerms: true}),
		acceptedInspectionPolicy(RegisterRequest{FullName: "Someone Else", Email: "RAVI@example.com", Phone: "+919800000000", Password: "Crane-Hire-4417", AccountType: "customer", AcceptTerms: true}),
	} {
		got, err := svc.Register(ctx, dup, ClientInfo{})
		if err != nil || got != first {
			t.Fatalf("duplicate answered differently: %+v vs %+v (%v)", got, first, err)
		}
	}
	if len(repo.accounts) != 1 || repo.audited("auth.register_duplicate") != 2 {
		t.Fatalf("accounts=%d", len(repo.accounts))
	}
}

func TestRegisterDuplicateExplainedOnlyInDevSimulation(t *testing.T) {
	svc, _ := newServiceWith(t, true)
	if _, err := svc.Register(ctx, acceptedInspectionPolicy(RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone, Password: pw, AccountType: "customer", AcceptTerms: true}), ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	cases := map[string]RegisterRequest{
		"This mobile number is":            acceptedInspectionPolicy(RegisterRequest{FullName: "Someone Else", Email: "other@example.com", Phone: phone, Password: "Crane-Hire-4417", AccountType: "customer", AcceptTerms: true}),
		"This email address is":            acceptedInspectionPolicy(RegisterRequest{FullName: "Someone Else", Email: "RAVI@example.com", Phone: "+919800000000", Password: "Crane-Hire-4417", AccountType: "customer", AcceptTerms: true}),
		"This mobile number and email are": acceptedInspectionPolicy(RegisterRequest{FullName: "Someone Else", Email: "ravi@example.com", Phone: phone, Password: "Crane-Hire-4417", AccountType: "customer", AcceptTerms: true}),
	}
	for want, req := range cases {
		got, err := svc.Register(ctx, req, ClientInfo{})
		if err != nil || got.DevOTP != "" || !strings.HasPrefix(got.DevNotice, want+" already registered") {
			t.Fatalf("%s: %+v (%v)", want, got, err)
		}
	}
}

func TestRegisterRejectsWeakPasswords(t *testing.T) {
	svc, repo := newTestService(t)
	_, err := svc.Register(ctx, RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone, Password: "ravipatil1234", AccountType: "customer", AcceptTerms: true}, ClientInfo{})
	if !errors.Is(err, apperror.ErrValidation) || len(repo.accounts) != 0 {
		t.Fatalf("weak password: %v", err)
	}
}

// ---- password login --------------------------------------------------------------------

func TestPasswordLoginRequiresVerifiedIdentifier(t *testing.T) {
	svc, _ := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	for _, id := range []string{"ravi@example.com", phone, "9876543210"} {
		if _, err := svc.Login(ctx, LoginRequest{Identifier: id, Password: pw}, ClientInfo{}); code(err) != "VERIFICATION_REQUIRED" {
			t.Fatalf("%s before verification: %v", id, err)
		}
	}
	if _, err := svc.VerifyEmail(ctx, VerifyEmailRequest{Token: resp.DevEmailToken}, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: "Ravi@Example.com", Password: pw}, ClientInfo{}); err != nil {
		t.Fatalf("email login after verification: %v", err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: "9876543210", Password: pw}, ClientInfo{}); code(err) != "VERIFICATION_REQUIRED" {
		t.Fatal("the phone is still unverified")
	}
	if _, err := svc.VerifyEmail(ctx, VerifyEmailRequest{Token: resp.DevEmailToken}, ClientInfo{}); code(err) != "TOKEN_INVALID" {
		t.Fatal("email tokens are single use")
	}
}

func TestLoginFailuresAreIndistinguishable(t *testing.T) {
	svc, _ := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	_, _ = svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})

	_, wrongPw := svc.Login(ctx, LoginRequest{Identifier: phone, Password: "Wrong-Password-1"}, ClientInfo{})
	_, unknown := svc.Login(ctx, LoginRequest{Identifier: "+919811111111", Password: "Wrong-Password-1"}, ClientInfo{})
	_, unknownEmail := svc.Login(ctx, LoginRequest{Identifier: "nobody@example.com", Password: "Wrong-Password-1"}, ClientInfo{})
	a, b, c := apperror.From(wrongPw), apperror.From(unknown), apperror.From(unknownEmail)
	if a.Code != "INVALID_CREDENTIALS" || a.Code != b.Code || b.Code != c.Code || a.Detail != b.Detail || a.Status != c.Status {
		t.Fatalf("errors differ: %+v / %+v / %+v", a, b, c)
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: phone, Password: pw}, ClientInfo{}); err != nil {
		t.Fatalf("right password: %v", err)
	}
}

func TestLoginLockoutAppliesEvenToTheRightPassword(t *testing.T) {
	svc, repo := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	_, _ = svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})
	for range 5 {
		_, _ = svc.Login(ctx, LoginRequest{Identifier: phone, Password: "Wrong-Password-1"}, ClientInfo{})
	}
	_, err := svc.Login(ctx, LoginRequest{Identifier: phone, Password: pw}, ClientInfo{})
	if code(err) != "ACCOUNT_TEMPORARILY_LOCKED" || apperror.From(err).RetryAfter == 0 {
		t.Fatalf("locked: %v", err)
	}
	// Unknown identifiers lock the same way (no enumeration by lockout).
	for range 6 {
		_, err = svc.Login(ctx, LoginRequest{Identifier: "ghost@example.com", Password: "Wrong-Password-1"}, ClientInfo{})
	}
	if code(err) != "ACCOUNT_TEMPORARILY_LOCKED" || repo.audited("auth.lockout") != 2 {
		t.Fatalf("unknown identifier lockout: %v", err)
	}
}

func TestSuspendedAccountsCannotSignInOrUseTokens(t *testing.T) {
	svc, repo := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	tok, _ := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})
	repo.accounts[tok.User.ID].Status = "suspended"
	if _, err := svc.Authenticate(ctx, tok.AccessToken); code(err) != "ACCOUNT_SUSPENDED" {
		t.Fatalf("token of suspended user: %v", err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: phone, Password: pw}, ClientInfo{}); code(err) != "ACCOUNT_SUSPENDED" {
		t.Fatalf("login: %v", err)
	}
	if _, err := svc.Refresh(ctx, tok.RefreshToken, ClientInfo{}); code(err) != "ACCOUNT_SUSPENDED" {
		t.Fatalf("refresh: %v", err)
	}
}

// ---- sessions -----------------------------------------------------------------------------

func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	svc, repo := newTestService(t)
	first := login(t, svc)
	second, err := svc.Refresh(ctx, first.RefreshToken, ClientInfo{})
	if err != nil || second.RefreshToken == first.RefreshToken {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := svc.Authenticate(ctx, first.AccessToken); err == nil {
		t.Fatal("rotated session's access token must stop working")
	}
	if _, err := svc.Refresh(ctx, first.RefreshToken, ClientInfo{}); !errors.Is(err, apperror.ErrUnauthenticated) {
		t.Fatalf("reuse: %v", err)
	}
	if _, err := svc.Authenticate(ctx, second.AccessToken); err == nil || repo.audited("auth.refresh_reuse") != 1 {
		t.Fatal("family must be revoked (and audited) after reuse")
	}
}

func TestLogoutAndRevokeOthers(t *testing.T) {
	svc, _ := newTestService(t)
	a := login(t, svc)
	svc.store = cache.NewMemoryStore()
	b := login(t, svc)
	pb, _ := svc.Authenticate(ctx, b.AccessToken)
	if err := svc.RevokeOtherSessions(ctx, pb, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, a.AccessToken); err == nil {
		t.Fatal("other device still signed in")
	}
	if err := svc.Logout(ctx, pb, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, b.AccessToken); err == nil {
		t.Fatal("token must be rejected after logout")
	}
}

// ---- reset / change -------------------------------------------------------------------------

func TestPasswordResetByEmailSignsOutEverywhere(t *testing.T) {
	svc, repo := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	_, _ = svc.VerifyEmail(ctx, VerifyEmailRequest{Token: resp.DevEmailToken}, ClientInfo{})
	tok, _ := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})

	forgot, err := svc.ForgotPassword(ctx, ForgotPasswordRequest{Identifier: "ravi@example.com"}, ClientInfo{})
	if err != nil || forgot.DevToken == "" {
		t.Fatalf("forgot: %+v %v", forgot, err)
	}
	if _, err := svc.ResetPassword(ctx, ResetPasswordRequest{Token: forgot.DevToken, NewPassword: "short"}, ClientInfo{}); !errors.Is(err, apperror.ErrValidation) {
		t.Fatalf("weak new password: %v", err)
	}
	// The failed attempt consumed the token: request a fresh one.
	forgot, _ = svc.ForgotPassword(ctx, ForgotPasswordRequest{Identifier: "ravi@example.com"}, ClientInfo{})
	if _, err := svc.ResetPassword(ctx, ResetPasswordRequest{Token: forgot.DevToken, NewPassword: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatalf("reset: %v", err)
	}
	if _, err := svc.Authenticate(ctx, tok.AccessToken); err == nil {
		t.Fatal("reset must revoke existing sessions")
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: "ravi@example.com", Password: pw}, ClientInfo{}); code(err) != "INVALID_CREDENTIALS" {
		t.Fatal("old password still works")
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: "ravi@example.com", Password: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatalf("new password: %v", err)
	}
	if repo.audited("auth.password_reset") != 1 {
		t.Error("reset not audited")
	}
}

func TestPasswordResetBySMSCode(t *testing.T) {
	svc, _ := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	_, _ = svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})
	forgot, _ := svc.ForgotPassword(ctx, ForgotPasswordRequest{Identifier: phone}, ClientInfo{})
	if forgot.DevCode == "" {
		t.Fatalf("forgot = %+v", forgot)
	}
	if _, err := svc.ResetPassword(ctx, ResetPasswordRequest{Phone: phone, Code: forgot.DevCode, NewPassword: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: phone, Password: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
}

func TestForgotPasswordDoesNotRevealAccounts(t *testing.T) {
	svc, _ := newServiceWith(t, false)
	_, _ = svc.Register(ctx, RegisterRequest{FullName: "Ravi Patil", Email: "ravi@example.com", Phone: phone, Password: pw, AccountType: "customer", AcceptTerms: true}, ClientInfo{})
	known, err1 := svc.ForgotPassword(ctx, ForgotPasswordRequest{Identifier: "ravi@example.com"}, ClientInfo{})
	unknown, err2 := svc.ForgotPassword(ctx, ForgotPasswordRequest{Identifier: "nobody@example.com"}, ClientInfo{})
	if err1 != nil || err2 != nil || known != unknown {
		t.Fatalf("known=%+v unknown=%+v", known, unknown)
	}
	if _, err := svc.ResetPassword(ctx, ResetPasswordRequest{Token: strings.Repeat("x", 43), NewPassword: "Brand-New-Secret-7"}, ClientInfo{}); code(err) != "TOKEN_INVALID" {
		t.Fatalf("forged token: %v", err)
	}
}

func TestChangePasswordNeedsCurrentAndKeepsThisDevice(t *testing.T) {
	svc, _ := newTestService(t)
	resp := register(t, svc, AccountTypeCustomer)
	a, _ := svc.VerifyOTP(ctx, VerifyOTPRequest{Phone: phone, Code: resp.DevOTP}, ClientInfo{})
	b, _ := svc.Login(ctx, LoginRequest{Identifier: phone, Password: pw}, ClientInfo{})
	pb, _ := svc.Authenticate(ctx, b.AccessToken)

	if _, err := svc.ChangePassword(ctx, pb, ChangePasswordRequest{CurrentPassword: "nope-nope-1", NewPassword: "Brand-New-Secret-7"}, ClientInfo{}); code(err) != "CURRENT_PASSWORD_INCORRECT" {
		t.Fatalf("wrong current: %v", err)
	}
	if _, err := svc.ChangePassword(ctx, pb, ChangePasswordRequest{CurrentPassword: pw, NewPassword: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Authenticate(ctx, b.AccessToken); err != nil {
		t.Fatal("the current device must stay signed in")
	}
	if _, err := svc.Authenticate(ctx, a.AccessToken); err == nil {
		t.Fatal("other devices must be signed out")
	}
}

func TestOTPAccountCanSetFirstPassword(t *testing.T) {
	svc, _ := newTestService(t)
	tok := login(t, svc)
	p, _ := svc.Authenticate(ctx, tok.AccessToken)
	if _, err := svc.ChangePassword(ctx, p, ChangePasswordRequest{NewPassword: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Login(ctx, LoginRequest{Identifier: phone, Password: "Brand-New-Secret-7"}, ClientInfo{}); err != nil {
		t.Fatalf("password login after setting one: %v", err)
	}
}

// ---- tokens ----------------------------------------------------------------------------------

func TestTokensRejectTamperingAndExpiry(t *testing.T) {
	tk := NewTokens("secret-one-secret-one-secret-one!", "equiprent", time.Minute)
	raw, _, err := tk.Issue(uuid.New(), uuid.New())
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := tk.Parse(raw + "x"); err == nil {
		t.Error("tampered token accepted")
	}
	if _, _, err := NewTokens("another-secret-another-secret-xx", "equiprent", time.Minute).Parse(raw); err == nil {
		t.Error("token signed with another key accepted")
	}
	if _, _, err := NewTokens("secret-one-secret-one-secret-one!", "someone-else", time.Minute).Parse(raw); err == nil {
		t.Error("token from another issuer accepted")
	}
	tk.now = func() time.Time { return time.Now().Add(2 * time.Minute) }
	if _, _, err := tk.Parse(raw); err == nil {
		t.Error("expired token accepted")
	}
	// alg=none must never be accepted.
	parts := strings.Split(raw, ".")
	none := "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0." + parts[1] + "."
	if _, _, err := NewTokens("secret-one-secret-one-secret-one!", "equiprent", time.Minute).Parse(none); err == nil {
		t.Error("alg=none token accepted")
	}
}

func TestMasking(t *testing.T) {
	if MaskPhone("+919876543210") != "+91********10" || MaskEmail("ravi@example.com") != "r***@example.com" {
		t.Fatalf("%s %s", MaskPhone("+919876543210"), MaskEmail("ravi@example.com"))
	}
}

func TestAppRoles(t *testing.T) {
	org := uuid.New()
	got := httpx.AppRoles([]httpx.Role{{Code: "org_admin", OrgID: org}, {Code: "fleet_manager", OrgID: org}, {Code: "admin_kyc"}, {Code: "customer", OrgID: org}})
	if !reflect.DeepEqual(got, []string{"CUSTOMER", "FLEET_MANAGER", "ADMIN"}) {
		t.Fatalf("app roles = %v", got)
	}
}
