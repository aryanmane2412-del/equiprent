import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart'
    hide Review, ChatMessage, AppNotification;
import '../../comms/data/comms_api.dart';
import '../../comms/presentation/comms_widgets.dart' show BookingStatusBadge;
import '../../verification/presentation/verification_widgets.dart'
    show AsyncBody;
import '../customer_module.dart';

/// Booking states in which a machine can be followed on the map.
const trackableStatuses = {
  'dispatched',
  'on_site',
  'active',
  'extension_requested',
  'return_initiated',
};

/// The Track tab: every rental that is on its way, on site or on its way back, each opening its live
/// tracking page.
class CustomerTrackPage extends ConsumerWidget {
  const CustomerTrackPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final bookings = ref.watch(bookingsProvider(BookingSide.customer));
    final theme = Theme.of(context);
    return AppPage(
      title: 'Track',
      subtitle: 'Follow your machines',
      actions: [
        IconButton(
          key: const Key('track-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () =>
              ref.invalidate(bookingsProvider(BookingSide.customer)),
        ),
      ],
      body: AsyncBody(
        value: bookings,
        onRetry: () => ref.invalidate(bookingsProvider(BookingSide.customer)),
        builder: (all) {
          final live = all
              .where((b) => trackableStatuses.contains(b.status))
              .toList();
          if (live.isEmpty) {
            return PageBody(
              children: [
                EmptyState(
                  icon: AppIcons.fleet,
                  title: 'Nothing to track yet',
                  message:
                      'When a machine is on its way or on your site, you can follow it here.',
                  action: AppButton(
                    key: const Key('track-browse'),
                    label: 'Browse vehicles',
                    icon: AppIcons.search,
                    onPressed: () => context.go(CustomerRoutes.home),
                  ),
                ),
              ],
            );
          }
          return RefreshIndicator(
            onRefresh: () async =>
                ref.invalidate(bookingsProvider(BookingSide.customer)),
            child: PageBody(
              children: [
                for (final b in live)
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                    child: AppCard(
                      key: Key('track-${b.id}'),
                      onTap: () => context.push(CustomerRoutes.track(b.id)),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.stretch,
                        children: [
                          Row(
                            crossAxisAlignment: CrossAxisAlignment.start,
                            children: [
                              Expanded(
                                child: Text(
                                  b.equipmentTitle,
                                  style: theme.textTheme.titleMedium,
                                ),
                              ),
                              const Gap(AppSpacing.sm),
                              BookingStatusBadge(b.status),
                            ],
                          ),
                          Text(
                            '${b.reference} · ${Fmt.dateRange(b.start, b.end)}',
                            style: theme.textTheme.bodySmall?.copyWith(
                              color: AppColors.slate600,
                            ),
                          ),
                          const Gap(AppSpacing.md),
                          AppButton(
                            key: Key('track-open-${b.id}'),
                            label: 'Track delivery',
                            icon: AppIcons.siteMarker,
                            variant: AppButtonVariant.accent,
                            expand: true,
                            onPressed: () =>
                                context.push(CustomerRoutes.track(b.id)),
                          ),
                        ],
                      ),
                    ),
                  ),
              ],
            ),
          );
        },
      ),
    );
  }
}
