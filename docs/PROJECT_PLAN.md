# EQUIPRENT — Project Plan

> **Rent Smart. Build Better.**
> Heavy equipment rental marketplace for India.

| | |
|---|---|
| Status | Draft v0.1, waiting for founder approval |
| Date | 2026-11-12 |
| Companion docs | [ARCHITECTURE](ARCHITECTURE.md) · [DATABASE_DESIGN](DATABASE_DESIGN.md) · [API_DESIGN](API_DESIGN.md) · [USER_FLOWS](USER_FLOWS.md) · [SECURITY](SECURITY.md) · [DEVELOPMENT_ROADMAP](DEVELOPMENT_ROADMAP.md) |

---

## 1. Problem statement

Indian construction runs on rented machinery: backhoe loaders, excavators, cranes, rollers, tippers, transit mixers, piling rigs, telehandlers and compactors. Today most of this rental happens through:

- Phone calls and WhatsApp groups to local brokers ("JCB wala").
- Owners with 1 to 5 machines who have idle time but no sales channel.
- Larger fleets that track utilisation, operators and hour meters (HMR) in Excel.
- Prices nobody can compare, disputed hour-meter readings, late payments and no paper trail for GST or TDS.

**EQUIPRENT** is a digital marketplace that makes this transaction discoverable, verifiable, trackable and payable.

## 2. Business objectives

| # | Objective | Success metric (12 months after launch) |
|---|---|---|
| O1 | Give customers fast access to verified machinery near their site | Median time from request to confirmed booking under 2 hours |
| O2 | Increase owners' utilisation and revenue | Listed-machine utilisation up 15% for active owners |
| O3 | Remove billing disputes through digital HMR, photos and checklists | Disputes under 3% of completed bookings |
| O4 | Make payments safe for both sides | 100% of online bookings go through escrow-style split settlement |
| O5 | Build a sustainable take rate | Contribution-positive per booking in the pilot city |

## 3. Market research: how established rental marketplaces work

This is desk research from general industry knowledge, not a study of anyone's proprietary designs. **Verify current figures before using them in a pitch.** Four business archetypes exist:

| Archetype | How it works | Examples of the model (global) | What we take from it |
|---|---|---|---|
| **Lead-generation directory** | Owners list, customers call, and the platform earns from subscriptions or paid leads. It never touches the transaction | B2B classified portals | Cheap to start, but there is no trust layer, no data and no transaction revenue. We **avoid** this as the end state but can use it as a cold-start fallback |
| **Peer-to-peer managed marketplace** | Owners list. The platform handles booking, payment, insurance and disputes, and takes a commission | DOZR-style and Yard Club-style models | **Our core model** |
| **Aggregator / broker with price guarantee** | Customer requests a category and the platform sources from a vendor network, sets one price and owns the customer relationship | BigRentz-style model | Use for **"Request for Quote"** when no instant listing matches |
| **Own fleet + technology** | The company owns the machines and builds telematics and software around them | EquipmentShare-style model | Asset-heavy, so not for us. Copy the **telematics, utilisation analytics and fleet tooling** for our Fleet Manager role |

**Marketplace workflows worth adopting:**

1. Two booking modes: **Instant Book** for trusted owners with live availability, and **Request → Quote → Accept** for everyone else. Indian customers expect to negotiate.
2. **Category-first search** ("I need a 20-ton excavator in Hinjewadi for 3 days"), not model-first.
3. **Wet vs. dry hire.** Wet means with operator and is the Indian default. Dry means machine only and is rare, restricted to verified companies.
4. **Hour-meter (HMR) based billing** with a minimum shift. Monthly hire is usually ~200–260 hours, and extra hours are pro-rated.
5. **Mobilisation and demobilisation charges** for moving machines by trailer, quoted separately.
6. **Fuel borne by customer** by default, with **operator bata** (daily allowance) and food/stay terms stated explicitly.
7. **Digital handover and return checklists** with timestamped, geotagged photos, HMR readings and OTP confirmation.
8. **Breakdown policy**: downtime hours are deducted, and a replacement machine is offered.
9. **Ratings in both directions** (customer ↔ owner/operator).
10. **Owner payouts on a schedule** after completion, net of commission and taxes.

## 4. Business model

| Revenue stream | Phase | Notes |
|---|---|---|
| **Commission (take rate)** on rental value | MVP | Start at **8–12%** paid by the owner, plus a **2–3% customer convenience fee**. Configurable per category and city |
| Featured / boosted listings | Phase 2 | Owner pays for ranking within a radius |
| Owner subscription (Fleet plan) | Phase 3 | Fleet dashboard, telematics, multi-user and analytics for 10+ machine owners |
| Logistics fee | Phase 2 | Margin on arranging trailer mobilisation |
| Damage protection / insurance add-on | Phase 3 | Through an IRDAI-licensed partner as corporate agent or referral. Never underwrite ourselves |
| Financing / credit (Buy-now-pay-later for B2B) | Phase 4 | Through NBFC partners. Big lever for contractors with 30–60 day payment cycles |
| Used equipment / spare parts leads | Phase 4 | Natural extension |

**Unit economics example:** A 3-day backhoe hire at ₹1,100/hr × 8 hrs × 3 days = ₹26,400 rental. A 10% owner commission is ₹2,640 and a 2% customer fee is ₹528, so platform revenue is ₹3,168 before payment-gateway cost (~₹550) and GST.

## 5. Roles

| Role | Who | Core jobs |
|---|---|---|
| **Customer** | Contractors, construction companies, builders, farmers, individuals | Find, compare, book, pay, track, approve HMR, rate, download GST invoices |
| **Equipment Owner** | Individual owners or companies (sole proprietor, partnership, Pvt Ltd) | List machines, set prices and availability, accept bookings, assign operators, get paid |
| **Operator / Driver** | Licensed operator employed by or contracted to an owner | See assigned jobs, navigate to site, do handover checklist, log HMR, share location, report breakdowns |
| **Fleet Manager** | Staff of a *multi-machine owner organisation* | Manage the fleet, operators, maintenance, utilisation and live map for their organisation |
| **Platform Administrator** | EQUIPRENT staff, split into sub-roles (Super Admin, Ops, KYC Verifier, Finance, Support) | Verify KYC, moderate listings, resolve disputes, manage payouts, configure pricing and commissions |

**Assumption (needs approval):** the Fleet Manager belongs to the **owner side**. Customer-side organisations such as large contractors get **Org Admin / Site Engineer** sub-roles inside the Customer role in Phase 2. One person can hold several roles. For example, a small owner can also be their own operator, or a contractor can both rent and rent out.

A permission matrix is in [SECURITY.md §3](SECURITY.md#3-authorisation-model-rbac--organisation-scoping).

## 6. Functional requirements

Priority: **M** = MVP, **P2** = Phase 2, **P3+** = later.

### 6.1 Identity and onboarding
| ID | Requirement | Pri |
|---|---|---|
| F-ID-1 | Sign up / log in with mobile number + OTP. Email optional | M |
| F-ID-2 | One account can hold multiple roles, switched from the profile | M |
| F-ID-3 | Create organisations (company/firm) and invite members by phone | M |
| F-ID-4 | KYC: PAN, GSTIN (optional for individuals), Aadhaar via DigiLocker / offline e-KYC (masked), bank account verification by penny drop | M |
| F-ID-5 | Operator onboarding: driving licence (HMV/CEV), experience, machine categories, photo | M |
| F-ID-6 | Admin login with password + TOTP 2FA | M |
| F-ID-7 | Language selection: English and Hindi at MVP, then Marathi, Tamil, Telugu, Kannada, Gujarati, Bengali | M / P2 |

### 6.2 Equipment catalogue and listings
| ID | Requirement | Pri |
|---|---|---|
| F-EQ-1 | Master taxonomy: category → sub-category → make → model with standard specs (operating weight, bucket capacity, lift capacity, boom length, HP) | M |
| F-EQ-2 | Owner creates a listing: model, year, registration no., HMR, photos (min 4), base location, service radius | M |
| F-EQ-3 | Upload documents: RC, insurance, fitness/PUC (road-going), invoice/ownership proof. Expiry tracking with reminders | M |
| F-EQ-4 | Pricing plans: hourly (min hours), shift/day (8 or 10 hr), weekly, monthly (with included hours), extra hour rate, mobilisation charge per km or fixed, operator bata | M |
| F-EQ-5 | Wet / dry hire flags. Fuel policy | M |
| F-EQ-6 | Availability calendar, blocked dates, maintenance blocks | M |
| F-EQ-7 | Listing moderation by admin before going live | M |
| F-EQ-8 | Instant Book toggle (only for verified owners above a rating threshold) | P2 |
| F-EQ-9 | Bulk listing via CSV for fleets | P2 |

### 6.3 Discovery
| ID | Requirement | Pri |
|---|---|---|
| F-SR-1 | Search by category + site location + date range + duration | M |
| F-SR-2 | Geo radius search (PostGIS) with distance and estimated mobilisation charge | M |
| F-SR-3 | Filters: capacity, make, year, price, with/without operator, rating, verified-only | M |
| F-SR-4 | Map view and list view | M |
| F-SR-5 | Request for Quote: broadcast to matching owners within radius, compare quotes | M |
| F-SR-6 | Saved sites, favourites, recently viewed | P2 |
| F-SR-7 | SEO-indexed public pages ("Excavator on rent in Pune") | P2 |

### 6.4 Booking
| ID | Requirement | Pri |
|---|---|---|
| F-BK-1 | Booking request with site address + pin, dates, shift, expected hours, work description | M |
| F-BK-2 | Owner accept / reject / counter-offer within an SLA (e.g. 2 hrs). Auto-expire | M |
| F-BK-3 | Price breakdown: rental, mobilisation, operator bata, platform fee, GST, deposit | M |
| F-BK-4 | Double-booking prevention at database level | M |
| F-BK-5 | Operator assignment by owner / fleet manager | M |
| F-BK-6 | Dispatch → on-site handover with checklist, photos, start HMR and customer OTP | M |
| F-BK-7 | Daily HMR / work log entered by operator and approved by customer (auto-approved after 24h) | M |
| F-BK-8 | Extension requests and early termination | M |
| F-BK-9 | Breakdown reporting, downtime deduction and replacement request | M |
| F-BK-10 | Return checklist, end HMR, damage report | M |
| F-BK-11 | Cancellation policy engine (time-based fees) | M |
| F-BK-12 | Disputes with evidence (photos, HMR logs, chat, GPS trail) | M |
| F-BK-13 | In-booking chat (customer ↔ owner ↔ operator) | P2 |
| F-BK-14 | Recurring / long-term contracts with monthly billing | P3 |

### 6.5 Payments and settlement
| ID | Requirement | Pri |
|---|---|---|
| F-PY-1 | Pay online via payment aggregator (UPI, cards, netbanking) | M |
| F-PY-2 | Advance payment + security deposit, then final settlement on actual HMR | M |
| F-PY-3 | Offline payment record ("paid to owner in cash / NEFT") with owner confirmation, so we keep the data even when money bypasses us | M |
| F-PY-4 | GST-compliant tax invoices (owner → customer for rental; platform → owner for commission; platform → customer for convenience fee) | M |
| F-PY-5 | Split settlement / payouts to owners T+N days after completion | M |
| F-PY-6 | Refunds (cancellations, deposit release) | M |
| F-PY-7 | Internal double-entry ledger and reconciliation against gateway reports | M |
| F-PY-8 | TDS (194-I) declaration by business customers and TDS certificate upload | P2 |
| F-PY-9 | Credit terms / invoicing for verified enterprise customers | P3 |

### 6.6 Tracking and operations
| ID | Requirement | Pri |
|---|---|---|
| F-TR-1 | Operator app shares location while a job is active (foreground service) | M |
| F-TR-2 | Customer sees machine ETA and live position during dispatch | M |
| F-TR-3 | Site geofence: auto-detect arrival and departure | P2 |
| F-TR-4 | Fleet live map for all machines in the organisation | P2 |
| F-TR-5 | Telematics device / OEM API integration for engine hours and location | P3 |
| F-TR-6 | Maintenance schedules (service every N hours), maintenance log | P2 |

### 6.7 Admin and support
| ID | Requirement | Pri |
|---|---|---|
| F-AD-1 | KYC verification queue | M |
| F-AD-2 | Listing moderation queue | M |
| F-AD-3 | Booking monitoring, manual overrides with audit trail | M |
| F-AD-4 | Dispute resolution workspace | M |
| F-AD-5 | Commission rules per category / city, platform fees, cancellation policies | M |
| F-AD-6 | Payout batches, reconciliation reports | M |
| F-AD-7 | Taxonomy management | M |
| F-AD-8 | CMS for banners, FAQs, city pages | P2 |
| F-AD-9 | Analytics: GMV, take rate, utilisation, funnel, cohort | P2 |

### 6.8 Notifications
| ID | Requirement | Pri |
|---|---|---|
| F-NT-1 | Push (FCM / APNs via FCM) | M |
| F-NT-2 | SMS (DLT-registered templates) for OTP and critical events | M |
| F-NT-3 | WhatsApp Business API for booking updates (customers prefer WhatsApp over email) | P2 |
| F-NT-4 | Email for invoices and statements | M |
| F-NT-5 | In-app notification centre | M |

## 7. Non-functional requirements

| Area | Requirement |
|---|---|
| **Performance** | p95 API latency under 300 ms for reads and under 600 ms for writes within India. Search under 500 ms for a 50 km radius |
| **Availability** | 99.5% at MVP, 99.9% from Phase 3. Planned maintenance under 30 min/month at night IST |
| **Scalability** | Design for 100k users, 20k machines and 2k concurrent tracked machines without re-architecture (see ARCHITECTURE §11) |
| **Low connectivity** | Construction sites have poor 2G/3G. Mobile app must queue checklists, photos and HMR logs offline and sync later. Images are compressed on device to under 300 KB |
| **Low-end devices** | Android 8+ (API 26), 2 GB RAM devices. App size under 30 MB download |
| **Localisation** | All strings externalised (ARB). INR formatting (₹1,23,456), IST, DD/MM/YYYY. RTL not needed |
| **Accessibility** | WCAG 2.1 AA on web. Large tap targets for operators wearing gloves |
| **Security** | See [SECURITY.md](SECURITY.md). OWASP ASVS L2 |
| **Privacy** | DPDP Act 2023 + DPDP Rules 2025 compliant. Data stored in India region |
| **Auditability** | Every state change and money movement is attributable and immutable |
| **Observability** | Structured logs, metrics, traces. Log retention 180 days (CERT-In) |
| **Maintainability** | Modular monolith with clear module boundaries. 70%+ unit test coverage on domain and payment code |
| **Data durability** | RPO under 15 min (WAL archiving / PITR), RTO under 4 h |

## 8. Indian marketplace requirements (compliance checklist)

> Not legal or tax advice. **Engage a CA and a tech lawyer before launch.** Rates below reflect recent public notifications and must be re-verified.

| Topic | What applies | Product impact |
|---|---|---|
| **GST on rental** | Renting machinery with or without operator is generally taxed at **18%**. SAC codes differ for "with operator" and "without operator" services, so confirm with a CA | Per-booking GST calculation. CGST+SGST if intra-state, IGST if inter-state (place of supply rules) |
| **GST TCS (Sec. 52)** | An e-commerce operator collecting consideration on behalf of suppliers must collect TCS (currently 0.5%) from registered owners and file GSTR-8 | Ledger account for TCS. Monthly GSTR-8 export. Applies only when payments flow through us |
| **Unregistered owners** | Small owners under the GST threshold may be unregistered. Supply of services through an e-commerce operator has specific registration rules | KYC captures GST status. Policy decision on whether unregistered owners can transact online at MVP (see open questions) |
| **Income tax TDS 194-O** | E-commerce operators deduct TDS (currently 0.1%) on gross amount paid to e-commerce participants | Deducted at payout. Form 26Q. Owner PAN mandatory (higher rate without PAN) |
| **TDS 194-I** | Business customers paying rent for machinery deduct 2% TDS | Customer can mark "TDS deducted". Owner receives net. Certificate upload. Affects reconciliation |
| **E-invoicing** | Mandatory for businesses above the turnover threshold | Platform's own invoices need IRN once our turnover crosses the limit. Owners above threshold need IRN for their invoices (via GSP integration, P3) |
| **E-way bill / delivery challan** | Moving machinery by road on a trailer (not on its own wheels) can need an e-way bill / delivery challan even for rental movement | Mobilisation step reminds owner. Document upload slot |
| **Vehicle compliance** | Road-going construction equipment vehicles need RC, insurance, fitness and PUC. Operators need an appropriate licence | Document expiry tracking. Block listing when insurance expires |
| **Payments regulation** | RBI Payment Aggregator rules: a marketplace should not hold customer funds in its own current account | Use a licensed PA's **split settlement / escrow / route** product (Razorpay Route, Cashfree Easy Split, PayU or similar). We store **no card data** |
| **Data localisation** | RBI requires payment system data stored in India. DPDP allows cross-border transfer except to notified countries | Host in an Indian region (AWS ap-south-1 Mumbai, ap-south-2 Hyderabad, or an Indian cloud) |
| **DPDP Act 2023 + Rules 2025** | Consent notices, purpose limitation, data principal rights, breach notification, data retention limits, children's data | Consent records, privacy centre, deletion workflow. See SECURITY.md |
| **Aadhaar** | Only authorised entities can do Aadhaar authentication. Aadhaar numbers must not be stored except in an Aadhaar Data Vault | Use **DigiLocker** or **offline XML/QR e-KYC** through a licensed KYC provider. Store only a reference ID + last 4 digits |
| **TRAI DLT** | Every SMS sender ID and template must be registered on DLT | Register entity + templates before launch (2–3 weeks lead time) |
| **CERT-In directions (2022)** | Report cyber incidents within 6 hours. Keep logs for 180 days in India. Sync clocks to NTP | Incident runbook, log retention policy |
| **Consumer Protection (E-Commerce) Rules 2020** | Grievance officer, display seller details, return/refund/cancellation policy, no fake reviews | Grievance officer page, seller info on listing, published policies |
| **IT Rules 2021** | Intermediary due diligence, grievance redressal in fixed timelines | Terms, privacy policy, grievance mechanism |
| **Entity** | Pvt Ltd company, GST registration, Startup India (DPIIT) recognition optional | Needed for PA onboarding and app store accounts as an organisation |

## 9. Development and deployment cost estimate

### 9.1 Team-based build (realistic for MVP in ~5–6 months)

| Role | FTE | Monthly cost (India, approx.) |
|---|---|---|
| Tech lead / Go backend | 1 | ₹1.5–2.5 L |
| Go backend developer | 1 | ₹0.8–1.4 L |
| Flutter developers | 2 | ₹1.6–2.6 L |
| UI/UX designer | 0.5 | ₹0.4–0.7 L |
| QA engineer | 1 | ₹0.5–0.8 L |
| PM / BA (founder may cover) | 0.5 | ₹0.5–1 L |
| DevOps (part-time / contract) | 0.25 | ₹0.3–0.5 L |
| **Total** | | **≈ ₹5.6–9.5 L / month → ₹30–55 L for MVP** |

**Solo founder with AI-assisted development:** cash cost is mostly infrastructure and services. Expect **9–12 months** to a production-grade MVP part-time, or ~6 months full-time, if scope is held tightly to the MVP column.

### 9.2 Infrastructure (monthly, approx.)

| Item | MVP / pilot | Growth (≈50k users) |
|---|---|---|
| Compute (API, workers, WebSocket) | ₹3–6k (1–2 small VMs or managed containers, India region) | ₹25–60k |
| PostgreSQL + PostGIS (managed, with backups) | ₹3–8k | ₹20–50k |
| Redis (managed or self-hosted) | ₹0–2k | ₹5–12k |
| Object storage + CDN (photos, documents) | ₹500–2k | ₹5–15k |
| Monitoring / logs | ₹0 (free tiers / self-hosted Grafana) | ₹5–20k |
| **Infra subtotal** | **₹8–20k** | **₹60k–1.5 L** |

### 9.3 Third-party services (usage-based, approx.)

| Service | Approx. cost |
|---|---|
| SMS OTP (DLT) | ₹0.15–0.25 per SMS |
| WhatsApp Business API (utility / auth messages) | ~₹0.12–0.15 per message + BSP fee |
| Maps (geocoding, places, directions) | Google Maps Platform has India pricing with a monthly free tier. **Ola Maps** and **Mappls (MapmyIndia)** are cheaper Indian alternatives. Budget ₹0–15k at MVP |
| Payment aggregator | ~2% on cards/netbanking. UPI often cheaper or negotiable. Split-settlement products may add a fee |
| KYC APIs (PAN, GSTIN, bank penny drop, DigiLocker) | ₹2–15 per verification |
| Push notifications (FCM) | Free |
| Apple Developer Program | US$99 / year |
| Google Play Console | US$25 one-time |
| iOS build machine | Codemagic free tier (macOS minutes) or a used Mac mini (~₹40–60k one-time) |
| Domain + email | ~₹2–5k / year |
| Legal (T&C, privacy, PA onboarding), CA (GST/TDS setup) | ₹50k–2 L one-time |

## 10. Scope boundaries for MVP

**In:** Customer + Owner + Operator mobile app, Customer/Owner web, Admin web, OTP login, KYC (manual verification + API checks), listings, geo search, request/quote booking, operator assignment, digital handover/HMR/return, online advance + final payment via PA, offline payment recording, invoices, payouts, ratings, disputes (admin-mediated), live tracking during dispatch and active jobs, English + Hindi.

**Out (deferred):** Instant Book, fleet analytics, telematics hardware, insurance, financing, chat, WhatsApp, e-invoicing IRN, multi-city pricing intelligence, recurring contracts, more languages.

## 11. Key risks

| Risk | Mitigation |
|---|---|
| **Disintermediation** (customer and owner meet once, then deal directly) | Value beyond matchmaking: payment protection, digital HMR, GST invoices, credit, ratings, insurance. Hide phone numbers until confirmed. Allow offline payment recording instead of fighting it |
| Cold start (no supply) | Launch in **one city**. Field-onboard 100+ machines before customer marketing. Offer owners free fleet tools |
| HMR disputes | Photo of meter at start and end, daily customer approval, GPS trail, auto-approval timeout |
| Payment regulations change | Never hold funds. Rely on the PA's split settlement |
| Operator safety and accidents | Verified licences, insurance requirement, incident reporting, clear liability T&C |
| Low smartphone literacy of operators | Very simple operator UI, voice prompts in Hindi, large buttons, offline-first |

## 12. Decisions needed from you (before development starts)

1. **Pilot city** for launch (e.g. Pune, Hyderabad, Bengaluru, Ahmedabad).
2. **Fleet Manager scope.** Is the owner-side only assumption OK?
3. **One mobile app with role switching** (recommended) or separate Customer and Partner apps? See ARCHITECTURE §4.
4. **Public SEO website.** Server-rendered pages from Go (recommended) or Flutter web only? See ARCHITECTURE §4.
5. **Payment aggregator preference** (Razorpay / Cashfree / PayU). This affects the split-settlement API.
6. **Maps provider** (Google vs. Ola Maps vs. Mappls).
7. **Do unregistered (no-GST) owners get online payments at MVP**, or offline-only until GST registered?
8. **Hosting**: AWS Mumbai (recommended for managed Postgres + PostGIS) vs. a cheaper Indian VPS provider.
9. **Team shape**: solo + AI-assisted, or hiring? This changes the roadmap timeline.
