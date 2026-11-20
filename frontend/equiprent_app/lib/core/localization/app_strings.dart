import 'package:flutter/widgets.dart';

import '../../l10n/app_localizations.dart';

extension AppLocalizationContext on BuildContext {
  AppLocalizations get l10n => AppLocalizations.of(this);
}

/// Translates shared labels that originate in role module metadata.
/// Screen copy should use the typed AppLocalizations getters directly.
String localizeRoleLabel(BuildContext context, String value) {
  final l = context.l10n;
  return switch (value) {
    'Home' => l.home,
    'Search' => l.search,
    'Bookings' => l.bookings,
    'Chat' => l.chat,
    'Profile' => l.profile,
    'More' => l.more,
    'Settings' => l.settings,
    'Current page' => l.currentPage,
    'Sign out' => l.signOut,
    'Sign in' => l.signIn,
    'Dashboard' => l.home,
    'Overview' => l.overview,
    'Equipment' => l.equipment,
    'Listings' || 'Machines' => l.listings,
    'Operators' || 'Drivers' => l.operators,
    'Assignments' => l.assignments,
    'Reports' || 'Analytics' => l.reports,
    'Customers' || 'Users' => l.users,
    'Revenue' || 'Payments' => l.paymentsInvoices,
    'Approvals' => l.approvals,
    'Verifications' => l.verifications,
    'Disputes' => l.disputes,
    'Reviews' => l.reviews,
    'Settlements' => l.settlements,
    'Operations' => l.operations,
    'Compliance' => l.compliance,
    'Today' => l.today,
    'History' => l.history,
    'Verification' => l.verification,
    'Calendar' => l.calendar,
    'Earnings' => l.earnings,
    'Maintenance' => l.maintenance,
    'Read-only monitor' => l.readOnlyMonitor,
    _ => value,
  };
}
