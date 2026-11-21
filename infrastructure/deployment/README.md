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

### Owner payouts (RazorpayX)

How owners are paid: when an owner saves a bank account, RazorpayX runs a **penny-drop validation**
(it confirms the account exists, is active, and that the holder name matches) and only then is the
account stored as the payout account. The worker then sends one **IMPS payout per owner** for their
eligible settlements and finishes it only when the bank confirms.

Safety rules built in (all covered by `backend/tests/payout_live_test.go`):

- A payout is marked **paid only when RazorpayX reports `processed` and the amount matches ours**;
  then the settlements are paid and the ledger entry is posted exactly once.
- Every payout carries an **idempotency key**. If a request times out (outcome unknown) it is **not**
  freed or duplicated: it is re-sent with the same key and RazorpayX returns the original payout.
- A definite refusal (e.g. not enough balance) or a bank failure **frees the settlements** for a retry
  with a new key; nothing is posted to the ledger.
- A new or changed payout account is held back for `PAYOUT_BANK_COOLDOWN` (default 24h) so a hijacked
  account cannot redirect money. A settlement above `PAYOUT_MAX_PAISE` waits for a person.
- The simulator never runs with real keys or in production.

**Go-live checklist (do these in order, do not skip the test-mode run):**

1. Create a RazorpayX account, complete its KYC, and note the **RazorpayX account number**.
2. Set `PAYMENT_KEY_ID` / `PAYMENT_KEY_SECRET` to **`rzp_test_`** keys, `PAYOUT_PROVIDER=razorpayx`,
   `RAZORPAYX_ACCOUNT_NUMBER=<test account number>`.
3. In test mode: add a bank account as an owner, check it verifies; complete a booking, let the worker
   run, and confirm in the RazorpayX dashboard that the payout appears and the settlement turns `paid`.
   Also test a failing account and an insufficient balance. **This integration was tested against a mock
   of the RazorpayX API, not the real service, so this step is required.**
4. Switch to `rzp_live_` keys and fund the RazorpayX balance. Money collected from customers does not
   move into the RazorpayX balance by itself: set up the transfer (or top up) so payouts do not fail with
   "insufficient balance".
5. Watch the worker logs for `ALERT:` lines (rejected payouts, amount mismatches, over-cap settlements)
   and the reconciliation mismatches that finance reviews.

Not yet covered: RazorpayX webhooks (the worker polls payout status every 2 minutes instead), and
`reversed` payouts after a bank return (they show up as a reconciliation mismatch for a person to resolve).
