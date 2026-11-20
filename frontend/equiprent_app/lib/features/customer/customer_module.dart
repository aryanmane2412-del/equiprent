import 'package:go_router/go_router.dart';

import '../../core/routing/role_module.dart';
import '../../core/session/user_role.dart';
import '../../core/theme/app_icons.dart';
import '../agreements/agreement_page.dart';
import '../documents/document_pages.dart';
import '../documents/invoice_detail_page.dart';
import '../bookings/booking_pages.dart';
import '../chat/chat_pages.dart';
import '../comms/data/comms_api.dart' show BookingSide;
import '../notifications/notifications_page.dart';
import '../discovery/presentation/discover_page.dart';
import '../discovery/presentation/saved_compare_pages.dart';
import '../listings/presentation/customer_listing_pages.dart';
import '../verification/presentation/customer_verification_page.dart';
import '../bookings/booking_request_page.dart';
import '../bookings/booking_tracking_page.dart';
import '../bookings/payments_invoices_page.dart';
import 'presentation/customer_home_page.dart';
import 'presentation/customer_profile_page.dart';

/// Customer routes. Tabs: Home, Search, Bookings, Chat, Profile.
abstract final class CustomerRoutes {
  static const home = '/customer';
  static const search = '/customer/search';
  static const bookings = '/customer/bookings';
  static const chat = '/customer/chat';
  static const profile = '/customer/profile';
  static const categories = '/customer/categories';
  static const compare = '/customer/compare';
  static const payments = '/customer/payments';
  static const verification = '/customer/verification';
  static const saved = '/customer/saved';

  static String searchCategory(String id) => '$search?category=$id';
  static String equipment(String id) => '/customer/equipment/$id';
  static String book(String id) => '/customer/book/$id';
  static String track(String bookingId) =>
      '/customer/bookings/$bookingId/track';
  static String thread(String id) => '/customer/chat/$id';
}

final customerModule = RoleModule(
  role: UserRole.customer,
  destinations: [
    RoleDestination(
      path: CustomerRoutes.home,
      label: 'Home',
      icon: AppIcons.home,
      builder: (_) => const CustomerHomePage(),
    ),
    RoleDestination(
      path: CustomerRoutes.search,
      label: 'Search',
      icon: AppIcons.search,
      builder: (_) => const DiscoverPage(),
      alsoMatches: const [
        '/customer/equipment',
        '/customer/book',
        CustomerRoutes.compare,
        CustomerRoutes.categories,
        CustomerRoutes.saved,
      ],
    ),
    RoleDestination(
      path: CustomerRoutes.bookings,
      label: 'Bookings',
      icon: AppIcons.bookings,
      builder: (_) => const BookingListPage(side: BookingSide.customer),
    ),
    RoleDestination(
      path: CustomerRoutes.chat,
      label: 'Chat',
      icon: AppIcons.chat,
      builder: (_) => const ConversationsPage(),
    ),
    RoleDestination(
      path: CustomerRoutes.profile,
      label: 'Profile',
      icon: AppIcons.profile,
      builder: (_) => const CustomerProfilePage(),
      alsoMatches: const [CustomerRoutes.payments],
    ),
  ],
  extraRoutes: [
    GoRoute(
      path: CustomerRoutes.categories,
      builder: (context, state) => const ListingCategoriesPage(),
    ),
    GoRoute(
      path: CustomerRoutes.compare,
      builder: (context, state) => CompareListingsPage(
        ids: state.uri.queryParameters['ids']
            ?.split(',')
            .where((s) => s.isNotEmpty)
            .toList(),
      ),
    ),
    GoRoute(
      path: CustomerRoutes.saved,
      builder: (context, state) => const SavedEquipmentPage(),
    ),
    GoRoute(
      path: CustomerRoutes.payments,
      builder: (context, state) => const PaymentsInvoicesPage(),
    ),
    GoRoute(
      path: CustomerRoutes.verification,
      builder: (context, state) => const CustomerVerificationPage(),
    ),
    GoRoute(
      path: '/customer/equipment/:id',
      builder: (context, state) =>
          ListingDetailPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/customer/book/:id',
      builder: (context, state) =>
          BookingRequestPage(equipmentId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/customer/bookings/:id/track',
      builder: (context, state) => BookingTrackingPage(
        side: BookingSide.customer,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/customer/chat/:id',
      builder: (context, state) =>
          ChatThreadPage(conversationId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/customer/bookings/:id',
      builder: (context, state) => BookingDetailPage(
        side: BookingSide.customer,
        id: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/customer/bookings/:id/agreement',
      builder: (context, state) => AgreementPage(
        side: BookingSide.customer,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/customer/bookings/:id/agreement/versions',
      builder: (context, state) => AgreementVersionsPage(
        side: BookingSide.customer,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/customer/bookings/:id/agreement/versions/:v',
      builder: (context, state) => AgreementVersionPage(
        side: BookingSide.customer,
        bookingId: state.pathParameters['id']!,
        version: int.tryParse(state.pathParameters['v']!) ?? 1,
      ),
    ),
    GoRoute(
      path: '/customer/bookings/:id/documents',
      builder: (context, state) => DocumentHistoryPage(
        side: BookingSide.customer,
        bookingId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/customer/invoices/:id',
      builder: (context, state) => InvoiceDetailPage(
        side: BookingSide.customer,
        invoiceId: state.pathParameters['id']!,
      ),
    ),
    GoRoute(
      path: '/customer/notifications',
      builder: (context, state) => const NotificationsPage(),
    ),
  ],
);
