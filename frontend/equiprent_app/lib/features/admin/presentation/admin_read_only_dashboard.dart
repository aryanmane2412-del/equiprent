import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';

typedef AdminMonitorJson = Map<String, dynamic>;
AdminMonitorJson _map(Object? value) =>
    value is Map ? value.cast<String, dynamic>() : <String, dynamic>{};

class AdminMonitorSnapshot {
  const AdminMonitorSnapshot(this.values);
  factory AdminMonitorSnapshot.fromJson(Object? value) =>
      AdminMonitorSnapshot(_map(value));
  final AdminMonitorJson values;
  int count(String key) => (values[key] as num?)?.toInt() ?? 0;
  int paise(String key) => (values[key] as num?)?.toInt() ?? 0;
}

final adminMonitorProvider = FutureProvider.autoDispose<AdminMonitorSnapshot>(
  (ref) async => (await ref.watch(
    adminAnalyticsProvider(AdminReportRange.last30Days()).future,
  )).summary,
);

@immutable
class AdminReportRange {
  const AdminReportRange(this.from, this.to);

  factory AdminReportRange.last30Days() {
    final to = DateTime.now().toUtc();
    return AdminReportRange(to.subtract(const Duration(days: 30)), to);
  }

  final DateTime from;
  final DateTime to;

  @override
  bool operator ==(Object other) =>
      other is AdminReportRange && other.from == from && other.to == to;

  @override
  int get hashCode => Object.hash(from, to);
}

class AdminAnalyticsSnapshot {
  const AdminAnalyticsSnapshot({
    required this.summary,
    required this.categories,
    required this.trends,
  });

  factory AdminAnalyticsSnapshot.fromJson(Object? value) {
    final root = _map(value);
    return AdminAnalyticsSnapshot(
      summary: AdminMonitorSnapshot(_map(root['summary'])),
      categories: [
        for (final item in (root['booking_categories'] as List? ?? const []))
          _map(item),
      ],
      trends: [
        for (final item in (root['booking_trends'] as List? ?? const []))
          _map(item),
      ],
    );
  }

  final AdminMonitorSnapshot summary;
  final List<Map<String, dynamic>> categories;
  final List<Map<String, dynamic>> trends;
}

final adminAnalyticsProvider = FutureProvider.autoDispose
    .family<AdminAnalyticsSnapshot, AdminReportRange>(
      (ref, range) => ref
          .watch(apiClientProvider)
          .get(
            '/admin/read-only-dashboard',
            query: {
              'from': range.from.toUtc().toIso8601String(),
              'to': range.to.toUtc().toIso8601String(),
            },
            decode: AdminAnalyticsSnapshot.fromJson,
          ),
    );

class AdminReadOnlyDashboardPage extends ConsumerWidget {
  const AdminReadOnlyDashboardPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final data = ref.watch(adminMonitorProvider);
    return AppPage(
      title: 'Read-only platform monitor',
      subtitle: 'Verified backend counts and ledger totals',
      body: AsyncBody(
        value: data,
        onRetry: () => ref.invalidate(adminMonitorProvider),
        builder: (s) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(adminMonitorProvider),
          child: PageBody(
            children: [
              const InlineBanner(
                tone: AppTone.neutral,
                icon: AppIcons.shield,
                title: 'Monitoring access only',
                message:
                    'This dashboard has no controls to edit users, listings, bookings, assignments or transactions.',
              ),
              const Gap(AppSpacing.md),
              ResponsiveGrid(
                columns: const Responsive(compact: 2, medium: 3, expanded: 4),
                children: [
                  StatCard(
                    label: 'Equipment owners',
                    value: Fmt.number(s.count('owners')),
                    icon: AppIcons.users,
                  ),
                  StatCard(
                    label: 'Customers',
                    value: Fmt.number(s.count('customers')),
                    icon: AppIcons.users,
                  ),
                  StatCard(
                    label: 'Drivers',
                    value: Fmt.number(s.count('drivers')),
                    icon: AppIcons.users,
                  ),
                  StatCard(
                    label: 'Equipment listed',
                    value: Fmt.number(s.count('equipment')),
                    icon: AppIcons.fleet,
                  ),
                  StatCard(
                    label: 'Total bookings',
                    value: Fmt.number(s.count('bookings')),
                    icon: AppIcons.bookings,
                  ),
                  StatCard(
                    label: 'Pending bookings',
                    value: Fmt.number(s.count('pending_bookings')),
                    icon: AppIcons.clock,
                    tone: AppTone.warning,
                  ),
                  StatCard(
                    label: 'Completed bookings',
                    value: Fmt.number(s.count('completed_bookings')),
                    icon: AppIcons.checkCircle,
                    tone: AppTone.success,
                  ),
                  StatCard(
                    label: 'Transactions',
                    value: Fmt.number(s.count('transactions')),
                    icon: AppIcons.payments,
                  ),
                  StatCard(
                    label: 'Successful customer payments',
                    value: Fmt.inrCompact(
                      s.paise('successful_customer_payments_paise') / 100,
                    ),
                    icon: AppIcons.payments,
                    tone: AppTone.success,
                  ),
                  StatCard(
                    label: 'Platform revenue',
                    value: Fmt.inrCompact(
                      s.paise('platform_revenue_paise') / 100,
                    ),
                    icon: AppIcons.earnings,
                  ),
                  StatCard(
                    label: 'Driver payouts',
                    value: Fmt.inrCompact(
                      s.paise('driver_payouts_paise') / 100,
                    ),
                    icon: AppIcons.payments,
                  ),
                  StatCard(
                    label: 'Refunds',
                    value: Fmt.inrCompact(s.paise('refunds_paise') / 100),
                    icon: AppIcons.history,
                    tone: AppTone.info,
                  ),
                  StatCard(
                    label: 'Recorded platform expenses',
                    value: Fmt.inrCompact(
                      s.paise('platform_expenses_paise') / 100,
                    ),
                    icon: AppIcons.earnings,
                  ),
                  StatCard(
                    label: 'Net platform profit',
                    value: Fmt.inrCompact(
                      s.paise('net_platform_profit_paise') / 100,
                    ),
                    icon: AppIcons.earnings,
                    tone: AppTone.success,
                    caption:
                        'Ledger revenue less recorded expenses, refunds and driver payouts',
                  ),
                  StatCard(
                    label: 'Pending settlements',
                    value: Fmt.number(s.count('pending_settlements')),
                    icon: AppIcons.settlements,
                    tone: AppTone.warning,
                  ),
                ],
              ),
              const Gap(AppSpacing.md),
              const Text(
                'Financial values are in INR and come from captured payment, processed refund, driver compensation and double-entry ledger records. No profit is inferred from rental booking value.',
              ),
            ],
          ),
        ),
      ),
    );
  }
}
