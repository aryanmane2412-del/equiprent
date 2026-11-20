import 'package:flutter/material.dart';

import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import '../theme/app_tone.dart';

/// White bordered surface. Tappable cards lift slightly on hover/press.
class AppCard extends StatefulWidget {
  const AppCard({
    super.key,
    required this.child,
    this.onTap,
    this.padding = const EdgeInsets.all(AppSpacing.md),
    this.semanticLabel,
    this.color,
    this.borderColor,
  });

  final Widget child;
  final VoidCallback? onTap;
  final EdgeInsetsGeometry padding;
  final String? semanticLabel;
  final Color? color;
  final Color? borderColor;

  @override
  State<AppCard> createState() => _AppCardState();
}

class _AppCardState extends State<AppCard> {
  bool _hover = false;

  @override
  Widget build(BuildContext context) {
    final interactive = widget.onTap != null;
    return Semantics(
      label: widget.semanticLabel,
      button: interactive,
      child: AnimatedContainer(
        duration: AppMotion.fast,
        curve: AppMotion.emphasized,
        decoration: BoxDecoration(
          color: widget.color ?? AppColors.white,
          borderRadius: AppRadius.card,
          border: Border.all(color: _hover ? AppColors.slate300 : (widget.borderColor ?? AppColors.border)),
          boxShadow: _hover ? AppShadows.md : null,
        ),
        child: Material(
          type: MaterialType.transparency,
          child: InkWell(
            borderRadius: AppRadius.card,
            onTap: widget.onTap,
            onHover: interactive ? (h) => setState(() => _hover = h) : null,
            child: Padding(padding: widget.padding, child: widget.child),
          ),
        ),
      ),
    );
  }
}

/// Card with a header row (title, subtitle, trailing action) and body.
class SectionCard extends StatelessWidget {
  const SectionCard({
    super.key,
    required this.title,
    required this.child,
    this.subtitle,
    this.icon,
    this.trailing,
    this.padding = const EdgeInsets.all(AppSpacing.md),
  });

  final String title;
  final String? subtitle;
  final IconData? icon;
  final Widget? trailing;
  final Widget child;
  final EdgeInsetsGeometry padding;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      padding: padding,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Row(
            children: [
              if (icon != null) ...[Icon(icon, size: 18, color: AppColors.navy700), const SizedBox(width: AppSpacing.sm)],
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: theme.textTheme.titleMedium),
                    if (subtitle != null)
                      Text(subtitle!, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
                  ],
                ),
              ),
              ?trailing,
            ],
          ),
          const SizedBox(height: AppSpacing.md),
          child,
        ],
      ),
    );
  }
}

/// KPI tile: icon, label, value and optional trend.
class StatCard extends StatelessWidget {
  const StatCard({
    super.key,
    required this.label,
    required this.value,
    required this.icon,
    this.tone = AppTone.brand,
    this.trend,
    this.trendUp = true,
    this.caption,
    this.onTap,
  });

  final String label;
  final String value;
  final IconData icon;
  final AppTone tone;
  final String? trend;
  final bool trendUp;
  final String? caption;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      onTap: onTap,
      semanticLabel: '$label: $value${trend == null ? '' : ', $trend'}',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              IconTile(icon: icon, tone: tone),
              const SizedBox(width: AppSpacing.sm),
              if (trend != null)
                Expanded(
                  child: Row(
                    mainAxisAlignment: MainAxisAlignment.end,
                    children: [
                      Icon(trendUp ? AppIcons.trendUp : AppIcons.trendDown, size: 14, color: trendUp ? AppColors.green700 : AppColors.red600),
                      const SizedBox(width: 4),
                      Flexible(
                        child: Text(
                          trend!,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: theme.textTheme.labelMedium?.copyWith(color: trendUp ? AppColors.green700 : AppColors.red600),
                        ),
                      ),
                    ],
                  ),
                ),
            ],
          ),
          const SizedBox(height: AppSpacing.ms),
          Text(label, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500), maxLines: 1, overflow: TextOverflow.ellipsis),
          const SizedBox(height: 2),
          FittedBox(
            fit: BoxFit.scaleDown,
            alignment: Alignment.centerLeft,
            child: Text(value, style: theme.textTheme.headlineMedium?.copyWith(fontFeatures: const [FontFeature.tabularFigures()])),
          ),
          if (caption != null) ...[
            const SizedBox(height: 2),
            Text(caption!, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500), maxLines: 1, overflow: TextOverflow.ellipsis),
          ],
        ],
      ),
    );
  }
}

/// Rounded square with a tinted background holding an icon.
class IconTile extends StatelessWidget {
  const IconTile({super.key, required this.icon, this.tone = AppTone.brand, this.size = 40});

  final IconData icon;
  final AppTone tone;
  final double size;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      decoration: BoxDecoration(color: tone.background, borderRadius: const BorderRadius.all(Radius.circular(AppRadius.sm + 2))),
      child: Icon(icon, size: size * 0.5, color: tone.foreground),
    );
  }
}

/// Label/value row used in summaries, price breakdowns and detail sheets.
class InfoRow extends StatelessWidget {
  const InfoRow({super.key, required this.label, required this.value, this.icon, this.emphasize = false, this.valueColor});

  final String label;
  final String value;
  final IconData? icon;
  final bool emphasize;
  final Color? valueColor;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final valueStyle = (emphasize ? theme.textTheme.titleMedium : theme.textTheme.bodyMedium)
        ?.copyWith(color: valueColor, fontWeight: emphasize ? FontWeight.w700 : FontWeight.w500, fontFeatures: const [FontFeature.tabularFigures()]);
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpacing.xs + 2),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (icon != null) ...[Icon(icon, size: 16, color: AppColors.slate500), const SizedBox(width: AppSpacing.sm)],
          // Label 60% / value 40%, value flush right so amounts line up.
          Expanded(
            flex: 3,
            child: Text(label, style: (emphasize ? theme.textTheme.titleSmall : theme.textTheme.bodyMedium)?.copyWith(color: emphasize ? null : AppColors.slate600)),
          ),
          const SizedBox(width: AppSpacing.md),
          Expanded(flex: 2, child: Text(value, textAlign: TextAlign.right, style: valueStyle)),
        ],
      ),
    );
  }
}

/// Generic list row inside cards: leading, title/subtitle, trailing.
class ListRow extends StatelessWidget {
  const ListRow({super.key, required this.title, this.subtitle, this.leading, this.trailing, this.onTap, this.dense = false});

  final String title;
  final String? subtitle;
  final Widget? leading;
  final Widget? trailing;
  final VoidCallback? onTap;
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return InkWell(
      onTap: onTap,
      borderRadius: AppRadius.control,
      child: Padding(
        padding: EdgeInsets.symmetric(vertical: dense ? AppSpacing.sm : AppSpacing.ms, horizontal: AppSpacing.xs),
        child: Row(
          children: [
            if (leading != null) ...[leading!, const SizedBox(width: AppSpacing.ms)],
            Expanded(
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(title, style: theme.textTheme.titleSmall, maxLines: 1, overflow: TextOverflow.ellipsis),
                  if (subtitle != null)
                    Text(subtitle!, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500), maxLines: 2, overflow: TextOverflow.ellipsis),
                ],
              ),
            ),
            if (trailing != null) ...[const SizedBox(width: AppSpacing.sm), trailing!],
            if (onTap != null && trailing == null) const Icon(AppIcons.chevronRight, size: 18, color: AppColors.slate400),
          ],
        ),
      ),
    );
  }
}
