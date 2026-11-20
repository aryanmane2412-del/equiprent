import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import 'admin_read_only_dashboard.dart';

class AdminPaymentRow {
  const AdminPaymentRow({
    required this.id,
    required this.bookingReference,
    required this.amountPaise,
    required this.status,
    required this.purpose,
    required this.gateway,
    required this.mode,
    required this.createdAt,
    this.capturedAt,
  });

  factory AdminPaymentRow.fromJson(Map<String, dynamic> json) =>
      AdminPaymentRow(
        id: '${json['id']}',
        bookingReference: '${json['booking_reference'] ?? ''}',
        amountPaise: (json['amount_paise'] as num?)?.toInt() ?? 0,
        status: '${json['status'] ?? 'unknown'}',
        purpose: '${json['purpose'] ?? ''}',
        gateway: '${json['gateway'] ?? ''}',
        mode: '${json['gateway_mode'] ?? ''}',
        createdAt: DateTime.parse('${json['created_at']}').toLocal(),
        capturedAt: json['captured_at'] == null
            ? null
            : DateTime.parse('${json['captured_at']}').toLocal(),
      );

  final String id;
  final String bookingReference;
  final int amountPaise;
  final String status;
  final String purpose;
  final String gateway;
  final String mode;
  final DateTime createdAt;
  final DateTime? capturedAt;
}

final adminPaymentHistoryProvider = FutureProvider.autoDispose
    .family<List<AdminPaymentRow>, AdminReportRange>((ref, range) async {
      final result = await ref
          .watch(apiClientProvider)
          .get(
            '/admin/read-only-payments',
            query: {
              'from': range.from.toUtc().toIso8601String(),
              'to': range.to.toUtc().toIso8601String(),
              'limit': 100,
            },
            decode: (value) => value as Map<String, dynamic>,
          );
      final data = result['data'] as List? ?? const [];
      return [
        for (final row in data)
          AdminPaymentRow.fromJson((row as Map).cast<String, dynamic>()),
      ];
    });

class AdminPaymentsPage extends ConsumerStatefulWidget {
  const AdminPaymentsPage({super.key});

  @override
  ConsumerState<AdminPaymentsPage> createState() => _AdminPaymentsPageState();
}

class _AdminPaymentsPageState extends ConsumerState<AdminPaymentsPage> {
  String? _status;
  final _range = AdminReportRange.last30Days();

  @override
  Widget build(BuildContext context) {
    final history = ref.watch(adminPaymentHistoryProvider(_range));
    return AppPage(
      title: 'Payments',
      subtitle: 'Read-only payment history · latest 100 records',
      body: AsyncBody(
        value: history,
        onRetry: () => ref.invalidate(adminPaymentHistoryProvider(_range)),
        builder: (all) {
          final rows = all
              .where((payment) => _status == null || payment.status == _status)
              .toList();
          int total(String status) => all
              .where((p) => p.status == status)
              .fold(0, (sum, payment) => sum + payment.amountPaise);
          return RefreshIndicator(
            onRefresh: () async =>
                ref.invalidate(adminPaymentHistoryProvider(_range)),
            child: PageBody(
              children: [
                ResponsiveGrid(
                  columns: const Responsive(compact: 2, expanded: 4),
                  children: [
                    StatCard(
                      label: 'Captured',
                      value: Fmt.inrCompact(total('captured') / 100),
                      icon: AppIcons.checkCircle,
                      tone: AppTone.success,
                    ),
                    StatCard(
                      label: 'Pending',
                      value: Fmt.inrCompact(total('pending') / 100),
                      icon: AppIcons.hourglass,
                      tone: AppTone.warning,
                    ),
                    StatCard(
                      label: 'Refunded',
                      value: Fmt.inrCompact(total('refunded') / 100),
                      icon: AppIcons.refresh,
                      tone: AppTone.info,
                    ),
                    StatCard(
                      label: 'Failed',
                      value: Fmt.inrCompact(total('failed') / 100),
                      icon: AppIcons.error,
                      tone: AppTone.danger,
                    ),
                  ],
                ),
                const Gap(AppSpacing.md),
                AppChoiceChips<String?>(
                  options: const [
                    null,
                    'captured',
                    'pending',
                    'refunded',
                    'failed',
                  ],
                  selected: _status,
                  onSelected: (value) => setState(() => _status = value),
                  labelOf: (value) => value ?? 'All',
                ),
                const Gap(AppSpacing.md),
                AppDataTable<AdminPaymentRow>(
                  rows: rows,
                  emptyMessage: 'No payment records in this date range.',
                  columns: [
                    TableColumnDef(
                      label: 'Txn',
                      cell: (p) => Text(p.id.toUpperCase()),
                    ),
                    TableColumnDef(
                      label: 'Booking',
                      cell: (p) => Text(p.bookingReference),
                    ),
                    TableColumnDef(
                      label: 'Purpose',
                      cell: (p) => Text(p.purpose),
                    ),
                    TableColumnDef(
                      label: 'Gateway',
                      cell: (p) => Text('${p.gateway} · ${p.mode}'),
                      showOnCompact: false,
                    ),
                    TableColumnDef(
                      label: 'Created',
                      sortKey: (p) => p.capturedAt ?? p.createdAt,
                      cell: (p) =>
                          Text(Fmt.dateTime(p.capturedAt ?? p.createdAt)),
                    ),
                    TableColumnDef(
                      label: 'Amount',
                      numeric: true,
                      sortKey: (p) => p.amountPaise,
                      cell: (p) => Text(Fmt.inr(p.amountPaise / 100)),
                    ),
                    TableColumnDef(
                      label: 'Status',
                      cell: (p) => Text(p.status),
                    ),
                  ],
                ),
              ],
            ),
          );
        },
      ),
    );
  }
}

class AdminSettlementsPage extends ConsumerWidget {
  const AdminSettlementsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final report = ref.watch(adminMonitorProvider);
    return AppPage(
      title: 'Settlements',
      subtitle: 'Read-only totals from recorded owner settlements',
      body: AsyncBody(
        value: report,
        onRetry: () => ref.invalidate(adminMonitorProvider),
        builder: (snapshot) => PageBody(
          children: [
            const InlineBanner(
              tone: AppTone.neutral,
              icon: AppIcons.lock,
              title: 'Monitoring access only',
              message:
                  'Payout status and owner accounts cannot be changed from this dashboard.',
            ),
            const Gap(AppSpacing.md),
            ResponsiveGrid(
              columns: const Responsive(compact: 1, medium: 3),
              children: [
                StatCard(
                  label: 'Recorded owner settlements',
                  value: Fmt.inrCompact(
                    snapshot.paise('owner_settlements_paise') / 100,
                  ),
                  icon: AppIcons.settlements,
                ),
                StatCard(
                  label: 'Pending settlements',
                  value: Fmt.number(snapshot.count('pending_settlements')),
                  icon: AppIcons.pause,
                  tone: AppTone.warning,
                ),
                StatCard(
                  label: 'Platform commissions',
                  value: Fmt.inrCompact(
                    snapshot.paise('platform_revenue_paise') / 100,
                  ),
                  icon: AppIcons.earnings,
                  tone: AppTone.success,
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}
