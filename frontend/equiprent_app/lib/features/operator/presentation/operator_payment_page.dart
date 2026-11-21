import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import '../data/operator_api.dart';

class OperatorPaymentPage extends ConsumerWidget {
  const OperatorPaymentPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final payments = ref.watch(myDriverPaymentsProvider);
    return AppPage(
      title: 'Driver payments',
      subtitle: 'Agreed amounts and recorded payment history',
      actions: [
        IconButton(
          key: const Key('operator-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () => ref.invalidate(myDriverPaymentsProvider),
        ),
      ],
      body: AsyncBody(
        value: payments,
        onRetry: () => ref.invalidate(myDriverPaymentsProvider),
        builder: (history) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(myDriverPaymentsProvider),
          child: PageBody(
            children: [
              const InlineBanner(
                tone: AppTone.info,
                icon: AppIcons.shield,
                title: 'Read-only payment records',
                message:
                    'Your equipment owner records payment after the completed job. Contact the owner if a record is missing.',
              ),
              const Gap(AppSpacing.md),
              if (history.data.isEmpty)
                const EmptyState(
                  icon: AppIcons.payments,
                  title: 'No driver payments yet',
                  message:
                      'Agreed payments for your bookings will appear here.',
                ),
              if (history.events.isNotEmpty) ...[
                const SectionHeader(title: 'Payment history'),
                for (final event in history.events)
                  ListRow(
                    dense: true,
                    title:
                        '${event.status == 'paid' ? 'Paid' : 'Agreed'} · ${Fmt.inr(event.amountPaise / 100)}',
                    subtitle: Fmt.dateTime(event.createdAt),
                    leading: IconTile(
                      icon: event.status == 'paid'
                          ? AppIcons.checkCircle
                          : AppIcons.history,
                      size: 34,
                    ),
                  ),
              ],
              for (final payment in history.data)
                SectionCard(
                  title: Fmt.inr(payment.amountPaise / 100),
                  subtitle: 'Booking ${payment.bookingId}',
                  icon: AppIcons.payments,
                  trailing: StatusBadge(
                    label: payment.status == 'paid' ? 'Paid' : 'Agreed',
                    tone: payment.status == 'paid'
                        ? AppTone.success
                        : AppTone.info,
                  ),
                  child: Column(
                    children: [
                      InfoRow(
                        label: 'Agreed',
                        value: Fmt.dayDate(payment.createdAt),
                      ),
                      if (payment.paidAt != null)
                        InfoRow(
                          label: 'Paid',
                          value: Fmt.dayDate(payment.paidAt!),
                        ),
                      if (payment.reference != null)
                        InfoRow(
                          label: 'Transaction reference',
                          value: payment.reference!,
                        ),
                    ],
                  ),
                ),
            ],
          ),
        ),
      ),
    );
  }
}
