import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../l10n/app_localizations.dart';
import '../../../core/widgets/widgets.dart'
    hide Review, ChatMessage, AppNotification;
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
    final name = ref.watch(authControllerProvider).user?.fullName ?? '';
    final equipment = ref.watch(browseProvider(const BrowseFilter()));
    final featured = equipment.value?.items ?? const <ListingCard>[];
    final hour = DateTime.now().hour;
    final greeting = hour < 12
        ? l.greetingMorning
        : (hour < 17 ? l.greetingAfternoon : l.greetingEvening);

    return AppPage(
      title: name.isEmpty ? greeting : '$greeting, ${name.split(' ').first}',
      subtitle: l.rentVerifiedMachines,
      body: PageBody(
        children: [
          _HeroSearch(
            onSearch: () => context.go(CustomerRoutes.search),
            featured: featured.isEmpty ? null : featured.first,
          ),
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
            height: 112,
            child: switch (ref.watch(categoriesProvider)) {
              AsyncData(value: final cats) => ListView.separated(
                scrollDirection: Axis.horizontal,
                itemCount: cats.length,
                separatorBuilder: (_, _) => const Gap(AppSpacing.sm),
                itemBuilder: (context, i) {
                  final c = cats[i];
                  return SizedBox(
                    width: 112,
                    child: AppCard(
                      onTap: () =>
                          context.go(CustomerRoutes.searchCategory(c.slug)),
                      padding: const EdgeInsets.all(AppSpacing.ms),
                      semanticLabel: c.name,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          IconTile(icon: c.icon, size: 36),
                          const Spacer(),
                          Text(
                            c.name,
                            maxLines: 2,
                            overflow: TextOverflow.ellipsis,
                            style: theme.textTheme.labelLarge,
                          ),
                        ],
                      ),
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
            actionLabel: l.seeAll,
            onAction: () => context.go(CustomerRoutes.search),
          ),
          switch (equipment) {
            AsyncData(value: final page) => SizedBox(
              height: 304,
              child: ListView.separated(
                scrollDirection: Axis.horizontal,
                itemCount: page.items.take(8).length,
                separatorBuilder: (_, _) => const Gap(AppSpacing.md),
                itemBuilder: (context, index) {
                  final listing = page.items[index];
                  return SizedBox(
                    width: context.windowSize.isCompact ? 274 : 320,
                    child: _FeaturedEquipmentCard(
                      listing: listing,
                      onTap: () =>
                          context.push(CustomerRoutes.equipment(listing.id)),
                    ),
                  );
                },
              ),
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

class _HeroSearch extends StatelessWidget {
  const _HeroSearch({required this.onSearch, this.featured});

  final VoidCallback onSearch;
  final ListingCard? featured;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final photo = featured?.coverUrl;
    return ClipRRect(
      borderRadius: const BorderRadius.all(Radius.circular(AppRadius.lg)),
      child: SizedBox(
        height: 292,
        child: Stack(
          fit: StackFit.expand,
          children: [
            if (photo != null)
              Image.network(
                photo,
                fit: BoxFit.cover,
                errorBuilder: (_, _, _) => const _HeroPlaceholder(),
                loadingBuilder: (context, child, progress) =>
                    progress == null ? child : const _HeroPlaceholder(),
              )
            else
              const _HeroPlaceholder(),
            const DecoratedBox(
              decoration: BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.centerLeft,
                  end: Alignment.centerRight,
                  colors: [
                    Color(0xF20A1628),
                    Color(0xC20A1628),
                    Color(0x180A1628),
                  ],
                  stops: [0, 0.56, 1],
                ),
              ),
            ),
            Padding(
              padding: const EdgeInsets.all(AppSpacing.lg),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Container(
                    padding: const EdgeInsets.symmetric(
                      horizontal: AppSpacing.sm,
                      vertical: AppSpacing.xs,
                    ),
                    decoration: BoxDecoration(
                      color: AppColors.white.withValues(alpha: 0.14),
                      borderRadius: BorderRadius.circular(AppRadius.full),
                    ),
                    child: Text(
                      'EQUIPMENT FOR EVERY PROJECT',
                      style: theme.textTheme.labelSmall?.copyWith(
                        color: AppColors.white,
                        letterSpacing: 0.8,
                      ),
                    ),
                  ),
                  const Spacer(),
                  ConstrainedBox(
                    constraints: const BoxConstraints(maxWidth: 390),
                    child: Text(
                      'Rent the right equipment, right here.',
                      style: theme.textTheme.headlineSmall?.copyWith(
                        color: AppColors.white,
                        fontWeight: FontWeight.w800,
                        height: 1.18,
                      ),
                    ),
                  ),
                  const Gap(AppSpacing.xs),
                  Text(
                    'Construction, industrial and more.',
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: AppColors.navy100,
                    ),
                  ),
                  const Gap(AppSpacing.md),
                  AppButton(
                    label: 'Search equipment',
                    icon: AppIcons.search,
                    trailingIcon: AppIcons.arrowRight,
                    variant: AppButtonVariant.accent,
                    onPressed: onSearch,
                  ),
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _HeroPlaceholder extends StatelessWidget {
  const _HeroPlaceholder();

  @override
  Widget build(BuildContext context) => const DecoratedBox(
    decoration: BoxDecoration(
      gradient: LinearGradient(
        begin: Alignment.topRight,
        end: Alignment.bottomLeft,
        colors: [AppColors.navy600, AppColors.navy950],
      ),
    ),
    child: Center(
      child: Icon(AppIcons.excavator, size: 112, color: AppColors.navy200),
    ),
  );
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
                child: StatusBadge(label: listing.categoryName),
              ),
            ],
          ),
          Padding(
            padding: const EdgeInsets.all(AppSpacing.md),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  listing.title,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.titleSmall,
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
                    const Spacer(),
                    if (listing.place.isNotEmpty) ...[
                      const Icon(
                        AppIcons.siteMarker,
                        size: 14,
                        color: AppColors.slate500,
                      ),
                      const Gap(4),
                      Flexible(
                        child: Text(
                          listing.place,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: AppColors.slate500,
                          ),
                        ),
                      ),
                    ],
                  ],
                ),
                const Gap(AppSpacing.xs),
                Row(
                  children: [
                    Expanded(
                      child: Text(
                        listing.priceLabel ?? 'Price on request',
                        style: theme.textTheme.titleMedium?.copyWith(
                          color: AppColors.navy800,
                        ),
                      ),
                    ),
                    const StatusBadge(
                      label: 'Available',
                      tone: AppTone.success,
                      dot: true,
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
    return AppCard(
      onTap: open,
      borderColor: AppColors.navy200,
      child: Row(
        children: [
          const IconTile(icon: AppIcons.bookings),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                BookingStatusBadge(b.status),
                const Gap(6),
                Text(
                  b.equipmentTitle,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.titleSmall,
                ),
                Text(
                  '${b.reference} · ${Fmt.dateRange(b.start, b.end)}',
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: AppColors.slate500,
                  ),
                ),
              ],
            ),
          ),
          const Gap(AppSpacing.sm),
          AppButton(
            label: trackable ? 'Track' : 'Open',
            icon: trackable ? AppIcons.tracking : null,
            size: AppButtonSize.small,
            onPressed: trackable
                ? () => context.push(CustomerRoutes.track(b.id))
                : open,
          ),
        ],
      ),
    );
  }
}
