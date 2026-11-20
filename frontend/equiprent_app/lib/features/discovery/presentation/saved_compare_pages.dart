import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart';
import '../../listings/data/listing_models.dart';
import '../../listings/presentation/listing_widgets.dart';
import '../data/discovery_api.dart';
import '../data/discovery_models.dart';
import 'discover_page.dart' show DiscoverRoutes;
import 'discovery_widgets.dart';

/// The customer's saved machines.
class SavedEquipmentPage extends ConsumerWidget {
  const SavedEquipmentPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final saved = ref.watch(savedListProvider);
    return AppPage(
      title: 'Saved equipment',
      subtitle: saved.whenOrNull(data: (l) => '${l.length} saved'),
      topLevel: false,
      body: AsyncBody(
        value: saved,
        onRetry: () => ref.invalidate(savedListProvider),
        builder: (items) => items.isEmpty
            ? EmptyState(
                icon: AppIcons.heart,
                title: 'Nothing saved yet',
                message: 'Tap the heart on any machine to keep it here.',
                action: AppButton(label: 'Find equipment', onPressed: () => context.go(DiscoverRoutes.search)),
              )
            : PageBody(
                children: [
                  ResponsiveGrid(
                    columns: const Responsive(compact: 1, medium: 2, expanded: 3, large: 4),
                    children: [
                      for (final i in items)
                        Opacity(
                          opacity: i.available ? 1 : 0.55,
                          child: ListingTile(
                            listing: i.card,
                            onTap: i.available
                                ? () => context.push(DiscoverRoutes.detail(i.id))
                                : () => AppToast.show(context, 'This machine is not available for rent right now.'),
                            trailing: SaveButton(listingId: i.id),
                            overlay: i.available
                                ? CompareToggle(listingId: i.id, onPhoto: true)
                                : const StatusBadge(label: 'Unavailable', tone: AppTone.neutral),
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

/// 2 to 4 machines side by side. Ids come from the route (shareable link)
/// or the comparison tray.
class CompareListingsPage extends ConsumerWidget {
  const CompareListingsPage({super.key, this.ids});
  final List<String>? ids;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tray = ref.watch(compareSelectionProvider);
    final chosen = (ids == null || ids!.isEmpty) ? tray : ids!;
    if (chosen.length < 2) {
      return AppPage(
        title: 'Compare',
        topLevel: false,
        body: EmptyState(
          icon: AppIcons.compare,
          title: 'Pick at least two machines',
          message: 'Use the compare button on search results to add up to ${CompareSelection.max} machines.',
          action: AppButton(label: 'Find equipment', onPressed: () => context.go(DiscoverRoutes.search)),
        ),
      );
    }
    final key = chosen.take(CompareSelection.max).join(',');
    final cmp = ref.watch(comparisonProvider(key));
    return AppPage(
      title: 'Compare',
      subtitle: '${chosen.length} machines',
      topLevel: false,
      actions: [
        TextButton(
          key: const Key('compare-clear'),
          onPressed: () {
            ref.read(compareSelectionProvider.notifier).clear();
            context.go(DiscoverRoutes.search);
          },
          child: const Text('Clear'),
        ),
      ],
      body: AsyncBody(value: cmp, onRetry: () => ref.invalidate(comparisonProvider(key)), builder: (c) => _CompareTable(c: c)),
    );
  }
}

class _CompareTable extends ConsumerWidget {
  const _CompareTable({required this.c});
  final Comparison c;

  static String _lowest(Listing l, String unit) {
    final p = l.pricing.where((p) => p.unit == unit).toList();
    if (p.isEmpty) return '—';
    return rupees(p.map((x) => x.ratePaise).reduce((a, b) => a < b ? a : b));
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final ls = c.listings;
    // Highlight the cheapest day rate.
    int dayPaise(Listing l) => l.pricing.where((p) => p.unit == 'day').map((p) => p.ratePaise).fold(1 << 62, (a, b) => a < b ? a : b);
    final cheapest = ls.map(dayPaise).reduce((a, b) => a < b ? a : b);

    final rows = <(String, Widget Function(Listing))>[
      ('Price per day', (l) => Text(_lowest(l, 'day'), style: theme.textTheme.titleSmall?.copyWith(color: dayPaise(l) == cheapest && cheapest < (1 << 62) ? AppColors.green700 : null))),
      ('Price per hour', (l) => Text(_lowest(l, 'hour'))),
      ('Price per month', (l) => Text(_lowest(l, 'month'))),
      ('Category', (l) => Text(l.categoryName)),
      ('Manufacturer', (l) => Text(l.makeName)),
      ('Model', (l) => Text(l.modelName)),
      ('Year', (l) => Text(l.year?.toString() ?? '—')),
      ('Condition', (l) => Text(l.condition.label)),
      ('Location', (l) => Text(l.place.isEmpty ? '—' : l.place)),
      ('Owner', (l) => Column(crossAxisAlignment: CrossAxisAlignment.start, children: [Text(l.ownerName), OwnerTrustTag(verified: l.ownerVerified)])),
      ('Rating', (l) => Text(l.ratingCount == 0 ? 'No reviews yet' : '★ ${l.rating?.toStringAsFixed(1)} (${l.ratingCount})')),
      ('Delivery', (l) => Text(l.delivery ? 'Within ${l.serviceRadiusKm} km' : 'Pick up only')),
      ('Operator', (l) => Text([if (l.wetHire) 'With operator', if (l.dryHire) 'Machine only'].join(' · '))),
      for (final k in c.specKeys)
        (SpecValue(key: k).label, (l) => Text(l.specs.where((s) => s.key == k).firstOrNull?.value ?? '—')),
    ];

    const labelW = 140.0, colW = 210.0;
    Widget cell(Widget child, {bool header = false}) => Container(
          width: colW,
          padding: const EdgeInsets.all(AppSpacing.sm),
          alignment: Alignment.topLeft,
          child: child,
        );

    return SingleChildScrollView(
      padding: const EdgeInsets.all(AppSpacing.md),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const SizedBox(width: labelW),
                for (final l in ls)
                  cell(Column(
                    key: Key('compare-col-${l.id}'),
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      ListingImage(categorySlug: l.categorySlug, url: l.photos.where((p) => p.isCover).firstOrNull?.url ?? l.photos.firstOrNull?.url, aspectRatio: 4 / 3),
                      const Gap(AppSpacing.xs),
                      Text(l.title, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall),
                      Row(
                        children: [
                          TextButton(onPressed: () => context.push(DiscoverRoutes.detail(l.id)), child: const Text('View')),
                          const Spacer(),
                          IconButton(
                            tooltip: 'Remove from comparison',
                            icon: const Icon(AppIcons.close, size: 16),
                            onPressed: () {
                              ref.read(compareSelectionProvider.notifier).remove(l.id);
                              final rest = [for (final x in ls) if (x.id != l.id) x.id];
                              context.replace(rest.length >= 2 ? DiscoverRoutes.compareIds(rest) : DiscoverRoutes.compare);
                            },
                          ),
                        ],
                      ),
                    ],
                  )),
              ],
            ),
            for (final (i, (label, value)) in rows.indexed)
              Container(
                color: i.isEven ? AppColors.slate50 : null,
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Container(
                      width: labelW,
                      padding: const EdgeInsets.all(AppSpacing.sm),
                      child: Text(label, style: theme.textTheme.labelLarge?.copyWith(color: AppColors.slate600)),
                    ),
                    for (final l in ls) cell(value(l)),
                  ],
                ),
              ),
          ],
        ),
      ),
    );
  }
}
