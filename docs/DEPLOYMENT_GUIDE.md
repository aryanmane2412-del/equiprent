# EQUIPRENT Deployment Guide

## Current state

Deployment is not configured. This guide provides safe preparation and exact build/test commands; it does not deploy anything or select/purchase a hosting provider. See `infrastructure/deployment/README.md` for existing container notes.

## Local stack

```powershell
Copy-Item .env.example .env
docker compose up -d
docker compose --profile redis up -d
docker compose --profile app up -d --build
docker compose --profile web up -d --build
docker compose ps
```

Do not use `docker compose down -v` unless you intend to delete local database volumes.

## Backend build

```powershell
Push-Location .\backend
go test -count=1 -p 1 ./...
go vet ./...
go build -trimpath -o .\bin\equiprent-api.exe .\cmd\api
go build -trimpath -o .\bin\equiprent-worker.exe .\cmd\worker
Pop-Location
```

Container images:

```powershell
docker build -f infrastructure/docker/backend.Dockerfile --build-arg TARGET=api backend
docker build -f infrastructure/docker/backend.Dockerfile --build-arg TARGET=worker backend
```

## Flutter builds

Requires a configured Flutter SDK. Supply public API endpoints via `--dart-define`; never place server secrets in a Flutter build.

```powershell
Push-Location .\frontend\equiprent_app
flutter pub get
flutter analyze
flutter test
flutter build web --release --dart-define=APP_ENV=production --dart-define=API_BASE_URL=https://api.example.invalid
flutter build apk --release --dart-define=APP_ENV=production --dart-define=API_BASE_URL=https://api.example.invalid
flutter build appbundle --release --dart-define=APP_ENV=production --dart-define=API_BASE_URL=https://api.example.invalid
Pop-Location
```

iOS release requires macOS, Xcode, signing assets, and Apple provisioning:

```bash
cd frontend/equiprent_app
flutter pub get
flutter analyze
flutter test
flutter build ipa --release --dart-define=APP_ENV=production --dart-define=API_BASE_URL=https://api.example.invalid
```

## Production preparation (not deployed)

1. Select a compliant host/region and managed PostgreSQL with PostGIS, TLS verification, PITR backups, and restore drills.
2. Provision shared Redis with TLS, authentication, and monitoring.
3. Provision private S3-compatible storage with encryption, restricted IAM, lifecycle/retention, and signed URLs.
4. Configure Razorpay, Firebase, restricted Google Maps keys, email/SMS providers, and cloud-managed secrets. Verify callbacks/webhooks and rotate credentials.
5. Set `APP_ENV=production`, `LOG_FORMAT=json`, exact `CORS_ALLOWED_ORIGINS`, `CORS_ALLOW_LOCALHOST=false`, secure database/Redis URLs, trusted proxy CIDRs, and required non-development secrets. Validate with `scripts/check-env.ps1`.
6. Build immutable tagged API/worker/web images, scan them, apply migrations as a separate release step, deploy behind HTTPS, then verify `/healthz` and `/readyz`.
7. Configure central logs, metrics, alerts, backups, incident response, DNS, certificates, and rollback.

Production launch remains blocked until real providers, hosting, CI/CD, backup/restore, and live integration checks are configured and approved.
