import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/session/user_role.dart';
import '../../../core/widgets/widgets.dart';
import '../data/verification_api.dart';
import '../data/verification_models.dart';
import 'verification_widgets.dart';

/// Fleet manager: their company profile, its verification, and the
/// machines an owner authorised them to manage.
class FleetCompanyPage extends ConsumerWidget {
  const FleetCompanyPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final v = ref.watch(fleetManagerViewProvider);
    return AppPage(
      title: 'Company & access',
      topLevel: false,
      body: AsyncBody(
        value: v,
        onRetry: () => ref.invalidate(fleetManagerViewProvider),
        builder: (f) => PageBody(
          children: [
            ResponsiveGrid(
              columns: const Responsive(compact: 1, medium: 2),
              children: [
                SectionCard(
                  title: 'Company profile',
                  icon: AppIcons.building,
                  trailing: f.companyVerified ? const VerifiedTag() : const StatusBadge(label: 'Not verified', tone: AppTone.warning),
                  child: Column(
                    children: [
                      InfoRow(label: 'Company', value: f.organizationName),
                      InfoRow(label: 'Legal name', value: f.legalName),
                      if (f.contactName != null) InfoRow(label: 'Contact', value: f.contactName!),
                      if (f.contactPhone != null) InfoRow(label: 'Phone', value: f.contactPhone!),
                      if (f.contactEmail != null) InfoRow(label: 'Email', value: f.contactEmail!),
                    ],
                  ),
                ),
                SectionCard(
                  title: 'Authorised equipment',
                  icon: AppIcons.fleet,
                  trailing: StatusBadge(label: f.allEquipment ? 'All machines' : '${f.equipment.length} machines', tone: AppTone.brand),
                  child: f.allEquipment
                      ? const Text('You can manage every machine of this company.')
                      : f.equipment.isEmpty
                      ? const EmptyState(
                          icon: AppIcons.lock,
                          title: 'No machines assigned yet',
                          message: 'Ask the owner to give you access to machines.',
                          compact: true,
                        )
                      : Column(
                          children: [
                            for (final m in f.equipment)
                              ListRow(dense: true, title: m.title, subtitle: m.status, leading: const Icon(AppIcons.fleet, size: 20)),
                          ],
                        ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// Owner: fleet managers of the organisation and which machines each may
/// manage. Only owners can change access; the server enforces it on every
/// machine endpoint.
class OwnerTeamAccessPage extends ConsumerWidget {
  const OwnerTeamAccessPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final managers = ref.watch(fleetManagersProvider);
    return AppPage(
      title: 'Fleet manager access',
      topLevel: false,
      body: AsyncBody(
        value: managers,
        onRetry: () => ref.invalidate(fleetManagersProvider),
        builder: (list) => PageBody(
          children: [
            const InlineBanner(message: 'Fleet managers can only see and manage the machines you assign to them (plus machines they add).', tone: AppTone.info),
            const Gap(AppSpacing.md),
            if (list.isEmpty)
              const EmptyState(icon: AppIcons.users, title: 'No fleet managers', message: 'Invite a fleet manager from your team settings first.')
            else
              AppCard(
                child: Column(
                  children: [
                    for (final m in list)
                      ListRow(
                        title: m.fullName,
                        subtitle: '${m.phone} · ${m.allEquipment ? 'all machines' : '${m.grantedCount} machines'}',
                        leading: AppAvatar(name: m.fullName, size: 36),
                        trailing: TextButton(key: Key('manage-${m.userId}'), onPressed: () => _edit(context, ref, m), child: const Text('Manage')),
                      ),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }

  Future<void> _edit(BuildContext context, WidgetRef ref, FleetManagerSummary m) async {
    final org = ref.read(roleOrgProvider(UserRole.owner));
    final api = ref.read(verificationApiProvider);
    List<OwnedMachine> machines;
    try {
      machines = await api.ownedMachines(org);
    } catch (e) {
      if (context.mounted) AppToast.error(context, describeError(e));
      return;
    }
    if (!context.mounted) return;
    final result = await showAppSheet<({bool all, List<String> ids})>(
      context,
      title: 'Access for ${m.fullName}',
      builder: (_) => _AccessForm(machines: machines, all: m.allEquipment),
    );
    if (result == null || !context.mounted) return;
    final ok = await runVerificationAction(
      context,
      () => api.setFleetAccess(org, m.userId, all: result.all, equipmentIds: result.ids),
      success: 'Access updated',
    );
    if (ok) ref.invalidate(fleetManagersProvider);
  }
}

class _AccessForm extends StatefulWidget {
  const _AccessForm({required this.machines, required this.all});
  final List<OwnedMachine> machines;
  final bool all;
  @override
  State<_AccessForm> createState() => _AccessFormState();
}

class _AccessFormState extends State<_AccessForm> {
  late bool _all = widget.all;
  final _selected = <String>{};

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SwitchListTile(
          key: const Key('all-equipment'),
          contentPadding: EdgeInsets.zero,
          title: const Text('All machines'),
          subtitle: const Text('Including machines added later'),
          value: _all,
          onChanged: (v) => setState(() => _all = v),
        ),
        if (!_all) ...[
          const Divider(),
          if (widget.machines.isEmpty) const Text('You have no machines yet.'),
          ConstrainedBox(
            constraints: const BoxConstraints(maxHeight: 360),
            child: ListView(
              shrinkWrap: true,
              children: [
                for (final m in widget.machines)
                  CheckboxListTile(
                    contentPadding: EdgeInsets.zero,
                    title: Text(m.title),
                    value: _selected.contains(m.id),
                    onChanged: (v) => setState(() => v == true ? _selected.add(m.id) : _selected.remove(m.id)),
                  ),
              ],
            ),
          ),
          const Text('Replaces the current assignment.', style: TextStyle(color: AppColors.slate600)),
        ],
        const Gap(AppSpacing.lg),
        AppButton(
          key: const Key('save-access'),
          label: 'Save access',
          onPressed: () => Navigator.of(context).pop((all: _all, ids: _all ? <String>[] : _selected.toList())),
        ),
      ],
    );
  }
}
