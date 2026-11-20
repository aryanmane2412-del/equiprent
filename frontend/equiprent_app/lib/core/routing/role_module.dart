import 'package:flutter/widgets.dart';
import 'package:go_router/go_router.dart';

import '../session/user_role.dart';
import '../widgets/role_shell.dart';

/// One navigation destination inside a role's shell (a bottom-nav tab on
/// phones, a side-nav item on tablets and desktop).
@immutable
class RoleDestination {
  const RoleDestination({
    required this.path,
    required this.label,
    required this.icon,
    required this.builder,
    this.alsoMatches = const [],
  });

  final String path;
  final String label;
  final IconData icon;
  final WidgetBuilder builder;

  /// Other route prefixes that belong to this tab (e.g. detail pages), so
  /// the navigation highlights the right item.
  final List<String> alsoMatches;

  /// Length of the longest prefix of [location] owned by this tab, or -1.
  int matchLength(String location) {
    var best = -1;
    for (final p in [path, ...alsoMatches]) {
      if ((location == p || location.startsWith('$p/')) && p.length > best) best = p.length;
    }
    return best;
  }
}

/// A feature module for one role: its navigation and routes. Each role's
/// folder under lib/features exposes one of these; the app router mounts
/// them all.
@immutable
class RoleModule {
  const RoleModule({required this.role, required this.destinations, this.extraRoutes = const []});

  final UserRole role;
  final List<RoleDestination> destinations;

  /// Detail routes that live inside the shell but are not tabs
  /// (e.g. `/owner/equipment/:id`). Must use paths under the role prefix.
  final List<RouteBase> extraRoutes;

  ShellRoute toShellRoute() {
    return ShellRoute(
      builder: (context, state, child) => RoleShell(role: role, destinations: destinations, child: child),
      routes: [
        for (final d in destinations)
          GoRoute(
            path: d.path,
            pageBuilder: (context, state) => NoTransitionPage(key: state.pageKey, child: d.builder(context)),
          ),
        ...extraRoutes,
      ],
    );
  }
}
