import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../features/comms/comms_routes.dart';
import '../../features/comms/data/comms_api.dart';
import '../auth/auth_controller.dart';
import '../mock/mock_data.dart';
import '../responsive/responsive.dart';
import '../session/active_role_provider.dart';
import '../session/user_role.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import '../localization/app_strings.dart';
import 'buttons.dart';
import 'feedback.dart';
import 'media.dart';

/// Demo identity shown for each role.
extension RolePersona on UserRole {
  String get personaName => switch (this) {
    UserRole.customer => MockData.customerName,
    UserRole.owner => MockData.ownerName,
    UserRole.operator => MockData.operatorName,
    UserRole.fleetManager => MockData.fleetManagerName,
    UserRole.admin => MockData.adminName,
  };

  String get personaOrg => switch (this) {
    UserRole.customer => MockData.customerCompany,
    UserRole.owner => MockData.ownerCompany,
    UserRole.operator => MockData.ownerCompany,
    UserRole.fleetManager => MockData.fleetCompany,
    UserRole.admin => 'EQUIPRENT Operations',
  };
}

/// Standard page: app bar (title, actions, notifications + account on
/// top-level pages) and body. Detail pages get an automatic back button.
class AppPage extends StatelessWidget {
  const AppPage({
    super.key,
    required this.title,
    required this.body,
    this.subtitle,
    this.actions = const [],
    this.floatingActionButton,
    this.bottomBar,
    this.topLevel = true,
    this.bottom,
  });

  final String title;
  final String? subtitle;
  final Widget body;
  final List<Widget> actions;
  final Widget? floatingActionButton;
  final Widget? bottomBar;

  /// Top-level (tab) pages show notifications and the account menu.
  final bool topLevel;
  final PreferredSizeWidget? bottom;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Scaffold(
      appBar: AppBar(
        toolbarHeight: 64,
        automaticallyImplyLeading: !topLevel,
        leadingWidth: topLevel ? 132 : null,
        leading: topLevel
            ? Padding(
                padding: const EdgeInsets.only(left: AppSpacing.sm),
                child: Center(
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                    decoration: BoxDecoration(
                      color: Colors.white,
                      borderRadius: BorderRadius.circular(8),
                    ),
                    child: Image.asset(
                      'assets/images/logo.png',
                      height: 34,
                      semanticLabel: 'EQUIPRENT',
                      errorBuilder: (_, _, _) => const SizedBox.shrink(),
                    ),
                  ),
                ),
              )
            : null,
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text(title, maxLines: 1, overflow: TextOverflow.ellipsis),
            if (subtitle != null)
              Text(
                subtitle!,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: theme.textTheme.bodySmall?.copyWith(
                  color: AppColors.slate500,
                ),
              ),
          ],
        ),
        actions: [
          ...actions,
          if (topLevel) ...[
            const MessagesButton(),
            const CustomerCareButton(),
            const NotificationsButton(),
            const AccountMenuButton(),
          ],
          const SizedBox(width: AppSpacing.sm),
        ],
        bottom: bottom,
      ),
      body: body,
      floatingActionButton: floatingActionButton,
      bottomNavigationBar: bottomBar,
    );
  }
}

/// Section title with optional action link.
class SectionHeader extends StatelessWidget {
  const SectionHeader({
    super.key,
    required this.title,
    this.subtitle,
    this.actionLabel,
    this.onAction,
    this.trailing,
  });

  final String title;
  final String? subtitle;
  final String? actionLabel;
  final VoidCallback? onAction;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.ms),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.end,
        children: [
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: theme.textTheme.titleLarge),
                if (subtitle != null)
                  Text(
                    subtitle!,
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: AppColors.slate500,
                    ),
                  ),
              ],
            ),
          ),
          ?trailing,
          if (actionLabel != null)
            TextButton(
              onPressed: onAction,
              child: Row(
                mainAxisSize: MainAxisSize.min,
                children: [
                  Text(actionLabel!),
                  const SizedBox(width: 4),
                  const Icon(AppIcons.arrowRight, size: 16),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

/// Two panes side by side on wide screens, stacked on narrow ones.
class SplitLayout extends StatelessWidget {
  const SplitLayout({
    super.key,
    required this.main,
    required this.side,
    this.sideWidth = 360,
    this.breakpoint = 960,
  });

  final Widget main;
  final Widget side;
  final double sideWidth;
  final double breakpoint;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, c) {
        if (c.maxWidth < breakpoint) {
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              main,
              const SizedBox(height: AppSpacing.md),
              side,
            ],
          );
        }
        return Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Expanded(child: main),
            const SizedBox(width: AppSpacing.lg),
            SizedBox(width: sideWidth, child: side),
          ],
        );
      },
    );
  }
}

/// Sticky bottom action area (e.g. price + Book now).
class BottomActionBar extends StatelessWidget {
  const BottomActionBar({super.key, required this.child});

  /// Summary (price) + actions. One row on wide screens; on phones the
  /// summary sits above full-width buttons so neither gets squeezed.
  factory BottomActionBar.split({
    Key? key,
    required Widget summary,
    required List<Widget> actions,
  }) => BottomActionBar(
    key: key,
    child: _SplitBar(summary: summary, actions: actions),
  );

  final Widget child;

  @override
  Widget build(BuildContext context) {
    return Container(
      decoration: const BoxDecoration(
        color: AppColors.white,
        border: Border(top: BorderSide(color: AppColors.border)),
        boxShadow: AppShadows.sm,
      ),
      child: SafeArea(
        top: false,
        // heightFactor: 1 shrink-wraps vertically; a plain Center would expand
        // to the full height a Scaffold offers its bottom bar and cover the page.
        child: Align(
          heightFactor: 1,
          child: ConstrainedBox(
            constraints: const BoxConstraints(
              maxWidth: Breakpoints.maxContentWidth,
            ),
            child: Padding(
              padding: EdgeInsets.symmetric(
                horizontal: context.pageGutter,
                vertical: AppSpacing.ms,
              ),
              child: child,
            ),
          ),
        ),
      ),
    );
  }
}

class _SplitBar extends StatelessWidget {
  const _SplitBar({required this.summary, required this.actions});

  final Widget summary;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    if (context.windowSize.isCompact) {
      return Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          summary,
          const SizedBox(height: AppSpacing.sm),
          Row(
            children: [
              for (var i = 0; i < actions.length; i++) ...[
                if (i > 0) const SizedBox(width: AppSpacing.sm),
                Expanded(child: actions[i]),
              ],
            ],
          ),
        ],
      );
    }
    return Row(
      children: [
        Expanded(child: summary),
        for (final a in actions) ...[const SizedBox(width: AppSpacing.sm), a],
      ],
    );
  }
}

/// Conversations shortcut for roles without a chat tab (owner, operator).
class MessagesButton extends ConsumerWidget {
  const MessagesButton({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final role = ref.watch(activeRoleProvider);
    // On phones the app bar has no room; owners and operators open
    // messages from their profile or a booking instead.
    if (role == null ||
        role == UserRole.customer ||
        !CommsRoutes.hasMessages(role) ||
        context.windowSize.isCompact) {
      return const SizedBox.shrink();
    }
    final unread = ref.watch(unreadMessagesProvider).value ?? 0;
    return AppIconButton(
      key: const Key('messages-button'),
      icon: AppIcons.chat,
      tooltip: unread > 0 ? 'Messages, $unread unread' : 'Messages',
      badgeCount: unread,
      onPressed: () => context.push(CommsRoutes.messages(role)),
    );
  }
}

/// Bell with the unread count; opens the role's notification centre.
class NotificationsButton extends ConsumerWidget {
  const NotificationsButton({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final unread = ref.watch(unreadNotificationsProvider).value ?? 0;
    final role = ref.watch(activeRoleProvider);
    return AppIconButton(
      key: const Key('notifications-button'),
      icon: AppIcons.bell,
      tooltip: unread > 0 ? 'Notifications, $unread unread' : 'Notifications',
      badgeCount: unread,
      onPressed: role == null
          ? null
          : () => context.push(CommsRoutes.notifications(role)),
    );
  }
}

/// Opens a customer-to-EQUIPRENT-support conversation from every role header.
class CustomerCareButton extends ConsumerWidget {
  const CustomerCareButton({super.key});

  Future<void> _open(BuildContext context, WidgetRef ref, UserRole role) async {
    if (role == UserRole.admin) {
      context.push(CommsRoutes.messages(role));
      return;
    }
    try {
      final conversation = await ref
          .read(commsApiProvider)
          .openSupportConversation();
      if (context.mounted) {
        context.push(CommsRoutes.thread(role, conversation.id));
      }
    } catch (_) {
      if (context.mounted) {
        AppToast.error(
          context,
          'Customer care is temporarily unavailable. Please try again.',
        );
      }
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final role = ref.watch(activeRoleProvider);
    return AppIconButton(
      key: const Key('customer-care-button'),
      icon: AppIcons.support,
      tooltip: 'Customer care',
      onPressed: role == null ? null : () => _open(context, ref, role),
    );
  }
}

/// Avatar menu: identity, account security, design system, sign out.
class AccountMenuButton extends ConsumerWidget {
  const AccountMenuButton({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final role = ref.watch(activeRoleProvider) ?? UserRole.customer;
    final name =
        ref.watch(authControllerProvider).user?.displayName ?? role.personaName;
    return PopupMenuButton<String>(
      tooltip: 'Account',
      position: PopupMenuPosition.under,
      offset: const Offset(0, 8),
      onSelected: (v) {
        switch (v) {
          case 'account':
            context.go('/account');
          case 'signout':
            ref.read(authControllerProvider.notifier).logout();
        }
      },
      itemBuilder: (context) => [
        PopupMenuItem<String>(
          enabled: false,
          child: Row(
            children: [
              AppAvatar(name: name, size: 36),
              const SizedBox(width: AppSpacing.ms),
              Flexible(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(name, style: Theme.of(context).textTheme.titleSmall),
                    Text(
                      role.label,
                      style: Theme.of(context).textTheme.bodySmall?.copyWith(
                        color: AppColors.slate500,
                      ),
                    ),
                  ],
                ),
              ),
            ],
          ),
        ),
        const PopupMenuDivider(),
        PopupMenuItem(
          value: 'account',
          child: _MenuRow(icon: AppIcons.shield, label: 'Account & security'),
        ),
        PopupMenuItem(
          value: 'signout',
          child: _MenuRow(
            icon: AppIcons.logout,
            label: localizeRoleLabel(context, 'Sign out'),
          ),
        ),
      ],
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.xs),
        child: AppAvatar(name: name, size: 34),
      ),
    );
  }
}

class _MenuRow extends StatelessWidget {
  const _MenuRow({required this.icon, required this.label});

  final IconData icon;
  final String label;

  @override
  Widget build(BuildContext context) => Row(
    children: [
      Icon(icon, size: 18, color: AppColors.slate600),
      const SizedBox(width: AppSpacing.ms),
      Text(label),
    ],
  );
}
