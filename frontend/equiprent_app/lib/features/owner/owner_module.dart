import 'package:go_router/go_router.dart';

import '../../core/routing/role_module.dart';
import '../../core/session/user_role.dart';
import '../../core/theme/app_icons.dart';
import '../settings/app_settings_page.dart';
import '../agreements/agreement_page.dart';
import '../fleet_manager/presentation/fleet_pages.dart'
    show FleetOperatorsPage;
import '../documents/document_pages.dart';
import '../documents/invoice_detail_page.dart';
import '../bookings/booking_pages.dart';
import '../bookings/booking_tracking_page.dart';
import '../tracking/presentation/fleet_live_page.dart';
import '../chat/chat_pages.dart';
import '../comms/data/comms_api.dart' show BookingSide;
import '../notifications/notifications_page.dart';
import '../maintenance/maintenance_page.dart';
import '../reviews/review_widgets.dart';
import 'presentation/owner_calendar_page.dart';
import 'package:equiprent_app/features/owner/presentation/owner_dashboard_page.dart';
import 'presentation/owner_earnings_page.dart';
import 'presentation/owner_profile_page.dart';
import 'owner_routes.dart';
import '../listings/presentation/owner_listing_pages.dart';
import '../verification/presentation/owner_verification_page.dart';

final ownerModule = RoleModule(
  role: UserRole.owner,
  destinations: [
    RoleDestination(
      path: OwnerRoutes.dashboard,
      label: 'Dashboard',
      icon: AppIcons.dashboard,
      builder: (_) => const OwnerDashboardPage(),
    ),
    RoleDestination(
      path: OwnerRoutes.listings,
      label: 'Listings',
      icon: AppIcons.listings,
      builder: (_) => const MyListingsPage(),
    ),
    RoleDestination(
      path: OwnerRoutes.bookings,
      label: 'Bookings',
      icon: AppIcons.bookings,
      builder: (_) => const BookingListPage(side: BookingSide.owner),
    ),
    RoleDestination(
      path: OwnerRoutes.calendar,
      label: 'Calendar',
      icon: AppIcons.calendar,
      builder: (_) => const OwnerCalendarPage(),
    ),
    RoleDestination(
      path: OwnerRoutes.earnings,
      label: 'Earnings',
      icon: AppIcons.earnings,
      builder: (_) => const OwnerEarningsPage(),
    ),
    RoleDestination(
      path: OwnerRoutes.maintenance,
      label: 'Maintenance',
      icon: AppIcons.maintenance,
      builder: (_) => const MaintenancePage(),
    ),
    RoleDestination(
      path: OwnerRoutes.profile,
      label: 'Profile',
      icon: AppIcons.profile,
      builder: (_) => const OwnerProfilePage(),
    ),
    RoleDestination(
      path: OwnerRoutes.settings,
      label: 'Settings',
      icon: AppIcons.settings,
      builder: (_) => const AppSettingsPage(),
    ),
  ],
  extraRoutes: [
    GoRoute(
      path: OwnerRoutes.addEquipment,
      builder: (context, state) => const ListingEditorPage(),
    ),
    GoRoute(
      path: '/owner/listings/:id',
      builder: (context, state) =>
          ManageListingPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/owner/listings/:id/edit',
      builder: (context, state) =>
          ListingEditorPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: OwnerRoutes.verification,
      builder: (context, state) => const OwnerVerificationPage(),
    ),
    GoRoute(
      path: OwnerRoutes.drivers,
      builder: (context, state) => const FleetOperatorsPage(),
    ),
    GoRoute(
      path: '/owner/live',
      builder: (context, state) => const FleetLivePage(),
    ),
    GoRoute(
      path: '/owner/bookings/:id',
      builder: (context, state) => BookingDetailPage(
        side: BookingSide.owner,
        id: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/owner/bookings/:id/track',
      builder: (context, state) => BookingTrackingPage(
        side: BookingSide.owner,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/owner/bookings/:id/agreement',
      builder: (context, state) => AgreementPage(
        side: BookingSide.owner,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/owner/bookings/:id/agreement/versions',
      builder: (context, state) => AgreementVersionsPage(
        side: BookingSide.owner,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/owner/bookings/:id/agreement/versions/:v',
      builder: (context, state) => AgreementVersionPage(
        side: BookingSide.owner,
        bookingId: state.pathParameters['id']!,
        version: int.tryParse(state.pathParameters['v']!) ?? 1,
      ),
    ),
    GoRoute(
      path: '/owner/bookings/:id/documents',
      builder: (context, state) => DocumentHistoryPage(
        side: BookingSide.owner,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/owner/invoices/:id',
      builder: (context, state) => InvoiceDetailPage(
        side: BookingSide.owner,
        invoiceId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: OwnerRoutes.messages,
      builder: (context, state) => const ConversationsPage(topLevel: false),
    ),
    GoRoute(
      path: '/owner/messages/:id',
      builder: (context, state) =>
          ChatThreadPage(conversationId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: OwnerRoutes.reviews,
      builder: (context, state) => const OwnerReviewsPage(),
    ),
    GoRoute(
      path: '/owner/notifications',
      builder: (context, state) => const NotificationsPage(),
    ),
  ],
);
