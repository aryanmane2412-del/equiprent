# EQUIPRENT — Design System

Status: v0.1 · 2026-11-12 · Live reference: run the app and open **/design-system** (also linked from the role picker).

> Premium construction technology: navy and white, with orange and green used carefully. Every screen must work on a 360 px Android phone and a 1440 px desktop.

## 1. Principles

1. **One primary action per screen.** Orange is reserved for it (Book now, Add equipment, Start trip). Everything else is navy, outlined or text.
2. **Status is never colour alone.** Every badge has a label; pass/fail rows have an icon *and* text.
3. **Money is exact and aligned.** ₹ with Indian grouping (₹1,23,456), tabular figures, amounts flush right.
4. **Built for the site, not just the office.** 48 px minimum tap targets, large single-action buttons for operators, offline-friendly messaging, reduced-motion respected.
5. **Nothing is silently dead.** Every button either works on mock data or shows “isn’t available yet — planned for Phase N”.

## 2. Tokens (`lib/core/theme/app_tokens.dart`)

### Colour
| Token | Hex | Use |
|---|---|---|
| `navy900` | #0F1F3A | Side navigation, dark surfaces, hero gradients |
| `navy700` (brand) | #1C3A6B | Primary buttons, links, selected states |
| `navy100` | #E3EAF5 | Selected backgrounds, tints |
| `orange600` (accent) | #EA580C | **The** primary call-to-action; today marker; brand accent stripe |
| `green700` (success) | #15803D | Success, approve, money received, available |
| `red600` (danger) | #DC2626 | Errors, destructive actions |
| `amber700` (warning) | #B45309 | Warnings, due soon |
| `slate900 / 600 / 500` | | Primary / secondary / muted text |
| `slate200` | #E2E8F0 | Borders, dividers |
| `canvas` | #F5F7FB | App background behind white cards |

Semantic sets for badges, banners and icon tiles live in `AppTone` (`neutral, brand, accent, info, success, warning, danger`), each with foreground, background and border.

### Typography: Inter (bundled, OFL)
| Style | Size / weight | Use |
|---|---|---|
| displaySmall | 32 / 700 | Marketing hero |
| headlineMedium | 24 / 700 | KPI values |
| headlineSmall | 20 / 600 | Section titles on big pages, state titles |
| titleLarge | 18 / 600 | App bar titles, section headers |
| titleMedium / Small | 16 / 14 · 600 | Card titles, list titles |
| bodyLarge / Medium / Small | 16 / 14 / 12 · 400 | Text |
| labelLarge / Medium / Small | 14 / 12 / 11 · 600 | Buttons, badges, captions |

Use `AppTypography.tabular` (tabular figures) for prices, counts and tables. The full Inter character set is bundled so the ₹ sign (U+20B9) renders in brand type on every platform.

### Spacing, radius, elevation, motion
- Spacing (4-pt): `xxs 2 · xs 4 · sm 8 · ms 12 · md 16 · lg 24 · xl 32 · xxl 48 · xxxl 64`. Page gutters: 16 (phone) / 24 (tablet) / 32 (desktop). Content max width 1200.
- Radius: `xs 6 · sm 8 · md 12 (cards) · 10 (controls) · lg 16 (dialogs) · 20 (sheets) · full`.
- Elevation: cards use a 1 px border. Shadows (`AppShadows.sm/md/lg`, navy-tinted) are for hover, sheets, menus and sticky bars.
- Motion: `fast 150 ms · medium 250 ms · slow 400 ms`, `easeOutCubic`. Page transitions: fade-forwards (Android/desktop/web), Cupertino (iOS). Looping animations (map vehicle, skeleton shimmer, spinner) stop when the OS asks for reduced motion.

### Icons
Lucide (`lucide_icons_flutter`), always referenced through semantic names in `AppIcons` (e.g. `AppIcons.bookings`). Lucide has no heavy-machinery glyphs. Category icons are the closest consistent stand-ins until custom pictograms are drawn (see §7).

## 3. Components (`lib/core/widgets/`, one import: `widgets.dart`)

| Component | File | Notes |
|---|---|---|
| `AppButton` | buttons.dart | Variants: primary, **accent**, secondary, ghost, success, danger. Sizes S/M/L (36/48/56). Loading state, icons, full width |
| `AppIconButton` | buttons.dart | Tooltip required; optional badge count |
| `AppTextField`, `AppSearchField`, `AppDropdown`, `PickerField`, `AppChoiceChips`, `QuantityStepper` | inputs.dart | Labels above fields; required marker; helper and error text |
| `AppCard`, `SectionCard`, `StatCard`, `IconTile`, `InfoRow`, `ListRow` | cards.dart | Cards lift on hover. KPI tiles with trend |
| `EquipmentCard` | equipment_card.dart | Grid and horizontal variants, favourite + compare toggles |
| `StatusBadge`, `StatusBadges.*`, `RatingLabel`, `StarRating`, `VerifiedTag` | badges.dart | One mapping per domain status, so colours are consistent app-wide |
| `showConfirmDialog`, `AppDialog`, `showAppSheet`, `showActionSheet` | overlays.dart | Sheets on phones become centred dialogs on desktop |
| `AppDataTable` | data_table.dart | Sortable `DataTable` on wide screens; stacked cards below 720 px |
| `RoleShell` | role_shell.dart | Role navigation: bottom bar (≤ 5, else “More” sheet) → collapsed rail (tablet) → full navy sidebar (desktop) |
| `AppPage`, `SectionHeader`, `SplitLayout`, `BottomActionBar(.split)` | layout.dart | Standard page with notifications + account menu; sticky action bar that stacks on phones |
| `LoadingView`, `Skeleton`, `SkeletonCard` | feedback.dart | Skeletons for content, spinners for short actions |
| `EmptyState`, `ErrorState` (incl. offline), `SuccessState`, `InlineBanner`, `AppToast`, `showComingSoon` | feedback.dart | Always explain and offer a next step |
| `BarChart`, `LineTrend`, `HorizontalBars`, `ProgressMeter` | charts.dart | Single-series, one navy hue, current period emphasised + direct-labelled, tooltip and semantics on every mark |
| `MapPreview` | map_preview.dart | Stylised map with route, markers and an animated vehicle until a maps provider is chosen |
| `MonthCalendar`, `StatusTimeline`, `StepIndicator` | schedule.dart | Availability, lifecycle progress, multi-step flows |
| `EquipmentImage`, `AppAvatar` | media.dart | Photo slot (branded placeholder until real photos), initials avatars |

## 4. Responsive rules

| Window | Width | Navigation | Layout |
|---|---|---|---|
| compact | < 600 | Bottom bar | 1 column, stacked action bars, tables as cards |
| medium | 600–839 | Collapsed side rail | 2 columns |
| expanded | 840–1199 | Full sidebar | 3 columns, split panes from 960 |
| large | ≥ 1200 | Full sidebar | 4 columns, content capped at 1200 |

## 5. Screens (all on realistic mock data, no backend)

| Role | Navigation | Screens |
|---|---|---|
| Customer | Home · Search · Bookings · Chat · Profile | Home, search (filters, sort, list/map, skeleton loading, empty state), categories, equipment details (gallery, pricing, specs, owner, availability, reviews), comparison (best value highlighted), booking (3 steps with live GST breakdown, payment, success), payments, live tracking (map, ETA, timeline, hour-log approval), chat (split view on desktop), profile |
| Owner | Dashboard · Listings · Bookings · Calendar · Earnings · Maintenance · Profile | Dashboard (KPIs, requests to accept, earnings chart, fleet status), add-equipment wizard (4 steps, validation), listings (pause/resume), bookings (accept/decline/assign operator), calendar (block days), earnings (payout breakdown), maintenance (mark done), profile |
| Operator | Today · Assignments · History | Dashboard (duty toggle, one big next-step button), assignments (accept/decline), job details, navigation, handover checklist (photos, HMR, fuel, safety, customer OTP), history |
| Fleet manager | Overview · Equipment · Operators · Assignments · Reports | Live map, utilisation trend, alerts; fleet table; operator roster + invite; staffing queue; reports |
| Admin | Overview · Approvals · Bookings · Disputes · Users · Payments · Settlements · Analytics | Work queues, approvals with automated checks, bookings with SLA alerts, dispute workspace, users (suspend/reactivate), payments (refund), settlements (release/hold), analytics with table view |

Interactive state is in `lib/core/mock/mock_store.dart` (Riverpod notifiers). Each notifier is the seam where a Phase 1 API repository plugs in.

## 6. Quality gates

- `flutter analyze`: no issues.
- `flutter test`: 26 tests. They render **every route of every role at 360, 800 and 1440 px** with no layout errors and with sticky bars under 40% of the screen. They also exercise the booking + payment, compare, chat, accept request, operator handover, admin approval and phone “More” menu flows, plus “unavailable” feedback.

## 7. Known gaps (design debt)

| Gap | Plan |
|---|---|
| Real equipment photography | Owner uploads (Phase 1). `EquipmentImage(photoUrl:)` is ready; placeholders keep layouts honest |
| Heavy-machinery pictograms | Commission a custom icon set in the Lucide style (2 px stroke, 24 grid) |
| Dark mode | Tokens are structured for it; ship after colour validation on the dark surface |
| Hindi / Marathi | Strings to be moved to ARB files. Inter lacks Devanagari, so pair it with Noto Sans Devanagari |
| Real maps | Swap `MapPreview` for the chosen provider (Google / Ola / Mappls) |
| Accessibility audit | Semantics are labelled throughout. A screen-reader pass on a device is still due |
