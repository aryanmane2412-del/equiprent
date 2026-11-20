import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/session/active_role_provider.dart';
import '../../../core/session/user_role.dart';
import '../../comms/presentation/comms_widgets.dart';
import '../../comms/presentation/ui.dart';
import '../../bookings/data/booking_models.dart' show availabilityLabel;
import '../data/fleet_api.dart';

/// Where the fleet screens live for the active role.
abstract final class FleetPaths {
  static String base(UserRole? role) => role == UserRole.owner ? '/owner' : '/fleet';
  static String fleet(UserRole? role, String id) => '${base(role)}/fleets/$id';
  static String equipmentNew(UserRole? role) => role == UserRole.owner ? '/owner/listings/new' : '/fleet/equipment/new';
  static String equipmentEdit(UserRole? role, String id) => role == UserRole.owner ? '/owner/listings/$id/edit' : '/fleet/equipment/$id/edit';
  static String equipmentManage(UserRole? role, String id) => role == UserRole.owner ? '/owner/listings/$id' : '/fleet/equipment/$id';
  static String maintenance(UserRole? role) => role == UserRole.owner ? '/owner/maintenance' : '/fleet/maintenance';
}

AppTone stateTone(String state) => switch (state) {
  'on_job' => AppTone.success,
  'reserved' => AppTone.info,
  'maintenance' => AppTone.danger,
  'blocked' => AppTone.warning,
  'unlisted' => AppTone.neutral,
  _ => AppTone.neutral,
};

void _refreshFleet(WidgetRef ref) {
  ref.invalidate(fleetOverviewProvider);
  ref.invalidate(fleetBoardProvider);
  ref.invalidate(fleetListProvider);
  ref.invalidate(fleetScheduleProvider);
  ref.invalidate(fleetRosterProvider);
}

// ---- overview -------------------------------------------------------------------------------------

class FleetOverviewPage extends ConsumerWidget {
  const FleetOverviewPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final overview = ref.watch(fleetOverviewProvider);
    final fleets = ref.watch(fleetListProvider);
    final schedule = ref.watch(fleetScheduleProvider);
    final roster = ref.watch(fleetRosterProvider);
    final role = ref.watch(activeRoleProvider);
    return AppPage(
      title: 'Fleet overview',
      subtitle: overview.value?.scope == 'assigned' ? 'Your assigned machines' : 'All machines',
      actions: [
        AppIconButton(key: const Key('fleet-live'), icon: AppIcons.map, tooltip: 'Live map', onPressed: () => context.push(role == UserRole.owner ? '/owner/live' : '/fleet/live')),
        if (role == UserRole.fleetManager)
          AppIconButton(key: const Key('fleet-company'), icon: AppIcons.building, tooltip: 'Company and my access', onPressed: () => context.push('/fleet/company')),
      ],
      body: RefreshIndicator(
        onRefresh: () async => _refreshFleet(ref),
        child: PageBody(
          children: [
            AsyncBody(
              value: overview,
              onRetry: () => ref.invalidate(fleetOverviewProvider),
              builder: (o) => ResponsiveGrid(
                columns: const Responsive(compact: 2, medium: 4),
                children: [
                  StatCard(
                    key: const Key('kpi-machines'),
                    label: 'Machines',
                    value: '${o.equipmentTotal}',
                    icon: AppIcons.fleet,
                    caption: '${o.listed} listed',
                  ),
                  StatCard(
                    label: 'On a job',
                    value: '${o.onJob}',
                    icon: AppIcons.tracking,
                    tone: AppTone.success,
                    caption: '${o.utilisationPct.toStringAsFixed(0)}% utilisation',
                  ),
                  StatCard(label: 'Operators on duty', value: '${o.operatorsOnDuty}/${o.operators}', icon: AppIcons.operator, tone: AppTone.info),
                  StatCard(
                    label: 'Earned (30 days)',
                    value: money(o.revenue30dPaise),
                    icon: AppIcons.earnings,
                    tone: AppTone.accent,
                    caption: '${o.openMaintenance} open maintenance',
                  ),
                ],
              ),
            ),
            const Gap(AppSpacing.lg),
            const SectionHeader(title: 'Needs attention'),
            _Alerts(schedule: schedule.value ?? const [], roster: roster.value ?? const []),
            const Gap(AppSpacing.lg),
            SectionHeader(
              title: 'Fleets',
              trailing: AppButton(
                key: const Key('new-fleet'),
                label: 'New fleet',
                icon: AppIcons.add,
                size: AppButtonSize.small,
                variant: AppButtonVariant.secondary,
                onPressed: () => _createFleet(context, ref),
              ),
            ),
            AsyncBody(
              value: fleets,
              onRetry: () => ref.invalidate(fleetListProvider),
              builder: (list) => list.isEmpty
                  ? const EmptyState(icon: AppIcons.fleet, title: 'No fleets yet', message: 'Group machines into fleets and give each fleet its managers.')
                  : Column(
                      children: [
                        for (final f in list)
                          ListRow(
                            key: Key('fleet-${f.id}'),
                            leading: const IconTile(icon: AppIcons.fleet),
                            title: f.name,
                            subtitle: '${f.equipmentCount} machines · ${f.managers.isEmpty ? 'no managers' : f.managers.join(', ')}',
                            onTap: () => context.push(FleetPaths.fleet(role, f.id)),
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

Future<void> _createFleet(BuildContext context, WidgetRef ref) async {
  final name = await askReason(context, title: 'New fleet', label: 'Fleet name', confirm: 'Create', minLength: 2, message: 'e.g. Pune north yard, Excavators');
  if (name == null || !context.mounted) return;
  final org = ref.read(fleetOrgProvider);
  if (org == null) return;
  final ok = await runComms(context, () => ref.read(fleetApiProvider).createFleet(org, name), success: 'Fleet "$name" created.');
  if (ok) ref.invalidate(fleetListProvider);
}

class _Alerts extends StatelessWidget {
  const _Alerts({required this.schedule, required this.roster});
  final List<ScheduleItem> schedule;
  final List<RosterOperator> roster;

  @override
  Widget build(BuildContext context) {
    final soon = DateTime.now().add(const Duration(days: 60));
    final items = <Widget>[
      for (final s in schedule.where((s) => s.needsOperator))
        ListRow(
          key: Key('alert-operator-${s.itemId}'),
          leading: const IconTile(icon: AppIcons.operator, tone: AppTone.accent),
          title: 'No operator for ${s.label}',
          subtitle: '${s.equipmentTitle} · starts ${Fmt.dayDate(s.start)}',
          onTap: () => context.go('/fleet/assignments'),
        ),
      for (final o in roster.where((o) => o.licenceExpiresOn != null && o.licenceExpiresOn!.isBefore(soon)))
        ListRow(
          leading: const IconTile(icon: AppIcons.fileCheck, tone: AppTone.warning),
          title: 'Licence expires ${Fmt.date(o.licenceExpiresOn!)}',
          subtitle: o.name,
        ),
      for (final o in roster.where((o) => o.blocked))
        ListRow(
          leading: const IconTile(icon: AppIcons.ban, tone: AppTone.danger),
          title: '${o.name} cannot take jobs',
          subtitle: 'Restricted by EQUIPRENT',
        ),
    ];
    return items.isEmpty ? const Text('Nothing needs attention.') : Column(children: items);
  }
}

// ---- fleet detail ----------------------------------------------------------------------------------

/// One fleet: its machines and managers. Owners add managers; owners and the
/// fleet's managers add or remove machines.
class FleetDetailPage extends ConsumerWidget {
  const FleetDetailPage({super.key, required this.fleetId});
  final String fleetId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final fleet = ref.watch(fleetDetailProvider(fleetId));
    final role = ref.watch(activeRoleProvider);
    final owner = role == UserRole.owner;
    final org = ref.watch(fleetOrgProvider);
    void reload() {
      ref.invalidate(fleetDetailProvider(fleetId));
      ref.invalidate(fleetListProvider);
      ref.invalidate(fleetBoardProvider);
    }

    return AppPage(
      title: fleet.value?.name ?? 'Fleet',
      topLevel: false,
      body: AsyncBody(
        value: fleet,
        onRetry: () => ref.invalidate(fleetDetailProvider(fleetId)),
        builder: (f) => PageBody(
          children: [
            SectionCard(
              title: 'Machines',
              subtitle: '${f.machines.length} in this fleet',
              icon: AppIcons.fleet,
              trailing: TextButton(key: const Key('add-machines'), onPressed: () => _addMachines(context, ref, f, reload), child: const Text('Add')),
              child: f.machines.isEmpty
                  ? const Text('No machines yet.')
                  : Column(
                      children: [
                        for (final m in f.machines)
                          ListRow(
                            key: Key('fleet-machine-${m.id}'),
                            dense: true,
                            title: m.name,
                            subtitle: m.detail,
                            trailing: IconButton(
                              key: Key('remove-machine-${m.id}'),
                              tooltip: 'Remove from fleet',
                              icon: const Icon(AppIcons.close),
                              onPressed: () async {
                                final ok = await runComms(
                                  context,
                                  () => ref.read(fleetApiProvider).removeMachine(org!, f.id, m.id),
                                  success: '${m.name} removed.',
                                );
                                if (ok) reload();
                              },
                            ),
                          ),
                      ],
                    ),
            ),
            const Gap(AppSpacing.md),
            SectionCard(
              title: 'Managers',
              icon: AppIcons.users,
              trailing: owner
                  ? TextButton(key: const Key('add-manager'), onPressed: () => _addManager(context, ref, f, reload), child: const Text('Add'))
                  : null,
              child: f.fleetManagers.isEmpty
                  ? Text(owner ? 'Add a fleet manager to give them these machines.' : 'No managers.')
                  : Column(
                      children: [
                        for (final m in f.fleetManagers)
                          ListRow(
                            dense: true,
                            title: m.name,
                            subtitle: m.detail,
                            trailing: owner
                                ? IconButton(
                                    key: Key('remove-manager-${m.id}'),
                                    tooltip: 'Remove manager',
                                    icon: const Icon(AppIcons.close),
                                    onPressed: () async {
                                      final ok = await runComms(
                                        context,
                                        () => ref.read(fleetApiProvider).removeManager(org!, f.id, m.id),
                                        success: '${m.name} no longer manages this fleet.',
                                      );
                                      if (ok) reload();
                                    },
                                  )
                                : null,
                          ),
                      ],
                    ),
            ),
            const Gap(AppSpacing.md),
            AppButton(
              key: const Key('archive-fleet'),
              label: 'Archive fleet',
              icon: AppIcons.delete,
              variant: AppButtonVariant.ghost,
              onPressed: () async {
                final reason = await askReason(context, title: 'Archive ${f.name}?', confirm: 'Archive', message: 'Its managers lose access to its machines.');
                if (reason == null || !context.mounted) return;
                final ok = await runComms(context, () => ref.read(fleetApiProvider).archiveFleet(org!, f.id, reason), success: 'Fleet archived.');
                if (ok && context.mounted) {
                  ref.invalidate(fleetListProvider);
                  context.pop();
                }
              },
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _addMachines(BuildContext context, WidgetRef ref, Fleet f, VoidCallback reload) async {
    final board = await ref.read(fleetBoardProvider(null).future);
    if (!context.mounted) return;
    final candidates = board.where((m) => !f.machines.any((x) => x.id == m.id)).toList();
    final picked = await showAppSheet<List<String>>(
      context,
      title: 'Add machines to ${f.name}',
      subtitle: 'Machines move here from any other fleet',
      builder: (_) => _MachinePicker(machines: candidates),
    );
    if (picked == null || picked.isEmpty || !context.mounted) return;
    final org = ref.read(fleetOrgProvider)!;
    final ok = await runComms(context, () => ref.read(fleetApiProvider).addMachines(org, f.id, picked), success: '${picked.length} machine(s) added.');
    if (ok) reload();
  }

  Future<void> _addManager(BuildContext context, WidgetRef ref, Fleet f, VoidCallback reload) async {
    final managers = await ref.read(orgFleetManagersProvider.future);
    if (!context.mounted) return;
    final candidates = managers.where((m) => !f.fleetManagers.any((x) => x.id == m.id)).toList();
    final id = await showAppSheet<String>(
      context,
      title: 'Add a manager',
      subtitle: 'Fleet managers of your organisation',
      builder: (ctx) => candidates.isEmpty
          ? const Text('No other fleet managers. Invite one from Team access first.')
          : Column(
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final m in candidates)
                  ListRow(key: Key('pick-manager-${m.id}'), title: m.name, subtitle: m.detail, onTap: () => Navigator.of(ctx).pop(m.id)),
              ],
            ),
    );
    if (id == null || !context.mounted) return;
    final ok = await runComms(context, () => ref.read(fleetApiProvider).addManager(ref.read(fleetOrgProvider)!, f.id, id), success: 'Manager added.');
    if (ok) reload();
  }
}

class _MachinePicker extends StatefulWidget {
  const _MachinePicker({required this.machines});
  final List<MachineStatus> machines;
  @override
  State<_MachinePicker> createState() => _MachinePickerState();
}

class _MachinePickerState extends State<_MachinePicker> {
  final Set<String> _picked = {};

  @override
  Widget build(BuildContext context) => widget.machines.isEmpty
      ? const Text('Every machine you manage is already in this fleet.')
      : Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            for (final m in widget.machines)
              CheckboxListTile(
                key: Key('pick-machine-${m.id}'),
                value: _picked.contains(m.id),
                title: Text(m.title),
                subtitle: Text([m.registrationNo, if (m.fleetName != null) 'now in ${m.fleetName}'].whereType<String>().join(' · ')),
                onChanged: (v) => setState(() => v == true ? _picked.add(m.id) : _picked.remove(m.id)),
              ),
            const Gap(AppSpacing.md),
            AppButton(
              key: const Key('pick-machines-done'),
              label: 'Add ${_picked.length} machine(s)',
              onPressed: _picked.isEmpty ? null : () => Navigator.of(context).pop(_picked.toList()),
            ),
          ],
        );
}

// ---- equipment board -------------------------------------------------------------------------------

class FleetEquipmentPage extends ConsumerStatefulWidget {
  const FleetEquipmentPage({super.key});
  @override
  ConsumerState<FleetEquipmentPage> createState() => _FleetEquipmentPageState();
}

class _FleetEquipmentPageState extends ConsumerState<FleetEquipmentPage> {
  String? _fleet;

  @override
  Widget build(BuildContext context) {
    final board = ref.watch(fleetBoardProvider(_fleet));
    final fleets = ref.watch(fleetListProvider).value ?? const <Fleet>[];
    final role = ref.watch(activeRoleProvider);
    return AppPage(
      title: 'Equipment',
      subtitle: 'Status of the machines you manage',
      actions: [
        AppIconButton(
          key: const Key('add-equipment'),
          icon: AppIcons.add,
          tooltip: 'Add a machine',
          onPressed: () => context.push(FleetPaths.equipmentNew(role)),
        ),
      ],
      body: RefreshIndicator(
        onRefresh: () async => ref.invalidate(fleetBoardProvider(_fleet)),
        child: PageBody(
          children: [
            if (fleets.isNotEmpty)
              Wrap(
                spacing: AppSpacing.sm,
                children: [
                  ChoiceChip(
                    key: const Key('fleet-filter-all'),
                    label: const Text('All'),
                    selected: _fleet == null,
                    onSelected: (_) => setState(() => _fleet = null),
                  ),
                  for (final f in fleets)
                    ChoiceChip(
                      key: Key('fleet-filter-${f.id}'),
                      label: Text(f.name),
                      selected: _fleet == f.id,
                      onSelected: (_) => setState(() => _fleet = f.id),
                    ),
                ],
              ),
            const Gap(AppSpacing.sm),
            AsyncBody(
              value: board,
              onRetry: () => ref.invalidate(fleetBoardProvider(_fleet)),
              builder: (list) => list.isEmpty
                  ? const EmptyState(icon: AppIcons.fleet, title: 'No machines', message: 'Machines appear here once an owner puts them in a fleet you manage.')
                  : Column(
                      children: [for (final m in list) _MachineCard(m: m, role: role)],
                    ),
            ),
            const Gap(AppSpacing.xl),
          ],
        ),
      ),
    );
  }
}

class _MachineCard extends StatelessWidget {
  const _MachineCard({required this.m, required this.role});
  final MachineStatus m;
  final UserRole? role;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: AppCard(
        key: Key('machine-${m.id}'),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(child: Text(m.title, style: theme.textTheme.titleSmall)),
                StatusBadge(label: m.stateLabel, tone: stateTone(m.state)),
              ],
            ),
            Text(
              [m.registrationNo, if (m.fleetName != null) m.fleetName].whereType<String>().join(' · '),
              style: theme.textTheme.bodySmall,
            ),
            if (m.currentReference != null)
              Text('Now: ${m.currentReference}${m.currentOperator == null ? '' : ' · ${m.currentOperator}'}', style: theme.textTheme.bodySmall),
            if (m.nextReference != null && m.nextStart != null)
              Text('Next: ${m.nextReference} on ${Fmt.dayDate(m.nextStart!)}', style: theme.textTheme.bodySmall),
            if (m.lastSeen != null)
              Text(
                'Last seen ${Fmt.dateTime(m.lastSeen!)} at ${m.lastLat!.toStringAsFixed(4)}, ${m.lastLng!.toStringAsFixed(4)}',
                style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500),
              ),
            const Gap(AppSpacing.sm),
            Wrap(
              spacing: AppSpacing.sm,
              children: [
                AppButton(
                  key: Key('edit-${m.id}'),
                  label: 'Edit',
                  icon: AppIcons.edit,
                  size: AppButtonSize.small,
                  variant: AppButtonVariant.secondary,
                  onPressed: () => context.push(FleetPaths.equipmentEdit(role, m.id)),
                ),
                AppButton(
                  key: Key('availability-${m.id}'),
                  label: 'Availability',
                  icon: AppIcons.calendar,
                  size: AppButtonSize.small,
                  variant: AppButtonVariant.secondary,
                  onPressed: () => context.push(FleetPaths.equipmentManage(role, m.id)),
                ),
                AppButton(
                  label: 'Maintenance${m.openMaintenance > 0 ? ' (${m.openMaintenance})' : ''}',
                  icon: AppIcons.maintenance,
                  size: AppButtonSize.small,
                  variant: AppButtonVariant.ghost,
                  onPressed: () => context.push(FleetPaths.maintenance(role)),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

// ---- operators -------------------------------------------------------------------------------------

class FleetOperatorsPage extends ConsumerWidget {
  const FleetOperatorsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final roster = ref.watch(fleetRosterProvider);
    return AppPage(
      title: 'Operators',
      actions: [
        AppIconButton(key: const Key('invite-operator'), icon: AppIcons.userPlus, tooltip: 'Invite an operator', onPressed: () => _invite(context, ref)),
      ],
      body: AsyncBody(
        value: roster,
        onRetry: () => ref.invalidate(fleetRosterProvider),
        builder: (list) => list.isEmpty
            ? const EmptyState(
                icon: AppIcons.operator,
                title: 'No operators',
                message: 'Invite operators by phone. They sign in with an OTP and complete verification.',
              )
            : PageBody(
                children: [
                  for (final o in list)
                    ListRow(
                      key: Key('operator-${o.userId}'),
                      leading: IconTile(icon: AppIcons.operator, tone: o.onDuty ? AppTone.success : AppTone.neutral),
                      title: o.name,
                      subtitle: [
                        o.phone,
                        availabilityLabel(o.availability).toLowerCase(),
                        if (o.currentBooking != null) 'on ${o.currentBooking}',
                        if (o.licenceExpiresOn != null) 'licence to ${Fmt.date(o.licenceExpiresOn!)}',
                      ].join(' · '),
                      trailing: StatusBadge(
                        label: o.blocked ? 'Restricted' : (o.verified ? 'Verified' : 'Unverified'),
                        tone: o.blocked ? AppTone.danger : (o.verified ? AppTone.success : AppTone.warning),
                      ),
                    ),
                ],
              ),
      ),
    );
  }

  Future<void> _invite(BuildContext context, WidgetRef ref) async {
    final ok = await showAppSheet<bool>(
      context,
      title: 'Invite an operator',
      subtitle: 'They sign in with an OTP sent to this number',
      builder: (_) => const _InviteForm(),
    );
    if (ok == true && context.mounted) {
      AppToast.success(context, 'Operator added. They complete their licence verification next.');
      ref.invalidate(fleetRosterProvider);
    }
  }
}

class _InviteForm extends ConsumerStatefulWidget {
  const _InviteForm();
  @override
  ConsumerState<_InviteForm> createState() => _InviteFormState();
}

class _InviteFormState extends ConsumerState<_InviteForm> {
  final _form = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _phone = TextEditingController(text: '+91');
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    _phone.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    setState(() => _busy = true);
    try {
      await ref.read(fleetApiProvider).inviteOperator(ref.read(fleetOrgProvider)!, phone: _phone.text.trim(), name: _name.text.trim());
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
        AppTextField(
          key: const Key('invite-name'),
          label: 'Full name',
          controller: _name,
          textInputAction: TextInputAction.next,
          validator: (v) => (v ?? '').trim().length < 2 ? 'Enter their name' : null,
        ),
        const Gap(AppSpacing.sm),
        AppTextField(
          key: const Key('invite-phone'),
          label: 'Mobile number',
          controller: _phone,
          keyboardType: TextInputType.phone,
          validator: (v) => RegExp(r'^\+[1-9]\d{9,14}$').hasMatch((v ?? '').trim()) ? null : 'Use the international format, e.g. +919876543210',
        ),
        const Gap(AppSpacing.md),
        AppButton(key: const Key('invite-submit'), label: 'Add operator', isLoading: _busy, onPressed: _busy ? null : _submit),
      ],
    ),
  );
}

// ---- assignments / schedule ------------------------------------------------------------------------

/// The next 30 days on your machines: bookings that need an operator first,
/// then everything else (with blocks), with assign / reassign.
class FleetAssignmentsPage extends ConsumerWidget {
  const FleetAssignmentsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final schedule = ref.watch(fleetScheduleProvider);
    return AppPage(
      title: 'Assignments & schedule',
      subtitle: 'Next 30 days',
      body: AsyncBody(
        value: schedule,
        onRetry: () => ref.invalidate(fleetScheduleProvider),
        builder: (items) {
          final needs = items.where((i) => i.needsOperator).toList();
          final rest = items.where((i) => !i.needsOperator).toList();
          if (items.isEmpty) {
            return const EmptyState(
              icon: AppIcons.calendar,
              title: 'Nothing scheduled',
              message: 'Bookings and availability blocks on your machines appear here.',
            );
          }
          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(fleetScheduleProvider),
            child: PageBody(
              children: [
                if (needs.isNotEmpty) ...[
                  const SectionHeader(title: 'Needs an operator'),
                  for (final i in needs) _ScheduleRow(item: i),
                  const Gap(AppSpacing.lg),
                ],
                const SectionHeader(title: 'Schedule'),
                for (final i in rest) _ScheduleRow(item: i),
              ],
            ),
          );
        },
      ),
    );
  }
}

class _ScheduleRow extends ConsumerWidget {
  const _ScheduleRow({required this.item});
  final ScheduleItem item;

  Future<void> _assign(BuildContext context, WidgetRef ref) async {
    final roster = await ref.read(fleetRosterProvider.future);
    if (!context.mounted) return;
    final picked = await showAppSheet<String>(
      context,
      title: item.operatorName == null ? 'Assign an operator' : 'Reassign the operator',
      subtitle: '${item.label} · ${Fmt.dateRange(item.start, item.end)}',
      builder: (ctx) => Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          for (final o in roster)
            ListRow(
              key: Key('pick-operator-${o.userId}'),
              leading: IconTile(icon: AppIcons.operator, tone: o.assignable ? AppTone.success : AppTone.neutral),
              title: o.name,
              subtitle: o.assignable
                  ? (o.currentBooking == null ? 'Available' : 'Also on ${o.currentBooking}')
                  : (o.blocked ? 'Restricted' : 'Not verified yet'),
              onTap: o.assignable ? () => Navigator.of(ctx).pop(o.userId) : null,
            ),
        ],
      ),
    );
    if (picked == null || !context.mounted) return;
    final ok = await runComms(
      context,
      () => ref.read(fleetApiProvider).assignOperator(ref.read(fleetOrgProvider)!, item.itemId, picked),
      success: 'Operator assigned.',
    );
    if (ok) _refreshFleet(ref);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final booking = item.kind == 'booking';
    final canAssign = booking && (item.status == 'confirmed' || item.status == 'operator_assigned');
    return ListRow(
      key: Key('schedule-${item.itemId}'),
      leading: IconTile(
        icon: booking ? AppIcons.bookings : AppIcons.ban,
        tone: item.needsOperator ? AppTone.accent : (booking ? AppTone.info : AppTone.neutral),
      ),
      title: '${item.equipmentTitle} · ${item.label}',
      subtitle: [Fmt.dateRange(item.start, item.end), item.status.replaceAll('_', ' '), if (item.operatorName != null) item.operatorName!].join(' · '),
      trailing: canAssign
          ? AppButton(
              key: Key('assign-${item.itemId}'),
              label: item.operatorName == null ? 'Assign' : 'Reassign',
              size: AppButtonSize.small,
              variant: item.needsOperator ? AppButtonVariant.accent : AppButtonVariant.secondary,
              onPressed: () => _assign(context, ref),
            )
          : null,
    );
  }
}

// ---- reports ---------------------------------------------------------------------------------------

class FleetReportsPage extends ConsumerStatefulWidget {
  const FleetReportsPage({super.key});
  @override
  ConsumerState<FleetReportsPage> createState() => _FleetReportsPageState();
}

class _FleetReportsPageState extends ConsumerState<FleetReportsPage> {
  int _days = 30;

  @override
  Widget build(BuildContext context) {
    final report = ref.watch(fleetUtilisationProvider(_days));
    final board = ref.watch(fleetBoardProvider(null));
    return AppPage(
      title: 'Utilisation & earnings',
      body: PageBody(
        children: [
          SegmentedButton<int>(
            key: const Key('report-period'),
            segments: const [
              ButtonSegment(value: 7, label: Text('7 days')),
              ButtonSegment(value: 30, label: Text('30 days')),
              ButtonSegment(value: 90, label: Text('90 days')),
            ],
            selected: {_days},
            onSelectionChanged: (s) => setState(() => _days = s.first),
          ),
          const Gap(AppSpacing.md),
          AsyncBody(
            value: report,
            onRetry: () => ref.invalidate(fleetUtilisationProvider(_days)),
            builder: (r) => Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                ResponsiveGrid(
                  columns: const Responsive(compact: 2, medium: 2),
                  children: [
                    StatCard(key: const Key('report-earnings'), label: 'Earned', value: money(r.earningsPaise), icon: AppIcons.earnings, tone: AppTone.accent),
                    StatCard(label: 'Booked hours', value: r.bookedHours.toStringAsFixed(0), icon: AppIcons.clock),
                  ],
                ),
                const Gap(AppSpacing.md),
                for (final row in r.rows)
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                    child: AppCard(
                      key: Key('util-${row.equipmentId}'),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Row(
                            children: [
                              Expanded(child: Text(row.title, style: Theme.of(context).textTheme.titleSmall)),
                              Text(money(row.earningsPaise)),
                            ],
                          ),
                          const Gap(4),
                          LinearProgressIndicator(value: (row.utilisationPct / 100).clamp(0, 1)),
                          const Gap(4),
                          Text(
                            '${row.utilisationPct.toStringAsFixed(0)}% · ${row.bookedHours.toStringAsFixed(0)} h · ${row.bookings} bookings',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          ),
          const Gap(AppSpacing.lg),
          const SectionHeader(title: 'Machine locations'),
          Text('Positions come from trips and active rentals of your machines only.', style: Theme.of(context).textTheme.bodySmall),
          const Gap(AppSpacing.sm),
          AsyncBody(
            value: board,
            onRetry: () => ref.invalidate(fleetBoardProvider(null)),
            builder: (list) {
              final located = list.where((m) => m.lastSeen != null).toList();
              return located.isEmpty
                  ? const Text('No positions recorded yet.')
                  : Column(
                      children: [
                        for (final m in located)
                          ListRow(
                            key: Key('location-${m.id}'),
                            dense: true,
                            leading: const IconTile(icon: AppIcons.locate),
                            title: m.title,
                            subtitle: '${m.lastLat!.toStringAsFixed(4)}, ${m.lastLng!.toStringAsFixed(4)} · ${Fmt.dateTime(m.lastSeen!)}',
                          ),
                      ],
                    );
            },
          ),
          const Gap(AppSpacing.xl),
        ],
      ),
    );
  }
}
