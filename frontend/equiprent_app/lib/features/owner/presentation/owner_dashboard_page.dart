import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart'
    hide Review, ChatMessage, AppNotification;
import '../../comms/data/comms_api.dart';
import '../../comms/data/comms_models.dart';
import '../../comms/presentation/comms_widgets.dart'
    show BookingStatusBadge, askReason, money, runComms;
import '../../fleet_manager/data/fleet_api.dart';
import '../../listings/data/listing_models.dart';
import '../../listings/data/listings_api.dart';
import '../../listings/presentation/listing_widgets.dart'
    show ListingImage, ListingStatusBadge;
import '../../verification/presentation/verification_widgets.dart'
    show AsyncBody;
import '../data/owner_api.dart';
import '../owner_routes.dart';

/// Booking states that wait for the owner.
const _openRequestStatuses = {
  'requested',
  'quoted',
  'cancellation_requested',
  'extension_requested',
};

/// The owner's home: revenue, fleet use and payout at a glance, the booking requests waiting for an
/// answer (a new request can be approved or declined right here), and the fleet inventory.
class OwnerDashboardPage extends ConsumerWidget {
  const OwnerDashboardPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final bookings =
        ref.watch(bookingsProvider(BookingSide.owner)).value ??
        const <BookingSummary>[];
    final requests = bookings
        .where((b) => _openRequestStatuses.contains(b.status))
        .toList();
    final overview = ref.watch(fleetOverviewProvider);
    final earnings = ref.watch(ownerEarningsProvider);
    final dashboard = ref.watch(ownerDashboardProvider).value;
    final listings = ref.watch(myListingsProvider(null));
    final soon = DateTime.now().add(const Duration(days: 7));
    final tasks =
        (ref.watch(maintenanceHistoryProvider).value ??
                const <MaintenanceRecord>[])
            .where(
              (m) =>
                  m.open &&
                  (m.overdue ||
                      (m.scheduledFor != null &&
                          m.scheduledFor!.isBefore(soon))),
            )
            .toList();
    final duePlans =
        ref.watch(maintenanceDueProvider).value ?? const <MaintenanceDue>[];
    final compact = context.windowSize.isCompact;
    final theme = Theme.of(context);

    return AppPage(
      title: 'Manage your fleet',
      subtitle: 'Your equipment, bookings and payouts',
      actions: [
        if (!compact)
          Padding(
            padding: const EdgeInsets.only(right: AppSpacing.sm),
            child: AppButton(
              key: const Key('add-equipment'),
              label: 'Add equipment',
              icon: AppIcons.add,
              size: AppButtonSize.small,
              onPressed: () => context.push(OwnerRoutes.addEquipment),
            ),
          ),
      ],
      body: PageBody(
        children: [
          if (compact) ...[
            AppButton(
              key: const Key('add-equipment'),
              label: 'Add new equipment',
              icon: AppIcons.add,
              size: AppButtonSize.large,
              expand: true,
              onPressed: () => context.push(OwnerRoutes.addEquipment),
            ),
            const Gap(AppSpacing.md),
          ],
          ResponsiveGrid(
            columns: const Responsive(compact: 2, medium: 3, expanded: 3),
            children: [
              StatCard(
                key: const Key('stat-revenue'),
                label: 'Rental revenue · 30 days',
                value: overview.when(
                  data: (o) => money(o.revenue30dPaise),
                  loading: () => '…',
                  error: (_, _) => money(dashboard?.capturedLast30dPaise ?? 0),
                ),
                icon: AppIcons.payments,
                tone: AppTone.success,
                caption: '${dashboard?.bookingsCompleted ?? 0} completed',
                onTap: () => context.go(OwnerRoutes.earnings),
              ),
              StatCard(
                key: const Key('stat-fleet'),
                label: 'Fleet utilisation',
                value: overview.when(
                  data: (o) => '${o.onJob} / ${o.equipmentTotal} units',
                  loading: () => '…',
                  error: (_, _) => '—',
                ),
                icon: AppIcons.gauge,
                caption: overview.when(
                  data: (o) => '${o.idle} available',
                  loading: () => '',
                  error: (_, _) => 'Could not load · tap to retry',
                ),
                onTap: overview.hasError
                    ? () => ref.invalidate(fleetOverviewProvider)
                    : () => context.go(OwnerRoutes.listings),
              ),
              _PayoutCard(earnings: earnings),
            ],
          ),
          const Gap(AppSpacing.lg),
          SectionHeader(
            title: requests.isEmpty
                ? 'Booking requests'
                : 'Incoming booking requests (${requests.length})',
            actionLabel: 'All bookings',
            onAction: () => context.go(OwnerRoutes.bookings),
          ),
          if (requests.isEmpty)
            const AppCard(
              child: EmptyState(
                icon: AppIcons.checkCircle,
                title: 'All caught up',
                message: 'New booking requests will appear here.',
                compact: true,
              ),
            )
          else
            ResponsiveGrid(
              columns: const Responsive(compact: 1, medium: 1, expanded: 2),
              children: [for (final b in requests) _RequestCard(booking: b)],
            ),
          const Gap(AppSpacing.lg),
          SectionHeader(
            title: 'Fleet inventory',
            actionLabel: 'All equipment',
            onAction: () => context.go(OwnerRoutes.listings),
          ),
          AsyncBody(
            value: listings,
            onRetry: () => ref.invalidate(myListingsProvider(null)),
            builder: (page) => page.items.isEmpty
                ? const AppCard(
                    child: EmptyState(
                      icon: AppIcons.fleet,
                      title: 'No equipment yet',
                      message:
                          'Add your first machine to start receiving requests.',
                      compact: true,
                    ),
                  )
                : ResponsiveGrid(
                    columns: const Responsive(
                      compact: 1,
                      medium: 1,
                      expanded: 2,
                    ),
                    children: [
                      for (final l in page.items.take(6))
                        _InventoryCard(listing: l),
                    ],
                  ),
          ),
          const Gap(AppSpacing.lg),
          SectionCard(
            title: 'Maintenance alerts',
            icon: AppIcons.maintenance,
            trailing: TextButton(
              onPressed: () => context.go(OwnerRoutes.maintenance),
              child: const Text('View'),
            ),
            child: tasks.isEmpty && duePlans.isEmpty
                ? Text('No service due.', style: theme.textTheme.bodyMedium)
                : Column(
                    children: [
                      for (final m in tasks)
                        ListRow(
                          dense: true,
                          title: m.task,
                          subtitle:
                              '${m.equipmentTitle}${m.scheduledFor == null ? '' : ' · ${Fmt.shortDate(m.scheduledFor!)}'}',
                          trailing: StatusBadge(
                            label: m.overdue ? 'Overdue' : 'Due soon',
                            tone: m.overdue ? AppTone.danger : AppTone.warning,
                          ),
                          onTap: () => context.go(OwnerRoutes.maintenance),
                        ),
                      for (final d in duePlans)
                        ListRow(
                          dense: true,
                          title: d.task,
                          subtitle: d.equipmentTitle,
                          trailing: const StatusBadge(
                            label: 'Plan due',
                            tone: AppTone.warning,
                          ),
                          onTap: () => context.go(OwnerRoutes.maintenance),
                        ),
                    ],
                  ),
          ),
          const Gap(AppSpacing.xl),
        ],
      ),
    );
  }
}

/// Money waiting to be paid out, with a way to the earnings screen. (Payouts are sent by the platform
/// to the verified bank account, so there is nothing to "withdraw" here.)
class _PayoutCard extends StatelessWidget {
  const _PayoutCard({required this.earnings});
  final AsyncValue<OwnerEarnings> earnings;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final info = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(
          'Ready for payout',
          style: theme.textTheme.labelMedium?.copyWith(
            color: AppColors.slate600,
          ),
        ),
        Text(
          earnings.when(
            data: (e) => money(e.pendingPaise),
            loading: () => '…',
            error: (_, _) => '—',
          ),
          style: theme.textTheme.headlineSmall,
        ),
        Text(
          earnings.maybeWhen(
            data: (e) => '${money(e.paidOutPaise)} paid so far',
            orElse: () => '',
          ),
          style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500),
        ),
      ],
    );
    final button = AppButton(
      key: const Key('payout-earnings'),
      label: 'Earnings',
      icon: AppIcons.wallet,
      variant: AppButtonVariant.accent,
      size: AppButtonSize.small,
      onPressed: () => context.go(OwnerRoutes.earnings),
    );
    return AppCard(
      key: const Key('stat-payout'),
      // Side by side when there is room, stacked in a narrow (phone) column.
      child: LayoutBuilder(
        builder: (context, box) => box.maxWidth >= 300
            ? Row(
                children: [
                  Expanded(child: info),
                  const Gap(AppSpacing.sm),
                  button,
                ],
              )
            : Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [info, const Gap(AppSpacing.sm), button],
              ),
      ),
    );
  }
}

/// A booking waiting for the owner. A new request can be approved or declined here (the same
/// calls as the booking page); cancellations and extensions open the booking to be reviewed.
class _RequestCard extends ConsumerWidget {
  const _RequestCard({required this.booking});
  final BookingSummary booking;

  Future<void> _respond(
    BuildContext context,
    WidgetRef ref, {
    required bool accept,
  }) async {
    String? reason;
    if (!accept) {
      reason = await askReason(
        context,
        title: 'Decline this request?',
        label: 'Reason (shown to the customer)',
        confirm: 'Decline',
      );
      if (reason == null) return;
    }
    if (!context.mounted) return;
    final ok = await runComms(
      context,
      () => ref
          .read(commsApiProvider)
          .respondToBooking(
            ref.read(sideOrgProvider(BookingSide.owner)),
            booking.id,
            accept: accept,
            reason: reason,
          ),
      success: accept ? 'Request accepted.' : 'Request declined.',
    );
    if (ok) ref.invalidate(bookingsProvider(BookingSide.owner));
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final fresh = booking.status == 'requested';
    void open() => context.push('/owner/bookings/${booking.id}');
    return AppCard(
      key: Key('request-${booking.id}'),
      onTap: open,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                child: Text(
                  booking.equipmentTitle,
                  style: theme.textTheme.titleMedium,
                ),
              ),
              const Gap(AppSpacing.sm),
              if (fresh)
                const StatusBadge(
                  label: 'Needs approval',
                  tone: AppTone.warning,
                )
              else
                BookingStatusBadge(booking.status),
            ],
          ),
          Text(
            booking.reference,
            style: theme.textTheme.bodySmall?.copyWith(
              color: AppColors.slate600,
            ),
          ),
          Text(
            '${Fmt.dateRange(booking.start, booking.end)} • Payout ${money(booking.totalPaise)}',
            style: theme.textTheme.labelLarge?.copyWith(
              color: AppColors.navy700,
            ),
          ),
          const Gap(AppSpacing.md),
          if (fresh)
            Row(
              children: [
                Expanded(
                  child: AppButton(
                    key: Key('approve-${booking.id}'),
                    label: 'Approve request',
                    icon: AppIcons.checkCircle,
                    variant: AppButtonVariant.success,
                    expand: true,
                    onPressed: () => _respond(context, ref, accept: true),
                  ),
                ),
                const Gap(AppSpacing.sm),
                Expanded(
                  child: AppButton(
                    key: Key('decline-${booking.id}'),
                    label: 'Decline',
                    icon: AppIcons.close,
                    variant: AppButtonVariant.secondary,
                    expand: true,
                    onPressed: () => _respond(context, ref, accept: false),
                  ),
                ),
              ],
            )
          else
            AppButton(
              key: Key('review-${booking.id}'),
              label: 'Review request',
              icon: AppIcons.checkCircle,
              expand: true,
              onPressed: open,
            ),
        ],
      ),
    );
  }
}

/// One machine of the owner's fleet.
class _InventoryCard extends StatelessWidget {
  const _InventoryCard({required this.listing});
  final ListingCard listing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final l = listing;
    final price = l.pricePaise > 0
        ? '${Fmt.inr(l.pricePaise / 100)}/${l.priceUnit ?? 'day'}'
        : 'Price not set';
    final where = l.locationLabel ?? l.cityName;
    void open() => context.push('/owner/listings/${l.id}');
    return AppCard(
      key: Key('inventory-${l.id}'),
      onTap: open,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 88,
            child: ListingImage(
              categorySlug: l.categorySlug,
              url: l.coverUrl,
              aspectRatio: 1,
              borderRadius: BorderRadius.circular(AppRadius.sm),
            ),
          ),
          const Gap(AppSpacing.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Text(l.title, style: theme.textTheme.titleMedium),
                    ),
                    const Gap(AppSpacing.sm),
                    ListingStatusBadge(l.status),
                  ],
                ),
                Text(
                  [price, ?where].join(' • '),
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: AppColors.slate600,
                  ),
                ),
                const Gap(AppSpacing.sm),
                AppButton(
                  key: Key('manage-${l.id}'),
                  label: 'Manage vehicle',
                  icon: AppIcons.edit,
                  variant: AppButtonVariant.secondary,
                  size: AppButtonSize.small,
                  onPressed: open,
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
