import 'package:flutter/widgets.dart';

import '../../../core/theme/app_icons.dart';

typedef Json = Map<String, dynamic>;

Json _m(Object? v) => v is Map ? v.cast<String, dynamic>() : const {};
List<Json> _l(Object? v) => [for (final e in (v as List?) ?? const []) _m(e)];
String? _s(Object? v) => v is String && v.isNotEmpty ? v : null;
int _i(Object? v) => (v as num?)?.toInt() ?? 0;
double? _d(Object? v) => (v as num?)?.toDouble();

/// Listing lifecycle states.
enum ListingStatus {
  draft('Draft'),
  pendingReview('In review'),
  active('Live'),
  paused('Paused'),
  rejected('Needs changes'),
  suspended('Suspended'),
  removed('Removed'),
  retired('Deleted');

  const ListingStatus(this.label);
  final String label;

  static ListingStatus parse(Object? v) => switch (v) {
        'pending_review' => pendingReview,
        'active' => active,
        'paused' => paused,
        'rejected' => rejected,
        'suspended' => suspended,
        'removed' => removed,
        'retired' => retired,
        _ => draft,
      };

  String get wire => this == pendingReview ? 'pending_review' : name;
}

/// Declared machine condition.
enum MachineCondition {
  excellent('Excellent'),
  good('Good'),
  fair('Fair'),
  needsRepair('Needs repair');

  const MachineCondition(this.label);
  final String label;
  String get wire => this == needsRepair ? 'needs_repair' : name;
  static MachineCondition parse(Object? v) =>
      MachineCondition.values.firstWhere((c) => c.wire == v, orElse: () => MachineCondition.good);
}

/// Icon for a category slug (Lucide stand-ins, see AppIcons).
IconData categoryIcon(String slug) => switch (slug) {
      'backhoe-loader' || 'wheel-loader' => AppIcons.backhoe,
      'excavator' => AppIcons.excavator,
      'bulldozer' => AppIcons.dozer,
      'crane' => AppIcons.crane,
      'dump-truck' || 'concrete-mixer' => AppIcons.tipper,
      'road-roller' => AppIcons.roller,
      'forklift' => AppIcons.telehandler,
      'tractor' => AppIcons.backhoe,
      _ => AppIcons.generator,
    };

const priceUnits = {'hour': 'hour', 'shift': 'shift', 'day': 'day', 'week': 'week', 'month': 'month'};

String rupees(int paise) {
  final r = paise ~/ 100;
  final s = r.toString();
  if (s.length <= 3) return '₹$s';
  final last3 = s.substring(s.length - 3);
  var rest = s.substring(0, s.length - 3);
  final parts = <String>[];
  while (rest.length > 2) {
    parts.insert(0, rest.substring(rest.length - 2));
    rest = rest.substring(0, rest.length - 2);
  }
  if (rest.isNotEmpty) parts.insert(0, rest);
  return '₹${parts.join(',')},$last3';
}

@immutable
class ListingCategory {
  const ListingCategory({
    required this.id,
    required this.slug,
    required this.name,
    this.listingCount = 0,
    this.specSchema = const {},
    this.isActive = true,
    this.nameHi,
    this.sortOrder = 0,
    this.modelCount = 0,
  });

  factory ListingCategory.fromJson(Json j) => ListingCategory(
        id: '${j['id']}',
        slug: '${j['slug']}',
        name: '${j['name_en'] ?? j['name']}',
        listingCount: _i(j['listing_count']),
        specSchema: {for (final e in _m(j['spec_schema']).entries) e.key: '${e.value}'},
        isActive: j['is_active'] != false,
        nameHi: _s(j['name_hi']),
        sortOrder: _i(j['sort_order']),
        modelCount: _i(j['model_count']),
      );

  final String id;
  final String slug;
  final String name;
  final int listingCount;
  final Map<String, String> specSchema; // key -> "number" | "text"
  final bool isActive;
  final String? nameHi;
  final int sortOrder;
  final int modelCount;

  IconData get icon => categoryIcon(slug);
}

@immutable
class City {
  const City({required this.id, required this.name, this.lat = 0, this.lng = 0});
  factory City.fromJson(Json j) =>
      City(id: '${j['id']}', name: '${j['name']}', lat: _d(_m(j['center'])['lat']) ?? 0, lng: _d(_m(j['center'])['lng']) ?? 0);
  final String id;
  final String name;
  final double lat;
  final double lng;
}

@immutable
class CatalogModel {
  const CatalogModel({required this.id, required this.name, required this.makeName, required this.categorySlug});
  factory CatalogModel.fromJson(Json j) =>
      CatalogModel(id: '${j['id']}', name: '${j['name']}', makeName: '${j['make_name']}', categorySlug: '${j['category_slug']}');
  final String id;
  final String name;
  final String makeName;
  final String categorySlug;
}

/// A listing card (browse, my listings, moderation queue).
@immutable
class ListingCard {
  const ListingCard({
    required this.id,
    required this.title,
    required this.status,
    required this.categorySlug,
    required this.categoryName,
    required this.makeName,
    required this.modelName,
    required this.condition,
    required this.ownerName,
    this.ownerVerified = false,
    this.version = 1,
    this.year,
    this.locationLabel,
    this.cityName,
    this.delivery = true,
    this.coverUrl,
    this.photoCount = 0,
    this.pricePaise = 0,
    this.priceUnit,
    this.rating,
    this.ratingCount = 0,
    this.moderationNote,
  });

  factory ListingCard.fromJson(Json j) => ListingCard(
        id: '${j['id']}',
        title: '${j['title']}',
        status: ListingStatus.parse(j['status']),
        version: _i(j['version']),
        categorySlug: '${j['category_slug'] ?? ''}',
        categoryName: '${j['category_name'] ?? ''}',
        makeName: '${j['make_name'] ?? ''}',
        modelName: '${j['model_name'] ?? ''}',
        year: (j['manufacture_year'] as num?)?.toInt(),
        condition: MachineCondition.parse(j['condition']),
        locationLabel: _s(j['location_label']),
        cityName: _s(j['city_name']),
        delivery: j['delivery_available'] != false,
        ownerName: '${j['owner_name'] ?? ''}',
        ownerVerified: j['owner_verified'] == true,
        coverUrl: _s(j['cover_url']),
        photoCount: _i(j['photo_count']),
        pricePaise: _i(j['price_paise']),
        priceUnit: _s(j['price_unit']),
        rating: _d(j['rating_avg']),
        ratingCount: _i(j['rating_count']),
        moderationNote: _s(j['moderation_note']),
      );

  final String id;
  final String title;
  final ListingStatus status;
  final int version;
  final String categorySlug;
  final String categoryName;
  final String makeName;
  final String modelName;
  final int? year;
  final MachineCondition condition;
  final String? locationLabel;
  final String? cityName;
  final bool delivery;
  final String ownerName;
  final bool ownerVerified;
  final String? coverUrl;
  final int photoCount;
  final int pricePaise;
  final String? priceUnit;
  final double? rating;
  final int ratingCount;
  final String? moderationNote;

  String get machine => [makeName, modelName, if (year != null) '($year)'].where((s) => s.isNotEmpty).join(' ');
  String get place => locationLabel ?? cityName ?? '';
  String? get priceLabel => pricePaise > 0 ? '${rupees(pricePaise)}/${priceUnit ?? 'day'}' : null;
}

@immutable
class PricingPlan {
  const PricingPlan({required this.unit, required this.withOperator, required this.ratePaise, this.operatorBataPerDayPaise = 0});
  factory PricingPlan.fromJson(Json j) => PricingPlan(
        unit: '${j['unit']}',
        withOperator: j['with_operator'] == true,
        ratePaise: _i(j['rate_paise']),
        operatorBataPerDayPaise: _i(j['operator_bata_per_day_paise']),
      );
  final String unit;
  final bool withOperator;
  final int ratePaise;
  final int operatorBataPerDayPaise;
  Json toJson() => {
        'unit': unit,
        'with_operator': withOperator,
        'rate_paise': ratePaise,
        if (withOperator && operatorBataPerDayPaise > 0) 'operator_bata_per_day_paise': operatorBataPerDayPaise,
      };
}

@immutable
class Mobilisation {
  const Mobilisation({this.fixedPaise = 0, this.perKmPaise = 0, this.freeKm = 0, this.needsTrailer = false});
  static Mobilisation? fromJson(Object? v) {
    final j = _m(v);
    if (j.isEmpty) return null;
    return Mobilisation(fixedPaise: _i(j['fixed_paise']), perKmPaise: _i(j['per_km_paise']), freeKm: _i(j['free_km']), needsTrailer: j['needs_trailer'] == true);
  }

  final int fixedPaise;
  final int perKmPaise;
  final int freeKm;
  final bool needsTrailer;
  Json toJson() => {'fixed_paise': fixedPaise, 'per_km_paise': perKmPaise, 'free_km': freeKm, 'needs_trailer': needsTrailer};
}

@immutable
class SpecValue {
  const SpecValue({required this.key, this.number, this.text, this.unit});
  factory SpecValue.fromJson(Json j) => SpecValue(key: '${j['key']}', number: _d(j['value_numeric']), text: _s(j['value_text']), unit: _s(j['unit']));
  final String key;
  final double? number;
  final String? text;
  final String? unit;

  String get label {
    final words = key.split('_');
    const units = {'kg', 'm', 'm3', 'hp', 't', 'mm', 'kva'};
    if (units.contains(words.last) && words.length > 1) words.removeLast();
    final s = words.join(' ');
    return s.isEmpty ? key : s[0].toUpperCase() + s.substring(1);
  }

  String get value {
    if (text != null) return text!;
    final n = number ?? 0;
    final num = n == n.roundToDouble() ? n.toInt().toString() : n.toString();
    final u = unit ?? _unitFromKey(key);
    return u == null ? num : '$num $u';
  }

  static String? _unitFromKey(String key) => switch (key.split('_').last) {
        'kg' => 'kg',
        'm' => 'm',
        'm3' => 'm³',
        'hp' => 'HP',
        't' => 't',
        'mm' => 'mm',
        _ => null,
      };

  Json toJson() => {'key': key, 'value_numeric': ?number, 'value_text': ?text, 'unit': ?unit};
}

@immutable
class ListingPhoto {
  const ListingPhoto({required this.id, required this.url, this.isCover = false, this.angle});
  factory ListingPhoto.fromJson(Json j) => ListingPhoto(id: '${j['id']}', url: '${j['url'] ?? ''}', isCover: j['is_cover'] == true, angle: _s(j['angle']));
  final String id;
  final String url;
  final bool isCover;
  final String? angle;
}

const equipmentDocTypes = {
  'rc': 'Registration certificate (RC)',
  'insurance': 'Insurance',
  'fitness': 'Fitness certificate',
  'puc': 'Pollution (PUC)',
  'invoice': 'Purchase invoice',
  'permit': 'Permit',
  'load_test': 'Load test certificate',
  'other': 'Other document',
};

@immutable
class ListingDocument {
  const ListingDocument({required this.id, required this.docType, required this.status, this.number, this.validUntil, this.downloadUrl});
  factory ListingDocument.fromJson(Json j) => ListingDocument(
        id: '${j['id']}',
        docType: '${j['doc_type']}',
        status: '${j['status']}',
        number: _s(j['document_number']),
        validUntil: _s(j['valid_until']),
        downloadUrl: _s(_m(j['download'])['url']),
      );
  final String id;
  final String docType;
  final String status;
  final String? number;
  final String? validUntil;
  final String? downloadUrl;
  String get label => equipmentDocTypes[docType] ?? docType;
}

@immutable
class GeoPoint {
  const GeoPoint(this.lat, this.lng);
  final double lat;
  final double lng;
  Json toJson() => {'lat': lat, 'lng': lng};
}

/// Full listing (detail, owner manage, moderator review).
@immutable
class Listing {
  const Listing({
    required this.id,
    required this.title,
    required this.status,
    required this.version,
    required this.categorySlug,
    required this.categoryName,
    required this.makeName,
    required this.modelName,
    required this.modelId,
    required this.ownerName,
    required this.condition,
    required this.location,
    this.ownerOrgId = '',
    this.ownerVerified = false,
    this.description = '',
    this.year,
    this.registrationNo,
    this.hmr = 0,
    this.cityId,
    this.cityName,
    this.locationLabel,
    this.serviceRadiusKm = 50,
    this.delivery = true,
    this.wetHire = true,
    this.dryHire = false,
    this.instantBook = false,
    this.rating,
    this.ratingCount = 0,
    this.pricing = const [],
    this.mobilisation,
    this.specs = const [],
    this.photos = const [],
    this.documents = const [],
    this.moderationNote,
  });

  factory Listing.fromJson(Json j) => Listing(
        id: '${j['id']}',
        title: '${j['title']}',
        status: ListingStatus.parse(j['status']),
        version: _i(j['version']),
        categorySlug: '${j['category_slug'] ?? ''}',
        categoryName: '${j['category_name'] ?? ''}',
        makeName: '${j['make_name'] ?? ''}',
        modelName: '${j['model_name'] ?? ''}',
        modelId: '${j['model_id'] ?? ''}',
        ownerOrgId: '${j['owner_org_id'] ?? ''}',
        ownerName: '${j['owner_name'] ?? ''}',
        ownerVerified: j['owner_verified'] == true,
        description: '${j['description'] ?? ''}',
        year: (j['manufacture_year'] as num?)?.toInt(),
        registrationNo: _s(j['registration_no']),
        hmr: _d(j['current_hmr']) ?? 0,
        location: GeoPoint(_d(_m(j['base_location'])['lat']) ?? 0, _d(_m(j['base_location'])['lng']) ?? 0),
        cityId: _s(j['city_id']),
        cityName: _s(j['city_name']),
        locationLabel: _s(j['location_label']),
        serviceRadiusKm: _i(j['service_radius_km']),
        condition: MachineCondition.parse(j['condition']),
        delivery: j['delivery_available'] != false,
        wetHire: j['wet_hire'] != false,
        dryHire: j['dry_hire'] == true,
        instantBook: j['instant_book'] == true,
        rating: _d(j['rating_avg']),
        ratingCount: _i(j['rating_count']),
        pricing: [for (final p in _l(j['pricing'])) PricingPlan.fromJson(p)],
        mobilisation: Mobilisation.fromJson(j['mobilisation']),
        specs: [for (final s in _l(j['specs'])) SpecValue.fromJson(s)],
        photos: [for (final p in _l(j['photos'])) ListingPhoto.fromJson(p)],
        documents: [for (final d in _l(j['documents'])) ListingDocument.fromJson(d)],
        moderationNote: _s(j['moderation_note']),
      );

  final String id;
  final String title;
  final ListingStatus status;
  final int version;
  final String categorySlug;
  final String categoryName;
  final String makeName;
  final String modelName;
  final String modelId;
  final String ownerOrgId;
  final String ownerName;
  final bool ownerVerified;
  final String description;
  final int? year;
  final String? registrationNo;
  final double hmr;
  final GeoPoint location;
  final String? cityId;
  final String? cityName;
  final String? locationLabel;
  final int serviceRadiusKm;
  final MachineCondition condition;
  final bool delivery;
  final bool wetHire;
  final bool dryHire;
  final bool instantBook;
  final double? rating;
  final int ratingCount;
  final List<PricingPlan> pricing;
  final Mobilisation? mobilisation;
  final List<SpecValue> specs;
  final List<ListingPhoto> photos;
  final List<ListingDocument> documents;
  final String? moderationNote;

  String get machine => [makeName, modelName].where((s) => s.isNotEmpty).join(' ');
  String get place => locationLabel ?? cityName ?? '';
  bool get editable => status != ListingStatus.removed && status != ListingStatus.retired;
}

/// A busy period on the public calendar.
@immutable
class BusyRange {
  const BusyRange({required this.start, required this.end, required this.kind});
  factory BusyRange.fromJson(Json j) =>
      BusyRange(start: DateTime.parse('${j['start']}'), end: DateTime.parse('${j['end']}'), kind: '${j['kind']}');
  final DateTime start;
  final DateTime end;
  final String kind;
}

/// A period the owner took off the calendar (shown as booked to customers).
@immutable
class OwnerBlock {
  const OwnerBlock({required this.id, required this.start, required this.end, this.note});
  factory OwnerBlock.fromJson(Json j) => OwnerBlock(
        id: '${j['id']}',
        start: DateTime.parse('${j['start']}'),
        end: DateTime.parse('${j['end']}'),
        note: j['note'] as String?,
      );
  final String id;
  final DateTime start;
  final DateTime end;
  final String? note;
}

@immutable
class ListingPage {
  const ListingPage({required this.items, this.nextCursor});
  factory ListingPage.fromJson(Json j) =>
      ListingPage(items: [for (final e in _l(j['data'])) ListingCard.fromJson(e)], nextCursor: _s(j['next_cursor']));
  final List<ListingCard> items;
  final String? nextCursor;
}

/// Customer browse filters.
@immutable
class BrowseFilter {
  const BrowseFilter({this.category, this.query, this.condition, this.deliveryOnly = false, this.verifiedOnly = false});
  final String? category;
  final String? query;
  final MachineCondition? condition;
  final bool deliveryOnly;
  final bool verifiedOnly;

  BrowseFilter copyWith({String? category, String? query, MachineCondition? condition, bool? deliveryOnly, bool? verifiedOnly,
          bool clearCategory = false, bool clearCondition = false}) =>
      BrowseFilter(
        category: clearCategory ? null : category ?? this.category,
        query: query ?? this.query,
        condition: clearCondition ? null : condition ?? this.condition,
        deliveryOnly: deliveryOnly ?? this.deliveryOnly,
        verifiedOnly: verifiedOnly ?? this.verifiedOnly,
      );

  Map<String, dynamic> toQuery() => {
        'category': ?category,
        if (query != null && query!.trim().isNotEmpty) 'q': query!.trim(),
        'condition': ?condition?.wire,
        if (deliveryOnly) 'delivery_only': true,
        if (verifiedOnly) 'verified_only': true,
      };

  @override
  bool operator ==(Object other) =>
      other is BrowseFilter &&
      other.category == category &&
      other.query == query &&
      other.condition == condition &&
      other.deliveryOnly == deliveryOnly &&
      other.verifiedOnly == verifiedOnly;

  @override
  int get hashCode => Object.hash(category, query, condition, deliveryOnly, verifiedOnly);
}
