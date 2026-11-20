import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../responsive/responsive.dart';
import '../localization/app_strings.dart';
import '../routing/role_module.dart';
import '../session/user_role.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import 'brand_logo.dart';
import 'layout.dart';
import 'media.dart';
import 'overlays.dart';

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

class _SideNav extends StatelessWidget {
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
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
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
                    AppAvatar(name: role.personaName, size: 34),
                    const SizedBox(width: AppSpacing.sm),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            role.personaName,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.labelLarge?.copyWith(
                              color: AppColors.white,
                            ),
                          ),
                          Text(
                            role.personaOrg,
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
