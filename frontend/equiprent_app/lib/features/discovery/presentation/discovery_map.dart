import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart' as gm;

import '../../../core/widgets/widgets.dart';
import '../../listings/data/listing_models.dart';
import '../../listings/data/listings_api.dart';
import '../data/discovery_models.dart';
import 'maps/maps_config.dart';

/// Short price for a map pin: ₹8.5k/day.
String pinPrice(DiscoverItem i) {
  final r = i.card.pricePaise / 100;
  final unit = i.card.priceUnit ?? 'day';
  if (r <= 0) return 'Price?';
  final v = r >= 100000 ? '${(r / 100000).toStringAsFixed(r % 100000 == 0 ? 0 : 1)}L' : r >= 1000 ? '${(r / 1000).toStringAsFixed(r % 1000 == 0 ? 0 : 1)}k' : r.round().toString();
  return '₹$v/$unit';
}

/// Results on a map. Google Maps when a key is configured, otherwise a
/// built-in map drawn from the (approximate) listing points.
class DiscoveryMap extends StatefulWidget {
  const DiscoveryMap({
    super.key,
    required this.items,
    required this.onSelect,
    required this.onSearchArea,
    this.origin,
    this.radiusKm,
    this.selectedId,
    this.forceBuiltin = false,
  });

  final List<DiscoverItem> items;
  final SearchOrigin? origin;
  final double? radiusKm;
  final String? selectedId;
  final ValueChanged<DiscoverItem> onSelect;
  final ValueChanged<MapBounds> onSearchArea;
  final bool forceBuiltin;

  @override
  State<DiscoveryMap> createState() => _DiscoveryMapState();
}

class _DiscoveryMapState extends State<DiscoveryMap> {
  late final Future<bool> _google = widget.forceBuiltin ? Future.value(false) : ensureGoogleMapsLoaded();

  @override
  Widget build(BuildContext context) {
    if (!googleMapsConfigured || widget.forceBuiltin) return BuiltinMap(widget: widget);
    return FutureBuilder<bool>(
      future: _google,
      builder: (context, snap) {
        if (snap.connectionState != ConnectionState.done) return const LoadingView();
        return snap.data == true ? _GoogleMapView(widget: widget) : BuiltinMap(widget: widget);
      },
    );
  }
}

// ---- Google Maps -----------------------------------------------------------------------------

class _GoogleMapView extends StatefulWidget {
  const _GoogleMapView({required this.widget});
  final DiscoveryMap widget;
  @override
  State<_GoogleMapView> createState() => _GoogleMapViewState();
}

class _GoogleMapViewState extends State<_GoogleMapView> {
  gm.GoogleMapController? _controller;
  bool _moved = false;

  DiscoveryMap get w => widget.widget;

  gm.CameraPosition get _initial {
    final o = w.origin;
    if (o != null) return gm.CameraPosition(target: gm.LatLng(o.lat, o.lng), zoom: 10);
    final pts = w.items.where((i) => i.lat != null).toList();
    if (pts.isEmpty) return const gm.CameraPosition(target: gm.LatLng(21.0, 78.0), zoom: 4.3);
    return gm.CameraPosition(target: gm.LatLng(pts.first.lat!, pts.first.lng!), zoom: 7);
  }

  @override
  Widget build(BuildContext context) {
    final markers = {
      for (final i in w.items.where((i) => i.lat != null))
        gm.Marker(
          markerId: gm.MarkerId(i.id),
          position: gm.LatLng(i.lat!, i.lng!),
          infoWindow: gm.InfoWindow(title: i.card.title, snippet: '${pinPrice(i)} · approximate location'),
          icon: gm.BitmapDescriptor.defaultMarkerWithHue(i.id == w.selectedId ? gm.BitmapDescriptor.hueOrange : gm.BitmapDescriptor.hueAzure),
          onTap: () => w.onSelect(i),
        ),
    };
    final o = w.origin;
    return Stack(
      children: [
        gm.GoogleMap(
          initialCameraPosition: _initial,
          markers: markers,
          circles: {
            if (o != null && w.radiusKm != null)
              gm.Circle(circleId: const gm.CircleId('radius'), center: gm.LatLng(o.lat, o.lng), radius: w.radiusKm! * 1000,
                  strokeWidth: 1, strokeColor: AppColors.navy600, fillColor: AppColors.navy600.withValues(alpha: 0.08)),
          },
          myLocationButtonEnabled: false,
          onMapCreated: (c) => _controller = c,
          onCameraMoveStarted: () => setState(() => _moved = true),
        ),
        if (_moved)
          Positioned(
            top: AppSpacing.sm,
            left: 0,
            right: 0,
            child: Center(
              child: AppButton(
                key: const Key('search-area'),
                label: 'Search this area',
                icon: AppIcons.search,
                size: AppButtonSize.small,
                onPressed: () async {
                  final r = await _controller?.getVisibleRegion();
                  if (r == null) return;
                  setState(() => _moved = false);
                  w.onSearchArea(clampBounds(MapBounds(r.southwest.latitude, r.southwest.longitude, r.northeast.latitude, r.northeast.longitude)));
                },
              ),
            ),
          ),
      ],
    );
  }
}

/// Keeps a viewport inside what the API accepts (30 x 40 degrees).
MapBounds clampBounds(MapBounds b) {
  double mid(double a, double c) => (a + c) / 2;
  var minLat = b.minLat, maxLat = b.maxLat, minLng = b.minLng, maxLng = b.maxLng;
  if (maxLat - minLat > 29.9) {
    final m = mid(minLat, maxLat);
    minLat = m - 14.95;
    maxLat = m + 14.95;
  }
  if (maxLng - minLng > 39.9) {
    final m = mid(minLng, maxLng);
    minLng = m - 19.95;
    maxLng = m + 19.95;
  }
  return MapBounds(math.max(minLat, -90), math.max(minLng, -180), math.min(maxLat, 90), math.min(maxLng, 180));
}

// ---- built-in map ----------------------------------------------------------------------------

/// A lightweight map: results, the search point and radius, and city
/// reference dots on a projected grid. Drag to pan, buttons or pinch to
/// zoom, then "Search this area".
class BuiltinMap extends ConsumerStatefulWidget {
  const BuiltinMap({super.key, required this.widget});
  final DiscoveryMap widget;
  @override
  ConsumerState<BuiltinMap> createState() => _BuiltinMapState();
}

class _BuiltinMapState extends ConsumerState<BuiltinMap> {
  late double _cLat, _cLng, _span; // centre and latitude span (degrees)
  bool _moved = false;
  double _scaleStartSpan = 1;

  DiscoveryMap get w => widget.widget;

  @override
  void initState() {
    super.initState();
    _fit();
  }

  @override
  void didUpdateWidget(BuiltinMap old) {
    super.didUpdateWidget(old);
    if (!_moved && (old.widget.items != w.items || old.widget.origin != w.origin)) _fit();
  }

  void _fit() {
    final lats = <double>[], lngs = <double>[];
    for (final i in w.items) {
      if (i.lat != null) {
        lats.add(i.lat!);
        lngs.add(i.lng!);
      }
    }
    final o = w.origin;
    if (o != null) {
      lats.add(o.lat);
      lngs.add(o.lng);
      final r = (w.radiusKm ?? 0) / 111.0;
      if (r > 0) {
        lats.addAll([o.lat - r, o.lat + r]);
        lngs.addAll([o.lng - r, o.lng + r]);
      }
    }
    if (lats.isEmpty) {
      _cLat = 21.0;
      _cLng = 78.5;
      _span = 26;
      return;
    }
    final minLat = lats.reduce(math.min), maxLat = lats.reduce(math.max);
    final minLng = lngs.reduce(math.min), maxLng = lngs.reduce(math.max);
    _cLat = (minLat + maxLat) / 2;
    _cLng = (minLng + maxLng) / 2;
    _span = math.max(math.max(maxLat - minLat, (maxLng - minLng) * math.cos(_cLat * math.pi / 180)) * 1.35, 0.06);
  }

  // Projection: equirectangular around the centre, shorter side = span.
  ({double sx, double sy}) _scale(Size s) {
    final pxPerDegLat = math.min(s.width, s.height) / _span;
    return (sx: pxPerDegLat * math.cos(_cLat * math.pi / 180), sy: pxPerDegLat);
  }

  Offset _toPx(Size s, double lat, double lng) {
    final k = _scale(s);
    return Offset(s.width / 2 + (lng - _cLng) * k.sx, s.height / 2 - (lat - _cLat) * k.sy);
  }

  MapBounds _bounds(Size s) {
    final k = _scale(s);
    final halfLat = s.height / 2 / k.sy, halfLng = s.width / 2 / k.sx;
    return clampBounds(MapBounds(_cLat - halfLat, _cLng - halfLng, _cLat + halfLat, _cLng + halfLng));
  }

  void _zoom(double f) => setState(() {
        _span = (_span * f).clamp(0.02, 30.0);
        _moved = true;
      });

  @override
  Widget build(BuildContext context) {
    final cities = ref.watch(citiesProvider).value ?? const <City>[];
    return LayoutBuilder(builder: (context, c) {
      final size = Size(c.maxWidth, c.maxHeight);
      final selected = w.selectedId;
      // Pins that would overlap are drawn as one cluster (tap to zoom in).
      final clusters = <({List<DiscoverItem> items, Offset at})>[];
      for (final i in w.items.where((i) => i.lat != null)) {
        final p = _toPx(size, i.lat!, i.lng!);
        final hit = clusters.indexWhere((c) => (c.at.dx - p.dx).abs() < 64 && (c.at.dy - p.dy).abs() < 28);
        if (hit < 0) {
          clusters.add((items: [i], at: p));
        } else {
          final c = clusters[hit];
          final n = c.items.length;
          clusters[hit] = (items: [...c.items, i], at: Offset((c.at.dx * n + p.dx) / (n + 1), (c.at.dy * n + p.dy) / (n + 1)));
        }
      }
      clusters.sort((a, b) => (a.items.any((i) => i.id == selected) ? 1 : 0).compareTo(b.items.any((i) => i.id == selected) ? 1 : 0));
      return Semantics(
        label: 'Map of ${w.items.length} machines. Locations are approximate.',
        child: ClipRect(
          child: GestureDetector(
            onScaleStart: (_) => _scaleStartSpan = _span,
            onScaleUpdate: (d) {
              final k = _scale(size);
              setState(() {
                _cLng -= d.focalPointDelta.dx / k.sx;
                _cLat += d.focalPointDelta.dy / k.sy;
                if (d.scale != 1) _span = (_scaleStartSpan / d.scale).clamp(0.02, 30.0);
                _moved = true;
              });
            },
            child: Stack(
              children: [
                Positioned.fill(
                  child: CustomPaint(
                    painter: _MapPainter(
                      toPx: (lat, lng) => _toPx(size, lat, lng),
                      span: _span,
                      center: (_cLat, _cLng),
                      cities: [for (final c in cities) (c.name, c.lat, c.lng)],
                      origin: w.origin,
                      radiusPx: w.origin == null || w.radiusKm == null ? null : w.radiusKm! / 111.0 * _scale(size).sy,
                    ),
                  ),
                ),
                for (final c in clusters)
                  if (c.at.dx > -40 && c.at.dx < size.width + 40 && c.at.dy > -20 && c.at.dy < size.height + 20)
                    Positioned(
                      left: c.at.dx - 36,
                      top: c.at.dy - 30,
                      child: c.items.length == 1
                          ? _Pin(item: c.items.single, selected: c.items.single.id == selected, onTap: () => w.onSelect(c.items.single))
                          : _ClusterPin(
                              count: c.items.length,
                              onTap: () => setState(() {
                                final k = _scale(size);
                                _cLng += (c.at.dx - size.width / 2) / k.sx;
                                _cLat -= (c.at.dy - size.height / 2) / k.sy;
                                _span = (_span / 3).clamp(0.02, 30.0);
                                _moved = true;
                              }),
                            ),
                    ),
                Positioned(
                  right: AppSpacing.sm,
                  bottom: AppSpacing.sm,
                  child: Column(
                    children: [
                      _MapButton(icon: AppIcons.add, tooltip: 'Zoom in', onTap: () => _zoom(0.5)),
                      const Gap(4),
                      _MapButton(icon: Icons.remove, tooltip: 'Zoom out', onTap: () => _zoom(2)),
                      const Gap(4),
                      _MapButton(icon: AppIcons.locate, tooltip: 'Fit results', onTap: () => setState(() {
                            _moved = false;
                            _fit();
                          })),
                    ],
                  ),
                ),
                if (_moved)
                  Positioned(
                    top: AppSpacing.sm,
                    left: 0,
                    right: 0,
                    child: Center(
                      child: AppButton(
                        key: const Key('search-area'),
                        label: 'Search this area',
                        icon: AppIcons.search,
                        size: AppButtonSize.small,
                        onPressed: () {
                          setState(() => _moved = false);
                          w.onSearchArea(_bounds(size));
                        },
                      ),
                    ),
                  ),
                Positioned(
                  left: AppSpacing.sm,
                  bottom: AppSpacing.sm,
                  child: Container(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                    decoration: BoxDecoration(color: Colors.white.withValues(alpha: 0.85), borderRadius: BorderRadius.circular(4)),
                    child: Text('Approximate locations (±1 km)', style: Theme.of(context).textTheme.labelSmall?.copyWith(color: AppColors.slate600, letterSpacing: 0)),
                  ),
                ),
              ],
            ),
          ),
        ),
      );
    });
  }
}

class _Pin extends StatelessWidget {
  const _Pin({required this.item, required this.selected, required this.onTap});
  final DiscoverItem item;
  final bool selected;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final bg = selected ? AppColors.orange500 : AppColors.navy700;
    return Semantics(
      button: true,
      label: '${item.card.title}, ${pinPrice(item)}',
      child: GestureDetector(
        key: Key('pin-${item.id}'),
        onTap: onTap,
        child: SizedBox(
          width: 72,
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Container(
                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 3),
                decoration: BoxDecoration(
                  color: bg,
                  borderRadius: BorderRadius.circular(12),
                  boxShadow: const [BoxShadow(color: Color(0x33000000), blurRadius: 4, offset: Offset(0, 1))],
                ),
                child: Text(pinPrice(item), maxLines: 1, overflow: TextOverflow.ellipsis,
                    style: const TextStyle(color: Colors.white, fontSize: 11, fontWeight: FontWeight.w700)),
              ),
              CustomPaint(size: const Size(10, 6), painter: _TrianglePainter(bg)),
            ],
          ),
        ),
      ),
    );
  }
}

class _ClusterPin extends StatelessWidget {
  const _ClusterPin({required this.count, required this.onTap});
  final int count;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Semantics(
        button: true,
        label: '$count machines here. Zoom in',
        child: GestureDetector(
          onTap: onTap,
          child: SizedBox(
            width: 72,
            height: 36,
            child: Center(
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
                decoration: BoxDecoration(
                  color: Colors.white,
                  border: Border.all(color: AppColors.navy700, width: 2),
                  borderRadius: BorderRadius.circular(14),
                  boxShadow: const [BoxShadow(color: Color(0x33000000), blurRadius: 4, offset: Offset(0, 1))],
                ),
                child: Text('$count', style: const TextStyle(color: AppColors.navy800, fontSize: 12, fontWeight: FontWeight.w800)),
              ),
            ),
          ),
        ),
      );
}

class _TrianglePainter extends CustomPainter {
  const _TrianglePainter(this.color);
  final Color color;
  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawPath(Path()..moveTo(0, 0)..lineTo(size.width, 0)..lineTo(size.width / 2, size.height)..close(), Paint()..color = color);
  }

  @override
  bool shouldRepaint(_TrianglePainter old) => old.color != color;
}

class _MapButton extends StatelessWidget {
  const _MapButton({required this.icon, required this.tooltip, required this.onTap});
  final IconData icon;
  final String tooltip;
  final VoidCallback onTap;
  @override
  Widget build(BuildContext context) => Material(
        color: Colors.white,
        shape: const CircleBorder(),
        elevation: 2,
        child: IconButton(tooltip: tooltip, icon: Icon(icon, size: 18), onPressed: onTap),
      );
}

class _MapPainter extends CustomPainter {
  _MapPainter({required this.toPx, required this.span, required this.center, required this.cities, this.origin, this.radiusPx});
  final Offset Function(double lat, double lng) toPx;
  final double span;
  final (double, double) center;
  final List<(String, double, double)> cities;
  final SearchOrigin? origin;
  final double? radiusPx;

  @override
  void paint(Canvas canvas, Size size) {
    canvas.drawRect(Offset.zero & size, Paint()..color = const Color(0xFFEFF3F8));
    // Graticule at a step that gives a few lines per screen.
    final step = [0.01, 0.02, 0.05, 0.1, 0.2, 0.5, 1.0, 2.0, 5.0].firstWhere((s) => span / s <= 8, orElse: () => 10.0);
    final grid = Paint()
      ..color = const Color(0xFFD9E1EC)
      ..strokeWidth = 1;
    final (cLat, cLng) = center;
    for (var lat = ((cLat - span * 2) / step).floor() * step; lat <= cLat + span * 2; lat += step) {
      final y = toPx(lat, cLng).dy;
      canvas.drawLine(Offset(0, y), Offset(size.width, y), grid);
    }
    for (var lng = ((cLng - span * 3) / step).floor() * step; lng <= cLng + span * 3; lng += step) {
      final x = toPx(cLat, lng).dx;
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), grid);
    }
    // Reference cities.
    for (final (name, lat, lng) in cities) {
      final p = toPx(lat, lng);
      if (!(Offset.zero & size).inflate(-4).contains(p)) continue;
      canvas.drawCircle(p, 3, Paint()..color = AppColors.slate400);
      final tp = TextPainter(
        text: TextSpan(text: name, style: const TextStyle(color: AppColors.slate500, fontSize: 11, fontWeight: FontWeight.w600)),
        textDirection: TextDirection.ltr,
      )..layout();
      tp.paint(canvas, p + const Offset(5, -7));
    }
    // Search point and radius.
    final o = origin;
    if (o != null) {
      final p = toPx(o.lat, o.lng);
      if (radiusPx != null) {
        canvas.drawCircle(p, radiusPx!, Paint()..color = AppColors.navy600.withValues(alpha: 0.07));
        canvas.drawCircle(p, radiusPx!, Paint()
          ..color = AppColors.navy600.withValues(alpha: 0.4)
          ..style = PaintingStyle.stroke
          ..strokeWidth = 1.2);
      }
      canvas.drawCircle(p, 8, Paint()..color = Colors.white);
      canvas.drawCircle(p, 6, Paint()..color = const Color(0xFF2F6FED));
    }
  }

  @override
  bool shouldRepaint(_MapPainter old) =>
      old.span != span || old.center != center || old.origin != origin || old.radiusPx != radiusPx || old.cities.length != cities.length;
}
