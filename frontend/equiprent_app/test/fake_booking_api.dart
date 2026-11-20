import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/bookings/data/booking_api.dart';
import 'package:equiprent_app/features/bookings/data/booking_models.dart';
import 'package:equiprent_app/features/documents/data/compliance_models.dart';

import 'fake_comms_api.dart';

/// The booking engine against [comms]'s simulated server state: it moves
/// bookings through the same lifecycle as the backend (and refuses steps the
/// server would refuse), and records every write in [calls].
class FakeBookingApi implements BookingApi {
  FakeBookingApi(this.comms);
  final FakeCommsApi comms;
  final calls = <String>[];
  final _driverCompensations = <DriverCompensation>[];

  /// Request bodies already seen per idempotency key (replays return the same booking).
  final _byKey = <String, String>{};
  int _seq = 0;

  /// Local start date that the machine is already booked on.
  DateTime? busyOn;

  final sitesList = <SiteAddress>[
    const SiteAddress(
      id: 'site-1',
      label: 'Wakad tower',
      line1: 'Plot 14, Wakad',
      city: 'Pune',
      stateCode: '27',
      pincode: '411057',
      isSite: true,
      lat: 18.59,
      lng: 73.76,
    ),
  ];

  String _st(String id) =>
      comms.statusOf[id] ??
      (comms.created.containsKey(id)
          ? 'requested'
          : (id == 'bk-2' ? 'completed' : 'payment_pending'));

  void _expect(String id, Set<String> from, String step) {
    if (!from.contains(_st(id))) {
      throw ApiException(
        kind: ApiErrorKind.conflict,
        code: 'INVALID_TRANSITION',
        message: 'cannot $step a ${_st(id)} booking',
        statusCode: 409,
      );
    }
  }

  @override
  Future<Estimate> estimate(String equipmentId, BookingChoice choice) async {
    final days = choice.dates.days;
    final rental = days * 1500000;
    final delivery = choice.delivery ? 300000 : 0;
    final operator = choice.withOperator ? days * 80000 : 0;
    final gst = ((rental + delivery + operator) * 18 / 100).round();
    final busy =
        busyOn != null && isoDate(choice.dates.start) == isoDate(busyOn!);
    final start = DateTime(
      choice.dates.start.year,
      choice.dates.start.month,
      choice.dates.start.day,
      choice.dates.startHour,
    );
    return Estimate(
      totalPaise: rental + delivery + operator + gst,
      charges: [
        ChargeLine(
          component: 'rental',
          label: 'Rental',
          detail: '$days day',
          paise: rental,
        ),
        if (delivery > 0)
          ChargeLine(component: 'delivery', label: 'Delivery', paise: delivery),
        if (operator > 0)
          ChargeLine(
            component: 'operator',
            label: 'Operator bata',
            paise: operator,
          ),
        ChargeLine(component: 'tax', label: 'GST 18%', paise: gst),
      ],
      depositPaise: 1000000,
      advanceDuePaise: ((rental + delivery + operator + gst) / 2).round(),
      dueOnConfirmationPaise: 0,
      unit: 'day',
      start: start,
      end: start.add(Duration(days: days)),
      availability: busy
          ? Availability(
              available: false,
              conflicts: [
                BusySlot(
                  start: start,
                  end: start.add(const Duration(days: 1)),
                  kind: 'booked',
                ),
              ],
              nextAvailableStart: start.add(const Duration(days: 2)),
            )
          : const Availability(available: true),
    );
  }

  @override
  Future<List<SiteAddress>> sites(String org) async => List.of(sitesList);

  @override
  Future<SiteAddress> addSite(String org, NewSite site) async {
    calls.add('site ${site.label} ${site.pincode}');
    final s = SiteAddress(
      id: 'site-${sitesList.length + 1}',
      label: site.label,
      line1: site.line1,
      city: site.city,
      stateCode: site.stateCode,
      pincode: site.pincode,
      isSite: true,
      lat: site.lat,
      lng: site.lng,
    );
    sitesList.add(s);
    return s;
  }

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
  }) async {
    final replay = _byKey[idempotencyKey];
    if (replay != null) return replay;
    if (busyOn != null && isoDate(choice.dates.start) == isoDate(busyOn!)) {
      throw const ApiException(
        kind: ApiErrorKind.conflict,
        code: 'BOOKING_OVERLAP',
        message: 'The machine is already booked for these dates.',
        statusCode: 409,
      );
    }
    final id = 'bk-new-${++_seq}';
    _byKey[idempotencyKey] = id;
    comms.created[id] = 'Tata Hitachi EX200';
    calls.add(
      'create $equipmentId $siteAddressId ${choice.dates.days}d op=${choice.withOperator} delivery=${choice.delivery} $paymentMode',
    );
    return id;
  }

  @override
  Future<void> acknowledgeInspection(String? org, String id, String stage) async {}

  @override
  Future<void> act(
    String? org,
    String path, {
    String? reason,
    int? version,
  }) async {
    final parts = path.split('/');
    final at = parts.first == 'bookings'
        ? 1
        : 2; // bookings/{id}/... or owner/bookings/{id}/...
    final id = parts[at];
    final step = parts.sublist(at + 1).join('/');
    calls.add('$step $id');
    final (from, to) = switch (step) {
      'dispatch' => ({'confirmed', 'operator_assigned'}, 'dispatched'),
      'arrived' => ({'dispatched'}, 'on_site'),
      'handover/confirm' => ({'on_site'}, 'active'),
      'return-request' => ({'active'}, 'return_initiated'),
      'mark-returned' => ({'return_initiated'}, 'returned'),
      'complete' => ({'returned'}, 'completed'),
      _ => throw StateError('unknown step $step'),
    };
    _expect(id, from, step);
    comms.moveTo(id, to);
  }

  @override
  Future<CancelOutcome> cancel(
    String? org,
    String id, {
    String? reason,
    bool dryRun = false,
    int? version,
  }) async {
    final st = _st(id);
    final approval = st == 'dispatched' || st == 'on_site';
    final paid = comms.paidOf[id] ?? 0;
    final fee = paid > 0 ? 500000 : 0;
    final to = approval
        ? 'cancellation_requested'
        : (paid > 0 ? 'refund_pending' : 'cancelled');
    if (!dryRun) {
      calls.add('cancel $id $reason');
      comms.moveTo(id, to);
    }
    return CancelOutcome(
      status: to,
      feePaise: fee,
      refundPaise: approval ? 0 : (paid - fee).clamp(0, paid),
      requiresApproval: approval,
    );
  }

  @override
  Future<void> answerCancellation(
    String? org,
    String id,
    String decision, {
    String? reason,
  }) async {
    _expect(id, {'cancellation_requested'}, '$decision cancellation');
    calls.add('cancellation $decision $id${reason == null ? '' : ' $reason'}');
    final previous = comms.historyOf[id]!.reversed.skip(1).first;
    comms.moveTo(
      id,
      decision == 'approve'
          ? ((comms.paidOf[id] ?? 0) > 0 ? 'refund_pending' : 'cancelled')
          : previous,
    );
  }

  @override
  Future<ExtensionQuote> quoteExtension(
    String? org,
    String id,
    DateTime newEnd,
  ) async =>
      ExtensionQuote(newEnd: newEnd, amountPaise: 1770000, available: true);

  @override
  Future<void> requestExtension(
    String? org,
    String id,
    DateTime newEnd, {
    String note = '',
  }) async {
    _expect(id, {'active'}, 'extend');
    calls.add('extend $id');
    comms.extensionOf[id] = ExtensionInfo(
      id: 'ext-1',
      requestedSide: org == 'org-owner' ? 'owner' : 'customer',
      previousEnd: newEnd.subtract(const Duration(days: 1)),
      newEnd: newEnd,
      amountPaise: 1770000,
      status: 'pending',
      note: note,
    );
    comms.moveTo(id, 'extension_requested');
  }

  @override
  Future<void> answerExtension(
    String? org,
    String id,
    String extensionId,
    String decision, {
    String? reason,
  }) async {
    _expect(id, {'extension_requested'}, '$decision extension');
    calls.add('extension $decision $id');
    comms.extensionOf.remove(id);
    comms.moveTo(id, 'active');
  }

  @override
  Future<List<OperatorOption>> operators(String org) async => const [
    OperatorOption(
      userId: 'op-1',
      name: 'Suresh Jadhav',
      onDuty: true,
      rating: 4.6,
    ),
    OperatorOption(
      userId: 'op-2',
      name: 'Imran Shaikh',
      onDuty: false,
      currentJob: 'ER-2611-XYZ1',
    ),
  ];

  @override
  Future<void> assignOperator(
    String? org,
    String id,
    String operatorUserId,
  ) async {
    _expect(id, {'confirmed', 'operator_assigned'}, 'assign');
    calls.add('assign $id $operatorUserId');
    comms.operatorOf[id] = operatorUserId == 'op-1'
        ? 'Suresh Jadhav'
        : 'Imran Shaikh';
    comms.moveTo(id, 'operator_assigned');
  }

  @override
  Future<Checkout> startPayment(String? org, String id, String purpose) async {
    _expect(id, {
      'accepted',
      'payment_pending',
      'payment_processing',
      'active',
      'returned',
      'completed',
    }, 'pay');
    calls.add('pay $id $purpose');
    if (purpose == 'advance') comms.moveTo(id, 'payment_processing');
    return Checkout(
      payment: BookingPayment(
        id: 'pay-$id',
        purpose: purpose,
        amountPaise: 2947000,
        status: 'created',
        createdAt: DateTime.now(),
      ),
      sandboxPaymentId: 'pay_sbx_1',
      sandboxSignature: 'a' * 64,
    );
  }

  @override
  Future<void> verifyPayment(
    String? org,
    String paymentId, {
    required String gatewayPaymentId,
    required String signature,
  }) async {
    final id = paymentId.substring(4);
    calls.add('verify $id');
    comms.paidOf[id] = (comms.paidOf[id] ?? 0) + 2947000;
    if (_st(id) == 'payment_processing') comms.moveTo(id, 'confirmed');
  }

  @override
  Future<List<BookingPayment>> payments(String? org, String id) async =>
      const [];

  @override
  Future<List<DeliveryPhoto>> deliveryPhotos(String? org, String id) async =>
      const [];

  @override
  Future<void> approveDeliveryPhoto(String? org, String id, String photoId) async {
    calls.add('approve-photo $id $photoId');
  }

  @override
  Future<DeliveryPhoto> addDeliveryPhoto(
    String? org,
    String id, {
    required String uploadId,
    required DateTime capturedAt,
    required String clientId,
  }) async {
    calls.add('delivery-photo $id $uploadId');
    return DeliveryPhoto(
      id: clientId,
      url: 'https://files.test/$uploadId',
      capturedAt: capturedAt,
      submittedAt: DateTime.now(),
      submittedByName: 'Customer',
    );
  }

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
  }) async {
    if (!dryRun) calls.add('final $id $operatingHours $damagePaise');
    return FinalBill(
      totalPaise: 3894000 + damagePaise,
      charges: const [
        ChargeLine(component: 'rental', label: 'Rental', paise: 3894000),
      ],
      saved: !dryRun,
    );
  }

  final _invoices = <InvoiceInfo>[];

  @override
  Future<List<InvoiceInfo>> invoices(String? org, String id) async =>
      List.of(_invoices);

  @override
  Future<List<InvoiceInfo>> orgInvoices(String? org) async =>
      List.of(_invoices);

  @override
  Future<void> issueInvoice(String? org, String id) async {
    _expect(id, {'returned', 'completed'}, 'invoice');
    calls.add('invoice $id');
    _invoices.add(
      InvoiceInfo(
        id: 'inv-1',
        number: 'ER/2627/00001',
        type: 'rental',
        totalPaise: 3894000,
        status: 'issued',
        issuedAt: DateTime.now(),
      ),
    );
  }

  @override
  Future<String> invoiceLink(String? org, String invoiceId) async =>
      'https://files.test/$invoiceId.pdf';

  final _credits = <String, List<InvoiceAdjustment>>{};

  // ---- compliance
  final taxRuleList = <TaxRule>[
    const TaxRule(
      id: 'tax-1',
      code: 'gst',
      name: 'GST',
      rateBps: 1800,
      appliesTo: ['rental', 'delivery'],
      sacCode: '997313',
      validFrom: '2017-07-01',
      isActive: true,
    ),
  ];
  final templateList = <LegalTemplate>[
    const LegalTemplate(
      id: 'tpl-1',
      version: 'rental-terms-2026.1',
      title: 'Equipment rental terms',
      status: 'published',
      missing: [],
      sections: {
        'Damage responsibility': [
          'The customer is responsible for damage beyond normal wear.',
        ],
      },
    ),
    const LegalTemplate(
      id: 'tpl-2',
      version: 'rental-terms-2026.2',
      title: 'Equipment rental terms',
      status: 'draft',
      missing: [],
      sections: {},
    ),
  ];

  @override
  Future<List<TaxRule>> taxRules() async => List.of(taxRuleList);

  @override
  Future<List<TaxRule>> reviewTaxRule(
    String id, {
    required String reviewedBy,
    String? legalBasis,
  }) async {
    calls.add('review-tax $id $reviewedBy ${legalBasis ?? ''}'.trim());
    final i = taxRuleList.indexWhere((t) => t.id == id);
    final t = taxRuleList[i];
    taxRuleList[i] = TaxRule(
      id: t.id,
      code: t.code,
      name: t.name,
      rateBps: t.rateBps,
      appliesTo: t.appliesTo,
      sacCode: t.sacCode,
      validFrom: t.validFrom,
      isActive: t.isActive,
      legalBasis: legalBasis,
      reviewedBy: reviewedBy,
      reviewedAt: DateTime.now(),
    );
    return List.of(taxRuleList);
  }

  @override
  Future<List<LegalTemplate>> legalTemplates() async => List.of(templateList);

  @override
  Future<LegalTemplate> reviewLegalTemplate(
    String id, {
    required String reviewedBy,
    required String capacity,
    String? reference,
  }) async {
    calls.add('review-template $id $reviewedBy $capacity');
    final i = templateList.indexWhere((t) => t.id == id);
    final t = templateList[i];
    return templateList[i] = LegalTemplate(
      id: t.id,
      version: t.version,
      title: t.title,
      status: t.status,
      missing: t.missing,
      sections: t.sections,
      reviewedBy: reviewedBy,
      reviewerCapacity: capacity,
      reviewedAt: DateTime.now(),
    );
  }

  @override
  Future<LegalTemplate> publishLegalTemplate(String id) async {
    calls.add('publish-template $id');
    for (var i = 0; i < templateList.length; i++) {
      final t = templateList[i];
      final status = t.id == id
          ? 'published'
          : (t.status == 'published' ? 'retired' : t.status);
      templateList[i] = LegalTemplate(
        id: t.id,
        version: t.version,
        title: t.title,
        status: status,
        missing: t.missing,
        sections: t.sections,
        reviewedBy: t.reviewedBy,
        reviewerCapacity: t.reviewerCapacity,
        reviewedAt: t.reviewedAt,
      );
    }
    return templateList.firstWhere((t) => t.id == id);
  }

  InvoiceDetail _detail(InvoiceInfo info) {
    final credits = _credits[info.id] ?? const <InvoiceAdjustment>[];
    final credited = credits.fold<int>(0, (s, a) => s + a.amountPaise);
    final subtotal = (info.totalPaise / 1.18).round();
    return InvoiceDetail(
      info: info,
      placeOfSupply: '27',
      supplyType: 'intra',
      subtotalPaise: subtotal,
      cgstPaise: (info.totalPaise - subtotal) ~/ 2,
      sgstPaise: (info.totalPaise - subtotal) ~/ 2,
      igstPaise: 0,
      lines: [
        InvoiceLine(
          lineNo: 1,
          description: 'Equipment rental',
          sacCode: '997313',
          quantity: 3,
          unit: 'day',
          ratePaise: subtotal ~/ 3,
          taxablePaise: subtotal,
          gstRateBps: 1800,
        ),
      ],
      payments: [
        InvoicePaymentRef(
          purpose: 'advance',
          amountPaise: 1947000,
          receiptNumber: 'RCPT-2026-0000001',
          method: 'upi',
          capturedAt: DateTime(2026, 11, 10),
        ),
      ],
      adjustments: credits,
      notes: const [],
      creditedPaise: credited,
      netPaise: info.totalPaise - credited,
      supplier: const InvoiceParty(
        legalName: 'Patil Earthmovers Pvt Ltd',
        gstin: '27ABCDE1234F1Z5',
        stateCode: '27',
      ),
      recipient: const InvoiceParty(
        legalName: 'Shree Builders LLP',
        stateCode: '27',
      ),
      bookingReference: 'ER-2611-BK1',
      equipmentTitle: 'JCB 3DX backhoe loader',
      registrationNo: 'MH12KX4471',
      rentalStart: DateTime(2026, 11, 10),
      rentalEnd: DateTime(2026, 11, 13),
      basis: 'final',
      originalNumber: info.isCreditNote
          ? _invoices
                .where((i) => i.id == info.originalInvoiceId)
                .firstOrNull
                ?.number
          : null,
    );
  }

  @override
  Future<InvoiceDetail> invoice(String? org, String invoiceId) async {
    calls.add('invoice-detail $invoiceId');
    return _detail(
      _invoices.firstWhere(
        (i) => i.id == invoiceId,
        orElse: () => InvoiceInfo(
          id: invoiceId,
          number: 'ER/2627/00009',
          type: 'rental',
          totalPaise: 3894000,
          status: 'issued',
          issuedAt: DateTime(2026, 11, 13),
        ),
      ),
    );
  }

  @override
  Future<InvoiceDetail> creditNote(
    String? org,
    String invoiceId, {
    required int amountPaise,
    required String reason,
  }) async {
    calls.add('credit $invoiceId $amountPaise $reason');
    final total = (amountPaise * 1.18).round();
    final note = InvoiceInfo(
      id: 'cn-${_invoices.length}',
      number: 'CN/2627/00001',
      type: 'credit_note',
      title: 'Credit note',
      totalPaise: total,
      status: 'issued',
      issuedAt: DateTime.now(),
      originalInvoiceId: invoiceId,
    );
    _invoices.add(note);
    _credits
        .putIfAbsent(invoiceId, () => [])
        .add(
          InvoiceAdjustment(
            kind: 'credit_note',
            amountPaise: total,
            status: 'issued',
            reference: note.number,
            reason: reason,
          ),
        );
    return _detail(note);
  }

  @override
  Future<void> dispute(
    String? org,
    String id, {
    required String category,
    required String summary,
  }) async {
    calls.add('dispute $id $category');
    comms.moveTo(id, 'disputed');
  }

  @override
  Future<DriverCompensation> recordDriverCompensation(
    String? org,
    String bookingId,
    int agreedAmountPaise,
  ) async {
    final item = DriverCompensation(
      id: 'driver-comp-${_driverCompensations.length + 1}',
      bookingId: bookingId,
      driverUserId: 'operator-1',
      driverName: 'Suresh Jadhav',
      agreedAmountPaise: agreedAmountPaise,
      status: 'recorded',
    );
    _driverCompensations.add(item);
    calls.add('record driver compensation $bookingId $agreedAmountPaise');
    return item;
  }

  @override
  Future<List<DriverCompensation>> driverCompensations(String? org) async =>
      List.of(_driverCompensations);

  @override
  Future<DriverCompensation> payDriverCompensation(
    String? org,
    String id,
    String transactionReference,
  ) async {
    final index = _driverCompensations.indexWhere((item) => item.id == id);
    if (index < 0) {
      throw const ApiException(
        kind: ApiErrorKind.notFound,
        code: 'NOT_FOUND',
        message: 'Driver compensation not found.',
        statusCode: 404,
      );
    }
    final previous = _driverCompensations[index];
    final paid = DriverCompensation(
      id: previous.id,
      bookingId: previous.bookingId,
      driverUserId: previous.driverUserId,
      driverName: previous.driverName,
      agreedAmountPaise: previous.agreedAmountPaise,
      status: 'paid',
      transactionReference: transactionReference,
      paidAt: DateTime.now(),
    );
    _driverCompensations[index] = paid;
    calls.add('pay driver compensation $id $transactionReference');
    return paid;
  }
}
