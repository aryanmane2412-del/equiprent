// Package app is the composition root: it builds every module's repository,
// service and handler from the shared infrastructure and exposes them to the
// HTTP server and the worker.
package app

import (
	"context"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/equiprent/backend/internal/access"
	"github.com/equiprent/backend/internal/cache"
	"github.com/equiprent/backend/internal/config"
	"github.com/google/uuid"

	"github.com/equiprent/backend/internal/apperror"
	"github.com/equiprent/backend/internal/database"
	"github.com/equiprent/backend/internal/db"
	"github.com/equiprent/backend/internal/fieldcrypt"
	"github.com/equiprent/backend/internal/geo"
	"github.com/equiprent/backend/internal/httpx"
	"github.com/equiprent/backend/internal/idempotency"
	"github.com/equiprent/backend/internal/live"
	"github.com/equiprent/backend/internal/modules/admin"
	"github.com/equiprent/backend/internal/modules/agreements"
	"github.com/equiprent/backend/internal/modules/auth"
	"github.com/equiprent/backend/internal/modules/availability"
	"github.com/equiprent/backend/internal/modules/bookings"
	"github.com/equiprent/backend/internal/modules/chat"
	"github.com/equiprent/backend/internal/modules/discovery"
	"github.com/equiprent/backend/internal/modules/disputes"
	"github.com/equiprent/backend/internal/modules/equipment"
	"github.com/equiprent/backend/internal/modules/fleet"
	"github.com/equiprent/backend/internal/modules/invoices"
	"github.com/equiprent/backend/internal/modules/maintenance"
	"github.com/equiprent/backend/internal/modules/notifications"
	"github.com/equiprent/backend/internal/modules/operations"
	"github.com/equiprent/backend/internal/modules/operators"
	"github.com/equiprent/backend/internal/modules/owners"
	"github.com/equiprent/backend/internal/modules/payments"
	"github.com/equiprent/backend/internal/modules/reviews"
	"github.com/equiprent/backend/internal/modules/search"
	"github.com/equiprent/backend/internal/modules/tracking"
	"github.com/equiprent/backend/internal/modules/uploads"
	"github.com/equiprent/backend/internal/modules/users"
	"github.com/equiprent/backend/internal/modules/verification"
	"github.com/equiprent/backend/internal/ratelimit"
	"github.com/equiprent/backend/internal/storage"
)

// App holds the wired modules.
type App struct {
	Modules       []httpx.Module
	Guards        httpx.Guards
	Limiter       *ratelimit.Limiter
	Auth          *auth.Service
	Bookings      *bookings.Service
	Notifications *notifications.Service
	Payments      *payments.Service
	Disputes      *disputes.Service
	Tracking      *tracking.Service
	Tx            database.Transactor
}

// New wires all modules. pool may be a pool that cannot reach the database
// yet (routes still register; requests fail until it is up).
func New(cfg config.Config, log *slog.Logger, pool *pgxpool.Pool, store cache.Store) *App {
	tx := database.NewTxManager(pool)
	limiter := ratelimit.New(store, log)

	tokens := auth.NewTokens(cfg.Auth.JWTSecret, cfg.Auth.Issuer, cfg.Auth.AccessTokenTTL)
	// Delivery: development simulation or configured HTTPS JSON providers.
	var sms auth.SMSSender = auth.DisabledSMS{}
	var email auth.EmailSender = auth.DisabledEmail{}
	if cfg.Auth.DevSimulation {
		sim := auth.DevSimulator{Log: log}
		sms, email = sim, sim
		log.Warn("auth DEV SIMULATION is on: OTPs and email tokens are returned in API responses (never enable outside development)")
	} else {
		if cfg.Auth.SMSProvider == "http_json" {
			sms = auth.SMSHTTP{HTTPJSON: auth.HTTPJSON{Endpoint: cfg.Auth.SMSAPIURL, Token: cfg.Auth.SMSToken}}
			log.Info("authentication SMS delivery enabled", "provider", cfg.Auth.SMSProvider)
		}
		if cfg.Auth.EmailProvider == "http_json" {
			email = auth.EmailHTTP{HTTPJSON: auth.HTTPJSON{Endpoint: cfg.Auth.EmailAPIURL, Token: cfg.Auth.EmailToken}, From: cfg.Auth.EmailFrom}
			log.Info("authentication email delivery enabled", "provider", cfg.Auth.EmailProvider)
		}
		if !cfg.Auth.OTPEnabled() || !cfg.Auth.EmailEnabled() {
			log.Info("authentication delivery channel disabled: configure SMS_PROVIDER and EMAIL_PROVIDER; password sign-in remains available")
		}
	}
	authSvc := auth.NewService(auth.NewRepository(pool, tx), tokens, store, sms, email, cfg.Auth, log)
	equipmentAccess := access.NewChecker(pool)
	guards := httpx.Guards{
		Authenticate:        auth.Middleware(authSvc),
		Idempotency:         idempotency.Middleware(idempotency.NewPGStore(pool), log),
		RequirePlatformRole: auth.RequirePlatformRole,
		EquipmentAccess:     access.Guard(equipmentAccess),
	}

	// Private documents: stored outside any public path, readable only
	// through short-lived signed links minted after an access check.
	crypt, err := fieldcrypt.New(cfg.Security.FieldEncryptionKey, cfg.Security.LookupHashKey)
	if err != nil {
		panic("field encryption key: " + err.Error()) // config validation rejects bad keys first
	}
	signer := storage.NewSigner(cfg.Storage.SigningSecret, strings.TrimRight(cfg.Docs.PublicURL, "/")+"/api/v1/files") // httpserver.APIPrefix + the uploads route
	uploadSvc := uploads.NewService(uploads.NewRepository(pool), NewStore(cfg.Storage), signer, log)
	payouts := newPayoutProvider(cfg)
	verifySvc := verification.NewService(verification.NewRepository(pool, tx), tx, uploadSvc, crypt, payouts,
		verification.Options{URLTTL: cfg.Storage.URLTTL, AvatarURLTTL: cfg.Storage.AvatarURLTTL})
	var authLimit = limiter.Middleware(ratelimit.Rule{Name: "auth", Limit: cfg.RateLimit.AuthPerMinute, Window: time.Minute, FailClosed: cfg.Env.IsProduction()}, ratelimit.ByIP)
	if !cfg.RateLimit.Enabled {
		authLimit = nil
	}

	equipmentSvc := equipment.NewService(equipment.NewRepository(pool, tx))
	equipmentSvc.WithAudit(tx)
	equipmentSvc.WithMedia(uploadSvc, equipment.MediaOptions{PhotoTTL: cfg.Storage.AvatarURLTTL, DocumentTTL: cfg.Storage.URLTTL})
	bookingSvc := bookings.NewService(bookings.NewRepository(pool), tx, cfg.Booking).WithScope(equipmentAccess)
	inspectionSvc := operations.NewService(pool, tx, bookingSvc, uploadSvc, cfg.Storage.URLTTL).WithDemoGate(cfg.Env != config.EnvProduction && os.Getenv("DEMO_SKIP_INSPECTION_GATE") == "true")
	bookingSvc.WithInspectionGate(inspectionSvc.InspectionAcknowledgements).WithInspectionRecorder(inspectionSvc.RecordInspection)
	notifyRepo := notifications.NewRepository(pool, tx)
	notifySvc := notifications.NewService(notifyRepo, newPusher(cfg, notifyRepo, log), log)
	var esign agreements.Provider = agreements.Builtin{}
	if cfg.ESign.Provider == "sandbox" {
		esign = agreements.Sandbox{Secret: cfg.ESign.WebhookSecret}
		log.Warn("e-signature provider in SANDBOX mode: signing sessions are simulated")
	}
	gateway := payments.NewRazorpay(cfg.Payments.KeyID, cfg.Payments.KeySecret, cfg.Payments.WebhookSecret)
	if gateway.Sandbox() {
		log.Warn("payment gateway in SANDBOX mode: orders are simulated (set PAYMENT_KEY_ID / PAYMENT_KEY_SECRET)")
	}

	// Location: Google Directions for arrival estimates when a server key is
	// configured, otherwise straight-line estimates (labelled as such).
	var router geo.Router = geo.StraightLine{}
	if cfg.Maps.Google() {
		router = geo.NewGoogle(cfg.Maps.GoogleAPIKey)
	}
	trackingSvc := tracking.NewService(tracking.NewRepository(pool), bookingSvc, store, tx).WithLocation(pool, live.NewHub(),
		live.NewTickets(cfg.Auth.JWTSecret, cfg.Tracking.TicketTTL), router, equipmentAccess,
		func(ctx context.Context, userID, _ uuid.UUID) (*httpx.Principal, error) {
			q := database.Queries(ctx, pool)
			u, err := q.GetUserByID(ctx, userID)
			if err != nil || u.Status != db.AccountStatusActive {
				return nil, apperror.ErrUnauthenticated
			}
			roles, err := auth.LoadRoles(ctx, q, userID)
			if err != nil {
				return nil, err
			}
			return &httpx.Principal{UserID: userID, Roles: roles}, nil
		},
		tracking.Options{PolicyVersion: cfg.Tracking.PolicyVersion, AllowSimulated: cfg.Tracking.AllowSimulated,
			IdleTimeout: cfg.Tracking.IdleTimeout, Origins: wsOrigins(cfg)})

	paymentSvc := payments.NewService(payments.NewRepository(pool), bookingSvc, gateway, tx, log)
	disputeSvc := disputes.NewService(disputes.NewRepository(pool), bookingSvc, tx)

	var geocoder discovery.Geocoder
	if cfg.Maps.Google() {
		geocoder = discovery.NewGoogleGeocoder(cfg.Maps.GoogleAPIKey)
	}

	modules := []httpx.Module{
		auth.NewHandler(authSvc, authLimit),
		users.NewHandler(users.NewService(users.NewRepository(pool, tx)).WithReauth(
			func(ctx context.Context, id uuid.UUID, pw string) error {
				return authSvc.ConfirmPassword(ctx, id, pw, auth.ClientInfo{})
			})),
		owners.NewHandler(owners.NewService(owners.NewRepository(pool, tx))),
		operators.NewHandler(operators.NewService(operators.NewRepository(pool, tx))),
		equipment.NewHandler(equipmentSvc, auth.OptionalMiddleware(authSvc), equipmentAccess),
		search.NewHandler(search.NewService(search.NewRepository(pool)).WithPhotoLinks(func(key string) string {
			return uploadSvc.CacheableURL(key, cfg.Storage.AvatarURLTTL).URL
		})),
		discovery.NewHandler(discovery.NewService(discovery.NewRepository(pool), equipmentSvc, geocoder, func(key string) string {
			return uploadSvc.CacheableURL(key, cfg.Storage.AvatarURLTTL).URL
		}), auth.OptionalMiddleware(authSvc)),
		availability.NewHandler(availability.NewService(availability.NewRepository(pool))),
		bookings.NewHandler(bookingSvc),
		payments.NewHandler(paymentSvc),
		fleet.NewHandler(fleet.NewService(fleet.NewRepository(pool, tx), equipmentAccess)),
		operations.NewHandler(inspectionSvc),
		tracking.NewHandler(trackingSvc),
		maintenance.NewHandler(maintenance.NewService(maintenance.NewRepository(pool, tx), tx, uploadSvc, cfg.Storage.URLTTL), equipmentAccess),
		notifications.NewHandler(notifySvc),
		chat.NewHandler(chat.NewService(chat.NewRepository(pool, tx), bookingSvc, uploadSvc, tx, cfg.Storage.AvatarURLTTL)),
		invoices.NewHandler(invoices.NewService(invoices.NewRepository(pool, tx), bookingSvc, cfg.Booking).RequireReviewedTaxRules(cfg.Legal.RequireReviewed)).
			WithLinks(invoices.NewLinks(cfg.Storage.SigningSecret, strings.TrimRight(cfg.Docs.PublicURL, "/")+"/api/v1", cfg.Storage.URLTTL)),
		agreements.NewHandler(agreements.NewService(agreements.NewRepository(pool), bookingSvc, uploadSvc, esign, tx, cfg.Storage.URLTTL, log).RequireReviewedTemplates(cfg.Legal.RequireReviewed)),
		reviews.NewHandler(reviews.NewService(reviews.NewRepository(pool), bookingSvc, tx)),
		disputes.NewHandler(disputeSvc),
		admin.NewHandler(admin.NewService(admin.NewRepository(pool), tx, equipmentSvc, bookingSvc)),
		uploads.NewHandler(uploadSvc, cfg.Storage.MaxUploadBytes),
		verification.NewHandler(verifySvc),
	}
	return &App{
		Modules: modules, Guards: guards, Limiter: limiter, Auth: authSvc,
		Bookings: bookingSvc, Notifications: notifySvc, Payments: paymentSvc, Disputes: disputeSvc, Tracking: trackingSvc, Tx: tx,
	}
}

// newPusher sends push notifications through FCM when credentials are
// configured, and only logs them otherwise.
func newPusher(cfg config.Config, devices notifications.DeviceStore, log *slog.Logger) notifications.Pusher {
	if !cfg.Push.FCM() {
		log.Info("push notifications are logged only: set FCM_CREDENTIALS to send them through Firebase Cloud Messaging")
		return notifications.LogPusher{Log: log}
	}
	sa, err := notifications.LoadServiceAccount(cfg.Push.FCMCredentials)
	if err == nil {
		var f *notifications.FCMPusher
		if f, err = notifications.NewFCMPusher(sa, cfg.Push.FCMProjectID, devices, log); err == nil {
			f.AppURL = cfg.Push.AppURL
			log.Info("push notifications through Firebase Cloud Messaging", "project", f.ProjectID)
			return f
		}
	}
	log.Error("FCM credentials are invalid; push notifications are logged only", "error", err)
	return notifications.LogPusher{Log: log}
}

// NewStore builds the configured private object store.
func NewStore(c config.StorageConfig) storage.Store {
	if c.Driver == "s3" {
		return &storage.S3{
			Endpoint: c.S3.Endpoint, Region: c.S3.Region, Bucket: c.S3.Bucket,
			AccessKey: c.S3.AccessKeyID, SecretKey: c.S3.SecretAccessKey, PathStyle: c.S3.PathStyle,
		}
	}
	return storage.Local{Dir: c.Dir}
}

// wsOrigins are the browser origins allowed to open WebSockets (the CORS
// allow-list; localhost in development).
func wsOrigins(cfg config.Config) []string {
	var out []string
	for _, o := range cfg.CORS.AllowedOrigins {
		out = append(out, strings.TrimPrefix(strings.TrimPrefix(o, "https://"), "http://"))
	}
	if cfg.CORS.AllowLocalhost {
		out = append(out, "localhost:*", "127.0.0.1:*")
	}
	return out
}
