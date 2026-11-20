import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';

/// Dispute workspace: list on the left, evidence + resolution on the right
/// (stacked on phones).
class AdminDisputesPage extends ConsumerStatefulWidget {
  const AdminDisputesPage({super.key});

  @override
  ConsumerState<AdminDisputesPage> createState() => _AdminDisputesPageState();
}

class _AdminDisputesPageState extends ConsumerState<AdminDisputesPage> {
  String? _selected;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final disputes = ref.watch(disputesProvider);
    final current = disputes.firstWhere(
      (d) => d.id == (_selected ?? disputes.first.id),
      orElse: () => disputes.first,
    );

    final list = Column(
      children: [
        for (final d in disputes)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.sm),
            child: AppCard(
              onTap: () => setState(() => _selected = d.id),
              borderColor: d.id == current.id ? AppColors.navy600 : null,
              color: d.id == current.id ? AppColors.navy50 : null,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    children: [
                      Expanded(
                        child: Text(
                          d.category,
                          style: theme.textTheme.titleSmall,
                        ),
                      ),
                      StatusBadges.dispute(d.status),
                    ],
                  ),
                  Text(
                    '${d.bookingRef} · ${Fmt.inr(d.amount)}',
                    style: theme.textTheme.bodySmall?.copyWith(
                      color: AppColors.slate500,
                    ),
                  ),
                  Text(
                    '${d.raisedBy} vs ${d.against}',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.bodySmall,
                  ),
                ],
              ),
            ),
          ),
      ],
    );

    return AppPage(
      title: 'Disputes',
      subtitle:
          '${disputes.where((d) => d.status != DisputeStatus.resolved).length} open',
      body: PageBody(
        children: [
          SplitLayout(
            breakpoint: 900,
            sideWidth: 360,
            main: _Workspace(dispute: current),
            side: list,
          ),
        ],
      ),
    );
  }
}

class _Workspace extends ConsumerWidget {
  const _Workspace({required this.dispute});

  final Dispute dispute;

  Future<void> _resolve(BuildContext context, WidgetRef ref) async {
    String outcome = 'Partial refund to customer';
    final ok = await showAppSheet<bool>(
      context,
      title: 'Resolve dispute',
      subtitle: '${dispute.bookingRef} · claim ${Fmt.inr(dispute.amount)}',
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, set) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            RadioGroup<String>(
              groupValue: outcome,
              onChanged: (v) => set(() => outcome = v ?? outcome),
              child: Column(
                children: [
                  for (final o in const [
                    'Full refund to customer',
                    'Partial refund to customer',
                    'Charge customer (damage)',
                    'No action — claim rejected',
                  ])
                    RadioListTile<String>(
                      contentPadding: EdgeInsets.zero,
                      value: o,
                      title: Text(o),
                    ),
                ],
              ),
            ),
            const AppTextField(
              label: 'Resolution note (shared with both parties)',
              maxLines: 3,
              initialValue:
                  'Based on the GPS trail and job photos, 4 idle hours are removed from the bill.',
            ),
            const Gap(AppSpacing.lg),
            AppButton(
              label: 'Resolve & post adjustment',
              variant: AppButtonVariant.success,
              expand: true,
              onPressed: () => Navigator.of(ctx).pop(true),
            ),
          ],
        ),
      ),
    );
    if (ok == true && context.mounted) {
      ref.read(disputesProvider.notifier).resolve(dispute.id, outcome);
      AppToast.success(
        context,
        'Dispute resolved: $outcome. Ledger adjustment posted (demo).',
      );
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final d = dispute;
    return SectionCard(
      title: '${d.category} · ${d.bookingRef}',
      subtitle: 'Opened ${Fmt.dateTime(d.opened)}',
      icon: AppIcons.disputes,
      trailing: StatusBadges.dispute(d.status),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              Expanded(child: _party(context, 'Raised by', d.raisedBy)),
              const Icon(
                AppIcons.arrowRight,
                size: 16,
                color: AppColors.slate400,
              ),
              const Gap(AppSpacing.sm),
              Expanded(child: _party(context, 'Against', d.against)),
            ],
          ),
          const Gap(AppSpacing.md),
          Text('Summary', style: theme.textTheme.titleSmall),
          Text(d.summary, style: theme.textTheme.bodyMedium),
          const Gap(AppSpacing.md),
          InfoRow(
            label: 'Amount claimed',
            value: Fmt.inr(d.amount),
            emphasize: true,
          ),
          const Gap(AppSpacing.md),
          Text('Evidence', style: theme.textTheme.titleSmall),
          const Gap(AppSpacing.sm),
          ResponsiveGrid(
            columns: const Responsive(compact: 2, medium: 4),
            spacing: AppSpacing.sm,
            children: [
              for (final label in const [
                'Handover photos',
                'Return photos',
                'GPS trail',
              ])
                AppCard(
                  padding: const EdgeInsets.all(AppSpacing.ms),
                  onTap: null,
                  child: Column(
                    children: [
                      Icon(switch (label) {
                        'GPS trail' => AppIcons.route,
                        _ => AppIcons.image,
                      }, color: AppColors.navy700),
                      const Gap(6),
                      Text(
                        label,
                        textAlign: TextAlign.center,
                        style: theme.textTheme.labelMedium,
                      ),
                    ],
                  ),
                ),
            ],
          ),
          const Gap(AppSpacing.md),
          if (d.status == DisputeStatus.resolved)
            InlineBanner(
              tone: AppTone.success,
              title: 'Resolved',
              message: d.resolution ?? '',
            )
          else
            Wrap(
              spacing: AppSpacing.sm,
              runSpacing: AppSpacing.sm,
              alignment: WrapAlignment.end,
              children: [
                AppButton(
                  label: 'Messaging unavailable',
                  icon: AppIcons.chat,
                  variant: AppButtonVariant.secondary,
                  onPressed: null,
                ),
                if (d.status == DisputeStatus.open)
                  AppButton(
                    label: 'Start review',
                    variant: AppButtonVariant.secondary,
                    onPressed: () {
                      ref.read(disputesProvider.notifier).startReview(d.id);
                      AppToast.show(
                        context,
                        'You’re now reviewing ${d.bookingRef}. Settlement is on hold.',
                      );
                    },
                  ),
                AppButton(
                  label: 'Resolve',
                  icon: AppIcons.check,
                  variant: AppButtonVariant.success,
                  onPressed: () => _resolve(context, ref),
                ),
              ],
            ),
        ],
      ),
    );
  }

  Widget _party(BuildContext context, String label, String name) => Row(
    children: [
      AppAvatar(name: name, size: 32),
      const Gap(AppSpacing.sm),
      Expanded(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              label,
              style: Theme.of(context).textTheme.labelSmall?.copyWith(
                color: AppColors.slate500,
                letterSpacing: 0,
              ),
            ),
            Text(
              name,
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: Theme.of(context).textTheme.labelLarge,
            ),
          ],
        ),
      ),
    ],
  );
}
