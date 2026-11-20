import 'package:go_router/go_router.dart';

import '../../core/routing/role_module.dart';
import '../../core/session/user_role.dart';
import '../../core/theme/app_icons.dart';
import '../settings/app_settings_page.dart';
import '../chat/chat_pages.dart';
import '../listings/presentation/owner_listing_pages.dart';
import '../maintenance/maintenance_page.dart';
import '../notifications/notifications_page.dart';
import '../verification/presentation/fleet_access_pages.dart';
import '../tracking/presentation/fleet_live_page.dart';
import 'presentation/fleet_pages.dart';

abstract final class FleetRoutes {
  static const overview = '/fleet';
  static const equipment = '/fleet/equipment';
  static const operators = '/fleet/operators';
  static const assignments = '/fleet/assignments';
  static const reports = '/fleet/reports';
  static const company = '/fleet/company';
  static const maintenance = '/fleet/maintenance';
  static const settings = '/fleet/settings';
  static const messages = '/fleet/messages';
}

final fleetManagerModule = RoleModule(
  role: UserRole.fleetManager,
  destinations: [
    RoleDestination(
      path: FleetRoutes.overview,
      label: 'Overview',
      icon: AppIcons.dashboard,
      builder: (_) => const FleetOverviewPage(),
      alsoMatches: const ['/fleet/fleets'],
    ),
    RoleDestination(
      path: FleetRoutes.equipment,
      label: 'Equipment',
      icon: AppIcons.fleet,
      builder: (_) => const FleetEquipmentPage(),
      alsoMatches: const [FleetRoutes.maintenance],
    ),
    RoleDestination(
      path: FleetRoutes.operators,
      label: 'Operators',
      icon: AppIcons.operator,
      builder: (_) => const FleetOperatorsPage(),
    ),
    RoleDestination(
      path: FleetRoutes.assignments,
      label: 'Assignments',
      icon: AppIcons.assignments,
      builder: (_) => const FleetAssignmentsPage(),
    ),
    RoleDestination(
      path: FleetRoutes.reports,
      label: 'Reports',
      icon: AppIcons.reports,
      builder: (_) => const FleetReportsPage(),
    ),
    RoleDestination(
      path: FleetRoutes.settings,
      label: 'Settings',
      icon: AppIcons.settings,
      builder: (_) => const AppSettingsPage(),
    ),
    RoleDestination(
      path: FleetRoutes.messages,
      label: 'Messages',
      icon: AppIcons.chat,
      builder: (_) => const ConversationsPage(topLevel: false),
    ),
  ],
  extraRoutes: [
    GoRoute(
      path: '/fleet/live',
      builder: (context, state) => const FleetLivePage(),
    ),
    GoRoute(
      path: FleetRoutes.company,
      builder: (context, state) => const FleetCompanyPage(),
    ),
    GoRoute(
      path: '/fleet/notifications',
      builder: (context, state) => const NotificationsPage(),
    ),
    GoRoute(
      path: '/fleet/messages/:id',
      builder: (context, state) =>
          ChatThreadPage(conversationId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/fleet/fleets/:id',
      builder: (context, state) =>
          FleetDetailPage(fleetId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/fleet/equipment/new',
      builder: (context, state) => const ListingEditorPage(),
    ),
    GoRoute(
      path: '/fleet/equipment/:id',
      builder: (context, state) =>
          ManageListingPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/fleet/equipment/:id/edit',
      builder: (context, state) =>
          ListingEditorPage(id: state.pathParameters['id']!),
    ),
    GoRoute(
      path: FleetRoutes.maintenance,
      builder: (context, state) => const MaintenancePage(),
    ),
  ],
);
