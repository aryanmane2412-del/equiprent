import 'package:flutter/foundation.dart';

import '../../listings/data/listing_models.dart';

Json _m(Object? v) => v is Map ? v.cast<String, dynamic>() : const {};
String? _s(Object? v) => v is String && v.isNotEmpty ? v : null;
int _i(Object? v) => (v as num?)?.toInt() ?? 0;
double? _d(Object? v) => (v as num?)?.toDouble();

/// Sort orders offered by the discovery API.
enum DiscoverSort {
  relevance('relevance', 'Best match'),
  newest('newest', 'Recently listed'),
  priceAsc('price_asc', 'Lowest price'),
  priceDesc('price_desc', 'Highest price'),
  distance('distance', 'Nearest'),
  rating('rating', 'Highest rated');

  const DiscoverSort(this.wire, this.label);
  final String wire;
  final String label;
  static DiscoverSort? parse(Object? v) =>
      DiscoverSort.values.where((s) => s.wire == v).firstOrNull;
}

/// Operator availability filter.
enum OperatorFilter {
  any(null, 'Any'),
  withOperator('with', 'With operator'),
  machineOnly('without', 'Machine only');

  const OperatorFilter(this.wire, this.label);
  final String? wire;
  final String label;
  static OperatorFilter parse(Object? v) =>
      OperatorFilter.values.firstWhere((o) => o.wire == v, orElse: () => any);
}

/// Where to search: a city, a PIN code or a point (device location).
@immutable
class SearchPlace {
  const SearchPlace.city(this.cityId, this.label, {this.lat, this.lng})
    : pincode = null,
      isPoint = false;
  const SearchPlace.pincode(this.pincode, this.label, {this.lat, this.lng})
    : cityId = null,
      isPoint = false;
  const SearchPlace.point(
    double this.lat,
    double this.lng, {
    this.label = 'Your location',
  }) : cityId = null,
       pincode = null,
       isPoint = true;

  final String? cityId;
  final String? pincode;
  final bool isPoint;
  final String label;
  final double? lat;
  final double? lng;

  @override
  bool operator ==(Object other) =>
      other is SearchPlace &&
      other.cityId == cityId &&
      other.pincode == pincode &&
      other.isPoint == isPoint &&
      other.lat == lat &&
      other.lng == lng;
  @override
  int get hashCode => Object.hash(cityId, pincode, isPoint, lat, lng);
}

/// Map viewport bounds (map search).
@immutable
class MapBounds {
  const MapBounds(this.minLat, this.minLng, this.maxLat, this.maxLng);
  final double minLat, minLng, maxLat, maxLng;
  @override
  bool operator ==(Object other) =>
      other is MapBounds &&
      other.minLat == minLat &&
      other.minLng == minLng &&
      other.maxLat == maxLat &&
      other.maxLng == maxLng;
  @override
  int get hashCode => Object.hash(minLat, minLng, maxLat, maxLng);
}

/// A complete discovery query. Immutable and comparable so it can key a
/// provider family (each distinct query is fetched once).
@immutable
class DiscoverQuery {
  const DiscoverQuery({
    this.text = '',
    this.categories = const [],
    this.make,
    this.model,
    this.place,
    this.radiusKm,
    this.bounds,
    this.unit = 'day',
    this.priceMinRupees,
    this.priceMaxRupees,
    this.ratingMin,
    this.availableFrom,
    this.availableTo,
    this.conditions = const [],
    this.deliveryOnly = false,
    this.operator = OperatorFilter.any,
    this.verifiedOnly = false,
    this.sort,
    this.page = 1,
    this.pageSize = 20,
  });

  final String text;
  final List<String> categories;
  final String? make;
  final String? model;
  final SearchPlace? place;
  final double? radiusKm;
  final MapBounds? bounds;
  final String unit;
  final int? priceMinRupees;
  final int? priceMaxRupees;
  final double? ratingMin;
  final DateTime? availableFrom;
  final DateTime? availableTo;
  final List<MachineCondition> conditions;
  final bool deliveryOnly;
  final OperatorFilter operator;
  final bool verifiedOnly;
  final DiscoverSort? sort;
  final int page;
  final int pageSize;

  /// Filters shown on the filter button badge (not text, place or sort).
  int get activeFilterCount =>
      (categories.isNotEmpty ? 1 : 0) +
      (make != null || model != null ? 1 : 0) +
      (priceMinRupees != null || priceMaxRupees != null ? 1 : 0) +
      (radiusKm != null ? 1 : 0) +
      (ratingMin != null ? 1 : 0) +
      (availableFrom != null ? 1 : 0) +
      (conditions.isNotEmpty ? 1 : 0) +
      (deliveryOnly ? 1 : 0) +
      (operator != OperatorFilter.any ? 1 : 0) +
      (verifiedOnly ? 1 : 0);

  static String _date(DateTime d) =>
      '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

  Map<String, dynamic> toQuery() => {
    if (text.trim().isNotEmpty) 'q': text.trim(),
    if (categories.isNotEmpty) 'category': categories.join(','),
    'make': ?make,
    'model': ?model,
    if (place?.cityId != null) 'city': place!.cityId,
    if (place?.pincode != null) 'pincode': place!.pincode,
    if (place?.isPoint ?? false) ...{'lat': place!.lat, 'lng': place!.lng},
    if (radiusKm != null && place != null && place!.cityId == null)
      'radius_km': radiusKm,
    if (bounds != null) ...{
      'min_lat': bounds!.minLat,
      'min_lng': bounds!.minLng,
      'max_lat': bounds!.maxLat,
      'max_lng': bounds!.maxLng,
    },
    if (unit != 'day') 'unit': unit,
    if (priceMinRupees != null) 'price_min': priceMinRupees! * 100,
    if (priceMaxRupees != null) 'price_max': priceMaxRupees! * 100,
    'rating_min': ?ratingMin,
    if (availableFrom != null) 'available_from': _date(availableFrom!),
    if (availableTo != null) 'available_to': _date(availableTo!),
    if (conditions.isNotEmpty)
      'condition': conditions.map((c) => c.wire).join(','),
    if (deliveryOnly) 'delivery': true,
    'operator': ?operator.wire,
    if (verifiedOnly) 'verified_only': true,
    'sort': ?sort?.wire,
    if (page > 1) 'page': page,
    if (pageSize != 20) 'page_size': pageSize,
  };

  /// Rebuilds a query from saved search-history parameters.
  factory DiscoverQuery.fromParams(Json p, {String? placeLabel}) {
    SearchPlace? place;
    if (p['pincode'] != null) {
      place = SearchPlace.pincode(
        '${p['pincode']}',
        placeLabel ?? 'PIN ${p['pincode']}',
      );
    } else if (p['city'] != null) {
      place = SearchPlace.city('${p['city']}', placeLabel ?? 'City');
    } else if (p['lat'] != null) {
      place = SearchPlace.point(_d(p['lat'])!, _d(p['lng'])!);
    }
    List<String> list(Object? v) => v == null
        ? const []
        : '$v'.split(',').where((s) => s.isNotEmpty).toList();
    DateTime? date(Object? v) => v == null ? null : DateTime.tryParse('$v');
    return DiscoverQuery(
      text: '${p['q'] ?? ''}',
      categories: list(p['category']),
      make: _s(p['make']),
      model: _s(p['model']),
      place: place,
      radiusKm: _d(p['radius_km']),
      unit: _s(p['unit']) ?? 'day',
      priceMinRupees: p['price_min'] == null ? null : _i(p['price_min']) ~/ 100,
      priceMaxRupees: p['price_max'] == null ? null : _i(p['price_max']) ~/ 100,
      ratingMin: _d(p['rating_min']),
      availableFrom: date(p['available_from']),
      availableTo: date(p['available_to']),
      conditions: [
        for (final c in list(p['condition'])) MachineCondition.parse(c),
      ],
      deliveryOnly: p['delivery'] == true,
      operator: OperatorFilter.parse(p['operator']),
      verifiedOnly: p['verified_only'] == true,
      sort: DiscoverSort.parse(p['sort']),
    );
  }

  static const _keep = Object();

  /// Copies the query. Any change other than page resets to page one.
  DiscoverQuery copyWith({
    String? text,
    List<String>? categories,
    Object? make = _keep,
    Object? model = _keep,
    Object? place = _keep,
    Object? radiusKm = _keep,
    Object? bounds = _keep,
    String? unit,
    Object? priceMinRupees = _keep,
    Object? priceMaxRupees = _keep,
    Object? ratingMin = _keep,
    Object? availableFrom = _keep,
    Object? availableTo = _keep,
    List<MachineCondition>? conditions,
    bool? deliveryOnly,
    OperatorFilter? operator,
    bool? verifiedOnly,
    Object? sort = _keep,
    int? page,
    int? pageSize,
  }) => DiscoverQuery(
    text: text ?? this.text,
    categories: categories ?? this.categories,
    make: identical(make, _keep) ? this.make : make as String?,
    model: identical(model, _keep) ? this.model : model as String?,
    place: identical(place, _keep) ? this.place : place as SearchPlace?,
    radiusKm: identical(radiusKm, _keep) ? this.radiusKm : radiusKm as double?,
    bounds: identical(bounds, _keep) ? this.bounds : bounds as MapBounds?,
    unit: unit ?? this.unit,
    priceMinRupees: identical(priceMinRupees, _keep)
        ? this.priceMinRupees
        : priceMinRupees as int?,
    priceMaxRupees: identical(priceMaxRupees, _keep)
        ? this.priceMaxRupees
        : priceMaxRupees as int?,
    ratingMin: identical(ratingMin, _keep)
        ? this.ratingMin
        : ratingMin as double?,
    availableFrom: identical(availableFrom, _keep)
        ? this.availableFrom
        : availableFrom as DateTime?,
    availableTo: identical(availableTo, _keep)
        ? this.availableTo
        : availableTo as DateTime?,
    conditions: conditions ?? this.conditions,
    deliveryOnly: deliveryOnly ?? this.deliveryOnly,
    operator: operator ?? this.operator,
    verifiedOnly: verifiedOnly ?? this.verifiedOnly,
    sort: identical(sort, _keep) ? this.sort : sort as DiscoverSort?,
    page: page ?? 1,
    pageSize: pageSize ?? this.pageSize,
  );

  /// The same search with every filter cleared (text and place kept).
  DiscoverQuery clearFilters() =>
      DiscoverQuery(text: text, place: place, sort: sort);

  @override
  bool operator ==(Object other) =>
      other is DiscoverQuery &&
      other.text == text &&
      listEquals(other.categories, categories) &&
      other.make == make &&
      other.model == model &&
      other.place == place &&
      other.radiusKm == radiusKm &&
      other.bounds == bounds &&
      other.unit == unit &&
      other.priceMinRupees == priceMinRupees &&
      other.priceMaxRupees == priceMaxRupees &&
      other.ratingMin == ratingMin &&
      other.availableFrom == availableFrom &&
      other.availableTo == availableTo &&
      listEquals(other.conditions, conditions) &&
      other.deliveryOnly == deliveryOnly &&
      other.operator == operator &&
      other.verifiedOnly == verifiedOnly &&
      other.sort == sort &&
      other.page == page &&
      other.pageSize == pageSize;

  @override
  int get hashCode => Object.hashAll([
    text,
    Object.hashAll(categories),
    make,
    model,
    place,
    radiusKm,
    bounds,
    unit,
    priceMinRupees,
    priceMaxRupees,
    ratingMin,
    availableFrom,
    availableTo,
    Object.hashAll(conditions),
    deliveryOnly,
    operator,
    verifiedOnly,
    sort,
    page,
    pageSize,
  ]);
}

/// One search result. Locations are approximate (about 1 km).
@immutable
class DiscoverItem {
  const DiscoverItem({
    required this.card,
    this.lat,
    this.lng,
    this.distanceKm,
    this.deliversToYou,
    this.wetHire = true,
    this.dryHire = false,
    this.available = true,
  });

  factory DiscoverItem.fromJson(Json j) {
    final loc = _m(j['approx_location']);
    return DiscoverItem(
      card: ListingCard.fromJson({...j, 'status': 'active'}),
      lat: _d(loc['lat']),
      lng: _d(loc['lng']),
      distanceKm: _d(j['distance_km']),
      deliversToYou: j['delivers_to_you'] as bool?,
      wetHire: j['wet_hire'] != false,
      dryHire: j['dry_hire'] == true,
      available: j['available'] != false,
    );
  }

  final ListingCard card;
  final double? lat;
  final double? lng;
  final double? distanceKm;
  final bool? deliversToYou;
  final bool wetHire;
  final bool dryHire;
  final bool available;

  String get id => card.id;
}

@immutable
class SearchOrigin {
  const SearchOrigin({
    required this.source,
    required this.label,
    required this.lat,
    required this.lng,
  });
  static SearchOrigin? fromJson(Object? v) {
    final j = _m(v);
    if (j.isEmpty) return null;
    return SearchOrigin(
      source: '${j['source']}',
      label: '${j['label']}',
      lat: _d(j['lat']) ?? 0,
      lng: _d(j['lng']) ?? 0,
    );
  }

  final String source;
  final String label;
  final double lat;
  final double lng;
}

@immutable
class DiscoverResults {
  const DiscoverResults({
    required this.items,
    this.page = 1,
    this.pages = 0,
    this.total = 0,
    this.sort,
    this.origin,
    this.fuzzy = false,
    this.tookMs = 0,
  });
  factory DiscoverResults.fromJson(Json j) => DiscoverResults(
    items: [
      for (final e in (j['data'] as List? ?? const []))
        DiscoverItem.fromJson(_m(e)),
    ],
    page: _i(j['page']),
    pages: _i(j['pages']),
    total: _i(j['total']),
    sort: DiscoverSort.parse(j['sort']),
    origin: SearchOrigin.fromJson(j['origin']),
    fuzzy: j['fuzzy'] == true,
    tookMs: _i(j['took_ms']),
  );

  final List<DiscoverItem> items;
  final int page;
  final int pages;
  final int total;
  final DiscoverSort? sort;
  final SearchOrigin? origin;
  final bool fuzzy;
  final int tookMs;
}

/// A type-ahead suggestion.
@immutable
class Suggestion {
  const Suggestion({
    required this.kind,
    required this.label,
    required this.value,
    this.detail,
  });
  factory Suggestion.fromJson(Json j) => Suggestion(
    kind: '${j['kind']}',
    label: '${j['label']}',
    value: '${j['value']}',
    detail: _s(j['detail']),
  );
  final String kind; // category, make, model, city, pincode
  final String label;
  final String value;
  final String? detail;
}

/// A city or PIN code in the location picker.
@immutable
class PlaceOption {
  const PlaceOption({
    required this.kind,
    required this.label,
    required this.value,
    required this.lat,
    required this.lng,
  });
  factory PlaceOption.fromJson(Json j) => PlaceOption(
    kind: '${j['kind']}',
    label: '${j['label']}',
    value: '${j['value']}',
    lat: _d(j['lat']) ?? 0,
    lng: _d(j['lng']) ?? 0,
  );
  final String kind;
  final String label;
  final String value;
  final double lat;
  final double lng;

  SearchPlace toPlace() => kind == 'pincode'
      ? SearchPlace.pincode(value, label, lat: lat, lng: lng)
      : SearchPlace.city(value, label, lat: lat, lng: lng);
}

@immutable
class LocationDetails {
  const LocationDetails({
    this.pincode = '',
    this.area = '',
    this.addressLine = '',
    this.formattedAddress = '',
    this.city = '',
    this.district = '',
    this.state = '',
    this.stateCode = '',
    required this.lat,
    required this.lng,
    this.source = '',
  });
  factory LocationDetails.fromJson(Json j) => LocationDetails(
    pincode: '${j['pincode'] ?? ''}',
    area: '${j['area'] ?? ''}',
    addressLine: '${j['address_line'] ?? ''}',
    formattedAddress: '${j['formatted_address'] ?? ''}',
    city: '${j['city'] ?? ''}',
    district: '${j['district'] ?? ''}',
    state: '${j['state'] ?? ''}',
    stateCode: '${j['state_code'] ?? ''}',
    lat: _d(j['lat']) ?? 0,
    lng: _d(j['lng']) ?? 0,
    source: '${j['source'] ?? ''}',
  );
  final String pincode,
      area,
      addressLine,
      formattedAddress,
      city,
      district,
      state,
      stateCode,
      source;
  final double lat, lng;
}

@immutable
class HistoryEntry {
  const HistoryEntry({
    required this.id,
    required this.label,
    required this.params,
    this.resultCount = 0,
    this.searchedAt,
  });
  factory HistoryEntry.fromJson(Json j) => HistoryEntry(
    id: '${j['id']}',
    label: '${j['label']}',
    params: _m(j['params']),
    resultCount: _i(j['result_count']),
    searchedAt: DateTime.tryParse('${j['searched_at']}'),
  );
  final String id;
  final String label;
  final Json params;
  final int resultCount;
  final DateTime? searchedAt;

  /// The PIN code / city label stored in the history label ("near …").
  String? get placeLabel {
    final i = label.indexOf('near ');
    return i < 0 ? null : label.substring(i + 5);
  }

  DiscoverQuery toQuery() =>
      DiscoverQuery.fromParams(params, placeLabel: placeLabel);
}

/// Listings side by side.
@immutable
class Comparison {
  const Comparison({required this.listings, required this.specKeys});
  factory Comparison.fromJson(Json j) => Comparison(
    listings: [
      for (final e in (j['data'] as List? ?? const [])) Listing.fromJson(_m(e)),
    ],
    specKeys: [for (final k in (j['spec_keys'] as List? ?? const [])) '$k'],
  );
  final List<Listing> listings;
  final List<String> specKeys;
}
