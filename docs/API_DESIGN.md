# EQUIPRENT — API Design

Status: Draft v0.1 · 2026-11-12. The contract source of truth will be `backend/api/openapi.yaml` (OpenAPI 3.1). The Dart client is generated from it.

## 1. Conventions

| Topic | Rule |
|---|---|
| Base URL | `https://api.equiprent.in/api/v1` (staging: `api.staging.equiprent.in`) |
| Format | JSON, `snake_case` fields, UTF-8 |
| IDs | UUIDv7 strings. Bookings also have human `reference` |
| Money | Integer **paise** + `currency` (`"INR"`). Clients format as ₹ |
| Time | RFC 3339 UTC (`2026-11-12T04:30:00Z`). Clients display IST |
| Geo | `{ "lat": 18.5204, "lng": 73.8567 }` |
| Auth | `Authorization: Bearer <access_jwt>` |
| Acting context | `X-Org-Id: <uuid>` + `X-Role: owner` when a user belongs to several orgs/roles. The server verifies membership |
| Idempotency | `Idempotency-Key: <uuid>` **required** on all POSTs that create bookings, payments, refunds, payouts, checklists, work logs |
| Concurrency | `If-Match: "<version>"` on PATCH of bookings/equipment. `412` on mismatch |
| Pagination | Cursor based: `?limit=20&cursor=<opaque>`, response `{ "data": [...], "next_cursor": "..." }` |
| Filtering / sort | `?status=active,paused&sort=-created_at` |
| Localisation | `Accept-Language: hi` returns localised category names and messages |
| Versioning | URI major version. Additive changes only within v1. Deprecations use the `Sunset` header |
| Rate limits | `429` with `Retry-After`. Headers `RateLimit-Limit`, `RateLimit-Remaining` |
| Client info | `X-App-Version`, `X-Platform` (android/ios/web). The server can return `426` to force an update |

### Error envelope (RFC 9457 problem+json style)
```json
{
  "type": "https://docs.equiprent.in/errors/booking-overlap",
  "title": "Equipment not available for the selected dates",
  "status": 409,
  "code": "BOOKING_OVERLAP",
  "detail": "This machine is already booked between 14 Nov and 16 Nov.",
  "request_id": "01JD3...",
  "errors": [{ "field": "period.start", "code": "in_past" }]
}
```
Standard codes: `400 VALIDATION_FAILED`, `401 UNAUTHENTICATED`, `403 FORBIDDEN`, `404 NOT_FOUND`, `409 CONFLICT / INVALID_STATE_TRANSITION / BOOKING_OVERLAP`, `412 VERSION_MISMATCH`, `422 KYC_REQUIRED`, `429 RATE_LIMITED`, `5xx INTERNAL`.

## 2. Endpoint catalogue

Legend for access: **C** customer, **O** owner, **OP** operator, **F** fleet manager, **A** admin (scope), **P** public.

### 2.1 Auth and session
| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/auth/otp/request` | P | `{phone, purpose: login}` sends OTP. Rate-limited per phone + IP + device |
| POST | `/auth/otp/verify` | P | `{phone, code, device_id}` returns `{access_token, refresh_token, user, is_new}` |
| POST | `/auth/refresh` | P | Rotate refresh token. Reuse detection revokes the session family |
| POST | `/auth/logout` | any | Revoke current session |
| GET | `/auth/sessions` · DELETE `/auth/sessions/{id}` | any | Manage devices |
| POST | `/auth/admin/login` | P | Email + password, then `/auth/admin/totp` |
| POST | `/ws/ticket` | any | Short-lived (60s) WebSocket ticket |

### 2.2 Me, roles, organisations
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET/PATCH | `/me` | any | Profile, language |
| POST | `/me/roles` | any | Enable a role (`owner`, `operator`) to start that onboarding |
| GET | `/me/orgs` | any | Orgs + member roles |
| POST | `/orgs` | any | Create company org |
| GET/PATCH | `/orgs/{org_id}` | org_admin | |
| GET/POST | `/orgs/{org_id}/members` | org_admin, F | Invite by phone with role |
| PATCH/DELETE | `/orgs/{org_id}/members/{user_id}` | org_admin | |
| GET/POST | `/orgs/{org_id}/addresses` | members | Saved sites / base yards |
| POST/DELETE | `/me/devices` | any | Register FCM token |
| GET/POST | `/me/consents` | any | DPDP consents |
| POST | `/me/data-export` · `/me/deletion-request` | any | Data principal rights |

### 2.3 KYC
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/orgs/{org_id}/kyc` | org_admin | Status per document |
| POST | `/orgs/{org_id}/kyc/pan` | org_admin | Verify PAN via API |
| POST | `/orgs/{org_id}/kyc/gstin` | org_admin | Fetch + verify GSTIN (auto-fills legal name, address, state) |
| POST | `/orgs/{org_id}/kyc/digilocker/start` · `/callback` | org_admin | Aadhaar/DL via DigiLocker consent flow |
| POST | `/orgs/{org_id}/bank-accounts` | org_admin | Penny-drop verify, create PA linked account |
| POST | `/operators/me/profile` | OP | Licence + categories |

### 2.4 Uploads
| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/uploads` | any | `{purpose, content_type, size_bytes}` returns `{upload_id, put_url, headers, expires_at}` |
| POST | `/uploads/{id}/complete` | any | Confirms object exists, triggers processing |

### 2.5 Catalogue (public read)
| Method | Path | Access |
|---|---|---|
| GET | `/categories` (tree, localised) | P |
| GET | `/categories/{slug}/models?make=` | P |
| GET | `/makes` | P |
| GET | `/cities` | P |

### 2.6 Owner listings
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET/POST | `/owner/equipment` | O, F | List / create draft |
| GET/PATCH/DELETE | `/owner/equipment/{id}` | O, F | Edit (If-Match), retire |
| POST | `/owner/equipment/{id}/submit` | O | Draft → pending_review |
| POST | `/owner/equipment/{id}/pause` · `/resume` | O, F | |
| PUT | `/owner/equipment/{id}/media` | O, F | Ordered list of upload_ids |
| POST | `/owner/equipment/{id}/documents` | O, F | RC / insurance / fitness with expiry |
| PUT | `/owner/equipment/{id}/pricing` | O | Pricing plans + mobilisation rates |
| GET/POST/DELETE | `/owner/equipment/{id}/blocks` | O, F | Availability blocks |
| GET | `/owner/equipment/{id}/calendar?from&to` | O, F | Bookings + blocks |
| POST | `/owner/equipment/import` | O | CSV bulk (P2) |

### 2.7 Search and listing detail
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/search/equipment` | P | `category, lat, lng, radius_km, start, end, unit, with_operator, min_capacity, make, price_max, verified_only, sort (distance/price/rating), cursor` |
| GET | `/equipment/{id}` | P | Public detail (owner contact hidden until booking confirmed) |
| GET | `/equipment/{id}/availability?from&to` | P | Busy ranges only |
| POST | `/equipment/{id}/estimate` | P | `{site, period, unit, with_operator, expected_hours}` returns full price breakdown incl. mobilisation, bata, fees, GST |
| GET | `/equipment/{id}/reviews` | P | |

### 2.8 RFQ and quotes
| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/rfqs` | C | Broadcast request |
| GET | `/rfqs` · `/rfqs/{id}` | C | With received quotes |
| POST | `/rfqs/{id}/cancel` | C | |
| GET | `/owner/rfqs` | O | Matching open RFQs nearby |
| POST | `/owner/rfqs/{id}/quotes` | O | Offer with equipment_id + price |
| POST | `/quotes/{id}/accept` · `/reject` · `/counter` | C / O | Accept creates booking |

### 2.9 Bookings (customer side)
| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/bookings` | C (KYC basic) | Direct request on a listing |
| GET | `/bookings?status=&role=customer` | C | |
| GET | `/bookings/{id}` | parties, A | Detail with timeline, price, assignments |
| POST | `/bookings/{id}/cancel` | C, O | Returns fee per policy. Preview with `?dry_run=true` |
| POST | `/bookings/{id}/handover/confirm` | C | Customer confirms handover with OTP shown on operator device, or the operator enters the customer's OTP |
| GET | `/bookings/{id}/work-logs` | parties | |
| POST | `/bookings/{id}/work-logs/{log_id}/approve` · `/dispute` | C | |
| POST | `/bookings/{id}/extensions` | C | Request extension |
| POST | `/bookings/{id}/return-request` | C | Release machine early / on time |
| POST | `/bookings/{id}/breakdowns` | C, OP | |
| POST | `/bookings/{id}/reviews` | C, O | After completion |
| POST | `/bookings/{id}/disputes` | C, O | |
| GET | `/bookings/{id}/invoices` | parties | Presigned PDF links |
| GET | `/bookings/{id}/track` | C, O, F | Current position + ETA (REST snapshot, then WS) |

### 2.10 Bookings (owner / fleet side)
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/owner/bookings?status=` | O, F | Inbox |
| POST | `/owner/bookings/{id}/accept` · `/reject` · `/counter` | O | Within response SLA |
| POST | `/owner/bookings/{id}/assign-operator` | O, F | `{operator_user_id}` |
| POST | `/owner/bookings/{id}/dispatch` | O, F, OP | Starts mobilisation trip |
| POST | `/owner/bookings/{id}/extensions/{ext_id}/approve` · `/reject` | O | |
| POST | `/owner/bookings/{id}/inspection` | O | Post-return inspection result, damage claim |
| POST | `/owner/bookings/{id}/offline-payments` | O | Record cash/NEFT received with proof |

### 2.11 Operator
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/operator/jobs?status=` | OP | Assigned jobs |
| POST | `/operator/jobs/{booking_id}/accept` · `/decline` | OP | |
| POST | `/operator/jobs/{booking_id}/arrived` | OP | Manual arrival (geofence fallback) |
| POST | `/operator/jobs/{booking_id}/checklists` | OP | `kind=handover/return`, HMR, photos (upload_ids), fuel, items, `client_id` |
| POST | `/operator/jobs/{booking_id}/work-logs` | OP | Daily HMR log |
| POST | `/operator/jobs/{booking_id}/tracking/batch` | OP | Offline catch-up of location points |
| POST | `/operator/availability` | OP | On/off duty |

### 2.12 Payments
| Method | Path | Access | Purpose |
|---|---|---|---|
| POST | `/bookings/{id}/payments` | C | `{purpose: advance/deposit/balance}`. Creates PA order, returns checkout payload |
| POST | `/payments/{id}/verify` | C | Client callback with signature. Server verifies, but **webhook is authoritative** |
| GET | `/bookings/{id}/payments` | parties | |
| POST | `/webhooks/payments/{provider}` | P (signature-verified) | Payment, refund, transfer and settlement events. Stored in `webhook_events` and processed idempotently |
| GET | `/owner/settlements` · `/owner/payouts` | O | Earnings, deductions, TDS/TCS |
| GET | `/owner/statements?month=` | O | Downloadable statement |

### 2.13 Fleet
| Method | Path | Access | Purpose |
|---|---|---|---|
| GET | `/fleet/{org_id}/overview` | F, org_admin | KPIs: utilisation, active jobs, idle machines, revenue |
| GET | `/fleet/{org_id}/live` | F | Last positions (then WS `fleet:{org_id}`) |
| GET | `/fleet/{org_id}/operators` | F | Roster + status |
| GET/POST | `/fleet/{org_id}/equipment/{id}/maintenance` | F | Plans + records |
| GET | `/fleet/{org_id}/reports/utilisation?from&to` | F | CSV/JSON |

### 2.14 Notifications
`GET /notifications`, `POST /notifications/{id}/read`, `POST /notifications/read-all`, `GET/PUT /me/notification-preferences`.

### 2.15 Admin (`/admin/*`, admin role + scope + TOTP session)
| Path group | Scope | Capabilities |
|---|---|---|
| `/admin/kyc` | kyc | Queue, approve/reject with reason |
| `/admin/listings` | ops | Moderate, suspend |
| `/admin/bookings` | ops | Search, view, force transitions (reason required, audited) |
| `/admin/disputes` | ops, finance | Assign, message, resolve with financial outcome |
| `/admin/payments`, `/admin/refunds` | finance | Manual refunds, reconciliation reports |
| `/admin/payouts` | finance | Batches, hold/release, retry |
| `/admin/config/commission-rules`, `/cancellation-policies`, `/cities`, `/categories` | super, ops | |
| `/admin/users`, `/admin/orgs` | support, super | Suspend, role grants |
| `/admin/audit-logs` | super | Read-only |
| `/admin/reports/*` | finance, super | GMV, take rate, GSTR-8 / TCS, TDS-194O exports |

### 2.16 Public SSR pages (non-API, served as HTML)
`/`, `/{city}/{category}-on-rent`, `/equipment/{slug}-{shortid}`, `/owners` (list your machine), `/legal/*`, `/grievance`, `sitemap.xml`, `robots.txt`.

## 3. Example: create booking

```http
POST /api/v1/bookings
Authorization: Bearer eyJ...
X-Org-Id: 0193f6c2-...
Idempotency-Key: 4d0e5c1a-7b3e-4f7a-9c8e-2f1b3a6d9e00
Content-Type: application/json

{
  "equipment_id": "0193f6d1-...",
  "site_address_id": "0193f6c9-...",
  "period": { "start": "2026-11-20T02:30:00Z", "end": "2026-11-23T12:30:00Z" },
  "pricing_unit": "shift",
  "with_operator": true,
  "expected_hours": 24,
  "work_description": "Foundation excavation, 3 days",
  "payment_mode": "online"
}
```

```json
201 Created
{
  "id": "0193f6e0-...",
  "reference": "ER-2611-4X7K",
  "status": "requested",
  "owner_response_due_at": "2026-11-12T08:15:00Z",
  "price": {
    "currency": "INR",
    "rental_paise": 2640000,
    "mobilisation_paise": 450000,
    "operator_bata_paise": 150000,
    "platform_fee_paise": 52800,
    "gst_paise": 592704,
    "estimated_total_paise": 3885504,
    "deposit_paise": 1000000,
    "advance_due_paise": 1942752,
    "due_on_confirmation_paise": 2942752
  }
}
```

How the numbers are built: GST 18% on (rental ₹26,400 + mobilisation ₹4,500 + bata ₹1,500) = ₹5,832, plus 18% on the ₹528 fee = ₹95.04. The estimated total excludes the refundable deposit. The advance is 50% of the estimate, so ₹19,427.52 + ₹10,000 deposit is due on confirmation. The advance % is configurable.

## 4. WebSocket protocol

Connect to `wss://api.equiprent.in/ws` with subprotocol `equiprent.v1, ticket.<ticket>`.

```jsonc
// client → server
{ "op": "sub",   "topic": "booking:0193f6e0-..." }
{ "op": "unsub", "topic": "booking:0193f6e0-..." }
{ "op": "loc",   "booking_id": "...", "lat": 18.52, "lng": 73.85, "acc": 8, "spd": 6.1, "hdg": 270, "ts": "2026-11-20T03:01:12Z" }  // operator only
{ "op": "ping" }

// server → client
{ "op": "ack",   "ref": "sub:booking:..." }
{ "op": "event", "topic": "booking:...", "type": "location",       "data": { "lat": 18.52, "lng": 73.85, "eta_s": 1260 } }
{ "op": "event", "topic": "booking:...", "type": "status_changed", "data": { "from": "dispatched", "to": "on_site" } }
{ "op": "event", "topic": "user:...",    "type": "notification",   "data": { ... } }
{ "op": "error", "code": "FORBIDDEN_TOPIC" }
```

## 5. Webhook handling (payment provider)

1. Verify HMAC signature with the provider secret, using constant-time compare. Invalid → `400`, logged.
2. `INSERT INTO webhook_events ... ON CONFLICT (provider, event_id) DO NOTHING`. A duplicate gets `200` immediately.
3. Enqueue `payment.process_webhook` (asynq), return `200` within 2s.
4. The worker applies the state change + ledger entries in one DB transaction, then marks the event processed.
5. Daily reconciliation pulls provider settlement reports and flags mismatches.

## 6. API security checklist (summary, details in SECURITY.md)

- Object-level authorisation on every `{id}`: the booking must belong to the caller's org as customer/owner, or the caller must be the assigned operator.
- Never expose owner/customer phone numbers until booking is `confirmed`. Mask after `closed`.
- Server-side recomputation of all prices. Client totals are ignored.
- Payloads capped (1 MB JSON). Uploads go only through presigned URLs with size and content-type conditions.
