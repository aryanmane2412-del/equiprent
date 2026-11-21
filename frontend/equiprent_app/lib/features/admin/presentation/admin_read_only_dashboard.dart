import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/network/api_client.dart';
import '../../../core/widgets/widgets.dart';
import '../../verification/data/verification_api.dart';
import '../../verification/data/verification_models.dart';
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
              const _AdminOverviewTop(),
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

// ---- the design's overview: headline numbers, verification queue, disputes (all view-only) --------

/// Platform headline numbers from GET /admin/stats.
class AdminStats {
  const AdminStats({
    this.gmv30dPaise = 0,
    this.bookingsInProgress = 0,
    this.bookingsRequested = 0,
    this.openDisputes = 0,
    this.listingsPendingReview = 0,
  });
  factory AdminStats.fromJson(Object? value) {
    final j = _map(value);
    int i(String k) => (j[k] as num?)?.toInt() ?? 0;
    return AdminStats(
      gmv30dPaise: i('gmv_30d_paise'),
      bookingsInProgress: i('bookings_in_progress'),
      bookingsRequested: i('bookings_requested'),
      openDisputes: i('open_disputes'),
      listingsPendingReview: i('listings_pending_review'),
    );
  }

  final int gmv30dPaise;
  final int bookingsInProgress;
  final int bookingsRequested;
  final int openDisputes;
  final int listingsPendingReview;
}

final adminStatsProvider = FutureProvider.autoDispose<AdminStats>(
  (ref) => ref
      .watch(apiClientProvider)
      .get('/admin/stats', decode: AdminStats.fromJson),
);

/// One open dispute (read-only).
class AdminDisputeRow {
  const AdminDisputeRow({
    required this.id,
    required this.category,
    required this.summary,
    required this.status,
    this.claimedPaise = 0,
    this.openedAt,
  });
  factory AdminDisputeRow.fromJson(Map<String, dynamic> j) => AdminDisputeRow(
    id: '${j['id']}',
    category: '${j['category'] ?? ''}',
    summary: '${j['summary'] ?? ''}',
    status: '${j['status'] ?? ''}',
    claimedPaise: (j['amount_claimed_paise'] as num?)?.toInt() ?? 0,
    openedAt: DateTime.tryParse('${j['opened_at'] ?? ''}'),
  );
  final String id;
  final String category;
  final String summary;
  final String status;
  final int claimedPaise;
  final DateTime? openedAt;
}

/// Disputes still waiting for a decision, newest first.
final adminOpenDisputesProvider =
    FutureProvider.autoDispose<List<AdminDisputeRow>>(
      (ref) => ref
          .watch(apiClientProvider)
          .get(
            '/admin/disputes',
            decode: (value) =>
                [
                      for (final item
                          in (_map(value)['data'] as List? ?? const []))
                        AdminDisputeRow.fromJson(_map(item)),
                    ]
                    .where(
                      (d) => d.status == 'open' || d.status == 'under_review',
                    )
                    .toList(),
          ),
    );

/// The four headline cards, then the verification queue and the open disputes. Nothing here changes
/// anything: the buttons only open the existing review screens. A section a role cannot see (for
/// example the dispute queue for a finance admin) is simply left out.
class _AdminOverviewTop extends ConsumerWidget {
  const _AdminOverviewTop();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final stats = ref.watch(adminStatsProvider).value;
    final queue = ref.watch(
      verificationQueueProvider((ApplicationStatus.submitted, null)),
    );
    final disputes = ref.watch(adminOpenDisputesProvider);
    final pending = queue.value ?? const <ApplicationSummary>[];
    final open = disputes.value ?? const <AdminDisputeRow>[];
    final owners = pending.where((a) => a.role == 'owner').length;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ResponsiveGrid(
          columns: const Responsive(compact: 2, medium: 4, expanded: 4),
          children: [
            StatCard(
              key: const Key('admin-kpi-volume'),
              label: 'Gross volume · 30 days',
              value: stats == null
                  ? '…'
                  : Fmt.inrCompact(stats.gmv30dPaise / 100),
              icon: AppIcons.payments,
              tone: AppTone.success,
            ),
            StatCard(
              key: const Key('admin-kpi-rentals'),
              label: 'Active rentals',
              value: stats == null ? '…' : Fmt.number(stats.bookingsInProgress),
              icon: AppIcons.fleet,
              caption: stats == null
                  ? null
                  : '${stats.bookingsRequested} requests waiting',
            ),
            StatCard(
              key: const Key('admin-kpi-approvals'),
              label: 'Pending approvals',
              value: queue.hasValue ? '${pending.length}' : '…',
              icon: AppIcons.shield,
              tone: AppTone.warning,
              caption: queue.hasValue
                  ? '$owners owners, ${pending.length - owners} others'
                  : null,
            ),
            StatCard(
              key: const Key('admin-kpi-disputes'),
              label: 'Open disputes',
              value: stats == null ? '…' : Fmt.number(stats.openDisputes),
              icon: AppIcons.warning,
              tone: AppTone.danger,
              caption: stats == null
                  ? null
                  : (stats.openDisputes == 0 ? 'All clear' : 'Needs review'),
            ),
          ],
        ),
        const Gap(AppSpacing.lg),
        if (pending.isNotEmpty) ...[
          SectionHeader(
            title: 'Role verification queue',
            actionLabel: 'Open queue',
            onAction: () => context.go('/admin/verifications'),
          ),
          for (final a in pending.take(3))
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: AppCard(
                key: Key('queue-${a.id}'),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            a.subjectName,
                            style: theme.textTheme.titleMedium,
                          ),
                          Text(
                            [
                              'Role: ${a.role}',
                              if (a.submittedAt != null)
                                'submitted ${Fmt.shortDate(a.submittedAt!)}',
                            ].join(' · '),
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: AppColors.slate600,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const Gap(AppSpacing.sm),
                    AppButton(
                      key: Key('inspect-${a.id}'),
                      label: 'Inspect docs',
                      icon: AppIcons.fileText,
                      variant: AppButtonVariant.secondary,
                      size: AppButtonSize.small,
                      onPressed: () =>
                          context.push('/admin/verifications/${a.id}'),
                    ),
                  ],
                ),
              ),
            ),
          const Gap(AppSpacing.md),
        ],
        if (open.isNotEmpty) ...[
          const SectionHeader(title: 'Active disputes'),
          for (final d in open.take(3))
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: AppCard(
                key: Key('dispute-${d.id}'),
                child: Row(
                  children: [
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            d.summary.isEmpty ? d.category : d.summary,
                            style: theme.textTheme.titleMedium,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                          ),
                          Text(
                            [
                              d.category,
                              if (d.claimedPaise > 0)
                                '${Fmt.inr(d.claimedPaise / 100)} contested',
                              if (d.openedAt != null)
                                'opened ${Fmt.shortDate(d.openedAt!)}',
                            ].where((x) => x.isNotEmpty).join(' · '),
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: AppColors.slate600,
                            ),
                          ),
                        ],
                      ),
                    ),
                    const Gap(AppSpacing.sm),
                    StatusBadge(
                      label: d.status == 'under_review' ? 'In review' : 'Open',
                      tone: AppTone.danger,
                    ),
                  ],
                ),
              ),
            ),
          const Gap(AppSpacing.md),
        ],
        const SectionHeader(title: 'Platform monitor'),
      ],
    );
  }
}
