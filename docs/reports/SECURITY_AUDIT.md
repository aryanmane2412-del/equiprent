# EQUIPRENT Security Audit

## Scope

Static repository review on 2026-11-17. No penetration test, dependency vulnerability scan, production configuration review, live payment test, or database privilege review was performed. This is not a security certification.

## Controls found in code/documentation

- Go backend uses authenticated Gin routes, role/organization checks, resource ownership checks, request validation, and audit logging across business modules.
- Auth documentation describes Argon2id passwords, short-lived JWTs, rotating refresh tokens, lockout, request IDs, and throttled public auth routes.
- Uploads are validated by content and stored privately with purpose-specific claims and short-lived signed links.
- Location sessions require consent and are restricted to booking sessions; access is audited. Production configuration rejects simulated tracking.
- Payments verify provider signatures and maintain idempotent ledger/payment records; sandbox mode is development-only.
- SQL uses pgx/sqlc parameterized queries in the reviewed modules. Database constraints cover key booking overlap and state rules.
- Admin mutations generally require reasons and write audit records.

## Risks and production blockers

1. **Live integrations remain unproven.** Backend docs state Razorpay and FCM have fake-server tests but no live provider validation. Razorpay checkout SDK is not integrated in Flutter; live checkout is unavailable. SMS/email senders are simulation/disabled, so production OTP and email verification delivery need a real provider.
2. **Deployment is not configured.** `infrastructure/deployment/README.md` says deployment is not set up. TLS termination, secret manager, production alerting, backup/restore, and CI/CD are not evidenced as deployed.
3. **Redis is optional and rate limiting fails open.** Single-process fallback is unsuitable for multi-replica production; require shared Redis and review behavior during Redis outage.
4. **WebSocket live tracking is in-process.** Multiple API replicas require shared pub/sub or sticky/session architecture; current docs identify Redis pub/sub as future work.
5. **Database grants and row-level security were not verified.** Review actual managed DB roles, migration/runtime privilege separation, backup encryption, TLS verification, and audit retention before launch.
6. **Secrets:** `.env` exists locally. Its contents were not printed or included here. Keep it out of version control and rotate any credentials if exposure is suspected. Use a secret manager in deployed environments.
7. **Privacy:** confirm retention/deletion policies for tracking, uploaded evidence, KYC data, chat, and audit logs; test signed-link expiry and access revocation.
8. **Feature gaps:** support-ticket APIs/screens are not present in the inspected module list; admin analytics contains mock/hardcoded data. Treat those areas as incomplete, not production-ready.

## Pre-production validation

- Run automated tests and static analysis; run dependency scanning and secret scanning in CI.
- Test authorization across all roles and tenant boundaries, including object IDs belonging to another owner.
- Run SQL injection, upload MIME/content, malware, size-limit, signed URL, JWT rotation/reuse, webhook replay/signature, payment idempotency, and rate-limit tests.
- Verify database grants with a runtime user that cannot perform DDL; test encrypted backups and restore drills.
- Complete privacy review, incident response contacts, log redaction, monitoring, and retention configuration.
