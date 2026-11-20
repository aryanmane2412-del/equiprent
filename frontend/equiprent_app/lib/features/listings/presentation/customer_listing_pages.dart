import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart';
import '../data/listing_models.dart';
import '../data/listings_api.dart';
import '../../discovery/presentation/discovery_widgets.dart';
import 'listing_widgets.dart';
import '../../chat/enquiry_button.dart';
import '../../reviews/review_widgets.dart' show ListingReviewsSection;

/// Customer-facing listing routes (inside the customer shell).
abstract final class ListingRoutes {
  static const categories = '/customer/categories';
  static const browse = '/customer/search';
  static String browseCategory(String slug) => '$browse?category=$slug';
  static String detail(String id) => '/customer/equipment/$id';
}

/// Every active category with its number of live listings.
class ListingCategoriesPage extends ConsumerWidget {
  const ListingCategoriesPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cats = ref.watch(categoriesProvider);
    final theme = Theme.of(context);
    return AppPage(
      title: 'Categories',
      subtitle: cats.whenOrNull(
        data: (c) =>
            '${c.fold<int>(0, (s, x) => s + x.listingCount)} machines listed',
      ),
      topLevel: false,
      body: AsyncBody(
        value: cats,
        onRetry: () => ref.invalidate(categoriesProvider),
        builder: (list) => PageBody(
          children: [
            ResponsiveGrid(
              columns: const Responsive(
                compact: 2,
                medium: 3,
                expanded: 4,
                large: 5,
              ),
              children: [
                for (final c in list)
                  AppCard(
                    key: Key('category-${c.slug}'),
                    onTap: () =>
                        context.go(ListingRoutes.browseCategory(c.slug)),
                    semanticLabel: '${c.name}, ${c.listingCount} listed',
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        IconTile(icon: c.icon, size: 40),
                        const Gap(AppSpacing.sm),
                        Text(
                          c.name,
                          maxLines: 2,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.titleSmall,
                        ),
                        Text(
                          c.listingCount == 0
                              ? 'None listed yet'
                              : '${c.listingCount} listed',
                          style: theme.textTheme.bodySmall?.copyWith(
                            color: AppColors.slate500,
                          ),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
          ],
        ),
      ),
    );
  }
}

/// Full listing for customers: photos, specs, prices, owner trust and
/// availability.
class ListingDetailPage extends ConsumerWidget {
  const ListingDetailPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final listing = ref.watch(listingProvider(id));
    return AppPage(
      title: listing.whenOrNull(data: (l) => l.title) ?? 'Equipment',
      topLevel: false,
      actions: [
        CompareToggle(listingId: id),
        SaveButton(listingId: id),
        const IconButton(
          tooltip: 'Sharing unavailable',
          icon: Icon(AppIcons.share),
          onPressed: null,
        ),
      ],
      bottomBar: listing.whenOrNull(
        data: (l) => BottomActionBar.split(
          summary: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                _fromPrice(l) ?? 'Price on request',
                style: Theme.of(context).textTheme.titleMedium,
              ),
              Text(
                l.delivery ? 'Delivery to site available' : 'Pick up from yard',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
          ),
          actions: [
            AppButton(
              key: const Key('request-booking'),
              label: l.instantBook ? 'Book now' : 'Request booking',
              icon: AppIcons.calendar,
              variant: AppButtonVariant.accent,
              onPressed: () => context.push('/customer/book/${l.id}'),
            ),
          ],
        ),
      ),
      body: AsyncBody(
        value: listing,
        onRetry: () => ref.invalidate(listingProvider(id)),
        builder: (l) => _DetailBody(l: l),
      ),
    );
  }

  static String? _fromPrice(Listing l) {
    if (l.pricing.isEmpty) return null;
    final day = l.pricing.where((p) => p.unit == 'day').toList();
    final p = (day.isEmpty ? l.pricing : day).reduce(
      (a, b) => a.ratePaise <= b.ratePaise ? a : b,
    );
    return '${rupees(p.ratePaise)} / ${priceUnits[p.unit] ?? p.unit}';
  }
}

class _DetailBody extends StatelessWidget {
  const _DetailBody({required this.l});
  final Listing l;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final main = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ListingGallery(listing: l),
        const Gap(AppSpacing.md),
        Text(l.title, style: theme.textTheme.headlineSmall),
        const Gap(4),
        Text(
          [
            l.machine,
            if (l.year != null) '${l.year}',
            if (l.place.isNotEmpty) l.place,
          ].join(' · '),
          style: theme.textTheme.bodyMedium?.copyWith(
            color: AppColors.slate600,
          ),
        ),
        const Gap(AppSpacing.sm),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            OwnerTrustTag(verified: l.ownerVerified),
            StatusBadge(label: l.condition.label),
            if (l.delivery)
              const StatusBadge(
                label: 'Delivery available',
                tone: AppTone.info,
                icon: AppIcons.tipper,
              ),
            if (l.wetHire)
              const StatusBadge(
                label: 'With operator',
                tone: AppTone.brand,
                icon: AppIcons.operator,
              ),
            if (l.dryHire)
              const StatusBadge(label: 'Machine only', tone: AppTone.brand),
            if (l.ratingCount > 0)
              StatusBadge(
                label: '★ ${l.rating?.toStringAsFixed(1)} (${l.ratingCount})',
                tone: AppTone.accent,
              ),
          ],
        ),
        if (l.description.isNotEmpty) ...[
          const Gap(AppSpacing.md),
          Text(l.description),
        ],
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Specifications',
          icon: AppIcons.ruler,
          child: SpecsTable(listing: l),
        ),
        const Gap(AppSpacing.lg),
        ListingReviewsSection(equipmentId: l.id),
      ],
    );
    final side = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionCard(
          title: 'Rental prices',
          icon: AppIcons.tag,
          subtitle: 'GST extra where applicable',
          child: PricingTable(listing: l),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Owner',
          icon: AppIcons.building,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(l.ownerName, style: theme.textTheme.titleSmall),
              const Gap(AppSpacing.xs),
              OwnerTrustTag(verified: l.ownerVerified),
              const Gap(AppSpacing.xs),
              Text(
                l.ownerVerified
                    ? 'Business documents, identity and payout account checked by EQUIPRENT.'
                    : 'This owner has not completed business verification.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: AppColors.slate600,
                ),
              ),
              if (l.serviceRadiusKm > 0) ...[
                const Gap(AppSpacing.xs),
                Text(
                  'Serves sites within ${l.serviceRadiusKm} km',
                  style: theme.textTheme.bodySmall,
                ),
              ],
              const Gap(AppSpacing.xs),
              Text(
                'Location shown is approximate (about 1 km). The exact yard address is shared once a booking is confirmed.',
                key: const Key('approximate-location'),
                style: theme.textTheme.bodySmall?.copyWith(
                  color: AppColors.slate500,
                ),
              ),
              const Gap(AppSpacing.sm),
              MessageOwnerButton(equipmentId: l.id),
            ],
          ),
        ),
        const Gap(AppSpacing.md),
        AvailabilityCard(listingId: l.id),
      ],
    );
    return PageBody(
      children: [SplitLayout(main: main, side: side)],
    );
  }
}
