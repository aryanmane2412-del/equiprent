import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../listings/data/listings_api.dart' show myListingsProvider;
import '../verification/data/verification_api.dart' show documentPickerProvider, linkOpenerProvider;

/// Owner maintenance: what is due and scheduled, the service and repair
/// history, costs and downtime, and the preventive plans per machine.
class MaintenancePage extends ConsumerWidget {
  const MaintenancePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    return DefaultTabController(
      length: 4,
      child: AppPage(
        title: 'Maintenance',
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: AppSpacing.xs),
            child: AppButton(
              key: const Key('add-maintenance'),
              label: 'Add',
              icon: AppIcons.add,
              size: AppButtonSize.small,
              variant: AppButtonVariant.accent,
              onPressed: () => showRecordEditor(context),
            ),
          ),
        ],
        bottom: const TabBar(isScrollable: true, tabAlignment: TabAlignment.start, tabs: [
          Tab(text: 'Upcoming'),
          Tab(text: 'History'),
          Tab(text: 'Costs'),
          Tab(text: 'Plans'),
        ]),
        body: const TabBarView(children: [_UpcomingTab(), _HistoryTab(), _CostsTab(), _PlansTab()]),
      ),
    );
  }
}

void _refreshAll(WidgetRef ref) {
  ref.invalidate(maintenanceHistoryProvider);
  ref.invalidate(maintenanceDueProvider);
  ref.invalidate(maintenanceCostsProvider);
}

String? _org(WidgetRef ref) => ref.read(sideOrgProvider(BookingSide.owner));

// ---- upcoming ------------------------------------------------------------------------------

class _UpcomingTab extends ConsumerWidget {
  const _UpcomingTab();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final due = ref.watch(maintenanceDueProvider);
    final history = ref.watch(maintenanceHistoryProvider);
    return AsyncBody(
      value: history,
      onRetry: () => _refreshAll(ref),
      builder: (records) {
        final open = records.where((r) => r.open).toList()
          ..sort((a, b) => (a.scheduledFor ?? a.createdAt).compareTo(b.scheduledFor ?? b.createdAt));
        final overdue = open.where((r) => r.overdue).length;
        final dueList = due.value ?? const <MaintenanceDue>[];
        return RefreshIndicator(
          onRefresh: () async => _refreshAll(ref),
          child: ListView(padding: const EdgeInsets.all(AppSpacing.md), children: [
            ResponsiveGrid(columns: const Responsive(compact: 3, medium: 3), children: [
              StatCard(label: 'Overdue', value: '$overdue', icon: AppIcons.warning, tone: overdue > 0 ? AppTone.danger : AppTone.neutral),
              StatCard(label: 'Scheduled', value: '${open.length}', icon: AppIcons.calendar, tone: AppTone.info),
              StatCard(label: 'Plans due', value: '${dueList.length}', icon: AppIcons.clock, tone: dueList.isEmpty ? AppTone.neutral : AppTone.warning),
            ]),
            if (due.hasError) ...[
              const Gap(AppSpacing.md),
              LoadFailedBanner(what: 'maintenance plans', onRetry: () => ref.invalidate(maintenanceDueProvider)),
            ],
            if (dueList.isNotEmpty) ...[
              const Gap(AppSpacing.lg),
              const SectionHeader(title: 'Due by plan', subtitle: 'By calendar'),
              for (final d in dueList)
                Padding(
                  padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                  child: AppCard(
                    borderColor: AppColors.orange500,
                    child: Row(children: [
                      const IconTile(icon: AppIcons.maintenance, tone: AppTone.warning),
                      const Gap(AppSpacing.ms),
                      Expanded(
                        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                          Text(d.task, style: Theme.of(context).textTheme.titleSmall),
                          Text(d.equipmentTitle, style: Theme.of(context).textTheme.bodySmall),
                        ]),
                      ),
                      AppButton(
                        label: 'Schedule',
                        size: AppButtonSize.small,
                        variant: AppButtonVariant.secondary,
                        onPressed: () => showRecordEditor(context, equipmentId: d.equipmentId, planId: d.planId, task: d.task),
                      ),
                    ]),
                  ),
                ),
            ],
            const Gap(AppSpacing.lg),
            const SectionHeader(title: 'Scheduled & in progress', subtitle: 'Downtime blocks the booking calendar'),
            if (open.isEmpty) const EmptyState(icon: AppIcons.check, title: 'Nothing scheduled', message: 'Schedule a service or log a repair with Add.', compact: true),
            for (final r in open) Padding(padding: const EdgeInsets.only(bottom: AppSpacing.sm), child: _RecordCard(r: r)),
          ]),
        );
      },
    );
  }
}

class _RecordCard extends ConsumerWidget {
  const _RecordCard({required this.r});
  final MaintenanceRecord r;

  Future<void> _update(BuildContext context, WidgetRef ref, Json body, String done) async {
    final ok = await runComms(context, () => ref.read(commsApiProvider).updateRecord(_org(ref), r.equipmentId, r.id, body), success: done);
    if (ok) _refreshAll(ref);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final tone = r.overdue ? AppTone.danger : (r.status == 'in_progress' ? AppTone.brand : AppTone.info);
    return AppCard(
      key: Key('record-${r.id}'),
      borderColor: r.overdue ? const Color(0xFFFECACA) : null,
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Row(children: [
          IconTile(icon: r.kind == 'breakdown' || r.kind == 'repair' ? AppIcons.warning : AppIcons.maintenance, tone: tone),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text(r.task, style: theme.textTheme.titleSmall),
              Text('${kindLabel(r.kind)} · ${r.equipmentTitle}', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
            ]),
          ),
          StatusBadge(label: r.overdue ? 'Overdue' : maintenanceStatusLabel(r.status), tone: tone),
        ]),
        const Gap(AppSpacing.sm),
        if (r.scheduledFor != null) InfoRow(label: 'Date', value: Fmt.date(r.scheduledFor!), icon: AppIcons.calendar),
        if (r.downtimeStart != null)
          InfoRow(
            label: 'Downtime',
            value: '${Fmt.dateRange(r.downtimeStart!, r.downtimeEnd!)}${r.blocksCalendar ? ' · blocked' : ''}',
            icon: AppIcons.lock,
          ),
        if (r.vendor != null) InfoRow(label: 'Vendor', value: r.vendor!, icon: AppIcons.building),
        const Gap(AppSpacing.sm),
        Wrap(alignment: WrapAlignment.end, spacing: AppSpacing.sm, runSpacing: AppSpacing.sm, children: [
          AppButton(
            label: 'Cancel',
            size: AppButtonSize.small,
            variant: AppButtonVariant.ghost,
            onPressed: () async {
              final ok = await showConfirmDialog(context, title: 'Cancel this job?', message: 'Any blocked dates are freed for bookings.', confirmLabel: 'Cancel job', tone: AppTone.danger);
              if (ok && context.mounted) await _update(context, ref, {'status': 'cancelled'}, 'Job cancelled.');
            },
          ),
          if (r.status == 'scheduled')
            AppButton(label: 'Start', size: AppButtonSize.small, variant: AppButtonVariant.secondary, onPressed: () => _update(context, ref, {'status': 'in_progress'}, 'Job started.')),
          AppButton(key: Key('complete-${r.id}'), label: 'Complete', size: AppButtonSize.small, onPressed: () => showCompleteSheet(context, r)),
        ]),
      ]),
    );
  }
}

// ---- history -------------------------------------------------------------------------------

class _HistoryTab extends ConsumerStatefulWidget {
  const _HistoryTab();
  @override
  ConsumerState<_HistoryTab> createState() => _HistoryTabState();
}

class _HistoryTabState extends ConsumerState<_HistoryTab> {
  String? _kind;

  @override
  Widget build(BuildContext context) {
    final history = ref.watch(maintenanceHistoryProvider);
    return AsyncBody(
      value: history,
      onRetry: () => _refreshAll(ref),
      builder: (records) {
        final past = records.where((r) => !r.open && (_kind == null || r.kind == _kind)).toList();
        return ListView(padding: const EdgeInsets.all(AppSpacing.md), children: [
          AppChoiceChips<String?>(
            options: const [null, ...maintenanceKinds],
            selected: _kind,
            onSelected: (k) => setState(() => _kind = k),
            labelOf: (k) => k == null ? 'All' : kindLabel(k),
          ),
          const Gap(AppSpacing.md),
          if (past.isEmpty) const EmptyState(icon: AppIcons.history, title: 'No service history yet', message: 'Completed services, repairs and inspections appear here.', compact: true),
          for (final r in past)
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: AppCard(
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  Row(children: [
                    Expanded(
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                        Text(r.task, style: Theme.of(context).textTheme.titleSmall),
                        Text('${kindLabel(r.kind)} · ${r.equipmentTitle}${r.performedAt == null ? '' : ' · ${Fmt.date(r.performedAt!)}'}',
                            style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
                      ]),
                    ),
                    if (r.costPaise != null) Text(money(r.costPaise!), style: Theme.of(context).textTheme.titleSmall),
                    if (r.status == 'cancelled') const StatusBadge(label: 'Cancelled'),
                  ]),
                  if (r.partsCostPaise != null || r.labourCostPaise != null)
                    Text('Parts ${money(r.partsCostPaise ?? 0)} · Labour ${money(r.labourCostPaise ?? 0)}', style: Theme.of(context).textTheme.bodySmall),
                  if (r.notes != null) Text(r.notes!, style: Theme.of(context).textTheme.bodySmall),
                  if (r.hasInvoice)
                    Align(
                      alignment: Alignment.centerRight,
                      child: TextButton.icon(
                        icon: const Icon(AppIcons.receipt, size: 16),
                        label: const Text('Invoice'),
                        onPressed: () => runComms(context, () async {
                          final url = await ref.read(commsApiProvider).invoiceLink(_org(ref), r.equipmentId, r.id);
                          await ref.read(linkOpenerProvider)(url);
                        }),
                      ),
                    ),
                ]),
              ),
            ),
        ]);
      },
    );
  }
}

// ---- costs ---------------------------------------------------------------------------------

class _CostsTab extends ConsumerWidget {
  const _CostsTab();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final costs = ref.watch(maintenanceCostsProvider);
    return AsyncBody(
      value: costs,
      onRetry: () => ref.invalidate(maintenanceCostsProvider),
      builder: (c) => ListView(padding: const EdgeInsets.all(AppSpacing.md), children: [
        Text('${c.from} to ${c.to}', style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
        const Gap(AppSpacing.sm),
        ResponsiveGrid(columns: const Responsive(compact: 1, medium: 3), children: [
          StatCard(key: const Key('cost-total'), label: 'Maintenance spend', value: money(c.totalPaise), icon: AppIcons.wallet),
          StatCard(label: 'Jobs completed', value: '${c.jobs}', icon: AppIcons.check, tone: AppTone.success),
          StatCard(label: 'Downtime', value: Fmt.hours(c.downtimeHours), icon: AppIcons.clock, tone: AppTone.warning),
        ]),
        const Gap(AppSpacing.lg),
        if (c.byEquipment.isEmpty) const EmptyState(icon: AppIcons.chartBars, title: 'No costs yet', message: 'Log costs when you complete jobs to see spend by machine.', compact: true),
        if (c.byEquipment.isNotEmpty)
          SectionCard(
            title: 'By machine',
            icon: AppIcons.fleet,
            child: HorizontalBars(points: [for (final t in c.byEquipment) SeriesPoint(t.label, rupees(t.costPaise))], format: Fmt.inr),
          ),
        if (c.byKind.isNotEmpty) ...[
          const Gap(AppSpacing.md),
          SectionCard(
            title: 'By kind',
            icon: AppIcons.maintenance,
            child: Column(children: [
              for (final t in c.byKind) InfoRow(label: '${t.label} (${t.jobs})', value: money(t.costPaise)),
            ]),
          ),
        ],
        if (c.byMonth.isNotEmpty) ...[
          const Gap(AppSpacing.md),
          SectionCard(
            title: 'By month',
            icon: AppIcons.calendar,
            child: Column(children: [
              for (final t in c.byMonth) InfoRow(label: t.label, value: '${money(t.costPaise)} · ${Fmt.hours(t.downtimeHours)} down'),
            ]),
          ),
        ],
      ]),
    );
  }
}

// ---- plans ---------------------------------------------------------------------------------

class _PlansTab extends ConsumerStatefulWidget {
  const _PlansTab();
  @override
  ConsumerState<_PlansTab> createState() => _PlansTabState();
}

class _PlansTabState extends ConsumerState<_PlansTab> {
  String? _machine;

  @override
  Widget build(BuildContext context) {
    final machines = ref.watch(myListingsProvider(null));
    return AsyncBody(
      value: machines,
      onRetry: () => ref.invalidate(myListingsProvider(null)),
      builder: (page) {
        final list = page.items;
        if (list.isEmpty) return const EmptyState(icon: AppIcons.fleet, title: 'No machines', message: 'Add a machine to plan its maintenance.');
        final selected = _machine ?? list.first.id;
        final plans = ref.watch(maintenancePlansProvider(selected));
        return ListView(padding: const EdgeInsets.all(AppSpacing.md), children: [
          AppDropdown<String>(
            label: 'Machine',
            value: selected,
            items: [for (final l in list) l.id],
            itemLabel: (id) => list.where((l) => l.id == id).firstOrNull?.title ?? id,
            onChanged: (v) => setState(() => _machine = v),
          ),
          const Gap(AppSpacing.md),
          plans.when(
            loading: () => const SkeletonCard(),
            error: (e, _) => ErrorState(message: describeError(e), onRetry: () => ref.invalidate(maintenancePlansProvider(selected))),
            data: (ps) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              if (ps.isEmpty) const Text('No plans yet. A plan reminds you when a service falls due by hours or days.'),
              for (final p in ps)
                Padding(
                  padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                  child: AppCard(
                    child: Row(children: [
                      Expanded(
                        child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                          Text(p.task, style: Theme.of(context).textTheme.titleSmall),
                          Text(
                            '${p.interval[0].toUpperCase()}${p.interval.substring(1)}'
                            '${p.lastDoneAt == null ? '' : ' · last done ${Fmt.date(p.lastDoneAt!)}'}',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                        ]),
                      ),
                      AppButton(
                        label: 'Schedule',
                        size: AppButtonSize.small,
                        variant: AppButtonVariant.secondary,
                        onPressed: () => showRecordEditor(context, equipmentId: selected, planId: p.id, task: p.task),
                      ),
                    ]),
                  ),
                ),
              const Gap(AppSpacing.sm),
              AppButton(key: const Key('add-plan'), label: 'Add plan', icon: AppIcons.add, variant: AppButtonVariant.secondary, onPressed: () => _addPlan(selected)),
            ]),
          ),
        ]);
      },
    );
  }

  Future<void> _addPlan(String equipmentId) async {
    final task = TextEditingController();
    final days = TextEditingController();
    final saved = await showAppSheet<bool>(
      context,
      title: 'Maintenance plan',
      subtitle: 'Due every so many days.',
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setSheet) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
          AppTextField(key: const Key('plan-task'), label: 'Task', hint: 'e.g. Engine oil and filters', controller: task, requiredField: true),
          const Gap(AppSpacing.sm),
          AppTextField(key: const Key('plan-days'), label: 'Every (days)', controller: days, keyboardType: TextInputType.number, inputFormatters: [FilteringTextInputFormatter.digitsOnly]),
          const Gap(AppSpacing.md),
          AppButton(
            key: const Key('save-plan'),
            label: 'Save plan',
            onPressed: () async {
              final ok = await runComms(ctx, () => ref.read(commsApiProvider).createPlan(_org(ref), equipmentId, {
                    'task': task.text.trim(),
                    if (days.text.isNotEmpty) 'every_days': int.parse(days.text),
                  }));
              if (ok && ctx.mounted) Navigator.of(ctx).pop(true);
            },
          ),
        ]),
      ),
    );
    if (saved == true && mounted) {
      ref.invalidate(maintenancePlansProvider(equipmentId));
      AppToast.success(context, 'Plan saved.');
    }
  }
}

// ---- editors ---------------------------------------------------------------------------------

int? _paise(TextEditingController c) {
  final v = double.tryParse(c.text.replaceAll(',', '').trim());
  return v == null ? null : (v * 100).round();
}

String _date(DateTime d) => '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

/// Schedules a job or logs work already done.
Future<void> showRecordEditor(BuildContext context, {String? equipmentId, String? planId, String? task}) =>
    showAppSheet<void>(context, title: 'Maintenance job', subtitle: 'Schedule a service or log a repair, inspection or breakdown.',
        builder: (_) => _RecordEditor(equipmentId: equipmentId, planId: planId, task: task));

class _RecordEditor extends ConsumerStatefulWidget {
  const _RecordEditor({this.equipmentId, this.planId, this.task});
  final String? equipmentId;
  final String? planId;
  final String? task;
  @override
  ConsumerState<_RecordEditor> createState() => _RecordEditorState();
}

class _RecordEditorState extends ConsumerState<_RecordEditor> {
  late String? _machine = widget.equipmentId;
  String _kind = 'service';
  String _status = 'scheduled';
  late final _task = TextEditingController(text: widget.task ?? '');
  final _vendor = TextEditingController();
  final _notes = TextEditingController();
  final _parts = TextEditingController();
  final _labour = TextEditingController();
  final _total = TextEditingController();
  DateTime _day = DateTime.now().add(const Duration(days: 1));
  bool _downtime = false;
  int _downDays = 1;
  bool _block = true;
  String? _invoiceId;
  String? _invoiceName;
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_task, _vendor, _notes, _parts, _labour, _total]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _pickDay() async {
    final now = DateTime.now();
    final d = await showDatePicker(
      context: context,
      initialDate: _day,
      firstDate: _status == 'done' ? now.subtract(const Duration(days: 365)) : DateTime(now.year, now.month, now.day),
      lastDate: now.add(const Duration(days: 365)),
    );
    if (d != null) setState(() => _day = d);
  }

  Future<void> _attach() async {
    final f = await ref.read(documentPickerProvider).pick();
    if (f == null || !mounted) return;
    await runComms(context, () async {
      final id = await ref.read(commsApiProvider).upload('maintenance_document', f);
      setState(() {
        _invoiceId = id;
        _invoiceName = f.name;
      });
    });
  }

  Future<void> _save() async {
    final start = DateTime(_day.year, _day.month, _day.day, 8);
    final body = <String, dynamic>{
      'kind': _kind,
      'task': _task.text.trim(),
      'status': _status,
      'plan_id': ?widget.planId,
      if (_status == 'scheduled') 'scheduled_for': _date(_day),
      if (_status == 'done') 'performed_at': DateTime(_day.year, _day.month, _day.day, 12).isAfter(DateTime.now()) ? null : DateTime(_day.year, _day.month, _day.day, 12).toUtc().toIso8601String(),
      if (_downtime) 'downtime': {'start': start.toUtc().toIso8601String(), 'end': start.add(Duration(days: _downDays)).toUtc().toIso8601String()},
      if (_downtime && _status != 'done') 'block_calendar': _block,
      'parts_cost_paise': ?_paise(_parts),
      'labour_cost_paise': ?_paise(_labour),
      'cost_paise': ?_paise(_total),
      if (_vendor.text.trim().isNotEmpty) 'vendor': _vendor.text.trim(),
      if (_notes.text.trim().isNotEmpty) 'notes': _notes.text.trim(),
      'invoice_upload_id': ?_invoiceId,
    }..removeWhere((_, v) => v == null);
    setState(() => _busy = true);
    final ok = await runComms(context, () => ref.read(commsApiProvider).createRecord(_org(ref), _machine!, body),
        success: _status == 'scheduled' ? 'Job scheduled.' : 'Saved to the service history.');
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      _refreshAll(ref);
      Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    final machines = ref.watch(myListingsProvider(null)).value?.items ?? const [];
    final valid = _machine != null && _task.text.trim().length >= 3 && !_busy;
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
      AppDropdown<String>(
        key: const Key('record-machine'),
        label: 'Machine',
        value: machines.any((l) => l.id == _machine) ? _machine : null,
        hint: 'Choose a machine',
        items: [for (final l in machines) l.id],
        itemLabel: (id) => machines.where((l) => l.id == id).firstOrNull?.title ?? id,
        onChanged: (v) => setState(() => _machine = v),
      ),
      const Gap(AppSpacing.sm),
      AppChoiceChips<String>(options: maintenanceKinds, selected: _kind, onSelected: (k) => setState(() => _kind = k), labelOf: kindLabel),
      const Gap(AppSpacing.sm),
      AppTextField(key: const Key('record-task'), label: 'What needs doing', hint: 'e.g. 500 hour service', controller: _task, requiredField: true, onChanged: (_) => setState(() {})),
      const Gap(AppSpacing.sm),
      AppChoiceChips<String>(
        options: const ['scheduled', 'in_progress', 'done'],
        selected: _status,
        onSelected: (s) => setState(() {
          _status = s;
          if (s == 'done' && _day.isAfter(DateTime.now())) _day = DateTime.now();
        }),
        labelOf: (s) => switch (s) { 'scheduled' => 'Schedule', 'in_progress' => 'In progress now', _ => 'Already done' },
      ),
      const Gap(AppSpacing.sm),
      PickerField(label: _status == 'done' ? 'Done on' : 'Date', value: Fmt.date(_day), icon: AppIcons.calendar, onTap: _pickDay),
      SwitchListTile(
        key: const Key('record-downtime'),
        contentPadding: EdgeInsets.zero,
        title: const Text('Machine out of service'),
        subtitle: Text(_downtime ? 'From ${Fmt.date(_day)} for $_downDays day${_downDays == 1 ? '' : 's'}' : 'Record downtime for this job'),
        value: _downtime,
        onChanged: (v) => setState(() => _downtime = v),
      ),
      if (_downtime) ...[
        QuantityStepper(value: _downDays, min: 1, max: 60, unit: 'days', onChanged: (v) => setState(() => _downDays = v)),
        if (_status != 'done')
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Block the booking calendar'),
            subtitle: const Text('Customers cannot book these days'),
            value: _block,
            onChanged: (v) => setState(() => _block = v),
          ),
      ],
      const Gap(AppSpacing.sm),
      Row(children: [
        Expanded(child: AppTextField(key: const Key('record-parts'), label: 'Parts (₹)', controller: _parts, keyboardType: const TextInputType.numberWithOptions(decimal: true))),
        const Gap(AppSpacing.sm),
        Expanded(child: AppTextField(key: const Key('record-labour'), label: 'Labour (₹)', controller: _labour, keyboardType: const TextInputType.numberWithOptions(decimal: true))),
      ]),
      const Gap(AppSpacing.sm),
      AppTextField(label: 'Total cost (₹)', helper: 'Leave empty to add parts and labour', controller: _total, keyboardType: const TextInputType.numberWithOptions(decimal: true)),
      const Gap(AppSpacing.sm),
      AppTextField(label: 'Vendor / workshop', controller: _vendor),
      const Gap(AppSpacing.sm),
      AppTextField(label: 'Notes', controller: _notes, maxLines: 2),
      const Gap(AppSpacing.sm),
      AppButton(
        label: _invoiceName == null ? 'Attach invoice or job card' : 'Attached: $_invoiceName',
        icon: AppIcons.attach,
        variant: AppButtonVariant.ghost,
        onPressed: _attach,
      ),
      const Gap(AppSpacing.md),
      AppButton(key: const Key('save-record'), label: 'Save', isLoading: _busy, onPressed: valid ? _save : null),
    ]);
  }
}

/// Completes a job with its costs and invoice.
Future<void> showCompleteSheet(BuildContext context, MaintenanceRecord r) =>
    showAppSheet<void>(context, title: 'Complete job', subtitle: r.task, builder: (_) => _CompleteForm(r: r));

class _CompleteForm extends ConsumerStatefulWidget {
  const _CompleteForm({required this.r});
  final MaintenanceRecord r;
  @override
  ConsumerState<_CompleteForm> createState() => _CompleteFormState();
}

class _CompleteFormState extends ConsumerState<_CompleteForm> {
  final _parts = TextEditingController();
  final _labour = TextEditingController();
  final _total = TextEditingController();
  final _notes = TextEditingController();
  String? _invoiceId;
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_parts, _labour, _total, _notes]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _save() async {
    setState(() => _busy = true);
    final body = <String, dynamic>{
      'status': 'done',
      'parts_cost_paise': ?_paise(_parts),
      'labour_cost_paise': ?_paise(_labour),
      'cost_paise': ?_paise(_total),
      if (_notes.text.trim().isNotEmpty) 'notes': _notes.text.trim(),
      'invoice_upload_id': ?_invoiceId,
    };
    final ok = await runComms(context, () => ref.read(commsApiProvider).updateRecord(_org(ref), widget.r.equipmentId, widget.r.id, body),
        success: 'Job completed. Any remaining blocked days are free again.');
    if (!mounted) return;
    setState(() => _busy = false);
    if (ok) {
      _refreshAll(ref);
      Navigator.of(context).pop();
    }
  }

  @override
  Widget build(BuildContext context) {
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
      Row(children: [
        Expanded(child: AppTextField(key: const Key('complete-parts'), label: 'Parts (₹)', controller: _parts, keyboardType: const TextInputType.numberWithOptions(decimal: true))),
        const Gap(AppSpacing.sm),
        Expanded(child: AppTextField(key: const Key('complete-labour'), label: 'Labour (₹)', controller: _labour, keyboardType: const TextInputType.numberWithOptions(decimal: true))),
      ]),
      const Gap(AppSpacing.sm),
      AppTextField(label: 'Total cost (₹)', helper: 'Leave empty to add parts and labour', controller: _total, keyboardType: const TextInputType.numberWithOptions(decimal: true)),
      const Gap(AppSpacing.sm),
      AppTextField(label: 'Notes', controller: _notes, maxLines: 2),
      const Gap(AppSpacing.sm),
      AppButton(
        label: _invoiceId == null ? 'Attach invoice' : 'Invoice attached',
        icon: AppIcons.attach,
        variant: AppButtonVariant.ghost,
        onPressed: () async {
          final f = await ref.read(documentPickerProvider).pick();
          if (f == null || !context.mounted) return;
          await runComms(context, () async {
            final id = await ref.read(commsApiProvider).upload('maintenance_document', f);
            setState(() => _invoiceId = id);
          });
        },
      ),
      const Gap(AppSpacing.md),
      AppButton(key: const Key('save-complete'), label: 'Mark as done', variant: AppButtonVariant.success, isLoading: _busy, onPressed: _busy ? null : _save),
    ]);
  }
}
