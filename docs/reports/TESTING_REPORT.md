# EQUIPRENT Testing Report

## Scope and evidence

This report reflects repository inspection on 2026-11-17. The project contains Go unit and integration test packages (`backend/internal/...`, `backend/tests`), Flutter widget tests (`frontend/equiprent_app/test`), and a PowerShell quality script (`scripts/test.ps1`). A test is not considered passed unless its command completed successfully in this environment.

## Results

| Area | Status | Evidence / limitation |
|---|---|---|
| Go unit tests | Not verified in this audit | Run `go test -count=1 -p 1 ./...` from `backend`. |
| Go vet | Not verified | Run `go vet ./...` from `backend`. |
| PostgreSQL/PostGIS integration | Not run | `backend/tests/integration_test.go` skips without `TEST_DATABASE_URL`; use a disposable migrated database. |
| Flutter analyze/widget tests | Not run | Flutter/Dart executables were not found on PATH in this environment. |
| Android/iOS builds | Not run | No Flutter SDK/device/toolchain verification. iOS builds require macOS/Xcode. |
| Web build | Not run | Requires Flutter SDK. |
| Booking concurrency/database constraints | Not verified end-to-end | Existing booking tests are present; execute integration suite against isolated PostgreSQL/PostGIS. |
| External payment/push/maps integrations | Not live-tested | Backend docs identify sandbox/fake-provider coverage; credentials and external accounts are not available. |

## Commands

From repository root on PowerShell:

```powershell
.\scripts\test.ps1
.\scripts\test.ps1 -BackendOnly
.\scripts\test.ps1 -FrontendOnly
.\scripts\test.ps1 -Integration
```

The integration command requires `TEST_DATABASE_URL` to point to a disposable test database. Do not point it at production or valuable development data: integration setup applies migrations and uses database state.

Direct commands:

```powershell
Push-Location .\backend; go vet ./...; go test -count=1 -p 1 ./...; Pop-Location
Push-Location .\frontend\equiprent_app; flutter analyze; flutter test; Pop-Location
```

## Workflow coverage

The repository has role-based widget and integration flows for registration/verification, listings, bookings, payments, operator checklists, tracking, maintenance, reviews, disputes, and fleet operations. These are a mix of real API adapters and fake/in-memory test implementations. Passing a fake-backed Flutter test demonstrates UI behavior against a test double, not live service integration.
