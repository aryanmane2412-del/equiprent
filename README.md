# EQUIPRENT

**Rent Smart. Build Better.** A heavy-equipment rental marketplace for India, connecting contractors, construction companies and individuals with equipment owners, operators and fleets.

> **Status: core backend built.** The Go API implements 19 modules (auth, users, owners, operators, equipment, search, availability, bookings, payments, fleet, tracking, maintenance, notifications, chat, invoices, agreements, reviews, disputes, admin) with a background worker and generated OpenAPI docs. See [docs/BACKEND.md](docs/BACKEND.md). The Flutter app runs the customer and owner journeys (discovery, listings, booking, payment, tracking, invoices, chat, reviews) on the real API; the operator, fleet-manager and most admin screens still use mock data. Roadmap: [docs/DEVELOPMENT_ROADMAP.md](docs/DEVELOPMENT_ROADMAP.md).

## Repository layout

```
.
├── backend/                    Go API + worker (Gin, pgx, sqlc, golang-migrate)
│   ├── api/openapi.json        generated OpenAPI 3.1 contract (checked by a test)
│   ├── cmd/api/                REST API entrypoint (`api openapi` prints the spec)
│   ├── cmd/worker/             background worker entrypoint
│   ├── internal/               platform: config, logger, apperror, httpx, httpserver, apidoc,
│   │                           database (pool, tx, error mapping), cache, ratelimit, idempotency,
│   │                           events (outbox), worker, app (wiring), db (sqlc output)
│   ├── internal/modules/       19 feature modules (handler → service → repository → model)
│   ├── migrations/             golang-migrate SQL (NNNNNN_name.up/down.sql)
│   ├── sql/queries/            sqlc query files
│   ├── tests/                  integration tests (real PostgreSQL)
│   └── sqlc.yaml
├── frontend/equiprent_app/     Flutter app: Android, iOS and Web
│   └── lib/
│       ├── app/                MaterialApp + GoRouter (role guards)
│       ├── core/               config, network (Dio), responsive, theme, widgets, session, routing
│       └── features/           customer / owner / operator / fleet_manager / admin / onboarding
├── infrastructure/
│   ├── docker/                 Dockerfiles, postgres init scripts
│   ├── deployment/             deployment notes (staging / production)
│   └── monitoring/             observability notes
├── scripts/                    PowerShell dev scripts
├── docs/                       product, architecture, DB, API, flows, security, roadmap
├── docker-compose.yml          local stack (PostGIS, Redis, API, worker, web)
└── .env.example                configuration template
```

## Prerequisites

| Tool | Version | Notes |
|---|---|---|
| Go | 1.26+ | |
| Flutter | 3.44+ (Dart 3.12) | `C:\flutter\bin` |
| Git | any | Flutter needs it on PATH |
| sqlc, golang-migrate | 1.31 / 4.20 | installed by `scripts\setup.ps1` |
| Docker Desktop | with WSL 2 | for PostGIS / Redis / containers |
| PostgreSQL 18 (native) | optional | for `scripts\db.ps1` when Docker is unavailable; `db.ps1 postgis` adds PostGIS |
| JDK 17 + Android cmdline-tools | | Android builds |
| macOS + Xcode (or Codemagic) | | iOS builds |

Run `.\scripts\check-env.ps1` to see what is installed and how to fix gaps. The scripts add Flutter, Git, Go tools and PostgreSQL to `PATH` themselves.

## Quick start (Windows PowerShell, from the repo root)

```powershell
# 1. One-time setup: creates .env, installs sqlc + migrate, fetches Go and Flutter packages
.\scripts\setup.ps1

# 2. Start a database: pick ONE
docker compose up -d          # PostGIS 18 on localhost:5434 (needs Docker engine / WSL 2)
.\scripts\db.ps1 postgis      # native, once: private PostgreSQL 18 + PostGIS runtime (no admin)
.\scripts\db.ps1 start        #   then: cluster on localhost:5434

# 3. Apply migrations
.\scripts\migrate.ps1 up

# 4. Run the API (terminal 1): http://localhost:8080
.\scripts\run-api.ps1

# 5. Run the app (terminal 2): Flutter web on http://localhost:5000
.\scripts\run-app.ps1
#    Android emulator:          .\scripts\run-app.ps1 -Device android
#    Physical Android phone:    .\scripts\run-app.ps1 -Device android -ApiBaseUrl http://<your-PC-LAN-IP>:8080

# Optional: background worker (terminal 3)
.\scripts\run-api.ps1 -Worker
```

Check it works:

```powershell
curl http://localhost:8080/healthz          # {"status":"up"}
curl http://localhost:8080/readyz           # postgres up, redis disabled
curl http://localhost:8080/api/v1/status    # service + API version
start http://localhost:8080/docs            # Swagger UI for every endpoint
```

Or open the Flutter app (`.\scripts\run-app.ps1`): it starts on the sign-in screen. Register as a customer or equipment owner; in development the SMS code and email token are shown on screen (no SMS/email is sent). Sign in from the terminal (the OTP comes back as `dev_code`):

```powershell
$api = 'http://localhost:8080/api/v1'
$otp = Invoke-RestMethod "$api/auth/otp/request" -Method Post -ContentType 'application/json' -Body '{"phone":"+919876543210"}'
$tok = Invoke-RestMethod "$api/auth/otp/verify" -Method Post -ContentType 'application/json' -Body (@{phone='+919876543210'; code=$otp.dev_code} | ConvertTo-Json)
Invoke-RestMethod "$api/me" -Headers @{ Authorization = "Bearer $($tok.access_token)" }
```

[docs/BACKEND.md](docs/BACKEND.md) walks through a whole rental (listing → booking → payment → operator → invoice).

The app signs in against the API (password or mobile OTP), then shows only the roles the server granted. The role screens themselves still run on **realistic mock data**. The full design system is at `/design-system` (link on the role picker) and in [docs/DESIGN_SYSTEM.md](docs/DESIGN_SYSTEM.md). Run the API first: the app needs it to sign in.

## Everyday commands

| Task | Command |
|---|---|
| All checks (go vet, sqlc vet, go test, flutter analyze, flutter test) | `.\scripts\test.ps1` |
| Same, plus integration tests against `TEST_DATABASE_URL` | `.\scripts\test.ps1 -Integration` |
| New migration | `.\scripts\migrate.ps1 create add_users` |
| Migrate up / down one / version | `.\scripts\migrate.ps1 up` · `down 1` · `version` |
| Regenerate sqlc code | `.\scripts\sqlc.ps1` |
| Stop / reset native DB | `.\scripts\db.ps1 stop` · `reset` |
| Redis (optional) | `docker compose --profile redis up -d`, then set `REDIS_URL=redis://localhost:6379/0` in `.env` |
| Full stack in containers | `docker compose --profile app up -d --build` |
| Web build in a container | `docker compose --profile web up -d --build`, then open http://localhost:8081 |
| Flutter web release build | `cd frontend\equiprent_app; flutter build web --no-web-resources-cdn --dart-define=API_BASE_URL=https://api.example` |

## Backend conventions

- **Versioned REST:** business endpoints live under `/api/v1` and responses carry `API-Version: v1`. `/healthz` (liveness) and `/readyz` (readiness) are unversioned, for infrastructure.
- **Modules:** `internal/modules/<name>`: handler (HTTP + docs) → service (rules) → repository interface (SQL via sqlc) → models. Wired in `internal/app`. Routes registered through `httpx.Router` are added to the OpenAPI spec automatically.
- **Errors:** handlers return `apperror` values (`httpx.Fail` / `httpx.Handle`). Repositories map PostgreSQL errors (`database.MapError`), e.g. the double-booking exclusion constraint becomes `409 BOOKING_OVERLAP`. Responses are RFC 9457 `application/problem+json` with `code` and `request_id`. Internal causes are logged, never returned.
- **Auth:** phone OTP → 15-minute JWT access token + rotating refresh token (reuse revokes the session family). Multi-organisation users send `X-Org-Id`.
- **Safety:** `Idempotency-Key` is required on booking and payment POSTs (responses replayed); per-IP rate limits; multi-step writes use `database.Transactor` with the actor recorded for audit triggers.
- **Logging:** `log/slog`. Text output in development, JSON elsewhere. One line per request with the request ID.
- **Config:** environment variables only (see `.env.example`). `.env` is read automatically outside production. Invalid config fails fast, with every problem listed together.
- **Database:** write SQL in `backend/sql/queries/*.sql`, run `scripts\sqlc.ps1`, and use `db.New(pool)`. Schema changes go only through migrations.
- **Redis is optional:** when `REDIS_URL` is empty, readiness reports it as `disabled`.

## Frontend conventions

- **State:** Riverpod (`flutter_riverpod` 3). **Routing:** GoRouter. Each role has a `RoleModule` under `lib/features/<role>/`, mounted in a `ShellRoute` with an adaptive shell: bottom nav on phones, a collapsed rail on tablets, a full sidebar on desktop.
- **Design system:** tokens in `lib/core/theme/`, components in `lib/core/widgets/` (import `widgets.dart`). Inter and Lucide icons are bundled. See [docs/DESIGN_SYSTEM.md](docs/DESIGN_SYSTEM.md).
- **Mock data:** `lib/core/mock/` (models, data, Riverpod notifiers). Screens read and write these notifiers. Phase 1 replaces each one with an API repository.
- **HTTP (ready, not used yet):** `apiClientProvider` wraps the shared Dio with request IDs and logging. Failures surface as `ApiException`.
- **Push (FCM, optional):** `--dart-define=FIREBASE_API_KEY=... --dart-define=FIREBASE_APP_ID=... --dart-define=FIREBASE_MESSAGING_SENDER_ID=... --dart-define=FIREBASE_PROJECT_ID=...` (plus `FIREBASE_VAPID_KEY` and `web/firebase-config.js` for web). Without them push is off and notifications still show in the app. See docs/BACKEND.md §3d.
- **Config:** `--dart-define=API_BASE_URL=... --dart-define=APP_ENV=staging|production`. Development defaults to `localhost:8080` (`10.0.2.2` on the Android emulator).
- **Responsive:** `context.windowSize`, `PageBody`, `ResponsiveGrid`, `ResponsiveBuilder` in `lib/core/responsive`. Shared widgets live in `lib/core/widgets`.
- On this machine, run Flutter web with `--no-web-resources-cdn` (the scripts do this), or CanvasKit loads very slowly.

## Known environment gaps (this machine, 2026-11-12)

- **Docker engine cannot start: WSL is not installed.** Run `wsl --install` as admin, reboot, then start Docker Desktop. Until then use `scripts\db.ps1 postgis` + `db.ps1 start`. They build a private PostgreSQL + PostGIS 3.7 runtime in `%LOCALAPPDATA%\equiprent\pgsql`, which all 11 migrations need.
- **Android builds are blocked:** `JAVA_HOME` points to a missing JDK and the Android cmdline-tools are missing. See `scripts\check-env.ps1` for the fixes.
- **iOS** builds need macOS (Codemagic or a Mac).
- **Port 5433** is used by another local PostgreSQL cluster, which is why EQUIPRENT uses **5434**.
- The repo is inside **OneDrive**. Moving it to e.g. `C:\dev\equiprent` avoids sync locks on build folders.

## Documentation

[Backend guide](docs/BACKEND.md) · [Project plan](docs/PROJECT_PLAN.md) · [Architecture](docs/ARCHITECTURE.md) · [Database](docs/DATABASE_DESIGN.md) · [API](docs/API_DESIGN.md) · [User flows](docs/USER_FLOWS.md) · [Security](docs/SECURITY.md) · [Roadmap](docs/DEVELOPMENT_ROADMAP.md)
