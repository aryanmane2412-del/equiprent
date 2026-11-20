import 'package:flutter/material.dart';

import '../responsive/responsive.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import 'cards.dart';

/// A destination used by the common dashboard quick-action panel.
@immutable
class DashboardAction {
  const DashboardAction({
    required this.label,
    required this.icon,
    required this.onPressed,
  });

  final String label;
  final IconData icon;
  final VoidCallback onPressed;
}

/// Shared quick-action treatment used by every role dashboard. Actions are
/// supplied by role pages and should navigate to their real feature routes.
class DashboardQuickActions extends StatelessWidget {
  const DashboardQuickActions({super.key, required this.actions});

  final List<DashboardAction> actions;

  @override
  Widget build(BuildContext context) => SectionCard(
    title: 'Quick actions',
    icon: AppIcons.dashboard,
    child: ResponsiveGrid(
      columns: const Responsive(compact: 2, medium: 3, expanded: 4),
      children: [
        for (final action in actions)
          AppCard(
            onTap: action.onPressed,
            padding: const EdgeInsets.all(AppSpacing.ms),
            color: AppColors.slate50,
            child: Row(
              children: [
                IconTile(icon: action.icon, size: 36),
                const SizedBox(width: AppSpacing.sm),
                Expanded(
                  child: Text(
                    action.label,
                    maxLines: 2,
                    overflow: TextOverflow.ellipsis,
                    style: Theme.of(context).textTheme.labelLarge,
                  ),
                ),
              ],
            ),
          ),
      ],
    ),
  );
}
