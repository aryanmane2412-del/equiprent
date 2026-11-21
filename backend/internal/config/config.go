// Package config loads and validates runtime configuration from environment
// variables (twelve-factor). A .env file is honoured in non-production
// environments for local development only.
package config

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Environment is the deployment environment the process runs in.
type Environment string

const (
	EnvDevelopment Environment = "development"
	EnvTest        Environment = "test"
	EnvStaging     Environment = "staging"
	EnvProduction  Environment = "production"
)

// IsProduction reports whether this is the production environment.
func (e Environment) IsProduction() bool { return e == EnvProduction }

// IsDevelopment reports whether this is a local development environment.
func (e Environment) IsDevelopment() bool { return e == EnvDevelopment }

// Config is the complete, validated application configuration.
type Config struct {
	Env             Environment
	ServiceName     string
	ShutdownTimeout time.Duration
	HTTP            HTTPConfig
	Log             LogConfig
	Database        DatabaseConfig
	Redis           RedisConfig
	CORS            CORSConfig
	Worker          WorkerConfig
	Auth            AuthConfig
	RateLimit       RateLimitConfig
	Payments        PaymentsConfig
	Maps            MapsConfig
	Push            PushConfig
	ESign           ESignConfig
	Legal           LegalConfig
	Tracking        TrackingConfig
	Booking         BookingConfig
	Docs            DocsConfig
	Storage         StorageConfig
	Security        SecurityConfig
}

// TrackingConfig configures location sharing.
type TrackingConfig struct {
	// AllowSimulated accepts positions marked simulated (the app's GPS
	// simulator, the simulated tracker). Development only.
	AllowSimulated bool
	// IdleTimeout closes a tracking session with no position for this long.
	IdleTimeout time.Duration
	// TicketTTL is how long a WebSocket ticket is valid.
	TicketTTL time.Duration
	// PolicyVersion is the tracking notice users consent to; change it when
	// the notice changes so new consent is collected.
	PolicyVersion string
}

// LegalConfig controls compliance gates on documents.
type LegalConfig struct {
	// RequireReviewed refuses to issue rental agreements from a template
	// with no recorded legal review, and invoices while an active tax rule
	// has no recorded compliance review. On by default in staging and
	// production; development seeds unreviewed wording.
	RequireReviewed bool
}

// StorageConfig configures the PRIVATE object store for uploaded documents
// and images. Files are never publicly addressable: reads go through
// short-lived HMAC-signed URLs issued after an authorisation check.
type StorageConfig struct {
	Driver         string // local (development, one server) | s3 (any S3-compatible service)
	Dir            string // local driver
	S3             S3Config
	SigningSecret  string
	URLTTL         time.Duration // documents
	AvatarURLTTL   time.Duration // profile images
	MaxUploadBytes int64
}

// S3Config addresses a private S3-compatible bucket (AWS S3, MinIO, R2…).
type S3Config struct {
	Endpoint        string
	Region          string
	Bucket          string
	AccessKeyID     string
	SecretAccessKey string
	PathStyle       bool
}

// SecurityConfig holds keys for application-level field encryption of
// identity / bank numbers (AES-256-GCM) and their lookup hashes (HMAC).
type SecurityConfig struct {
	FieldEncryptionKey string // base64, 32 bytes
	LookupHashKey      string
}

// AuthConfig controls OTP login and tokens.
type AuthConfig struct {
	// JWTSecret signs access tokens (HS256). At least 32 bytes outside development.
	JWTSecret       string
	Issuer          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	// OTPSecret keys the HMAC that stores OTP codes (never the code itself).
	OTPSecret string
	OTPTTL    time.Duration
	// DevSimulation simulates SMS and email delivery: OTP codes and email
	// tokens are returned in API responses (dev_code / dev_token) and only
	// logged. Development/test only; refused in staging and production.
	DevSimulation bool
	// SMSProvider / EmailProvider select the generic HTTPS JSON adapter.
	SMSProvider   string
	SMSAPIURL     string
	SMSToken      string
	EmailProvider string
	EmailAPIURL   string
	EmailToken    string
	EmailFrom     string
	// LoginMaxAttempts password attempts per identifier per LoginWindow
	// before the identifier is temporarily locked.
	LoginMaxAttempts int
	LoginWindow      time.Duration
	EmailTokenTTL    time.Duration
	ResetTokenTTL    time.Duration
}

// OTPEnabled reports whether mobile OTP can be delivered.
func (a AuthConfig) OTPEnabled() bool { return a.DevSimulation || a.SMSProvider != "" }

// EmailEnabled reports whether email links can be delivered.
func (a AuthConfig) EmailEnabled() bool { return a.DevSimulation || a.EmailProvider != "" }

// RateLimitConfig sets the per-client request budgets.
type RateLimitConfig struct {
	Enabled bool
	// PerMinute is the general budget per client IP (or user) per minute.
	PerMinute int
	// AuthPerMinute is the budget for unauthenticated auth endpoints per IP.
	AuthPerMinute int
}

// PaymentsConfig configures the payment gateway.
type PaymentsConfig struct {
	Provider      string // razorpay (only provider wired so far)
	KeyID         string
	KeySecret     string
	WebhookSecret string
}

// Sandbox reports whether no gateway credentials are configured, in which
// case orders are simulated locally (development only).
func (p PaymentsConfig) Sandbox() bool { return p.KeyID == "" || p.KeySecret == "" }

// MapsConfig configures Google Maps Platform. Without a key, search still
// works with the PIN code reference table and the built-in map.
type MapsConfig struct {
	// GoogleAPIKey is a server key (Geocoding API) used to resolve PIN codes
	// missing from the reference table.
	GoogleAPIKey string
}

// Google reports whether Google Maps credentials are configured.
func (m MapsConfig) Google() bool { return m.GoogleAPIKey != "" }

// PushConfig configures push notifications through Firebase Cloud
// Messaging (HTTP v1). Without credentials pushes are only logged; in-app
// notifications always work.
type PushConfig struct {
	// FCMCredentials is a service-account key: a file path or the JSON itself.
	FCMCredentials string
	// FCMProjectID overrides the key's project_id.
	FCMProjectID string
	// AppURL is opened when a web push is clicked.
	AppURL string
}

// FCM reports whether FCM credentials are configured.
func (p PushConfig) FCM() bool { return p.FCMCredentials != "" }

// ESignConfig selects how rental agreements are signed.
//   - builtin: the signer types their full name and consents; the signature
//     is bound to the SHA-256 of the exact text with time, IP and device
//     (an electronic contract under the IT Act 2000, s.10A).
//   - sandbox: simulates an external provider's redirect + webhook flow for
//     development; refused in production.
//
// A provider such as Aadhaar eSign (via an ESP) plugs in behind
// agreements.Provider.
type ESignConfig struct {
	Provider      string
	WebhookSecret string
}

// BookingConfig holds commercial policy defaults.
type BookingConfig struct {
	OwnerResponseWindow time.Duration
	AdvanceBps          int // share of the estimate due on confirmation (5000 = 50%)
	GSTRateBps          int // 1800 = 18%
	DefaultDepositPaise int64
	PaymentWindow       time.Duration // online bookings must pay the advance within this after acceptance
	ProcessingTimeout   time.Duration // an unfinished checkout returns to payment_pending after this
	SettlementHold      time.Duration // completion -> owner settlement eligible (T+3 by default)
	// Policy values the business has not decided yet. Zero means the rule is OFF; nothing is invented.
	NoShowGrace         time.Duration // wait after the start time before a no-show may be declared
	OverdueFeeBpsPerDay int           // late-return fee, basis points of the daily rate per day
	TurnaroundBuffer    time.Duration // gap kept between consecutive bookings of one machine
	CancelPenaltyBps    int           // cancellation penalty, basis points of the booking total
}

// DocsConfig controls the OpenAPI document and Swagger UI.
type DocsConfig struct {
	Enabled   bool
	PublicURL string // base URL shown in the spec's "servers"
}

type HTTPConfig struct {
	Host              string
	Port              int
	ReadTimeout       time.Duration
	ReadHeaderTimeout time.Duration
	WriteTimeout      time.Duration
	IdleTimeout       time.Duration
	MaxBodyBytes      int64
	TrustedProxies    []string
}

// Addr returns the listen address, e.g. "0.0.0.0:8080".
func (h HTTPConfig) Addr() string { return fmt.Sprintf("%s:%d", h.Host, h.Port) }

type LogConfig struct {
	Level  slog.Level
	Format string // "text" or "json"
}

type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
	ConnectTimeout  time.Duration
}

type RedisConfig struct {
	// URL is optional. When empty, Redis is disabled and reported as such
	// by the readiness endpoint.
	URL string
}

// Enabled reports whether Redis has been configured.
func (r RedisConfig) Enabled() bool { return r.URL != "" }

type CORSConfig struct {
	AllowedOrigins []string
	// AllowLocalhost allows any http(s)://localhost:<port> origin. Only
	// enabled by default in development, where Flutter web uses random ports.
	AllowLocalhost bool
}

type WorkerConfig struct {
	HeartbeatInterval time.Duration
}

// LoadDotEnv loads the first .env file found among the candidates. Variables
// that are already set in the process environment are never overridden.
// It returns the path that was loaded, or "" when none exists.
func LoadDotEnv(candidates ...string) (string, error) {
	if explicit := os.Getenv("ENV_FILE"); explicit != "" {
		candidates = append([]string{explicit}, candidates...)
	}
	for _, path := range candidates {
		if _, err := os.Stat(path); err != nil {
			continue
		}
		if err := godotenv.Load(path); err != nil {
			return "", fmt.Errorf("load %s: %w", path, err)
		}
		return path, nil
	}
	return "", nil
}

// FromEnv loads configuration from the process environment.
func FromEnv() (Config, error) { return Load(os.LookupEnv) }

// Load builds configuration from a lookup function (os.LookupEnv in
// production code, a map in tests). All problems are reported together.
func Load(lookup func(string) (string, bool)) (Config, error) {
	l := &loader{lookup: lookup}

	env := Environment(strings.ToLower(l.str("APP_ENV", "")))
	if env == "" {
		// A missing APP_ENV is only treated as development on a machine that clearly is one
		// (no database configured, or one on the loopback interface); anywhere else it is an
		// error, so a forgotten variable can never start production in development mode.
		if looksLocal(lookup) {
			env = EnvDevelopment
		} else {
			l.fail("APP_ENV", errors.New("is required (development, test, staging or production)"))
			env = EnvProduction // validate everything else as the strictest environment
		}
	}
	switch env {
	case EnvDevelopment, EnvTest, EnvStaging, EnvProduction:
	default:
		l.fail("APP_ENV", fmt.Errorf("must be one of development, test, staging, production; got %q", env))
	}

	defaultLogFormat := "text"
	if !env.IsDevelopment() && env != EnvTest {
		defaultLogFormat = "json"
	}

	cfg := Config{
		Env:             env,
		ServiceName:     l.str("SERVICE_NAME", "equiprent-api"),
		ShutdownTimeout: l.duration("SHUTDOWN_TIMEOUT", 15*time.Second),
		HTTP: HTTPConfig{
			Host:              l.str("HTTP_HOST", "0.0.0.0"),
			Port:              l.int("HTTP_PORT", 8080),
			ReadTimeout:       l.duration("HTTP_READ_TIMEOUT", 15*time.Second),
			ReadHeaderTimeout: l.duration("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
			WriteTimeout:      l.duration("HTTP_WRITE_TIMEOUT", 30*time.Second),
			IdleTimeout:       l.duration("HTTP_IDLE_TIMEOUT", 120*time.Second),
			MaxBodyBytes:      int64(l.int("HTTP_MAX_BODY_BYTES", 1<<20)),
			TrustedProxies:    l.list("HTTP_TRUSTED_PROXIES", nil),
		},
		Log: LogConfig{
			Level:  l.logLevel("LOG_LEVEL", slog.LevelInfo),
			Format: strings.ToLower(l.str("LOG_FORMAT", defaultLogFormat)),
		},
		Database: DatabaseConfig{
			URL:             l.required("DATABASE_URL"),
			MaxConns:        int32(l.int("DB_MAX_CONNS", 10)),
			MinConns:        int32(l.int("DB_MIN_CONNS", 0)),
			MaxConnLifetime: l.duration("DB_MAX_CONN_LIFETIME", time.Hour),
			MaxConnIdleTime: l.duration("DB_MAX_CONN_IDLE_TIME", 30*time.Minute),
			ConnectTimeout:  l.duration("DB_CONNECT_TIMEOUT", 5*time.Second),
		},
		Redis: RedisConfig{URL: l.str("REDIS_URL", "")},
		CORS: CORSConfig{
			AllowedOrigins: l.list("CORS_ALLOWED_ORIGINS", nil),
			AllowLocalhost: l.bool("CORS_ALLOW_LOCALHOST", env.IsDevelopment()),
		},
		Worker: WorkerConfig{
			HeartbeatInterval: l.duration("WORKER_HEARTBEAT_INTERVAL", 30*time.Second),
		},
		Auth: AuthConfig{
			JWTSecret:        l.str("JWT_SECRET", devSecret(env, "dev-only-jwt-secret-change-me-0123456789")),
			Issuer:           l.str("JWT_ISSUER", "equiprent"),
			AccessTokenTTL:   l.duration("ACCESS_TOKEN_TTL", 15*time.Minute),
			RefreshTokenTTL:  l.duration("REFRESH_TOKEN_TTL", 30*24*time.Hour),
			OTPSecret:        l.str("OTP_SECRET", devSecret(env, "dev-only-otp-secret-change-me-0123456789")),
			OTPTTL:           l.duration("OTP_TTL", 5*time.Minute),
			DevSimulation:    l.bool("AUTH_DEV_SIMULATION", l.bool("OTP_DEV_ECHO", env.IsDevelopment() || env == EnvTest)),
			SMSProvider:      strings.ToLower(l.str("SMS_PROVIDER", "")),
			SMSAPIURL:        l.str("SMS_API_URL", ""),
			SMSToken:         l.str("SMS_API_TOKEN", ""),
			EmailProvider:    strings.ToLower(l.str("EMAIL_PROVIDER", "")),
			EmailAPIURL:      l.str("EMAIL_API_URL", ""),
			EmailToken:       l.str("EMAIL_API_TOKEN", ""),
			EmailFrom:        l.str("EMAIL_FROM", "EQUIPRENT <no-reply@equiprent.in>"),
			LoginMaxAttempts: l.int("AUTH_LOGIN_MAX_ATTEMPTS", 10),
			LoginWindow:      l.duration("AUTH_LOGIN_WINDOW", 15*time.Minute),
			EmailTokenTTL:    l.duration("AUTH_EMAIL_TOKEN_TTL", 24*time.Hour),
			ResetTokenTTL:    l.duration("AUTH_RESET_TOKEN_TTL", 30*time.Minute),
		},
		RateLimit: RateLimitConfig{
			Enabled:       l.bool("RATE_LIMIT_ENABLED", true),
			PerMinute:     l.int("RATE_LIMIT_PER_MINUTE", 300),
			AuthPerMinute: l.int("RATE_LIMIT_AUTH_PER_MINUTE", 20),
		},
		Payments: PaymentsConfig{
			Provider:      strings.ToLower(l.str("PAYMENT_PROVIDER", "razorpay")),
			KeyID:         l.str("PAYMENT_KEY_ID", ""),
			KeySecret:     l.str("PAYMENT_KEY_SECRET", ""),
			WebhookSecret: l.str("PAYMENT_WEBHOOK_SECRET", devSecret(env, "dev-only-webhook-secret")),
		},
		Tracking: TrackingConfig{
			AllowSimulated: l.bool("TRACKING_ALLOW_SIMULATED", !env.IsProduction()),
			IdleTimeout:    l.duration("TRACKING_IDLE_TIMEOUT", 3*time.Hour),
			TicketTTL:      l.duration("TRACKING_TICKET_TTL", time.Minute),
			PolicyVersion:  l.str("TRACKING_POLICY_VERSION", "2026-11"),
		},
		Legal: LegalConfig{RequireReviewed: l.bool("LEGAL_REQUIRE_REVIEWED", env == EnvStaging || env.IsProduction())},
		Maps:  MapsConfig{GoogleAPIKey: l.str("GOOGLE_MAPS_API_KEY", "")},
		Push: PushConfig{
			FCMCredentials: l.str("FCM_CREDENTIALS", ""),
			FCMProjectID:   l.str("FCM_PROJECT_ID", ""),
			AppURL:         l.str("APP_PUBLIC_URL", ""),
		},
		ESign: ESignConfig{
			Provider:      strings.ToLower(l.str("ESIGN_PROVIDER", "builtin")),
			WebhookSecret: l.str("ESIGN_WEBHOOK_SECRET", devSecret(env, "dev-only-esign-webhook-secret")),
		},
		Booking: BookingConfig{
			OwnerResponseWindow: l.duration("BOOKING_OWNER_RESPONSE_WINDOW", 2*time.Hour),
			AdvanceBps:          l.int("BOOKING_ADVANCE_BPS", 5000),
			GSTRateBps:          l.int("GST_RATE_BPS", 1800),
			DefaultDepositPaise: int64(l.int("BOOKING_DEFAULT_DEPOSIT_PAISE", 0)),
			PaymentWindow:       l.duration("BOOKING_PAYMENT_WINDOW", 24*time.Hour),
			ProcessingTimeout:   l.duration("BOOKING_PAYMENT_PROCESSING_TIMEOUT", 30*time.Minute),
			SettlementHold:      l.duration("BOOKING_SETTLEMENT_HOLD", 72*time.Hour),
			NoShowGrace:         l.duration("BOOKING_NO_SHOW_GRACE", 0),
			OverdueFeeBpsPerDay: l.int("BOOKING_OVERDUE_FEE_BPS_PER_DAY", 0),
			TurnaroundBuffer:    l.duration("BOOKING_TURNAROUND_BUFFER", 0),
			CancelPenaltyBps:    l.int("BOOKING_CANCEL_PENALTY_BPS", 0),
		},
		Docs: DocsConfig{
			Enabled:   l.bool("API_DOCS_ENABLED", !env.IsProduction()),
			PublicURL: l.str("API_PUBLIC_URL", ""),
		},
	}
	cfg.Storage = StorageConfig{
		Driver: strings.ToLower(l.str("STORAGE_DRIVER", "local")),
		Dir:    l.str("STORAGE_DIR", "var/objects"),
		S3: S3Config{
			Endpoint:        l.str("STORAGE_S3_ENDPOINT", ""),
			Region:          l.str("STORAGE_S3_REGION", "us-east-1"),
			Bucket:          l.str("STORAGE_S3_BUCKET", ""),
			AccessKeyID:     l.str("STORAGE_S3_ACCESS_KEY_ID", ""),
			SecretAccessKey: l.str("STORAGE_S3_SECRET_ACCESS_KEY", ""),
			PathStyle:       l.bool("STORAGE_S3_PATH_STYLE", true),
		},
		SigningSecret:  l.str("STORAGE_SIGNING_SECRET", devSecret(env, "dev-only-storage-signing-secret-0123456789")),
		URLTTL:         l.duration("STORAGE_URL_TTL", 5*time.Minute),
		AvatarURLTTL:   l.duration("STORAGE_AVATAR_URL_TTL", time.Hour),
		MaxUploadBytes: int64(l.int("STORAGE_MAX_UPLOAD_BYTES", 10<<20)),
	}
	cfg.Security = SecurityConfig{
		FieldEncryptionKey: l.str("FIELD_ENCRYPTION_KEY", devSecret(env, "ZGV2LW9ubHktZmllbGQta2V5LWV4YWN0bHktMzItYnk=")),
		LookupHashKey:      l.str("LOOKUP_HASH_KEY", devSecret(env, "dev-only-lookup-hash-key-0123456789abcdef")),
	}
	if cfg.Docs.PublicURL == "" {
		cfg.Docs.PublicURL = fmt.Sprintf("http://localhost:%d", cfg.HTTP.Port)
	}

	cfg.validate(l)
	if len(l.errs) > 0 {
		return Config{}, fmt.Errorf("invalid configuration: %w", errors.Join(l.errs...))
	}
	return cfg, nil
}

func (c Config) validate(l *loader) {
	if c.HTTP.Port < 1 || c.HTTP.Port > 65535 {
		l.fail("HTTP_PORT", fmt.Errorf("must be between 1 and 65535"))
	}
	if c.HTTP.MaxBodyBytes <= 0 {
		l.fail("HTTP_MAX_BODY_BYTES", fmt.Errorf("must be positive"))
	}
	if c.Log.Format != "text" && c.Log.Format != "json" {
		l.fail("LOG_FORMAT", fmt.Errorf("must be text or json"))
	}
	if c.Database.MaxConns < 1 {
		l.fail("DB_MAX_CONNS", fmt.Errorf("must be at least 1"))
	}
	if c.Database.MinConns < 0 || c.Database.MinConns > c.Database.MaxConns {
		l.fail("DB_MIN_CONNS", fmt.Errorf("must be between 0 and DB_MAX_CONNS"))
	}
	if c.Database.URL != "" {
		if u, err := url.Parse(c.Database.URL); err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") {
			l.fail("DATABASE_URL", fmt.Errorf("must be a postgres:// URL"))
		}
	}
	if (c.Env == EnvStaging || c.Env.IsProduction()) && c.Redis.URL == "" {
		l.fail("REDIS_URL", errors.New("is required in staging and production (shared rate limits, OTP cooldowns and login lockouts)"))
	}
	if c.Redis.URL != "" {
		if u, err := url.Parse(c.Redis.URL); err != nil || (u.Scheme != "redis" && u.Scheme != "rediss") {
			l.fail("REDIS_URL", fmt.Errorf("must be a redis:// or rediss:// URL"))
		}
	}
	if c.Worker.HeartbeatInterval <= 0 {
		l.fail("WORKER_HEARTBEAT_INTERVAL", fmt.Errorf("must be positive"))
	}
	if c.Env.IsProduction() && c.CORS.AllowLocalhost {
		l.fail("CORS_ALLOW_LOCALHOST", fmt.Errorf("must not be enabled in production"))
	}
	deployed := c.Env == EnvStaging || c.Env.IsProduction()
	if c.Env.IsProduction() && c.Tracking.AllowSimulated {
		l.fail("TRACKING_ALLOW_SIMULATED", errors.New("simulated positions must not be accepted in production"))
	}
	if c.Tracking.IdleTimeout < 10*time.Minute || c.Tracking.TicketTTL <= 0 || c.Tracking.TicketTTL > 10*time.Minute {
		l.fail("TRACKING_IDLE_TIMEOUT", errors.New("idle timeout must be at least 10m and the ticket TTL at most 10m"))
	}
	if c.Env.IsProduction() && !c.Legal.RequireReviewed {
		l.fail("LEGAL_REQUIRE_REVIEWED", errors.New("must stay on in production (agreements and invoices need reviewed terms and tax rules)"))
	}
	for key, secret := range map[string]string{
		"JWT_SECRET": c.Auth.JWTSecret, "OTP_SECRET": c.Auth.OTPSecret, "PAYMENT_WEBHOOK_SECRET": c.Payments.WebhookSecret,
		"ESIGN_WEBHOOK_SECRET": c.ESign.WebhookSecret,
	} {
		switch {
		case secret == "":
			l.fail(key, errors.New("is required outside development"))
		case deployed && (len(secret) < 32 || strings.HasPrefix(secret, "dev-only")):
			l.fail(key, errors.New("must be a random value of at least 32 characters"))
		}
	}
	for key, secret := range map[string]string{"STORAGE_SIGNING_SECRET": c.Storage.SigningSecret, "LOOKUP_HASH_KEY": c.Security.LookupHashKey} {
		switch {
		case secret == "":
			l.fail(key, errors.New("is required outside development"))
		case deployed && (len(secret) < 32 || strings.HasPrefix(secret, "dev-only")):
			l.fail(key, errors.New("must be a random value of at least 32 characters"))
		}
	}
	if k, err := base64.StdEncoding.DecodeString(c.Security.FieldEncryptionKey); err != nil || len(k) != 32 {
		l.fail("FIELD_ENCRYPTION_KEY", errors.New("must be 32 random bytes, base64-encoded (openssl rand -base64 32)"))
	} else if deployed && c.Security.FieldEncryptionKey == "ZGV2LW9ubHktZmllbGQta2V5LWV4YWN0bHktMzItYnk=" {
		l.fail("FIELD_ENCRYPTION_KEY", errors.New("the development key must not be used outside development"))
	}
	switch c.Storage.Driver {
	case "local":
		if deployed {
			l.fail("STORAGE_DRIVER", errors.New("local storage is for development; use s3 (an S3-compatible bucket) when deployed"))
		}
	case "s3":
		s3 := c.Storage.S3
		for key, v := range map[string]string{"STORAGE_S3_ENDPOINT": s3.Endpoint, "STORAGE_S3_BUCKET": s3.Bucket,
			"STORAGE_S3_ACCESS_KEY_ID": s3.AccessKeyID, "STORAGE_S3_SECRET_ACCESS_KEY": s3.SecretAccessKey, "STORAGE_S3_REGION": s3.Region} {
			if v == "" {
				l.fail(key, errors.New("is required when STORAGE_DRIVER=s3"))
			}
		}
		if deployed && !strings.HasPrefix(s3.Endpoint, "https://") {
			l.fail("STORAGE_S3_ENDPOINT", errors.New("must use https outside development"))
		}
	default:
		l.fail("STORAGE_DRIVER", fmt.Errorf("unknown driver %q (local or s3)", c.Storage.Driver))
	}
	if c.Storage.URLTTL <= 0 || c.Storage.URLTTL > 15*time.Minute {
		l.fail("STORAGE_URL_TTL", errors.New("must be positive and at most 15m (identity documents)"))
	}
	if c.Storage.MaxUploadBytes < 1<<20 || c.Storage.MaxUploadBytes > 10<<20 {
		l.fail("STORAGE_MAX_UPLOAD_BYTES", errors.New("must be between 1 MiB and 10 MiB"))
	}
	if c.Auth.AccessTokenTTL <= 0 || c.Auth.RefreshTokenTTL <= c.Auth.AccessTokenTTL {
		l.fail("REFRESH_TOKEN_TTL", errors.New("must be longer than ACCESS_TOKEN_TTL (both positive)"))
	}
	if c.Auth.OTPTTL < time.Minute || c.Auth.OTPTTL > 15*time.Minute {
		l.fail("OTP_TTL", errors.New("must be between 1m and 15m"))
	}
	if deployed && c.Auth.DevSimulation {
		l.fail("AUTH_DEV_SIMULATION", errors.New("must not be enabled in staging or production (it exposes OTPs and email tokens)"))
	}
	validateDelivery := func(name, provider, endpoint, token string) {
		if provider == "" {
			if endpoint != "" || token != "" {
				l.fail(name+"_PROVIDER", errors.New("must be set when delivery credentials are provided"))
			}
			return
		}
		if provider != "http_json" {
			l.fail(name+"_PROVIDER", fmt.Errorf("unsupported provider %q (supported: http_json)", provider))
			return
		}
		u, err := url.Parse(endpoint)
		if err != nil || u.Host == "" || (u.Scheme != "https" && !(c.Env.IsDevelopment() || c.Env == EnvTest)) {
			l.fail(name+"_API_URL", errors.New("must be an absolute HTTPS URL (HTTP is allowed only in development/test)"))
		}
		if token == "" {
			l.fail(name+"_API_TOKEN", errors.New("is required when provider is configured"))
		}
	}
	validateDelivery("SMS", c.Auth.SMSProvider, c.Auth.SMSAPIURL, c.Auth.SMSToken)
	validateDelivery("EMAIL", c.Auth.EmailProvider, c.Auth.EmailAPIURL, c.Auth.EmailToken)
	if c.Auth.LoginMaxAttempts < 3 || c.Auth.LoginWindow < time.Minute {
		l.fail("AUTH_LOGIN_MAX_ATTEMPTS", errors.New("must be at least 3 attempts over at least 1m"))
	}
	if c.Auth.ResetTokenTTL <= 0 || c.Auth.ResetTokenTTL > 2*time.Hour || c.Auth.EmailTokenTTL <= 0 {
		l.fail("AUTH_RESET_TOKEN_TTL", errors.New("must be positive and at most 2h"))
	}
	if c.RateLimit.PerMinute < 1 || c.RateLimit.AuthPerMinute < 1 {
		l.fail("RATE_LIMIT_PER_MINUTE", errors.New("rate limits must be positive"))
	}
	if c.Payments.Provider != "razorpay" {
		l.fail("PAYMENT_PROVIDER", fmt.Errorf("unsupported provider %q (supported: razorpay)", c.Payments.Provider))
	}
	if c.Env.IsProduction() && c.Payments.Sandbox() {
		l.fail("PAYMENT_KEY_ID", errors.New("gateway credentials are required in production"))
	}
	switch c.ESign.Provider {
	case "builtin":
	case "sandbox":
		if c.Env.IsProduction() {
			l.fail("ESIGN_PROVIDER", errors.New("the sandbox e-signature provider is for development only"))
		}
	default:
		l.fail("ESIGN_PROVIDER", fmt.Errorf("unsupported provider %q (supported: builtin, sandbox)", c.ESign.Provider))
	}
	if c.Booking.AdvanceBps < 0 || c.Booking.AdvanceBps > 10000 {
		l.fail("BOOKING_ADVANCE_BPS", errors.New("must be between 0 and 10000"))
	}
	if c.Booking.CancelPenaltyBps < 0 || c.Booking.CancelPenaltyBps > 10000 || c.Booking.OverdueFeeBpsPerDay < 0 || c.Booking.OverdueFeeBpsPerDay > 10000 ||
		c.Booking.NoShowGrace < 0 || c.Booking.TurnaroundBuffer < 0 {
		l.fail("BOOKING_CANCEL_PENALTY_BPS", errors.New("penalty, fee, grace and buffer values must be >= 0 (bps <= 10000)"))
	}
	if c.Booking.GSTRateBps < 0 || c.Booking.GSTRateBps > 2800 {
		l.fail("GST_RATE_BPS", errors.New("must be between 0 and 2800"))
	}
	if c.Booking.OwnerResponseWindow < time.Minute {
		l.fail("BOOKING_OWNER_RESPONSE_WINDOW", errors.New("must be at least 1m"))
	}
	if c.Booking.PaymentWindow < time.Minute {
		l.fail("BOOKING_PAYMENT_WINDOW", errors.New("must be at least 1m"))
	}
	if c.Booking.ProcessingTimeout < time.Minute {
		l.fail("BOOKING_PAYMENT_PROCESSING_TIMEOUT", errors.New("must be at least 1m"))
	}
}

// devSecret returns a fixed, clearly-marked secret for local development and
// tests so the API starts without setup; deployed environments must set real
// values (enforced in validate).
func devSecret(env Environment, value string) string {
	if env.IsDevelopment() || env == EnvTest {
		return value
	}
	return ""
}

type loader struct {
	lookup func(string) (string, bool)
	errs   []error
}

func (l *loader) fail(key string, err error) { l.errs = append(l.errs, fmt.Errorf("%s: %w", key, err)) }

func (l *loader) raw(key string) (string, bool) {
	v, ok := l.lookup(key)
	v = strings.TrimSpace(v)
	if ok && v != "" {
		return v, true
	}
	// KEY_FILE points at a file holding the value (Docker/Kubernetes secrets), so secrets need
	// not sit in the process environment.
	if path, ok := l.lookup(key + "_FILE"); ok && strings.TrimSpace(path) != "" {
		b, err := readSecretFile(strings.TrimSpace(path))
		if err != nil {
			l.fail(key+"_FILE", err)
			return "", false
		}
		if s := strings.TrimSpace(string(b)); s != "" {
			return s, true
		}
	}
	return "", false
}

// readSecretFile reads a small secret file; the error never contains the content.
func readSecretFile(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, errors.New("cannot be read")
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, 64<<10))
	if err != nil {
		return nil, errors.New("cannot be read")
	}
	return b, nil
}

// looksLocal reports whether the process is plainly a developer machine: no DATABASE_URL, or
// one whose host is loopback.
func looksLocal(lookup func(string) (string, bool)) bool {
	raw, ok := lookup("DATABASE_URL")
	if !ok || strings.TrimSpace(raw) == "" {
		return true
	}
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	switch u.Hostname() {
	case "localhost", "127.0.0.1", "::1":
		return true
	}
	return false
}

func (l *loader) str(key, def string) string {
	if v, ok := l.raw(key); ok {
		return v
	}
	return def
}

func (l *loader) required(key string) string {
	v, ok := l.raw(key)
	if !ok {
		l.fail(key, errors.New("is required"))
	}
	return v
}

func (l *loader) int(key string, def int) int {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		l.fail(key, fmt.Errorf("must be an integer, got %q", v))
		return def
	}
	return n
}

func (l *loader) bool(key string, def bool) bool {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		l.fail(key, fmt.Errorf("must be true or false, got %q", v))
		return def
	}
	return b
}

func (l *loader) duration(key string, def time.Duration) time.Duration {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		l.fail(key, fmt.Errorf("must be a duration like 15s or 1m, got %q", v))
		return def
	}
	return d
}

func (l *loader) list(key string, def []string) []string {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	var out []string
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (l *loader) logLevel(key string, def slog.Level) slog.Level {
	v, ok := l.raw(key)
	if !ok {
		return def
	}
	var lvl slog.Level
	if err := lvl.UnmarshalText([]byte(v)); err != nil {
		l.fail(key, fmt.Errorf("must be debug, info, warn or error, got %q", v))
		return def
	}
	return lvl
}
