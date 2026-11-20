import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../bookings/data/booking_api.dart';
import '../bookings/data/booking_models.dart';
import '../comms/data/comms_api.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import 'document_pages.dart';

/// A tax invoice, bill of supply or credit note: parties, equipment, rental
/// dates, lines with their GST, totals, payments, refunds and credit notes,
/// with the PDF. The supplier (owner) can issue credit notes here.
class InvoiceDetailPage extends ConsumerWidget {
  const InvoiceDetailPage({super.key, required this.side, required this.invoiceId});
  final BookingSide side;
  final String invoiceId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final key = (side, invoiceId);
    final inv = ref.watch(invoiceDetailProvider(key));
    Future<void> download() => openDocument(context, ref, () => ref.read(bookingApiProvider).invoiceLink(ref.read(sideOrgProvider(side)), invoiceId));
    return AppPage(
      title: inv.value?.info.title ?? 'Invoice',
      subtitle: inv.value?.info.number,
      topLevel: false,
      actions: [AppIconButton(key: const Key('invoice-pdf'), icon: AppIcons.download, tooltip: 'Download PDF', onPressed: download)],
      body: AsyncBody(
        value: inv,
        onRetry: () => ref.invalidate(invoiceDetailProvider(key)),
        builder: (d) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(invoiceDetailProvider(key)),
          child: PageBody(
            children: [
              _Header(d: d),
              const Gap(AppSpacing.md),
              ResponsiveGrid(
                columns: const Responsive(compact: 1, medium: 2),
                children: [
                  if (d.supplier != null) _PartyCard(title: 'Supplier (owner)', p: d.supplier!),
                  if (d.recipient != null) _PartyCard(title: 'Recipient (customer)', p: d.recipient!),
                ],
              ),
              if (d.equipmentTitle != null) ...[
                const Gap(AppSpacing.md),
                SectionCard(
                  title: 'Equipment and rental',
                  icon: AppIcons.backhoe,
                  child: Column(
                    children: [
                      InfoRow(label: 'Machine', value: d.equipmentTitle!),
                      if (d.registrationNo != null && d.registrationNo!.isNotEmpty) InfoRow(label: 'Registration', value: d.registrationNo!),
                      if (d.rentalStart != null && d.rentalEnd != null) InfoRow(label: 'Rental period', value: Fmt.dateRange(d.rentalStart!, d.rentalEnd!)),
                      if (d.bookingReference != null) InfoRow(label: 'Booking', value: d.bookingReference!),
                      if (d.basis != null) InfoRow(label: 'Basis', value: d.basis == 'final' ? 'Final charges' : 'Accepted estimate'),
                    ],
                  ),
                ),
              ],
              const Gap(AppSpacing.md),
              _LinesCard(d: d),
              if (d.payments.isNotEmpty) ...[const Gap(AppSpacing.md), _PaymentsCard(d: d)],
              if (d.adjustments.isNotEmpty) ...[const Gap(AppSpacing.md), _AdjustmentsCard(d: d)],
              if (d.notes.isNotEmpty) ...[
                const Gap(AppSpacing.md),
                for (final n in d.notes)
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.xs),
                    child: Text(n, style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate600)),
                  ),
              ],
              const Gap(AppSpacing.md),
              AppButton(
                key: const Key('invoice-download'),
                label: 'Download PDF',
                icon: AppIcons.download,
                variant: AppButtonVariant.secondary,
                expand: true,
                onPressed: download,
              ),
              if (side == BookingSide.owner && !d.info.isCreditNote && d.info.status == 'issued' && d.creditableTaxablePaise > 0) ...[
                const Gap(AppSpacing.sm),
                AppButton(
                  key: const Key('credit-note'),
                  label: 'Issue credit note',
                  icon: AppIcons.receipt,
                  variant: AppButtonVariant.ghost,
                  expand: true,
                  onPressed: () => _creditNote(context, ref, d),
                ),
              ],
              const Gap(AppSpacing.xl),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _creditNote(BuildContext context, WidgetRef ref, InvoiceDetail d) async {
    final note = await showAppSheet<InvoiceDetail>(
      context,
      title: 'Issue credit note',
      subtitle: 'Against ${d.info.number}. Tax is credited in proportion.',
      builder: (ctx) => _CreditNoteForm(side: side, invoice: d),
    );
    if (note == null || !context.mounted) return;
    AppToast.success(context, 'Credit note ${note.info.number} issued.');
    ref.invalidate(invoiceDetailProvider((side, invoiceId)));
    context.push(DocumentRoutes.invoice(side, note.info.id));
  }
}

class _Header extends StatelessWidget {
  const _Header({required this.d});
  final InvoiceDetail d;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(d.info.number, key: const Key('invoice-number'), style: theme.textTheme.titleMedium),
              ),
              StatusBadge(label: d.info.title, tone: d.info.isCreditNote ? AppTone.warning : AppTone.info),
            ],
          ),
          const Gap(4),
          Text(
            [
              if (d.info.issuedAt != null) 'Issued ${Fmt.date(d.info.issuedAt!)}',
              'Place of supply ${d.placeOfSupply}',
              if (d.taxPaise > 0) d.supplyType == 'intra' ? 'CGST + SGST' : 'IGST',
            ].join(' · '),
            style: theme.textTheme.bodySmall,
          ),
          if (d.originalNumber != null) Text('Against invoice ${d.originalNumber}', style: theme.textTheme.bodySmall),
          if (d.reason != null) Text('Reason: ${d.reason}', style: theme.textTheme.bodySmall),
          const Gap(AppSpacing.sm),
          Text(money(d.info.totalPaise), style: theme.textTheme.headlineSmall?.copyWith(fontFeatures: const [FontFeature.tabularFigures()])),
          if (d.creditedPaise > 0) Text('Net after credit notes: ${money(d.netPaise)}', style: theme.textTheme.bodySmall),
        ],
      ),
    );
  }
}

class _PartyCard extends StatelessWidget {
  const _PartyCard({required this.title, required this.p});
  final String title;
  final InvoiceParty p;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(title, style: theme.textTheme.labelMedium?.copyWith(color: AppColors.slate500)),
          const Gap(4),
          Text(p.legalName, style: theme.textTheme.titleSmall),
          Text(p.gstin == null || p.gstin!.isEmpty ? 'No GSTIN' : 'GSTIN ${p.gstin}', style: theme.textTheme.bodySmall),
          if (p.address != null && p.address!.isNotEmpty) Text(p.address!, style: theme.textTheme.bodySmall),
          if (p.stateCode != null && p.stateCode!.isNotEmpty) Text('State code ${p.stateCode}', style: theme.textTheme.bodySmall),
        ],
      ),
    );
  }
}

class _LinesCard extends StatelessWidget {
  const _LinesCard({required this.d});
  final InvoiceDetail d;

  static String _pct(int bps) => bps % 100 == 0 ? '${bps ~/ 100}%' : '${(bps / 100).toStringAsFixed(2)}%';
  static String _qty(double q) => q == q.roundToDouble() ? q.toInt().toString() : q.toStringAsFixed(1);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return SectionCard(
      title: 'Charges',
      icon: AppIcons.receipt,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (final l in d.lines)
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(l.description, style: theme.textTheme.bodyMedium),
                        Text(
                          'SAC ${l.sacCode} · ${_qty(l.quantity)} ${l.unit} × ${money(l.ratePaise)} · ${l.gstRateBps == 0 ? 'no GST' : 'GST ${_pct(l.gstRateBps)}'}',
                          style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500),
                        ),
                      ],
                    ),
                  ),
                  const Gap(AppSpacing.sm),
                  Text(money(l.taxablePaise), style: theme.textTheme.bodyMedium?.copyWith(fontFeatures: const [FontFeature.tabularFigures()])),
                ],
              ),
            ),
          const Divider(),
          InfoRow(label: 'Taxable value', value: money(d.subtotalPaise)),
          if (d.igstPaise > 0) InfoRow(label: 'IGST', value: money(d.igstPaise)),
          if (d.cgstPaise > 0) ...[InfoRow(label: 'CGST', value: money(d.cgstPaise)), InfoRow(label: 'SGST', value: money(d.sgstPaise))],
          InfoRow(label: 'Total', value: money(d.info.totalPaise), emphasize: true),
        ],
      ),
    );
  }
}

class _PaymentsCard extends StatelessWidget {
  const _PaymentsCard({required this.d});
  final InvoiceDetail d;

  @override
  Widget build(BuildContext context) => SectionCard(
    title: 'Payments received',
    icon: AppIcons.payments,
    child: Column(
      children: [
        for (final p in d.payments)
          ListRow(
            dense: true,
            title: '${p.purpose[0].toUpperCase()}${p.purpose.substring(1)} · ${money(p.amountPaise)}',
            subtitle: [
              if (p.receiptNumber != null && p.receiptNumber!.isNotEmpty) p.receiptNumber!,
              if (p.method != null && p.method!.isNotEmpty) p.method!.toUpperCase(),
              if (p.capturedAt != null) Fmt.date(p.capturedAt!),
            ].join(' · '),
          ),
      ],
    ),
  );
}

class _AdjustmentsCard extends StatelessWidget {
  const _AdjustmentsCard({required this.d});
  final InvoiceDetail d;

  @override
  Widget build(BuildContext context) => SectionCard(
    title: 'Refunds and adjustments',
    icon: AppIcons.refresh,
    child: Column(
      children: [
        for (final a in d.adjustments)
          ListRow(
            dense: true,
            title: '${a.kind == 'credit_note' ? 'Credit note ${a.reference ?? ''}' : 'Refund'} · ${money(a.amountPaise)}',
            subtitle: [a.status, if (a.reason != null && a.reason!.isNotEmpty) a.reason!, if (a.at != null) Fmt.date(a.at!)].join(' · '),
          ),
      ],
    ),
  );
}

class _CreditNoteForm extends ConsumerStatefulWidget {
  const _CreditNoteForm({required this.side, required this.invoice});
  final BookingSide side;
  final InvoiceDetail invoice;
  @override
  ConsumerState<_CreditNoteForm> createState() => _CreditNoteFormState();
}

class _CreditNoteFormState extends ConsumerState<_CreditNoteForm> {
  final _form = GlobalKey<FormState>();
  final _amount = TextEditingController();
  final _reason = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _amount.dispose();
    _reason.dispose();
    super.dispose();
  }

  int? get _paise {
    final v = double.tryParse(_amount.text.trim());
    return v == null ? null : (v * 100).round();
  }

  Future<void> _submit() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    setState(() => _busy = true);
    try {
      final note = await ref
          .read(bookingApiProvider)
          .creditNote(ref.read(sideOrgProvider(widget.side)), widget.invoice.info.id, amountPaise: _paise!, reason: _reason.text.trim());
      if (mounted) Navigator.of(context).pop(note);
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final max = widget.invoice.creditableTaxablePaise;
    return Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppTextField(
            key: const Key('credit-amount'),
            label: 'Taxable value to credit (₹)',
            helper: 'Up to ${money(max)}. GST is added in the same proportion as the invoice.',
            controller: _amount,
            keyboardType: const TextInputType.numberWithOptions(decimal: true),
            inputFormatters: [FilteringTextInputFormatter.allow(RegExp(r'^\d*\.?\d{0,2}'))],
            textInputAction: TextInputAction.next,
            validator: (_) {
              final p = _paise;
              if (p == null || p <= 0) return 'Enter an amount';
              if (p > max) return 'At most ${money(max)}';
              return null;
            },
          ),
          const Gap(AppSpacing.sm),
          AppTextField(
            key: const Key('credit-reason'),
            label: 'Reason',
            hint: 'e.g. Machine idle for half a day',
            controller: _reason,
            maxLines: 2,
            maxLength: 300,
            validator: (v) => (v ?? '').trim().length < 5 ? 'Give a reason (at least 5 characters)' : null,
          ),
          const Gap(AppSpacing.md),
          AppButton(key: const Key('credit-submit'), label: 'Issue credit note', isLoading: _busy, onPressed: _busy ? null : _submit),
        ],
      ),
    );
  }
}
