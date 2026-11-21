import 'dart:math' as math;

import 'package:flutter/foundation.dart' show listEquals;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../auth/auth_controller.dart';
import '../responsive/responsive.dart';
import '../localization/app_strings.dart';
import '../routing/role_module.dart';
import '../session/user_role.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import 'brand_logo.dart';
import 'media.dart';
import 'overlays.dart';

/// The sections of a role that are not bottom tabs on phones. The account menu lists them there.
class RoleNavScope extends InheritedWidget {
  const RoleNavScope({super.key, required this.others, required super.child});

  final List<RoleDestination> others;

  static RoleNavScope? maybeOf(BuildContext context) =>
      context.dependOnInheritedWidgetOfExactType<RoleNavScope>();

  @override
  bool updateShouldNotify(RoleNavScope oldWidget) =>
      !listEquals(others, oldWidget.others);
}

/// Role-specific navigation frame:
/// - compact (phones): bottom NavigationBar; > 5 destinations → 4 + "More"
/// - medium (tablets): collapsed side rail (icons + tooltips)
/// - expanded+ (desktop dashboards): full side navigation with labels
class RoleShell extends ConsumerWidget {
  const RoleShell({
    super.key,
    required this.role,
    required this.destinations,
    required this.child,
  });

  final UserRole role;
  final List<RoleDestination> destinations;
  final Widget child;

  static const _maxBottomItems = 5;

  int _selectedIndex(String location) {
    var best = 0;
    var bestLength = -1;
    for (var i = 0; i < destinations.length; i++) {
      final len = destinations[i].matchLength(location);
      if (len > bestLength) {
        best = i;
        bestLength = len;
      }
    }
    return best;
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selected = _selectedIndex(GoRouterState.of(context).uri.path);
    void go(int i) => context.go(destinations[i].path);
    final size = context.windowSize;
    // Pages get their own messenger, so toasts appear inside the page's
    // Scaffold, floating above its sticky action bar instead of covering it.
    final child = ScaffoldMessenger(child: this.child);

    if (size.isCompact) {
      final tabs = destinations.where((d) => d.phoneOrder != null).toList()
        ..sort((a, b) => a.phoneOrder!.compareTo(b.phoneOrder!));
      if (tabs.isNotEmpty) return _phoneTabs(context, tabs, child);
      final overflow = destinations.length > _maxBottomItems;
      final visible = overflow
          ? destinations.take(_maxBottomItems - 1).toList()
          : destinations;
      final selectedVisible = selected < visible.length
          ? selected
          : visible.length; // "More"
      return Scaffold(
        body: child,
        bottomNavigationBar: DecoratedBox(
          decoration: const BoxDecoration(
            border: Border(top: BorderSide(color: AppColors.border)),
          ),
          child: NavigationBar(
            selectedIndex: selectedVisible,
            onDestinationSelected: (i) {
              if (overflow && i == visible.length) {
                _showMore(context, visible.length, selected);
              } else {
                go(i);
              }
            },
            destinations: [
              for (final d in visible)
                NavigationDestination(
                  icon: Icon(d.icon),
                  label: localizeRoleLabel(context, d.label),
                  tooltip: localizeRoleLabel(context, d.label),
                ),
              if (overflow)
                NavigationDestination(
                  icon: Icon(AppIcons.more),
                  label: localizeRoleLabel(context, 'More'),
                  tooltip: localizeRoleLabel(context, 'More'),
                ),
            ],
          ),
        ),
      );
    }

    final extended = size.atLeast(WindowSize.expanded);
    return Scaffold(
      backgroundColor: AppColors.canvas,
      body: Row(
        children: [
          _SideNav(
            role: role,
            destinations: destinations,
            selected: selected,
            extended: extended,
            onSelect: go,
          ),
          const VerticalDivider(width: 1),
          Expanded(child: child),
        ],
      ),
    );
  }

  /// Phones: exactly the role's designed tabs. Every other section is in the account menu.
  Widget _phoneTabs(
    BuildContext context,
    List<RoleDestination> tabs,
    Widget child,
  ) {
    final path = GoRouterState.of(context).uri.path;
    var selected = -1;
    var bestLength = -1;
    for (var i = 0; i < tabs.length; i++) {
      final len = tabs[i].matchLength(path);
      if (len > bestLength) {
        bestLength = len;
        selected = i;
      }
    }
    final others = destinations.where((d) => d.phoneOrder == null).toList();
    // On a section that is not a tab (e.g. Settings) no tab is current.
    final otherBest = others.fold<int>(
      -1,
      (best, d) => math.max(best, d.matchLength(path)),
    );
    if (otherBest > bestLength) selected = -1;

    final theme = Theme.of(context);
    Widget bar = NavigationBar(
      selectedIndex: math.max(0, selected),
      onDestinationSelected: (i) => context.go(tabs[i].path),
      destinations: [
        for (final d in tabs)
          NavigationDestination(
            key: Key('tab-${d.path}'),
            icon: Icon(d.phoneIcon ?? d.icon),
            label: localizeRoleLabel(context, d.phoneLabel ?? d.label),
            tooltip: localizeRoleLabel(context, d.phoneLabel ?? d.label),
          ),
      ],
    );
    if (selected < 0) {
      bar = NavigationBarTheme(
        data: theme.navigationBarTheme.copyWith(
          indicatorColor: Colors.transparent,
          iconTheme: const WidgetStatePropertyAll(
            IconThemeData(color: AppColors.slate500),
          ),
          labelTextStyle: WidgetStatePropertyAll(
            theme.textTheme.labelMedium?.copyWith(color: AppColors.slate500),
          ),
        ),
        child: bar,
      );
    }
    return RoleNavScope(
      others: others,
      child: Scaffold(
        body: child,
        bottomNavigationBar: DecoratedBox(
          decoration: const BoxDecoration(
            border: Border(top: BorderSide(color: AppColors.border)),
          ),
          child: bar,
        ),
      ),
    );
  }

  Future<void> _showMore(BuildContext context, int start, int selected) async {
    final index = await showActionSheet<int>(
      context,
      title: 'More',
      actions: [
        for (var i = start; i < destinations.length; i++)
          SheetAction(
            value: i,
            label: localizeRoleLabel(context, destinations[i].label),
            icon: destinations[i].icon,
            subtitle: i == selected
                ? localizeRoleLabel(context, 'Current page')
                : null,
          ),
      ],
    );
    if (index != null && context.mounted) context.go(destinations[index].path);
  }
}

class _SideNav extends ConsumerWidget {
  const _SideNav({
    required this.role,
    required this.destinations,
    required this.selected,
    required this.extended,
    required this.onSelect,
  });

  final UserRole role;
  final List<RoleDestination> destinations;
  final int selected;
  final bool extended;
  final ValueChanged<int> onSelect;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final user = ref.watch(authControllerProvider).user;
    return AnimatedContainer(
      duration: AppMotion.medium,
      curve: AppMotion.emphasized,
      width: extended ? AppSizes.sideNavWidth : AppSizes.sideNavCollapsed,
      color: AppColors.navy950,
      child: SafeArea(
        right: false,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Padding(
              padding: EdgeInsets.fromLTRB(
                extended ? AppSpacing.md : AppSpacing.md + 4,
                AppSpacing.md,
                AppSpacing.md,
                AppSpacing.sm,
              ),
              child: extended
                  ? const BrandLogo(compact: true, onDark: true)
                  : const Align(
                      alignment: Alignment.centerLeft,
                      child: BrandLogo(
                        compact: true,
                        onDark: true,
                        showTagline: false,
                      ),
                    ),
            ),
            if (extended)
              Padding(
                padding: const EdgeInsets.fromLTRB(
                  AppSpacing.md,
                  AppSpacing.sm,
                  AppSpacing.md,
                  AppSpacing.xs,
                ),
                child: Text(
                  role.label.toUpperCase(),
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: AppColors.navy200,
                  ),
                ),
              ),
            Expanded(
              child: ListView(
                padding: const EdgeInsets.symmetric(
                  horizontal: AppSpacing.sm,
                  vertical: AppSpacing.xs,
                ),
                children: [
                  for (var i = 0; i < destinations.length; i++)
                    _NavItem(
                      destination: destinations[i],
                      selected: i == selected,
                      extended: extended,
                      onTap: () => onSelect(i),
                    ),
                ],
              ),
            ),
            const Divider(color: AppColors.navy800),
            if (extended)
              Padding(
                padding: const EdgeInsets.fromLTRB(
                  AppSpacing.md,
                  0,
                  AppSpacing.md,
                  AppSpacing.md,
                ),
                child: Row(
                  children: [
                    AppAvatar(name: user?.displayName ?? 'User', size: 34),
                    const SizedBox(width: AppSpacing.sm),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            user?.displayName ?? '',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.labelLarge?.copyWith(
                              color: AppColors.white,
                            ),
                          ),
                          Text(
                            user?.orgs[role] ??
                                user?.email ??
                                user?.phone ??
                                '',
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: AppColors.navy200,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}

class _NavItem extends StatelessWidget {
  const _NavItem({
    required this.destination,
    required this.selected,
    required this.extended,
    required this.onTap,
  });

  final RoleDestination destination;
  final bool selected;
  final bool extended;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final fg = selected ? AppColors.navy950 : AppColors.navy200;
    final item = Semantics(
      selected: selected,
      button: true,
      label: localizeRoleLabel(context, destination.label),
      excludeSemantics: true,
      child: Material(
        color: selected ? AppColors.orange500 : Colors.transparent,
        borderRadius: AppRadius.control,
        child: InkWell(
          borderRadius: AppRadius.control,
          hoverColor: selected ? AppColors.orange500 : AppColors.navy800,
          onTap: onTap,
          child: SizedBox(
            height: 44,
            child: Row(
              mainAxisAlignment: extended
                  ? MainAxisAlignment.start
                  : MainAxisAlignment.center,
              children: [
                if (extended) const SizedBox(width: AppSpacing.ms),
                Icon(destination.icon, size: 20, color: fg),
                if (extended) ...[
                  const SizedBox(width: AppSpacing.ms),
                  Expanded(
                    child: Text(
                      localizeRoleLabel(context, destination.label),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: Theme.of(context).textTheme.labelLarge?.copyWith(
                        color: fg,
                        fontWeight: selected
                            ? FontWeight.w600
                            : FontWeight.w500,
                      ),
                    ),
                  ),
                ],
              ],
            ),
          ),
        ),
      ),
    );
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 2),
      child: extended
          ? item
          : Tooltip(
              message: localizeRoleLabel(context, destination.label),
              child: item,
            ),
    );
  }
}
