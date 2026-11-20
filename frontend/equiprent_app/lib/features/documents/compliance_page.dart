import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../bookings/data/booking_api.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import 'data/compliance_models.dart';

/// Admin: the configuration documents depend on. Tax rules (rates, SAC,
/// legal basis) and agreement terms are data, and each records who reviewed
/// it; staging and production refuse to issue invoices or agreements from
/// unreviewed configuration.
class CompliancePage extends ConsumerWidget {
  const CompliancePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final taxes = ref.watch(taxRulesProvider);
    final templates = ref.watch(legalTemplatesProvider);
    return AppPage(
      title: 'Compliance',
      subtitle: 'Tax rules and agreement terms',
      body: RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(taxRulesProvider);
          ref.invalidate(legalTemplatesProvider);
        },
        child: PageBody(
          children: [
            const InlineBanner(
              tone: AppTone.info,
              icon: AppIcons.shield,
              title: 'Reviewed configuration',
              message:
                  'GST rates, SAC codes and legal terms are not built into the app. Record who checked each one against current law; '
                  'changing a tax rule clears its review.',
            ),
            const Gap(AppSpacing.md),
            const SectionHeader(title: 'Tax rules'),
            AsyncBody(
              value: taxes,
              onRetry: () => ref.invalidate(taxRulesProvider),
              builder: (list) => list.isEmpty
                  ? const EmptyState(icon: AppIcons.receipt, title: 'No tax rules', message: 'Add tax rules through the finance API before issuing invoices.')
                  : Column(children: [for (final t in list) _TaxRuleCard(rule: t)]),
            ),
            const Gap(AppSpacing.lg),
            const SectionHeader(title: 'Rental agreement terms'),
            AsyncBody(
              value: templates,
              onRetry: () => ref.invalidate(legalTemplatesProvider),
              builder: (list) => list.isEmpty
                  ? const EmptyState(icon: AppIcons.fileCheck, title: 'No terms', message: 'Draft terms through the admin API.')
                  : Column(children: [for (final t in list) _TemplateCard(t: t)]),
            ),
            const Gap(AppSpacing.xl),
          ],
        ),
      ),
    );
  }
}

class _TaxRuleCard extends ConsumerWidget {
  const _TaxRuleCard({required this.rule});
  final TaxRule rule;

  Future<void> _review(BuildContext context, WidgetRef ref) async {
    final done = await showAppSheet<bool>(
      context,
      title: 'Record review of ${rule.name}',
      subtitle: 'Who checked this rate and SAC against the current GST notifications?',
      builder: (_) => _ReviewForm(
        onSubmit: (name, basis, _) => ref.read(bookingApiProvider).reviewTaxRule(rule.id, reviewedBy: name, legalBasis: basis),
        basisLabel: 'Legal basis (notification / entry)',
        initialBasis: rule.legalBasis,
      ),
    );
    if (done == true && context.mounted) {
      AppToast.success(context, 'Review recorded.');
      ref.invalidate(taxRulesProvider);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: AppCard(
        key: Key('tax-${rule.code}-${rule.validFrom}'),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(child: Text('${rule.name} · ${rule.rateLabel}', style: theme.textTheme.titleSmall)),
                StatusBadge(
                  label: rule.reviewed ? 'Reviewed' : 'Needs review',
                  tone: rule.reviewed ? AppTone.success : AppTone.warning,
                  icon: rule.reviewed ? AppIcons.verified : AppIcons.warning,
                ),
              ],
            ),
            const Gap(4),
            Text(
              'SAC ${rule.sacCode} · from ${rule.validFrom}${rule.validUntil == null ? '' : ' to ${rule.validUntil}'}${rule.isActive ? '' : ' · inactive'}',
              style: theme.textTheme.bodySmall,
            ),
            Text('Applies to: ${rule.appliesTo.map((a) => a.replaceAll('_', ' ')).join(', ')}', style: theme.textTheme.bodySmall),
            if (rule.legalBasis != null) Text('Basis: ${rule.legalBasis}', style: theme.textTheme.bodySmall),
            if (rule.reviewed)
              Text('Reviewed by ${rule.reviewedBy} on ${Fmt.date(rule.reviewedAt!)}', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.green700)),
            const Gap(AppSpacing.sm),
            Align(
              alignment: Alignment.centerRight,
              child: AppButton(
                key: Key('review-tax-${rule.code}'),
                label: rule.reviewed ? 'Review again' : 'Record review',
                icon: AppIcons.userCheck,
                size: AppButtonSize.small,
                variant: AppButtonVariant.secondary,
                onPressed: () => _review(context, ref),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _TemplateCard extends ConsumerWidget {
  const _TemplateCard({required this.t});
  final LegalTemplate t;

  Future<void> _review(BuildContext context, WidgetRef ref) async {
    final done = await showAppSheet<bool>(
      context,
      title: 'Record legal review',
      subtitle: '${t.title} (${t.version})',
      builder: (_) => _ReviewForm(
        withCapacity: true,
        basisLabel: 'Opinion or reference (optional)',
        onSubmit: (name, reference, capacity) =>
            ref.read(bookingApiProvider).reviewLegalTemplate(t.id, reviewedBy: name, capacity: capacity ?? '', reference: reference),
      ),
    );
    if (done == true && context.mounted) {
      AppToast.success(context, 'Legal review recorded.');
      ref.invalidate(legalTemplatesProvider);
    }
  }

  Future<void> _publish(BuildContext context, WidgetRef ref) async {
    final ok = await runComms(
      context,
      () => ref.read(bookingApiProvider).publishLegalTemplate(t.id),
      success: 'Published. New agreements use ${t.version}; issued agreements keep their own text.',
    );
    if (ok) ref.invalidate(legalTemplatesProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final (label, tone) = switch (t.status) {
      'published' => ('In use', AppTone.success),
      'retired' => ('Retired', AppTone.neutral),
      _ => ('Draft', AppTone.info),
    };
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
      child: AppCard(
        key: Key('template-${t.version}'),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Expanded(child: Text('${t.title} · ${t.version}', style: theme.textTheme.titleSmall)),
                StatusBadge(label: label, tone: tone),
              ],
            ),
            const Gap(4),
            Text(
              t.reviewed
                  ? 'Reviewed by ${t.reviewedBy}${t.reviewerCapacity == null ? '' : ' (${t.reviewerCapacity})'} on ${Fmt.date(t.reviewedAt!)}'
                  : 'No legal review recorded',
              style: theme.textTheme.bodySmall?.copyWith(color: t.reviewed ? AppColors.green700 : AppColors.orange700),
            ),
            if (t.missing.isNotEmpty) Text('Missing: ${t.missing.join(', ')}', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.red600)),
            ExpansionTile(
              key: Key('template-clauses-${t.version}'),
              tilePadding: EdgeInsets.zero,
              title: Text('Clauses', style: theme.textTheme.labelLarge),
              children: [
                for (final e in t.sections.entries)
                  if (e.value.isNotEmpty)
                    Padding(
                      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(e.key, style: theme.textTheme.labelMedium),
                          for (final c in e.value) Text('• $c', style: theme.textTheme.bodySmall),
                        ],
                      ),
                    ),
              ],
            ),
            if (t.isDraft)
              Wrap(
                spacing: AppSpacing.sm,
                alignment: WrapAlignment.end,
                children: [
                  AppButton(
                    key: Key('review-template-${t.version}'),
                    label: 'Record review',
                    size: AppButtonSize.small,
                    variant: AppButtonVariant.secondary,
                    onPressed: () => _review(context, ref),
                  ),
                  AppButton(
                    key: Key('publish-template-${t.version}'),
                    label: 'Publish',
                    size: AppButtonSize.small,
                    onPressed: t.missing.isEmpty ? () => _publish(context, ref) : null,
                  ),
                ],
              ),
          ],
        ),
      ),
    );
  }
}

/// Name (+ capacity) and reference of the person who reviewed something.
class _ReviewForm extends StatefulWidget {
  const _ReviewForm({required this.onSubmit, required this.basisLabel, this.withCapacity = false, this.initialBasis});
  final Future<Object?> Function(String name, String? basis, String? capacity) onSubmit;
  final String basisLabel;
  final bool withCapacity;
  final String? initialBasis;
  @override
  State<_ReviewForm> createState() => _ReviewFormState();
}

class _ReviewFormState extends State<_ReviewForm> {
  final _form = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _capacity = TextEditingController();
  late final _basis = TextEditingController(text: widget.initialBasis ?? '');
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    _capacity.dispose();
    _basis.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    setState(() => _busy = true);
    try {
      final basis = _basis.text.trim();
      await widget.onSubmit(_name.text.trim(), basis.isEmpty ? null : basis, widget.withCapacity ? _capacity.text.trim() : null);
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
          key: const Key('reviewer-name'),
          label: 'Reviewed by',
          hint: 'e.g. CA Nikhil Shah',
          controller: _name,
          textInputAction: TextInputAction.next,
          validator: (v) => (v ?? '').trim().length < 2 ? 'Enter the reviewer\'s name' : null,
        ),
        if (widget.withCapacity) ...[
          const Gap(AppSpacing.sm),
          AppTextField(
            key: const Key('reviewer-capacity'),
            label: 'Capacity',
            hint: 'e.g. Advocate, in-house counsel',
            controller: _capacity,
            textInputAction: TextInputAction.next,
            validator: (v) => (v ?? '').trim().length < 2 ? 'Enter their capacity' : null,
          ),
        ],
        const Gap(AppSpacing.sm),
        AppTextField(key: const Key('review-basis'), label: widget.basisLabel, controller: _basis, maxLines: 2, maxLength: 300),
        const Gap(AppSpacing.md),
        AppButton(key: const Key('review-submit'), label: 'Record review', isLoading: _busy, onPressed: _busy ? null : _submit),
      ],
    ),
  );
}
