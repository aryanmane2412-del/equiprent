import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../comms/data/comms_api.dart' show commsApiProvider;
import '../../comms/presentation/comms_widgets.dart';
import '../../comms/presentation/ui.dart' hide operatorJobsProvider;
import '../../verification/data/verification_api.dart'
    show inspectionCameraProvider, linkOpenerProvider;
import '../../bookings/booking_tracking_page.dart' show trackableStatuses;
import '../../tracking/data/tracking_api.dart' show LatLngPoint;
import '../../tracking/presentation/share_location_card.dart';
import '../data/operator_api.dart';

/// Operator routes (jobs are addressed by booking id).
abstract final class OperatorJobRoutes {
  static String job(String bookingId) => '/operator/jobs/$bookingId';
  static String navigate(String bookingId) =>
      '/operator/jobs/$bookingId/navigate';
  static String checklist(String bookingId, String kind) =>
      '/operator/jobs/$bookingId/checklist/$kind';
}

String _statusLabel(String s) => switch (s) {
  'operator_assigned' || 'confirmed' => 'Ready to dispatch',
  'dispatched' => 'On the way',
  'on_site' => 'On site',
  'active' => 'Working',
  'extension_requested' => 'Working (extension asked)',
  'return_initiated' => 'Return requested',
  'returned' => 'Returned',
  'completed' || 'closed' => 'Completed',
  'cancelled' => 'Cancelled',
  _ => s.replaceAll('_', ' '),
};

AppTone _assignmentTone(String s) => switch (s) {
  'assigned' => AppTone.warning,
  'accepted' => AppTone.info,
  'completed' => AppTone.success,
  _ => AppTone.neutral,
};

/// A job row used by the lists.
class _JobCard extends StatelessWidget {
  const _JobCard({required this.job});
  final JobSummary job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: AppCard(
        key: Key('job-${job.bookingId}'),
        onTap: () => context.push(OperatorJobRoutes.job(job.bookingId)),
        borderColor: job.awaitingAnswer ? AppColors.orange500 : null,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(
                  child: Text(
                    Fmt.dateRange(job.start, job.end),
                    style: theme.textTheme.labelLarge?.copyWith(
                      color: AppColors.navy700,
                    ),
                  ),
                ),
                StatusBadge(
                  label: job.awaitingAnswer
                      ? 'Needs your answer'
                      : job.assignmentStatus,
                  tone: _assignmentTone(job.assignmentStatus),
                ),
              ],
            ),
            const Gap(4),
            Text(job.equipmentTitle, style: theme.textTheme.titleMedium),
            if (job.driverPaymentStatus != 'not_set')
              Text(
                'Driver payment · ${Fmt.inr(job.driverPaymentPaise / 100)} · ${job.driverPaymentStatus}',
                style: theme.textTheme.bodySmall,
              ),
            Text(
              [
                job.customerName,
                job.site,
              ].where((s) => s.isNotEmpty).join(' · '),
              style: theme.textTheme.bodySmall?.copyWith(
                color: AppColors.slate500,
              ),
            ),
            Text(
              '${job.reference} · ${_statusLabel(job.bookingStatus)}',
              style: theme.textTheme.bodySmall,
            ),
          ],
        ),
      ),
    );
  }
}

// ---- dashboard / lists ----------------------------------------------------------------------------

/// Today: the next jobs and the next two weeks.
class OperatorTodayPage extends ConsumerWidget {
  const OperatorTodayPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final current = ref.watch(myJobsProvider('current'));
    final schedule = ref.watch(myScheduleProvider);
    return AppPage(
      title: 'Today',
      subtitle: Fmt.dayDate(DateTime.now()),
      actions: [
        IconButton(
          key: const Key('operator-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () {
            ref.invalidate(myJobsProvider('current'));
            ref.invalidate(myScheduleProvider);
          },
        ),
      ],
      body: RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(myJobsProvider('current'));
          ref.invalidate(myScheduleProvider);
        },
        child: PageBody(
          children: [
            AsyncBody(
              value: current,
              onRetry: () => ref.invalidate(myJobsProvider('current')),
              builder: (jobs) => jobs.isEmpty
                  ? const EmptyState(
                      icon: AppIcons.assignments,
                      title: 'No open jobs',
                      message: 'New assignments appear here and notify you.',
                    )
                  : _TodayJobs(jobs: jobs),
            ),
            const Gap(AppSpacing.lg),
            const SectionHeader(title: 'Next 14 days'),
            AsyncBody(
              value: schedule,
              onRetry: () => ref.invalidate(myScheduleProvider),
              builder: (jobs) => jobs.isEmpty
                  ? const Text('Nothing scheduled.')
                  : Column(
                      children: [
                        for (final j in jobs)
                          ListRow(
                            key: Key('schedule-${j.bookingId}'),
                            dense: true,
                            leading: const IconTile(icon: AppIcons.calendar),
                            title:
                                '${Fmt.dayDate(j.start)} → ${Fmt.dayDate(j.end)}',
                            subtitle: '${j.equipmentTitle} · ${j.site}',
                            onTap: () => context.push(
                              OperatorJobRoutes.job(j.bookingId),
                            ),
                          ),
                      ],
                    ),
            ),
            const Gap(AppSpacing.xl),
          ],
        ),
      ),
    );
  }
}

/// Today's dispatches, laid out like the driver design: the active dispatch with its actions,
/// the pre-trip safety check (beside it on wide screens, below it on phones), then what is next.
class _TodayJobs extends StatelessWidget {
  const _TodayJobs({required this.jobs});
  final List<JobSummary> jobs;

  @override
  Widget build(BuildContext context) {
    final active = jobs.first;
    final upNext = jobs.skip(1).toList();
    final wide = !context.windowSize.isCompact;
    final dispatch = _ActiveDispatchCard(job: active);
    final check = _SafetyCheckCard(job: active);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (wide)
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(flex: 3, child: dispatch),
              const Gap(AppSpacing.md),
              Expanded(flex: 2, child: check),
            ],
          )
        else ...[
          dispatch,
          const Gap(AppSpacing.md),
          check,
        ],
        if (upNext.isNotEmpty) ...[
          const Gap(AppSpacing.lg),
          SectionHeader(title: wide ? 'Upcoming deliveries' : 'Up next'),
          for (final j in upNext) _UpNextCard(job: j),
        ],
      ],
    );
  }
}

/// A small grey tile with a caps label, e.g. DROP-OFF / the site.
class _InfoTile extends StatelessWidget {
  const _InfoTile({required this.label, required this.value});
  final String label;
  final String value;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.symmetric(
        horizontal: AppSpacing.md,
        vertical: AppSpacing.sm,
      ),
      decoration: BoxDecoration(
        color: AppColors.slate100,
        borderRadius: BorderRadius.circular(AppRadius.md),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            label,
            style: theme.textTheme.labelSmall?.copyWith(
              color: AppColors.onWarningContainer,
              fontWeight: FontWeight.w700,
              letterSpacing: 0.4,
            ),
          ),
          Text(value, style: theme.textTheme.titleSmall),
        ],
      ),
    );
  }
}

/// The job the driver is on: reference and state, the machine, where and when, and the actions.
class _ActiveDispatchCard extends StatelessWidget {
  const _ActiveDispatchCard({required this.job});
  final JobSummary job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      key: Key('job-${job.bookingId}'),
      onTap: () => context.push(OperatorJobRoutes.job(job.bookingId)),
      borderColor: job.awaitingAnswer ? AppColors.orange500 : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(
                  'ACTIVE DISPATCH • ${job.reference}',
                  style: theme.textTheme.labelSmall?.copyWith(
                    color: AppColors.onWarningContainer,
                    fontWeight: FontWeight.w700,
                    letterSpacing: 0.4,
                  ),
                ),
              ),
              StatusBadge(
                label: job.awaitingAnswer
                    ? 'Needs your answer'
                    : _statusLabel(job.bookingStatus),
                tone: _assignmentTone(job.assignmentStatus),
              ),
            ],
          ),
          const Gap(AppSpacing.xs),
          Text(job.equipmentTitle, style: theme.textTheme.headlineSmall),
          if (job.registrationNo != null)
            Text(
              job.registrationNo!,
              style: theme.textTheme.bodySmall?.copyWith(
                color: AppColors.slate500,
              ),
            ),
          const Gap(AppSpacing.md),
          _InfoTile(
            label: 'DROP-OFF',
            value: job.site.isEmpty ? 'Shown once you accept' : job.site,
          ),
          const Gap(AppSpacing.sm),
          _InfoTile(label: 'WHEN', value: Fmt.dateRange(job.start, job.end)),
          if (job.customerName.isNotEmpty) ...[
            const Gap(AppSpacing.sm),
            _InfoTile(label: 'CUSTOMER', value: job.customerName),
          ],
          const Gap(AppSpacing.md),
          _ActionRow(
            children: [
              AppButton(
                key: Key('active-gps-${job.bookingId}'),
                label: 'Open GPS',
                icon: AppIcons.navigation,
                expand: true,
                onPressed: () =>
                    context.push(OperatorJobRoutes.navigate(job.bookingId)),
              ),
              AppButton(
                key: Key('active-open-${job.bookingId}'),
                label: job.awaitingAnswer ? 'Respond' : 'Open job',
                icon: AppIcons.checkCircle,
                variant: AppButtonVariant.success,
                expand: true,
                onPressed: () =>
                    context.push(OperatorJobRoutes.job(job.bookingId)),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

/// Action buttons that fill the card: side by side on wide screens, stacked on phones.
class _ActionRow extends StatelessWidget {
  const _ActionRow({required this.children});
  final List<Widget> children;

  @override
  Widget build(BuildContext context) {
    if (context.windowSize.isCompact) {
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (var i = 0; i < children.length; i++) ...[
            if (i > 0) const Gap(AppSpacing.sm),
            children[i],
          ],
        ],
      );
    }
    return Row(
      children: [
        for (var i = 0; i < children.length; i++) ...[
          if (i > 0) const Gap(AppSpacing.sm),
          Expanded(child: children[i]),
        ],
      ],
    );
  }
}

/// The pre-trip safety check: leads to the job's handover checklist.
class _SafetyCheckCard extends StatelessWidget {
  const _SafetyCheckCard({required this.job});
  final JobSummary job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      key: Key('safety-check-${job.bookingId}'),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('Pre-trip safety check', style: theme.textTheme.headlineSmall),
          const Gap(AppSpacing.xs),
          Text(
            'Required before unloading at the job site.',
            style: theme.textTheme.bodyMedium?.copyWith(
              color: AppColors.slate600,
            ),
          ),
          const Gap(AppSpacing.md),
          AppButton(
            key: Key('safety-open-${job.bookingId}'),
            label: 'Open safety checklist',
            icon: AppIcons.handover,
            variant: AppButtonVariant.accent,
            expand: true,
            onPressed: () => context.push(
              OperatorJobRoutes.checklist(job.bookingId, 'handover'),
            ),
          ),
        ],
      ),
    );
  }
}

/// A job coming up after the active one.
class _UpNextCard extends StatelessWidget {
  const _UpNextCard({required this.job});
  final JobSummary job;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: AppCard(
        key: Key('up-next-${job.bookingId}'),
        onTap: () => context.push(OperatorJobRoutes.job(job.bookingId)),
        child: Row(
          children: [
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(
                    '${Fmt.dayDate(job.start)} • ${job.reference}',
                    style: theme.textTheme.labelSmall?.copyWith(
                      color: AppColors.slate600,
                    ),
                  ),
                  Text(job.equipmentTitle, style: theme.textTheme.titleMedium),
                  if (job.site.isNotEmpty)
                    Text(
                      job.site,
                      style: theme.textTheme.bodySmall?.copyWith(
                        color: AppColors.slate500,
                      ),
                    ),
                ],
              ),
            ),
            const Gap(AppSpacing.sm),
            AppButton(
              label: job.awaitingAnswer ? 'Respond' : 'Start',
              icon: AppIcons.play,
              size: AppButtonSize.small,
              onPressed: () =>
                  context.push(OperatorJobRoutes.job(job.bookingId)),
            ),
          ],
        ),
      ),
    );
  }
}

/// Open assignments, soonest first.
class OperatorJobsPage extends ConsumerWidget {
  const OperatorJobsPage({super.key, required this.scope});
  final String scope; // current | history

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final jobs = ref.watch(myJobsProvider(scope));
    final history = scope == 'history';
    return AppPage(
      title: history ? 'Job history' : 'Assignments',
      actions: [
        IconButton(
          key: const Key('operator-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () {
            ref.invalidate(myJobsProvider(scope));
          },
        ),
      ],
      body: AsyncBody(
        value: jobs,
        onRetry: () => ref.invalidate(myJobsProvider(scope)),
        builder: (list) => list.isEmpty
            ? EmptyState(
                icon: history ? AppIcons.history : AppIcons.assignments,
                title: history ? 'No past jobs' : 'No assignments',
                message: history
                    ? 'Finished, declined and reassigned jobs are listed here.'
                    : 'You’re all clear. New jobs will notify you.',
              )
            : RefreshIndicator(
                onRefresh: () async => ref.invalidate(myJobsProvider(scope)),
                child: PageBody(
                  children: [for (final j in list) _JobCard(job: j)],
                ),
              ),
      ),
    );
  }
}

// ---- job detail ----------------------------------------------------------------------------------

/// One job: customer, machine, site, navigation, evidence and the next step.
class OperatorJobPage extends ConsumerWidget {
  const OperatorJobPage({super.key, required this.bookingId});
  final String bookingId;

  Future<void> _step(
    BuildContext context,
    WidgetRef ref,
    String step,
    String done,
  ) async {
    if (step == 'decline') {
      final ok = await showConfirmDialog(
        context,
        title: 'Decline this job?',
        message: 'The owner will assign someone else.',
        confirmLabel: 'Decline',
        tone: AppTone.danger,
      );
      if (!ok) return;
    }
    if (!context.mounted) return;
    final ok = await runComms(
      context,
      () => ref.read(operatorApiProvider).step(bookingId, step),
      success: done,
    );
    if (ok) {
      ref.invalidate(myJobProvider(bookingId));
      ref.invalidate(myJobsProvider('current'));
      ref.invalidate(myJobsProvider('history'));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final job = ref.watch(myJobProvider(bookingId));
    return AppPage(
      title: job.value?.equipmentTitle ?? 'Job',
      subtitle: job.value?.reference,
      topLevel: false,
      actions: [
        IconButton(
          key: const Key('operator-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () {
            ref.invalidate(myJobProvider(bookingId));
          },
        ),
      ],
      bottomBar: job.value == null || job.value!.nextSteps.isEmpty
          ? null
          : BottomActionBar(
              child: _NextStep(
                job: job.value!,
                onStep: (s, m) => _step(context, ref, s, m),
              ),
            ),
      body: AsyncBody(
        value: job,
        onRetry: () => ref.invalidate(myJobProvider(bookingId)),
        builder: (j) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(myJobProvider(bookingId)),
          child: PageBody(
            children: [
              InlineBanner(
                key: const Key('job-status'),
                tone: j.current ? AppTone.info : AppTone.neutral,
                icon: j.current ? AppIcons.jobDetails : AppIcons.history,
                title: j.current
                    ? _statusLabel(j.bookingStatus)
                    : 'This job is ${j.assignmentStatus}',
                message: '${Fmt.dateRange(j.start, j.end)} · ${j.reference}',
              ),
              if (j.current && trackableStatuses.contains(j.bookingStatus)) ...[
                const Gap(AppSpacing.md),
                ShareLocationCard(
                  bookingId: bookingId,
                  destination: LatLngPoint(j.siteLat, j.siteLng),
                ),
              ],
              const Gap(AppSpacing.md),
              ResponsiveGrid(
                columns: const Responsive(compact: 1, medium: 2),
                children: [
                  SectionCard(
                    title: 'Site',
                    icon: AppIcons.siteMarker,
                    trailing: TextButton(
                      key: const Key('navigate'),
                      onPressed: () =>
                          context.push(OperatorJobRoutes.navigate(bookingId)),
                      child: const Text('Navigate'),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        if (j.siteLabel != null)
                          Text(
                            j.siteLabel!,
                            style: Theme.of(context).textTheme.titleSmall,
                          ),
                        Text(j.siteAddress),
                        if (j.workDescription.isNotEmpty) ...[
                          const Gap(AppSpacing.sm),
                          Text(
                            'Work: ${j.workDescription}',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ],
                      ],
                    ),
                  ),
                  SectionCard(
                    title: 'Machine',
                    icon: AppIcons.backhoe,
                    child: Column(
                      children: [
                        InfoRow(label: 'Model', value: j.equipmentLine),
                        if (j.registrationNo != null)
                          InfoRow(
                            label: 'Registration',
                            value: j.registrationNo!,
                          ),
                        if (j.serialNo != null)
                          InfoRow(label: 'Serial no.', value: j.serialNo!),
                      ],
                    ),
                  ),
                  SectionCard(
                    title: 'Customer',
                    icon: AppIcons.user,
                    child: _ContactBlock(
                      contact: j.customer,
                      hidden: j.assignmentStatus == 'assigned',
                    ),
                  ),
                  SectionCard(
                    title: 'Owner',
                    icon: AppIcons.building,
                    child: _ContactBlock(
                      contact: j.owner,
                      hidden: j.assignmentStatus == 'assigned',
                    ),
                  ),
                  if (j.driverPaymentStatus != 'not_set')
                    SectionCard(
                      title: 'Agreed driver payment',
                      icon: AppIcons.payments,
                      child: Column(
                        children: [
                          InfoRow(
                            label: 'Amount',
                            value: Fmt.inr(j.driverPaymentPaise / 100),
                          ),
                          InfoRow(
                            label: 'Status',
                            value: j.driverPaymentStatus,
                          ),
                        ],
                      ),
                    ),
                ],
              ),
              const Gap(AppSpacing.md),
              SectionCard(
                title: 'Evidence',
                subtitle: 'Handover (before) and return (after) checklists',
                icon: AppIcons.camera,
                child: j.checklists.isEmpty
                    ? const Text(
                        'No checklist yet. The handover checklist is done on site before work starts.',
                      )
                    : Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          for (final c in j.checklists) _ChecklistTile(c: c),
                        ],
                      ),
              ),
              const Gap(AppSpacing.md),
              SectionCard(
                title: 'Issues',
                icon: AppIcons.warning,
                trailing: j.current
                    ? TextButton(
                        key: const Key('report-issue'),
                        onPressed: () => _report(context, ref, j),
                        child: const Text('Report'),
                      )
                    : null,
                child: j.issues.isEmpty
                    ? const Text('No issues reported.')
                    : Column(
                        children: [
                          for (final i in j.issues) IssueTile(issue: i),
                        ],
                      ),
              ),
              const Gap(AppSpacing.xl),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _report(BuildContext context, WidgetRef ref, JobDetail j) async {
    final stage = switch (j.bookingStatus) {
      'active' || 'extension_requested' => 'during_rental',
      'return_initiated' || 'returned' => 'at_return',
      _ => 'before_handover',
    };
    final ok = await showAppSheet<bool>(
      context,
      title: 'Report an issue',
      subtitle: j.reference,
      builder: (_) => IssueForm(bookingId: bookingId, stage: stage),
    );
    if (ok == true && context.mounted) {
      AppToast.success(
        context,
        'Issue reported. The owner and EQUIPRENT were told.',
      );
      ref.invalidate(myJobProvider(bookingId));
    }
  }
}

class _ContactBlock extends ConsumerWidget {
  const _ContactBlock({required this.contact, required this.hidden});
  final JobContact contact;
  final bool hidden;

  @override
  Widget build(BuildContext context, WidgetRef ref) => Column(
    crossAxisAlignment: CrossAxisAlignment.start,
    children: [
      Text(contact.name, style: Theme.of(context).textTheme.titleSmall),
      if (contact.contact != null) Text(contact.contact!),
      if (contact.phone != null)
        TextButton.icon(
          key: Key('call-${contact.phone}'),
          onPressed: () => ref.read(linkOpenerProvider)('tel:${contact.phone}'),
          icon: const Icon(AppIcons.phone, size: 16),
          label: Text(contact.phone!),
        )
      else if (hidden)
        Text(
          'Phone shown once you accept the job.',
          style: Theme.of(
            context,
          ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
        ),
    ],
  );
}

class _ChecklistTile extends StatelessWidget {
  const _ChecklistTile({required this.c});
  final JobChecklist c;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final failed = c.items.entries
        .where((e) => !e.value)
        .map((e) => e.key)
        .toList();
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            '${c.kind == 'handover' ? 'Handover (before)' : 'Return (after)'} · ${Fmt.dateTime(c.capturedAt)}',
            style: theme.textTheme.titleSmall,
          ),
          Text(
            '${c.fuelPct == null ? '' : 'Fuel ${c.fuelPct}% · '}${c.photos.length} photos · by ${c.submittedBy}',
            style: theme.textTheme.bodySmall,
          ),
          if (failed.isNotEmpty)
            Text(
              'Not OK: ${failed.join(', ')}',
              style: theme.textTheme.bodySmall?.copyWith(
                color: AppColors.red600,
              ),
            ),
          if (c.damageNotes != null)
            Text(
              'Damage: ${c.damageNotes}',
              style: theme.textTheme.bodySmall?.copyWith(
                color: AppColors.red600,
              ),
            ),
        ],
      ),
    );
  }
}

/// A reported issue (used by operators, booking parties and admins).
class IssueTile extends StatelessWidget {
  const IssueTile({super.key, required this.issue, this.trailing});
  final JobIssue issue;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    final tone = switch (issue.severity) {
      'critical' => AppTone.danger,
      'major' => AppTone.warning,
      _ => AppTone.neutral,
    };
    return ListRow(
      key: Key('issue-${issue.id}'),
      leading: IconTile(icon: AppIcons.warning, tone: tone),
      title: issue.title,
      subtitle: [
        issue.description,
        '${issue.status.replaceAll('_', ' ')} · ${issue.reporterRole}${issue.reporterName == null ? '' : ' ${issue.reporterName}'} · ${Fmt.dateTime(issue.createdAt)}',
        if (issue.resolutionNote != null) 'Outcome: ${issue.resolutionNote}',
      ].join('\n'),
      trailing: trailing,
    );
  }
}

/// The next step as one large button (decline is secondary).
class _NextStep extends StatelessWidget {
  const _NextStep({required this.job, required this.onStep});
  final JobDetail job;
  final void Function(String step, String doneMessage) onStep;

  @override
  Widget build(BuildContext context) {
    Widget big(
      String key,
      String label,
      IconData icon,
      VoidCallback? onTap, {
      AppButtonVariant variant = AppButtonVariant.primary,
    }) => AppButton(
      key: Key('step-$key'),
      label: label,
      icon: icon,
      size: AppButtonSize.large,
      variant: variant,
      expand: true,
      onPressed: onTap,
    );
    final steps = job.nextSteps;
    if (steps.contains('accept')) {
      return Row(
        children: [
          Expanded(
            child: big(
              'decline',
              'Decline',
              AppIcons.close,
              () => onStep('decline', 'Job declined.'),
              variant: AppButtonVariant.secondary,
            ),
          ),
          const Gap(AppSpacing.sm),
          Expanded(
            child: big(
              'accept',
              'Accept job',
              AppIcons.check,
              () => onStep('accept', 'Job accepted.'),
              variant: AppButtonVariant.success,
            ),
          ),
        ],
      );
    }
    return switch (steps.first) {
      'dispatch' => big(
        'dispatch',
        'Start trip to site',
        AppIcons.navigation,
        () {
          onStep('dispatch', 'Trip started. Tracking is on.');
          context.push(OperatorJobRoutes.navigate(job.bookingId));
        },
        variant: AppButtonVariant.accent,
      ),
      'arrived' => big(
        'arrived',
        'I have arrived',
        AppIcons.siteMarker,
        () => onStep('arrived', 'Marked on site.'),
        variant: AppButtonVariant.accent,
      ),
      'handover_checklist' => big(
        'handover',
        'Handover checklist',
        AppIcons.handover,
        () => context.push(
          OperatorJobRoutes.checklist(job.bookingId, 'handover'),
        ),
        variant: AppButtonVariant.accent,
      ),
      'await_customer' => big(
        'await-customer',
        'Waiting for the customer to confirm handover',
        AppIcons.clock,
        null,
      ),
      'start' => big(
        'start',
        'Start work',
        AppIcons.play,
        () => onStep('start', 'Work started.'),
        variant: AppButtonVariant.success,
      ),
      'return_checklist' => big(
        'return',
        'Return checklist',
        AppIcons.handover,
        () =>
            context.push(OperatorJobRoutes.checklist(job.bookingId, 'return')),
      ),
      'returned' => big(
        'returned',
        'Hand the machine back',
        AppIcons.check,
        () => onStep('returned', 'Machine returned. Job complete.'),
        variant: AppButtonVariant.success,
      ),
      _ => const SizedBox.shrink(),
    };
  }
}

// ---- navigation ---------------------------------------------------------------------------------

/// Route to the site: opens turn-by-turn directions; arrival is confirmed here.
class OperatorNavigationPage extends ConsumerWidget {
  const OperatorNavigationPage({super.key, required this.bookingId});
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final job = ref.watch(myJobProvider(bookingId));
    return AppPage(
      title: 'Navigate to site',
      topLevel: false,
      actions: [
        IconButton(
          key: const Key('operator-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () => ref.invalidate(myJobProvider(bookingId)),
        ),
      ],
      body: AsyncBody(
        value: job,
        onRetry: () => ref.invalidate(myJobProvider(bookingId)),
        builder: (j) => PageBody(
          children: [
            MapPreview(
              height: 220,
              route: const [
                Offset(0.1, 0.8),
                Offset(0.4, 0.55),
                Offset(0.85, 0.2),
              ],
              progress: j.bookingStatus == 'dispatched' ? 0.5 : 0,
            ),
            const Gap(AppSpacing.md),
            SectionCard(
              title: j.siteLabel ?? 'Site',
              icon: AppIcons.siteMarker,
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Text(j.siteAddress),
                  Text(
                    '${j.siteLat.toStringAsFixed(5)}, ${j.siteLng.toStringAsFixed(5)}',
                    style: Theme.of(context).textTheme.bodySmall,
                  ),
                  const Gap(AppSpacing.md),
                  AppButton(
                    key: const Key('open-directions'),
                    label: 'Open directions',
                    icon: AppIcons.external,
                    variant: AppButtonVariant.accent,
                    expand: true,
                    onPressed: () async {
                      final ok = await ref.read(linkOpenerProvider)(
                        j.navigationUrl,
                      );
                      if (!ok && context.mounted) {
                        AppToast.error(context, 'Could not open the maps app.');
                      }
                    },
                  ),
                  if (j.nextSteps.contains('arrived')) ...[
                    const Gap(AppSpacing.sm),
                    AppButton(
                      key: const Key('arrived-here'),
                      label: 'I have arrived',
                      icon: AppIcons.check,
                      expand: true,
                      onPressed: () async {
                        final ok = await runComms(
                          context,
                          () => ref
                              .read(operatorApiProvider)
                              .step(bookingId, 'arrived'),
                          success: 'Marked on site.',
                        );
                        if (ok && context.mounted) {
                          ref.invalidate(myJobProvider(bookingId));
                          context.pop();
                        }
                      },
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

// ---- checklist ----------------------------------------------------------------------------------

/// Handover (before) or return (after) checklist: hour meter and its photo,
/// at least two photos of the machine, checks, fuel and notes.
class OperatorChecklistPage extends ConsumerStatefulWidget {
  const OperatorChecklistPage({
    super.key,
    required this.bookingId,
    required this.kind,
  });
  final String bookingId;
  final String kind; // handover | return

  @override
  ConsumerState<OperatorChecklistPage> createState() =>
      _OperatorChecklistPageState();
}

class _OperatorChecklistPageState extends ConsumerState<OperatorChecklistPage> {
  static const _checks = [
    'No new visible damage',
    'Hydraulics working',
    'Lights and horn',
    'Tyres / tracks',
    'Safety kit present',
  ];
  final _form = GlobalKey<FormState>();
  final _damage = TextEditingController();
  final _notes = TextEditingController();
  final Map<String, bool> _items = {for (final c in _checks) c: true};
  final List<String> _photos = [];
  double _fuel = 75;
  bool _busy = false;
  bool _uploading = false;
  late final String _clientId = newClientId();

  bool get _return => widget.kind == 'return';

  @override
  void dispose() {
    _damage.dispose();
    _notes.dispose();
    super.dispose();
  }

  Future<void> _addPhotos() async {
    setState(() => _uploading = true);
    try {
      final file = await ref.read(inspectionCameraProvider).capture();
      if (file == null) return;
      final id = await ref.read(commsApiProvider).upload('job_photo', file);
      if (mounted) setState(() => _photos.add(id));
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _uploading = false);
    }
  }

  Future<void> _submit() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    if (_photos.length < 2) {
      AppToast.show(
        context,
        'Add at least two photos of the machine.',
        tone: AppTone.warning,
      );
      return;
    }
    setState(() => _busy = true);
    try {
      await ref
          .read(operatorApiProvider)
          .submitChecklist(
            widget.bookingId,
            ChecklistInput(
              kind: widget.kind,
              photoIds: _photos,
              items: _items,
              clientId: _clientId,
              fuelPct: _fuel.round(),
              damageNotes: _damage.text.trim().isEmpty
                  ? null
                  : _damage.text.trim(),
              notes: _notes.text.trim().isEmpty ? null : _notes.text.trim(),
            ),
          );
      if (!mounted) return;
      AppToast.success(
        context,
        _return
            ? 'Return checklist saved.'
            : 'Handover checklist saved. You can start work.',
      );
      ref.invalidate(myJobProvider(widget.bookingId));
      context.pop();
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final job = ref.watch(myJobProvider(widget.bookingId));
    return AppPage(
      title: _return ? 'Return checklist' : 'Handover checklist',
      subtitle: job.value?.equipmentTitle,
      topLevel: false,
      body: Form(
        key: _form,
        child: PageBody(
          children: [
            InlineBanner(
              tone: AppTone.info,
              icon: AppIcons.camera,
              message: _return
                  ? 'Record the machine as it comes back: photos from every side and any damage.'
                  : 'Record the machine before work starts: photos from every side and the checks below.',
            ),
            const Gap(AppSpacing.md),
            SectionCard(
              title: _return ? 'After photos' : 'Before photos',
              subtitle: '${_photos.length} added · at least 2',
              icon: AppIcons.image,
              child: AppButton(
                key: const Key('checklist-photos'),
                label: 'Add photos',
                icon: AppIcons.camera,
                variant: AppButtonVariant.secondary,
                onPressed: _uploading ? null : _addPhotos,
              ),
            ),
            const Gap(AppSpacing.md),
            SectionCard(
              title: 'Checks',
              icon: AppIcons.checkCircle,
              child: Column(
                children: [
                  for (final c in _checks)
                    SwitchListTile.adaptive(
                      key: Key('check-$c'),
                      contentPadding: EdgeInsets.zero,
                      title: Text(c),
                      value: _items[c]!,
                      onChanged: (v) => setState(() => _items[c] = v),
                    ),
                  Row(
                    children: [
                      const Text('Fuel'),
                      Expanded(
                        child: Slider(
                          value: _fuel,
                          max: 100,
                          divisions: 20,
                          label: '${_fuel.round()}%',
                          onChanged: (v) => setState(() => _fuel = v),
                        ),
                      ),
                      Text('${_fuel.round()}%'),
                    ],
                  ),
                ],
              ),
            ),
            const Gap(AppSpacing.md),
            if (_return)
              AppTextField(
                key: const Key('checklist-damage'),
                label: 'Damage seen (reported to the owner)',
                controller: _damage,
                maxLines: 2,
                maxLength: 1000,
              ),
            AppTextField(
              key: const Key('checklist-notes'),
              label: 'Notes',
              controller: _notes,
              maxLines: 2,
              maxLength: 1000,
            ),
            const Gap(AppSpacing.md),
            AppButton(
              key: const Key('checklist-submit'),
              label: 'Save checklist',
              variant: AppButtonVariant.success,
              size: AppButtonSize.large,
              expand: true,
              isLoading: _busy,
              onPressed: _busy || _uploading ? null : _submit,
            ),
            const Gap(AppSpacing.xl),
          ],
        ),
      ),
    );
  }
}

// ---- issue form ----------------------------------------------------------------------------------

/// Report damage, a breakdown, a safety or access problem (sheet body).
class IssueForm extends ConsumerStatefulWidget {
  const IssueForm({
    super.key,
    required this.bookingId,
    required this.stage,
    this.submit,
  });
  final String bookingId;
  final String stage;

  /// Overrides the operator endpoint (booking parties use their own).
  final Future<void> Function(
    String kind,
    String severity,
    String description,
    List<String> photos,
  )?
  submit;

  @override
  ConsumerState<IssueForm> createState() => _IssueFormState();
}

class _IssueFormState extends ConsumerState<IssueForm> {
  final _form = GlobalKey<FormState>();
  final _desc = TextEditingController();
  String _kind = 'damage';
  String _severity = 'minor';
  final List<String> _photos = [];
  bool _busy = false;

  @override
  void dispose() {
    _desc.dispose();
    super.dispose();
  }

  Future<void> _addPhoto() async {
    try {
      final f = await ref.read(inspectionCameraProvider).capture();
      if (f == null) return;
      final id = await ref.read(commsApiProvider).upload('job_photo', f);
      if (mounted) setState(() => _photos.add(id));
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    }
  }

  Future<void> _send() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    setState(() => _busy = true);
    try {
      final d = _desc.text.trim();
      if (widget.submit != null) {
        await widget.submit!(_kind, _severity, d, _photos);
      } else {
        await ref
            .read(operatorApiProvider)
            .reportIssue(
              widget.bookingId,
              kind: _kind,
              stage: widget.stage,
              severity: _severity,
              description: d,
              photoIds: _photos,
            );
      }
      if (mounted) Navigator.of(context).pop(true);
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => Form(
    key: _form,
    child: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SegmentedButton<String>(
          key: const Key('issue-kind'),
          segments: const [
            ButtonSegment(value: 'damage', label: Text('Damage')),
            ButtonSegment(value: 'breakdown', label: Text('Breakdown')),
            ButtonSegment(value: 'safety', label: Text('Safety')),
            ButtonSegment(value: 'site_access', label: Text('Access')),
          ],
          selected: {_kind},
          onSelectionChanged: (s) => setState(() => _kind = s.first),
        ),
        const Gap(AppSpacing.sm),
        SegmentedButton<String>(
          key: const Key('issue-severity'),
          segments: const [
            ButtonSegment(value: 'minor', label: Text('Minor')),
            ButtonSegment(value: 'major', label: Text('Major')),
            ButtonSegment(value: 'critical', label: Text('Critical')),
          ],
          selected: {_severity},
          onSelectionChanged: (s) => setState(() => _severity = s.first),
        ),
        const Gap(AppSpacing.sm),
        AppTextField(
          key: const Key('issue-description'),
          label: 'What happened?',
          controller: _desc,
          maxLines: 3,
          maxLength: 2000,
          validator: (v) => (v ?? '').trim().length < 5
              ? 'Describe the issue (at least 5 characters)'
              : null,
        ),
        AppButton(
          key: const Key('issue-photo'),
          label: 'Add a photo (${_photos.length})',
          icon: AppIcons.camera,
          variant: AppButtonVariant.secondary,
          onPressed: _addPhoto,
        ),
        const Gap(AppSpacing.md),
        AppButton(
          key: const Key('issue-submit'),
          label: 'Send report',
          isLoading: _busy,
          onPressed: _busy ? null : _send,
        ),
      ],
    ),
  );
}
