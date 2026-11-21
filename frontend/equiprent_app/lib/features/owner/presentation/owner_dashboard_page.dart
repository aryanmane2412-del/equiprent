import 'package:flutter/material.dart';
import '../../fleet_manager/data/fleet_api.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart'
    hide Review, ChatMessage, AppNotification;
import '../../comms/data/comms_api.dart';
import '../../comms/data/comms_models.dart';
import '../../comms/presentation/comms_widgets.dart'
    show BookingStatusBadge, money;
import '../data/owner_api.dart';
import '../owner_routes.dart';

class OwnerDashboardPage extends ConsumerWidget {
  const OwnerDashboardPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final listings = ref.watch(ownerListingsProvider);
    final bookings =
        ref.watch(bookingsProvider(BookingSide.owner)).value ??
        const <BookingSummary>[];
    final requests = bookings
        .where(
          (b) => const {
            'requested',
            'quoted',
            'cancellation_requested',
            'extension_requested',
          }.contains(b.status),
        )
        .toList();
    final active = bookings
        .where(
          (b) => const {
            'confirmed',
            'operator_assigned',
            'dispatched',
            'on_site',
            'active',
            'extension_requested',
            'return_initiated',
            'cancellation_requested',
          }.contains(b.status),
        )
        .length;
    final overview = ref.watch(fleetOverviewProvider);
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
    final dashboard = ref.watch(ownerDashboardProvider).value;

    return AppPage(
      title: 'Dashboard',
      subtitle: 'Your equipment, bookings and business performance',
      actions: [
        if (context.windowSize.atLeast(WindowSize.medium))
          Padding(
            padding: const EdgeInsets.only(right: AppSpacing.sm),
            child: AppButton(
              label: 'Add equipment',
              icon: AppIcons.add,
              variant: AppButtonVariant.accent,
              size: AppButtonSize.small,
              onPressed: () => context.push(OwnerRoutes.addEquipment),
            ),
          ),
      ],
      floatingActionButton: context.windowSize.isCompact
          ? FloatingActionButton.extended(
              onPressed: () => context.push(OwnerRoutes.addEquipment),
              icon: const Icon(AppIcons.add),
              label: const Text('Add equipment'),
            )
          : null,
      body: PageBody(
        children: [
          ResponsiveGrid(
            columns: const Responsive(compact: 2, medium: 2, expanded: 4),
            children: [
              StatCard(
                label: 'Active rentals',
                value: '${dashboard?.bookingsInProgress ?? active}',
                icon: AppIcons.fleet,
                tone: AppTone.info,
                caption:
                    '${dashboard?.equipmentTotal ?? listings.length} machines listed',
                onTap: () => context.go(OwnerRoutes.bookings),
              ),
              StatCard(
                label: 'Fleet utilisation',
                value: overview.when(
                  data: (o) => '${o.utilisationPct.toStringAsFixed(0)}%',
                  loading: () => '…',
                  error: (_, _) => '—',
                ),
                icon: AppIcons.gauge,
                caption: overview.hasError ? 'Could not load · tap to retry' : 'Machines on a job now',
                onTap: overview.hasError ? () => ref.invalidate(fleetOverviewProvider) : () => context.go(OwnerRoutes.listings),
              ),
              StatCard(
                label: 'New requests',
                value: '${requests.length}',
                icon: AppIcons.bell,
                tone: AppTone.accent,
                caption: 'Respond within 2 hrs',
                onTap: () => context.go(OwnerRoutes.bookings),
              ),
            ],
          ),
          const Gap(AppSpacing.lg),
          SplitLayout(
            sideWidth: 380,
            main: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SectionHeader(
                  title: 'Booking requests',
                  subtitle: 'Accept quickly to keep your response rate high',
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
                  for (final b in requests)
                    Padding(
                      padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                      child: AppCard(
                        onTap: () => context.push('/owner/bookings/${b.id}'),
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Row(
                              crossAxisAlignment: CrossAxisAlignment.start,
                              children: [
                                Expanded(
                                  child: Column(
                                    crossAxisAlignment:
                                        CrossAxisAlignment.start,
                                    children: [
                                      Text(
                                        b.equipmentTitle,
                                        style: Theme.of(
                                          context,
                                        ).textTheme.titleMedium,
                                      ),
                                      Text(
                                        '${b.reference} · ${Fmt.dateRange(b.start, b.end)}',
                                        style: Theme.of(
                                          context,
                                        ).textTheme.bodySmall,
                                      ),
                                      Text(
                                        'Payout ${money(b.totalPaise)}',
                                        style: Theme.of(context)
                                            .textTheme
                                            .labelLarge
                                            ?.copyWith(
                                              color: AppColors.navy700,
                                            ),
                                      ),
                                    ],
                                  ),
                                ),
                                BookingStatusBadge(b.status),
                              ],
                            ),
                            const Gap(AppSpacing.sm),
                            SizedBox(
                              width: double.infinity,
                              child: AppButton(
                                label: 'Review request',
                                icon: AppIcons.checkCircle,
                                onPressed: () =>
                                    context.push('/owner/bookings/${b.id}'),
                              ),
                            ),
                          ],
                        ),
                      ),
                    ),
              ],
            ),
            side: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SectionCard(
                  title: 'Fleet status',
                  icon: AppIcons.fleet,
                  child: Column(
                    children: [
                      for (final e in listings)
                        ListRow(
                          dense: true,
                          title: e.name,
                          subtitle: e.registration ?? e.area,
                          leading: SizedBox(
                            width: 40,
                            child: EquipmentImage(
                              categoryId: e.categoryId,
                              aspectRatio: 1,
                              borderRadius: const BorderRadius.all(
                                Radius.circular(6),
                              ),
                            ),
                          ),
                          trailing: StatusBadges.equipment(e.status),
                          onTap: () => context.go(OwnerRoutes.listings),
                        ),
                    ],
                  ),
                ),
                const Gap(AppSpacing.md),
                SectionCard(
                  title: 'Maintenance alerts',
                  icon: AppIcons.maintenance,
                  trailing: TextButton(
                    onPressed: () => context.go(OwnerRoutes.maintenance),
                    child: const Text('View'),
                  ),
                  child: tasks.isEmpty && duePlans.isEmpty
                      ? const Text('No service due.')
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
                                  tone: m.overdue
                                      ? AppTone.danger
                                      : AppTone.warning,
                                ),
                                onTap: () =>
                                    context.go(OwnerRoutes.maintenance),
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
                                onTap: () =>
                                    context.go(OwnerRoutes.maintenance),
                              ),
                          ],
                        ),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
