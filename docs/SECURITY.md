# EQUIPRENT — Security and Privacy

Status: Draft v0.1 · 2026-11-12 · Target: OWASP ASVS Level 2, OWASP MASVS-L1 for mobile, DPDP Act 2023 + DPDP Rules 2025, CERT-In directions 2022.

> Compliance notes are engineering guidance, not legal advice. Have counsel review before launch.

## 1. Threat model (top risks)

| # | Threat | Impact | Primary controls |
|---|---|---|---|
| T1 | IDOR: user reads or changes another org's booking, KYC or invoice | Data breach, fraud | Mandatory object-level authz in the service layer. Org-scoped queries. Tests per endpoint |
| T2 | OTP abuse (SMS pumping, brute force) | Cost, account takeover | Rate limits per phone/IP/device, 6-digit OTP, 5 attempts, 5 min TTL, CAPTCHA/Play Integrity after threshold |
| T3 | Payment tampering (client changes amount, fake success callback) | Revenue loss | Server-side pricing. Webhook-authoritative capture. Signature verification. Idempotency |
| T4 | Fake owners / stolen machines listed | Fraud, reputational | KYC (PAN, bank name match, GSTIN), RC owner-name match, admin review, payout hold for new owners |
| T5 | HMR / checklist manipulation | Billing disputes | Photo evidence with device timestamp + location. Server timestamps. Customer approval. Immutable records |
| T6 | Leakage of KYC documents | Legal liability | Private bucket, encryption, short presigned URLs, access logged, minimal retention |
| T7 | Location data misuse (stalking operators) | Privacy harm | Track only during active job states. Role-limited visibility. Retention limit |
| T8 | Admin account compromise | Total compromise | Password + TOTP, IP allow-list option, least-privilege scopes, audit, session timeout |
| T9 | Disintermediation via contact scraping | Business loss | Contact reveal only after confirmation. Rate-limited profile views. Masked numbers (P2: call masking) |
| T10 | Supply chain (malicious package) | Compromise | Pinned deps, `govulncheck`, `dart pub outdated`/OSV scan, Dependabot, minimal base images |

## 2. Authentication

| Aspect | Design |
|---|---|
| Primary login | Phone + OTP via DLT-registered SMS. Android SMS Retriever API for auto-read (no SMS permission needed) |
| OTP storage | `HMAC-SHA256(server_secret, phone‖code)`. Never plain |
| Access token | JWT, **EdDSA (Ed25519)**, 15 min TTL. Claims: `sub`, `sid` (session), `roles`, `iat`, `exp`, `aud`. Key ID `kid` for rotation |
| Refresh token | 256-bit random opaque token, stored **hashed** in `sessions`. 30-day sliding expiry (7 days web). **Rotation on every use. Reuse detection revokes the whole session family** |
| Token storage | Mobile: `flutter_secure_storage` (Android Keystore / iOS Keychain). Web: refresh token in `HttpOnly; Secure; SameSite=Strict` cookie on the API domain, access token kept in memory only |
| Admins | Email + **argon2id** password (m=64MB, t=3, p=2) + **TOTP** mandatory. 8h max session, 30 min idle timeout. Optional IP allow-list. WebAuthn in Phase 3 |
| Step-up | Re-OTP for: changing bank account, changing phone, adding org admin, large refund requests |
| Device binding | `device_id` per install. Unusual new device triggers a notification to the old devices |
| Account recovery | Phone-based. Changing the phone number needs old-phone OTP or a support-verified KYC match |

## 3. Authorisation model (RBAC + organisation scoping)

Authorisation = **role** (what kind of action) × **org membership** (which data) × **resource relationship** (is this your booking).

### 3.1 Permission matrix

| Capability | Customer | Owner (org_admin) | Fleet Mgr | Operator | Admin (scope) |
|---|---|---|---|---|---|
| Search / view listings | ✓ | ✓ | ✓ | ✓ | ✓ |
| Create booking / RFQ | ✓ | ✓ (as customer) | – | – | ops (on behalf, audited) |
| View own org's bookings as customer | ✓ | – | – | – | ops, support |
| Create / edit equipment listings | – | ✓ | ✓ (own org) | – | ops (moderate) |
| Set pricing | – | ✓ | configurable (default ✗) | – | – |
| Accept / reject booking requests | – | ✓ | ✓ | – | ops (force) |
| Assign operators | – | ✓ | ✓ | – | ops |
| View assigned jobs | – | ✓ | ✓ | ✓ (own only) | ops |
| Submit checklists / work logs | – | ✓ (if also operator) | – | ✓ (assigned only) | – |
| Approve work logs | ✓ (customer party) | – | – | – | ops (dispute) |
| Share location | – | – | – | ✓ (active job only) | – |
| View live location | ✓ (own booking, dispatched→returned) | ✓ (own fleet) | ✓ (own fleet) | own | ops |
| Manage org members | org_admin | ✓ | operators only | – | support |
| Bank accounts / payouts view | – | ✓ | ✗ | – | finance |
| Change bank account | – | ✓ + step-up | ✗ | – | – |
| Refunds / payout release | – | – | – | – | finance |
| KYC decisions | – | – | – | – | kyc |
| Commission / policies config | – | – | – | – | super |
| Audit log read | – | – | – | – | super |

### 3.2 Implementation
- Gin middleware resolves `ActorContext{UserID, SessionID, ActiveOrgID, OrgRole, GlobalRoles, AdminScopes}` and verifies membership on every request (cached in Redis 60s, invalidated on membership change).
- **Policy functions per module** (e.g. `booking.CanView(actor, booking)`) are called inside services, not just handlers, so workers and future gRPC paths stay protected.
- Every sqlc query that reads tenant data takes `org_id` as a parameter. Code review rule: no query by `id` alone outside admin/system code.
- Defence in depth (Phase 2): PostgreSQL **Row-Level Security** on the most sensitive tables (`kyc_records`, `bank_accounts`, `invoices`) using `SET LOCAL app.org_id`.
- Admin actions require `admin` role + scope + TOTP-verified session. Write actions require a `reason` and are audited.

## 4. Data protection

### 4.1 Classification
| Class | Examples | Controls |
|---|---|---|
| **Restricted** | PAN, bank account no., DL number, Aadhaar reference, KYC images, TOTP secrets | App-level AES-256-GCM envelope encryption (data keys wrapped by cloud KMS). Private bucket. Access audited. Masked in UI/logs |
| **Confidential** | Phone, email, addresses, location trails, invoices, payment details | TLS everywhere, DB encryption at rest, RBAC, masked in logs |
| **Internal** | Bookings, pricing, ratings | RBAC |
| **Public** | Approved listing info, category taxonomy | – |

### 4.2 Rules
- **Aadhaar:** never store the full Aadhaar number or images unmasked. Use DigiLocker or a licensed KYC provider's offline e-KYC. Store a provider reference + last 4 digits only. No biometric handling.
- **Card data:** never touches our servers (PA hosted checkout / SDK). We're out of PCI DSS scope except SAQ-A-level hygiene.
- **Logs:** structured logging with a redaction middleware (phone → `+91******3210`, tokens, OTPs, PAN removed). No request bodies on auth/KYC/payment endpoints.
- **Backups:** encrypted, India region, restore tested quarterly.
- **Secrets:** cloud secret manager or SOPS-encrypted files. Never in git. Rotated on staff exit. Separate keys per environment.
- **Media:** EXIF stripped from public listing photos. Checklist photo metadata (time, GPS) extracted into DB before stripping.

## 5. Privacy (DPDP Act 2023 + DPDP Rules 2025)

| Obligation | Implementation |
|---|---|
| Notice and consent | Plain-language notice in the user's language at signup. Itemised purposes. Consent stored in `consents` with notice version. Separate opt-in for marketing |
| Legitimate uses | Booking execution, invoicing, legal compliance (tax), fraud prevention documented as purposes |
| Purpose limitation / minimisation | Collect KYC only when the user's role needs it. Location only during active job |
| Data principal rights | In-app Privacy Centre: access/export, correction, erasure request, consent withdrawal, nominate. SLA tracked |
| Retention | Delete when purpose is served unless law requires retention (tax records 8 yrs). Retention jobs per [DATABASE_DESIGN §13](DATABASE_DESIGN.md#13-data-retention) |
| Breach notification | Notify the Data Protection Board and affected users per Rules timelines. CERT-In within 6 hours. Runbook in `ops/incident-response.md` |
| Children | Service is for adults. Age declaration at signup. No processing of children's data |
| Processors | DPAs with SMS, maps, KYC, payment, cloud vendors. Vendor register |
| Grievance | Named Grievance Officer + contact in app and website. Ticketed with timelines (also needed under IT Rules 2021 and E-Commerce Rules) |

**Operator location privacy:** tracking runs as an Android foreground service with a persistent notification "Sharing location for job ER-xxxx". It stops automatically when the job is no longer active. Operators can see what is shared. Customers see the location only from dispatch until return.

## 6. Application security controls

| Area | Control |
|---|---|
| Transport | TLS 1.2+ (1.3 preferred), HSTS (preload after stabilisation). Mobile: certificate pinning via public-key hashes with a backup pin (Phase 2, carefully, to avoid bricking) |
| Input validation | Gin binding + validator. Strict JSON (unknown fields rejected on write endpoints). Size limits |
| SQL injection | sqlc parameterised queries only. No string-built SQL |
| XSS | SSR pages use `html/template` auto-escaping. CSP with nonces. Flutter web: no raw HTML rendering of user content |
| CSRF | Web uses a bearer access token + SameSite=Strict refresh cookie. Refresh endpoint also requires a custom header |
| CORS | Allow-list of EQUIPRENT web origins only |
| Rate limiting | Redis sliding window: OTP request 3/10 min/phone, 10/hr/IP. Login verify 5 attempts. Search 60/min/user. Writes 30/min/user. Webhooks exempt but signature-checked |
| Uploads | Presigned PUT with `content-length-range` and content-type conditions. Magic-byte sniffing. Image re-encode. Max 10 MB image, 15 MB PDF. Malware scan (ClamAV) on documents |
| SSRF | No user-supplied URLs fetched server-side. Outbound allow-list for third-party APIs |
| Business logic | Server recomputes prices. State machine guards. Optimistic locking. Idempotency keys. Exclusion constraints |
| Webhooks | HMAC verification, timestamp tolerance, replay dedupe by event ID |
| Mobile | Play Integrity / App Attest signals on OTP + payment endpoints (Phase 2). Obfuscation (`--obfuscate --split-debug-info`). No secrets in the app. Root/jailbreak = warn, don't block (low-end Android reality) |
| Dependencies | `govulncheck`, `gosec`, `golangci-lint`, OSV-Scanner for Dart, Dependabot. Container image scanning (Trivy). Distroless/alpine base, non-root user |
| Headers (web/SSR) | CSP, `X-Content-Type-Options`, `Referrer-Policy: strict-origin-when-cross-origin`, `Permissions-Policy`, `frame-ancestors 'none'` |

## 7. Fraud and trust controls

- **New owner payout hold:** first 3 payouts held 7 days. Bank account name must match KYC name (fuzzy) or GSTIN legal name.
- **Velocity checks:** many bookings from a new account, mismatched site locations, repeated cancellations.
- **Machine identity:** RC registration owner name vs. org/KYC name. Duplicate registration number across owners gets blocked and flagged.
- **Review integrity:** only completed-booking parties can review. One review per party per booking.
- **Contact reveal** only after confirmation. Phone numbers masked in chat until then.

## 8. Audit and monitoring

- `audit_logs` (append-only; DB role cannot UPDATE/DELETE) for: auth events, role changes, KYC decisions, listing moderation, booking transitions, payment/refund/payout actions, admin views of restricted data, config changes.
- Security alerts: spikes in OTP requests, failed admin logins, webhook signature failures, mass data access by one user, payout bank changes.
- Logs kept **180 days in India** (CERT-In). NTP time sync on all hosts.

## 9. Incident response (summary)

1. Detect → triage severity (S1 data breach / payment loss, S2 outage, S3 minor).
2. Contain: revoke keys/sessions, block IPs, disable feature flag.
3. Report: **CERT-In within 6 hours** for reportable incidents. DPDP Board + affected users for personal data breaches, per Rules.
4. Eradicate, recover, post-mortem within 5 working days.

## 10. Security roadmap

| Phase | Items |
|---|---|
| MVP | Everything in §2–§6 marked without a phase. Pre-launch external pentest (web + API + Android) |
| Phase 2 | RLS on sensitive tables, Play Integrity/App Attest, certificate pinning, call masking, WAF |
| Phase 3 | WebAuthn for admins, SIEM, bug bounty, ISO 27001 readiness if enterprise customers require it |
