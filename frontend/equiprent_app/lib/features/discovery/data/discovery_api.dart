import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:geolocator/geolocator.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/network/api_client.dart';
import '../../listings/data/listing_models.dart' show Json;
import 'discovery_models.dart';

/// The discovery endpoints (search, suggestions, saved equipment, history,
/// comparison).
abstract class DiscoveryApi {
  Future<DiscoverResults> search(DiscoverQuery q);
  Future<List<Suggestion>> suggest(String text);
  Future<List<PlaceOption>> places(String text);
  Future<LocationDetails> lookupPincode(String pincode);
  Future<LocationDetails> reverseGeocode(double lat, double lng);
  Future<String> mapProvider();
  Future<Comparison> compare(List<String> ids);

  Future<List<DiscoverItem>> saved();
  Future<Set<String>> savedIds();
  Future<void> save(String id);
  Future<void> unsave(String id);

  Future<List<HistoryEntry>> history();
  Future<void> forget(String id);
  Future<void> clearHistory();
}

class HttpDiscoveryApi implements DiscoveryApi {
  HttpDiscoveryApi(this._c);
  final ApiClient _c;

  static Json _j(Object? v) => (v as Map).cast<String, dynamic>();
  static List<Json> _data(Object? v) => [
    for (final e in (_j(v)['data'] as List? ?? const [])) _j(e),
  ];

  @override
  Future<DiscoverResults> search(DiscoverQuery q) => _c.get(
    '/discover/equipment',
    query: q.toQuery(),
    decode: (j) => DiscoverResults.fromJson(_j(j)),
  );
  @override
  Future<List<Suggestion>> suggest(String text) => _c.get(
    '/discover/suggest',
    query: {'q': text},
    decode: (j) => [for (final s in _data(j)) Suggestion.fromJson(s)],
  );
  @override
  Future<List<PlaceOption>> places(String text) => _c.get(
    '/discover/locations',
    query: {'q': text},
    decode: (j) => [for (final s in _data(j)) PlaceOption.fromJson(s)],
  );
  @override
  Future<LocationDetails> lookupPincode(String pincode) => _c.get(
    '/discover/locations/pincode/${Uri.encodeComponent(pincode)}',
    decode: (j) => LocationDetails.fromJson(_j(j)),
  );
  @override
  Future<LocationDetails> reverseGeocode(double lat, double lng) => _c.get(
    '/discover/locations/reverse',
    query: {'lat': lat, 'lng': lng},
    decode: (j) => LocationDetails.fromJson(_j(j)),
  );
  @override
  Future<String> mapProvider() =>
      _c.get('/discover/maps', decode: (j) => '${_j(j)['provider']}');
  @override
  Future<Comparison> compare(List<String> ids) => _c.get(
    '/discover/compare',
    query: {'ids': ids.join(',')},
    decode: (j) => Comparison.fromJson(_j(j)),
  );

  @override
  Future<List<DiscoverItem>> saved() => _c.get(
    '/me/saved-equipment',
    decode: (j) => [for (final s in _data(j)) DiscoverItem.fromJson(s)],
  );
  @override
  Future<Set<String>> savedIds() => _c.get(
    '/me/saved-equipment/ids',
    decode: (j) => {for (final v in (_j(j)['data'] as List? ?? const [])) '$v'},
  );
  @override
  Future<void> save(String id) =>
      _c.put('/me/saved-equipment/$id', decode: (_) {});
  @override
  Future<void> unsave(String id) => _c.delete('/me/saved-equipment/$id');

  @override
  Future<List<HistoryEntry>> history() => _c.get(
    '/me/search-history',
    decode: (j) => [for (final s in _data(j)) HistoryEntry.fromJson(s)],
  );
  @override
  Future<void> forget(String id) => _c.delete('/me/search-history/$id');
  @override
  Future<void> clearHistory() => _c.delete('/me/search-history');
}

final discoveryApiProvider = Provider<DiscoveryApi>(
  (ref) => HttpDiscoveryApi(ref.watch(apiClientProvider)),
);

bool _signedIn(Ref ref) => ref.watch(authControllerProvider).user != null;

final discoverProvider = FutureProvider.autoDispose
    .family<DiscoverResults, DiscoverQuery>((ref, q) async {
      final page = await ref.watch(discoveryApiProvider).search(q);
      // A signed-in search may have been added to the history.
      if (_signedIn(ref) && q.page == 1 && q.bounds == null) {
        ref.invalidate(searchHistoryProvider);
      }
      return page;
    });

final suggestionsProvider = FutureProvider.autoDispose
    .family<List<Suggestion>, String>((ref, text) async {
      if (text.trim().length < 2) return const [];
      return ref.watch(discoveryApiProvider).suggest(text.trim());
    });

final placesProvider = FutureProvider.autoDispose
    .family<List<PlaceOption>, String>(
      (ref, text) => ref.watch(discoveryApiProvider).places(text.trim()),
    );

/// "google" when the server has Google Maps credentials, else "builtin".
final mapProviderProvider = FutureProvider<String>(
  (ref) => ref.watch(discoveryApiProvider).mapProvider(),
);

final searchHistoryProvider = FutureProvider.autoDispose<List<HistoryEntry>>((
  ref,
) async {
  if (!_signedIn(ref)) return const [];
  return ref.watch(discoveryApiProvider).history();
});

final savedListProvider = FutureProvider.autoDispose<List<DiscoverItem>>((
  ref,
) async {
  if (!_signedIn(ref)) return const [];
  return ref.watch(discoveryApiProvider).saved();
});

final comparisonProvider = FutureProvider.autoDispose
    .family<Comparison, String>(
      (ref, ids) => ref.watch(discoveryApiProvider).compare(ids.split(',')),
    );

/// Ids of saved listings, with optimistic toggling.
class SavedIds extends AsyncNotifier<Set<String>> {
  @override
  Future<Set<String>> build() async {
    if (!_signedIn(ref)) return <String>{};
    return ref.watch(discoveryApiProvider).savedIds();
  }

  /// Saves or removes [id]; returns the new state (true = saved). Reverts
  /// and rethrows on failure.
  Future<bool> toggle(String id) async {
    final before = state.value ?? <String>{};
    final saving = !before.contains(id);
    state = AsyncData(saving ? {...before, id} : ({...before}..remove(id)));
    try {
      final api = ref.read(discoveryApiProvider);
      await (saving ? api.save(id) : api.unsave(id));
      ref.invalidate(savedListProvider);
      return saving;
    } catch (_) {
      state = AsyncData(before);
      rethrow;
    }
  }
}

final savedIdsProvider = AsyncNotifierProvider<SavedIds, Set<String>>(
  SavedIds.new,
);

/// Machines picked for comparison (max 4), kept across screens.
class CompareSelection extends Notifier<List<String>> {
  static const max = 4;

  @override
  List<String> build() => const [];

  /// Adds or removes [id]. Returns false when the tray is full.
  bool toggle(String id) {
    if (state.contains(id)) {
      state = [...state]..remove(id);
      return true;
    }
    if (state.length >= max) return false;
    state = [...state, id];
    return true;
  }

  void remove(String id) => state = [...state]..remove(id);
  void clear() => state = const [];
}

final compareSelectionProvider =
    NotifierProvider<CompareSelection, List<String>>(CompareSelection.new);

// ---- device location -------------------------------------------------------------------------

/// Thrown when the device location is unavailable (permission denied,
/// services off, unsupported).
class LocationUnavailable implements Exception {
  const LocationUnavailable(this.message);
  final String message;
  @override
  String toString() => message;
}

typedef DeviceLocator = Future<({double lat, double lng})> Function();

Future<({double lat, double lng})> _geolocate() async {
  try {
    if (!await Geolocator.isLocationServiceEnabled()) {
      throw const LocationUnavailable(
        'Turn on location services, or choose a city or PIN code.',
      );
    }
    var perm = await Geolocator.checkPermission();
    if (perm == LocationPermission.denied) {
      perm = await Geolocator.requestPermission();
    }
    if (perm == LocationPermission.deniedForever) {
      throw const LocationUnavailable(
        'Location permission is blocked. Enable location access for EQUIPRENT in your device or browser settings, then try again.',
      );
    }
    if (perm == LocationPermission.denied) {
      throw const LocationUnavailable(
        'Location permission was denied. Allow location access to use GPS, or enter a PIN code instead.',
      );
    }
    final p = await Geolocator.getCurrentPosition(
      locationSettings: const LocationSettings(
        accuracy: LocationAccuracy.best,
        timeLimit: Duration(seconds: 20),
      ),
    );
    return (lat: p.latitude, lng: p.longitude);
  } on LocationUnavailable {
    rethrow;
  } catch (_) {
    throw const LocationUnavailable(
      'Could not get your location. Choose a city or PIN code instead.',
    );
  }
}

/// Reads the device position (overridable in tests).
final deviceLocatorProvider = Provider<DeviceLocator>((ref) => _geolocate);
