import 'package:go_router/go_router.dart';

import '../../core/routing/role_module.dart';
import '../../core/session/user_role.dart';
import '../../core/theme/app_icons.dart';
import '../settings/app_settings_page.dart';
import '../notifications/notifications_page.dart';
import '../chat/chat_pages.dart';
import 'presentation/admin_analytics_page.dart';
import 'presentation/admin_bookings_page.dart';
import 'presentation/admin_disputes_page.dart';
import 'presentation/admin_finance_pages.dart';
import 'presentation/admin_overview_page.dart';
import 'presentation/admin_users_page.dart';
import 'presentation/admin_equipment_page.dart';
import 'presentation/admin_owners_page.dart';
import '../listings/presentation/admin_listing_pages.dart';
import '../verification/presentation/admin_verification_pages.dart';

abstract final class AdminRoutes {
  static const overview = '/admin';
  static const disputes = '/admin/disputes';
  static const owners = '/admin/owners';
  static const users = '/admin/users';
  static const equipment = '/admin/equipment';
  static const bookings = '/admin/bookings';
  static const payments = '/admin/payments';
  static const settlements = '/admin/settlements';
  static const analytics = '/admin/analytics';
  static const settings = '/admin/settings';
  static const messages = '/admin/messages';
  static const equipmentVerification = '/admin/listings';
}

final adminModule = RoleModule(
  role: UserRole.admin,
  destinations: [
    RoleDestination(
      path: AdminRoutes.overview,
      label: 'Overview',
      phoneOrder: 0,
      phoneLabel: 'Overview',
      phoneIcon: AppIcons.dashboard,
      icon: AppIcons.dashboard,
      builder: (_) => const AdminOverviewPage(),
    ),
    RoleDestination(
      path: AdminRoutes.bookings,
      label: 'Bookings',
      icon: AppIcons.bookings,
      builder: (_) => const AdminBookingsPage(),
    ),
    RoleDestination(
      path: AdminRoutes.users,
      label: 'Users',
      phoneOrder: 2,
      phoneLabel: 'Users',
      phoneIcon: AppIcons.users,
      icon: AppIcons.users,
      builder: (_) => const AdminUsersPage(),
    ),
    RoleDestination(
      path: AdminRoutes.disputes,
      label: 'Disputes',
      icon: AppIcons.disputes,
      phoneOrder: 3,
      builder: (_) => const AdminDisputesPage(),
    ),
    RoleDestination(
      path: AdminRoutes.owners,
      label: 'Equipment owners',
      icon: AppIcons.users,
      builder: (_) => const AdminOwnersPage(),
    ),
    RoleDestination(
      path: AdminRoutes.equipment,
      label: 'Equipment',
      icon: AppIcons.fleet,
      builder: (_) => const AdminEquipmentPage(),
    ),
    RoleDestination(
      path: AdminRoutes.equipmentVerification,
      label: 'Equipment verification',
      icon: AppIcons.approvals,
      builder: (_) => const ListingModerationPage(),
    ),
    RoleDestination(
      path: '/admin/verifications',
      label: 'Driver verification',
      phoneOrder: 1,
      phoneLabel: 'Approvals',
      phoneIcon: AppIcons.shield,
      icon: AppIcons.operator,
      builder: (_) => const VerificationQueuePage(role: 'operator'),
    ),
    RoleDestination(
      path: AdminRoutes.payments,
      label: 'Payments',
      icon: AppIcons.payments,
      builder: (_) => const AdminPaymentsPage(),
    ),
    RoleDestination(
      path: AdminRoutes.settlements,
      label: 'Settlements',
      icon: AppIcons.settlements,
      builder: (_) => const AdminSettlementsPage(),
    ),
    RoleDestination(
      path: AdminRoutes.analytics,
      label: 'Analytics',
      icon: AppIcons.analytics,
      builder: (_) => const AdminAnalyticsPage(),
    ),
    RoleDestination(
      path: AdminRoutes.settings,
      label: 'Settings',
      icon: AppIcons.settings,
      builder: (_) => const AppSettingsPage(),
    ),
    RoleDestination(
      path: AdminRoutes.messages,
      label: 'Support messages',
      icon: AppIcons.support,
      builder: (_) => const ConversationsPage(topLevel: false),
    ),
  ],
  extraRoutes: [
    GoRoute(
      path: '/admin/listings/:id',
      builder: (context, state) =>
          ListingReviewPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/admin/verifications/:id',
      builder: (context, state) =>
          VerificationDetailPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/admin/notifications',
      builder: (context, state) => const NotificationsPage(),
    ),
    GoRoute(
      path: '/admin/messages/:id',
      builder: (context, state) =>
          ChatThreadPage(conversationId: state.pathParameters['id']!),
    ),
  ],
);
