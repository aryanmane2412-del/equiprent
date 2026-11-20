import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../mock/mock_store.dart';
import '../mock/models.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import '../theme/app_tone.dart';
import '../utils/formatters.dart';
import 'badges.dart';
import 'cards.dart';
import 'feedback.dart';
import 'media.dart';

/// Marketplace card for one machine: photo, badges, name, location, rating,
/// shift price, favourite and compare toggles.
class EquipmentCard extends ConsumerWidget {
  const EquipmentCard({super.key, required this.equipment, required this.onTap, this.horizontal = false});

  final Equipment equipment;
  final VoidCallback onTap;

  /// Row layout for search result lists on phones.
  final bool horizontal;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final e = equipment;
    final isFav = ref.watch(favoritesProvider).contains(e.id);
    final inCompare = ref.watch(compareProvider).contains(e.id);

    void toggleCompare() {
      final ok = ref.read(compareProvider.notifier).toggle(e.id);
      if (!ok) {
        AppToast.show(context, 'You can compare up to ${CompareNotifier.max} machines.', tone: AppTone.warning);
      } else {
        AppToast.show(context, inCompare ? 'Removed from comparison' : 'Added to comparison');
      }
    }

    final favButton = _RoundToggle(
      icon: AppIcons.heart,
      active: isFav,
      tooltip: isFav ? 'Remove from saved' : 'Save',
      onTap: () => ref.read(favoritesProvider.notifier).toggle(e.id),
    );

    final details = Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        Wrap(
          spacing: 6,
          runSpacing: 4,
          children: [
            if (e.verified) const VerifiedTag(),
            if (e.instantBook) const StatusBadge(label: 'Instant book', tone: AppTone.accent, icon: AppIcons.instant),
          ],
        ),
        const SizedBox(height: AppSpacing.sm),
        Text(e.name, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall),
        const SizedBox(height: 4),
        Row(
          children: [
            const Icon(AppIcons.tracking, size: 13, color: AppColors.slate500),
            const SizedBox(width: 4),
            Flexible(
              child: Text('${e.area} · ${e.distanceKm.toStringAsFixed(1)} km',
                  maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
            ),
            const SizedBox(width: AppSpacing.sm),
            RatingLabel(rating: e.rating, count: e.reviewCount, compact: true),
          ],
        ),
        const SizedBox(height: AppSpacing.sm),
        Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Expanded(
              child: Text.rich(
                TextSpan(children: [
                  TextSpan(text: Fmt.inr(e.shiftRate), style: theme.textTheme.titleMedium?.copyWith(color: AppColors.navy800, fontWeight: FontWeight.w700)),
                  TextSpan(text: ' / 8-hr shift', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
                ]),
              ),
            ),
            Tooltip(
              message: inCompare ? 'Remove from compare' : 'Add to compare',
              child: InkWell(
                borderRadius: AppRadius.chip,
                onTap: toggleCompare,
                child: Padding(
                  padding: const EdgeInsets.all(6),
                  child: Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Icon(inCompare ? AppIcons.check : AppIcons.compare, size: 16, color: inCompare ? AppColors.green700 : AppColors.slate600),
                      const SizedBox(width: 4),
                      Text('Compare', style: theme.textTheme.labelMedium?.copyWith(color: inCompare ? AppColors.green700 : AppColors.slate600)),
                    ],
                  ),
                ),
              ),
            ),
          ],
        ),
      ],
    );

    final image = Stack(
      children: [
        EquipmentImage(categoryId: e.categoryId, heroTag: horizontal ? null : 'eq-${e.id}', aspectRatio: horizontal ? 1 : 16 / 10,
            borderRadius: horizontal ? const BorderRadius.all(Radius.circular(AppRadius.sm)) : const BorderRadius.vertical(top: Radius.circular(AppRadius.md))),
        if (!horizontal) Positioned(right: 8, top: 8, child: favButton),
      ],
    );

    if (horizontal) {
      return AppCard(
        onTap: onTap,
        padding: const EdgeInsets.all(AppSpacing.ms),
        semanticLabel: '${e.name}, ${Fmt.inr(e.shiftRate)} per shift',
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            SizedBox(width: 96, child: image),
            const SizedBox(width: AppSpacing.ms),
            Expanded(child: details),
            favButton,
          ],
        ),
      );
    }

    return AppCard(
      onTap: onTap,
      padding: EdgeInsets.zero,
      semanticLabel: '${e.name}, ${Fmt.inr(e.shiftRate)} per shift',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [image, Padding(padding: const EdgeInsets.all(AppSpacing.ms), child: details)],
      ),
    );
  }
}

class _RoundToggle extends StatelessWidget {
  const _RoundToggle({required this.icon, required this.active, required this.tooltip, required this.onTap});

  final IconData icon;
  final bool active;
  final String tooltip;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    return Tooltip(
      message: tooltip,
      child: Material(
        color: AppColors.white,
        shape: const CircleBorder(),
        elevation: 1,
        child: InkWell(
          customBorder: const CircleBorder(),
          onTap: onTap,
          child: SizedBox.square(
            dimension: 36,
            child: AnimatedSwitcher(
              duration: AppMotion.fast,
              transitionBuilder: (c, a) => ScaleTransition(scale: a, child: c),
              child: active
                  ? const Icon(Icons.favorite_rounded, key: ValueKey('on'), size: 18, color: AppColors.red600)
                  : Icon(icon, key: const ValueKey('off'), size: 18, color: AppColors.slate600),
            ),
          ),
        ),
      ),
    );
  }
}
