# Deployment

Not set up yet. This folder will hold staging and production deployment config. The plan is in [docs/ARCHITECTURE.md §10](../../docs/ARCHITECTURE.md#10-deployment).

## What exists today

- Container images: `infrastructure/docker/backend.Dockerfile` (`TARGET=api|worker`, distroless, non-root) and `frontend-web.Dockerfile` (nginx + SPA fallback).
- `api healthcheck` subcommand for container health checks. `/healthz` is for liveness and `/readyz` for readiness.
- Graceful shutdown on SIGTERM (`SHUTDOWN_TIMEOUT`, default 15s).
- Configuration only through environment variables. In production `.env` files are ignored.

## Planned (Phase 0 → Phase A pilot)

| Item | Choice (pending approval) |
|---|---|
| Region | India (AWS ap-south-1 Mumbai or an Indian provider) |
| Database | Managed PostgreSQL 18 + PostGIS, PITR backups |
| Compute | 1–2 VMs with Docker Compose behind Caddy/Nginx (TLS), later ECS/Kubernetes |
| Migrations | A separate release step (`migrate up`) before rollout, expand → migrate → contract |
| Secrets | Cloud secret manager. Never in git |
| CI/CD | GitHub Actions (backend, Android, web), Codemagic (iOS) |

## Production checklist (to be completed)

- [ ] `APP_ENV=production`, `LOG_FORMAT=json`, `CORS_ALLOWED_ORIGINS` set, `CORS_ALLOW_LOCALHOST=false`
- [ ] `DATABASE_URL` with `sslmode=require` (or `verify-full`)
- [ ] `HTTP_TRUSTED_PROXIES` set to the load balancer ranges
- [ ] Image built with `--build-arg VERSION=<tag> --build-arg COMMIT=<sha>`
- [ ] Log retention ≥ 180 days in India (CERT-In)

### Owner payouts (read before launch)

- No live payout gateway is integrated. The only one simulates transfers, so the worker
  **does not run `payouts.run` / `payouts.reconcile` when real payment keys are set or in
  production**. Settlements are still calculated and become `eligible`; pay owners manually
  (bank transfer) and record it until a live gateway is built and its reconciliation tested.
- With real keys, `PUT /verification/owner/bank-account` answers 503 ("not enabled yet"):
  owners cannot complete the "payout bank account" verification step. Either integrate the
  provider's fund-account validation or add a manual admin verification before onboarding owners.
