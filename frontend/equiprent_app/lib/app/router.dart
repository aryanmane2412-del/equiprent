import 'package:flutter/material.dart';
import 'session_reset.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../core/auth/auth_controller.dart';
import '../core/routing/role_module.dart';
import '../core/session/active_role_provider.dart';
import '../core/session/user_role.dart';
import '../core/widgets/widgets.dart';
import '../features/admin/admin_module.dart';
import '../features/auth/account_security_page.dart';
import '../features/auth/auth_common.dart';
import '../features/auth/forgot_password_page.dart';
import '../features/auth/register_page.dart';
import '../features/auth/sign_in_page.dart';
import '../features/auth/verify_email_page.dart';
import '../features/auth/verify_page.dart';
import '../features/customer/customer_module.dart';
import '../features/fleet_manager/fleet_manager_module.dart';
import '../features/onboarding/role_selection_page.dart';
import '../features/operator/operator_module.dart';
import '../features/owner/owner_module.dart';

abstract final class AppRoutes {
  static const roleSelection = '/';
}

/// All role modules mounted by the router, one per role.
final List<RoleModule> roleModules = [
  customerModule,
  ownerModule,
  operatorModule,
  fleetManagerModule,
  adminModule,
];

/// Route guard. Returns the redirect target, or null to allow navigation.
///
/// * While the stored session is being restored, everything waits on a
///   loading screen.
/// * Signed out: only the auth screens (and the design system) are reachable.
/// * Signed in: a role's area opens only when the server granted that role
///   AND it is the active role. This only decides which screens to show;
///   the API authorises every request on its own.
@visibleForTesting
String? appRedirect(AuthState auth, UserRole? activeRole, String location) {
  switch (auth.status) {
    case AuthStatus.restoring:
      return location == AuthRoutes.loading ? null : AuthRoutes.loading;
    case AuthStatus.signedOut:
      return AuthRoutes.public.contains(location) ? null : AuthRoutes.signIn;
    case AuthStatus.signedIn:
      if (location == AuthRoutes.signIn || location == AuthRoutes.register || location == AuthRoutes.loading) {
        return AppRoutes.roleSelection;
      }
      final target = UserRole.fromLocation(location);
      if (target == null) return null;
      final granted = auth.user?.roles ?? const [];
      if (!granted.contains(target) || target != activeRole) return AppRoutes.roleSelection;
      return null;
  }
}

final routerProvider = Provider<GoRouter>((ref) {
  // GoRouter re-runs redirects whenever this notifier fires.
  final refresh = ValueNotifier<int>(0);
  ref.listen(activeRoleProvider, (_, _) => refresh.value++);
  ref.listen(authControllerProvider, (prev, next) {
    refresh.value++;
    final left = prev?.isSignedIn == true && !next.isSignedIn;
    final switched = prev?.user != null && next.user != null && prev!.user!.id != next.user!.id;
    if (left || switched) resetUserState(ref);
  });

  final router = GoRouter(
    initialLocation: AppRoutes.roleSelection,
    refreshListenable: refresh,
    redirect: (context, state) =>
        appRedirect(ref.read(authControllerProvider), ref.read(activeRoleProvider), state.matchedLocation),
    routes: [
      GoRoute(path: AppRoutes.roleSelection, builder: (context, state) => const RoleSelectionPage()),
      GoRoute(path: AuthRoutes.loading, builder: (context, state) => const Scaffold(body: LoadingView(message: 'Signing you in…'))),
      GoRoute(path: AuthRoutes.signIn, builder: (context, state) => const SignInPage()),
      GoRoute(path: AuthRoutes.register, builder: (context, state) => const RegisterPage()),
      GoRoute(path: AuthRoutes.verify, builder: (context, state) => const VerifyPage()),
      GoRoute(path: AuthRoutes.forgotPassword, builder: (context, state) => const ForgotPasswordPage()),
      GoRoute(
        path: AuthRoutes.verifyEmail,
        builder: (context, state) => VerifyEmailPage(token: state.uri.queryParameters['token']),
      ),
      GoRoute(path: AuthRoutes.account, builder: (context, state) => const AccountSecurityPage()),
      for (final module in roleModules) module.toShellRoute(),
    ],
    errorBuilder: (context, state) => Scaffold(
      body: EmptyState(
        icon: AppIcons.search,
        title: 'Page not found',
        message: 'Nothing lives at ${state.uri.path}.',
        action: AppButton(label: 'Go home', onPressed: () => context.go(AppRoutes.roleSelection)),
      ),
    ),
  );

  ref.onDispose(() {
    router.dispose();
    refresh.dispose();
  });
  return router;
});
