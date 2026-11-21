import 'package:go_router/go_router.dart';

import '../../core/routing/role_module.dart';
import '../../core/session/user_role.dart';
import '../../core/theme/app_icons.dart';
import '../settings/app_settings_page.dart';
import '../chat/chat_pages.dart';
import '../notifications/notifications_page.dart';
import '../verification/presentation/operator_verification_page.dart';
import 'presentation/operator_pages.dart';
import 'presentation/operator_payment_page.dart';

/// Operator routes. Jobs are addressed by their booking id.
abstract final class OperatorRoutes {
  static const dashboard = '/operator';
  static const route = '/operator/route';
  static const safety = '/operator/safety';
  static const assignments = '/operator/assignments';
  static const history = '/operator/history';
  static const verification = '/operator/verification';
  static const payments = '/operator/payments';
  static const settings = '/operator/settings';

  static String job(String id) => OperatorJobRoutes.job(id);
  static String navigate(String id) => OperatorJobRoutes.navigate(id);
  static String checklist(String id, String kind) =>
      OperatorJobRoutes.checklist(id, kind);
}

final operatorModule = RoleModule(
  role: UserRole.operator,
  destinations: [
    RoleDestination(
      path: OperatorRoutes.dashboard,
      label: 'Today',
      phoneOrder: 0,
      phoneLabel: 'Jobs',
      phoneIcon: AppIcons.fleet,
      icon: AppIcons.home,
      builder: (_) => const OperatorTodayPage(),
    ),
    RoleDestination(
      path: OperatorRoutes.route,
      label: 'Route',
      icon: AppIcons.navigation,
      phoneOrder: 1,
      builder: (_) => const OperatorRoutePage(),
    ),
    RoleDestination(
      path: OperatorRoutes.safety,
      label: 'Safety',
      icon: AppIcons.handover,
      phoneOrder: 2,
      builder: (_) => const OperatorSafetyPage(),
    ),
    RoleDestination(
      path: OperatorRoutes.assignments,
      label: 'Assignments',
      icon: AppIcons.assignments,
      builder: (_) => const OperatorJobsPage(scope: 'current'),
      alsoMatches: const ['/operator/jobs'],
    ),
    RoleDestination(
      path: OperatorRoutes.history,
      label: 'History',
      phoneOrder: 3,
      phoneLabel: 'Log',
      phoneIcon: AppIcons.history,
      icon: AppIcons.history,
      builder: (_) => const OperatorJobsPage(scope: 'history'),
    ),
    RoleDestination(
      path: OperatorRoutes.verification,
      label: 'Verification',
      icon: AppIcons.shield,
      builder: (_) => const OperatorVerificationPage(),
    ),
    RoleDestination(
      path: OperatorRoutes.payments,
      label: 'Payments',
      icon: AppIcons.payments,
      builder: (_) => const OperatorPaymentPage(),
    ),
    RoleDestination(
      path: OperatorRoutes.settings,
      label: 'Settings',
      icon: AppIcons.settings,
      builder: (_) => const AppSettingsPage(),
    ),
  ],
  extraRoutes: [
    GoRoute(
      path: '/operator/messages',
      builder: (context, state) => const ConversationsPage(topLevel: false),
    ),
    GoRoute(
      path: '/operator/messages/:id',
      builder: (context, state) =>
          ChatThreadPage(conversationId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/operator/notifications',
      builder: (context, state) => const NotificationsPage(),
    ),
    GoRoute(
      path: '/operator/jobs/:id',
      builder: (context, state) =>
          OperatorJobPage(bookingId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/operator/jobs/:id/navigate',
      builder: (context, state) =>
          OperatorNavigationPage(bookingId: state.pathParameters['id']!),
    ),
    GoRoute(
      path: '/operator/jobs/:id/checklist/:kind',
      builder: (context, state) => OperatorChecklistPage(
        bookingId: state.pathParameters['id']!,
        kind: state.pathParameters['kind'] == 'return' ? 'return' : 'handover',
      ),
    ),
  ],
);
