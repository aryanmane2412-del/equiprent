import 'package:flutter/material.dart';

/// The five EQUIPRENT roles (docs/PROJECT_PLAN.md §5). Each role owns a URL
/// prefix, and the router only lets the active role into its own prefix.
enum UserRole {
  customer(
    pathSegment: 'customer',
    label: 'Customer',
    description: 'Find and rent machinery for your site',
    icon: Icons.construction,
  ),
  owner(
    pathSegment: 'owner',
    label: 'Equipment Owner',
    description: 'List your machines and earn from idle time',
    icon: Icons.precision_manufacturing,
  ),
  operator(
    pathSegment: 'operator',
    label: 'Operator / Driver',
    description: 'See assigned jobs, navigate and log hours',
    icon: Icons.engineering,
  ),
  fleetManager(
    pathSegment: 'fleet',
    label: 'Fleet Manager',
    description: 'Run machines, operators and maintenance',
    icon: Icons.local_shipping,
  ),
  admin(
    pathSegment: 'admin',
    label: 'Platform Admin',
    description: 'Verify, moderate and support the marketplace',
    icon: Icons.admin_panel_settings,
  );

  const UserRole({
    required this.pathSegment,
    required this.label,
    required this.description,
    required this.icon,
  });

  final String pathSegment;
  final String label;
  final String description;
  final IconData icon;

  /// Root route of this role's area, e.g. `/owner`.
  String get basePath => '/$pathSegment';

  /// The role whose area contains [location], or null for shared routes.
  static UserRole? fromLocation(String location) {
    for (final role in UserRole.values) {
      if (location == role.basePath || location.startsWith('${role.basePath}/')) {
        return role;
      }
    }
    return null;
  }
}
