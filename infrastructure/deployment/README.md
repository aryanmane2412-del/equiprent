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
- A gateway only pays payout accounts its own provider and mode verified (`simulated`, RazorpayX `test`,
  RazorpayX `live`). After switching from test to live keys, owners must verify their bank account again
  (a fresh production database avoids the problem; otherwise an owner opens "Reopen" on their verification and saves the bank account again); until then
  they appear under `unpayable_owners` in `/admin/payouts/attention`.
- A failed attempt is retried with a new key after a growing wait (30m, 1h, 2h, 4h, then 6h), so an empty
  RazorpayX balance is not hammered every 15 minutes. Top up the balance and the next retry succeeds.
- If a dispute opens while a payout is in flight, the payout still completes when the bank confirms
  (the money has already left); the dispute is then handled against the owner's next settlements.
- Bank verification waits at most ~12 s (the app times out at 20 s). A slow bank is resumed on the
  owner's next try instead of starting a new penny drop. A RazorpayX problem (e.g. no balance for the
  penny drop) is shown to the owner as "temporarily unavailable", never as "your account was refused".

**Go-live checklist (do these in order, do not skip the test-mode run):**

1. Create a RazorpayX account, complete its KYC, and note the **RazorpayX account number**.
2. Set `PAYMENT_KEY_ID` / `PAYMENT_KEY_SECRET` to **`rzp_test_`** keys, `PAYOUT_PROVIDER=razorpayx`,
   `RAZORPAYX_ACCOUNT_NUMBER=<test account number>`.
3. **Run the test-mode checker** (this is the step that tests the real RazorpayX API; the unit tests only
   used a mock). From `backend/`, with the settings from step 2 in `../.env`:

   ```
   go run ./cmd/payoutcheck -name "Account Holder" -account <test account no> -ifsc <IFSC>
   ```

   It refuses live keys, never prints a key or a full account number, and checks: the bank account
   verifies, a payout is accepted, **sending the same idempotency key twice returns the same payout**
   (the guarantee that prevents paying an owner twice), the payout reaches a final status with the right
   amount, and an unaffordable payout is refused as a definite rejection. If any line says FAIL, do not
   go live; send the output to be fixed.
   Then do one real flow by hand: an owner adds a bank account in the app, a booking completes, the
   worker runs, and the payout appears in the RazorpayX dashboard and the settlement turns `paid`.
4. Switch to `rzp_live_` keys and fund the RazorpayX balance. Money collected from customers does not
   move into the RazorpayX balance by itself: set up the transfer (or top up) so payouts do not fail with
   "insufficient balance".
5. Finance can see problems without reading logs. Admin finance/ops roles can call
   `GET /admin/payouts` (all payouts, `?status=` filter) and `GET /admin/payouts/attention`, which lists:
   payouts **stuck** in `processing` for over 2 hours, payouts that **failed or were reversed** in the last
   7 days, **mismatches** between our books and the gateway (including a payout the bank reversed after it
   was marked paid), and **owners who cannot be paid** because they have no verified payout account.
   Also watch the worker logs for `ALERT:` lines. Check `attention` daily at first.

### Resolving a stuck payout (engineer runbook)

Admin access to the platform is read-only by design, so there is no button for this. It is rare: it needs an
unknown submission outcome **and** a changed payout account. `/admin/payouts/attention` lists it under `stuck`
(a `processing` payout with no `gateway_transfer_id` after 2 hours). **First look the payout up in the RazorpayX
dashboard.** Its `reference_id` is the first 32 characters of the SHA-256 of the payout's `idempotency_key`:

```sql
SELECT id, amount_paise, left(encode(sha256(convert_to(idempotency_key, 'UTF8')), 'hex'), 32) AS reference_id
FROM payouts WHERE id = '<payout id>';
```

- **It exists in RazorpayX** (money was sent): record its id and the worker finishes it automatically.
  `UPDATE payouts SET gateway_transfer_id = 'pout_xxx' WHERE id = '<payout id>' AND gateway_transfer_id IS NULL;`
- **It does not exist** (nothing was sent): release the money for a new payout.
  ```sql
  BEGIN;
  UPDATE payouts SET status = 'failed', failure_reason = 'released by <name>: <why>'
   WHERE id = '<payout id>' AND status = 'processing' AND gateway_transfer_id IS NULL;
  UPDATE payout_items SET active = false WHERE payout_id = '<payout id>';
  COMMIT;
  ```
Never release a payout you have not checked in RazorpayX: that could pay the owner twice.

Not built (by choice): RazorpayX webhooks (the worker polls payout status every 2 minutes instead), and
automatic handling of a payout the bank reverses after paying. Paid payouts are re-checked against the
gateway for 14 days, so a reversal is detected and appears under `mismatches` in `/admin/payouts/attention`;
a person decides what to do (re-pay, recover, or adjust the ledger).
