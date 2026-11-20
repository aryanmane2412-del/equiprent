import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/network/api_client.dart';
import '../../../core/session/user_role.dart';
import '../../verification/data/verification_models.dart' show PickedFile;
import 'listing_models.dart';

/// Lifecycle actions an owner can take.
enum OwnerAction { submit, pause, resume }

/// Moderation actions (admins).
enum ModerationAction { approve, reject, suspend, reinstate, remove }

/// The listing endpoints: public catalogue, owner management and
/// moderation. Owner calls carry the organisation (X-Org-Id); the server
/// checks the role and, for fleet managers, the machines they may manage.
abstract class ListingsApi {
  Future<List<ListingCategory>> categories();
  Future<List<City>> cities();
  Future<List<CatalogModel>> models(String category);
  Future<ListingPage> browse(BrowseFilter filter, {String? cursor});
  Future<Listing> listing(String id);
  Future<List<BusyRange>> availability(String id, DateTime from, DateTime to);
  Future<List<OwnerBlock>> ownerBlocks(String? org, String id, DateTime from, DateTime to);
  Future<void> addBlock(String? org, String id, DateTime start, DateTime end, {String? note});
  Future<void> removeBlock(String? org, String id, String blockId);

  Future<ListingPage> myListings(String? org, {ListingStatus? status, String? cursor});
  Future<Listing> myListing(String? org, String id);
  Future<Listing> create(String? org, Json body);
  Future<Listing> update(String? org, String id, int version, Json body);
  Future<Listing> setPricing(String? org, String id, List<PricingPlan> plans, Mobilisation? mobilisation);
  Future<Listing> setSpecs(String? org, String id, List<SpecValue> specs);
  Future<String> upload(String purpose, PickedFile file);
  Future<Listing> addPhoto(String? org, String id, String uploadId, {String? angle});
  Future<void> deletePhoto(String? org, String id, String photoId);
  Future<Listing> setCover(String? org, String id, String photoId);
  Future<ListingDocument> addDocument(String? org, String id, {required String docType, required String uploadId, String? number, String? validUntil});
  Future<void> deleteDocument(String? org, String id, String docId);
  Future<String> documentLink(String? org, String id, String docId);
  Future<void> act(String? org, String id, OwnerAction action);
  Future<void> delete(String? org, String id);

  Future<ListingPage> moderationQueue(ListingStatus status, {String? cursor});
  Future<Listing> reviewListing(String id);
  Future<void> moderate(String id, ModerationAction action, {String? reason});
  Future<List<ListingCategory>> adminCategories();
  Future<ListingCategory> createCategory(Json body);
  Future<ListingCategory> updateCategory(String id, Json body);
}

class HttpListingsApi implements ListingsApi {
  HttpListingsApi(this._c);
  final ApiClient _c;

  static Json _j(Object? v) => (v as Map).cast<String, dynamic>();
  static Map<String, String>? _org(String? org) => org == null ? null : {'X-Org-Id': org};
  static List<Json> _data(Object? v) => [for (final e in (_j(v)['data'] as List? ?? const [])) _j(e)];

  @override
  Future<List<ListingCategory>> categories() =>
      _c.get('/categories', decode: (j) => [for (final c in _data(j)) ListingCategory.fromJson(c)]);
  @override
  Future<List<City>> cities() => _c.get('/cities', decode: (j) => [for (final c in _data(j)) City.fromJson(c)]);
  @override
  Future<List<CatalogModel>> models(String category) =>
      _c.get('/models', query: {'category': category}, decode: (j) => [for (final m in _data(j)) CatalogModel.fromJson(m)]);
  @override
  Future<ListingPage> browse(BrowseFilter filter, {String? cursor}) => _c.get('/equipment',
      query: {...filter.toQuery(), 'limit': 24, 'cursor': ?cursor}, decode: (j) => ListingPage.fromJson(_j(j)));
  @override
  Future<Listing> listing(String id) => _c.get('/equipment/$id', decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<List<BusyRange>> availability(String id, DateTime from, DateTime to) => _c.get('/equipment/$id/availability',
      query: {'from': from.toUtc().toIso8601String(), 'to': to.toUtc().toIso8601String()},
      decode: (j) => [for (final b in (_j(j)['busy'] as List? ?? const [])) BusyRange.fromJson(_j(b))]);

  @override
  Future<List<OwnerBlock>> ownerBlocks(String? org, String id, DateTime from, DateTime to) => _c.get('/owner/equipment/$id/calendar',
      headers: _org(org),
      query: {'from': from.toUtc().toIso8601String(), 'to': to.toUtc().toIso8601String()},
      decode: (j) => [for (final b in (_j(j)['blocks'] as List? ?? const [])) OwnerBlock.fromJson(_j(b))]);
  @override
  Future<void> addBlock(String? org, String id, DateTime start, DateTime end, {String? note}) => _c.post('/owner/equipment/$id/blocks',
      headers: _org(org),
      body: {'start': start.toUtc().toIso8601String(), 'end': end.toUtc().toIso8601String(), 'reason': 'owner_block', 'note': ?note},
      decode: (_) {});
  @override
  Future<void> removeBlock(String? org, String id, String blockId) => _c.delete('/owner/equipment/$id/blocks/$blockId', headers: _org(org));

  @override
  Future<ListingPage> myListings(String? org, {ListingStatus? status, String? cursor}) => _c.get('/owner/equipment',
      headers: _org(org), query: {'limit': 50, 'status': ?status?.wire, 'cursor': ?cursor}, decode: (j) => ListingPage.fromJson(_j(j)));
  @override
  Future<Listing> myListing(String? org, String id) =>
      _c.get('/owner/equipment/$id', headers: _org(org), decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<Listing> create(String? org, Json body) =>
      _c.post('/owner/equipment', headers: _org(org), body: body, decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<Listing> update(String? org, String id, int version, Json body) => _c.patch('/owner/equipment/$id',
      headers: {...?_org(org), 'If-Match': '"$version"'}, body: {...body, 'version': version}, decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<Listing> setPricing(String? org, String id, List<PricingPlan> plans, Mobilisation? mobilisation) => _c.put(
        '/owner/equipment/$id/pricing',
        headers: _org(org),
        body: {'plans': [for (final p in plans) p.toJson()], 'mobilisation': ?mobilisation?.toJson()},
        decode: (j) => Listing.fromJson(_j(j)),
      );
  @override
  Future<Listing> setSpecs(String? org, String id, List<SpecValue> specs) => _c.put('/owner/equipment/$id/specs',
      headers: _org(org), body: {'specs': [for (final s in specs) s.toJson()]}, decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<String> upload(String purpose, PickedFile file) => _c.post('/uploads',
      body: FormData.fromMap({'purpose': purpose, 'file': MultipartFile.fromBytes(file.bytes, filename: file.name)}),
      decode: (j) => '${_j(j)['id']}');
  @override
  Future<Listing> addPhoto(String? org, String id, String uploadId, {String? angle}) => _c.post('/owner/equipment/$id/photos',
      headers: _org(org), body: {'upload_id': uploadId, 'angle': ?angle}, decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<void> deletePhoto(String? org, String id, String photoId) => _c.delete('/owner/equipment/$id/photos/$photoId', headers: _org(org));
  @override
  Future<Listing> setCover(String? org, String id, String photoId) =>
      _c.post('/owner/equipment/$id/photos/$photoId/cover', headers: _org(org), decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<ListingDocument> addDocument(String? org, String id, {required String docType, required String uploadId, String? number, String? validUntil}) =>
      _c.post('/owner/equipment/$id/documents',
          headers: _org(org),
          body: {'doc_type': docType, 'upload_id': uploadId, 'document_number': ?number, 'valid_until': ?validUntil},
          decode: (j) => ListingDocument.fromJson(_j(j)));
  @override
  Future<void> deleteDocument(String? org, String id, String docId) => _c.delete('/owner/equipment/$id/documents/$docId', headers: _org(org));
  @override
  Future<String> documentLink(String? org, String id, String docId) =>
      _c.get('/owner/equipment/$id/documents/$docId/url', headers: _org(org), decode: (j) => '${_j(j)['url']}');
  @override
  Future<void> act(String? org, String id, OwnerAction action) =>
      _c.post('/owner/equipment/$id/${action.name}', headers: _org(org), decode: (_) {});
  @override
  Future<void> delete(String? org, String id) => _c.delete('/owner/equipment/$id', headers: _org(org));

  @override
  Future<ListingPage> moderationQueue(ListingStatus status, {String? cursor}) => _c.get('/admin/listings',
      query: {'status': status.wire, 'limit': 50, 'cursor': ?cursor}, decode: (j) => ListingPage.fromJson(_j(j)));
  @override
  Future<Listing> reviewListing(String id) => _c.get('/admin/listings/$id', decode: (j) => Listing.fromJson(_j(j)));
  @override
  Future<void> moderate(String id, ModerationAction action, {String? reason}) =>
      _c.post('/admin/listings/$id/${action.name}', body: {'reason': ?reason}, decode: (_) {});
  @override
  Future<List<ListingCategory>> adminCategories() =>
      _c.get('/admin/categories', decode: (j) => [for (final c in _data(j)) ListingCategory.fromJson(c)]);
  @override
  Future<ListingCategory> createCategory(Json body) =>
      _c.post('/admin/categories', body: body, decode: (j) => ListingCategory.fromJson(_j(j)));
  @override
  Future<ListingCategory> updateCategory(String id, Json body) =>
      _c.put('/admin/categories/$id', body: body, decode: (j) => ListingCategory.fromJson(_j(j)));
}

final listingsApiProvider = Provider<ListingsApi>((ref) => HttpListingsApi(ref.watch(apiClientProvider)));

/// The organisation owner-side listing calls act for (owner, else fleet
/// manager).
final listingOrgProvider = Provider<String?>((ref) {
  final orgs = ref.watch(authControllerProvider).user?.orgs ?? const {};
  return orgs[UserRole.owner] ?? orgs[UserRole.fleetManager];
});

// ---- data providers ---------------------------------------------------------------------

final categoriesProvider = FutureProvider<List<ListingCategory>>((ref) => ref.watch(listingsApiProvider).categories());
final citiesProvider = FutureProvider<List<City>>((ref) => ref.watch(listingsApiProvider).cities());
final modelsProvider = FutureProvider.autoDispose.family<List<CatalogModel>, String>((ref, c) => ref.watch(listingsApiProvider).models(c));

final browseProvider =
    FutureProvider.autoDispose.family<ListingPage, BrowseFilter>((ref, f) => ref.watch(listingsApiProvider).browse(f));
final listingProvider = FutureProvider.autoDispose.family<Listing, String>((ref, id) => ref.watch(listingsApiProvider).listing(id));
final availabilityProvider = FutureProvider.autoDispose.family<List<BusyRange>, String>((ref, id) {
  final now = DateTime.now();
  final from = DateTime(now.year, now.month, now.day);
  return ref.watch(listingsApiProvider).availability(id, from, from.add(const Duration(days: 42)));
});

final ownerBlocksProvider = FutureProvider.autoDispose.family<List<OwnerBlock>, String>((ref, id) {
  final now = DateTime.now();
  final from = DateTime(now.year, now.month, now.day);
  return ref.watch(listingsApiProvider).ownerBlocks(ref.watch(listingOrgProvider), id, from, from.add(const Duration(days: 42)));
});

final myListingsProvider = FutureProvider.autoDispose.family<ListingPage, ListingStatus?>(
    (ref, status) => ref.watch(listingsApiProvider).myListings(ref.watch(listingOrgProvider), status: status));
final myListingProvider = FutureProvider.autoDispose.family<Listing, String>(
    (ref, id) => ref.watch(listingsApiProvider).myListing(ref.watch(listingOrgProvider), id));

final moderationQueueProvider = FutureProvider.autoDispose.family<ListingPage, ListingStatus>(
    (ref, status) => ref.watch(listingsApiProvider).moderationQueue(status));
final reviewListingProvider = FutureProvider.autoDispose.family<Listing, String>((ref, id) => ref.watch(listingsApiProvider).reviewListing(id));
final adminCategoriesProvider = FutureProvider.autoDispose<List<ListingCategory>>((ref) => ref.watch(listingsApiProvider).adminCategories());
