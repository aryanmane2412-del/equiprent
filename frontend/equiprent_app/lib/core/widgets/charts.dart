import 'dart:math' as math;

import 'package:flutter/material.dart';

import '../mock/models.dart';
import '../theme/app_tokens.dart';
import '../theme/app_tone.dart';
import 'feedback.dart';

/// Lightweight, dependency-free charts. All are single-series: one navy hue,
/// the card title names the series (no legend), the current/peak value gets
/// the only direct label, and every mark has a tooltip + semantics label.

/// Vertical bars, e.g. earnings per month. The last bar (current period) is
/// emphasised and direct-labelled.
class BarChart extends StatelessWidget {
  const BarChart({super.key, required this.points, required this.format, this.height = 200, this.axisFormat});

  final List<SeriesPoint> points;
  final String Function(double) format;
  final String Function(double)? axisFormat;
  final double height;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final maxV = points.map((p) => p.value).fold<double>(0, math.max);
    final top = _niceCeil(maxV);
    final axis = axisFormat ?? format;
    final label = theme.textTheme.labelSmall?.copyWith(color: AppColors.slate500, letterSpacing: 0);
    final animate = !reduceMotion(context);

    return SizedBox(
      height: height,
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          // Y axis: 0, half, top.
          SizedBox(
            width: 44,
            child: Padding(
              padding: const EdgeInsets.only(bottom: 22),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.end,
                mainAxisAlignment: MainAxisAlignment.spaceBetween,
                children: [Text(axis(top), style: label), Text(axis(top / 2), style: label), Text(axis(0), style: label)],
              ),
            ),
          ),
          const SizedBox(width: AppSpacing.sm),
          Expanded(
            child: Column(
              children: [
                Expanded(
                  child: Stack(
                    children: [
                      // Recessive grid.
                      const Positioned.fill(child: _Grid(lines: 3)),
                      Row(
                        crossAxisAlignment: CrossAxisAlignment.end,
                        children: [
                          for (var i = 0; i < points.length; i++)
                            Expanded(
                              child: Padding(
                                padding: const EdgeInsets.symmetric(horizontal: 4),
                                child: _Bar(
                                  fraction: top == 0 ? 0 : points[i].value / top,
                                  emphasis: i == points.length - 1,
                                  tooltip: '${points[i].label}: ${format(points[i].value)}',
                                  directLabel: i == points.length - 1 ? format(points[i].value) : null,
                                  animate: animate,
                                ),
                              ),
                            ),
                        ],
                      ),
                    ],
                  ),
                ),
                const SizedBox(height: 6),
                SizedBox(
                  height: 16,
                  child: Row(
                    children: [
                      for (final p in points) Expanded(child: Text(p.label, textAlign: TextAlign.center, style: label)),
                    ],
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

class _Bar extends StatelessWidget {
  const _Bar({required this.fraction, required this.emphasis, required this.tooltip, required this.animate, this.directLabel});

  final double fraction;
  final bool emphasis;
  final String tooltip;
  final String? directLabel;
  final bool animate;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: tooltip,
      excludeSemantics: true,
      child: Tooltip(
        message: tooltip,
        // The hit target is the full column, not just the bar.
        child: LayoutBuilder(
          builder: (context, c) {
            return TweenAnimationBuilder<double>(
              tween: Tween(begin: animate ? 0 : fraction, end: fraction),
              duration: AppMotion.slow,
              curve: AppMotion.emphasized,
              builder: (context, f, _) {
                final h = (c.maxHeight - (directLabel == null ? 0 : 18)) * f.clamp(0, 1);
                return Column(
                  mainAxisAlignment: MainAxisAlignment.end,
                  children: [
                    if (directLabel != null)
                      SizedBox(
                        height: 18,
                        child: FittedBox(
                          child: Text(directLabel!, style: Theme.of(context).textTheme.labelSmall?.copyWith(color: AppColors.slate800, letterSpacing: 0)),
                        ),
                      ),
                    Container(
                      height: math.max(h, 2),
                      constraints: const BoxConstraints(maxWidth: 40),
                      decoration: BoxDecoration(
                        color: emphasis ? AppColors.chartMarkEmphasis : AppColors.chartMark,
                        borderRadius: const BorderRadius.vertical(top: Radius.circular(4)),
                      ),
                    ),
                  ],
                );
              },
            );
          },
        ),
      ),
    );
  }
}

class _Grid extends StatelessWidget {
  const _Grid({required this.lines});

  final int lines;

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisAlignment: MainAxisAlignment.spaceBetween,
      children: [for (var i = 0; i < lines; i++) Container(height: 1, color: i == lines - 1 ? AppColors.slate300 : AppColors.chartGrid)],
    );
  }
}

double _niceCeil(double v) {
  if (v <= 0) return 1;
  final exp = math.pow(10, (math.log(v) / math.ln10).floor()).toDouble();
  final f = v / exp;
  final nice = f <= 1 ? 1 : f <= 2 ? 2 : f <= 2.5 ? 2.5 : f <= 5 ? 5 : 10;
  return nice * exp;
}

/// Ranked horizontal bars with label left and value right (magnitude
/// comparison across categories).
class HorizontalBars extends StatelessWidget {
  const HorizontalBars({super.key, required this.points, required this.format});

  final List<SeriesPoint> points;
  final String Function(double) format;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final maxV = points.map((p) => p.value).fold<double>(0, math.max);
    final sorted = [...points]..sort((a, b) => b.value.compareTo(a.value));
    return Column(
      children: [
        for (final p in sorted)
          Padding(
            padding: const EdgeInsets.symmetric(vertical: 5),
            child: Tooltip(
              message: '${p.label}: ${format(p.value)}',
              child: Row(
                children: [
                  SizedBox(width: 84, child: Text(p.label, style: theme.textTheme.bodySmall, overflow: TextOverflow.ellipsis)),
                  Expanded(
                    child: LayoutBuilder(
                      builder: (context, c) => Align(
                        alignment: Alignment.centerLeft,
                        child: Container(
                          height: 12,
                          width: maxV == 0 ? 0 : math.max(4, c.maxWidth * p.value / maxV),
                          decoration: const BoxDecoration(
                            color: AppColors.chartMark,
                            borderRadius: BorderRadius.horizontal(right: Radius.circular(4)),
                          ),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(width: AppSpacing.sm),
                  SizedBox(
                    width: 48,
                    child: Text(format(p.value), textAlign: TextAlign.right, style: theme.textTheme.labelMedium?.copyWith(color: AppColors.slate700)),
                  ),
                ],
              ),
            ),
          ),
      ],
    );
  }
}

/// 2px line with point tooltips, e.g. weekly utilisation.
class LineTrend extends StatelessWidget {
  const LineTrend({super.key, required this.points, required this.format, this.height = 160, this.maxValue});

  final List<SeriesPoint> points;
  final String Function(double) format;
  final double height;
  final double? maxValue;

  @override
  Widget build(BuildContext context) {
    final label = Theme.of(context).textTheme.labelSmall?.copyWith(color: AppColors.slate500, letterSpacing: 0);
    final top = maxValue ?? _niceCeil(points.map((p) => p.value).fold<double>(0, math.max));
    return SizedBox(
      height: height,
      child: Column(
        children: [
          Expanded(
            child: LayoutBuilder(
              builder: (context, c) {
                final w = c.maxWidth;
                final h = c.maxHeight;
                Offset at(int i) => Offset(
                      points.length == 1 ? w / 2 : w * i / (points.length - 1),
                      h - (h - 8) * (points[i].value / top).clamp(0, 1) - 4,
                    );
                return Stack(
                  clipBehavior: Clip.none,
                  children: [
                    const Positioned.fill(child: _Grid(lines: 3)),
                    Positioned.fill(child: CustomPaint(painter: _LinePainter([for (var i = 0; i < points.length; i++) at(i)]))),
                    for (var i = 0; i < points.length; i++)
                      Positioned(
                        left: at(i).dx - 16,
                        top: at(i).dy - 16,
                        child: Tooltip(
                          message: '${points[i].label}: ${format(points[i].value)}',
                          child: SizedBox(
                            width: 32,
                            height: 32,
                            child: Center(
                              child: Container(
                                width: 8,
                                height: 8,
                                decoration: BoxDecoration(
                                  color: AppColors.chartMarkEmphasis,
                                  shape: BoxShape.circle,
                                  border: Border.all(color: AppColors.white, width: 2),
                                ),
                              ),
                            ),
                          ),
                        ),
                      ),
                  ],
                );
              },
            ),
          ),
          const SizedBox(height: 6),
          Row(
            mainAxisAlignment: MainAxisAlignment.spaceBetween,
            children: [for (final p in points) Text(p.label, style: label)],
          ),
        ],
      ),
    );
  }
}

class _LinePainter extends CustomPainter {
  _LinePainter(this.pts);

  final List<Offset> pts;

  @override
  void paint(Canvas canvas, Size size) {
    if (pts.length < 2) return;
    final path = Path()..moveTo(pts.first.dx, pts.first.dy);
    for (final p in pts.skip(1)) {
      path.lineTo(p.dx, p.dy);
    }
    final area = Path.from(path)
      ..lineTo(pts.last.dx, size.height)
      ..lineTo(pts.first.dx, size.height)
      ..close();
    canvas.drawPath(
      area,
      Paint()
        ..shader = const LinearGradient(
          begin: Alignment.topCenter,
          end: Alignment.bottomCenter,
          colors: [Color(0x332F64B0), Color(0x002F64B0)],
        ).createShader(Offset.zero & size),
    );
    canvas.drawPath(
      path,
      Paint()
        ..color = AppColors.chartMark
        ..style = PaintingStyle.stroke
        ..strokeWidth = 2
        ..strokeJoin = StrokeJoin.round,
    );
  }

  @override
  bool shouldRepaint(_LinePainter old) => old.pts != pts;
}

/// Labelled linear meter (utilisation, progress, quota).
class ProgressMeter extends StatelessWidget {
  const ProgressMeter({super.key, required this.value, required this.label, this.tone = AppTone.brand, this.trailing});

  final double value;
  final String label;
  final AppTone tone;
  final String? trailing;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Semantics(
      label: '$label ${trailing ?? '${(value * 100).round()}%'}',
      excludeSemantics: true,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(child: Text(label, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate600))),
              Text(trailing ?? '${(value * 100).round()}%', style: theme.textTheme.labelMedium),
            ],
          ),
          const SizedBox(height: 6),
          ClipRRect(
            borderRadius: const BorderRadius.all(Radius.circular(AppRadius.full)),
            child: TweenAnimationBuilder<double>(
              tween: Tween(begin: reduceMotion(context) ? value : 0, end: value),
              duration: AppMotion.slow,
              curve: AppMotion.emphasized,
              builder: (context, v, _) => LinearProgressIndicator(
                value: v.clamp(0, 1),
                minHeight: 6,
                color: tone == AppTone.brand ? AppColors.navy600 : tone.foreground,
                backgroundColor: AppColors.slate100,
              ),
            ),
          ),
        ],
      ),
    );
  }
}
