import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/discovery/data/discovery_api.dart';
import 'package:equiprent_app/features/discovery/data/discovery_models.dart';
import 'package:equiprent_app/features/listings/data/listing_models.dart';

import 'fake_listings_api.dart';

/// A machine in the fake index.
class FakeMachine {
  FakeMachine(
    this.id,
    this.title,
    this.category,
    this.make,
    this.model,
    this.city,
    this.lat,
    this.lng,
    this.dayRupees, {
    this.verified = true,
    this.delivery = true,
    this.dry = false,
    this.condition = MachineCondition.good,
    this.rating,
    this.created = 0,
  });
  final String id, title, category, make, model, city;
  final double lat, lng;
  final int dayRupees;
  final bool verified, delivery, dry;
  final MachineCondition condition;
  final double? rating;
  final int created; // larger = newer
}

/// In-memory discovery backend for widget tests: text, filters, location,
/// sorting, pages, saved equipment, history and comparison.
class FakeDiscoveryApi implements DiscoveryApi {
  FakeDiscoveryApi({FakeListingsApi? listings})
    : _listings = listings ?? FakeListingsApi();

  final FakeListingsApi _listings;
  final queries = <DiscoverQuery>[];
  final savedSet = <String>{};
  final historyList = <HistoryEntry>[];
  int _ids = 0;
  bool failSave = false;

  static const pincodes = {
    '411057': ('Hinjewadi, Pune', 18.5912, 73.7389),
    '400069': ('Andheri East, Mumbai', 19.1136, 72.8697),
  };

  final machines = [
    FakeMachine(
      'l-live',
      'JCB 3DX backhoe loader',
      'backhoe-loader',
      'JCB',
      '3DX Super',
      'Pune',
      18.595,
      73.735,
      18000,
      rating: 4.8,
      created: 5,
    ),
    FakeMachine(
      'l-crane',
      'Escorts 15 t pick-and-carry crane',
      'crane',
      'Escorts',
      'Hydra 15',
      'Mumbai',
      19.115,
      72.865,
      22000,
      verified: false,
      created: 4,
    ),
    FakeMachine(
      'm-exc',
      'Tata Hitachi EX200 excavator',
      'excavator',
      'Tata Hitachi',
      'EX 200LC',
      'Pune',
      18.505,
      73.925,
      16000,
      dry: true,
      delivery: false,
      condition: MachineCondition.excellent,
      rating: 4.5,
      created: 3,
    ),
    FakeMachine(
      'm-roller',
      'Dynapac vibratory roller',
      'road-roller',
      'Dynapac',
      'CA2500D',
      'Pune',
      18.455,
      73.865,
      9000,
      created: 2,
    ),
    FakeMachine(
      'm-dozer',
      'CAT D6R bulldozer',
      'bulldozer',
      'CAT',
      'D6R',
      'Nagpur',
      21.145,
      79.085,
      24000,
      condition: MachineCondition.fair,
      created: 1,
    ),
  ];

  static double _km(double aLat, double aLng, double bLat, double bLng) {
    final dLat = (bLat - aLat) * 111.0, dLng = (bLng - aLng) * 111.0 * 0.95;
    return (dLat * dLat + dLng * dLng) == 0
        ? 0
        : _sqrt(dLat * dLat + dLng * dLng);
  }

  static double _sqrt(double v) {
    var x = v;
    for (var i = 0; i < 30; i++) {
      x = 0.5 * (x + v / x);
    }
    return x;
  }

  DiscoverItem _item(FakeMachine m, double? dist) => DiscoverItem.fromJson({
    'id': m.id,
    'title': m.title,
    'category_slug': m.category,
    'category_name': m.category,
    'make_name': m.make,
    'model_name': m.model,
    'condition': m.condition.wire,
    'location_label': m.city,
    'city_name': m.city,
    'delivery_available': m.delivery,
    'owner_name': 'Owner of ${m.title}',
    'owner_verified': m.verified,
    'price_paise': m.dayRupees * 100,
    'price_unit': 'day',
    'rating_avg': ?m.rating,
    'approx_location': {'lat': m.lat, 'lng': m.lng},
    'distance_km': ?dist,
    if (dist != null) 'delivers_to_you': m.delivery && dist < 50,
    'wet_hire': true,
    'dry_hire': m.dry,
  });

  @override
  Future<DiscoverResults> search(DiscoverQuery q) async {
    queries.add(q);
    SearchOrigin? origin;
    final place = q.place;
    if (place != null) {
      if (place.pincode != null) {
        final p = pincodes[place.pincode];
        if (p == null) {
          throw const ApiException(
            kind: ApiErrorKind.badRequest,
            code: 'VALIDATION_FAILED',
            message: "We don't have this PIN code yet",
            statusCode: 400,
          );
        }
        origin = SearchOrigin(
          source: 'pincode',
          label: '${place.pincode} · ${p.$1}',
          lat: p.$2,
          lng: p.$3,
        );
      } else if (place.isPoint) {
        origin = SearchOrigin(
          source: 'point',
          label: 'Your location',
          lat: place.lat!,
          lng: place.lng!,
        );
      } else {
        origin = SearchOrigin(
          source: 'city',
          label: place.label,
          lat: 18.52,
          lng: 73.85,
        );
      }
    }
    final text = q.text.toLowerCase();
    final radius = q.radiusKm ?? (place?.pincode != null ? 25 : 50);
    var hits = <(FakeMachine, double?)>[];
    for (final m in machines) {
      final dist = origin == null
          ? null
          : _km(origin.lat, origin.lng, m.lat, m.lng);
      if (text.isNotEmpty &&
          !text
              .split(' ')
              .every(
                (w) => '${m.title} ${m.make} ${m.model} ${m.city}'
                    .toLowerCase()
                    .contains(w),
              )) {
        continue;
      }
      if (q.categories.isNotEmpty && !q.categories.contains(m.category)) {
        continue;
      }
      if (q.make != null && q.make!.toLowerCase() != m.make.toLowerCase()) {
        continue;
      }
      if (place?.cityId != null && m.city != place!.label) continue;
      if (origin != null && origin.source != 'city' && dist! > radius) continue;
      if (q.bounds != null &&
          (m.lat < q.bounds!.minLat ||
              m.lat > q.bounds!.maxLat ||
              m.lng < q.bounds!.minLng ||
              m.lng > q.bounds!.maxLng)) {
        continue;
      }
      if (q.priceMaxRupees != null && m.dayRupees > q.priceMaxRupees!) continue;
      if (q.priceMinRupees != null && m.dayRupees < q.priceMinRupees!) continue;
      if (q.ratingMin != null && (m.rating ?? 0) < q.ratingMin!) continue;
      if (q.conditions.isNotEmpty && !q.conditions.contains(m.condition)) {
        continue;
      }
      if (q.deliveryOnly && !m.delivery) continue;
      if (q.verifiedOnly && !m.verified) continue;
      if (q.operator == OperatorFilter.machineOnly && !m.dry) continue;
      hits.add((m, dist));
    }
    var sort =
        q.sort ??
        (text.isNotEmpty
            ? DiscoverSort.relevance
            : origin != null
            ? DiscoverSort.distance
            : DiscoverSort.newest);
    if (sort == DiscoverSort.distance && origin == null) {
      sort = DiscoverSort.newest;
    }
    hits.sort(switch (sort) {
      DiscoverSort.priceAsc => (a, b) => a.$1.dayRupees.compareTo(
        b.$1.dayRupees,
      ),
      DiscoverSort.priceDesc => (a, b) => b.$1.dayRupees.compareTo(
        a.$1.dayRupees,
      ),
      DiscoverSort.distance => (a, b) => a.$2!.compareTo(b.$2!),
      DiscoverSort.rating => (a, b) => (b.$1.rating ?? 0).compareTo(
        a.$1.rating ?? 0,
      ),
      _ => (a, b) => b.$1.created.compareTo(a.$1.created),
    });
    final total = hits.length;
    final start = (q.page - 1) * q.pageSize;
    hits = hits.skip(start).take(q.pageSize).toList();
    if (text.isNotEmpty && q.page == 1 && q.bounds == null) {
      final label = [
        if (q.text.isNotEmpty) '"${q.text}"',
        if (origin != null) 'near ${origin.label}',
      ].join(' · ');
      historyList.removeWhere((h) => h.label == label);
      historyList.insert(
        0,
        HistoryEntry(
          id: 'h-${++_ids}',
          label: label,
          params: {'q': q.text, 'pincode': ?place?.pincode},
          resultCount: total,
        ),
      );
    }
    return DiscoverResults(
      items: [for (final (m, d) in hits) _item(m, d)],
      page: q.page,
      pages: (total + q.pageSize - 1) ~/ q.pageSize,
      total: total,
      sort: sort,
      origin: origin,
    );
  }

  @override
  Future<List<Suggestion>> suggest(String text) async {
    final t = text.toLowerCase();
    return [
      if ('excavator'.startsWith(t))
        const Suggestion(
          kind: 'category',
          label: 'Excavator',
          value: 'excavator',
        ),
      for (final m in machines)
        if (m.make.toLowerCase().startsWith(t))
          Suggestion(
            kind: 'make',
            label: m.make,
            value: m.make,
            detail: '1 listed',
          ),
      for (final e in pincodes.entries)
        if (e.key.startsWith(t))
          Suggestion(
            kind: 'pincode',
            label: '${e.key} · ${e.value.$1}',
            value: e.key,
          ),
    ];
  }

  @override
  Future<List<PlaceOption>> places(String text) async =>
      [
            const PlaceOption(
              kind: 'city',
              label: 'Pune',
              value: 'city-pune',
              lat: 18.52,
              lng: 73.85,
            ),
            const PlaceOption(
              kind: 'city',
              label: 'Mumbai',
              value: 'city-mumbai',
              lat: 19.07,
              lng: 72.87,
            ),
            for (final e in pincodes.entries)
              if (text.isNotEmpty && e.key.startsWith(text))
                PlaceOption(
                  kind: 'pincode',
                  label: '${e.key} · ${e.value.$1}',
                  value: e.key,
                  lat: e.value.$2,
                  lng: e.value.$3,
                ),
          ]
          .where(
            (p) =>
                text.isEmpty ||
                p.label.toLowerCase().contains(text.toLowerCase()) ||
                p.value.startsWith(text),
          )
          .toList();

  @override
  Future<LocationDetails> lookupPincode(String pincode) async {
    final value = pincodes[pincode];
    if (value == null) {
      throw const ApiException(
        kind: ApiErrorKind.notFound,
        code: 'NOT_FOUND',
        message: 'This PIN code could not be verified',
        statusCode: 404,
      );
    }
    return LocationDetails(
      pincode: pincode,
      area: value.$1.split(',').first,
      city: value.$1.split(',').last.trim(),
      district: value.$1.split(',').last.trim(),
      state: 'Maharashtra',
      stateCode: '27',
      lat: value.$2,
      lng: value.$3,
      source: 'test',
    );
  }

  @override
  Future<LocationDetails> reverseGeocode(double lat, double lng) async =>
      LocationDetails(
        pincode: '411057',
        area: 'Hinjewadi',
        addressLine: 'Phase 2',
        city: 'Pune',
        district: 'Pune',
        state: 'Maharashtra',
        stateCode: '27',
        lat: lat,
        lng: lng,
        source: 'test',
      );

  @override
  Future<String> mapProvider() async => 'builtin';

  @override
  Future<Comparison> compare(List<String> ids) async {
    final ls = <Listing>[];
    for (final id in ids) {
      final r = _listings.listings[id];
      if (r == null || r.status != ListingStatus.active) {
        throw const ApiException(
          kind: ApiErrorKind.notFound,
          code: 'NOT_FOUND',
          message: 'This listing is not available',
          statusCode: 404,
        );
      }
      ls.add(r.full(withDocs: false));
    }
    return Comparison(
      listings: ls,
      specKeys: {
        for (final l in ls)
          for (final s in l.specs) s.key,
      }.toList(),
    );
  }

  @override
  Future<List<DiscoverItem>> saved() async => [
    for (final m in machines)
      if (savedSet.contains(m.id)) _item(m, null),
  ];
  @override
  Future<Set<String>> savedIds() async => {...savedSet};
  @override
  Future<void> save(String id) async {
    if (failSave) {
      throw const ApiException(
        kind: ApiErrorKind.server,
        code: 'X',
        message: 'Could not save',
        statusCode: 500,
      );
    }
    savedSet.add(id);
  }

  @override
  Future<void> unsave(String id) async => savedSet.remove(id);

  @override
  Future<List<HistoryEntry>> history() async => [...historyList];
  @override
  Future<void> forget(String id) async =>
      historyList.removeWhere((h) => h.id == id);
  @override
  Future<void> clearHistory() async => historyList.clear();
}
