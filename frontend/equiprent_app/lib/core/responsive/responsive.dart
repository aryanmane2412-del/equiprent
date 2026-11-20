import 'package:flutter/widgets.dart';

import '../theme/app_tokens.dart';

/// Material 3 window size classes.
/// compact < 600 ≤ medium < 840 ≤ expanded < 1200 ≤ large
enum WindowSize {
  compact,
  medium,
  expanded,
  large;

  static WindowSize fromWidth(double width) {
    if (width < Breakpoints.medium) return WindowSize.compact;
    if (width < Breakpoints.expanded) return WindowSize.medium;
    if (width < Breakpoints.large) return WindowSize.expanded;
    return WindowSize.large;
  }

  bool get isCompact => this == WindowSize.compact;
  bool atLeast(WindowSize other) => index >= other.index;
}

abstract final class Breakpoints {
  static const double medium = 600;
  static const double expanded = 840;
  static const double large = 1200;

  /// Content never stretches wider than this on large monitors.
  static const double maxContentWidth = 1200;
}

extension ResponsiveContext on BuildContext {
  /// Size class of the whole window (use for page-level layout decisions).
  WindowSize get windowSize => WindowSize.fromWidth(MediaQuery.sizeOf(this).width);

  /// Horizontal page gutter for the current window size.
  double get pageGutter => switch (windowSize) {
        WindowSize.compact => AppSpacing.md,
        WindowSize.medium => AppSpacing.lg,
        WindowSize.expanded || WindowSize.large => AppSpacing.xl,
      };
}

/// Picks a value per size class, falling back to the next smaller one.
class Responsive<T> {
  const Responsive({required this.compact, this.medium, this.expanded, this.large});

  final T compact;
  final T? medium;
  final T? expanded;
  final T? large;

  T resolve(WindowSize size) => switch (size) {
        WindowSize.compact => compact,
        WindowSize.medium => medium ?? compact,
        WindowSize.expanded => expanded ?? medium ?? compact,
        WindowSize.large => large ?? expanded ?? medium ?? compact,
      };
}

/// Builds different layouts based on the space available to *this widget*
/// (not the window), so it also works inside panes and dialogs.
class ResponsiveBuilder extends StatelessWidget {
  const ResponsiveBuilder({super.key, required this.compact, this.medium, this.expanded});

  final WidgetBuilder compact;
  final WidgetBuilder? medium;
  final WidgetBuilder? expanded;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final size = WindowSize.fromWidth(constraints.maxWidth);
        final builder = Responsive<WidgetBuilder>(compact: compact, medium: medium, expanded: expanded).resolve(size);
        return builder(context);
      },
    );
  }
}

/// Standard page body: responsive gutters, centred, capped width, scrollable.
class PageBody extends StatelessWidget {
  const PageBody({super.key, required this.children, this.scrollable = true});

  final List<Widget> children;
  final bool scrollable;

  @override
  Widget build(BuildContext context) {
    final gutter = context.pageGutter;
    final content = Center(
      child: ConstrainedBox(
        constraints: const BoxConstraints(maxWidth: Breakpoints.maxContentWidth),
        child: Padding(
          padding: EdgeInsets.fromLTRB(gutter, AppSpacing.lg, gutter, AppSpacing.xl),
          child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: children),
        ),
      ),
    );
    return scrollable ? SingleChildScrollView(child: content) : content;
  }
}

/// Lays children out in equal-width columns: 1 / 2 / 3 / 4 by width.
class ResponsiveGrid extends StatelessWidget {
  const ResponsiveGrid({
    super.key,
    required this.children,
    this.columns = const Responsive(compact: 1, medium: 2, expanded: 3, large: 4),
    this.spacing = AppSpacing.md,
  });

  final List<Widget> children;
  final Responsive<int> columns;
  final double spacing;

  @override
  Widget build(BuildContext context) {
    return LayoutBuilder(
      builder: (context, constraints) {
        final count = columns.resolve(WindowSize.fromWidth(constraints.maxWidth)).clamp(1, 12);
        // floor() avoids sub-pixel overflow wrapping the last item early.
        final itemWidth = ((constraints.maxWidth - spacing * (count - 1)) / count).floorToDouble();
        return Wrap(
          spacing: spacing,
          runSpacing: spacing,
          children: [for (final child in children) SizedBox(width: itemWidth, child: child)],
        );
      },
    );
  }
}

/// Fixed-size gap for Rows and Columns.
class Gap extends StatelessWidget {
  const Gap(this.size, {super.key});

  final double size;

  @override
  Widget build(BuildContext context) => SizedBox(width: size, height: size);
}
