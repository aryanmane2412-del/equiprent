import 'dart:math' as math;
import 'dart:ui' show PathMetric;

import 'package:flutter/material.dart';

import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import 'feedback.dart';

/// A marker on [MapPreview], positioned in 0..1 map coordinates.
class MapMarker {
  const MapMarker({
    required this.position,
    required this.label,
    this.icon = AppIcons.fleet,
    this.color = AppColors.navy700,
  });

  final Offset position;
  final String label;
  final IconData icon;
  final Color color;
}

/// Stylised map used until a maps provider is chosen (Google / Ola / Mappls,
/// see docs/PROJECT_PLAN.md §12). Draws roads, an optional route and markers.
/// With [animateVehicle] the vehicle moves along the route (paused when the
/// platform requests reduced motion).
class MapPreview extends StatefulWidget {
  const MapPreview({
    super.key,
    this.route = const [],
    this.markers = const [],
    this.progress = 0.6,
    this.animateVehicle = false,
    this.height = 260,
    this.showControls = true,
    this.onRecenter,
  });

  /// Route waypoints in 0..1 coordinates (start → destination).
  final List<Offset> route;
  final List<MapMarker> markers;

  /// Fraction of the route already travelled (vehicle position).
  final double progress;
  final bool animateVehicle;
  final double height;
  final bool showControls;
  final VoidCallback? onRecenter;

  @override
  State<MapPreview> createState() => _MapPreviewState();
}

class _MapPreviewState extends State<MapPreview>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(seconds: 8),
  );

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    if (widget.animateVehicle && !reduceMotion(context)) {
      if (!_c.isAnimating) _c.repeat();
    } else {
      _c.stop();
    }
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: 'Map preview${widget.route.isEmpty ? '' : ' showing route'}',
      child: ClipRRect(
        borderRadius: AppRadius.card,
        child: SizedBox(
          height: widget.height,
          child: LayoutBuilder(
            builder: (context, c) {
              final size = c.biggest;
              return AnimatedBuilder(
                animation: _c,
                builder: (context, _) {
                  final animating = widget.animateVehicle && _c.isAnimating;
                  // Vehicle oscillates between progress and the destination.
                  final p = animating
                      ? widget.progress +
                            (1 - widget.progress) *
                                0.35 *
                                (0.5 - 0.5 * math.cos(_c.value * 2 * math.pi))
                      : widget.progress;
                  final vehicle = _pointAlong(widget.route, size, p);
                  return Stack(
                    children: [
                      Positioned.fill(
                        child: CustomPaint(
                          painter: _MapPainter(
                            route: widget.route,
                            progress: p,
                          ),
                        ),
                      ),
                      for (final m in widget.markers)
                        _marker(m.position.scale(size.width, size.height), m),
                      if (widget.route.length >= 2) ...[
                        _pin(widget.route.last.scale(size.width, size.height)),
                        if (vehicle != null) _vehicle(vehicle),
                      ],
                      if (widget.showControls)
                        Positioned(
                          right: 10,
                          top: 10,
                          child: Column(
                            children: [
                              _mapButton(
                                AppIcons.locate,
                                widget.onRecenter == null
                                    ? 'Live maps unavailable'
                                    : 'Recenter',
                                widget.onRecenter,
                              ),
                              const SizedBox(height: 6),
                              _mapButton(
                                Icons.add,
                                'Live maps unavailable',
                                null,
                              ),
                              const SizedBox(height: 6),
                              _mapButton(
                                Icons.remove,
                                'Live maps unavailable',
                                null,
                              ),
                            ],
                          ),
                        ),
                      Positioned(
                        left: 10,
                        bottom: 8,
                        child: Text(
                          'Map preview · live maps arrive with the maps provider',
                          style: Theme.of(context).textTheme.labelSmall
                              ?.copyWith(
                                color: AppColors.slate500,
                                letterSpacing: 0,
                              ),
                        ),
                      ),
                    ],
                  );
                },
              );
            },
          ),
        ),
      ),
    );
  }

  Widget _mapButton(IconData icon, String tip, VoidCallback? onTap) => Material(
    color: AppColors.white,
    elevation: 2,
    shadowColor: Colors.black26,
    borderRadius: const BorderRadius.all(Radius.circular(8)),
    child: InkWell(
      borderRadius: const BorderRadius.all(Radius.circular(8)),
      onTap: onTap,
      child: Tooltip(
        message: tip,
        child: SizedBox.square(
          dimension: 36,
          child: Icon(icon, size: 18, color: AppColors.slate700),
        ),
      ),
    ),
  );

  Widget _vehicle(Offset at) => Positioned(
    left: at.dx - 18,
    top: at.dy - 18,
    child: Container(
      width: 36,
      height: 36,
      decoration: BoxDecoration(
        color: AppColors.orange600,
        shape: BoxShape.circle,
        border: Border.all(color: AppColors.white, width: 3),
        boxShadow: AppShadows.md,
      ),
      child: const Icon(AppIcons.fleet, size: 16, color: AppColors.white),
    ),
  );

  Widget _pin(Offset at) => Positioned(
    left: at.dx - 16,
    top: at.dy - 34,
    child: const Icon(Icons.location_on, size: 34, color: AppColors.navy800),
  );

  Widget _marker(Offset at, MapMarker m) => Positioned(
    left: at.dx - 16,
    top: at.dy - 16,
    child: Tooltip(
      message: m.label,
      child: Container(
        width: 32,
        height: 32,
        decoration: BoxDecoration(
          color: m.color,
          shape: BoxShape.circle,
          border: Border.all(color: AppColors.white, width: 2.5),
          boxShadow: AppShadows.sm,
        ),
        child: Icon(m.icon, size: 14, color: AppColors.white),
      ),
    ),
  );

  static Offset? _pointAlong(List<Offset> route, Size size, double t) {
    if (route.length < 2) return null;
    final path = _routePath(route, size);
    final metrics = path.computeMetrics().toList();
    if (metrics.isEmpty) return null;
    final PathMetric m = metrics.first;
    return m.getTangentForOffset(m.length * t.clamp(0, 1))?.position;
  }
}

Path _routePath(List<Offset> route, Size size) {
  final pts = [for (final p in route) p.scale(size.width, size.height)];
  final path = Path()..moveTo(pts.first.dx, pts.first.dy);
  for (final p in pts.skip(1)) {
    path.lineTo(p.dx, p.dy);
  }
  return path;
}

class _MapPainter extends CustomPainter {
  _MapPainter({required this.route, required this.progress});

  final List<Offset> route;
  final double progress;

  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawRect(
      Offset.zero & size,
      Paint()..color = const Color(0xFFEFF2EA),
    );
    // Parks and water.
    canvas.drawRRect(
      RRect.fromRectAndRadius(
        Rect.fromLTWH(
          size.width * 0.62,
          size.height * 0.08,
          size.width * 0.22,
          size.height * 0.22,
        ),
        const Radius.circular(14),
      ),
      Paint()..color = const Color(0xFFD7E8CF),
    );
    canvas.drawPath(
      Path()
        ..moveTo(0, size.height * 0.82)
        ..quadraticBezierTo(
          size.width * 0.3,
          size.height * 0.7,
          size.width * 0.55,
          size.height * 0.95,
        )
        ..lineTo(size.width * 0.55, size.height)
        ..lineTo(0, size.height)
        ..close(),
      Paint()..color = const Color(0xFFCFE0F2),
    );
    // Road grid.
    final minor = Paint()
      ..color = Colors.white
      ..strokeWidth = 6;
    final major = Paint()
      ..color = const Color(0xFFFFF4D6)
      ..strokeWidth = 10;
    for (final f in [0.18, 0.42, 0.7]) {
      canvas.drawLine(
        Offset(0, size.height * f),
        Offset(size.width, size.height * (f + 0.05)),
        minor,
      );
    }
    for (final f in [0.25, 0.55, 0.82]) {
      canvas.drawLine(
        Offset(size.width * f, 0),
        Offset(size.width * (f - 0.06), size.height),
        minor,
      );
    }
    canvas.drawLine(
      Offset(0, size.height * 0.58),
      Offset(size.width, size.height * 0.48),
      major,
    );

    if (route.length >= 2) {
      final path = _routePath(route, size);
      canvas.drawPath(
        path,
        Paint()
          ..color = AppColors.navy200
          ..style = PaintingStyle.stroke
          ..strokeWidth = 6
          ..strokeCap = StrokeCap.round
          ..strokeJoin = StrokeJoin.round,
      );
      final m = path.computeMetrics().first;
      canvas.drawPath(
        m.extractPath(0, m.length * progress.clamp(0, 1)),
        Paint()
          ..color = AppColors.navy600
          ..style = PaintingStyle.stroke
          ..strokeWidth = 6
          ..strokeCap = StrokeCap.round
          ..strokeJoin = StrokeJoin.round,
      );
      canvas.drawCircle(
        route.first.scale(size.width, size.height),
        6,
        Paint()..color = AppColors.navy700,
      );
      canvas.drawCircle(
        route.first.scale(size.width, size.height),
        3,
        Paint()..color = Colors.white,
      );
    }
  }

  @override
  bool shouldRepaint(_MapPainter old) =>
      old.progress != progress || old.route != route;
}
