import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';
import 'package:intl/intl.dart';

import '../../../core/widgets/widgets.dart';
import '../data/verification_api.dart';
import '../data/verification_models.dart';
import 'verification_widgets.dart';

abstract final class VerificationAdminRoutes {
  static const queue = '/admin/verifications';
  static String detail(String id) => '/admin/verifications/$id';
}

/// KYC reviewers: applications waiting for review (and decided ones).
class VerificationQueuePage extends ConsumerStatefulWidget {
  const VerificationQueuePage({super.key, this.role});
  final String? role;
  @override
  ConsumerState<VerificationQueuePage> createState() =>
      _VerificationQueuePageState();
}

class _VerificationQueuePageState extends ConsumerState<VerificationQueuePage> {
  ApplicationStatus? filter = ApplicationStatus.submitted;

  @override
  Widget build(BuildContext context) {
    final queueKey = (filter, widget.role);
    final queue = ref.watch(verificationQueueProvider(queueKey));
    final fmt = DateFormat('d MMM, HH:mm');
    return AppPage(
      title: 'Verifications',
      subtitle: widget.role == 'operator'
          ? 'Driver applications'
          : widget.role == 'owner'
          ? 'Equipment owner account verification'
          : 'Owner and driver applications',
      body: PageBody(
        children: [
          AppChoiceChips<ApplicationStatus?>(
            options: const [
              ApplicationStatus.submitted,
              ApplicationStatus.underReview,
              ApplicationStatus.approved,
              ApplicationStatus.rejected,
              null,
            ],
            selected: filter,
            labelOf: (s) => s?.label ?? 'All',
            onSelected: (s) => setState(() => filter = s),
          ),
          const Gap(AppSpacing.md),
          AsyncBody(
            value: queue,
            onRetry: () => ref.invalidate(verificationQueueProvider(queueKey)),
            builder: (items) => items.isEmpty
                ? const EmptyState(
                    icon: AppIcons.approvals,
                    title: 'Nothing here',
                    message: 'No applications in this state.',
                  )
                : AppCard(
                    child: Column(
                      children: [
                        for (final a in items)
                          ListRow(
                            key: Key('application-${a.id}'),
                            leading: Icon(
                              a.role == 'owner'
                                  ? AppIcons.building
                                  : AppIcons.operator,
                              size: 20,
                            ),
                            title: a.subjectName.isEmpty
                                ? '(unnamed)'
                                : a.subjectName,
                            subtitle:
                                '${a.role == 'owner' ? 'Equipment owner account' : 'Driver'}'
                                '${a.submittedAt == null ? '' : ' · submitted ${fmt.format(a.submittedAt!.toLocal())}'}',
                            trailing: ApplicationStatusBadge(a.status),
                            onTap: () => context.push(
                              VerificationAdminRoutes.detail(a.id),
                            ),
                          ),
                      ],
                    ),
                  ),
          ),
        ],
      ),
    );
  }
}

/// One application: details, documents (opened through audited signed
/// links), per-document decisions, approve / reject with reason, history.
class VerificationDetailPage extends ConsumerWidget {
  const VerificationDetailPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final detail = ref.watch(applicationDetailProvider(id));
    return AppPage(
      title: 'Review application',
      topLevel: false,
      body: AsyncBody(
        value: detail,
        onRetry: () => ref.invalidate(applicationDetailProvider(id)),
        builder: (d) => _DetailBody(d: d),
      ),
    );
  }
}

class _DetailBody extends ConsumerWidget {
  const _DetailBody({required this.d});
  final ApplicationDetail d;

  bool get _pending =>
      d.application.status == ApplicationStatus.submitted ||
      d.application.status == ApplicationStatus.underReview;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.read(verificationApiProvider);
    final id = d.application.id;
    final queueKey = (null, d.application.role);
    void refresh() {
      ref.invalidate(applicationDetailProvider(id));
      ref.invalidate(verificationQueueProvider(queueKey));
    }

    final fmt = DateFormat('d MMM yyyy, HH:mm');
    final o = d.owner;
    final op = d.operator;

    final main = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (o != null)
          SectionCard(
            title: 'Business',
            icon: AppIcons.building,
            child: Column(
              children: [
                InfoRow(
                  label: 'Account',
                  value: o.accountKind == 'company'
                      ? 'Company (${o.orgType})'
                      : 'Individual (${o.orgType})',
                ),
                InfoRow(label: 'Legal name', value: o.legalName),
                InfoRow(label: 'Business name', value: o.businessName),
                InfoRow(
                  label: 'PAN',
                  value: o.panLast4 == null ? '—' : '••••••${o.panLast4}',
                ),
                if (o.gstin != null) InfoRow(label: 'GSTIN', value: o.gstin!),
                if (o.cin != null) InfoRow(label: 'CIN', value: o.cin!),
                if (o.udyam != null) InfoRow(label: 'Udyam', value: o.udyam!),
                InfoRow(
                  label: 'Contact',
                  value: [
                    o.contactName,
                    o.contactPhone,
                    o.contactEmail,
                  ].whereType<String>().join(' · '),
                ),
                if (o.address != null)
                  InfoRow(label: 'Address', value: o.address!.oneLine),
                InfoRow(
                  label: 'Payout account',
                  value: o.bank == null
                      ? '—'
                      : '${o.bank!.holder} · ••••${o.bank!.last4} · ${o.bank!.ifsc} · ${o.bank!.status}',
                ),
              ],
            ),
          ),
        if (op != null)
          SectionCard(
            title: 'Driver',
            icon: AppIcons.operator,
            child: Column(
              children: [
                InfoRow(
                  label: 'Licence',
                  value: op.licenceLast4 == null
                      ? '—'
                      : '••••${op.licenceLast4} · ${op.licenceClass ?? ''}',
                ),
                InfoRow(
                  label: 'Valid until',
                  value: op.licenceExpiresOn ?? '—',
                ),
                InfoRow(
                  label: 'Experience',
                  value: '${op.experienceYears ?? 0} years',
                ),
                InfoRow(label: 'Machines', value: op.skills.join(', ')),
                if (op.availability != null)
                  InfoRow(
                    label: 'Availability',
                    value:
                        '${op.availability!.days.join(', ')} · ${op.availability!.start}–${op.availability!.end}',
                  ),
                for (final q in op.qualifications)
                  InfoRow(
                    label: 'Qualification',
                    value: [
                      q.title,
                      q.issuer,
                      q.validUntil,
                    ].whereType<String>().join(' · '),
                  ),
              ],
            ),
          ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Documents',
          subtitle: 'Links are short-lived and every opening is audited',
          icon: AppIcons.fileCheck,
          child: DocumentList(
            documents: d.documents,
            trailing: _pending && d.documents.isNotEmpty
                ? (doc) => doc.status == 'submitted'
                      ? Wrap(
                          children: [
                            IconButton(
                              key: Key('approve-doc-${doc.id}'),
                              tooltip: 'Accept document',
                              icon: const Icon(
                                AppIcons.check,
                                size: 18,
                                color: AppColors.green700,
                              ),
                              onPressed: () async {
                                final ok = await runVerificationAction(
                                  context,
                                  () => api.reviewDocument(
                                    id,
                                    doc.id,
                                    approve: true,
                                  ),
                                  success: 'Document accepted',
                                );
                                if (ok) refresh();
                              },
                            ),
                            IconButton(
                              key: Key('reject-doc-${doc.id}'),
                              tooltip: 'Reject document',
                              icon: const Icon(
                                AppIcons.close,
                                size: 18,
                                color: AppColors.red700,
                              ),
                              onPressed: () async {
                                final reason = await askReason(
                                  context,
                                  title: 'Reject ${doc.label}',
                                  hint: 'e.g. The image is blurred',
                                );
                                if (reason == null || !context.mounted) return;
                                if (await runVerificationAction(
                                  context,
                                  () => api.reviewDocument(
                                    id,
                                    doc.id,
                                    approve: false,
                                    reason: reason,
                                  ),
                                  success: 'Document rejected',
                                )) {
                                  refresh();
                                }
                              },
                            ),
                          ],
                        )
                      : const SizedBox.shrink()
                : null,
          ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'History',
          icon: AppIcons.history,
          child: d.history.isEmpty
              ? const Text('No events yet.')
              : Column(
                  children: [
                    for (final h in d.history.reversed)
                      ListRow(
                        dense: true,
                        title: h.label,
                        subtitle:
                            [fmt.format(h.at.toLocal()), h.actorName, h.reason]
                                .whereType<String>()
                                .where((s) => s.isNotEmpty)
                                .join(' · '),
                      ),
                  ],
                ),
        ),
      ],
    );

    final side = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ChecklistCard(checks: d.checks, title: 'Completeness'),
        const Gap(AppSpacing.md),
        if (d.application.status == ApplicationStatus.submitted)
          AppButton(
            key: const Key('start-review'),
            label: 'Start review',
            variant: AppButtonVariant.secondary,
            expand: true,
            onPressed: () async {
              if (await runVerificationAction(
                context,
                () => api.startReview(id),
                success: 'Review started',
              )) {
                refresh();
              }
            },
          ),
        if (_pending) ...[
          const Gap(AppSpacing.sm),
          AppButton(
            key: const Key('approve-application'),
            label: 'Approve',
            icon: AppIcons.checkCircle,
            variant: AppButtonVariant.success,
            expand: true,
            onPressed: () async {
              final yes = await showConfirmDialog(
                context,
                title: 'Approve ${d.subjectName}?',
                message:
                    'All pending documents are accepted and the account becomes verified.',
                confirmLabel: 'Approve',
              );
              if (!yes || !context.mounted) return;
              if (await runVerificationAction(
                context,
                () => api.approve(id),
                success: 'Approved',
              )) {
                refresh();
              }
            },
          ),
        ],
        if (_pending || d.application.status == ApplicationStatus.approved) ...[
          const Gap(AppSpacing.sm),
          AppButton(
            key: const Key('reject-application'),
            label: d.application.status == ApplicationStatus.approved
                ? 'Revoke verification'
                : 'Reject',
            icon: AppIcons.closeCircle,
            variant: AppButtonVariant.danger,
            expand: true,
            onPressed: () async {
              final reason = await askReason(
                context,
                title: 'Reason (shown to the applicant)',
                hint: 'What should they fix?',
              );
              if (reason == null || !context.mounted) return;
              if (await runVerificationAction(
                context,
                () => api.reject(id, reason),
                success: 'Rejected',
              )) {
                refresh();
              }
            },
          ),
        ],
      ],
    );

    return PageBody(
      children: [
        Wrap(
          spacing: AppSpacing.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            Text(d.subjectName, style: Theme.of(context).textTheme.titleLarge),
            ApplicationStatusBadge(d.application.status),
            StatusBadge(
              label: d.application.role == 'owner'
                  ? 'Equipment owner account'
                  : 'Driver',
              tone: AppTone.brand,
            ),
          ],
        ),
        if (d.application.rejectionReason != null) ...[
          const Gap(AppSpacing.sm),
          InlineBanner(
            message: d.application.rejectionReason!,
            title: 'Last decision',
            tone: AppTone.warning,
          ),
        ],
        const Gap(AppSpacing.md),
        SplitLayout(main: main, side: side, sideWidth: 300),
      ],
    );
  }
}

/// Asks for a reason (at least 5 characters). Returns null when cancelled.
Future<String?> askReason(
  BuildContext context, {
  required String title,
  String? hint,
}) {
  return showAppSheet<String>(
    context,
    title: title,
    builder: (_) => _ReasonForm(hint: hint),
  );
}

class _ReasonForm extends StatefulWidget {
  const _ReasonForm({this.hint});
  final String? hint;
  @override
  State<_ReasonForm> createState() => _ReasonFormState();
}

class _ReasonFormState extends State<_ReasonForm> {
  final _form = GlobalKey<FormState>();
  final _reason = TextEditingController();

  @override
  void dispose() {
    _reason.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppTextField(
            key: const Key('reason'),
            label: 'Reason',
            controller: _reason,
            hint: widget.hint,
            maxLines: 3,
            maxLength: 500,
            requiredField: true,
            validator: (v) => (v ?? '').trim().length < 5
                ? 'Give a reason (at least 5 characters)'
                : null,
          ),
          const Gap(AppSpacing.md),
          AppButton(
            key: const Key('confirm-reason'),
            label: 'Confirm',
            onPressed: () {
              if (_form.currentState!.validate()) {
                Navigator.of(context).pop(_reason.text.trim());
              }
            },
          ),
        ],
      ),
    );
  }
}
