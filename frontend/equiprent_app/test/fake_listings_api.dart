import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/listings/data/listing_models.dart';
import 'package:equiprent_app/features/listings/data/listings_api.dart';
import 'package:equiprent_app/features/verification/data/verification_models.dart' show PickedFile;

ApiException _rule(String m, {String code = 'BUSINESS_RULE', int status = 422}) =>
    ApiException(kind: ApiErrorKind.conflict, code: code, message: m, statusCode: status);

/// In-memory listing backend that follows the server's rules closely
/// enough for UI tests: owner scoping by organisation, optimistic version
/// checks, submit requirements and the moderation state machine.
class FakeListingsApi implements ListingsApi {
  FakeListingsApi() {
    _seed();
  }

  final calls = <String>[];
  final orgsSeen = <String?>[];
  final uploads = <String, PickedFile>{};
  int _ids = 0;

  /// Organisation that owns the seeded owner listings (matches signedInAs).
  static const ownerOrg = 'org-owner';

  final _cats = const [
    ListingCategory(id: 'c-1', slug: 'backhoe-loader', name: 'JCB / Backhoe Loader', specSchema: {'operating_weight_kg': 'number', 'bucket_capacity_m3': 'number'}),
    ListingCategory(id: 'c-2', slug: 'excavator', name: 'Excavator', specSchema: {'operating_weight_kg': 'number'}),
    ListingCategory(id: 'c-3', slug: 'crane', name: 'Crane'),
  ];
  final _cities = const [City(id: 'city-pune', name: 'Pune', lat: 18.52, lng: 73.85), City(id: 'city-mumbai', name: 'Mumbai', lat: 19.07, lng: 72.87)];

  final listings = <String, _Row>{};

  void _seed() {
    _add(_Row('l-live', 'JCB 3DX backhoe loader', 'backhoe-loader', 'JCB', '3DX Super', 'org-other', 'Patil Earthmovers', ListingStatus.active, verified: true)
      ..pricing = [const PricingPlan(unit: 'day', withOperator: true, ratePaise: 1800000)]
      ..photos = [const ListingPhoto(id: 'p-1', url: 'https://example.invalid/1.jpg', isCover: true), const ListingPhoto(id: 'p-2', url: 'https://example.invalid/2.jpg')]
      ..specs = [const SpecValue(key: 'operating_weight_kg', number: 7660)]);
    _add(_Row('l-crane', 'Escorts 15 t pick-and-carry crane', 'crane', 'Escorts', 'Hydra 15', 'org-other', 'Shinde Cranes', ListingStatus.active)
      ..pricing = [const PricingPlan(unit: 'day', withOperator: true, ratePaise: 2200000)]);
    _add(_Row('l-draft', 'Tata Hitachi EX 200 excavator', 'excavator', 'Tata Hitachi', 'EX 200 LC', ownerOrg, 'Test Rentals', ListingStatus.draft));
    _add(_Row('l-review', 'JCB 3DX Xtra backhoe loader', 'backhoe-loader', 'JCB', '3DX Xtra', ownerOrg, 'Test Rentals', ListingStatus.pendingReview)
      ..pricing = [const PricingPlan(unit: 'day', withOperator: true, ratePaise: 1900000)]
      ..photos = [const ListingPhoto(id: 'p-9', url: 'https://example.invalid/9.jpg', isCover: true)]);
  }

  void _add(_Row r) => listings[r.id] = r;

  _Row _own(String? org, String id) {
    orgsSeen.add(org);
    final r = listings[id];
    if (r == null || r.org != org || r.status == ListingStatus.retired) {
      throw const ApiException(kind: ApiErrorKind.notFound, code: 'NOT_FOUND', message: 'Listing not found', statusCode: 404);
    }
    return r;
  }

  void _editable(_Row r) {
    if (r.status == ListingStatus.removed || r.status == ListingStatus.suspended) throw _rule('This listing cannot be changed', code: 'INVALID_STATE_TRANSITION', status: 409);
  }

  /// Material edits to a live listing send it back for review, like the server.
  void _touched(_Row r) {
    r.version++;
    if (r.status == ListingStatus.active || r.status == ListingStatus.rejected) r.status = r.status == ListingStatus.active ? ListingStatus.pendingReview : r.status;
  }

  @override
  Future<List<ListingCategory>> categories() async => _cats;
  @override
  Future<List<City>> cities() async => _cities;
  @override
  Future<List<CatalogModel>> models(String category) async => const [];

  @override
  Future<ListingPage> browse(BrowseFilter filter, {String? cursor}) async {
    calls.add('browse ${filter.category ?? '-'}');
    final q = filter.query?.toLowerCase();
    return ListingPage(items: [
      for (final r in listings.values)
        if (r.status == ListingStatus.active &&
            (filter.category == null || r.category == filter.category) &&
            (!filter.verifiedOnly || r.verified) &&
            (q == null || q.isEmpty || r.title.toLowerCase().contains(q)))
          r.card(),
    ]);
  }

  @override
  Future<Listing> listing(String id) async {
    final r = listings[id];
    if (r == null || r.status != ListingStatus.active) {
      throw const ApiException(kind: ApiErrorKind.notFound, code: 'NOT_FOUND', message: 'This listing is not available', statusCode: 404);
    }
    return r.full(withDocs: false);
  }

  @override
  Future<List<BusyRange>> availability(String id, DateTime from, DateTime to) async => [
        BusyRange(start: from.add(const Duration(days: 2)), end: from.add(const Duration(days: 4)), kind: 'booked'),
      ];

  final blocks = <OwnerBlock>[];
  @override
  Future<List<OwnerBlock>> ownerBlocks(String? org, String id, DateTime from, DateTime to) async => List.of(blocks);
  @override
  Future<void> addBlock(String? org, String id, DateTime start, DateTime end, {String? note}) async {
    blocks.add(OwnerBlock(id: 'blk-${blocks.length + 1}', start: start, end: end, note: note));
  }

  @override
  Future<void> removeBlock(String? org, String id, String blockId) async => blocks.removeWhere((b) => b.id == blockId);

  @override
  Future<ListingPage> myListings(String? org, {ListingStatus? status, String? cursor}) async {
    orgsSeen.add(org);
    return ListingPage(items: [
      for (final r in listings.values)
        if (r.org == org && r.status != ListingStatus.retired && (status == null || r.status == status)) r.card(),
    ]);
  }

  @override
  Future<Listing> myListing(String? org, String id) async => _own(org, id).full();

  @override
  Future<Listing> create(String? org, Json body) async {
    calls.add('create');
    orgsSeen.add(org);
    if (org == null) throw const ApiException(kind: ApiErrorKind.forbidden, code: 'FORBIDDEN', message: 'Owner organisation required', statusCode: 403);
    final cat = _cats.firstWhere((c) => c.slug == body['category']);
    final r = _Row('l-new-${++_ids}', '${body['title']}', cat.slug, '${body['manufacturer']}', '${body['model']}', org, 'Test Rentals', ListingStatus.draft)
      ..body = body;
    _add(r);
    return r.full();
  }

  @override
  Future<Listing> update(String? org, String id, int version, Json body) async {
    calls.add('update $id v$version');
    final r = _own(org, id);
    _editable(r);
    if (version != r.version) throw _rule('Someone else changed this listing. Reload and try again.', code: 'VERSION_CONFLICT', status: 412);
    r
      ..title = '${body['title'] ?? r.title}'
      ..make = '${body['manufacturer'] ?? r.make}'
      ..model = '${body['model'] ?? r.model}'
      ..body = {...r.body, ...body};
    _touched(r);
    return r.full();
  }

  @override
  Future<Listing> setPricing(String? org, String id, List<PricingPlan> plans, Mobilisation? mobilisation) async {
    calls.add('pricing $id ${plans.length}');
    final r = _own(org, id);
    _editable(r);
    r
      ..pricing = plans
      ..mobilisation = mobilisation;
    _touched(r);
    return r.full();
  }

  @override
  Future<Listing> setSpecs(String? org, String id, List<SpecValue> specs) async {
    calls.add('specs $id ${specs.length}');
    final r = _own(org, id)..specs = specs;
    return r.full();
  }

  @override
  Future<String> upload(String purpose, PickedFile file) async {
    calls.add('upload $purpose ${file.name}');
    final id = 'up-${++_ids}';
    uploads[id] = file;
    return id;
  }

  @override
  Future<Listing> addPhoto(String? org, String id, String uploadId, {String? angle}) async {
    calls.add('photo $id');
    final r = _own(org, id);
    _editable(r);
    if (!uploads.containsKey(uploadId)) throw _rule('Upload not found');
    if (r.photos.length >= 20) throw _rule('A listing can have at most 20 photos');
    r.photos = [...r.photos, ListingPhoto(id: 'p-$uploadId', url: 'https://example.invalid/$uploadId.jpg', isCover: r.photos.isEmpty)];
    return r.full();
  }

  @override
  Future<void> deletePhoto(String? org, String id, String photoId) async {
    calls.add('delete-photo $photoId');
    final r = _own(org, id);
    r.photos = [for (final p in r.photos) if (p.id != photoId) p];
  }

  @override
  Future<Listing> setCover(String? org, String id, String photoId) async {
    calls.add('cover $photoId');
    final r = _own(org, id);
    r.photos = [for (final p in r.photos) ListingPhoto(id: p.id, url: p.url, isCover: p.id == photoId)];
    return r.full();
  }

  @override
  Future<ListingDocument> addDocument(String? org, String id, {required String docType, required String uploadId, String? number, String? validUntil}) async {
    calls.add('document $id $docType');
    final r = _own(org, id);
    final d = ListingDocument(id: 'd-${++_ids}', docType: docType, status: 'pending', number: number, validUntil: validUntil);
    r.documents = [...r.documents, d];
    return d;
  }

  @override
  Future<void> deleteDocument(String? org, String id, String docId) async {
    final r = _own(org, id);
    r.documents = [for (final d in r.documents) if (d.id != docId) d];
  }

  @override
  Future<String> documentLink(String? org, String id, String docId) async {
    _own(org, id);
    return 'https://files.invalid/$docId';
  }

  @override
  Future<void> act(String? org, String id, OwnerAction action) async {
    calls.add('${action.name} $id');
    final r = _own(org, id);
    switch (action) {
      case OwnerAction.submit:
        if (r.status != ListingStatus.draft && r.status != ListingStatus.rejected) throw _rule('cannot submit', code: 'INVALID_STATE_TRANSITION', status: 409);
        if (r.pricing.isEmpty) throw _rule('set at least one price before submitting for review');
        if (r.photos.isEmpty) throw _rule('add at least one photo before submitting for review');
        r.status = ListingStatus.pendingReview;
      case OwnerAction.pause:
        if (r.status != ListingStatus.active) throw _rule('cannot pause', code: 'INVALID_STATE_TRANSITION', status: 409);
        r.status = ListingStatus.paused;
      case OwnerAction.resume:
        if (r.status != ListingStatus.paused) throw _rule('cannot resume', code: 'INVALID_STATE_TRANSITION', status: 409);
        r.status = ListingStatus.active;
    }
    r.version++;
  }

  @override
  Future<void> delete(String? org, String id) async {
    calls.add('delete $id');
    _own(org, id).status = ListingStatus.retired;
  }

  @override
  Future<ListingPage> moderationQueue(ListingStatus status, {String? cursor}) async =>
      ListingPage(items: [for (final r in listings.values) if (r.status == status) r.card()]);

  @override
  Future<Listing> reviewListing(String id) async => listings[id]!.full();

  @override
  Future<void> moderate(String id, ModerationAction action, {String? reason}) async {
    calls.add('${action.name} $id${reason == null ? '' : ': $reason'}');
    final r = listings[id]!;
    final (from, to) = switch (action) {
      ModerationAction.approve => ([ListingStatus.pendingReview], ListingStatus.active),
      ModerationAction.reject => ([ListingStatus.pendingReview], ListingStatus.rejected),
      ModerationAction.suspend => ([ListingStatus.active, ListingStatus.paused], ListingStatus.suspended),
      ModerationAction.reinstate => ([ListingStatus.suspended], ListingStatus.active),
      ModerationAction.remove => (ListingStatus.values.where((s) => s != ListingStatus.removed && s != ListingStatus.retired).toList(), ListingStatus.removed),
    };
    if (!from.contains(r.status)) throw _rule('cannot ${action.name} a listing that is ${r.status.wire}', code: 'INVALID_STATE_TRANSITION', status: 409);
    r
      ..status = to
      ..note = reason
      ..version += 1;
  }

  final adminCats = <ListingCategory>[];

  @override
  Future<List<ListingCategory>> adminCategories() async => [..._cats, ...adminCats];

  @override
  Future<ListingCategory> createCategory(Json body) async {
    calls.add('create-category ${body['slug']}');
    final c = ListingCategory(id: 'c-new-${++_ids}', slug: '${body['slug']}', name: '${body['name_en']}', isActive: body['is_active'] != false);
    adminCats.add(c);
    return c;
  }

  @override
  Future<ListingCategory> updateCategory(String id, Json body) async {
    calls.add('update-category $id active=${body['is_active']}');
    return ListingCategory(id: id, slug: 'x', name: '${body['name_en']}', isActive: body['is_active'] != false);
  }
}

class _Row {
  _Row(this.id, this.title, this.category, this.make, this.model, this.org, this.ownerName, this.status, {this.verified = false});
  final String id;
  String title;
  final String category;
  String make;
  String model;
  final String org;
  final String ownerName;
  ListingStatus status;
  final bool verified;
  int version = 1;
  String? note;
  Json body = const {};
  List<PricingPlan> pricing = const [];
  Mobilisation? mobilisation;
  List<SpecValue> specs = const [];
  List<ListingPhoto> photos = const [];
  List<ListingDocument> documents = const [];

  String get categoryName => switch (category) { 'backhoe-loader' => 'JCB / Backhoe Loader', 'excavator' => 'Excavator', _ => 'Crane' };

  ListingCard card() => ListingCard(
        id: id,
        title: title,
        status: status,
        version: version,
        categorySlug: category,
        categoryName: categoryName,
        makeName: make,
        modelName: model,
        condition: MachineCondition.good,
        ownerName: ownerName,
        ownerVerified: verified,
        locationLabel: 'Hinjewadi, Pune',
        coverUrl: photos.where((p) => p.isCover).firstOrNull?.url,
        photoCount: photos.length,
        pricePaise: pricing.firstOrNull?.ratePaise ?? 0,
        priceUnit: pricing.firstOrNull?.unit,
        moderationNote: note,
      );

  Listing full({bool withDocs = true}) => Listing(
        id: id,
        title: title,
        status: status,
        version: version,
        categorySlug: category,
        categoryName: categoryName,
        makeName: make,
        modelName: model,
        modelId: 'm-$id',
        ownerOrgId: org,
        ownerName: ownerName,
        ownerVerified: verified,
        condition: MachineCondition.parse(body['condition']),
        location: const GeoPoint(18.59, 73.74),
        cityId: (body['city_id'] as String?) ?? 'city-pune',
        cityName: 'Pune',
        locationLabel: (body['location_label'] as String?) ?? 'Hinjewadi, Pune',
        year: body['manufacture_year'] as int?,
        description: (body['description'] as String?) ?? '',
        pricing: pricing,
        mobilisation: mobilisation,
        specs: specs,
        photos: photos,
        documents: withDocs ? documents : const [],
        moderationNote: note,
      );
}
