import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../data/listing_models.dart';
import '../data/listings_api.dart';
import '../../verification/presentation/verification_widgets.dart' show describeError;

export '../../verification/presentation/verification_widgets.dart' show AsyncBody, describeError, runVerificationAction;

/// Listing photo (signed link from the API) with a category placeholder
/// when there is no photo or it fails to load.
class ListingImage extends StatelessWidget {
  const ListingImage({super.key, required this.categorySlug, this.url, this.aspectRatio = 4 / 3, this.borderRadius = AppRadius.card});
  final String categorySlug;
  final String? url;
  final double aspectRatio;
  final BorderRadius borderRadius;

  @override
  Widget build(BuildContext context) {
    final placeholder = DecoratedBox(
      decoration: const BoxDecoration(
        gradient: LinearGradient(begin: Alignment.topLeft, end: Alignment.bottomRight, colors: [AppColors.navy800, AppColors.navy600]),
      ),
      child: Stack(
        fit: StackFit.expand,
        children: [
          Center(
            child: LayoutBuilder(
              builder: (context, c) => Icon(categoryIcon(categorySlug), size: (c.biggest.shortestSide * 0.38).clamp(20, 96), color: Colors.white.withValues(alpha: 0.9)),
            ),
          ),
          Positioned(left: 0, right: 0, bottom: 0, child: Container(height: 3, color: AppColors.orange500)),
        ],
      ),
    );
    final image = url == null
        ? placeholder
        : Image.network(
            url!,
            fit: BoxFit.cover,
            errorBuilder: (_, _, _) => placeholder,
            loadingBuilder: (context, child, progress) => progress == null ? child : placeholder,
          );
    return Semantics(
      image: true,
      label: 'Equipment photo',
      child: ClipRRect(borderRadius: borderRadius, child: AspectRatio(aspectRatio: aspectRatio, child: image)),
    );
  }
}

AppTone listingTone(ListingStatus s) => switch (s) {
      ListingStatus.active => AppTone.success,
      ListingStatus.pendingReview => AppTone.info,
      ListingStatus.draft || ListingStatus.paused => AppTone.neutral,
      ListingStatus.rejected => AppTone.warning,
      ListingStatus.suspended || ListingStatus.removed || ListingStatus.retired => AppTone.danger,
    };

class ListingStatusBadge extends StatelessWidget {
  const ListingStatusBadge(this.status, {super.key});
  final ListingStatus status;
  @override
  Widget build(BuildContext context) => StatusBadge(label: status.label, tone: listingTone(status), dot: true);
}

/// Owner verification shown to customers.
class OwnerTrustTag extends StatelessWidget {
  const OwnerTrustTag({super.key, required this.verified});
  final bool verified;
  @override
  Widget build(BuildContext context) => verified
      ? const StatusBadge(label: 'Verified owner', tone: AppTone.success, icon: AppIcons.verified)
      : const StatusBadge(label: 'Owner not yet verified', tone: AppTone.neutral, icon: AppIcons.shield);
}

/// A listing in a grid: photo, machine, place, price and trust signals.
class ListingTile extends StatelessWidget {
  const ListingTile({super.key, required this.listing, required this.onTap, this.showStatus = false, this.trailing, this.extra, this.overlay});
  final ListingCard listing;
  final VoidCallback onTap;
  final bool showStatus;
  final Widget? trailing;

  /// Extra line under the place (e.g. distance).
  final Widget? extra;

  /// Widget over the photo's top-right corner (e.g. a compare toggle).
  final Widget? overlay;

  @override
  Widget build(BuildContext context) {
    final l = listing;
    final theme = Theme.of(context);
    final muted = theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500);
    return AppCard(
      key: Key('listing-${l.id}'),
      padding: EdgeInsets.zero,
      onTap: onTap,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Stack(
            children: [
              ListingImage(
                categorySlug: l.categorySlug,
                url: l.coverUrl,
                aspectRatio: 16 / 10,
                borderRadius: const BorderRadius.vertical(top: Radius.circular(AppRadius.md)),
              ),
              Positioned(
                left: 8,
                top: 8,
                child: showStatus ? ListingStatusBadge(l.status) : StatusBadge(label: l.categoryName, tone: AppTone.brand),
              ),
              if (overlay != null) Positioned(right: 4, top: 4, child: overlay!),
              if (l.photoCount > 1)
                Positioned(
                  right: 8,
                  bottom: 10,
                  child: StatusBadge(label: '${l.photoCount}', icon: AppIcons.image),
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
                    Expanded(child: Text(l.title, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall)),
                    ?trailing,
                  ],
                ),
                const Gap(2),
                Text(l.machine, maxLines: 1, overflow: TextOverflow.ellipsis, style: muted),
                if (l.place.isNotEmpty)
                  Row(
                    children: [
                      const Icon(AppIcons.siteMarker, size: 14, color: AppColors.slate500),
                      const Gap(4),
                      Expanded(child: Text(l.place, maxLines: 1, overflow: TextOverflow.ellipsis, style: muted)),
                    ],
                  ),
                ?extra,
                const Gap(AppSpacing.sm),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    if (l.ownerVerified) const StatusBadge(label: 'Verified owner', tone: AppTone.success, icon: AppIcons.verified),
                    if (l.delivery) const StatusBadge(label: 'Delivery', tone: AppTone.info, icon: AppIcons.tipper),
                    StatusBadge(label: l.condition.label),
                  ],
                ),
                if (l.moderationNote != null && showStatus) ...[
                  const Gap(AppSpacing.sm),
                  Text(l.moderationNote!, maxLines: 2, overflow: TextOverflow.ellipsis, style: muted?.copyWith(color: AppColors.orange700)),
                ],
                const Gap(AppSpacing.sm),
                Text(l.priceLabel ?? 'Price not set', style: theme.textTheme.titleMedium?.copyWith(color: AppColors.navy800)),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Main photo with a strip of thumbnails.
class ListingGallery extends StatefulWidget {
  const ListingGallery({super.key, required this.listing});
  final Listing listing;
  @override
  State<ListingGallery> createState() => _ListingGalleryState();
}

class _ListingGalleryState extends State<ListingGallery> {
  int _index = 0;

  @override
  Widget build(BuildContext context) {
    final photos = widget.listing.photos;
    if (photos.isEmpty) return ListingImage(categorySlug: widget.listing.categorySlug, aspectRatio: 16 / 9);
    final i = _index.clamp(0, photos.length - 1);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ListingImage(key: Key('gallery-main-$i'), categorySlug: widget.listing.categorySlug, url: photos[i].url, aspectRatio: 16 / 9),
        if (photos.length > 1) ...[
          const Gap(AppSpacing.sm),
          SizedBox(
            height: 56,
            child: ListView.separated(
              scrollDirection: Axis.horizontal,
              itemCount: photos.length,
              separatorBuilder: (_, _) => const Gap(AppSpacing.sm),
              itemBuilder: (context, n) => Semantics(
                button: true,
                selected: n == i,
                label: 'Photo ${n + 1} of ${photos.length}',
                child: GestureDetector(
                  key: Key('thumb-$n'),
                  onTap: () => setState(() => _index = n),
                  child: Container(
                    width: 80,
                    decoration: BoxDecoration(
                      borderRadius: BorderRadius.circular(8),
                      border: Border.all(color: n == i ? AppColors.orange500 : Colors.transparent, width: 2),
                    ),
                    child: ListingImage(
                      categorySlug: widget.listing.categorySlug,
                      url: photos[n].url,
                      aspectRatio: 80 / 56,
                      borderRadius: BorderRadius.circular(6),
                    ),
                  ),
                ),
              ),
            ),
          ),
        ],
      ],
    );
  }
}

class SpecsTable extends StatelessWidget {
  const SpecsTable({super.key, required this.listing});
  final Listing listing;
  @override
  Widget build(BuildContext context) {
    final l = listing;
    return Column(
      children: [
        InfoRow(label: 'Category', value: l.categoryName),
        InfoRow(label: 'Manufacturer', value: l.makeName),
        InfoRow(label: 'Model', value: l.modelName),
        if (l.year != null) InfoRow(label: 'Year', value: '${l.year}'),
        InfoRow(label: 'Condition', value: l.condition.label),
        for (final s in l.specs) InfoRow(label: s.label, value: s.value),
      ],
    );
  }
}

class PricingTable extends StatelessWidget {
  const PricingTable({super.key, required this.listing});
  final Listing listing;
  @override
  Widget build(BuildContext context) {
    final l = listing;
    if (l.pricing.isEmpty) return const Text('The owner has not set prices yet.');
    final m = l.mobilisation;
    return Column(
      children: [
        for (final p in l.pricing)
          InfoRow(
            label: 'Per ${priceUnits[p.unit] ?? p.unit}${p.withOperator ? ' · with operator' : ' · machine only'}',
            value: rupees(p.ratePaise) + (p.operatorBataPerDayPaise > 0 ? ' + ${rupees(p.operatorBataPerDayPaise)}/day bata' : ''),
          ),
        if (m != null) ...[
          const Divider(height: AppSpacing.lg),
          InfoRow(
            label: 'Delivery to site',
            value: [
              if (m.fixedPaise > 0) rupees(m.fixedPaise),
              if (m.perKmPaise > 0) '${rupees(m.perKmPaise)}/km after ${m.freeKm} km',
              if (m.fixedPaise == 0 && m.perKmPaise == 0) 'Free',
            ].join(' + '),
          ),
          if (m.needsTrailer) const InfoRow(label: 'Transport', value: 'Moved on a trailer'),
        ],
      ],
    );
  }
}

/// Six-week availability from the public calendar.
class AvailabilityCard extends ConsumerStatefulWidget {
  const AvailabilityCard({super.key, required this.listingId});
  final String listingId;
  @override
  ConsumerState<AvailabilityCard> createState() => _AvailabilityCardState();
}

class _AvailabilityCardState extends ConsumerState<AvailabilityCard> {
  DateTime _month = DateTime(DateTime.now().year, DateTime.now().month);

  @override
  Widget build(BuildContext context) {
    final busy = ref.watch(availabilityProvider(widget.listingId));
    return SectionCard(
      title: 'Availability',
      subtitle: 'Next six weeks',
      icon: AppIcons.calendar,
      child: busy.when(
        loading: () => const SizedBox(height: 120, child: LoadingView()),
        error: (e, _) => Text(describeError(e)),
        data: (ranges) {
          final booked = <DateTime>{};
          for (final r in ranges) {
            var d = MonthCalendar.dateOnly(r.start.toLocal());
            final end = r.end.toLocal();
            while (d.isBefore(end)) {
              booked.add(d);
              d = d.add(const Duration(days: 1));
            }
          }
          return Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              Text(
                ranges.isEmpty ? 'Free on every day in the next six weeks.' : 'Booked on ${booked.length} of the next 42 days.',
                key: const Key('availability-summary'),
              ),
              const Gap(AppSpacing.sm),
              MonthCalendar(month: _month, onMonthChanged: (m) => setState(() => _month = m), booked: booked, today: DateTime.now()),
            ],
          );
        },
      ),
    );
  }
}
