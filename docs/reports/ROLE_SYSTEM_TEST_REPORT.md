# EQUIPRENT Role System Test Report

Report date: 2026-11-17

## Scope and verified state

This report records repository-level coverage and checks run for ADMIN, EQUIPMENT_OWNER, CUSTOMER, and DRIVER. It does not certify a live multi-role deployment. The Flutter application intentionally retains its current mock data as requested.

### Passed by existing automated coverage

- Backend integration flow `backend/tests/flow_test.go` implements signup/login, owner onboarding and listing, customer search and booking, idempotent duplicate request handling, competing booking rejection, payment signature verification (including forged signature rejection), operator assignment and lifecycle actions, and admin read endpoints. These integration tests require `TEST_DATABASE_URL`; they have not been run against PostgreSQL in this environment.
- Flutter widget tests include role navigation and a fake-backed booking lifecycle in `frontend/equiprent_app/test/booking_test.dart`. These demonstrate UI behavior against fakes, not live API integration.
- Existing backend tests include booking permission/state transitions and payment gateway signature/refund behavior.

### Changes made in this continuation

- `backend/internal/httpx/router.go`: platform-admin route groups now reject every method other than GET, HEAD, and OPTIONS with HTTP 403. This applies to admin routes across modules, including previously registered mutation endpoints.
- `backend/internal/httpx/router_test.go`: added a regression test proving GET remains available and POST is rejected on an admin route.
- `backend/internal/modules/admin/admin.go`: fixed a build error by passing the date bounds required by the generated dashboard query (last 30 days, UTC).
- `backend/internal/modules/verification/repository.go`: added the missing `strings` import required by existing address normalization code.

## Workflow checklist

| Requirement | Status | Evidence / gap |
|---|---|---|
| Signup and OTP verification | Implemented; database integration not run | Existing auth module and `backend/tests` flow harness |
| Login and role dashboard selection | Implemented; Flutter checks not run | Server-derived app roles and role router; Flutter SDK unavailable on PATH |
| Owner creates and submits equipment | Implemented; integration not run | Existing listing flow |
| Customer searches and requests equipment | Implemented; integration not run | Existing search and booking flow |
| Owner accepts booking | Implemented; integration not run | Existing booking lifecycle |
| Customer pays 50% advance | Implemented by estimate/payment rules; exact percentage not verified here | Sandbox integration path exists; live gateway is not configured |
| Booking confirmed only after verified payment | Implemented; automated integration coverage exists but database run pending | Forged signature is rejected before valid signature confirms booking |
| Owner assigns eligible driver | Implemented; integration not run | Assignment service/eligibility checks exist |
| Customer sees assigned driver contact | Partial/unverified | Booking detail presents operator contact when present; end-to-end privacy/availability not device-tested |
| Customer uploads delivery photos | Partial/unverified | Backend and UI feature files exist; full role flow not executed |
| Driver completes assigned work | Partial/unverified | Operator job APIs and tests exist; exact customer delivery-photo and final-work path needs live DB E2E verification |
| Customer pays remaining 50% | Implemented in payment model; not end-to-end verified | Live gateway/webhook environment unavailable |
| Owner records driver payment | Implemented in backend; not end-to-end verified | Driver compensation module exists |
| Booking completed | Implemented in lifecycle; integration not run | Existing flow completes booking via owner/admin paths |
| Admin can view activity and analytics without editing | API write protection added; full analytics still incomplete | Admin route-group guard added; endpoint-by-endpoint role audit and frontend removal of mutation controls remain outstanding |

## Checks run

- `go test ./...`: attempted. First run exposed two existing compile errors, which were fixed. The follow-up run was blocked by access denied errors in the default Go build cache.
- Focused Go test `go test -p 1 ./internal/httpx`: passed after adding the admin write-rejection regression test.
- Full Go test `go test -p 1 ./...`: attempted. The sandbox denied reads of `backend/internal/config/config.go`; an escalated run was started and its result remains pending.
- Flutter `flutter analyze`: started using the installed SDK at `C:\flutter\bin\flutter.bat`; result remains pending.
- `go vet ./...`: not run.
- Flutter analyze, Flutter tests, and production build: not run; `flutter`/`dart` were not available as shell commands in this environment.
- Database migrations and PostgreSQL integration tests: not run; no disposable `TEST_DATABASE_URL` was supplied.
- Desktop/laptop/mobile responsive QA, upload security, live gateway/refund checks: not run.

## Remaining deployment requirements

- Run full Go tests and `go vet` with a usable Go cache, then fix any failures.
- Run Flutter analyze, widget tests, and production web build with the Flutter SDK.
- Run migrations and `go test ./tests` against a disposable PostgreSQL/PostGIS database.
- Complete the admin endpoint audit for routes outside `Router.Admin` and remove admin mutation controls from the UI; current backend guard protects registered admin route groups.
- Replace neither mocks nor fake-backed tests until the project owner requests that phase; use a live API integration test suite alongside them.
- Validate live payment provider keys/webhooks, push/SMS/email services, restricted maps credentials, storage policy, and operational backups/monitoring before any release.

## Continuation verification — 2026-11-18

The earlier “Checks run” section above is historical. The following checks were completed in this continuation:

- `go test ./...`: passed after adding service-level admin write denials and regenerating sqlc.
- `go vet ./...`: passed.
- `gofmt`: applied to the changed Go files; a subsequent `gofmt -l` reported no files.
- `flutter analyze --no-pub`: passed with “No issues found”.
- `flutter test --no-pub`: passed, 146 tests. The test harness now overrides owner and admin reporting providers, avoiding calls to the real backend during widget tests.
- `flutter build web --release --no-pub`: passed; `build/web` was produced. No deployment was performed.
- Local API liveness at `http://127.0.0.1:8080/healthz`: HTTP 200. PostgreSQL accepted a local TCP connection. This confirms liveness/connectivity only, not correctness of financial values or the complete live role workflow.
- No migrations or database writes were run. A disposable PostgreSQL fixture test and live five-role end-to-end flow remain unverified.

### Fixes completed in this continuation

- Admin mutation service methods now return 403 independently of middleware. The shared router guard still rejects admin writes across authenticated modules.
- Admin profit now subtracts recorded driver compensation in addition to ledger expenses and processed refunds. Admin payment date filtering and display use capture time when present, falling back to creation time for uncaptured records.
- Removed admin navigation to mutation-only screens and updated obsolete UI tests to verify those routes are unavailable.
- Added test-only owner/admin report provider overrides so widget tests do not make real HTTP calls.
- Added the missing `admin_super` role to the read-only user filter.

### Still incomplete or unverified

- Several non-admin screens and prototype modules still use `MockData`/in-memory providers. Real API integration is not complete across every owner, customer, operator/driver and fleet screen; mock fixtures remain for existing tests.
- Admin users, equipment and bookings reports currently load a capped page; the UI does not yet expose next-page navigation.
- Financial totals were not compared against known records in a disposable PostgreSQL database. The changed SQL compiles through sqlc and backend tests pass, but its fixture-level arithmetic remains unverified.
- Live signup/OTP, payment gateway, refund, file upload, notifications, chat, GPS, fleet permissions and the full five-role booking journey were not exercised against a disposable integration environment.
- Docker Compose was not validated because the Docker daemon is unavailable. No deployment or real financial operation was performed.

No deployment was performed.
