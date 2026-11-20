import 'package:flutter/material.dart';

import '../responsive/responsive.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import '../theme/app_tone.dart';
import 'buttons.dart';

/// Confirmation dialog. Returns true when confirmed.
Future<bool> showConfirmDialog(
  BuildContext context, {
  required String title,
  required String message,
  String confirmLabel = 'Confirm',
  String cancelLabel = 'Cancel',
  AppTone tone = AppTone.brand,
  IconData? icon,
}) async {
  final result = await showDialog<bool>(
    context: context,
    builder: (context) => AppDialog(
      title: title,
      icon: icon ?? (tone == AppTone.danger ? AppIcons.warning : AppIcons.info),
      tone: tone,
      content: Text(message),
      actions: [
        AppButton(
          label: cancelLabel,
          variant: AppButtonVariant.secondary,
          onPressed: () => Navigator.of(context).pop(false),
        ),
        AppButton(
          label: confirmLabel,
          variant: switch (tone) {
            AppTone.danger => AppButtonVariant.danger,
            AppTone.success => AppButtonVariant.success,
            _ => AppButtonVariant.primary,
          },
          onPressed: () => Navigator.of(context).pop(true),
        ),
      ],
    ),
  );
  return result ?? false;
}

/// Branded dialog shell: icon badge, title, content, right-aligned actions.
class AppDialog extends StatelessWidget {
  const AppDialog({
    super.key,
    required this.title,
    required this.content,
    required this.actions,
    this.icon,
    this.tone = AppTone.brand,
  });

  final String title;
  final Widget content;
  final List<Widget> actions;
  final IconData? icon;
  final AppTone tone;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final availableHeight =
        (MediaQuery.sizeOf(context).height -
                MediaQuery.viewInsetsOf(context).bottom -
                32)
            .clamp(180.0, 720.0)
            .toDouble();
    return Dialog(
      insetPadding: const EdgeInsets.all(AppSpacing.md),
      child: ConstrainedBox(
        constraints: BoxConstraints(maxWidth: 440, maxHeight: availableHeight),
        child: Padding(
          padding: const EdgeInsets.all(AppSpacing.lg),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              if (icon != null) ...[
                Container(
                  width: 44,
                  height: 44,
                  decoration: BoxDecoration(
                    color: tone.background,
                    shape: BoxShape.circle,
                  ),
                  child: Icon(icon, color: tone.foreground, size: 22),
                ),
                const SizedBox(height: AppSpacing.md),
              ],
              Text(title, style: theme.textTheme.titleLarge),
              const SizedBox(height: AppSpacing.sm),
              Flexible(
                child: SingleChildScrollView(
                  keyboardDismissBehavior:
                      ScrollViewKeyboardDismissBehavior.onDrag,
                  child: DefaultTextStyle.merge(
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: AppColors.slate600,
                    ),
                    child: content,
                  ),
                ),
              ),
              const SizedBox(height: AppSpacing.lg),
              Wrap(
                alignment: WrapAlignment.end,
                spacing: AppSpacing.sm,
                runSpacing: AppSpacing.sm,
                children: actions,
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// Bottom sheet on phones; centred dialog on wide screens.
Future<T?> showAppSheet<T>(
  BuildContext context, {
  required String title,
  required WidgetBuilder builder,
  String? subtitle,
}) {
  final wide = context.windowSize.atLeast(WindowSize.expanded);
  Widget body(BuildContext ctx) =>
      _SheetBody(title: title, subtitle: subtitle, child: builder(ctx));
  if (wide) {
    return showDialog<T>(
      context: context,
      builder: (ctx) => Dialog(
        insetPadding: const EdgeInsets.all(AppSpacing.lg),
        child: ConstrainedBox(
          constraints: const BoxConstraints(maxWidth: 560, maxHeight: 720),
          child: body(ctx),
        ),
      ),
    );
  }
  return showModalBottomSheet<T>(
    context: context,
    isScrollControlled: true,
    useSafeArea: true,
    builder: (ctx) => ConstrainedBox(
      constraints: BoxConstraints(
        maxHeight: MediaQuery.sizeOf(ctx).height * 0.9,
      ),
      child: Padding(
        padding: EdgeInsets.only(bottom: MediaQuery.viewInsetsOf(ctx).bottom),
        child: body(ctx),
      ),
    ),
  );
}

class _SheetBody extends StatelessWidget {
  const _SheetBody({required this.title, required this.child, this.subtitle});

  final String title;
  final String? subtitle;
  final Widget child;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(
            AppSpacing.lg,
            AppSpacing.md,
            AppSpacing.sm,
            AppSpacing.sm,
          ),
          child: Row(
            children: [
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, style: theme.textTheme.titleLarge),
                    if (subtitle != null)
                      Text(
                        subtitle!,
                        style: theme.textTheme.bodySmall?.copyWith(
                          color: AppColors.slate500,
                        ),
                      ),
                  ],
                ),
              ),
              IconButton(
                tooltip: 'Close',
                onPressed: () => Navigator.of(context).maybePop(),
                icon: const Icon(AppIcons.close, size: 20),
              ),
            ],
          ),
        ),
        const Divider(),
        Flexible(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(AppSpacing.lg),
            child: child,
          ),
        ),
      ],
    );
  }
}

/// Action option for [showActionSheet].
class SheetAction<T> {
  const SheetAction({
    required this.value,
    required this.label,
    required this.icon,
    this.destructive = false,
    this.subtitle,
  });

  final T value;
  final String label;
  final IconData icon;
  final bool destructive;
  final String? subtitle;
}

/// List of actions in a bottom sheet (phones) or dialog (desktop).
Future<T?> showActionSheet<T>(
  BuildContext context, {
  required String title,
  required List<SheetAction<T>> actions,
}) {
  return showAppSheet<T>(
    context,
    title: title,
    builder: (ctx) => Column(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (final a in actions)
          ListTile(
            contentPadding: EdgeInsets.zero,
            leading: Icon(
              a.icon,
              color: a.destructive ? AppColors.red600 : AppColors.navy700,
            ),
            title: Text(
              a.label,
              style: TextStyle(color: a.destructive ? AppColors.red600 : null),
            ),
            subtitle: a.subtitle == null ? null : Text(a.subtitle!),
            onTap: () => Navigator.of(ctx).pop(a.value),
          ),
      ],
    ),
  );
}
