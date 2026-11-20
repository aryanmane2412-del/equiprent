import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:google_maps_flutter/google_maps_flutter.dart' as gm;

import '../../../core/widgets/widgets.dart';
import '../../discovery/presentation/maps/maps_config.dart';
import '../data/tracking_api.dart';

enum PinKind { machine, tracker, phone, site, simulated }

/// A point of interest on the tracking map.
@immutable
class TrackPin {
  const TrackPin({required this.id, required this.point, required this.label, required this.kind});
  final String id;
  final LatLngPoint point;
  final String label;
  final PinKind kind;

  IconData get icon => switch (kind) {
    PinKind.site => AppIcons.siteMarker,
    PinKind.phone => AppIcons.phone,
    PinKind.tracker => AppIcons.signal,
    _ => AppIcons.excavator,
  };

  Color get color => switch (kind) {
    PinKind.site => AppColors.navy700,
    PinKind.phone => AppColors.blue700,
    PinKind.tracker => AppColors.green700,
    PinKind.simulated => AppColors.slate500,
    PinKind.machine => AppColors.orange600,
  };

  double get hue => switch (kind) {
    PinKind.site => gm.BitmapDescriptor.hueBlue,
    PinKind.phone => gm.BitmapDescriptor.hueAzure,
    PinKind.tracker => gm.BitmapDescriptor.hueGreen,
    PinKind.simulated => gm.BitmapDescriptor.hueViolet,
    PinKind.machine => gm.BitmapDescriptor.hueOrange,
  };
}

/// Pin kind for a position by its source.
PinKind pinKindFor(String source) => switch (source) {
  'telematics' => PinKind.tracker,
  'simulated' => PinKind.simulated,
  _ => PinKind.machine,
};

/// Live tracking map. Uses Google Maps when a key is configured and the API
/// loads; otherwise (no key, offline, blocked, quota) a built-in map with the
/// same pins and paths, plus a note that it is approximate. Tracking itself
/// never depends on the map loading.
class TrackingMap extends StatefulWidget {
  const TrackingMap({
    super.key,
    required this.pins,
    this.travelled = const [],
    this.planned = const [],
    this.height = 280,
    this.forceBuiltin = false,
    this.followId,
  });

  final List<TrackPin> pins;

  /// The path covered in the current session.
  final List<LatLngPoint> travelled;

  /// The road route ahead (from the directions service), if known.
  final List<LatLngPoint> planned;
  final double height;
  final bool forceBuiltin;

  /// Keep this pin in view as it moves (Google Maps only).
  final String? followId;

  @override
  State<TrackingMap> createState() => _TrackingMapState();
}

class _TrackingMapState extends State<TrackingMap> {
  late final Future<bool> _google = widget.forceBuiltin || !googleMapsConfigured ? Future.value(false) : ensureGoogleMapsLoaded();

  @override
  Widget build(BuildContext context) {
    if (widget.forceBuiltin || !googleMapsConfigured) {
      return _Builtin(map: widget, note: 'Approximate map — Google Maps is not configured on this build.');
    }
    return FutureBuilder<bool>(
      future: _google,
      builder: (context, snap) {
        if (snap.connectionState != ConnectionState.done) {
          return SizedBox(height: widget.height, child: const LoadingView(message: 'Loading map…'));
        }
        if (snap.data == true) return _Google(map: widget);
        return _Builtin(map: widget, note: 'Approximate map — Google Maps could not be loaded. Positions are still live.');
      },
    );
  }
}

class _Google extends StatefulWidget {
  const _Google({required this.map});
  final TrackingMap map;
  @override
  State<_Google> createState() => _GoogleState();
}

class _GoogleState extends State<_Google> {
  gm.GoogleMapController? _controller;

  static gm.LatLng _ll(LatLngPoint p) => gm.LatLng(p.lat, p.lng);

  @override
  void didUpdateWidget(_Google old) {
    super.didUpdateWidget(old);
    final id = widget.map.followId;
    if (id == null || _controller == null) return;
    final now = widget.map.pins.where((p) => p.id == id).firstOrNull;
    final before = old.map.pins.where((p) => p.id == id).firstOrNull;
    if (now != null && (before == null || before.point.lat != now.point.lat || before.point.lng != now.point.lng)) {
      _controller!.animateCamera(gm.CameraUpdate.newLatLng(_ll(now.point)));
    }
  }

  void _fit() {
    final pts = [for (final p in widget.map.pins) p.point, ...widget.map.planned];
    if (pts.length < 2 || _controller == null) return;
    final b = boundsOf(pts);
    _controller!.moveCamera(gm.CameraUpdate.newLatLngBounds(gm.LatLngBounds(southwest: gm.LatLng(b.$1, b.$2), northeast: gm.LatLng(b.$3, b.$4)), 48));
  }

  @override
  Widget build(BuildContext context) {
    final m = widget.map;
    final first = m.pins.isEmpty ? const LatLngPoint(21.0, 78.0) : m.pins.first.point;
    return ClipRRect(
      borderRadius: AppRadius.card,
      child: SizedBox(
        height: m.height,
        child: gm.GoogleMap(
          initialCameraPosition: gm.CameraPosition(target: _ll(first), zoom: m.pins.isEmpty ? 4.3 : 13),
          markers: {
            for (final p in m.pins)
              gm.Marker(
                markerId: gm.MarkerId(p.id),
                position: _ll(p.point),
                infoWindow: gm.InfoWindow(title: p.label),
                icon: gm.BitmapDescriptor.defaultMarkerWithHue(p.hue),
              ),
          },
          polylines: {
            if (m.planned.length > 1)
              gm.Polyline(
                polylineId: const gm.PolylineId('planned'),
                points: [for (final p in m.planned) _ll(p)],
                color: AppColors.navy500,
                width: 4,
                patterns: [gm.PatternItem.dash(16), gm.PatternItem.gap(8)],
              ),
            if (m.travelled.length > 1)
              gm.Polyline(polylineId: const gm.PolylineId('travelled'), points: [for (final p in m.travelled) _ll(p)], color: AppColors.orange600, width: 5),
          },
          myLocationButtonEnabled: false,
          mapToolbarEnabled: false,
          onMapCreated: (c) {
            _controller = c;
            _fit();
          },
        ),
      ),
    );
  }
}

/// (minLat, minLng, maxLat, maxLng) around [pts].
(double, double, double, double) boundsOf(List<LatLngPoint> pts) {
  var a = pts.first.lat, b = pts.first.lng, c = a, d = b;
  for (final p in pts) {
    a = math.min(a, p.lat);
    b = math.min(b, p.lng);
    c = math.max(c, p.lat);
    d = math.max(d, p.lng);
  }
  return (a, b, c, d);
}

/// Projects coordinates into MapPreview's 0..1 canvas, keeping aspect and a margin.
Offset Function(LatLngPoint) projector(List<LatLngPoint> pts) {
  if (pts.isEmpty) return (_) => const Offset(0.5, 0.5);
  final (minLat, minLng, maxLat, maxLng) = boundsOf(pts);
  final span = math.max(math.max(maxLat - minLat, maxLng - minLng), 0.002);
  final padLat = (span - (maxLat - minLat)) / 2, padLng = (span - (maxLng - minLng)) / 2;
  return (p) => Offset(0.12 + 0.76 * (p.lng - minLng + padLng) / span, 0.88 - 0.76 * (p.lat - minLat + padLat) / span);
}

class _Builtin extends StatelessWidget {
  const _Builtin({required this.map, required this.note});
  final TrackingMap map;
  final String note;

  @override
  Widget build(BuildContext context) {
    final at = projector([for (final p in map.pins) p.point, ...map.travelled, ...map.planned]);
    final path = map.travelled.length > 1 ? map.travelled : (map.planned.length > 1 ? map.planned : const <LatLngPoint>[]);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        MapPreview(
          key: const Key('builtin-map'),
          route: [for (final p in path) at(p)],
          markers: [for (final p in map.pins) MapMarker(position: at(p.point), label: p.label, icon: p.icon, color: p.color)],
          progress: map.travelled.length > 1 ? 1 : 0,
          height: map.height,
          showControls: false,
        ),
        const Gap(AppSpacing.xs),
        Row(
          children: [
            const Icon(AppIcons.info, size: 14, color: AppColors.slate500),
            const Gap(AppSpacing.xs),
            Expanded(
              child: Text(
                note,
                key: const Key('map-note'),
                style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
              ),
            ),
          ],
        ),
      ],
    );
  }
}
