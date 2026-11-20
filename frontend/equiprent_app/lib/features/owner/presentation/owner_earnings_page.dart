import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../data/owner_api.dart';

class OwnerEarningsPage extends ConsumerWidget {
  const OwnerEarningsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) => AppPage(
        title: 'Earnings',
        body: PageBody(children: [
          ref.watch(ownerEarningsProvider).when(
            loading: () => const LinearProgressIndicator(),
            error: (_, _) => const InlineBanner(message: 'Verified settlement data is temporarily unavailable.'),
            data: (e) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              ResponsiveGrid(columns: const Responsive(compact: 2, expanded: 4), children: [
                StatCard(label: 'Gross settled', value: Fmt.inrCompact(e.grossPaise / 100), icon: AppIcons.earnings, tone: AppTone.success),
                StatCard(label: 'Paid out', value: Fmt.inrCompact(e.paidOutPaise / 100), icon: AppIcons.settlements, caption: 'Verified settlements'),
                StatCard(label: 'Pending', value: Fmt.inrCompact(e.pendingPaise / 100), icon: AppIcons.calendar, tone: AppTone.accent, caption: '${e.pendingCount} settlements'),
                StatCard(label: 'On hold', value: Fmt.inrCompact(e.onHoldPaise / 100), icon: AppIcons.lock, tone: AppTone.warning),
              ]),
              const Gap(AppSpacing.lg),
              SectionCard(
                title: 'Monthly net earnings', subtitle: 'From recorded settlements', icon: AppIcons.chartBars,
                child: e.monthly.isEmpty
                    ? const EmptyState(icon: AppIcons.chartBars, title: 'No settlements yet', message: 'Earnings appear after completed rentals are settled.', compact: true)
                    : BarChart(points: [for (final m in e.monthly) SeriesPoint(m.month, m.netPaise / 100)], height: 240, format: (v) => Fmt.inrCompact(v)),
              ),
              const Gap(AppSpacing.md),
              InfoRow(label: 'Commission and GST', value: Fmt.inrCompact(e.commissionPaise / 100)),
              InfoRow(label: 'Tax withheld', value: Fmt.inrCompact(e.withheldPaise / 100)),
              const Gap(AppSpacing.md),
              const SectionHeader(title: 'Settlement history', subtitle: 'Amounts from the owner settlement ledger'),
              if (e.monthly.isEmpty)
                const EmptyState(icon: AppIcons.settlements, title: 'No settlement history', message: 'Settlement records will appear here after rentals complete.', compact: true)
              else
                for (final m in e.monthly.reversed)
                  ListRow(dense: true, title: m.month, subtitle: '${m.settlements} settlements', trailing: Text(Fmt.inrCompact(m.netPaise / 100))),
            ]),
          ),
        ]),
      );
}