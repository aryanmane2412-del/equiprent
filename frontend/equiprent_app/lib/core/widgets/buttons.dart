import 'package:flutter/material.dart';

import '../theme/app_tokens.dart';

enum AppButtonVariant {
  /// Navy filled: the default primary action.
  primary,

  /// Orange filled: the single most important CTA on a screen.
  accent,

  /// White with border: secondary actions.
  secondary,

  /// Text only: low-emphasis actions.
  ghost,

  /// Red filled: destructive confirmations.
  danger,

  /// Green filled: positive confirmations (approve, complete).
  success,
}

enum AppButtonSize { small, medium, large }

/// The one button used across EQUIPRENT. Handles icon placement, loading
/// state (spinner + disabled), sizes and full-width layout.
class AppButton extends StatelessWidget {
  const AppButton({
    super.key,
    required this.label,
    required this.onPressed,
    this.variant = AppButtonVariant.primary,
    this.size = AppButtonSize.medium,
    this.icon,
    this.trailingIcon,
    this.isLoading = false,
    this.expand = false,
    this.tooltip,
  });

  final String label;
  final VoidCallback? onPressed;
  final AppButtonVariant variant;
  final AppButtonSize size;
  final IconData? icon;
  final IconData? trailingIcon;
  final bool isLoading;
  final bool expand;
  final String? tooltip;

  double get _height => switch (size) {
        AppButtonSize.small => AppSizes.buttonSm,
        AppButtonSize.medium => AppSizes.buttonMd,
        AppButtonSize.large => AppSizes.buttonLg,
      };

  @override
  Widget build(BuildContext context) {
    final enabled = onPressed != null && !isLoading;
    final (Color bg, Color fg, BorderSide? border) = switch (variant) {
      AppButtonVariant.primary => (AppColors.navy700, AppColors.white, null),
      AppButtonVariant.accent => (AppColors.orange600, AppColors.navy950, null),
      AppButtonVariant.danger => (AppColors.red600, AppColors.white, null),
      AppButtonVariant.success => (AppColors.green700, AppColors.white, null),
      AppButtonVariant.secondary => (AppColors.white, AppColors.navy700, const BorderSide(color: AppColors.slate300)),
      AppButtonVariant.ghost => (Colors.transparent, AppColors.navy700, null),
    };

    final iconSize = size == AppButtonSize.small ? 16.0 : 18.0;
    final textStyle = (size == AppButtonSize.small
            ? Theme.of(context).textTheme.labelMedium
            : Theme.of(context).textTheme.labelLarge)
        ?.copyWith(fontSize: size == AppButtonSize.large ? 15 : null);

    final child = AnimatedSwitcher(
      duration: AppMotion.fast,
      child: isLoading
          ? SizedBox.square(
              key: const ValueKey('loading'),
              dimension: iconSize,
              child: CircularProgressIndicator(strokeWidth: 2, color: enabled ? fg : AppColors.slate400),
            )
          : Row(
              key: const ValueKey('content'),
              mainAxisSize: MainAxisSize.min,
              children: [
                if (icon != null) ...[Icon(icon, size: iconSize), const SizedBox(width: AppSpacing.sm)],
                Flexible(child: Text(label, overflow: TextOverflow.ellipsis, maxLines: 1)),
                if (trailingIcon != null) ...[const SizedBox(width: AppSpacing.sm), Icon(trailingIcon, size: iconSize)],
              ],
            ),
    );

    final isFilled = variant != AppButtonVariant.secondary && variant != AppButtonVariant.ghost;
    final style = ButtonStyle(
      minimumSize: WidgetStatePropertyAll(Size(size == AppButtonSize.small ? 40 : 64, _height)),
      fixedSize: expand ? WidgetStatePropertyAll(Size.fromHeight(_height)) : null,
      padding: WidgetStatePropertyAll(
        EdgeInsets.symmetric(horizontal: size == AppButtonSize.small ? AppSpacing.ms : AppSpacing.lg - 4),
      ),
      textStyle: WidgetStatePropertyAll(textStyle),
      shape: const WidgetStatePropertyAll(RoundedRectangleBorder(borderRadius: AppRadius.control)),
      backgroundColor: WidgetStateProperty.resolveWith((s) {
        if (s.contains(WidgetState.disabled)) return isFilled ? AppColors.slate200 : bg;
        if (s.contains(WidgetState.hovered) || s.contains(WidgetState.pressed)) {
          return isFilled ? Color.lerp(bg, Colors.black, 0.12) : AppColors.navy50;
        }
        return bg;
      }),
      foregroundColor: WidgetStateProperty.resolveWith((s) => s.contains(WidgetState.disabled) ? AppColors.slate400 : fg),
      overlayColor: WidgetStatePropertyAll(fg.withValues(alpha: 0.08)),
      side: border == null ? null : WidgetStatePropertyAll(border),
      elevation: const WidgetStatePropertyAll(0),
    );

    Widget button = TextButton(onPressed: enabled ? onPressed : null, style: style, child: child);
    if (expand) button = SizedBox(width: double.infinity, child: button);
    if (tooltip != null) button = Tooltip(message: tooltip!, child: button);
    return Semantics(button: true, enabled: enabled, label: isLoading ? '$label, loading' : null, child: button);
  }
}

/// Square icon button with an outlined or filled container, and a required
/// tooltip for accessibility.
class AppIconButton extends StatelessWidget {
  const AppIconButton({
    super.key,
    required this.icon,
    required this.tooltip,
    required this.onPressed,
    this.filled = false,
    this.badgeCount,
    this.color,
  });

  final IconData icon;
  final String tooltip;
  final VoidCallback? onPressed;
  final bool filled;
  final int? badgeCount;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    Widget iconWidget = Icon(icon, size: 20, color: color ?? (filled ? AppColors.white : AppColors.slate700));
    if (badgeCount != null && badgeCount! > 0) {
      iconWidget = Badge.count(count: badgeCount!, child: iconWidget);
    }
    return IconButton(
      tooltip: tooltip,
      onPressed: onPressed,
      icon: iconWidget,
      style: IconButton.styleFrom(
        backgroundColor: filled ? AppColors.navy700 : null,
        minimumSize: const Size(44, 44),
        shape: const RoundedRectangleBorder(borderRadius: AppRadius.control),
      ),
    );
  }
}
