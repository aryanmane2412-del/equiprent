import 'maps_loader_stub.dart' if (dart.library.js_interop) 'maps_loader_web.dart' as loader;

/// Google Maps browser/app key, supplied at build time:
///   flutter run --dart-define=GOOGLE_MAPS_API_KEY=...
/// Android also reads it from android/local.properties (see docs). Without
/// a key the built-in map is used, so development needs no credentials.
const googleMapsKey = String.fromEnvironment('GOOGLE_MAPS_API_KEY');

bool get googleMapsConfigured => googleMapsKey.isNotEmpty;

/// Loads the Maps JavaScript API on web (no-op elsewhere). Completes with
/// false when it cannot be loaded, so callers fall back to the built-in map.
Future<bool> ensureGoogleMapsLoaded() => googleMapsConfigured ? loader.load(googleMapsKey) : Future.value(false);
