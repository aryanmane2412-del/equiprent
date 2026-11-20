import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../comms/presentation/comms_widgets.dart';
import '../../comms/presentation/ui.dart';
import '../../fleet_manager/data/fleet_api.dart';
import '../../operator/presentation/operator_pages.dart' show IssueTile;

/// Admin: reported issues, account restrictions and fleet accounts.
class AdminOperationsPage extends ConsumerStatefulWidget {
  const AdminOperationsPage({super.key});
  @override
  ConsumerState<AdminOperationsPage> createState() =>
      _AdminOperationsPageState();
}

class _AdminOperationsPageState extends ConsumerState<AdminOperationsPage> {
  String _tab = 'issues';

  @override
  Widget build(BuildContext context) => AppPage(
    title: 'Operations',
    subtitle: 'Issues, restrictions and fleet accounts',
    body: PageBody(
      children: [
        SegmentedButton<String>(
          key: const Key('ops-tabs'),
          segments: const [
            ButtonSegment(
              value: 'issues',
              label: Text('Issues'),
              icon: Icon(AppIcons.warning, size: 16),
            ),
            ButtonSegment(
              value: 'restrictions',
              label: Text('Restrictions'),
              icon: Icon(AppIcons.ban, size: 16),
            ),
            ButtonSegment(
              value: 'fleets',
              label: Text('Fleets'),
              icon: Icon(AppIcons.fleet, size: 16),
            ),
          ],
          selected: {_tab},
          onSelectionChanged: (s) => setState(() => _tab = s.first),
        ),
        const Gap(AppSpacing.md),
        switch (_tab) {
          'restrictions' => const _Restrictions(),
          'fleets' => const _Fleets(),
          _ => const _Issues(),
        },
      ],
    ),
  );
}

class _Issues extends ConsumerWidget {
  const _Issues();

  Future<void> _review(
    BuildContext context,
    WidgetRef ref,
    String id,
    String status,
  ) async {
    String note = '';
    if (status != 'under_review') {
      final n = await askReason(
        context,
        title: status == 'resolved' ? 'Resolve the issue' : 'Dismiss the issue',
        label: 'Outcome',
        confirm: 'Save',
      );
      if (n == null) return;
      note = n;
    }
    if (!context.mounted) return;
    final ok = await runComms(
      context,
      () => ref
          .read(fleetApiProvider)
          .reviewIssue(id, status: status, note: note),
      success: 'Issue updated. The owner and reporter were told.',
    );
    if (ok) ref.invalidate(adminIssuesProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final issues = ref.watch(adminIssuesProvider(null));
    return AsyncBody(
      value: issues,
      onRetry: () => ref.invalidate(adminIssuesProvider),
      builder: (list) {
        final open = list.where((i) => i.open).toList();
        return open.isEmpty
            ? const EmptyState(
                icon: AppIcons.checkCircle,
                title: 'No open issues',
                message:
                    'Damage, breakdown and safety reports from jobs appear here.',
              )
            : Column(
                children: [
                  for (final i in open)
                    AppCard(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          IssueTile(issue: i),
                          Text(
                            '${i.bookingReference ?? ''} · ${i.equipmentTitle ?? ''} · owner ${i.ownerName ?? ''} · customer ${i.customerName ?? ''}',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                          const Gap(AppSpacing.sm),
                          Wrap(
                            spacing: AppSpacing.sm,
                            alignment: WrapAlignment.end,
                            children: [
                              if (i.status == 'open')
                                AppButton(
                                  key: Key('review-${i.id}'),
                                  label: 'Start review',
                                  size: AppButtonSize.small,
                                  variant: AppButtonVariant.secondary,
                                  onPressed: () => _review(
                                    context,
                                    ref,
                                    i.id,
                                    'under_review',
                                  ),
                                ),
                              AppButton(
                                key: Key('dismiss-${i.id}'),
                                label: 'Dismiss',
                                size: AppButtonSize.small,
                                variant: AppButtonVariant.ghost,
                                onPressed: () =>
                                    _review(context, ref, i.id, 'dismissed'),
                              ),
                              AppButton(
                                key: Key('resolve-${i.id}'),
                                label: 'Resolve',
                                size: AppButtonSize.small,
                                onPressed: () =>
                                    _review(context, ref, i.id, 'resolved'),
                              ),
                            ],
                          ),
                        ],
                      ),
                    ),
                ],
              );
      },
    );
  }
}

class _Restrictions extends ConsumerWidget {
  const _Restrictions();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(adminRestrictionsProvider);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Align(
          alignment: Alignment.centerRight,
          child: AppButton(
            key: const Key('new-restriction'),
            label: 'Restrict an account',
            icon: AppIcons.ban,
            size: AppButtonSize.small,
            variant: AppButtonVariant.secondary,
            onPressed: () async {
              final ok = await showAppSheet<bool>(
                context,
                title: 'Restrict an account',
                builder: (_) => const RestrictForm(),
              );
              if (ok == true) ref.invalidate(adminRestrictionsProvider);
            },
          ),
        ),
        const Gap(AppSpacing.sm),
        AsyncBody(
          value: list,
          onRetry: () => ref.invalidate(adminRestrictionsProvider),
          builder: (rs) => rs.isEmpty
              ? const EmptyState(
                  icon: AppIcons.ban,
                  title: 'No active restrictions',
                  message:
                      'Restrictions block one capability; suspension blocks the whole account.',
                )
              : Column(
                  children: [
                    for (final r in rs)
                      ListRow(
                        key: Key('restriction-${r.id}'),
                        leading: const IconTile(
                          icon: AppIcons.ban,
                          tone: AppTone.danger,
                        ),
                        title: '${r.userName ?? r.userId} · ${r.kindLabel}',
                        subtitle:
                            '${r.reason} · since ${Fmt.date(r.createdAt)}${r.expiresAt == null ? '' : ' · until ${Fmt.date(r.expiresAt!)}'}',
                        trailing: AppButton(
                          key: Key('lift-${r.id}'),
                          label: 'Lift',
                          size: AppButtonSize.small,
                          variant: AppButtonVariant.secondary,
                          onPressed: () async {
                            final reason = await askReason(
                              context,
                              title: 'Lift the restriction',
                              confirm: 'Lift',
                            );
                            if (reason == null || !context.mounted) return;
                            final ok = await runComms(
                              context,
                              () =>
                                  ref.read(fleetApiProvider).lift(r.id, reason),
                              success: 'Restriction lifted.',
                            );
                            if (ok) ref.invalidate(adminRestrictionsProvider);
                          },
                        ),
                      ),
                  ],
                ),
        ),
      ],
    );
  }
}

/// Restrict one capability of an account (by user id, or fixed by the caller).
class RestrictForm extends ConsumerStatefulWidget {
  const RestrictForm({
    super.key,
    this.userId,
    this.kinds = const [
      'operator_assignments',
      'fleet_management',
      'new_bookings',
    ],
  });
  final String? userId;
  final List<String> kinds;
  @override
  ConsumerState<RestrictForm> createState() => _RestrictFormState();
}

class _RestrictFormState extends ConsumerState<RestrictForm> {
  final _form = GlobalKey<FormState>();
  final _user = TextEditingController();
  final _reason = TextEditingController();
  late String _kind = widget.kinds.first;
  bool _busy = false;

  @override
  void dispose() {
    _user.dispose();
    _reason.dispose();
    super.dispose();
  }

  static const _labels = {
    'operator_assignments': 'No job assignments',
    'fleet_management': 'No fleet management',
    'new_bookings': 'No new bookings',
  };

  Future<void> _submit() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    setState(() => _busy = true);
    try {
      await ref
          .read(fleetApiProvider)
          .restrict(
            widget.userId ?? _user.text.trim(),
            kind: _kind,
            reason: _reason.text.trim(),
          );
      if (mounted) {
        AppToast.success(context, 'Restriction in force.');
        Navigator.of(context).pop(true);
      }
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
        if (widget.userId == null) ...[
          AppTextField(
            key: const Key('restrict-user'),
            label: 'User ID',
            controller: _user,
            validator: (v) =>
                RegExp(r'^[0-9a-fA-F-]{36}$').hasMatch((v ?? '').trim())
                ? null
                : 'Paste the user ID',
          ),
          const Gap(AppSpacing.sm),
        ],
        if (widget.kinds.length > 1)
          SegmentedButton<String>(
            key: const Key('restrict-kind'),
            segments: [
              for (final k in widget.kinds)
                ButtonSegment(value: k, label: Text(_labels[k] ?? k)),
            ],
            selected: {_kind},
            onSelectionChanged: (s) => setState(() => _kind = s.first),
          ),
        const Gap(AppSpacing.sm),
        AppTextField(
          key: const Key('restrict-reason'),
          label: 'Reason (shown in the audit log)',
          controller: _reason,
          maxLines: 2,
          validator: (v) =>
              (v ?? '').trim().length < 3 ? 'Give a reason' : null,
        ),
        const Gap(AppSpacing.md),
        AppButton(
          key: const Key('restrict-submit'),
          label: 'Restrict',
          variant: AppButtonVariant.danger,
          isLoading: _busy,
          onPressed: _busy ? null : _submit,
        ),
      ],
    ),
  );
}

class _Fleets extends ConsumerWidget {
  const _Fleets();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final fleets = ref.watch(adminFleetsProvider);
    return AsyncBody(
      value: fleets,
      onRetry: () => ref.invalidate(adminFleetsProvider),
      builder: (list) => list.isEmpty
          ? const EmptyState(
              icon: AppIcons.fleet,
              title: 'No fleets',
              message:
                  'Owners group their machines into fleets for their fleet managers.',
            )
          : Column(
              children: [
                for (final f in list)
                  ListRow(
                    key: Key('admin-fleet-${f.id}'),
                    leading: IconTile(
                      icon: AppIcons.fleet,
                      tone: f.status == 'active'
                          ? AppTone.info
                          : AppTone.neutral,
                    ),
                    title: '${f.name} · ${f.organizationName ?? ''}',
                    subtitle:
                        '${f.equipmentCount} machines · ${f.managers.isEmpty ? 'no managers' : f.managers.join(', ')} · ${f.status}',
                    trailing: f.status == 'active'
                        ? AppButton(
                            key: Key('admin-archive-${f.id}'),
                            label: 'Archive',
                            size: AppButtonSize.small,
                            variant: AppButtonVariant.ghost,
                            onPressed: () async {
                              final reason = await askReason(
                                context,
                                title: 'Archive ${f.name}?',
                                confirm: 'Archive',
                                message:
                                    'Its managers lose access to its machines.',
                              );
                              if (reason == null || !context.mounted) return;
                              final ok = await runComms(
                                context,
                                () => ref
                                    .read(fleetApiProvider)
                                    .adminArchiveFleet(f.id, reason),
                                success: 'Fleet archived.',
                              );
                              if (ok) ref.invalidate(adminFleetsProvider);
                            },
                          )
                        : null,
                  ),
              ],
            ),
    );
  }
}
