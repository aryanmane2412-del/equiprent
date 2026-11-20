import 'dart:async';
import 'dart:math' as math;

import 'package:flutter/foundation.dart';
import 'package:flutter/services.dart' show MissingPluginException;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';

import '../../../core/config/app_config.dart';
import '../../discovery/presentation/maps/maps_config.dart';
import 'tracking_api.dart';

/// Outcome of asking for location access.
enum LocationAccess { granted, denied, deniedForever, serviceOff, unsupported }

/// Where positions come from. The phone's GPS identifies the phone, not the
/// machine; trackers fitted to machines report straight to the backend
/// (see /telematics/{provider}/positions) and never go through this.
abstract class LocationSource {
  /// True for the development simulator. Uploads are flagged so the server
  /// stores them as `simulated`, never as a real position.
  bool get simulated;

  /// Asks for permission (foreground; background where requested and the
  /// platform supports it). Never called before the person agreed to share.
  Future<LocationAccess> prepare({required bool background});

  /// Positions every [interval] until the subscription is cancelled.
  Stream<GeoFix> watch({required Duration interval, required bool background, LatLngPoint? towards});
}

/// The device's GPS through geolocator. With [background] on Android a
/// persistent notification is shown while sharing (a foreground service), and
/// iOS shows the blue location indicator, so sharing is never hidden.
class DeviceLocationSource implements LocationSource {
  const DeviceLocationSource();

  @override
  bool get simulated => false;

  @override
  Future<LocationAccess> prepare({required bool background}) async {
    try {
      if (!await Geolocator.isLocationServiceEnabled()) return LocationAccess.serviceOff;
      var perm = await Geolocator.checkPermission();
      if (perm == LocationPermission.denied) perm = await Geolocator.requestPermission();
      return switch (perm) {
        LocationPermission.deniedForever => LocationAccess.deniedForever,
        LocationPermission.denied => LocationAccess.denied,
        LocationPermission.unableToDetermine => LocationAccess.unsupported,
        _ => LocationAccess.granted,
      };
    } on UnsupportedError {
      return LocationAccess.unsupported;
    } on MissingPluginException {
      return LocationAccess.unsupported;
    }
  }

  @override
  Stream<GeoFix> watch({required Duration interval, required bool background, LatLngPoint? towards}) {
    final LocationSettings settings;
    if (!kIsWeb && defaultTargetPlatform == TargetPlatform.android) {
      settings = AndroidSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: 10,
        intervalDuration: interval,
        foregroundNotificationConfig: background
            ? const ForegroundNotificationConfig(
                notificationTitle: 'EQUIPRENT is sharing your location',
                notificationText: 'For the current job only. Stop sharing from the job screen.',
                setOngoing: true,
              )
            : null,
      );
    } else if (!kIsWeb && defaultTargetPlatform == TargetPlatform.iOS) {
      settings = AppleSettings(
        accuracy: LocationAccuracy.high,
        distanceFilter: 10,
        activityType: ActivityType.automotiveNavigation,
        allowBackgroundLocationUpdates: background,
        showBackgroundLocationIndicator: background,
      );
    } else {
      settings = const LocationSettings(accuracy: LocationAccuracy.high, distanceFilter: 10);
    }
    return Geolocator.getPositionStream(locationSettings: settings).map(
      (p) => GeoFix(
        lat: p.latitude,
        lng: p.longitude,
        at: p.timestamp,
        accuracyM: p.accuracy,
        speedMps: p.speed,
        heading: p.heading.isNaN ? null : p.heading.round() % 360,
      ),
    );
  }
}

/// Development GPS: drives from a point a few kilometres away towards the
/// destination. Used when no Google Maps key is configured (local builds),
/// so tracking can be exercised without credentials or a moving phone.
class SimulatedLocationSource implements LocationSource {
  SimulatedLocationSource({this.steps = 60, DateTime Function()? now}) : _now = now ?? DateTime.now;

  /// Number of positions from the start to the destination.
  final int steps;
  final DateTime Function() _now;

  @override
  bool get simulated => true;

  @override
  Future<LocationAccess> prepare({required bool background}) async => LocationAccess.granted;

  @override
  Stream<GeoFix> watch({required Duration interval, required bool background, LatLngPoint? towards}) {
    final dest = towards ?? const LatLngPoint(18.5204, 73.8567);
    return Stream<int>.periodic(interval, (i) => i).map((i) => fixAt(dest, i));
  }

  /// The [i]th simulated position on the way to [dest] (stays there after arrival).
  GeoFix fixAt(LatLngPoint dest, int i) {
    final start = LatLngPoint(dest.lat - 0.03, dest.lng - 0.035); // ~5 km south-west
    final t = math.min(1.0, i / steps);
    // A gentle curve so the path looks like a road rather than a ruler line.
    final bend = math.sin(t * math.pi) * 0.006;
    return GeoFix(
      lat: start.lat + (dest.lat - start.lat) * t + bend,
      lng: start.lng + (dest.lng - start.lng) * t - bend,
      at: _now(),
      accuracyM: 8,
      speedMps: t >= 1 ? 0 : 8.3,
    );
  }
}

/// Simulation is used in development builds without a Google Maps key, or
/// when forced with --dart-define=TRACKING_SIMULATE=true. Never in production
/// (the server rejects simulated uploads there too).
bool shouldSimulate(AppConfig config) {
  const forced = bool.fromEnvironment('TRACKING_SIMULATE');
  if (config.isProduction) return false;
  return forced || !googleMapsConfigured;
}

final locationSourceProvider = Provider<LocationSource>(
  (ref) => shouldSimulate(ref.watch(appConfigProvider)) ? SimulatedLocationSource() : const DeviceLocationSource(),
);
