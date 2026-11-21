package config

import (
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func lookupFrom(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) { v, ok := m[k]; return v, ok }
}

func TestLoadDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://u:p@localhost:5433/equiprent?sslmode=disable",
	}))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Env != EnvDevelopment {
		t.Errorf("Env = %q, want development", cfg.Env)
	}
	if cfg.HTTP.Addr() != "0.0.0.0:8080" {
		t.Errorf("Addr = %q", cfg.HTTP.Addr())
	}
	if cfg.Log.Format != "text" || cfg.Log.Level != slog.LevelInfo {
		t.Errorf("log = %+v", cfg.Log)
	}
	if cfg.Redis.Enabled() {
		t.Error("redis should be disabled when REDIS_URL is empty")
	}
	if !cfg.CORS.AllowLocalhost {
		t.Error("localhost CORS should default on in development")
	}
	if cfg.Worker.HeartbeatInterval != 30*time.Second {
		t.Errorf("heartbeat = %v", cfg.Worker.HeartbeatInterval)
	}
}

// productionEnv is the minimum valid production configuration.
func productionEnv(extra map[string]string) map[string]string {
	m := map[string]string{
		"APP_ENV":                      "production",
		"DATABASE_URL":                 "postgres://u:p@db:5432/equiprent",
		"REDIS_URL":                    "redis://cache:6379/0",
		"JWT_SECRET":                   strings.Repeat("j", 40),
		"OTP_SECRET":                   strings.Repeat("o", 40),
		"PAYMENT_WEBHOOK_SECRET":       strings.Repeat("w", 40),
		"ESIGN_WEBHOOK_SECRET":         strings.Repeat("e", 40),
		"PAYMENT_KEY_ID":               "rzp_live_x",
		"PAYMENT_KEY_SECRET":           "secret",
		"STORAGE_SIGNING_SECRET":       strings.Repeat("s", 40),
		"LOOKUP_HASH_KEY":              strings.Repeat("h", 40),
		"FIELD_ENCRYPTION_KEY":         "MDEyMzQ1Njc4OWFiY2RlZjAxMjM0NTY3ODlhYmNkZWY=",
		"STORAGE_DRIVER":               "s3",
		"STORAGE_S3_ENDPOINT":          "https://s3.ap-south-1.amazonaws.com",
		"STORAGE_S3_REGION":            "ap-south-1",
		"STORAGE_S3_BUCKET":            "equiprent-private",
		"STORAGE_S3_ACCESS_KEY_ID":     "AKIA_TEST",
		"STORAGE_S3_SECRET_ACCESS_KEY": "secret",
	}
	maps.Copy(m, extra)
	return m
}

func TestLoadProductionDefaultsToJSONLogs(t *testing.T) {
	cfg, err := Load(lookupFrom(productionEnv(nil)))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("format = %q, want json", cfg.Log.Format)
	}
	if cfg.CORS.AllowLocalhost {
		t.Error("localhost CORS must be off in production")
	}
	if !cfg.Redis.Enabled() {
		t.Error("redis should be enabled")
	}
	if cfg.Auth.DevSimulation || cfg.Auth.OTPEnabled() || cfg.Docs.Enabled {
		t.Error("OTP echo and API docs must default off in production")
	}
}

func TestProductionRequiresStrongSecrets(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{
		"APP_ENV":      "production",
		"DATABASE_URL": "postgres://db/x",
		"JWT_SECRET":   "short",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"JWT_SECRET: must be a random value", "OTP_SECRET: is required", "PAYMENT_KEY_ID"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
}

func TestHTTPJSONDeliveryProviderConfiguration(t *testing.T) {
	base := productionEnv(map[string]string{
		"SMS_PROVIDER": "http_json", "SMS_API_URL": "https://sms.example/send", "SMS_API_TOKEN": "token",
		"EMAIL_PROVIDER": "http_json", "EMAIL_API_URL": "https://email.example/send", "EMAIL_API_TOKEN": "token", "EMAIL_FROM": "verify@example.com",
	})
	cfg, err := Load(lookupFrom(base))
	if err != nil {
		t.Fatalf("valid providers: %v", err)
	}
	if !cfg.Auth.OTPEnabled() || !cfg.Auth.EmailEnabled() {
		t.Fatal("configured providers must enable verification")
	}
	for _, extra := range []map[string]string{
		{"SMS_PROVIDER": "msg91"},
		{"SMS_PROVIDER": "http_json", "SMS_API_URL": "https://sms.example/send"},
		{"EMAIL_PROVIDER": "http_json", "EMAIL_API_URL": "http://email.example/send", "EMAIL_API_TOKEN": "token", "EMAIL_FROM": "verify@example.com"},
	} {
		_, err := Load(lookupFrom(productionEnv(extra)))
		if err == nil {
			t.Errorf("invalid delivery config accepted: %v", extra)
		}
	}
	if _, err := Load(lookupFrom(productionEnv(map[string]string{"SMS_PROVIDER": "http_json", "SMS_API_URL": "https://sms.example/send", "SMS_API_TOKEN": "token", "AUTH_DEV_SIMULATION": "true"}))); err == nil || !strings.Contains(err.Error(), "AUTH_DEV_SIMULATION") {
		t.Fatalf("production simulation must be rejected: %v", err)
	}
	staging := productionEnv(map[string]string{"APP_ENV": "staging", "OTP_DEV_ECHO": "true"})
	if _, err := Load(lookupFrom(staging)); err == nil || !strings.Contains(err.Error(), "AUTH_DEV_SIMULATION") {
		t.Fatalf("legacy OTP_DEV_ECHO must also be refused in staging: %v", err)
	}
}

func TestDevelopmentHasWorkingDefaults(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{"DATABASE_URL": "postgres://localhost/x"}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Auth.JWTSecret == "" || !cfg.Auth.DevSimulation || !cfg.Auth.OTPEnabled() || !cfg.Payments.Sandbox() || !cfg.Docs.Enabled {
		t.Errorf("dev defaults = %+v %+v %+v", cfg.Auth, cfg.Payments, cfg.Docs)
	}
	if cfg.Docs.PublicURL != "http://localhost:8080" || cfg.Booking.AdvanceBps != 5000 {
		t.Errorf("docs=%+v booking=%+v", cfg.Docs, cfg.Booking)
	}
}

func TestLoadReportsAllErrorsTogether(t *testing.T) {
	_, err := Load(lookupFrom(map[string]string{
		"APP_ENV":      "moon",
		"HTTP_PORT":    "eighty",
		"LOG_FORMAT":   "xml",
		"DB_MIN_CONNS": "50",
		"REDIS_URL":    "http://nope",
	}))
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{"APP_ENV", "HTTP_PORT", "LOG_FORMAT", "DB_MIN_CONNS", "REDIS_URL", "DATABASE_URL: is required"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %s", err, want)
		}
	}
}

func TestListParsingTrimsAndDropsEmpty(t *testing.T) {
	cfg, err := Load(lookupFrom(map[string]string{
		"DATABASE_URL":         "postgres://localhost/x",
		"CORS_ALLOWED_ORIGINS": " https://app.equiprent.in , ,https://admin.equiprent.in",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(cfg.CORS.AllowedOrigins, "|"); got != "https://app.equiprent.in|https://admin.equiprent.in" {
		t.Errorf("origins = %q", got)
	}
}

func TestProductionRejectsLocalhostCORS(t *testing.T) {
	_, err := Load(lookupFrom(productionEnv(map[string]string{"CORS_ALLOW_LOCALHOST": "true"})))
	if err == nil || !strings.Contains(err.Error(), "CORS_ALLOW_LOCALHOST") {
		t.Fatalf("expected CORS_ALLOW_LOCALHOST error, got %v", err)
	}
}

func TestStorageDriverRules(t *testing.T) {
	for name, tc := range map[string]struct {
		extra map[string]string
		key   string
	}{
		"local in production": {map[string]string{"STORAGE_DRIVER": "local"}, "STORAGE_DRIVER"},
		"s3 without bucket":   {map[string]string{"STORAGE_S3_BUCKET": ""}, "STORAGE_S3_BUCKET"},
		"s3 over http":        {map[string]string{"STORAGE_S3_ENDPOINT": "http://minio:9000"}, "STORAGE_S3_ENDPOINT"},
		"unknown driver":      {map[string]string{"STORAGE_DRIVER": "ftp"}, "STORAGE_DRIVER"},
	} {
		t.Run(name, func(t *testing.T) {
			env := productionEnv(tc.extra)
			for k, v := range tc.extra {
				if v == "" {
					delete(env, k)
				}
			}
			_, err := Load(lookupFrom(env))
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("err = %v, want %s", err, tc.key)
			}
		})
	}
	if _, err := Load(lookupFrom(map[string]string{"APP_ENV": "development", "DATABASE_URL": "postgres://u:p@localhost/db", "STORAGE_DRIVER": "s3", "STORAGE_S3_ENDPOINT": "http://localhost:9000",
		"STORAGE_S3_BUCKET": "dev", "STORAGE_S3_ACCESS_KEY_ID": "minio", "STORAGE_S3_SECRET_ACCESS_KEY": "minio123"})); err != nil {
		t.Fatalf("MinIO over http is fine in development: %v", err)
	}
}

func TestMissingAppEnv(t *testing.T) {
	// A developer machine (no database, or a loopback one) may omit APP_ENV: development.
	for _, db := range []string{"postgres://u:p@localhost:5434/equiprent?sslmode=disable", "postgres://u:p@127.0.0.1/equiprent"} {
		m := map[string]string{}
		if db != "" {
			m["DATABASE_URL"] = db
		}
		cfg, err := Load(lookupFrom(m))
		if err != nil || cfg.Env != EnvDevelopment {
			t.Fatalf("local %q: %v %v", db, cfg.Env, err)
		}
	}
	// Anywhere else a missing APP_ENV is an error, never a silent development mode.
	m := productionEnv(nil)
	delete(m, "APP_ENV")
	if _, err := Load(lookupFrom(m)); err == nil || !strings.Contains(err.Error(), "APP_ENV") {
		t.Fatalf("missing APP_ENV with a remote database: %v", err)
	}
	if _, err := Load(lookupFrom(map[string]string{"APP_ENV": "prod"})); err == nil {
		t.Fatal("an unknown APP_ENV was accepted")
	}
}

func TestProductionAndStagingRequireRedis(t *testing.T) {
	for _, env := range []string{"production", "staging"} {
		m := productionEnv(map[string]string{"APP_ENV": env})
		delete(m, "REDIS_URL")
		if _, err := Load(lookupFrom(m)); err == nil || !strings.Contains(err.Error(), "REDIS_URL") {
			t.Fatalf("%s without Redis: %v", env, err)
		}
	}
	if _, err := Load(lookupFrom(productionEnv(nil))); err != nil {
		t.Fatalf("valid production configuration: %v", err)
	}
	// Development keeps working without Redis.
	if _, err := Load(lookupFrom(map[string]string{"APP_ENV": "development", "DATABASE_URL": "postgres://u:p@localhost:5434/e"})); err != nil {
		t.Fatalf("development: %v", err)
	}
}

func TestProductionNeverEchoesOTPs(t *testing.T) {
	for _, k := range []string{"AUTH_DEV_SIMULATION", "OTP_DEV_ECHO"} {
		if _, err := Load(lookupFrom(productionEnv(map[string]string{k: "true"}))); err == nil || !strings.Contains(err.Error(), "AUTH_DEV_SIMULATION") {
			t.Fatalf("production with %s: %v", k, err)
		}
	}
	cfg, err := Load(lookupFrom(productionEnv(nil)))
	if err != nil || cfg.Auth.DevSimulation {
		t.Fatalf("production simulation default: %v %v", cfg.Auth.DevSimulation, err)
	}
	dev, _ := Load(lookupFrom(map[string]string{"APP_ENV": "development", "DATABASE_URL": "postgres://u:p@localhost:5434/e"}))
	if !dev.Auth.DevSimulation {
		t.Fatal("development should simulate OTP delivery")
	}
}

func TestSecretsFromFiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jwt")
	secret := strings.Repeat("k", 40)
	if err := os.WriteFile(path, []byte(secret+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	m := productionEnv(map[string]string{"JWT_SECRET_FILE": path})
	delete(m, "JWT_SECRET")
	cfg, err := Load(lookupFrom(m))
	if err != nil || cfg.Auth.JWTSecret != secret {
		t.Fatalf("secret file: %v", err)
	}
	m["JWT_SECRET_FILE"] = filepath.Join(t.TempDir(), "missing")
	if _, err := Load(lookupFrom(m)); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("missing secret file: %v", err)
	}
}
