import 'package:flutter/material.dart';

import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import '../theme/app_tone.dart';
import 'buttons.dart';

/// True when the platform asks for reduced motion (also set in tests).
bool reduceMotion(BuildContext context) => MediaQuery.maybeDisableAnimationsOf(context) ?? false;

class LoadingView extends StatelessWidget {
  const LoadingView({super.key, this.message = 'Loading…'});

  final String message;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          // With reduced motion the spinner becomes a static arc.
          SizedBox.square(dimension: 32, child: CircularProgressIndicator(strokeWidth: 3, value: reduceMotion(context) ? 0.3 : null)),
          const SizedBox(height: AppSpacing.md),
          Text(message, style: Theme.of(context).textTheme.bodyMedium?.copyWith(color: AppColors.slate500)),
        ],
      ),
    );
  }
}

/// Placeholder block with a gentle shimmer while content loads.
class Skeleton extends StatefulWidget {
  const Skeleton({super.key, this.width, this.height = 14, this.radius = AppRadius.xs});

  final double? width;
  final double height;
  final double radius;

  @override
  State<Skeleton> createState() => _SkeletonState();
}

class _SkeletonState extends State<Skeleton> with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(vsync: this, duration: const Duration(milliseconds: 1200));

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (reduceMotion(context)) {
      _c.stop();
    } else if (!_c.isAnimating) {
      _c.repeat();
    }
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return AnimatedBuilder(
      animation: _c,
      builder: (context, _) {
        final t = _c.value;
        return Container(
          width: widget.width,
          height: widget.height,
          decoration: BoxDecoration(
            borderRadius: BorderRadius.circular(widget.radius),
            gradient: LinearGradient(
              begin: Alignment(-1.5 + 3 * t, 0),
              end: Alignment(-0.5 + 3 * t, 0),
              colors: const [AppColors.slate100, AppColors.slate200, AppColors.slate100],
            ),
          ),
        );
      },
    );
  }
}

/// Skeleton shaped like an equipment card, for list loading states.
class SkeletonCard extends StatelessWidget {
  const SkeletonCard({super.key});

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(AppSpacing.md),
      decoration: BoxDecoration(color: AppColors.white, borderRadius: AppRadius.card, border: Border.all(color: AppColors.border)),
      child: const Row(
        children: [
          Skeleton(width: 96, height: 72, radius: AppRadius.sm),
          SizedBox(width: AppSpacing.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Skeleton(width: 180),
                SizedBox(height: AppSpacing.sm),
                Skeleton(width: 120, height: 12),
                SizedBox(height: AppSpacing.md),
                Skeleton(width: 80, height: 18),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// Illustration-led state used for empty, error and success screens.
class StateMessage extends StatelessWidget {
  const StateMessage({
    super.key,
    required this.icon,
    required this.title,
    this.message,
    this.tone = AppTone.brand,
    this.primaryAction,
    this.secondaryAction,
    this.compact = false,
  });

  final IconData icon;
  final String title;
  final String? message;
  final AppTone tone;
  final Widget? primaryAction;
  final Widget? secondaryAction;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final size = compact ? 56.0 : 80.0;
    return Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: 440),
        child: Padding(
          padding: EdgeInsets.all(compact ? AppSpacing.md : AppSpacing.xl),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                width: size,
                height: size,
                decoration: BoxDecoration(
                  color: tone.background,
                  shape: BoxShape.circle,
                  border: Border.all(color: tone.border, width: 6, strokeAlign: BorderSide.strokeAlignOutside),
                ),
                child: Icon(icon, size: size * 0.42, color: tone.foreground),
              ),
              SizedBox(height: compact ? AppSpacing.md : AppSpacing.lg),
              Text(title, style: compact ? theme.textTheme.titleMedium : theme.textTheme.headlineSmall, textAlign: TextAlign.center),
              if (message != null) ...[
                const SizedBox(height: AppSpacing.sm),
                Text(message!, textAlign: TextAlign.center, style: theme.textTheme.bodyMedium?.copyWith(color: AppColors.slate600)),
              ],
              if (primaryAction != null || secondaryAction != null) ...[
                SizedBox(height: compact ? AppSpacing.md : AppSpacing.lg),
                Wrap(
                  alignment: WrapAlignment.center,
                  spacing: AppSpacing.sm,
                  runSpacing: AppSpacing.sm,
                  children: [?secondaryAction, ?primaryAction],
                ),
              ],
            ],
          ),
        ),
      ),
    );
  }
}

class EmptyState extends StatelessWidget {
  const EmptyState({super.key, required this.icon, required this.title, this.message, this.action, this.compact = false});

  final IconData icon;
  final String title;
  final String? message;
  final Widget? action;
  final bool compact;

  @override
  Widget build(BuildContext context) =>
      StateMessage(icon: icon, title: title, message: message, tone: AppTone.brand, primaryAction: action, compact: compact);
}

class ErrorState extends StatelessWidget {
  const ErrorState({
    super.key,
    this.title = 'Something went wrong',
    this.message = 'We couldn’t load this. Check your connection and try again.',
    this.onRetry,
    this.offline = false,
    this.reference,
  });

  final String title;
  final String message;
  final VoidCallback? onRetry;
  final bool offline;
  final String? reference;

  @override
  Widget build(BuildContext context) {
    return StateMessage(
      icon: offline ? AppIcons.offline : AppIcons.error,
      tone: AppTone.danger,
      title: offline ? 'You’re offline' : title,
      message: [
        offline ? 'Changes are saved on this device and will sync when you’re back online.' : message,
        if (reference != null) 'Reference: $reference',
      ].join('\n'),
      primaryAction: onRetry == null ? null : AppButton(label: 'Try again', icon: AppIcons.refresh, variant: AppButtonVariant.secondary, onPressed: onRetry),
    );
  }
}

class SuccessState extends StatelessWidget {
  const SuccessState({super.key, required this.title, this.message, this.primaryAction, this.secondaryAction});

  final String title;
  final String? message;
  final Widget? primaryAction;
  final Widget? secondaryAction;

  @override
  Widget build(BuildContext context) {
    return TweenAnimationBuilder<double>(
      tween: Tween(begin: reduceMotion(context) ? 1 : 0.85, end: 1),
      duration: AppMotion.slow,
      curve: Curves.easeOutBack,
      builder: (context, scale, child) => Transform.scale(scale: scale, child: child),
      child: StateMessage(
        icon: AppIcons.checkCircle,
        tone: AppTone.success,
        title: title,
        message: message,
        primaryAction: primaryAction,
        secondaryAction: secondaryAction,
      ),
    );
  }
}

/// Inline message strip inside a page or card.
class InlineBanner extends StatelessWidget {
  const InlineBanner({super.key, required this.message, this.title, this.tone = AppTone.info, this.icon, this.action});

  final String message;
  final String? title;
  final AppTone tone;
  final IconData? icon;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final glyph = icon ??
        switch (tone) {
          AppTone.success => AppIcons.checkCircle,
          AppTone.warning => AppIcons.warning,
          AppTone.danger => AppIcons.error,
          _ => AppIcons.info,
        };
    return Container(
      padding: const EdgeInsets.all(AppSpacing.ms),
      decoration: BoxDecoration(color: tone.background, borderRadius: AppRadius.control, border: Border.all(color: tone.border)),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(glyph, size: 18, color: tone.foreground),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (title != null) Text(title!, style: theme.textTheme.titleSmall?.copyWith(color: tone.foreground)),
                Text(message, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate700)),
              ],
            ),
          ),
          if (action != null) ...[const SizedBox(width: AppSpacing.sm), action!],
        ],
      ),
    );
  }
}

/// Toasts (floating snack bars) with a semantic icon.
abstract final class AppToast {
  static void show(BuildContext context, String message, {AppTone tone = AppTone.neutral, String? actionLabel, VoidCallback? onAction}) {
    final icon = switch (tone) {
      AppTone.success => AppIcons.checkCircle,
      AppTone.danger => AppIcons.error,
      AppTone.warning => AppIcons.warning,
      _ => AppIcons.info,
    };
    final color = switch (tone) {
      AppTone.success => const Color(0xFF4ADE80),
      AppTone.danger => const Color(0xFFF87171),
      AppTone.warning => const Color(0xFFFBBF24),
      _ => AppColors.navy200,
    };
    final messenger = ScaffoldMessenger.of(context);
    messenger
      ..hideCurrentSnackBar()
      ..showSnackBar(
        SnackBar(
          content: Row(
            children: [
              Icon(icon, size: 18, color: color),
              const SizedBox(width: AppSpacing.sm),
              Expanded(child: Text(message)),
            ],
          ),
          action: actionLabel == null ? null : SnackBarAction(label: actionLabel, onPressed: onAction ?? () {}),
          duration: const Duration(seconds: 3),
        ),
      );
  }

  static void success(BuildContext context, String message) => show(context, message, tone: AppTone.success);
  static void error(BuildContext context, String message) => show(context, message, tone: AppTone.danger);
}

/// Standard "not built yet" feedback so no button is ever silently dead.
void showComingSoon(BuildContext context, String feature, {String phase = 'Phase 1'}) {
  AppToast.show(context, '$feature isn’t available yet — planned for $phase.', tone: AppTone.info);
}

/// A section that could not load: says so and offers a retry, instead of silently showing it empty.
class LoadFailedBanner extends StatelessWidget {
  const LoadFailedBanner({super.key, required this.what, required this.onRetry});

  final String what;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) => InlineBanner(
    tone: AppTone.warning,
    message: 'Could not load $what.',
    action: TextButton(onPressed: onRetry, child: const Text('Retry')),
  );
}
