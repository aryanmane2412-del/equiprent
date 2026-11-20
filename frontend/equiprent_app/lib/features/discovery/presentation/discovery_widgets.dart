import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../listings/presentation/listing_widgets.dart';
import '../data/discovery_api.dart';
import '../data/discovery_models.dart';

/// Heart that saves / unsaves a listing (optimistic).
class SaveButton extends ConsumerWidget {
  const SaveButton({super.key, required this.listingId, this.onPhoto = false});
  final String listingId;
  final bool onPhoto;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final saved = ref.watch(savedIdsProvider).value?.contains(listingId) ?? false;
    final icon = Icon(saved ? Icons.favorite : Icons.favorite_border, size: 20, color: saved ? AppColors.red600 : (onPhoto ? Colors.white : AppColors.slate600));
    return IconButton(
      key: Key('save-$listingId'),
      tooltip: saved ? 'Remove from saved' : 'Save',
      style: onPhoto ? IconButton.styleFrom(backgroundColor: Colors.black.withValues(alpha: 0.35)) : null,
      icon: icon,
      onPressed: () async {
        try {
          final now = await ref.read(savedIdsProvider.notifier).toggle(listingId);
          if (context.mounted) AppToast.show(context, now ? 'Saved' : 'Removed from saved');
        } catch (e) {
          if (context.mounted) AppToast.error(context, describeError(e));
        }
      },
    );
  }
}

/// Adds a listing to the comparison tray.
class CompareToggle extends ConsumerWidget {
  const CompareToggle({super.key, required this.listingId, this.onPhoto = false});
  final String listingId;
  final bool onPhoto;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final selected = ref.watch(compareSelectionProvider).contains(listingId);
    return IconButton(
      key: Key('compare-$listingId'),
      tooltip: selected ? 'Remove from comparison' : 'Add to comparison',
      style: onPhoto ? IconButton.styleFrom(backgroundColor: selected ? AppColors.orange500 : Colors.black.withValues(alpha: 0.35)) : null,
      icon: Icon(AppIcons.compare, size: 18, color: onPhoto || selected ? (onPhoto ? Colors.white : AppColors.orange700) : AppColors.slate600),
      onPressed: () {
        if (!ref.read(compareSelectionProvider.notifier).toggle(listingId)) {
          AppToast.show(context, 'You can compare up to ${CompareSelection.max} machines.', tone: AppTone.warning);
        }
      },
    );
  }
}

String distanceLabel(double km) => km < 1 ? 'Under 1 km away' : '${km < 10 ? km.toStringAsFixed(1) : km.round()} km away';

/// A search result card: listing tile plus distance, save and compare.
class DiscoverTile extends StatelessWidget {
  const DiscoverTile({super.key, required this.item, required this.onTap});
  final DiscoverItem item;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final d = item.distanceKm;
    final muted = Theme.of(context).textTheme.bodySmall;
    return ListingTile(
      listing: item.card,
      onTap: onTap,
      trailing: SaveButton(listingId: item.id),
      overlay: CompareToggle(listingId: item.id, onPhoto: true),
      extra: d == null
          ? null
          : Padding(
              padding: const EdgeInsets.only(top: 2),
              child: Row(
                children: [
                  const Icon(AppIcons.route, size: 14, color: AppColors.navy600),
                  const Gap(4),
                  Flexible(
                    child: Text(
                      distanceLabel(d) + ((item.deliversToYou ?? false) ? ' · delivers to you' : ''),
                      key: Key('distance-${item.id}'),
                      style: muted?.copyWith(color: AppColors.navy700, fontWeight: FontWeight.w600),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                    ),
                  ),
                ],
              ),
            ),
    );
  }
}

/// Numbered pagination.
class Pager extends StatelessWidget {
  const Pager({super.key, required this.page, required this.pages, required this.onPage});
  final int page;
  final int pages;
  final ValueChanged<int> onPage;

  @override
  Widget build(BuildContext context) {
    if (pages <= 1) return const SizedBox.shrink();
    final start = (page - 2).clamp(1, (pages - 4).clamp(1, pages));
    final end = (start + 4).clamp(1, pages);
    Widget number(int n) => Padding(
          padding: const EdgeInsets.symmetric(horizontal: 2),
          child: n == page
              ? FilledButton(key: Key('page-$n'), onPressed: null, style: FilledButton.styleFrom(minimumSize: const Size(40, 40), padding: EdgeInsets.zero), child: Text('$n'))
              : OutlinedButton(key: Key('page-$n'), onPressed: () => onPage(n), style: OutlinedButton.styleFrom(minimumSize: const Size(40, 40), padding: EdgeInsets.zero), child: Text('$n')),
        );
    return Semantics(
      label: 'Page $page of $pages',
      child: Wrap(
        alignment: WrapAlignment.center,
        crossAxisAlignment: WrapCrossAlignment.center,
        runSpacing: AppSpacing.xs,
        children: [
          IconButton(key: const Key('page-prev'), tooltip: 'Previous page', onPressed: page > 1 ? () => onPage(page - 1) : null, icon: const Icon(AppIcons.chevronLeft)),
          for (var n = start; n <= end; n++) number(n),
          IconButton(key: const Key('page-next'), tooltip: 'Next page', onPressed: page < pages ? () => onPage(page + 1) : null, icon: const Icon(AppIcons.chevronRight)),
        ],
      ),
    );
  }
}
