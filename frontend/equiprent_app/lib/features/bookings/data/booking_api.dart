import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../comms/data/comms_api.dart' show BookingSide, sideOrgProvider;
import '../../comms/presentation/comms_widgets.dart' show newClientId;
import '../../documents/data/compliance_models.dart';
import 'booking_models.dart';

/// The booking engine endpoints the parties use: estimates, requests,
/// payments, cancellations, extensions, the owner's operations, tracking and
/// invoices. [org] is sent as X-Org-Id; the server re-checks every action
/// against the booking's state and the caller's side.
abstract class BookingApi {
  Future<Estimate> estimate(String equipmentId, BookingChoice choice);

  Future<List<SiteAddress>> sites(String org);
  Future<SiteAddress> addSite(String org, NewSite site);

  /// Creates the booking (one request per [idempotencyKey]); returns its id.
  Future<String> create(
    String? org, {
    required String equipmentId,
    required String siteAddressId,
    required BookingChoice choice,
    required String workDescription,
    required String paymentMode,
    required String idempotencyKey,
    String? customerType,
    Map<String, String>? customer,
  });

  /// A lifecycle step without a body other than an optional reason, e.g.
  /// `owner/bookings/{id}/dispatch` or `bookings/{id}/handover/confirm`.
  Future<void> act(String? org, String path, {String? reason, int? version});

  /// The customer's sign-off of the handover inspection (a prerequisite of
  /// confirming the handover). Fakes may rely on this no-op default.
  Future<void> acknowledgeInspection(String? org, String id, String stage) async {}

  Future<CancelOutcome> cancel(
    String? org,
    String id, {
    String? reason,
    bool dryRun = false,
    int? version,
  });
  Future<void> answerCancellation(
    String? org,
    String id,
    String decision, {
    String? reason,
  });

  Future<ExtensionQuote> quoteExtension(
    String? org,
    String id,
    DateTime newEnd,
  );
  Future<void> requestExtension(
    String? org,
    String id,
    DateTime newEnd, {
    String note = '',
  });
  Future<void> answerExtension(
    String? org,
    String id,
    String extensionId,
    String decision, {
    String? reason,
  });

  Future<List<OperatorOption>> operators(String org);
  Future<void> assignOperator(String? org, String id, String operatorUserId);
  Future<DriverCompensation> recordDriverCompensation(
    String? org,
    String bookingId,
    int agreedAmountPaise,
  );
  Future<List<DriverCompensation>> driverCompensations(String? org);
  Future<DriverCompensation> payDriverCompensation(
    String? org,
    String id,
    String transactionReference,
  );

  Future<Checkout> startPayment(String? org, String id, String purpose);
  Future<void> verifyPayment(
    String? org,
    String paymentId, {
    required String gatewayPaymentId,
    required String signature,
  });
  Future<List<BookingPayment>> payments(String? org, String id);
  Future<List<DeliveryPhoto>> deliveryPhotos(String? org, String id);
  Future<void> approveDeliveryPhoto(String? org, String id, String photoId);
  Future<DeliveryPhoto> addDeliveryPhoto(
    String? org,
    String id, {
    required String uploadId,
    required DateTime capturedAt,
    required String clientId,
  });

  Future<FinalBill> settleFinal(
    String? org,
    String id, {
    required DateTime actualStart,
    required DateTime actualEnd,
    double operatingHours = 0,
    int damagePaise = 0,
    String damageNote = '',
    bool dryRun = false,
  });

  Future<List<InvoiceInfo>> invoices(String? org, String id);

  /// Invoices issued by or to [org].
  Future<List<InvoiceInfo>> orgInvoices(String? org);
  Future<void> issueInvoice(String? org, String id);
  Future<String> invoiceLink(String? org, String invoiceId);
  Future<InvoiceDetail> invoice(String? org, String invoiceId);

  /// Credits [amountPaise] of taxable value (tax is added in proportion).
  Future<InvoiceDetail> creditNote(
    String? org,
    String invoiceId, {
    required int amountPaise,
    required String reason,
  });

  // compliance (admin): tax rules and agreement terms
  Future<List<TaxRule>> taxRules();
  Future<List<TaxRule>> reviewTaxRule(
    String id, {
    required String reviewedBy,
    String? legalBasis,
  });
  Future<List<LegalTemplate>> legalTemplates();
  Future<LegalTemplate> reviewLegalTemplate(
    String id, {
    required String reviewedBy,
    required String capacity,
    String? reference,
  });
  Future<LegalTemplate> publishLegalTemplate(String id);
  Future<void> dispute(
    String? org,
    String id, {
    required String category,
    required String summary,
  });
}

class HttpBookingApi implements BookingApi {
  HttpBookingApi(this._c);
  final ApiClient _c;

  static Json _j(Object? v) => (v as Map).cast<String, dynamic>();
  static List<Json> _data(Object? v) => [
    for (final e in (_j(v)['data'] as List? ?? const [])) _j(e),
  ];
  static Map<String, String> _h(String? org, {String? idem}) => {
    'X-Org-Id': ?org,
    'Idempotency-Key': ?idem,
  };
  static Json _reason(String? reason, int? version) => {
    if (reason != null && reason.isNotEmpty) 'reason': reason,
    'version': ?version,
  };

  @override
  Future<Estimate> estimate(String equipmentId, BookingChoice choice) =>
      _c.post(
        '/equipment/$equipmentId/estimate',
        body: choice.toEstimateJson(),
        decode: (j) => Estimate.fromJson(_j(j)),
      );

  @override
  Future<List<SiteAddress>> sites(String org) => _c.get(
    '/orgs/$org/addresses',
    decode: (j) => [for (final a in _data(j)) SiteAddress.fromJson(a)],
  );

  @override
  Future<SiteAddress> addSite(String org, NewSite site) => _c.post(
    '/orgs/$org/addresses',
    body: site.toJson(),
    decode: (j) => SiteAddress.fromJson(_j(j)),
  );

  @override
  Future<String> create(
    String? org, {
    required String equipmentId,
    required String siteAddressId,
    required BookingChoice choice,
    required String workDescription,
    required String paymentMode,
    required String idempotencyKey,
    String? customerType,
    Map<String, String>? customer,
  }) => _c.post(
    '/bookings',
    headers: _h(org, idem: idempotencyKey),
    body: {
      'equipment_id': equipmentId,
      'site_address_id': siteAddressId,
      'dates': choice.dates.toJson(),
      'pricing_unit': choice.unit,
      'with_operator': choice.withOperator,
      'delivery': choice.delivery,
      'pickup': choice.pickup,
      if (choice.expectedHours > 0) 'expected_hours': choice.expectedHours,
      'work_description': workDescription,
      'payment_mode': paymentMode,
      'customer_type': ?customerType,
      'customer': ?customer,
    },
    decode: (j) => '${_j(j)['id']}',
  );

  @override
  Future<void> acknowledgeInspection(String? org, String id, String stage) =>
      _c.post(
        '/bookings/$id/inspection-acknowledgements',
        headers: _h(org),
        body: {'stage': stage},
        decode: (_) {},
      );

  @override
  Future<void> act(String? org, String path, {String? reason, int? version}) =>
      _c.post(
        '/$path',
        headers: _h(org),
        body: _reason(reason, version),
        decode: (_) {},
      );

  @override
  Future<CancelOutcome> cancel(
    String? org,
    String id, {
    String? reason,
    bool dryRun = false,
    int? version,
  }) => _c.post(
    '/bookings/$id/cancel${dryRun ? '?dry_run=true' : ''}',
    headers: _h(org),
    body: dryRun ? null : _reason(reason, version),
    decode: (j) => CancelOutcome.fromJson(_j(j)),
  );

  @override
  Future<void> answerCancellation(
    String? org,
    String id,
    String decision, {
    String? reason,
  }) {
    final path = decision == 'withdraw'
        ? '/bookings/$id/cancellation/withdraw'
        : '/owner/bookings/$id/cancellation/$decision';
    return _c.post(
      path,
      headers: _h(org),
      body: _reason(reason, null),
      decode: (_) {},
    );
  }

  @override
  Future<ExtensionQuote> quoteExtension(
    String? org,
    String id,
    DateTime newEnd,
  ) => _c.post(
    '/bookings/$id/extensions/quote',
    headers: _h(org),
    body: {'new_end': newEnd.toUtc().toIso8601String()},
    decode: (j) => ExtensionQuote.fromJson(_j(j)),
  );

  @override
  Future<void> requestExtension(
    String? org,
    String id,
    DateTime newEnd, {
    String note = '',
  }) => _c.post(
    '/bookings/$id/extensions',
    headers: _h(org, idem: newClientId()),
    body: {'new_end': newEnd.toUtc().toIso8601String(), 'note': note},
    decode: (_) {},
  );

  @override
  Future<void> answerExtension(
    String? org,
    String id,
    String extensionId,
    String decision, {
    String? reason,
  }) => _c.post(
    '/bookings/$id/extensions/$extensionId/$decision',
    headers: _h(org),
    body: _reason(reason, null),
    decode: (_) {},
  );

  @override
  Future<List<OperatorOption>> operators(String org) => _c.get(
    '/fleet/$org/operators',
    decode: (j) => [for (final o in _data(j)) OperatorOption.fromJson(o)],
  );

  @override
  Future<void> assignOperator(String? org, String id, String operatorUserId) =>
      _c.post(
        '/owner/bookings/$id/assign-operator',
        headers: _h(org),
        body: {'operator_user_id': operatorUserId},
        decode: (_) {},
      );

  @override
  Future<DriverCompensation> recordDriverCompensation(
    String? org,
    String bookingId,
    int agreedAmountPaise,
  ) => _c.post(
    '/owner/bookings/$bookingId/driver-compensation',
    headers: _h(org),
    body: {'agreed_amount_paise': agreedAmountPaise},
    decode: (j) => DriverCompensation.fromJson(_j(j)),
  );
  @override
  Future<List<DriverCompensation>> driverCompensations(String? org) => _c.get(
    '/owner/driver-compensations',
    headers: _h(org),
    decode: (j) => [for (final x in _data(j)) DriverCompensation.fromJson(x)],
  );
  @override
  Future<DriverCompensation> payDriverCompensation(
    String? org,
    String id,
    String transactionReference,
  ) => _c.post(
    '/owner/driver-compensations/$id/pay',
    headers: _h(org),
    body: {'transaction_reference': transactionReference},
    decode: (j) => DriverCompensation.fromJson(_j(j)),
  );

  @override
  Future<Checkout> startPayment(String? org, String id, String purpose) =>
      _c.post(
        '/bookings/$id/payments',
        headers: _h(org, idem: newClientId()),
        body: {'purpose': purpose},
        decode: (j) => Checkout.fromJson(_j(j)),
      );

  @override
  Future<void> verifyPayment(
    String? org,
    String paymentId, {
    required String gatewayPaymentId,
    required String signature,
  }) => _c.post(
    '/payments/$paymentId/verify',
    headers: _h(org),
    body: {'gateway_payment_id': gatewayPaymentId, 'signature': signature},
    decode: (_) {},
  );

  @override
  Future<List<BookingPayment>> payments(String? org, String id) => _c.get(
    '/bookings/$id/payments',
    headers: _h(org),
    decode: (j) => [for (final p in _data(j)) BookingPayment.fromJson(p)],
  );

  @override
  Future<List<DeliveryPhoto>> deliveryPhotos(String? org, String id) => _c.get(
    '/bookings/$id/delivery-photos',
    headers: _h(org),
    decode: (j) => [for (final p in _data(j)) DeliveryPhoto.fromJson(p)],
  );

  @override
  Future<void> approveDeliveryPhoto(String? org, String id, String photoId) =>
      _c.post(
        '/bookings/$id/delivery-photos/$photoId/approve',
        headers: _h(org),
        decode: (_) {},
      );

  @override
  Future<DeliveryPhoto> addDeliveryPhoto(
    String? org,
    String id, {
    required String uploadId,
    required DateTime capturedAt,
    required String clientId,
  }) => _c.post(
    '/bookings/$id/delivery-photos',
    headers: _h(org, idem: clientId),
    body: {
      'upload_id': uploadId,
      'captured_at': capturedAt.toUtc().toIso8601String(),
      'client_id': clientId,
    },
    decode: (j) => DeliveryPhoto.fromJson(_j(j)),
  );

  @override
  Future<FinalBill> settleFinal(
    String? org,
    String id, {
    required DateTime actualStart,
    required DateTime actualEnd,
    double operatingHours = 0,
    int damagePaise = 0,
    String damageNote = '',
    bool dryRun = false,
  }) => _c.post(
    '/owner/bookings/$id/final-charges${dryRun ? '?dry_run=true' : ''}',
    headers: _h(org),
    body: {
      'actual_start': actualStart.toUtc().toIso8601String(),
      'actual_end': actualEnd.toUtc().toIso8601String(),
      'operating_hours': operatingHours,
      'damage_paise': damagePaise,
      'damage_note': damageNote,
    },
    decode: (j) => FinalBill.fromJson(_j(j)),
  );

  @override
  Future<List<InvoiceInfo>> invoices(String? org, String id) => _c.get(
    '/bookings/$id/invoices',
    headers: _h(org),
    decode: (j) => [for (final i in _data(j)) InvoiceInfo.fromJson(i)],
  );

  @override
  Future<List<InvoiceInfo>> orgInvoices(String? org) => _c.get(
    '/invoices',
    headers: _h(org),
    query: {'limit': 100},
    decode: (j) => [for (final i in _data(j)) InvoiceInfo.fromJson(i)],
  );

  @override
  Future<void> issueInvoice(String? org, String id) =>
      _c.post('/owner/bookings/$id/invoices', headers: _h(org), decode: (_) {});

  @override
  Future<String> invoiceLink(String? org, String invoiceId) => _c.get(
    '/invoices/$invoiceId/link',
    headers: _h(org),
    decode: (j) => '${_j(j)['url']}',
  );

  @override
  Future<InvoiceDetail> invoice(String? org, String invoiceId) => _c.get(
    '/invoices/$invoiceId',
    headers: _h(org),
    decode: (j) => InvoiceDetail.fromJson(_j(j)),
  );

  @override
  Future<List<TaxRule>> taxRules() => _c.get(
    '/admin/tax-rates',
    decode: (j) => [for (final t in _data(j)) TaxRule.fromJson(t)],
  );

  @override
  Future<List<TaxRule>> reviewTaxRule(
    String id, {
    required String reviewedBy,
    String? legalBasis,
  }) => _c.post(
    '/admin/tax-rates/$id/review',
    body: {'reviewed_by_name': reviewedBy, 'legal_basis': ?legalBasis},
    decode: (j) => [for (final t in _data(j)) TaxRule.fromJson(t)],
  );

  @override
  Future<List<LegalTemplate>> legalTemplates() => _c.get(
    '/admin/legal-templates',
    decode: (j) => [for (final t in _data(j)) LegalTemplate.fromJson(t)],
  );

  @override
  Future<LegalTemplate> reviewLegalTemplate(
    String id, {
    required String reviewedBy,
    required String capacity,
    String? reference,
  }) => _c.post(
    '/admin/legal-templates/$id/review',
    body: {
      'reviewed_by_name': reviewedBy,
      'reviewer_capacity': capacity,
      'review_reference': ?reference,
    },
    decode: (j) => LegalTemplate.fromJson(_j(j)),
  );

  @override
  Future<LegalTemplate> publishLegalTemplate(String id) => _c.post(
    '/admin/legal-templates/$id/publish',
    decode: (j) => LegalTemplate.fromJson(_j(j)),
  );

  @override
  Future<InvoiceDetail> creditNote(
    String? org,
    String invoiceId, {
    required int amountPaise,
    required String reason,
  }) => _c.post(
    '/invoices/$invoiceId/credit-notes',
    headers: _h(org),
    body: {'amount_paise': amountPaise, 'reason': reason},
    decode: (j) => InvoiceDetail.fromJson(_j(j)),
  );

  @override
  Future<void> dispute(
    String? org,
    String id, {
    required String category,
    required String summary,
  }) => _c.post(
    '/bookings/$id/disputes',
    headers: _h(org),
    body: {'category': category, 'summary': summary},
    decode: (_) {},
  );
}

final bookingApiProvider = Provider<BookingApi>(
  (ref) => HttpBookingApi(ref.watch(apiClientProvider)),
);

final driverCompensationForBookingProvider = FutureProvider.autoDispose
    .family<DriverCompensation?, (String?, String)>((ref, k) async {
      final list = await ref
          .watch(bookingApiProvider)
          .driverCompensations(k.$1);
      for (final x in list) {
        if (x.bookingId == k.$2) return x;
      }
      return null;
    });

// ---- data providers ------------------------------------------------------------------------------

/// The customer's saved work sites.
final customerSitesProvider = FutureProvider.autoDispose<List<SiteAddress>>((
  ref,
) async {
  final org = ref.watch(sideOrgProvider(BookingSide.customer));
  if (org == null) return const [];
  final all = await ref.watch(bookingApiProvider).sites(org);
  return [
    for (final a in all)
      if (a.isSite && a.point != null) a,
  ];
});

/// A live estimate for a listing and the customer's choices.
final estimateProvider = FutureProvider.autoDispose
    .family<Estimate, (String, BookingChoice)>(
      (ref, k) => ref.watch(bookingApiProvider).estimate(k.$1, k.$2),
    );

final bookingInvoicesProvider = FutureProvider.autoDispose
    .family<List<InvoiceInfo>, (BookingSide, String)>(
      (ref, k) => ref
          .watch(bookingApiProvider)
          .invoices(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );

final bookingPaymentsProvider = FutureProvider.autoDispose
    .family<List<BookingPayment>, (BookingSide, String)>(
      (ref, k) => ref
          .watch(bookingApiProvider)
          .payments(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );

final bookingDeliveryPhotosProvider = FutureProvider.autoDispose
    .family<List<DeliveryPhoto>, (BookingSide, String)>(
      (ref, k) => ref
          .watch(bookingApiProvider)
          .deliveryPhotos(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );

final ownerOperatorsProvider = FutureProvider.autoDispose<List<OperatorOption>>(
  (ref) async {
    final org = ref.watch(sideOrgProvider(BookingSide.owner));
    if (org == null) return const [];
    return ref.watch(bookingApiProvider).operators(org);
  },
);

final invoiceDetailProvider = FutureProvider.autoDispose
    .family<InvoiceDetail, (BookingSide, String)>(
      (ref, k) => ref
          .watch(bookingApiProvider)
          .invoice(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );
final taxRulesProvider = FutureProvider.autoDispose<List<TaxRule>>(
  (ref) => ref.watch(bookingApiProvider).taxRules(),
);
final legalTemplatesProvider = FutureProvider.autoDispose<List<LegalTemplate>>(
  (ref) => ref.watch(bookingApiProvider).legalTemplates(),
);
final orgInvoicesProvider = FutureProvider.autoDispose
    .family<List<InvoiceInfo>, BookingSide>(
      (ref, side) => ref
          .watch(bookingApiProvider)
          .orgInvoices(ref.watch(sideOrgProvider(side))),
    );
