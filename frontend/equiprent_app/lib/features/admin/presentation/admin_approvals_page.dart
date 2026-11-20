import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';

class AdminApprovalsPage extends ConsumerWidget {
  const AdminApprovalsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final all = ref.watch(approvalsProvider);
    final pending = all
        .where((a) => a.status == ApprovalStatus.pending)
        .toList();
    final decided = all
        .where((a) => a.status != ApprovalStatus.pending)
        .toList();

    return AppPage(
      title: 'Approvals',
      subtitle: '${pending.length} pending · target: under 4 working hours',
      body: PageBody(
        children: [
          if (pending.isEmpty)
            const AppCard(
              child: EmptyState(
                icon: AppIcons.checkCircle,
                title: 'Queue cleared',
                message: 'Nice work. New submissions appear here.',
                compact: true,
              ),
            )
          else
            ResponsiveGrid(
              columns: const Responsive(compact: 1, expanded: 2),
              children: [for (final a in pending) _ApprovalCard(request: a)],
            ),
          if (decided.isNotEmpty) ...[
            const Gap(AppSpacing.lg),
            const SectionHeader(title: 'Decided today'),
            AppDataTable<ApprovalRequest>(
              rows: decided,
              columns: [
                TableColumnDef(label: 'Request', cell: (a) => Text(a.title)),
                TableColumnDef(label: 'Type', cell: (a) => Text(a.type)),
                TableColumnDef(
                  label: 'Decision',
                  cell: (a) => StatusBadges.approval(a.status),
                ),
              ],
            ),
          ],
        ],
      ),
    );
  }
}

class _ApprovalCard extends ConsumerWidget {
  const _ApprovalCard({required this.request});

  final ApprovalRequest request;

  Future<void> _reject(BuildContext context, WidgetRef ref) async {
    String? reason = 'Document unclear or unreadable';
    final ok = await showAppSheet<bool>(
      context,
      title: 'Reject request',
      subtitle: request.title,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, set) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            RadioGroup<String>(
              groupValue: reason,
              onChanged: (v) => set(() => reason = v),
              child: Column(
                children: [
                  for (final r in const [
                    'Document unclear or unreadable',
                    'Name mismatch across documents',
                    'Expired insurance / RC',
                    'Suspected duplicate or fraud',
                  ])
                    RadioListTile<String>(
                      contentPadding: EdgeInsets.zero,
                      value: r,
                      title: Text(r),
                    ),
                ],
              ),
            ),
            const AppTextField(label: 'Note to user (optional)', maxLines: 2),
            const Gap(AppSpacing.lg),
            AppButton(
              label: 'Reject and notify',
              variant: AppButtonVariant.danger,
              expand: true,
              onPressed: () => Navigator.of(ctx).pop(true),
            ),
          ],
        ),
      ),
    );
    if (ok == true && context.mounted) {
      ref
          .read(approvalsProvider.notifier)
          .decide(request.id, ApprovalStatus.rejected);
      AppToast.show(
        context,
        'Rejected: $reason. ${request.submittedBy} has been notified.',
        tone: AppTone.warning,
      );
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final a = request;
    final passed = a.checks.values.where((v) => v).length;

    return AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              StatusBadge(
                label: a.type,
                tone: switch (a.type) {
                  'KYC' => AppTone.brand,
                  'Listing' => AppTone.info,
                  _ => AppTone.accent,
                },
              ),
              const Spacer(),
              Text(
                Fmt.ago(a.submittedAt, MockData.now),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: AppColors.slate500,
                ),
              ),
            ],
          ),
          const Gap(AppSpacing.sm),
          Text(a.title, style: theme.textTheme.titleMedium),
          Text(
            'Submitted by ${a.submittedBy}',
            style: theme.textTheme.bodySmall?.copyWith(
              color: AppColors.slate500,
            ),
          ),
          const Gap(AppSpacing.ms),
          Text(
            'Automated checks · $passed of ${a.checks.length} passed',
            style: theme.textTheme.labelLarge,
          ),
          const Gap(AppSpacing.xs),
          for (final c in a.checks.entries)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 2),
              child: Row(
                children: [
                  Icon(
                    c.value ? AppIcons.checkCircle : AppIcons.closeCircle,
                    size: 16,
                    color: c.value ? AppColors.green700 : AppColors.red600,
                  ),
                  const Gap(AppSpacing.sm),
                  Expanded(
                    child: Text(c.key, style: theme.textTheme.bodyMedium),
                  ),
                  const Gap(AppSpacing.sm),
                  Text(
                    c.value ? 'Pass' : 'Fail',
                    style: theme.textTheme.labelMedium?.copyWith(
                      color: c.value ? AppColors.green700 : AppColors.red600,
                    ),
                  ),
                ],
              ),
            ),
          if (a.flag != null) ...[
            const Gap(AppSpacing.sm),
            InlineBanner(tone: AppTone.warning, message: a.flag!),
          ],
          const Gap(AppSpacing.md),
          Wrap(
            alignment: WrapAlignment.end,
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            children: [
              AppButton(
                label: 'Documents unavailable',
                icon: AppIcons.eye,
                size: AppButtonSize.small,
                variant: AppButtonVariant.ghost,
                onPressed: null,
              ),
              AppButton(
                label: 'Reject',
                size: AppButtonSize.small,
                variant: AppButtonVariant.secondary,
                onPressed: () => _reject(context, ref),
              ),
              AppButton(
                label: 'Approve',
                size: AppButtonSize.small,
                variant: AppButtonVariant.success,
                icon: AppIcons.check,
                onPressed: () async {
                  if (passed < a.checks.length) {
                    final ok = await showConfirmDialog(
                      context,
                      title: 'Approve with failed checks?',
                      message:
                          'Some automated checks failed. Approve only if you verified the documents manually. Your decision is audited.',
                      confirmLabel: 'Approve anyway',
                      tone: AppTone.warning,
                    );
                    if (!ok) return;
                  }
                  ref
                      .read(approvalsProvider.notifier)
                      .decide(a.id, ApprovalStatus.approved);
                  if (context.mounted) {
                    AppToast.success(context, 'Approved: ${a.title}.');
                  }
                },
              ),
            ],
          ),
        ],
      ),
    );
  }
}
