import 'package:flutter/material.dart';

import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';

/// Wordmark until the final logo asset exists: navy tile with an orange
/// accent corner, "EQUIPRENT" in heavy Inter, optional tagline.
class BrandLogo extends StatelessWidget {
  const BrandLogo({super.key, this.showTagline = true, this.compact = false, this.onDark = false});

  static const name = 'EQUIPRENT';
  static const tagline = 'Rent Smart. Build Better.';

  final bool showTagline;
  final bool compact;
  final bool onDark;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final mark = compact ? 32.0 : 40.0;
    return Semantics(
      label: '$name. $tagline',
      excludeSemantics: true,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          SizedBox.square(
            dimension: mark,
            child: Stack(
              children: [
                Container(
                  decoration: BoxDecoration(
                    color: onDark ? AppColors.white : AppColors.navy800,
                    borderRadius: const BorderRadius.all(Radius.circular(AppRadius.sm + 2)),
                  ),
                  child: Center(child: Icon(AppIcons.backhoe, size: mark * 0.55, color: onDark ? AppColors.navy800 : AppColors.white)),
                ),
                Positioned(
                  right: 0,
                  top: 0,
                  child: Container(
                    width: mark * 0.3,
                    height: mark * 0.3,
                    decoration: const BoxDecoration(
                      color: AppColors.orange500,
                      borderRadius: BorderRadius.only(topRight: Radius.circular(AppRadius.sm + 2), bottomLeft: Radius.circular(6)),
                    ),
                  ),
                ),
              ],
            ),
          ),
          const SizedBox(width: AppSpacing.sm + 2),
          Flexible(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(
                  name,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: (compact ? theme.textTheme.titleMedium : theme.textTheme.headlineSmall)?.copyWith(
                    fontWeight: FontWeight.w800,
                    letterSpacing: 1.2,
                    color: onDark ? AppColors.white : AppColors.navy900,
                  ),
                ),
                if (showTagline)
                  Text(
                    tagline,
                    style: theme.textTheme.labelSmall?.copyWith(
                      letterSpacing: 0.2,
                      color: onDark ? AppColors.navy200 : AppColors.orange700,
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
