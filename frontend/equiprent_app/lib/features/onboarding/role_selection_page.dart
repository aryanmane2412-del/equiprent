import 'package:flutter/material.dart';
import '../comms/comms_routes.dart';
import '../../core/session/pending_link.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_controller.dart';
import '../../core/session/active_role_provider.dart';
import '../../core/session/user_role.dart';
import '../../core/widgets/widgets.dart';
import '../auth/auth_common.dart';

/// After sign-in: choose which of the account's roles to work in. Only roles
/// granted by the server are listed; with a single role it opens directly.
class RoleSelectionPage extends ConsumerWidget {
  const RoleSelectionPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final wide = context.windowSize.atLeast(WindowSize.expanded);

    void choose(UserRole role) {
      ref.read(activeRoleProvider.notifier).select(role);
      final link = ref.read(pendingLinkProvider.notifier).take();
      context.go(role.basePath);
      final route = link == null ? null : routeForNotification(role, link.type, link.data);
      if (route != null) context.push(route);
    }

    final user = ref.watch(authControllerProvider).user;
    final granted = user?.roles ?? const <UserRole>[];
    if (granted.length == 1 && ref.watch(activeRoleProvider) == null) {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (context.mounted) choose(granted.single);
      });
    }

    final hero = Container(
      padding: EdgeInsets.all(wide ? AppSpacing.xxl : AppSpacing.lg),
      decoration: const BoxDecoration(
        gradient: LinearGradient(begin: Alignment.topLeft, end: Alignment.bottomRight, colors: [AppColors.navy950, AppColors.navy700]),
      ),
      child: SafeArea(
        bottom: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            const BrandLogo(onDark: true),
            SizedBox(height: wide ? AppSpacing.xxl : AppSpacing.xl),
            Text('Heavy equipment,\nrented the smart way.', style: (wide ? theme.textTheme.displaySmall : theme.textTheme.headlineMedium)?.copyWith(color: AppColors.white)),
            const Gap(AppSpacing.md),
            Text(
              'Verified machines and operators, protected payments, live tracking and digital hour logs — for contractors and equipment owners across India.',
              style: theme.textTheme.bodyLarge?.copyWith(color: AppColors.navy100),
            ),
            const Gap(AppSpacing.lg),
            Wrap(spacing: AppSpacing.lg, runSpacing: AppSpacing.sm, children: [
              for (final (v, l) in const [('190+', 'machines in Pune'), ('64', 'verified owners'), ('4.7★', 'average rating')])
                Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                  Text(v, style: theme.textTheme.titleLarge?.copyWith(color: AppColors.orange500)),
                  Text(l, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.navy200)),
                ]),
            ]),
          ],
        ),
      ),
    );

    final roles = Padding(
      padding: EdgeInsets.all(wide ? AppSpacing.xxl : AppSpacing.md),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        mainAxisSize: MainAxisSize.min,
        children: [
          Text('How will you use EQUIPRENT?', style: theme.textTheme.headlineSmall),
          const Gap(AppSpacing.xs),
          Text(
            user == null ? '' : 'Signed in as ${user.displayName}. Choose where to work.',
            style: theme.textTheme.bodyMedium?.copyWith(color: AppColors.slate600),
          ),
          const Gap(AppSpacing.lg),
          if (granted.isEmpty)
            const InlineBanner(
              key: ValueKey('no-roles'),
              tone: AppTone.warning,
              message: 'Your account has no role yet. If you were invited as an operator or fleet manager, ask your company admin to check your mobile number.',
            ),
          for (final role in granted)
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: AppCard(
                key: ValueKey('role-${role.name}'),
                semanticLabel: '${role.label}. ${role.description}',
                onTap: () => choose(role),
                child: Row(children: [
                  IconTile(icon: _icon(role), size: 44, tone: role == UserRole.customer ? AppTone.accent : AppTone.brand),
                  const Gap(AppSpacing.md),
                  Expanded(
                    child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                      Text(role.label, style: theme.textTheme.titleMedium),
                      Text(role.description, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate600)),
                    ]),
                  ),
                  const Icon(AppIcons.chevronRight, color: AppColors.slate400),
                ]),
              ),
            ),
          const Gap(AppSpacing.md),
          Wrap(alignment: WrapAlignment.center, spacing: AppSpacing.sm, children: [
            TextButton.icon(
              key: const ValueKey('open-account'),
              onPressed: () => context.go(AuthRoutes.account),
              icon: const Icon(AppIcons.shield, size: 18),
              label: const Text('Account & security'),
            ),
            TextButton.icon(
              key: const ValueKey('role-sign-out'),
              onPressed: () => ref.read(authControllerProvider.notifier).logout(),
              icon: const Icon(AppIcons.logout, size: 18),
              label: const Text('Sign out'),
            ),
          ]),
        ],
      ),
    );

    return Scaffold(
      backgroundColor: AppColors.canvas,
      body: wide
          ? Row(children: [
              Expanded(flex: 5, child: SizedBox.expand(child: hero)),
              Expanded(flex: 6, child: Center(child: SingleChildScrollView(child: ConstrainedBox(constraints: const BoxConstraints(maxWidth: 620), child: roles)))),
            ])
          : SingleChildScrollView(child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [hero, roles])),
    );
  }

  static IconData _icon(UserRole r) => switch (r) {
        UserRole.customer => AppIcons.search,
        UserRole.owner => AppIcons.building,
        UserRole.operator => AppIcons.operator,
        UserRole.fleetManager => AppIcons.fleet,
        UserRole.admin => AppIcons.shield,
      };
}
