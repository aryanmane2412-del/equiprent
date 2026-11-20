import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import 'admin_read_only_dashboard.dart';

class AdminAnalyticsPage extends ConsumerStatefulWidget {
  const AdminAnalyticsPage({super.key});

  @override
  ConsumerState<AdminAnalyticsPage> createState() => _AdminAnalyticsPageState();
}

class _AdminAnalyticsPageState extends ConsumerState<AdminAnalyticsPage> {
  String _range = 'This Month';
  AdminReportRange _bounds = _calendarRange('This Month');
  bool _table = false;

  static AdminReportRange _calendarRange(String range) {
    final now = DateTime.now();
    final today = DateTime(now.year, now.month, now.day);
    late final DateTime from;
    late final DateTime to;
    switch (range) {
      case 'Today':
        from = today;
        to = today.add(const Duration(days: 1));
      case 'This Week':
        from = today.subtract(Duration(days: today.weekday - 1));
        to = from.add(const Duration(days: 7));
      case 'This Year':
        from = DateTime(now.year);
        to = DateTime(now.year + 1);
      default:
        from = DateTime(now.year, now.month);
        to = DateTime(now.year, now.month + 1);
    }
    return AdminReportRange(from.toUtc(), to.toUtc());
  }

  Future<void> _chooseCustomRange() async {
    final now = DateTime.now();
    final selected = await showDateRangePicker(
      context: context,
      firstDate: DateTime(2018),
      lastDate: now,
      initialDateRange: DateTimeRange(
        start: now.subtract(const Duration(days: 6)),
        end: now,
      ),
    );
    if (selected == null || !mounted) return;
    setState(() {
      _range = 'Custom';
      _bounds = AdminReportRange(
        DateTime(
          selected.start.year,
          selected.start.month,
          selected.start.day,
        ).toUtc(),
        DateTime(
          selected.end.year,
          selected.end.month,
          selected.end.day,
        ).add(const Duration(days: 1)).toUtc(),
      );
    });
  }

  void _selectRange(String value) {
    if (value == 'Custom') {
      _chooseCustomRange();
      return;
    }
    setState(() {
      _range = value;
      _bounds = _calendarRange(value);
    });
  }

  @override
  Widget build(BuildContext context) {
    final report = ref.watch(adminAnalyticsProvider(_bounds));
    return AppPage(
      title: 'Analytics',
      subtitle: 'Read-only reports from payments and the financial ledger',
      body: AsyncBody(
        value: report,
        onRetry: () => ref.invalidate(adminAnalyticsProvider(_bounds)),
        builder: (data) {
          final summary = data.summary;
          final trendPoints = [
            for (final point in data.trends)
              SeriesPoint(
                _monthLabel(point['month']),
                ((point['bookings'] as num?) ?? 0).toDouble(),
              ),
          ];
          final categories = [
            for (final item in data.categories)
              SeriesPoint(
                '${item['category'] ?? 'Other'}',
                ((item['bookings'] as num?) ?? 0).toDouble(),
              ),
          ];

          return RefreshIndicator(
            onRefresh: () async =>
                ref.invalidate(adminAnalyticsProvider(_bounds)),
            child: PageBody(
              children: [
                Wrap(
                  spacing: AppSpacing.sm,
                  runSpacing: AppSpacing.sm,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    for (final choice in const [
                      'Today',
                      'This Week',
                      'This Month',
                      'This Year',
                      'Custom',
                    ])
                      ChoiceChip(
                        label: Text(choice),
                        selected: _range == choice,
                        onSelected: (_) => _selectRange(choice),
                      ),
                    TextButton.icon(
                      onPressed: () => setState(() => _table = !_table),
                      icon: Icon(_table ? AppIcons.chartBars : AppIcons.list),
                      label: Text(_table ? 'Show charts' : 'Show as table'),
                    ),
                  ],
                ),
                const Gap(AppSpacing.md),
                ResponsiveGrid(
                  columns: const Responsive(compact: 2, medium: 3, expanded: 4),
                  children: [
                    StatCard(
                      label: 'Bookings',
                      value: Fmt.number(summary.count('bookings')),
                      icon: AppIcons.bookings,
                    ),
                    StatCard(
                      label: 'Customer payments',
                      value: Fmt.inrCompact(
                        summary.paise('successful_customer_payments_paise') /
                            100,
                      ),
                      icon: AppIcons.payments,
                    ),
                    StatCard(
                      label: 'Platform commission and fees',
                      value: Fmt.inrCompact(
                        summary.paise('platform_revenue_paise') / 100,
                      ),
                      icon: AppIcons.earnings,
                      tone: AppTone.success,
                    ),
                    StatCard(
                      label: 'Net platform profit',
                      value: Fmt.inrCompact(
                        summary.paise('net_platform_profit_paise') / 100,
                      ),
                      icon: AppIcons.earnings,
                      tone: AppTone.brand,
                    ),
                    StatCard(
                      label: 'Owner settlements',
                      value: Fmt.inrCompact(
                        summary.paise('owner_settlements_paise') / 100,
                      ),
                      icon: AppIcons.settlements,
                    ),
                    StatCard(
                      label: 'Refunds',
                      value: Fmt.inrCompact(
                        summary.paise('refunds_paise') / 100,
                      ),
                      icon: AppIcons.history,
                      tone: AppTone.info,
                    ),
                  ],
                ),
                const Gap(AppSpacing.md),
                SplitLayout(
                  sideWidth: 420,
                  main: SectionCard(
                    title: 'Booking trend',
                    subtitle: 'Monthly bookings in the selected date range',
                    icon: AppIcons.chartBars,
                    child: trendPoints.isEmpty
                        ? const EmptyState(
                            icon: AppIcons.chartBars,
                            title: 'No bookings in this range',
                            message: 'Try another date range.',
                          )
                        : _table
                        ? Column(
                            children: [
                              for (final point in trendPoints)
                                InfoRow(
                                  label: point.label,
                                  value: Fmt.number(point.value.toInt()),
                                ),
                            ],
                          )
                        : BarChart(
                            points: trendPoints,
                            height: 240,
                            format: (value) => Fmt.number(value.toInt()),
                          ),
                  ),
                  side: SectionCard(
                    title: 'Bookings by category',
                    subtitle: 'Booking count by equipment category',
                    icon: AppIcons.categories,
                    child: categories.isEmpty
                        ? const EmptyState(
                            icon: AppIcons.categories,
                            title: 'No category data',
                            message: 'No bookings were recorded in this range.',
                          )
                        : _table
                        ? Column(
                            children: [
                              for (final point in categories)
                                InfoRow(
                                  label: point.label,
                                  value: Fmt.number(point.value.toInt()),
                                ),
                            ],
                          )
                        : HorizontalBars(
                            points: categories,
                            format: (value) => Fmt.number(value.toInt()),
                          ),
                  ),
                ),
                const Gap(AppSpacing.md),
                const InlineBanner(
                  tone: AppTone.neutral,
                  icon: AppIcons.lock,
                  title: 'Financial calculation',
                  message:
                      'Profit uses recorded platform commission and fees, less recorded platform expenses and processed refunds. Rental value is not counted as platform profit.',
                ),
              ],
            ),
          );
        },
      ),
    );
  }

  String _monthLabel(Object? value) {
    final date = DateTime.tryParse('$value')?.toLocal();
    if (date == null) return '';
    return '${date.month}/${date.year % 100}';
  }
}
