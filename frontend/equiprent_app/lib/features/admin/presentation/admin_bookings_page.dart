import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import 'admin_read_only_records.dart';

class AdminBookingsPage extends ConsumerStatefulWidget {
  const AdminBookingsPage({super.key});

  @override
  ConsumerState<AdminBookingsPage> createState() => _AdminBookingsPageState();
}

class _AdminBookingsPageState extends ConsumerState<AdminBookingsPage> {
  String _filter = 'All';
  late DateTime _from = DateTime.now().toUtc().subtract(
    const Duration(days: 30),
  );
  late DateTime _to = DateTime.now().toUtc();

  @override
  Widget build(BuildContext context) {
    final range = (_from.toIso8601String(), _to.toIso8601String());
    final bookings = ref.watch(adminBookingsRecordsProvider(range));
    return AppPage(
      title: 'Bookings',
      subtitle: 'Read-only booking history from PostgreSQL',
      body: AsyncBody(
        value: bookings,
        onRetry: () => ref.invalidate(adminBookingsRecordsProvider(range)),
        builder: (all) {
          final rows = all.where((booking) {
            final status = '${booking['status'] ?? ''}';
            if (_filter == 'All') return true;
            if (_filter == 'Completed') {
              return status == 'completed' || status == 'closed';
            }
            if (_filter == 'Cancelled') return status.contains('cancel');
            if (_filter == 'Live') {
              return const {
                'confirmed',
                'operator_assigned',
                'dispatched',
                'on_site',
                'active',
                'return_initiated',
              }.contains(status);
            }
            return const {
              'requested',
              'payment_pending',
              'accepted',
              'quoted',
            }.contains(status);
          }).toList();
          return RefreshIndicator(
            onRefresh: () async =>
                ref.invalidate(adminBookingsRecordsProvider(range)),
            child: PageBody(
              children: [
                Wrap(
                  spacing: AppSpacing.sm,
                  children: [
                    for (final filter in const [
                      'All',
                      'Awaiting owner',
                      'Live',
                      'Completed',
                      'Cancelled',
                    ])
                      ChoiceChip(
                        label: Text(filter),
                        selected: _filter == filter,
                        onSelected: (_) => setState(() => _filter = filter),
                      ),
                    OutlinedButton.icon(
                      icon: const Icon(AppIcons.calendar),
                      label: Text(
                        '${Fmt.shortDate(_from)} – ${Fmt.shortDate(_to)}',
                      ),
                      onPressed: () async {
                        final now = DateTime.now();
                        final selected = await showDateRangePicker(
                          context: context,
                          firstDate: DateTime(2018),
                          lastDate: now,
                          initialDateRange: DateTimeRange(
                            start: _from.toLocal(),
                            end: _to.toLocal(),
                          ),
                        );
                        if (selected != null && mounted) {
                          setState(() {
                            _from = DateTime(
                              selected.start.year,
                              selected.start.month,
                              selected.start.day,
                            ).toUtc();
                            _to = DateTime(
                              selected.end.year,
                              selected.end.month,
                              selected.end.day,
                            ).add(const Duration(days: 1)).toUtc();
                          });
                        }
                      },
                    ),
                  ],
                ),
                const Gap(AppSpacing.md),
                AppDataTable<AdminRecord>(
                  rows: rows,
                  emptyMessage: 'No bookings in this date range.',
                  columns: [
                    TableColumnDef(
                      label: 'Reference',
                      cell: (b) => Text('${b['reference'] ?? '—'}'),
                    ),
                    TableColumnDef(
                      label: 'Equipment',
                      cell: (b) => Text('${b['equipment_title'] ?? '—'}'),
                    ),
                    TableColumnDef(
                      label: 'Customer',
                      cell: (b) => Text('${b['customer_name'] ?? '—'}'),
                    ),
                    TableColumnDef(
                      label: 'Owner',
                      cell: (b) => Text('${b['owner_name'] ?? '—'}'),
                      showOnCompact: false,
                    ),
                    TableColumnDef(
                      label: 'Date',
                      cell: (b) => Text(
                        Fmt.date(
                          DateTime.parse('${b['created_at']}').toLocal(),
                        ),
                      ),
                    ),
                    TableColumnDef(
                      label: 'Booking value',
                      numeric: true,
                      cell: (b) => Text(
                        Fmt.inr(
                          (((b['estimated_total_paise'] as num?) ?? 0) / 100),
                        ),
                      ),
                    ),
                    TableColumnDef(
                      label: 'Status',
                      cell: (b) => Text('${b['status'] ?? '—'}'),
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
