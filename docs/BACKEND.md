# EQUIPRENT — Backend Guide

Status: v0.3 · 2026-11-14. Go 1.26 · Gin 1.12 · pgx 5 + sqlc · PostgreSQL 18 + PostGIS 3.7 · optional Redis.

This guide covers how the API is put together, how to run it locally, and how to work on it. The endpoint contract is [`backend/api/openapi.json`](../backend/api/openapi.json): it is generated from the code, and a test fails if it goes stale. Browse it at <http://localhost:8080/docs> while the API runs.

## 1. Run it locally

From the repo root in PowerShell. The scripts add Go, the Go tools and PostgreSQL to `PATH` themselves.

```powershell
.\scripts\setup.ps1             # once: .env, sqlc + migrate, Go modules
.\scripts\db.ps1 postgis        # once (native DB only): private PostgreSQL 18 + PostGIS runtime
.\scripts\db.ps1 start          # PostgreSQL on localhost:5434 (or: docker compose up -d)
.\scripts\migrate.ps1 up        # 11 migrations (schema + reference data + seed models)
.\scripts\run-api.ps1           # API on http://localhost:8080 (Swagger UI at /docs)
.\scripts\run-api.ps1 -Worker   # background worker (second terminal)
```

| Check | Expect |
|---|---|
| `GET /healthz` | `{"status":"up"}`: the process is alive (never touches dependencies) |
| `GET /readyz` | `postgres: up`, `redis: disabled` (or `up`); 503 when a dependency is down |
| `GET /api/v1/status` | service, API version, build version, environment |
| `GET /docs` | Swagger UI; `GET /api/v1/openapi.json` is the raw spec |

**Without PostGIS** (plain `db.ps1 start` on the stock installation), migration 2 stops with `extension "postgis" is not available`. Run `db.ps1 postgis` once. It copies `C:\Program Files\PostgreSQL\18` to `%LOCALAPPDATA%\equiprent\pgsql`, adds the OSGeo PostGIS bundle on top, and needs no admin rights. Every script then uses that runtime first.

**Development conveniences** (all refused in production by config validation):

- `OTP_DEV_ECHO`: the OTP is returned as `dev_code` instead of being sent by SMS.
- Dev-only JWT/OTP/webhook secrets are built in, so `.env` needs no secrets.
- **Payment sandbox** (no `PAYMENT_KEY_ID`/`PAYMENT_KEY_SECRET`): orders are simulated, and the create-payment response includes a `sandbox` object you post to `/payments/{id}/verify` to simulate a successful payment.
- In-memory key-value store when `REDIS_URL` is empty: rate limits and OTP throttles are then per process.

### Walkthrough: one complete rental

Every call below is `$api = 'http://localhost:8080/api/v1'` plus a bearer token from the OTP login shown in the README. `X-Org-Id` selects the organisation when a user belongs to more than one.

| # | Who | Call |
|---|---|---|
| 1 | owner | `POST /orgs` (business, GSTIN) → `POST /owners/onboard` with `X-Org-Id` |
| 2 | owner | `GET /models?category=excavator` → `POST /owner/equipment` (draft) |
| 3 | owner | `PUT /owner/equipment/{id}/pricing` → `POST /owner/equipment/{id}/submit` |
| 4 | admin | `POST /admin/listings/{id}/approve` (grant yourself `admin_super` once: `INSERT INTO user_roles (user_id, role_code) VALUES ('<user id>', 'admin_super');`) |
| 5 | customer | `POST /orgs/{org}/addresses` with `is_site: true` and a `location` |
| 6 | customer | `GET /search/equipment?category=excavator&lat=..&lng=..&start=..&end=..` → `POST /equipment/{id}/estimate` |
| 7 | customer | `POST /bookings` with an `Idempotency-Key` header → `requested` |
| 8 | owner | `POST /owner/bookings/{id}/accept` → `payment_pending` (online) or `confirmed` (offline) |
| 9 | both | `GET /bookings/{id}/agreement` → `POST .../agreement/sign` with the `terms_sha256` you read, your typed name and `consent: true` (§3d) |
| 10 | customer | `POST /bookings/{id}/payments` `{"purpose":"advance"}` → `POST /payments/{pid}/verify` → booking `confirmed` |
| 11 | owner | `POST /orgs/{org}/members` `{"phone":..,"role":"operator"}` → `POST /owner/bookings/{id}/assign-operator` |
| 12 | operator | `POST /operator/jobs/{id}/accept` → `.../dispatch` → `.../tracking/batch` (entering the site geofence sets `on_site`) |
| 13 | customer | `POST /bookings/{id}/handover/confirm` (`active`) → `.../return-request` |
| 14 | operator / owner | `POST /operator/jobs/{id}/returned` → `POST /owner/bookings/{id}/complete` |
| 15 | owner | `POST /owner/bookings/{id}/invoices` (GST invoice, CGST+SGST or IGST) |
| 16 | customer | `POST /bookings/{id}/reviews`; `POST /bookings/{id}/disputes` if needed |

`backend/tests/flow_test.go` runs exactly this sequence against the real database.

## 2. Architecture

```
HTTP ─► middleware chain ─► httpx.Router (auth / admin / idempotency guards, OpenAPI)
        RequestID → Recovery → Logging → CORS → BodyLimit → Errors → [v1: API-Version, rate limit]
                                     │
               internal/modules/<name>: handler ─► service ─► Repository (interface)
                                                     │              │
                                     database.Transactor        sqlc (internal/db) ─► PostgreSQL/PostGIS
                                     events.Emit (outbox) ──────────┘
cmd/worker ─► worker.Scheduler (advisory-locked jobs) ─► outbox → notifications, expiries, scans
```

- **Handlers** bind and validate input and pick the acting organisation. They do no business logic.
- **Services** hold the rules: who may do what, state machines, pricing, money. They depend on a `Repository` interface and a `database.Transactor`, so the unit tests use in-memory fakes.
- **Repositories** wrap sqlc-generated queries (`backend/sql/queries/*.sql` → `internal/db`), map rows to domain models, and translate PostgreSQL errors with `database.MapError`.
- **The database is the last line of defence.** Booking transitions, double-booking (an exclusion constraint), immutable invoices and payments, append-only history, and audit triggers are all enforced in SQL ([DATABASE_DESIGN.md](DATABASE_DESIGN.md)). The services give friendly errors first.
- `internal/app` is the only place that wires concrete types together.

### Cross-cutting pieces

| Concern | Where | Notes |
|---|---|---|
| Config | `internal/config` | Env vars, `.env` outside production. All problems are reported at once. Production refuses dev secrets, OTP echo, localhost CORS and a payment sandbox |
| Errors | `internal/apperror`, `internal/httpx/respond.go` | RFC 9457 problem+json with a stable `code`, field `errors` and `request_id`. `Retry-After` on 429 |
| Validation | `internal/httpx/bind.go` | go-playground tags. Field errors use JSON names. Custom tags: `e164`, `objkey`, `statecode`, `pincode` |
| Auth | `modules/auth`, `internal/password` | Registration, password login (Argon2id), mobile OTP, email verification, password reset/change, 15-minute HS256 JWT, rotating refresh tokens with reuse detection, lockout, audit. See section 3a |
| Acting organisation | `auth.Middleware`, `httpx.ActingOrg` | `X-Org-Id` must be an organisation you belong to. It is optional when you belong to exactly one |
| Roles | `httpx.Principal` | Organisation roles (customer, owner, fleet_manager, operator, org_admin) and platform roles (admin_*). `org_admin` implies every organisation role and `admin_super` every platform role |
| Idempotency | `internal/idempotency` | Required on `POST /bookings` and `POST /bookings/{id}/payments`. Replays carry `Idempotency-Replayed: true`. The same key with a different body gives 422; while the first is still running, 409 |
| Rate limits | `internal/ratelimit` | Fixed window in the cache store: 300/min per IP on `/api/v1`, 20/min on public auth endpoints. Sends `RateLimit-*` headers. Fails open if Redis is down |
| Redis | `internal/cache` | `Store` interface: Redis implementation, in-memory fallback |
| Transactions | `internal/database/tx.go` | `InTx`/`InTxAs` carry the transaction in the context and repositories pick it up. Serialization failures retry. `InTxAs` sets `app.actor_*` for the audit and booking-history triggers |
| Outbox | `internal/events` | Events are written in the same transaction as the change and delivered by the worker |
| Logging | `log/slog` | One line per request (request ID, route, status, latency). 5xx causes are logged with the request ID |
| OpenAPI | `internal/apidoc` | Built by reflection from request/response types and binding tags. `api openapi` prints it |
| Shutdown | `httpserver.Run`, `worker.Scheduler` | SIGINT/SIGTERM: stop accepting, finish in-flight requests and jobs (`SHUTDOWN_TIMEOUT`) |

## 3. Modules

211 operations. The OpenAPI tag is in brackets.

| Module | Endpoints (prefix `/api/v1`) | Highlights |
|---|---|---|
| auth [auth] | `GET /auth/capabilities`; `POST /auth/register`, `/auth/login`, `/auth/otp/request`, `/auth/otp/verify`, `/auth/refresh`, `/auth/password/forgot`, `/auth/password/reset`, `/auth/email/verify`; signed in: `/auth/logout`, `GET/DELETE /auth/sessions`, `/auth/sessions/revoke-others`, `/auth/password/change`, `/auth/email/verification` | See section 3a |
| users [users] | `GET/PATCH /me`, `GET /me/orgs`, `POST /orgs`, `GET/PATCH /orgs/{org_id}`, members, addresses | GSTIN validation derives the state code. Adding an operator by phone creates their profile |
| owners [owners] | `POST /owners/onboard`, `GET /owners/me/dashboard`, `GET /owners/{org_id}` | Owner role, owner profile and supplier flag in one transaction |
| operators [operators, operator-jobs] | `GET/PUT /operators/me/profile`, `POST /operator/availability`, `GET /operator/jobs`, job actions | Only the last 4 characters of the licence number are stored |
| equipment [catalogue, equipment, owner-equipment] | `GET /categories` (localised), `/makes`, `/models`, `/cities`; `GET /equipment/{id}`; owner CRUD, submit/pause/resume, pricing, specs, photos | Optimistic locking (`version` / `If-Match` → 412). Lifecycle: draft → pending_review → active ⇄ paused |
| search [search] | `GET /search/equipment` | PostGIS distance, the listing's service radius, free for the whole window; sort by distance, price or rating |
| availability [availability] | `GET /equipment/{id}/availability`, owner calendar, `POST/DELETE` blocks | Overlaps with held bookings are refused by the database |
| bookings [bookings, owner-bookings] | estimate, `POST/GET /bookings`, detail with timeline, cancel (`?dry_run`), owner inbox, accept/reject/assign/dispatch/mark-returned/complete | Price snapshot, reference `ER-YYMM-XXXX`, response deadline, instant book, policy-based cancellation fee |
| payments [payments] | `POST/GET /bookings/{id}/payments`, `POST /payments/{id}/verify`, `POST /webhooks/payments/razorpay` | HMAC checkout and webhook verification. Webhooks are stored once (deduplicated by event ID). The advance confirms the booking |
| fleet [fleet] | `/fleet/{org_id}/overview`, `/operators`, `/live`, `/reports/utilisation` | KPIs, roster, last positions, booked hours per machine |
| tracking [tracking] | `POST /operator/jobs/{id}/tracking/batch`, trips start/end, `GET /bookings/{id}/track` | Offline-safe batches (duplicates ignored). The geofence moves dispatched → on_site. Live position is cached |
| maintenance [maintenance] | plans and records under `/owner/equipment/{id}/maintenance/*`, `GET /owner/maintenance/due`, `/records`, `/costs` | Service, repair, inspection and breakdown records with parts/labour costs and invoices; downtime blocks the calendar; cost reports; reminders. See §3d |
| notifications [notifications] | `GET /notifications`, unread count, read / read-all, `/me/devices`, `/me/notification-preferences` | Filled by the worker from outbox events; push through Firebase Cloud Messaging (HTTP v1) when configured. See §3d |
| chat [chat] | `GET /conversations`, unread count, `POST /bookings/{id}/conversation`, `POST /equipment/{id}/enquiry`, messages (history and polling), delete | Booking chats and listing enquiries; photo attachments; access re-checked on every request. See §3d |
| invoices [invoices] | `POST /owner/bookings/{id}/invoices`, `GET /bookings/{id}/invoices`, `GET /invoices`, `GET /invoices/{id}` | Gap-free `ER/2627/00001` numbering per supplier and financial year; CGST+SGST or IGST by place of supply |
| agreements [agreements] | `GET /bookings/{id}/agreement`, `POST .../sign`, `POST .../accept`, `GET .../document`, `POST /webhooks/esign/{provider}` | Parties, equipment, dates, price and terms; e-signatures bound to the SHA-256 of the text; PDFs in private storage. See §3d |
| reviews [reviews] | `POST/GET /bookings/{id}/reviews`, `GET /equipment/{id}/reviews`, `GET /owners/{org_id}/reviews`, `GET /owner/reviews`, reply, report, `/admin/reviews` moderation | Ratings with summaries; screening, reports and audited moderation; owner replies. See §3d |
| disputes [disputes, admin] | `POST /bookings/{id}/disputes`, `GET /disputes`, admin queue / assign / resolve | Opens a dispute conversation. The booking becomes `disputed`; resolving it returns to `completed` |
| admin [admin] | stats, users (suspend / reactivate / grant role), listing moderation, forced booking transitions, catalogue models, audit log | Every write needs a reason and is written to `audit_logs` with the request ID |

### Background jobs (`cmd/worker`)

| Job | Every | Does |
|---|---|---|
| `outbox.dispatch` | 5 s | Outbox events → in-app notifications and FCM pushes for the right members of both organisations (chat messages: push only) |
| `bookings.expire_requests` | 1 min | Requests past `owner_response_due_at` → `expired` (+ event) |
| `worklogs.auto_approve` | 15 min | Daily hour logs not acted on within 48 h → `auto_approved` |
| `cleanup.expired_keys` | 1 h | Purges expired idempotency keys and OTP challenges older than 7 days |
| `tracking.ensure_partitions` | 24 h | Keeps `location_pings` monthly partitions 6 months ahead |
| `documents.expiry_scan` | 24 h | Warns owners about RC/insurance/fitness documents expiring within 30 days |
| `maintenance.due_scan` | 6 h | Notifies owners about plans that have fallen due (at most once a week per plan) |
| `maintenance.reminders` | 1 h | Announces scheduled jobs within 3 days (once a day each) and scheduled jobs whose date passed without starting (once) |
| `heartbeat` | `WORKER_HEARTBEAT_INTERVAL` | Logs dependency health and pool stats |

Each run executes inside one transaction holding a `pg_try_advisory_xact_lock`, so with several worker replicas a job runs only once at a time. Panics are recovered and every run has a timeout.

## 3a. Authentication and authorisation

**Roles.** The five app roles are derived on the server from stored role assignments and returned as `app_roles`:

| App role | Stored role | How a user gets it |
|---|---|---|
| CUSTOMER | `customer` (or `org_admin`) | Registration as `customer`, or first mobile-OTP sign-in |
| EQUIPMENT_OWNER | `owner` | Registration as `equipment_owner`, or `POST /owners/onboard` by an org admin |
| OPERATOR | `operator` | Invited by an org admin or fleet manager (`POST /orgs/{org}/members`) |
| FLEET_MANAGER | `fleet_manager` | Invited by an org admin |
| ADMIN | `admin_*` (platform) | Granted by a super admin (`POST /admin/users/{id}/roles`) |

Roles are never accepted from the client. Registration only takes `account_type` = `customer` or `equipment_owner`; extra fields like `roles` are ignored. `X-Role`-style headers are ignored. Access tokens carry no roles, so every request reloads them from the database. `org_admin` manages its organisation and may buy, but does **not** imply owner, fleet-manager or operator powers. Only `admin_super` acts on other admins.

**Sign-in.**
- **Password**: email or phone (10-digit Indian numbers get +91). An identifier can be used only after it is verified: the phone by OTP, the email by its token.
- **Mobile OTP**: proves the phone and signs in. A new number gets a customer account.

**Tokens.** 15-minute HS256 access tokens. Refresh tokens rotate on every use, and only their SHA-256 is stored. Reusing a rotated token revokes the whole session family. Each request checks that the session is live and the account active.

**Defences:**
- **Passwords:** Argon2id (19 MiB, t=2), with transparent rehash if the parameters rise. Policy: 10–128 characters, mixing letters with digits or symbols, not a common password, not containing the name, email or phone.
- **Lockout:** 10 password attempts per identifier per 15 minutes, counted for unknown identifiers too. On top of that, auth endpoints allow 20 requests per minute per IP.
- **OTP limits:** 5 attempts per code, 30 s resend cooldown, 5 codes per hour.
- **Account enumeration:** register, login, OTP request and forgot-password answer identically for existing and unknown accounts. Unknown accounts do the same hashing work as real ones. The owner of an already-registered phone or email gets a notice instead.
- **Password reset and change:** a reset signs out every device; a change signs out the other devices.
- **Email changes:** changing the email clears its verification (database trigger).
- **Suspension:** revokes all sessions, and sign-in then returns `403 ACCOUNT_SUSPENDED`.
- **Audit:** every auth event (`auth.register`, `auth.login`, `auth.login_failed`, `auth.lockout`, `auth.refresh_reuse`, `auth.password_*`, `auth.email_*`, `auth.session(s)_revoked`, `auth.logout`) goes to `audit_logs` with IP and request ID.

**Delivery and development simulation.** `AUTH_DEV_SIMULATION` (default on in development and test, refused in staging and production) returns OTPs and email tokens in responses as `dev_code`, `dev_otp` and `dev_token` instead of sending them. No SMS or email provider is integrated yet: `SMS_PROVIDER`/`EMAIL_PROVIDER` must stay empty, and without simulation, OTP and email endpoints answer `503` while password sign-in keeps working. `GET /auth/capabilities` tells clients what is available. Production OTP therefore cannot be switched on until a real provider (with credentials) is implemented behind `auth.SMSSender`.

**Flutter.** The app now signs in for real, with five screens:
- sign-in (password or OTP), registration (two self-service account types), verification, forgot/reset password, and email verification.
- **Account & security**: profile, verification status, password, devices, sign out.

The refresh token lives in secure storage (Keychain/Keystore). The access token is kept only in memory and refreshed once on a 401, with concurrent requests sharing the refresh. The router opens a role area only when the server granted that role. Mock data still backs the role screens.

## 3b. User verification

Each role has its own verification:

| Role | What is collected | Who decides |
|---|---|---|
| Customer | full name, verified mobile and email, an address, a profile image | checklist only (`GET /verification/customer`) |
| Equipment owner | individual or company account, legal and business name, PAN (+ GSTIN, CIN, Udyam), contact details, business address, identity and business documents, payout bank account | KYC admin |
| Operator | licence number/class/expiry, experience, machine skills, availability, qualifications, licence + identity documents | KYC admin |
| Fleet manager | company profile and the machines an owner authorised (`GET /verification/fleet-manager`) | the owner (`PUT /verification/owner/fleet-managers/:user_id`) |

**Workflow.** Owner and operator applications go `draft → submitted → under_review → approved | rejected`. Submission is refused until every checklist item is done. While an application is submitted or approved, its details are locked (`409`); this includes the older `PUT /operators/me/profile`. A rejection or revocation needs a reason, which the applicant sees; they fix things and resubmit. Admins (`admin_kyc`, or `admin_super`) work from `GET /admin/verifications`: they open an application, accept or reject single documents, approve, reject or revoke. They cannot decide on their own organisation or on themselves. Every step is written to `verification_events`, which is append-only (updates and deletes are refused by a trigger), and also to `audit_logs`. Approval sets `organizations.kyc_status = verified` (owners) or `operator_profiles.verified_at` (operators) and emits `verification.approved` / `verification.rejected` notifications. KYC admins may also suspend the applicant's account (`POST /admin/users/:id/suspend`).

**What verification unlocks.** Listings can only be submitted for review once the owner organisation is verified (`422 KYC_REQUIRED`). Only verified operators can be assigned to bookings.

**Fleet manager scope.** Fleet managers reach only the machines an owner granted them (or every machine, with "all equipment"), plus the machines they create themselves. The router attaches the equipment guard (`access.Guard`) to every authenticated route under `/owner/equipment/:id`, whichever module serves it. A machine outside that scope answers `404`, so its existence is not disclosed. `GET /owner/equipment` and the maintenance due-list are filtered the same way.

**Documents and files.**
- **Upload:** `POST /uploads` is multipart, with `purpose` set to `identity_document`, `business_document`, `operator_document` or `avatar`. Files are checked by their content, not by extension or the client's content type. PDFs must be complete and unencrypted, with no JavaScript, launch actions or embedded files. Images are PDF, JPEG, PNG or WebP within pixel limits; avatars are re-encoded, which strips EXIF data. Uploads are limited to 10 MiB and 40 per hour per user.
- **Storage:** files go to the private store (`STORAGE_DIR`, default `backend/var/objects`, gitignored). No route serves that directory.
- **Ownership:** an upload can be attached only by the user who uploaded it, and only for its purpose.
- **Download:** the only way to read a file is `GET /files?key&exp&sig`. That link is HMAC-signed, valid for at most 15 minutes (5 by default) and bound to one object. It is issued by `GET /verification/documents/:id/url` only to the owning organisation's owners, the operator themselves or a KYC admin. Every issued link is written to `audit_logs` as `document.url_issued`. Responses are `no-store`, `nosniff`, sandboxed by CSP and sent as attachments. API responses never contain storage keys or permanent URLs.

**Sensitive numbers.** PAN, licence, document and bank account numbers are encrypted with AES-256-GCM (`FIELD_ENCRYPTION_KEY`). The field name is bound as associated data. A keyed HMAC (`LOOKUP_HASH_KEY`) catches duplicates, for example one licence registered to two operators. Only the last 4 characters are ever returned.

**Payout accounts** are verified through the payment provider before they are saved; payouts go to the provider's fund-account reference. While the gateway is in sandbox mode (no `PAYMENT_KEY_*`), `SandboxPayouts` simulates this: account numbers ending in `0000` fail. With live keys, the provider's account-validation API is not integrated yet, so the endpoint answers `503` instead of pretending (see section 6).

**Flutter.** Each role has its own screens, all responsive from phone to desktop:
- **Customer:** profile and verification (Profile → "Profile & verification").
- **Owner:** business verification and fleet manager access (Profile → links).
- **Operator:** a Verification tab.
- **Fleet manager:** company and access (building icon on the Fleet overview).
- **Admin:** a Verifications tab with the review screen.

Documents open through freshly issued signed links.

## 3c. Equipment discovery

Customers search approved equipment with `GET /discover/equipment` (module `internal/modules/discovery`). The response contains **active listings of active categories only**.

**What can be searched**
- **Text (`q`):** machine name, manufacturer, model, category (English and Hindi names), the owner's area label and the city. Matching is PostgreSQL full-text search over `equipment.search_document`, a weighted `tsvector` with the `simple` configuration so model codes such as 3DX or PC210 are not stemmed. Words are prefix-matched (`komat` finds Komatsu) and results are ranked by `ts_rank_cd`.
  - **Typos:** when nothing matches exactly, the search is retried with trigram word-similarity (pg_trgm `<%`, threshold 0.5) on titles, makes and models, and the response says `fuzzy: true`. "excavtor" finds excavators.
  - **Hindi:** Hindi terms need a database created with a UTF-8 locale. The local dev cluster uses the `C` locale, where PostgreSQL drops Devanagari characters from text search.
- **Category, manufacturer, model:** `category` takes one or more slugs, comma-separated; `make` and `model` are separate filters.
- **Location:**
  - `lat`/`lng` searches near a point (the device location), with a default radius of 50 km.
  - `pincode` searches near a PIN code, with a default radius of 25 km. PIN codes come from the `pincodes` reference table; with `GOOGLE_MAPS_API_KEY` set, unknown PIN codes are geocoded through Google.
  - `city` returns machines based in that city and orders them by distance from the city centre.
  - `radius_km` changes the radius.
- **Map:** `min_lat`, `min_lng`, `max_lat`, `max_lng` return what is inside the visible map area. Pages can hold up to 300 results here.

**Filters:** rental price (`price_min`/`price_max` in paise, per `unit`, default day), distance, category, rating (`rating_min`), availability (`available_from`/`available_to`; a date-only end includes that day; bookings that hold the slot and owner blocks both count), condition, delivery (`delivery`), operator (`operator=with|without`) and verified owners (`verified_only`).

**Sort:** `relevance` (default with text), `distance` (default with a location), `newest` (default otherwise), `price_asc`, `price_desc`, `rating`. Results come in numbered pages (`page`, `page_size` ≤ 50) with `total` and `pages`.

**Privacy.** Customers never see where a machine is parked:
- **Public point:** every listing has a generated `public_location`, the centre of the ~1.1 km grid cell holding the yard (`equipment_public_point()`, mirrored by `equipment.PublicPoint` in Go).
- **Where it is used:** search, map pins, distances, the public listing detail and comparison all use that point. The older availability search also reports distances from it now.
- **Owners:** only the owning organisation sees the exact `base_location`.
- **Tests:** they check that no response contains the exact coordinates, and that public points are 50–800 m from the yard.

**Query design.** The search SQL is built per request (`discovery/search_sql.go`) with only the filters actually present. A single static query full of `($x IS NULL OR …)` conditions stopped PostgreSQL using the GIN (full-text, trigram) and GiST (`public_location`) indexes. The query runs in two stages: the first finds the matching ids with only the columns needed to filter, sort and count; the second loads cover photos and prices for the page alone.

**Performance test.** `TestDiscoveryPerformance` loads 20,000 active listings spread across India. Measured medians on this machine:

| Search | Median |
|---|---|
| Geographic (point, PIN code, map area) | 5–8 ms |
| Text | 20–25 ms |
| Fuzzy fallback | about 90 ms |
| Unfiltered browse with a full count | about 50 ms |

The test fails above 250 ms and checks that the full-text and spatial indexes are used.

**Other endpoints**
- **Suggestions:** `GET /discover/suggest?q=` offers categories, makes and models with live listings, cities and PIN codes.
- **Location picker:** `GET /discover/locations?q=` lists cities and PIN codes.
- **Comparison:** `GET /discover/compare?ids=a,b[,c,d]` takes 2 to 4 listings and returns them with the union of their spec keys.
- **Map provider:** `GET /discover/maps` reports `google` or `builtin`.

**Signed-in customers**
- **Saved equipment:** `GET /me/saved-equipment` (paused or removed machines stay listed, marked `available: false`), `GET /me/saved-equipment/ids`, `PUT /me/saved-equipment/:id` and `DELETE /me/saved-equipment/:id`. Only active listings can be saved.
- **Search history:** searches with text, a category, make, model or location are recorded automatically. Browsing, paging and map panning are not. Repeating a search moves it to the top, and the newest 30 are kept (`GET /me/search-history`, `DELETE /me/search-history[/:id]`).

**Maps.**
- **Server:** `GOOGLE_MAPS_API_KEY` is a server key, used only for PIN-code geocoding.
- **App:** gets its own browser/app key with `--dart-define=GOOGLE_MAPS_API_KEY=...`. On web the Maps JavaScript API is loaded at runtime. Android reads the key from `android/local.properties` (`GOOGLE_MAPS_API_KEY=...`); iOS reads the `GOOGLE_MAPS_API_KEY` build setting, which reaches `Info.plist` as `GMSApiKey`.
- **No key:** the app draws a built-in map from the approximate points. It supports pan, zoom, clusters for overlapping pins, the search radius, city reference dots and "Search this area", so development needs no credentials.
- **Not verified:** the Google Maps path compiles and is wired up, but has not been run here because no key was available. The iOS configuration has not been built here either.

**Flutter** (`lib/features/discovery`):
- **Search page:** Customer → Search. It has suggestions and recent searches, a location chip (city, PIN code or "Use my location" through `geolocator`), a filter sheet with removable filter chips, a sort menu, numbered pages and a list/map toggle in the app bar.
- **Saving and comparing:** hearts save a machine (optimistic, rolled back on failure). The compare toggle adds a machine to a tray of up to 4 for `/customer/compare`. Saved machines are at `/customer/saved` and in Profile.

**Seed data.** `go run ./cmd/seed` adds owners in Hadapsar (Pune), Andheri (Mumbai), Nashik, Nagpur, Delhi NCR and Chennai alongside the earlier ones, for about 40 live machines across 10 cities with ratings on some.

## 3d. Communication and operations

Migration `000020_communication_ops`. Integration tests: `backend/tests/comms_test.go` (each module on its own, then `TestCommunicationAndOperationsTogether`).

### Notifications

**What is announced.** Every state change writes an outbox event in its own transaction. The worker turns it into notifications for the right people, never the person who acted:

| Event | Who hears | Category |
|---|---|---|
| `booking.requested` | owner side | bookings |
| `booking.accepted`, `booking.rejected` | customer | bookings |
| `payment.captured` | both, with different wording ("Payment confirmed" / "Payment received", amount in ₹) | payments |
| `booking.operator_assigned` | customer and the operator | bookings |
| `booking.active` (rental start), `booking.completed` | both | bookings |
| `booking.final_charges` (the owner settled the final bill) | customer | payments |
| `payout.paid`, `refund.processed` | owner / customer | payments (routed; emitted once payouts and refunds are built) |
| `agreement.signed`, `agreement.executed` | both | agreements |
| `chat.message` | the other participants, **push only** (the chat is the record) | chat |
| `maintenance.reminder`, `maintenance.overdue`, `maintenance.due`, `document.expiring` | owner side | maintenance |
| `review.received` | the reviewed party | reviews |

Notification `data` carries `booking_id`, `conversation_id`, `equipment_id` or `review_id`, so the app can open the right screen.

**Push (Firebase Cloud Messaging).**
- **Sending:** with `FCM_CREDENTIALS` (a service-account key file, or its JSON), `notifications.FCMPusher` signs an RS256 JWT and exchanges it for an OAuth token, cached until a minute before it expires. It then calls the HTTP v1 `messages:send` API for every registered device of the user.
- **Message options:** Android pushes use high priority, the `equiprent_default` channel and a collapse key (one stack per conversation). Web pushes open `APP_PUBLIC_URL`.
- **Failures:** tokens FCM reports as `UNREGISTERED` (or 404) are deleted. Other failures are recorded on the device (`last_error`) and logged.
- **Without credentials,** pushes are only logged.
- **Not verified live:** tested against a fake Google token endpoint and a fake FCM server. It has not been run against the real FCM service, because no Firebase project was available here.

**Preferences.** `GET/PUT /me/notification-preferences` switch push on or off per category: bookings, payments, agreements, chat, maintenance, reviews, account. In-app notifications are always kept. `GET /me/devices` lists the user's devices (never their tokens) and whether the server can push (`push: fcm|none`).

### Digital agreements

Moved to §3e (agreement versions, legal terms as reviewed configuration, acceptance records and the e-signature provider interface).

### Chat

- **Conversations** are of two kinds:
  - **booking chats** (`POST /bookings/{id}/conversation`) between the customer side, the owner side and the operator;
  - **listing enquiries** (`POST /equipment/{id}/enquiry`) between a customer organisation and the owner before any booking, optionally with a first message. There is one per customer organisation and machine.
- **Access** is checked on every request against the booking or the organisations, not just the participant list, so people who leave an organisation lose access. Outsiders get 404.
- **Messages:**
  - **Reading:** `GET .../messages?before=` pages through history, newest first. `?after=` returns new messages oldest first, for polling while a thread is open.
  - **Content:** text up to 4,000 characters and/or one photo. Upload the photo with purpose `chat_image` (it is re-encoded, which strips EXIF data, and scaled to at most 2,048 px), then send its `attachment_upload_id`. Only your own uploads can be attached. Messages carry a signed image link valid for an hour.
  - **Resends:** `client_id` makes resends idempotent per sender. It used to be globally unique, which let a resend return someone else's message.
  - **Removing:** senders can remove their own message within 15 minutes.
  - **Badge:** `GET /conversations/unread-count`.
- **Not built:** WebSockets. Clients poll every few seconds while a thread is open; otherwise push brings new messages.

### Maintenance

- **Records** have:
  - a kind (`service`, `repair`, `inspection`, `breakdown`) and a status (`scheduled`, `in_progress`, `done`, `cancelled`);
  - costs: total, parts and labour. The total defaults to parts + labour and may not be less than them;
  - vendor, notes, hour-meter reading, and an optional invoice: a `maintenance_document` upload, downloaded through `GET .../records/{id}/invoice`.

  Work done earlier can be logged with `performed_at`.
- **Availability.**
  - **Blocking:** a downtime window on a scheduled or in-progress job blocks the booking calendar with an availability block (reason `maintenance`), unless `block_calendar: false` is sent.
  - **Clashes:** a window over bookings that hold the slot is refused with `409 BOOKING_OVERLAP`, naming the bookings.
  - **Changes:** rescheduling moves the block, cancelling frees it, and completing early frees what is left (all of it if the work never started).
- **Plans** (every N hours and/or N days) can be edited or paused (`PATCH .../plans/{id}`). Completing a planned job resets its plan.
- **History and costs:**
  - `GET /owner/maintenance/records` filters by machine, kind and status.
  - `GET /owner/maintenance/costs?from&to` reports spend, jobs and downtime hours by machine, kind and month (IST). The default is the last 12 months.
  - Fleet managers see only their own machines.
- **Reminders** come from the `maintenance.reminders` and `maintenance.due_scan` jobs (section 3).

### Reviews

- **Ratings.** Customers rate the **machine**, the **owner's service** and the operator; owners rate the customer. Each review has 1 to 5 stars and optional written feedback. Each subject can be reviewed once per booking, after completion.
- **Lists.** `GET /bookings/{id}/reviews` lists a booking's reviews and what the caller may still review. Public lists return a `summary` (average, count, 1–5 histogram) on their first page.
- **Moderation:**
  - **Screening:** comments with phone numbers, email addresses, links or abusive words (English and common Hindi transliterations) are held as `pending`.
  - **Reports:** anyone signed in can report a review once (`POST /reviews/{id}/report`). Three reports hide it as `flagged`.
  - **Decisions:** moderators (`admin_ops`, `admin_support`) work from `GET /admin/reviews` and `POST /admin/reviews/{id}/moderate`: `publish` or `hide`, with a reason written to `audit_logs`.
  - **Ratings:** only published reviews count, and ratings are recomputed on every change.
- **Replies.** The reviewed organisation may reply publicly once (`POST /reviews/{id}/reply`). Replies are screened the same way.

### Flutter

- **Notifications:**
  - **Bell:** on every top-level page it shows the unread count (refreshed every 45 s and on every push) and opens `/<role>/notifications`. Tapping a notification marks it read and opens what it is about. Push settings are on the same screen.
  - **Push:** `PushCoordinator` registers the device's FCM token after sign-in and deletes it at sign-out. It shows a banner for pushes that arrive while the app is open, and opens the right screen when a notification is tapped.
- **Firebase setup:**
  - **All platforms:** pass `--dart-define=FIREBASE_API_KEY=... FIREBASE_APP_ID=... FIREBASE_MESSAGING_SENDER_ID=... FIREBASE_PROJECT_ID=...`. The app id differs per platform.
  - **Web:** also pass `FIREBASE_VAPID_KEY` and copy `web/firebase-config.example.js` to `web/firebase-config.js`.
  - **Without them** push is off and everything else works.
  - **Token API:** the app uses FCM registration tokens (`getToken`), which the server's HTTP v1 sends target. firebase_messaging 17 deprecates that API in favour of installation-id "direct send", which the server does not support yet.
- **Bookings** (`lib/features/bookings`, API in `data/booking_api.dart`): every booking screen uses the real API.
  - **Request flow** (`/customer/book/:id`, from "Request booking" on a listing): dates and start time, work site (saved sites, or a new one placed by device location or PIN code), operator and delivery choices, a live server estimate with availability (taken dates show the next free start), payment mode, notes. It is sent with an `Idempotency-Key` that changes only when the request changes, so a retried tap never makes two bookings.
  - **Detail:** a status card with progress and what happens next, then a **Next steps** card built from the server's `actions` list (pay, cancel with a fee/refund preview, request or withdraw a cancellation, confirm handover, release the machine; owner: accept/decline, assign operator, dispatch, mark arrived, approve/decline a cancellation, mark returned, settle the final bill, issue the invoice, complete). Also: extension request and answer, customer details and a date-clash check for owners, payments and refunds, the frozen price breakdown, agreement, chat, tracking, invoices with download, disputes, the status history and reviews. Lifecycle calls send the booking `version`; a refused step shows the server's message and reloads.
  - **Payment:** the sandbox gateway is completed in-app (a "Test checkout" dialog, then `/payments/{id}/verify`). The Razorpay checkout SDK is not integrated, so with live keys the app says checkout is unavailable.
  - **Tracking** (`/customer|owner/bookings/:id/track`): last position, distance to the site and trips, refreshed every 15 s.
  - **Payments & invoices** (customer profile): bookings waiting for payment and the organisation's invoices.
  - **Lists:** owners filter by Requests, Active rentals and History; the customer home shows live bookings.
  - **Tests:** `test/booking_test.dart` with `test/fake_booking_api.dart`, which moves bookings through the lifecycle on `FakeCommsApi`'s simulated server state (`statusOf`, `fakeActions`).
- **Other screens:**
  - **Agreement:** read, sign and download.
  - **Chat:** the customer's Chat tab. Owners and operators reach it from Profile, from a booking, or from the app-bar shortcut on wider screens.
  - **Maintenance:** the owner tab, with upcoming work, history, costs and plans.
  - **Reviews:** on finished bookings, on listing pages (next to "Message owner"), at `/owner/reviews`, and in the admin **Reviews** tab.
  - **Owner dashboard:** its request list and maintenance alerts now use live data.
- **Tests:** `test/comms_test.dart` with `test/fake_comms_api.dart`. Polling timers are switched off in tests (`badgePollProvider`, `chatPollProvider`).

## 3e. Documents: rental agreements and invoices

Migration `000022_documents`. Packages: `internal/modules/agreements` (`render_v3.go`, `versions.go`), `internal/modules/invoices` (`lines.go`, `pdf.go`), `internal/pdfdoc`.

### What an agreement states (template `rental-v3`)

| Required content | Where it comes from |
|---|---|
| Owner and customer | Organisation legal/trading name, GSTIN, billing address, contact (snapshot) |
| Equipment and identification | Title, make, model, category, year, registration no., serial/chassis no., EQUIPRENT listing ID |
| Start, end and duration | Booking period in the machine's time zone and UTC; duration in days/hours |
| Rental and additional charges | The accepted estimate's itemised lines, split into rental and additional (delivery, pickup, operator, fuel, platform fee, late return, damage), taxes per configured rule, total, advance |
| Deposit | Amount + the template's deposit clauses |
| Delivery information | Site address, work description, delivery/pickup by the owner and their charges |
| Equipment condition | Condition declared on the listing, hour meter when booked, the template's condition clauses (handover/return checklists are part of the agreement) |
| Cancellation policy | The policy frozen with the booking, stated rule by rule |
| Damage responsibility | The template's damage clauses |
| Terms and conditions | The template's use clauses and general terms |

**Legal terms are configuration, not code.** Clauses live in `legal_templates` (`kind=rental_agreement`):
- A template body holds preamble, condition, use, damage, deposit and general clauses, the acceptance statement and the consent checkbox text.
- **Lifecycle:** draft → published → retired. Exactly one template is published. A published or retired template is immutable (database trigger), so a change is a new version.
- **Review record:** a template carries `reviewed_by_name`, `reviewer_capacity`, `review_reference` and `reviewed_at`.
- **Admin API:** `GET /admin/legal-templates[/{id}]` (ops, support, super). Super admins only: `POST /admin/legal-templates` (draft), `PUT /{id}` (edit draft), `POST /{id}/review`, `POST /{id}/publish`, `DELETE /{id}` (drafts only).
- **Publish rules:** publishing requires every section to be filled. With `LEGAL_REQUIRE_REVIEWED=true` it also requires a recorded review.
- **The issued copy:** each agreement copies the template body into its snapshot, so its text and hash always reproduce, even after the template is retired.

**Seeded wording is NOT legally reviewed.** Migration 22 publishes `rental-terms-2026.1`, a working draft with no review record. The agreement text itself says "Legal review of these terms: not recorded". Have an Indian advocate review it, record the review, and publish a reviewed version before going live.

### Versions

- **One current version per booking.** `rental_agreements` now allows several versions per booking, with a unique "current" version per booking and a unique `(booking_id, version)`.
- **Re-issue and amendment.** When the booking's dates or price no longer match the current text, the next read by a party issues a new version, and the previous one is marked `superseded_at` with a reason:
  - **re-issued:** the previous version had not yet been accepted by both parties;
  - **amended:** it had.
- **Superseded versions are kept unchanged.** That includes their acceptance records and PDFs. An accepted (executed) agreement is frozen; the only change the trigger allows is marking it superseded.
- **Superseded versions cannot be accepted.** The acceptance queries require the current version. An e-sign session for a superseded version is expired when its webhook arrives.
- **API:** `GET /bookings/{id}/agreement/versions`, `GET .../versions/{n}`, and `GET .../versions/{n}/document` (PDF link). The executed PDF of a version is stored when the second acceptance lands and served unchanged later.

### Acceptance records versus electronic signatures

- **What the built-in provider records.** `ESIGN_PROVIDER=builtin` stores an **acceptance record**: the typed name, an explicit consent checkbox, account, time, IP, device and the SHA-256 of the exact text. The consent text comes from the template and is stored with each record (`agreement_signatures.consent_text`).
- **What that record is, legally.** It is evidence that a party agreed. It is **not** a digital signature or Aadhaar eSign under the Information Technology Act, 2000 (s.3A / Second Schedule). The API (`assurance`, `assurance_note`), the agreement text, the PDF ("ACCEPTANCE RECORD") and the app all say so.
- **What the code does not decide.** Whether a click-wrap acceptance is enough for a given rental is a legal question.
- **External providers** implement `agreements.Provider` (`Begin` → redirect, HMAC-verified webhook → signature with `method=provider`). The API reports these as `electronic_signature`.
  - `ESIGN_PROVIDER=sandbox` simulates one for development.
  - **No real e-sign service provider is integrated.** Aadhaar eSign through a licensed ESP (for example Leegality, Digio or SignDesk) needs an account and a `Provider` implementation.

### Invoices

- **Issuing.** `POST /owner/bookings/{id}/invoices` (owner, once the machine is returned) issues one rental invoice per booking.
  - **Lines:** from the settled final charges when present, else the accepted estimate. Rental lines keep units × rate.
  - **Excluded lines:** the estimate's own tax lines, and the platform fee. The platform fee is EQUIPRENT's supply, not the owner's, and a note on the invoice explains its absence.
- **GST is not hardcoded.**
  - **Per-line tax:** each line is taxed by the `tax_rates` rules frozen in the booking's estimate, matched by charge component. The rate is the sum of the applicable rules, and the SAC and tax code come from the rule.
  - **Split:** CGST + SGST when the supplier's state (GSTIN prefix) equals the place of supply (the site's state), else IGST.
  - **Rule review:** tax rules now carry `sac_code`, `legal_basis`, `reviewed_by_name` and `reviewed_at`. Saving a rule clears its review. `POST /admin/tax-rates/{id}/review` (finance) records a review.
  - **Gate:** with `LEGAL_REQUIRE_REVIEWED=true`, invoices are refused (503) while any active rule is unreviewed.
  - **Seeded rule not reviewed:** the seeded 18% GST rule (SAC 997313) has no review record. Have a chartered accountant confirm the rate and SAC for your services under the current notifications.
- **No GSTIN:** the document is a **bill of supply** with no GST.
- **Unique numbers.** Numbers are gap-free per supplier, prefix and Indian financial year: `ER/2627/00001` for invoices and `CN/2627/00001` for credit notes. The series row is locked until commit.
- **Frozen details.** Issued invoices freeze supplier and recipient (name, GSTIN, address, state code), equipment (registration, serial, listing ID), rental period and site, the basis (final or estimate), and the captured payments with receipt numbers and gateway payment IDs. The database keeps issued invoices immutable.
- **Refunds and adjustments.** `GET /invoices/{id}` adds the booking's refunds and the credit notes issued against the invoice, plus `credited_paise` and `net_paise`.
  - **Credit notes:** `POST /invoices/{id}/credit-notes` (supplier owner or finance) credits taxable value up to what remains. The tax is credited in proportion, and the last credit returns exactly the remaining tax.
- **PDF:** `GET /invoices/{id}/pdf` (session) and `/link` → `/download?exp&sig` (signed, short-lived). It shows parties, equipment, dates, lines with SAC and rate, the tax split, totals, payments, refunds and credit notes, and notes. The output is deterministic.

### Security and audit

- **Who can read.** Agreements, versions and history: only the booking's parties and admins (others get 404). Invoices: the issuer, the recipient and finance admins.
- **How files are served.** PDFs live in the private store or are rendered on request. They are served as attachments with `Cache-Control: private, no-store`.
- **What is audited.** Every link issued and every invoice download (`document.url_issued` / `document.downloaded`).
- **History.** `GET /bookings/{id}/documents/history` lists issues, acceptances, supersessions, invoice issues and downloads, newest first. It shows actor names and no IP addresses.

### Configuration

| Variable | Default | Meaning |
|---|---|---|
| `LEGAL_REQUIRE_REVIEWED` | on in staging/production, off otherwise; cannot be off in production | Refuse agreements from unreviewed terms and invoices under unreviewed tax rules; refuse to publish unreviewed terms |
| `ESIGN_PROVIDER` | `builtin` | `builtin` (acceptance records) or `sandbox` (simulated external e-sign, development only) |

### Flutter

- **Agreement:**
  - The page shows the version, whether the terms were legally reviewed, the server's consent text on the checkbox, and "Accept agreement" for the built-in provider ("Continue to e-sign" for an external one). It also has links to the versions list and the history.
  - `/…/bookings/{id}/agreement/versions[/{n}]` lists versions; a single version shows its exact text, acceptance records and PDF.
  - `/…/bookings/{id}/documents` is the history.
- **Invoices:** `/…/invoices/{id}` shows the invoice, bill of supply or credit note in full. The owner can issue credit notes there. Invoice rows on booking detail and on Payments & invoices open it.
- **Admin → Compliance:** tax rules (rate, SAC, basis, record review) and agreement terms (clauses, record review, publish drafts). Drafting new terms is an API action.
- **Tests:** `test/documents_test.dart`. The new routes are rendered at 360/800/1440 px in `test/app_test.dart`.

### Tests (backend)

- **Unit tests:**
  - `agreements/render_v3_test.go`: every required section; clauses come from the template; reproducible hash; template validation; duration; superseded watermark.
  - `invoices/lines_test.go`: configured per-component rules and SAC; final charges preferred; platform fee and estimate tax lines excluded; bill of supply; per-line split; credit-note tax; PDF content and determinism.
- **Integration tests, `tests/documents_test.go`:**
  - versions, re-issue and amendment, and accepting superseded text (409);
  - acceptance records with consent, frozen superseded rows, and outsider 404s;
  - history without IPs;
  - legal template draft, review and publish, with the review gate;
  - invoices with per-line GST, frozen details and receipts;
  - credit notes: limits, roles and exact tax;
  - PDF and signed-link permissions, other owners and outsiders;
  - the tax-review gate and bills of supply.

## 3f. Operators and fleet management

Migration `000023_fleet_operations`. Packages: `internal/access` (scope), `internal/modules/fleet`, `internal/modules/operations`, plus changes to `bookings`, `notifications`, `invoices`, `equipment` and `owners`.

### Fleets and the fleet-manager scope

- **Fleets.** A fleet (`fleets`) is a named group of an owner's machines (`fleet_equipment`: a machine is in at most one fleet) with its managers (`fleet_managers`). Triggers keep a fleet's machines and managers within the owner's organisation.
- **One scope view.** `fleet_manager_scope` = direct grants (`fleet_manager_equipment`, the older per-machine access) ∪ the machines of the active fleets a person manages.
  - It counts only while their `fleet_manager` role is current, and not while a `fleet_management` restriction is in force.
  - `all_equipment` still grants every machine, also unless restricted.
  - **Every check reads this view:** the equipment guard on `/owner/equipment/{id}*`, the listing and maintenance lists, bookings, fleet views and notifications.
- **What a fleet manager can reach, and only for machines in scope:**
  - **Equipment:** list, view, edit, delete, photos, documents, pricing, availability blocks, calendar and maintenance.
  - **Bookings:** `bookings.Service.Access` drops the owner side when the machine is out of scope, so the booking, its agreement, chat, invoices, evidence and issues all answer 404. `GET /owner/bookings` is filtered the same way. Accepting, assigning operators and final charges follow from that.
  - **Fleet views:** `GET /fleet/{org}/overview|live|equipment|schedule|reports/utilisation` are computed over the scoped machines (`scope: "assigned"`).
  - **Notifications:** owner-side notifications reach a fleet manager only when the booking's (or the named) machine is in their scope (`OwnerSideRecipients`).
  - **Invoices:** only invoices of bookings in scope; `/invoices` is filtered.
  - **Not available:** the organisation-wide `GET /owners/me/dashboard` is owner-only now.
- **Managing fleets.**
  - **Owners:** `POST /owner/fleets`, `PATCH /owner/fleets/{id}`, `POST .../archive`, `POST/DELETE .../equipment[/{equipment_id}]` (machines move from any other fleet), and `POST/DELETE .../managers[/{user_id}]` (only owners decide who manages a fleet).
  - **Fleet managers:**
    - create fleets and become their manager;
    - edit and archive the fleets they manage;
    - add only machines they already manage that are not in another manager's fleet (else 400 `not_in_scope` / 409).
  - **Reading fleets:** `GET /fleet/{org}/fleets[/{id}]` (owners see all; fleet managers see theirs).
  - **New machines:** a fleet manager who creates a machine gets a direct grant to it.

### Fleet views

| Endpoint | What |
|---|---|
| `GET /fleet/{org}/overview` | Machines, listed, on a job, utilisation, active bookings, operators on duty, open maintenance, rental earnings in the last 30 days (deposits excluded) |
| `GET /fleet/{org}/equipment?fleet_id=` | Status board: state (on_job, reserved, maintenance, blocked, idle, unlisted), current booking and operator, next booking, open maintenance, last position |
| `GET /fleet/{org}/schedule?from&to` | Bookings and availability blocks per machine (max 3 months); `needs_operator` marks confirmed wet-hire bookings with no operator |
| `GET /fleet/{org}/reports/utilisation?from&to` | Booked hours, bookings, utilisation % and earnings (captured rental money net of refunds) per machine, with totals |
| `GET /fleet/{org}/live` | Last known positions (only machines in scope; positions come from trips and active rentals) |
| `GET /fleet/{org}/operators` | Roster: duty, verification, `assignments_blocked`, current job, licence expiry |

Availability and maintenance are managed through the existing machine endpoints (`/owner/equipment/{id}/blocks`, `/maintenance/*`), which the scope guard already covers.

### Operators

- **Registration.** An org admin or fleet manager adds an operator by phone (`POST /orgs/{org}/members`, role `operator`). The operator signs in with an OTP and completes the profile, qualifications and licence (`/verification/operator/*`). An admin verifies them in the verification queue; only verified operators can be assigned.
- **Assignment rules.**
  - `POST /owner/bookings/{id}/assign-operator` (owner, or a fleet manager in scope) works on `confirmed` and, to reassign, `operator_assigned` bookings. On a reassignment the previous assignment becomes `replaced`.
  - **Refused:**
    - **Overlapping jobs:** 409 "this operator already has a job that overlaps these dates", from the database exclusion constraint.
    - **Unverified operators:** 422 `KYC_REQUIRED`.
    - **Restricted operators:** 422 (`operator_assignments` restriction).
- **Job workspace.**
  - **Lists:** `GET /operator/jobs?scope=current|history`, and `GET /operator/schedule?from&to`.
  - **Job detail:** `GET /operator/jobs/{booking_id}` shows the customer and owner, the machine (make, model, registration, serial, hour meter), the site with a Google Maps directions URL from the yard, checklists, issues and `next_steps`. Phone numbers appear once the operator has accepted.
- **Status changes (the booking engine).**
  - **Steps:** accept/decline → dispatch → arrived → **handover checklist** → start → (customer requests the return) → **return checklist** → returned.
  - **Evidence rules:** the operator's `start` is refused (422) until a handover checklist exists, and `returned` until a return checklist exists. On return the assignment becomes `completed`.
  - **Customer confirmation:** the customer can still confirm the handover themselves (`/bookings/{id}/handover/confirm`).
- **Evidence.**
  - **Submitting:** `POST /operator/jobs/{booking_id}/checklists` (or `POST /bookings/{id}/checklists` for the owner side on dry hire). It needs the hour meter and its photo, **at least two** `job_photo` uploads of the machine (the caller's own; re-encoded, metadata stripped), items, fuel, notes, position and a `client_id` for offline retries.
  - **When:** handover only on site; return only during or at the end of the rental, and never below the handover reading. One checklist of each kind.
  - **Side effects:** the machine's hour meter is updated. Damage notes at return also file a damage issue.
  - **Reading:** `GET /bookings/{id}/checklists` returns short-lived photo links, for the parties and the operator only.
- **Issues.** `POST /operator/jobs/{id}/issues` or `POST /bookings/{id}/issues` files damage, breakdown, safety, site-access or other reports (stage, severity, photos). Reporters are the operator, the owner side or the customer. The owner side (in scope) is notified (`issue.reported`).

### Admin

- **Operator verification:** unchanged (verification queue). The operator detail page adds **Block job assignments**.
- **Issues:** `GET /admin/issues?status&severity` (most severe first) and `POST /admin/issues/{id}/review` (`under_review`, `resolved` or `dismissed`; a note is required to close). The owner side and the reporter are notified (`issue.reviewed`). Roles: admin_ops / admin_support.
- **Fleet accounts:** `GET /admin/fleets?org_id&status`, and `POST /admin/fleets/{id}/archive` (the managers lose access).
- **Account restrictions** (`account_restrictions`, one active per user and kind, optional expiry, audited):
  - `operator_assignments`: cannot be given jobs.
  - `fleet_management`: loses all fleet machine access and cannot create listings.
  - `new_bookings`: cannot make bookings.

  `GET /admin/restrictions`, `POST /admin/users/{id}/restrictions`, `POST /admin/restrictions/{id}/lift`. Full suspension remains `POST /admin/users/{id}/suspend`.

### Flutter

- **Operator** (`lib/features/operator`, real API `data/operator_api.dart`):
  - **Lists:** Today (open jobs and the next 14 days), Assignments and History.
  - **Job:** contacts (call), machine, site, evidence, issues and one next-step button.
  - **Navigation:** opens directions; arrival is marked from it.
  - **Checklists:** handover and return, with hour-meter photo, machine photos, checks, fuel and damage.
  - **Issues:** a report sheet.
- **Fleet manager** (`lib/features/fleet_manager`, `data/fleet_api.dart`):
  - **Overview:** KPIs, alerts (no operator, licence expiring, restricted operators) and fleets.
  - **Equipment board:** filters by fleet, plus edit, availability and maintenance through the listing and maintenance pages under `/fleet/...`.
  - **Operators:** roster and invite.
  - **Assignments and schedule:** assign and reassign; restricted or unverified operators cannot be picked.
  - **Reports:** earnings, utilisation per machine and locations.
  - **Fleet detail:** add and remove machines.
- **Owner:** Profile → Fleets (`/owner/fleets[/{id}]`). Owners also add and remove fleet managers.
- **Admin → Operations:** issues (review, resolve, dismiss), restrictions (restrict by user ID, lift) and fleet accounts (archive).
- **Tests:** `test/fleet_ops_test.dart` with `test/fake_operations_api.dart`. The new routes are rendered at three widths in `app_test.dart`.

### Tests (backend)

- **`TestFleetManagerMachineScope`:**
  - fleets and managers;
  - listings and edits outside the fleet (404);
  - board, overview and utilisation scoped;
  - booking inbox, detail, accept and agreement outside the fleet (404);
  - notifications only for in-scope bookings;
  - fleet-manager fleets limited to their machines; only owners assign managers;
  - the `fleet_management` restriction removes access and listing creation, and lifting restores it;
  - removing the manager ends access;
  - admin fleet list and archive.
- **`TestOperatorAssignmentConflictsAndReassignment`:**
  - overlapping assignments refused (409); non-overlapping accepted;
  - reassignment to a busy operator refused, to an unverified one refused (422), to a verified one accepted;
  - the replaced operator loses the job and sees it in history without phone numbers;
  - restricted operators cannot be assigned.
- **`TestOperatorJobStatusChangesAndEvidence`:**
  - job detail before and after accepting;
  - other operators and customers on operator endpoints get 404;
  - handover before arriving refused (409), start without handover refused (422);
  - photo purpose, ownership and minimum count enforced;
  - offline replay returns the same checklist; a second handover is refused (409);
  - evidence visible to the parties with working photo links; outsiders 404;
  - issue reporting and admin review rules;
  - return checklist before returned, hour meter monotonic, damage notes become an issue;
  - assignment completed and in history; equipment hour meter updated;
  - notifications to the owner and the reporter.

## 3g. Location and live tracking

Migration 24, module `internal/modules/tracking` (`location.go`, `telematics.go`), `internal/geo` (routes and ETA) and `internal/live` (hub and WebSocket tickets). Flutter: `lib/features/tracking`.

**A phone is not the machine.** Every stored position has a `source`:
- `app`: the operator's phone. It shows where the phone is, which is usually but not always with the machine.
- `telematics`: a GPS tracker fitted to the machine.
- `simulated`: development only.

The track view keeps `equipment_position` (the best machine fix: a tracker less than 10 minutes old wins over the phone) separate from `operator_position` (the phone), and returns a `source_note` that says which is which.

**Hardware trackers.** Owners register a device per machine (`POST /owner/equipment/{id}/trackers`). A `trk_…` token is returned once; only its SHA-256 is stored. Devices post to `POST /telematics/{provider}/positions` with `Authorization: Bearer trk_…`.
- Providers implement `TelematicsProvider.Parse`. `generic` (JSON `{device_id, positions:[…]}`) is built in; `simulated` exists only when simulation is allowed.
- A new vendor means a new parser, with no schema change.
- Tracker positions always update the last known location, and are kept as history only while a trip is open.

**Sessions and consent.**
- Phone positions are accepted only inside a tracking session. `POST /bookings/{id}/tracking/session` (also under `/operator/jobs/{id}/…`) needs `consent: true` and the current `policy_version` from `GET /tracking/policy`; a stale version gets 409. The sharer is the assigned operator, or the owner side when there is no operator.
- Sessions are allowed only while the booking is `dispatched` … `return_initiated`.
- Sessions end in four ways, each recorded on the trip's `end_reason`:
  - the person stops sharing (`DELETE …/tracking/session`, which also withdraws consent);
  - the booking leaves the trackable statuses (the `bookings_stop_tracking` trigger);
  - no upload for `TRACKING_IDLE_TIMEOUT` (worker job `tracking.close_idle_sessions`);
  - arrival.
- Uploads without an active consent get 403 `TRACKING_CONSENT_REQUIRED`. Battery level is no longer stored.

**Who sees what.**
- The owner side, the assigned operator and admins see the track view.
- Customers see positions only from dispatch until hand-back; otherwise they get `hidden: true` and no coordinates, and history and the live stream return 403.
- Fleet managers see only machines in their scope.
- Every view, history read and live connection is written to `location_access_log`, one row per user, subject, kind and hour with a count. Admins read it at `GET /admin/location-access`.

**Live updates.**
- `POST /bookings/{id}/track/ticket` or `POST /fleet/{org}/live/ticket` returns a ticket that lasts `TRACKING_TICKET_TTL` (default 1 minute). It is an HMAC over user, kind, subject and expiry.
- The client then opens `GET /ws/track?ticket=…`, a WebSocket. Access is re-checked when it connects, and allowed origins come from the CORS settings.
- Messages are `snapshot`, `position` and `session_ended`. The hub is in-process: a slow client loses its oldest messages, never blocks ingest. Running several API instances would need a shared bus such as Redis pub/sub.
- The app falls back to polling the track view when the socket cannot be opened.

**ETA.**
- With `GOOGLE_MAPS_API_KEY` set on the server, the Directions API is used: driving with traffic, plus one third for heavy transport, cached for 2 minutes per roughly 100 m origin cell.
- Without a key, or on any error or quota problem, it falls back to a straight-line estimate (distance × 1.3 at 30 km/h). `eta.method` says which one was used.
- The key is never logged.

**Flutter.**
- The map uses Google Maps when `--dart-define=GOOGLE_MAPS_API_KEY` is set and the API loads. Otherwise it uses the built-in map with a note, and tracking still works.
- Development builds without a Maps key, or with `--dart-define=TRACKING_SIMULATE=true`, use a simulated GPS that drives to the site, and uploads are flagged `simulated`. Production never simulates, and the server refuses simulated uploads there.
- Location permission is requested only after the person accepts the tracking notice.
- Background sharing is opt-in:
  - Android: a foreground-service notification (`FOREGROUND_SERVICE_LOCATION`).
  - iOS: `UIBackgroundModes: location`, with the blue indicator shown.
- Screens:
  - booking tracking (live badge, source, ETA method, session, trips, trip history);
  - "Share my location" card on the operator job page;
  - live fleet map at `/fleet/live` and `/owner/live`.

**Tests.**
- `backend/tests/tracking_test.go` covers:
  - consent, the policy version and automatic stop on status change;
  - authorisation and the audit log;
  - idle close;
  - hardware trackers (token, provider mismatch, foreign device, revoke);
  - the WebSocket end to end (bad ticket, snapshot, live position, `session_ended`).
- Unit tests cover `geo` (haversine, polyline decoding, Google with fallbacks) and `live` (hub fan-out, drop-oldest, tampered or expired tickets).
- Flutter `test/tracking_test.dart` covers:
  - hidden before dispatch;
  - live updates, where a tracker outranks the phone;
  - the polling fallback and session end;
  - trip history;
  - consent before permission;
  - batched uploads and stop;
  - blocked permission;
  - server-side stop;
  - the fleet live map;
  - the simulator.

## 4. Tests

```powershell
.\scripts\test.ps1 -BackendOnly                # go vet, sqlc vet, unit tests
.\scripts\test.ps1 -BackendOnly -Integration   # + tests against TEST_DATABASE_URL
```

- **Unit tests** cover pricing (it reproduces the worked example in API_DESIGN §3 to the paisa), cancellation fees, booking permissions and transitions, listing lifecycle and pricing rules, auth (OTP limits, refresh rotation, reuse detection, tampered and expired tokens), idempotency replay, rate limiting, GST split and financial year, agreement hashing, webhook signatures, notification routing, the scheduler, Postgres error mapping, validation messages, pagination and OpenAPI generation.
- **Contract tests** (`internal/app`) cover three things: every served route is documented and every documented route is served; every secured operation returns 401 without a token; and the committed `api/openapi.json` matches the code.
- **Integration tests** (`backend/tests`) run the full rental lifecycle (section 1), the auth session flows and the verification workflows through the real router and database. The verification tests cover document permissions, signed and tampered links, upload validation, reasons, resubmission, revocation, append-only history, suspension and fleet-manager machine scope. They also roll every migration down and up on a throwaway database.

After changing routes or DTOs: `go test ./internal/app -run OpenAPI -update` (or `go run ./cmd/api openapi > api/openapi.json`).

## 5. Adding a feature

1. Write SQL in `backend/sql/queries/<area>.sql` and run `.\scripts\sqlc.ps1`. Use `sqlc.narg()` for optional filters and `COALESCE` for nullable scalar subqueries.
2. Create `internal/modules/<name>`: models and request DTOs with `binding` tags, a `Repository` interface plus a pgx implementation (wrap errors with `database.MapError`), a service, and a handler implementing `httpx.Module`.
3. Register routes with `r.Tag("<name>")`, `.Authed()` or `.Admin(roles...)`, and an `httpx.Doc` for each. Use `httpx.OrgHandle(status, roles, fn)` for organisation-scoped endpoints and set `Idempotent: true` on money- or booking-creating POSTs.
4. Add the handler in `internal/app/app.go`, write unit tests with a fake repository, then update the OpenAPI file (see section 4).

## 6. Not built yet

These are honest gaps, listed so nobody assumes they exist:

- **External providers are interfaces with development implementations.** SMS (`auth.SMSSender`) and email (`auth.EmailSender`) have only the dev simulator and a "disabled" sender, so production OTP and email links stay off until a provider is integrated (a DLT-registered SMS sender is needed in India). The live Razorpay client and the FCM sender are implemented, but have only been tested against fake HTTP servers, not the real services.
- **E-signatures:** only built-in acceptance records (typed name + consent; not a digital signature) and a sandbox provider exist. Aadhaar eSign through a licensed ESP needs an account and an `agreements.Provider` implementation.
- **Legal and tax review:** the seeded agreement terms (`rental-terms-2026.1`) and the seeded 18% GST rule have **no** recorded review. Staging/production refuse agreements and invoices until a lawyer and a chartered accountant review them and the review is recorded (§3e).
- **Platform-fee invoice:** EQUIPRENT's own GST invoice for the customer platform fee is not issued yet (it needs the platform's GSTIN and business details as configuration). Pricing also still adds GST for owners without a GSTIN, while their invoice is a bill of supply without GST.
- **Tracking:** no specific telematics vendor is integrated yet (use the `generic` JSON format or add a `TelematicsProvider`); the live hub is single-instance; Google Maps keys are needed for a real map and road ETAs. Checklists have no customer OTP countersignature yet.
- **Payments (Razorpay) — in progress:** migration 21 (receipt numbers, security deposits, ledger idempotency, settlement breakdown, Route linked accounts/transfers) and `internal/ledger` exist; the services, settlement jobs, Route onboarding, owner earnings and their screens do not yet.
- **Payouts:** `payout.paid` notifications are routed but nothing emits them yet (settlements and payouts are not built). Refunds are processed by the `refunds.process` job and announced as `booking.refunded`.
- **Payout-account validation with live gateway keys** (Razorpay fund-account validation) is not integrated; `PUT /verification/owner/bank-account` answers 503 until it is. Automated PAN/GSTIN/DigiLocker checks are not built either: documents are reviewed by people.
- **API_DESIGN endpoints not implemented yet:** RFQs and quotes, daily work-log approval (handover/return checklists are built, §3f), offline payment recording, settlements and payouts and the double-entry ledger postings, WebSocket live tracking and chat, TOTP / second factor for admins (admins currently sign in like everyone else and hold a platform role), phone-number change, and data-export / deletion requests.
- **KYC is not enforced on bookings yet** (the API design calls for KYC level "basic").
- Without Redis, rate limits and OTP throttles are per process. Set `REDIS_URL` before running more than one API replica.
