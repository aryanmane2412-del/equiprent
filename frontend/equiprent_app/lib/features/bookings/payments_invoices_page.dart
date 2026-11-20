import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../documents/document_pages.dart' show DocumentRoutes;
import '../verification/data/verification_api.dart' show linkOpenerProvider;
import 'data/booking_api.dart';
import 'data/booking_models.dart';

/// The customer's money view: bookings waiting for payment and the tax
/// invoices issued to the organisation.
class PaymentsInvoicesPage extends ConsumerWidget {
  const PaymentsInvoicesPage({super.key});

  static const _awaiting = {
    'accepted',
    'payment_pending',
    'payment_processing',
  };

  Future<void> _download(
    BuildContext context,
    WidgetRef ref,
    InvoiceInfo inv,
  ) async {
    try {
      final url = await ref
          .read(bookingApiProvider)
          .invoiceLink(ref.read(sideOrgProvider(BookingSide.customer)), inv.id);
      final opened = await ref.read(linkOpenerProvider)(url);
      if (!opened && context.mounted) {
        AppToast.error(context, 'Could not open the invoice.');
      }
    } catch (e) {
      if (context.mounted) AppToast.error(context, describeError(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final bookings = ref.watch(bookingsProvider(BookingSide.customer));
    final invoices = ref.watch(orgInvoicesProvider(BookingSide.customer));
    return AppPage(
      title: 'Payments & invoices',
      topLevel: false,
      body: RefreshIndicator(
        onRefresh: () async {
          ref.invalidate(bookingsProvider(BookingSide.customer));
          ref.invalidate(orgInvoicesProvider(BookingSide.customer));
        },
        child: PageBody(
          children: [
            const SectionHeader(title: 'Waiting for payment'),
            bookings.when(
              loading: () => const LinearProgressIndicator(),
              error: (e, _) => Text(describeError(e)),
              data: (list) {
                final due = list
                    .where((b) => _awaiting.contains(b.status))
                    .toList();
                if (due.isEmpty) return const Text('Nothing to pay right now.');
                return Column(
                  children: [
                    for (final b in due)
                      _DueRow(
                        b: b,
                        onTap: () => context.push('/customer/bookings/${b.id}'),
                      ),
                  ],
                );
              },
            ),
            const Gap(AppSpacing.lg),
            const SectionHeader(title: 'Invoices'),
            invoices.when(
              loading: () => const LinearProgressIndicator(),
              error: (e, _) => Text(describeError(e)),
              data: (list) => list.isEmpty
                  ? const EmptyState(
                      icon: AppIcons.receipt,
                      title: 'No invoices yet',
                      message: 'Owners issue an invoice after each rental.',
                    )
                  : Column(
                      children: [
                        for (final i in list)
                          ListRow(
                            key: Key('invoice-${i.id}'),
                            leading: const IconTile(icon: AppIcons.receipt),
                            title: i.number,
                            onTap: () => context.push(
                              DocumentRoutes.invoice(
                                BookingSide.customer,
                                i.id,
                              ),
                            ),
                            subtitle:
                                '${i.title} · ${money(i.totalPaise)}${i.issuedAt == null ? '' : ' · ${Fmt.date(i.issuedAt!)}'}',
                            trailing: AppButton(
                              key: Key('download-${i.id}'),
                              label: 'Download',
                              icon: AppIcons.download,
                              size: AppButtonSize.small,
                              variant: AppButtonVariant.secondary,
                              onPressed: () => _download(context, ref, i),
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

class _DueRow extends StatelessWidget {
  const _DueRow({required this.b, required this.onTap});
  final BookingSummary b;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
    child: AppCard(
      onTap: onTap,
      borderColor: AppColors.orange500,
      child: Row(
        children: [
          const IconTile(icon: AppIcons.payments, tone: AppTone.accent),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  b.equipmentTitle,
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                Text(
                  '${b.reference} · ${Fmt.dateRange(b.start, b.end)}',
                  style: Theme.of(context).textTheme.bodySmall,
                ),
              ],
            ),
          ),
          AppButton(
            label: 'Pay',
            size: AppButtonSize.small,
            variant: AppButtonVariant.accent,
            onPressed: onTap,
          ),
        ],
      ),
    ),
  );
}
