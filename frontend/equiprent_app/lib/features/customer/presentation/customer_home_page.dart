import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../l10n/app_localizations.dart';
import '../../../core/widgets/widgets.dart'
    hide Review, ChatMessage, AppNotification;
import '../../../core/theme/app_theme.dart' show AppTypography;
import '../../bookings/data/booking_api.dart' show customerSitesProvider;
import '../../comms/data/comms_api.dart';
import '../../comms/data/comms_models.dart';
import '../../comms/presentation/comms_widgets.dart' show BookingStatusBadge;
import '../../listings/data/listing_models.dart';
import '../../listings/data/listings_api.dart';
import '../../listings/presentation/listing_widgets.dart';
import '../customer_module.dart';

class CustomerHomePage extends ConsumerWidget {
  const CustomerHomePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final l = AppLocalizations.of(context);
    const liveStatuses = {
      'payment_pending',
      'accepted',
      'confirmed',
      'operator_assigned',
      'dispatched',
      'on_site',
      'active',
      'extension_requested',
      'return_initiated',
    };
    final live =
        (ref.watch(bookingsProvider(BookingSide.customer)).value ??
                const <BookingSummary>[])
            .where((b) => liveStatuses.contains(b.status))
            .take(3)
            .toList();
    final equipment = ref.watch(browseProvider(const BrowseFilter()));

    return AppPage(
      title: 'EQUIPRENT',
      titleWidget: const BrandLogo(compact: true),
      body: PageBody(
        children: [
          Text(
            'Rent Construction Vehicles',
            style: theme.textTheme.headlineLarge,
          ),
          const Gap(AppSpacing.xs),
          const _DeliveringTo(),
          const Gap(AppSpacing.sm),
          _SearchBar(onSearch: () => context.go(CustomerRoutes.search)),
          const Gap(AppSpacing.lg),
          if (live.isNotEmpty) ...[
            SectionHeader(
              title: l.liveNow,
              actionLabel: l.allBookings,
              onAction: () => context.go(CustomerRoutes.bookings),
            ),
            for (final b in live)
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                child: _LiveBookingCard(booking: b),
              ),
            const Gap(AppSpacing.md),
          ],
          SectionHeader(
            title: l.browseCategory,
            actionLabel: l.allCategories,
            onAction: () => context.push(CustomerRoutes.categories),
          ),
          SizedBox(
            height: 40,
            child: switch (ref.watch(categoriesProvider)) {
              AsyncData(value: final cats) => ListView.separated(
                scrollDirection: Axis.horizontal,
                itemCount: cats.length + 1,
                separatorBuilder: (_, _) => const Gap(AppSpacing.sm),
                itemBuilder: (context, i) {
                  final all = i == 0;
                  final c = all ? null : cats[i - 1];
                  return ActionChip(
                    label: Text(all ? 'All Vehicles' : c!.name),
                    shape: const StadiumBorder(
                      side: BorderSide(color: AppColors.border),
                    ),
                    backgroundColor: all ? AppColors.navy700 : AppColors.white,
                    labelStyle: theme.textTheme.labelLarge?.copyWith(
                      color: all ? AppColors.white : AppColors.textPrimary,
                    ),
                    onPressed: () => context.go(
                      all
                          ? CustomerRoutes.search
                          : CustomerRoutes.searchCategory(c!.slug),
                    ),
                  );
                },
              ),
              AsyncError(:final error) => Center(
                child: Text(describeError(error)),
              ),
              _ => const LoadingView(),
            },
          ),
          const Gap(AppSpacing.lg),
          SectionHeader(
            title: l.recentlyListed,
            subtitle: switch (equipment) {
              AsyncData(value: final page) =>
                'Showing ${page.items.take(8).length} vehicles',
              _ => null,
            },
            actionLabel: l.seeAll,
            onAction: () => context.go(CustomerRoutes.search),
          ),
          switch (equipment) {
            AsyncData(value: final page) => Column(
              children: [
                for (final listing in page.items.take(8))
                  Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.md),
                    child: _FeaturedEquipmentCard(
                      listing: listing,
                      onTap: () =>
                          context.push(CustomerRoutes.equipment(listing.id)),
                    ),
                  ),
              ],
            ),
            AsyncError(:final error) => Text(describeError(error)),
            _ => const SizedBox(height: 160, child: LoadingView()),
          },
          const Gap(AppSpacing.lg),
        ],
      ),
    );
  }
}

/// "Delivering to: Site name · City" from the customer's saved delivery site; a prompt to add one when there is
/// none. Nothing is shown while it loads or if it cannot be loaded.
class _DeliveringTo extends ConsumerWidget {
  const _DeliveringTo();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return ref
        .watch(customerSitesProvider)
        .maybeWhen(
          data: (sites) {
            final site = sites.isEmpty ? null : sites.first;
            return InkWell(
              key: const Key('delivering-to'),
              borderRadius: BorderRadius.circular(AppRadius.sm),
              onTap: () => context.go(CustomerRoutes.profile),
              child: Padding(
                padding: const EdgeInsets.symmetric(vertical: AppSpacing.xs),
                child: Row(
                  children: [
                    const Icon(
                      AppIcons.siteMarker,
                      size: 16,
                      color: AppColors.orange700,
                    ),
                    const Gap(AppSpacing.xs),
                    Flexible(
                      child: Text.rich(
                        TextSpan(
                          children: [
                            TextSpan(
                              text: site == null ? '' : 'Delivering to: ',
                              style: theme.textTheme.bodyMedium?.copyWith(
                                color: AppColors.slate600,
                              ),
                            ),
                            TextSpan(
                              text: site == null
                                  ? 'Add a delivery site'
                                  : '${site.label} · ${site.city}',
                              style: theme.textTheme.bodyMedium?.copyWith(
                                fontWeight: FontWeight.w700,
                                color: site == null
                                    ? AppColors.navy700
                                    : AppColors.textPrimary,
                              ),
                            ),
                          ],
                        ),
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                      ),
                    ),
                  ],
                ),
              ),
            );
          },
          orElse: () => const SizedBox.shrink(),
        );
  }
}

class _SearchBar extends StatelessWidget {
  const _SearchBar({required this.onSearch});

  final VoidCallback onSearch;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Semantics(
      button: true,
      label: 'Search construction equipment',
      child: InkWell(
        onTap: onSearch,
        borderRadius: AppRadius.control,
        child: Container(
          padding: const EdgeInsets.all(6),
          decoration: BoxDecoration(
            color: AppColors.white,
            borderRadius: AppRadius.control,
            border: Border.all(color: AppColors.border),
          ),
          child: Row(
            children: [
              const Padding(
                padding: EdgeInsets.symmetric(horizontal: 8),
                child: Icon(AppIcons.search, color: AppColors.slate600),
              ),
              Expanded(
                child: Text(
                  'Search excavator, loader, lift...',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: AppColors.slate600,
                  ),
                ),
              ),
              Container(
                padding: const EdgeInsets.symmetric(
                  horizontal: 16,
                  vertical: 10,
                ),
                decoration: BoxDecoration(
                  color: AppColors.navy700,
                  borderRadius: BorderRadius.circular(8),
                ),
                child: Text(
                  'Find',
                  style: theme.textTheme.labelLarge?.copyWith(
                    color: AppColors.white,
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _FeaturedEquipmentCard extends StatelessWidget {
  const _FeaturedEquipmentCard({required this.listing, required this.onTap});

  final ListingCard listing;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      padding: EdgeInsets.zero,
      onTap: onTap,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Stack(
            children: [
              ListingImage(
                categorySlug: listing.categorySlug,
                url: listing.coverUrl,
                aspectRatio: 16 / 9,
                borderRadius: const BorderRadius.vertical(
                  top: Radius.circular(AppRadius.md),
                ),
              ),
              Positioned(
                left: AppSpacing.sm,
                top: AppSpacing.sm,
                child: const StatusBadge(
                  label: 'Available Now',
                  tone: AppTone.success,
                ),
              ),
            ],
          ),
          Padding(
            padding: const EdgeInsets.all(AppSpacing.md),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Expanded(
                      child: Text(
                        listing.title,
                        maxLines: 2,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleMedium,
                      ),
                    ),
                    const Gap(AppSpacing.sm),
                    Text(
                      listing.priceLabel ?? 'Price on request',
                      style: theme.textTheme.titleMedium?.copyWith(
                        color: AppColors.navy700,
                        fontWeight: FontWeight.w700,
                      ),
                    ),
                  ],
                ),
                const Gap(AppSpacing.xs),
                Text(
                  [
                    listing.categoryName,
                    if (listing.year != null) '${listing.year}',
                    if (listing.place.isNotEmpty) listing.place,
                  ].where((x) => x.isNotEmpty).join(' • '),
                  key: Key('specs-${listing.id}'),
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: AppTypography.mono(
                    theme.textTheme.bodySmall,
                  ).copyWith(color: AppColors.slate600),
                ),
                const Gap(AppSpacing.xs),
                Row(
                  children: [
                    const Icon(
                      AppIcons.star,
                      size: 15,
                      color: AppColors.amber600,
                    ),
                    const Gap(4),
                    Text(
                      listing.rating == null
                          ? 'New listing'
                          : '${listing.rating!.toStringAsFixed(1)} (${listing.ratingCount})',
                      style: theme.textTheme.bodySmall,
                    ),
                  ],
                ),
                const Gap(AppSpacing.sm),
                Row(
                  children: [
                    Expanded(
                      child: AppButton(
                        label: 'Rent Now',
                        icon: AppIcons.bookings,
                        onPressed: onTap,
                      ),
                    ),
                    const Gap(AppSpacing.sm),
                    Expanded(
                      child: AppButton(
                        label: 'View Specs',
                        variant: AppButtonVariant.secondary,
                        onPressed: onTap,
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

class _LiveBookingCard extends StatelessWidget {
  const _LiveBookingCard({required this.booking});

  final BookingSummary booking;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final b = booking;
    final trackable = const {
      'dispatched',
      'on_site',
      'active',
      'extension_requested',
      'return_initiated',
    }.contains(b.status);
    void open() => context.push('/customer/bookings/${b.id}');
    return Material(
      color: AppColors.navy800,
      borderRadius: AppRadius.card,
      child: InkWell(
        onTap: open,
        borderRadius: AppRadius.card,
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.md),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Expanded(
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          'ACTIVE RENTAL • ${b.reference}',
                          style: theme.textTheme.labelSmall?.copyWith(
                            color: AppColors.orange500,
                            letterSpacing: 0.6,
                          ),
                        ),
                        Text(
                          b.equipmentTitle,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleLarge?.copyWith(
                            color: AppColors.white,
                          ),
                        ),
                      ],
                    ),
                  ),
                  BookingStatusBadge(b.status),
                ],
              ),
              const Gap(AppSpacing.sm),
              Text(
                Fmt.dateRange(b.start, b.end),
                style: theme.textTheme.bodyMedium?.copyWith(
                  color: AppColors.white,
                ),
              ),
              const Gap(AppSpacing.md),
              SizedBox(
                width: double.infinity,
                child: AppButton(
                  label: trackable ? 'Track Delivery' : 'Open',
                  icon: trackable ? AppIcons.tracking : null,
                  variant: AppButtonVariant.accent,
                  onPressed: trackable
                      ? () => context.push(CustomerRoutes.track(b.id))
                      : open,
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}
