// Models of the booking engine: estimates, availability, work sites,
// payments, cancellation previews, extensions, tracking, invoices and the
// parts of the booking detail that depend on the caller. Amounts are paise.

import 'package:flutter/foundation.dart';

typedef Json = Map<String, dynamic>;

Json _j(Object? v) => v == null ? const {} : (v as Map).cast<String, dynamic>();
List<Json> _list(Object? v) => [
  for (final e in (v as List? ?? const [])) _j(e),
];
DateTime? _dt(Object? v) =>
    v == null ? null : DateTime.parse(v as String).toLocal();
DateTime _dtReq(Object? v) => DateTime.parse(v as String).toLocal();
int _int(Object? v) => (v as num?)?.toInt() ?? 0;
double _dbl(Object? v) => (v as num?)?.toDouble() ?? 0;
String? _str(Object? v) => v?.toString();

/// `2026-11-14` for a local date.
String isoDate(DateTime d) =>
    '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

/// `08:00` for a time of day.
String hhmm(int hour, [int minute = 0]) =>
    '${hour.toString().padLeft(2, '0')}:${minute.toString().padLeft(2, '0')}';

// ---- choosing a slot -----------------------------------------------------------------------------

/// Local rental dates in the listing's time zone (inclusive): 12 to 14 March
/// from 08:00 is three days, ending 15 March 08:00.
@immutable
class RentalDates {
  const RentalDates({
    required this.start,
    required this.end,
    this.startHour = 8,
  });
  final DateTime start;
  final DateTime end;
  final int startHour;

  int get days =>
      end.difference(DateTime(start.year, start.month, start.day)).inDays + 1;

  Json toJson() => {
    'start_date': isoDate(start),
    'end_date': isoDate(end),
    'start_time': hhmm(startHour),
  };

  @override
  bool operator ==(Object other) =>
      other is RentalDates &&
      isoDate(other.start) == isoDate(start) &&
      isoDate(other.end) == isoDate(end) &&
      other.startHour == startHour;
  @override
  int get hashCode => Object.hash(isoDate(start), isoDate(end), startHour);
}

/// What the customer picks (the same fields price an estimate and create a booking).
@immutable
class BookingChoice {
  const BookingChoice({
    required this.dates,
    this.unit = 'auto',
    this.withOperator = false,
    this.delivery = true,
    this.pickup = true,
    this.expectedHours = 0,
    this.site,
  });

  final RentalDates dates;
  final String unit;
  final bool withOperator;
  final bool delivery;
  final bool pickup;
  final double expectedHours;
  final ({double lat, double lng})? site;

  BookingChoice copyWith({
    RentalDates? dates,
    String? unit,
    bool? withOperator,
    bool? delivery,
    bool? pickup,
    double? expectedHours,
    ({double lat, double lng})? site,
  }) => BookingChoice(
    dates: dates ?? this.dates,
    unit: unit ?? this.unit,
    withOperator: withOperator ?? this.withOperator,
    delivery: delivery ?? this.delivery,
    pickup: pickup ?? this.pickup,
    expectedHours: expectedHours ?? this.expectedHours,
    site: site ?? this.site,
  );

  Json toEstimateJson() => {
    'dates': dates.toJson(),
    'pricing_unit': unit,
    'with_operator': withOperator,
    'delivery': delivery,
    'pickup': pickup,
    if (expectedHours > 0) 'expected_hours': expectedHours,
    if (site != null) 'site': {'lat': site!.lat, 'lng': site!.lng},
  };

  @override
  bool operator ==(Object other) =>
      other is BookingChoice &&
      other.dates == dates &&
      other.unit == unit &&
      other.withOperator == withOperator &&
      other.delivery == delivery &&
      other.pickup == pickup &&
      other.expectedHours == expectedHours &&
      other.site == site;
  @override
  int get hashCode => Object.hash(
    dates,
    unit,
    withOperator,
    delivery,
    pickup,
    expectedHours,
    site,
  );
}

// ---- availability and estimate ---------------------------------------------------------------------

@immutable
class BusySlot {
  const BusySlot({required this.start, required this.end, required this.kind});
  factory BusySlot.fromJson(Json j) => BusySlot(
    start: _dtReq(j['start']),
    end: _dtReq(j['end']),
    kind: '${j['kind']}',
  );
  final DateTime start;
  final DateTime end;
  final String kind; // booked or blocked
}

@immutable
class Availability {
  const Availability({
    required this.available,
    this.conflicts = const [],
    this.ruleProblem,
    this.nextAvailableStart,
    this.start,
    this.end,
  });
  factory Availability.fromJson(Json j) => Availability(
    available: j['available'] == true,
    conflicts: [for (final c in _list(j['conflicts'])) BusySlot.fromJson(c)],
    ruleProblem: (j['rule_problem'] as String?)?.isEmpty ?? true
        ? null
        : j['rule_problem'] as String,
    nextAvailableStart: _dt(j['next_available_start']),
    start: _dt(_j(j['period'])['start']),
    end: _dt(_j(j['period'])['end']),
  );
  final bool available;
  final List<BusySlot> conflicts;
  final String? ruleProblem;
  final DateTime? nextAvailableStart;
  final DateTime? start;
  final DateTime? end;

  /// Why the slot cannot be booked, in words.
  String? get problem {
    if (available) return null;
    if (ruleProblem != null) return ruleProblem;
    if (conflicts.any((c) => c.kind == 'booked')) {
      return 'The machine is already booked for part of these dates.';
    }
    if (conflicts.isNotEmpty) {
      return 'The owner has blocked part of these dates.';
    }
    return 'These dates are not available.';
  }
}

@immutable
class ChargeLine {
  const ChargeLine({
    required this.component,
    required this.label,
    required this.paise,
    this.detail = '',
  });
  factory ChargeLine.fromJson(Json j) => ChargeLine(
    component: '${j['component']}',
    label: '${j['label']}',
    paise: _int(j['paise']),
    detail: _str(j['detail']) ?? '',
  );
  final String component;
  final String label;
  final String detail;
  final int paise;
}

/// A server-computed price (an estimate, or the frozen price of a booking).
@immutable
class Estimate {
  const Estimate({
    required this.totalPaise,
    required this.charges,
    required this.depositPaise,
    required this.advanceDuePaise,
    required this.dueOnConfirmationPaise,
    required this.unit,
    this.notes = const [],
    this.availability,
    this.start,
    this.end,
    this.timezone,
  });

  factory Estimate.fromJson(Json j) => Estimate(
    totalPaise: _int(j['estimated_total_paise']),
    charges: [for (final c in _list(j['charges'])) ChargeLine.fromJson(c)],
    depositPaise: _int(j['deposit_paise']),
    advanceDuePaise: _int(j['advance_due_paise']),
    dueOnConfirmationPaise: _int(j['due_on_confirmation_paise']),
    unit: _str(j['unit']) ?? '',
    notes: [for (final n in (j['notes'] as List? ?? const [])) '$n'],
    availability: j['availability'] == null
        ? null
        : Availability.fromJson(_j(j['availability'])),
    start: _dt(_j(j['period'])['start']),
    end: _dt(_j(j['period'])['end']),
    timezone: _str(_j(j['period'])['timezone']),
  );

  final int totalPaise;
  final List<ChargeLine> charges;
  final int depositPaise;
  final int advanceDuePaise;
  final int dueOnConfirmationPaise;
  final String unit;
  final List<String> notes;
  final Availability? availability;
  final DateTime? start;
  final DateTime? end;
  final String? timezone;
}

// ---- sites -------------------------------------------------------------------------------------------

@immutable
class SiteAddress {
  const SiteAddress({
    required this.id,
    required this.label,
    required this.line1,
    required this.city,
    required this.stateCode,
    required this.pincode,
    required this.isSite,
    this.lat,
    this.lng,
    this.locality,
    this.district,
  });

  factory SiteAddress.fromJson(Json j) {
    final loc = j['location'] == null ? null : _j(j['location']);
    return SiteAddress(
      id: '${j['id']}',
      label: '${j['label']}',
      line1: '${j['line1']}',
      city: '${j['city']}',
      stateCode: '${j['state_code']}',
      pincode: '${j['pincode']}',
      isSite: j['is_site'] == true,
      lat: loc == null ? null : _dbl(loc['lat']),
      lng: loc == null ? null : _dbl(loc['lng']),
      locality: _str(j['locality']),
      district: _str(j['district']),
    );
  }

  final String id;
  final String label;
  final String line1;
  final String? locality;
  final String? district;
  final String city;
  final String stateCode;
  final String pincode;
  final bool isSite;
  final double? lat;
  final double? lng;

  String get oneLine => [
    line1,
    if (locality != null && locality!.isNotEmpty) locality!,
    city,
    pincode,
  ].join(', ');
  ({double lat, double lng})? get point =>
      lat == null || lng == null ? null : (lat: lat!, lng: lng!);
}

/// A new work site.
@immutable
class NewSite {
  const NewSite({
    required this.label,
    required this.line1,
    required this.city,
    required this.stateCode,
    required this.pincode,
    required this.lat,
    required this.lng,
    this.district,
  });
  final String label;
  final String line1;
  final String city;
  final String stateCode;
  final String pincode;
  final String? district;
  final double lat;
  final double lng;

  Json toJson() => {
    'label': label,
    'line1': line1,
    'city': city,
    if (district != null && district!.isNotEmpty) 'district': district,
    'state_code': stateCode,
    'pincode': pincode,
    'is_site': true,
    'location': {'lat': lat, 'lng': lng},
  };
}

// ---- booking detail parts ---------------------------------------------------------------------------

@immutable
class BookingEvent {
  const BookingEvent({
    required this.to,
    required this.at,
    this.from,
    this.actorRole,
    this.reason,
  });
  factory BookingEvent.fromJson(Json j) => BookingEvent(
    from: _str(j['from']),
    to: '${j['to']}',
    actorRole: _str(j['actor_role']),
    reason: _str(j['reason']),
    at: _dtReq(j['at']),
  );
  final String? from;
  final String to;
  final String? actorRole;
  final String? reason;
  final DateTime at;
}

@immutable
class OperatorOnJob {
  const OperatorOnJob({
    required this.userId,
    required this.name,
    required this.status,
    this.phone,
  });
  factory OperatorOnJob.fromJson(Json j) => OperatorOnJob(
    userId: '${j['user_id']}',
    name: '${j['name']}',
    status: '${j['status']}',
    phone: _str(j['phone']),
  );
  final String userId;
  final String name;
  final String status;
  final String? phone;
}

@immutable
class CustomerInfo {
  const CustomerInfo({
    required this.orgName,
    required this.contactName,
    required this.kycStatus,
    required this.completedRentals,
    this.phone,
    this.customerType = 'individual',
    this.companyName,
    this.email,
    this.address,
    this.city,
    this.state,
    this.pinCode,
    this.gstin,
  });
  factory CustomerInfo.fromJson(Json j) => CustomerInfo(
    orgName: '${j['org_name'] ?? ''}',
    contactName: '${j['contact_name'] ?? ''}',
    phone: (j['phone'] as String?)?.isEmpty ?? true
        ? null
        : j['phone'] as String,
    kycStatus: '${j['kyc_status'] ?? ''}',
    completedRentals: _int(j['completed_rentals']),
    customerType: '${j['customer_type'] ?? 'individual'}',
    companyName: _opt(j['company_name']),
    email: _opt(j['email']),
    address: _opt(j['address']),
    city: _opt(j['city']),
    state: _opt(j['state']),
    pinCode: _opt(j['pin_code']),
    gstin: _opt(j['gstin']),
  );
  static String? _opt(Object? v) => v is String && v.isNotEmpty ? v : null;
  final String customerType;
  final String? companyName, email, address, city, state, pinCode, gstin;
  bool get isCompany => customerType == 'company';
  final String orgName;
  final String contactName;
  final String? phone;
  final String kycStatus;
  final int completedRentals;
}

@immutable
class RefundInfo {
  const RefundInfo({
    required this.amountPaise,
    required this.status,
    required this.reason,
    required this.createdAt,
  });
  factory RefundInfo.fromJson(Json j) => RefundInfo(
    amountPaise: _int(j['amount_paise']),
    status: '${j['status']}',
    reason: '${j['reason'] ?? ''}',
    createdAt: _dtReq(j['created_at']),
  );
  final int amountPaise;
  final String status;
  final String reason;
  final DateTime createdAt;
}

@immutable
class MoneySummary {
  const MoneySummary({
    required this.advanceDuePaise,
    required this.depositPaise,
    required this.paidPaise,
    required this.refundedPaise,
    required this.balanceDuePaise,
    this.paymentDueAt,
    this.refunds = const [],
  });
  factory MoneySummary.fromJson(Json j) => MoneySummary(
    advanceDuePaise: _int(j['advance_due_paise']),
    depositPaise: _int(j['deposit_paise']),
    paidPaise: _int(j['paid_paise']),
    refundedPaise: _int(j['refunded_paise']),
    balanceDuePaise: _int(j['balance_due_paise']),
    paymentDueAt: _dt(j['payment_due_at']),
    refunds: [for (final r in _list(j['refunds'])) RefundInfo.fromJson(r)],
  );
  final int advanceDuePaise;
  final int depositPaise;
  final int paidPaise;
  final int refundedPaise;
  final int balanceDuePaise;
  final DateTime? paymentDueAt;
  final List<RefundInfo> refunds;
}

@immutable
class ExtensionInfo {
  const ExtensionInfo({
    required this.id,
    required this.requestedSide,
    required this.previousEnd,
    required this.newEnd,
    required this.amountPaise,
    required this.status,
    this.note = '',
    this.responseReason,
  });
  factory ExtensionInfo.fromJson(Json j) => ExtensionInfo(
    id: '${j['id']}',
    requestedSide: '${j['requested_side']}',
    previousEnd: _dtReq(j['previous_end']),
    newEnd: _dtReq(j['new_end']),
    amountPaise: _int(j['amount_paise']),
    status: '${j['status']}',
    note: '${j['note'] ?? ''}',
    responseReason: _str(j['response_reason']),
  );
  final String id;
  final String requestedSide;
  final DateTime previousEnd;
  final DateTime newEnd;
  final int amountPaise;
  final String status;
  final String note;
  final String? responseReason;
}

@immutable
class ExtensionQuote {
  const ExtensionQuote({
    required this.newEnd,
    required this.amountPaise,
    required this.available,
    this.problem,
  });
  factory ExtensionQuote.fromJson(Json j) {
    final a = Availability.fromJson(_j(j['availability']));
    return ExtensionQuote(
      newEnd: _dtReq(j['new_end']),
      amountPaise: _int(j['amount_paise']),
      available: a.available,
      problem: a.problem,
    );
  }
  final DateTime newEnd;
  final int amountPaise;
  final bool available;
  final String? problem;
}

@immutable
class CancellationView {
  const CancellationView({required this.summary, required this.feeNowPaise});
  factory CancellationView.fromJson(Json j) => CancellationView(
    summary: '${j['summary'] ?? ''}',
    feeNowPaise: _int(j['fee_now_paise']),
  );
  final String summary;
  final int feeNowPaise;
}

/// A cancellation or its preview.
@immutable
class CancelOutcome {
  const CancelOutcome({
    required this.status,
    required this.feePaise,
    required this.refundPaise,
    required this.requiresApproval,
  });
  factory CancelOutcome.fromJson(Json j) => CancelOutcome(
    status: '${j['status']}',
    feePaise: _int(j['fee_paise']),
    refundPaise: _int(j['refund_paise']),
    requiresApproval: j['requires_approval'] == true,
  );
  final String status;
  final int feePaise;
  final int refundPaise;
  final bool requiresApproval;
}

// ---- payments ----------------------------------------------------------------------------------------

@immutable
class BookingPayment {
  const BookingPayment({
    required this.id,
    required this.purpose,
    required this.amountPaise,
    required this.status,
    required this.createdAt,
    this.capturedAt,
    this.method,
    this.receiptNumber,
  });
  factory BookingPayment.fromJson(Json j) => BookingPayment(
    id: '${j['id']}',
    purpose: '${j['purpose']}',
    amountPaise: _int(j['amount_paise']),
    status: '${j['status']}',
    createdAt: _dtReq(j['created_at']),
    capturedAt: _dt(j['captured_at']),
    method: _str(j['method']),
    receiptNumber: _str(j['receipt_number']),
  );
  final String id;
  final String purpose;
  final int amountPaise;
  final String status;
  final DateTime createdAt;
  final DateTime? capturedAt;
  final String? method;
  final String? receiptNumber;
}

@immutable
class DeliveryPhoto {
  const DeliveryPhoto({
    required this.id,
    required this.url,
    required this.capturedAt,
    required this.submittedAt,
    required this.submittedByName,
    this.approvedAt,
  });
  factory DeliveryPhoto.fromJson(Json j) => DeliveryPhoto(
    id: '${j['id']}',
    url: '${j['url']}',
    capturedAt: _dtReq(j['captured_at']),
    submittedAt: _dtReq(j['submitted_at']),
    submittedByName: '${j['submitted_by_name'] ?? ''}',
    approvedAt: j['approved_at'] == null ? null : _dtReq(j['approved_at']),
  );
  final DateTime? approvedAt;
  bool get approved => approvedAt != null;
  final String id;
  final String url;
  final DateTime capturedAt;
  final DateTime submittedAt;
  final String submittedByName;
}

/// A started checkout. In sandbox mode the server also returns the values a
/// real gateway would hand back, so the payment can be completed without one.
@immutable
class Checkout {
  const Checkout({
    required this.payment,
    this.options = const {},
    this.sandboxPaymentId,
    this.sandboxSignature,
  });
  factory Checkout.fromJson(Json j) {
    final s = j['sandbox'] == null ? null : _j(j['sandbox']);
    return Checkout(
      payment: BookingPayment.fromJson(_j(j['payment'])),
      options: j['checkout'] == null ? const {} : _j(j['checkout']),
      sandboxPaymentId: _str(s?['gateway_payment_id']),
      sandboxSignature: _str(s?['signature']),
    );
  }
  final BookingPayment payment;

  /// Public checkout parameters from the server. Never contains the API secret.
  final Json options;
  final String? sandboxPaymentId;
  final String? sandboxSignature;
  bool get sandbox => sandboxPaymentId != null && sandboxSignature != null;
}

// ---- tracking, invoices, operators -----------------------------------------------------------------

@immutable
class InvoiceInfo {
  const InvoiceInfo({
    required this.id,
    required this.number,
    required this.type,
    required this.totalPaise,
    required this.status,
    this.issuedAt,
    this.title = 'Tax invoice',
    this.originalInvoiceId,
  });
  factory InvoiceInfo.fromJson(Json j) => InvoiceInfo(
    id: '${j['id']}',
    number: '${j['invoice_number']}',
    type: '${j['invoice_type']}',
    totalPaise: _int(j['total_paise']),
    status: '${j['status']}',
    issuedAt: _dt(j['issued_at']),
    title: _str(j['title']) ?? 'Tax invoice',
    originalInvoiceId: _str(j['original_invoice_id']),
  );
  final String id;
  final String number;
  final String type;
  final int totalPaise;
  final String status;
  final DateTime? issuedAt;
  final String title;
  final String? originalInvoiceId;

  bool get isCreditNote => type == 'credit_note';
}

/// A supplier or recipient as printed on an invoice.
@immutable
class InvoiceParty {
  const InvoiceParty({
    required this.legalName,
    this.displayName,
    this.gstin,
    this.address,
    this.stateCode,
    this.contact,
  });
  factory InvoiceParty.fromJson(Json j) => InvoiceParty(
    legalName: _str(j['legal_name']) ?? '',
    displayName: _str(j['display_name']),
    gstin: _str(j['gstin']),
    address: _str(j['address']),
    stateCode: _str(j['state_code']),
    contact: [
      j['contact_name'],
      j['contact_phone'],
    ].whereType<String>().join(' ').trim(),
  );
  final String legalName;
  final String? displayName;
  final String? gstin;
  final String? address;
  final String? stateCode;
  final String? contact;
}

@immutable
class InvoiceLine {
  const InvoiceLine({
    required this.lineNo,
    required this.description,
    required this.sacCode,
    required this.quantity,
    required this.unit,
    required this.ratePaise,
    required this.taxablePaise,
    required this.gstRateBps,
  });
  factory InvoiceLine.fromJson(Json j) => InvoiceLine(
    lineNo: _int(j['line_no']),
    description: _str(j['description']) ?? '',
    sacCode: _str(j['sac_code']) ?? '',
    quantity: _dbl(j['quantity']),
    unit: _str(j['unit']) ?? '',
    ratePaise: _int(j['rate_paise']),
    taxablePaise: _int(j['taxable_paise']),
    gstRateBps: _int(j['gst_rate_bps']),
  );
  final int lineNo;
  final String description;
  final String sacCode;
  final double quantity;
  final String unit;
  final int ratePaise;
  final int taxablePaise;
  final int gstRateBps;
}

@immutable
class InvoicePaymentRef {
  const InvoicePaymentRef({
    required this.purpose,
    required this.amountPaise,
    this.receiptNumber,
    this.gatewayPaymentId,
    this.method,
    this.capturedAt,
  });
  factory InvoicePaymentRef.fromJson(Json j) => InvoicePaymentRef(
    purpose: _str(j['purpose']) ?? '',
    amountPaise: _int(j['amount_paise']),
    receiptNumber: _str(j['receipt_number']),
    gatewayPaymentId: _str(j['gateway_payment_id']),
    method: _str(j['method']),
    capturedAt: _dt(j['captured_at']),
  );
  final String purpose;
  final int amountPaise;
  final String? receiptNumber;
  final String? gatewayPaymentId;
  final String? method;
  final DateTime? capturedAt;
}

/// A refund on the booking or a credit note against the invoice.
@immutable
class InvoiceAdjustment {
  const InvoiceAdjustment({
    required this.kind,
    required this.amountPaise,
    required this.status,
    this.reference,
    this.reason,
    this.at,
  });
  factory InvoiceAdjustment.fromJson(Json j) => InvoiceAdjustment(
    kind: _str(j['kind']) ?? '',
    amountPaise: _int(j['amount_paise']),
    status: _str(j['status']) ?? '',
    reference: _str(j['reference']),
    reason: _str(j['reason']),
    at: _dt(j['at']),
  );
  final String kind; // refund | credit_note
  final int amountPaise;
  final String status;
  final String? reference;
  final String? reason;
  final DateTime? at;
}

/// An invoice, bill of supply or credit note with its frozen details.
@immutable
class InvoiceDetail {
  const InvoiceDetail({
    required this.info,
    required this.placeOfSupply,
    required this.supplyType,
    required this.subtotalPaise,
    required this.cgstPaise,
    required this.sgstPaise,
    required this.igstPaise,
    required this.lines,
    required this.payments,
    required this.adjustments,
    required this.notes,
    required this.creditedPaise,
    required this.netPaise,
    this.supplier,
    this.recipient,
    this.bookingReference,
    this.equipmentTitle,
    this.registrationNo,
    this.rentalStart,
    this.rentalEnd,
    this.site,
    this.basis,
    this.originalNumber,
    this.reason,
  });

  factory InvoiceDetail.fromJson(Json j) {
    final d = _j(j['details']);
    final eq = _j(d['equipment']);
    final rental = _j(d['rental']);
    return InvoiceDetail(
      info: InvoiceInfo.fromJson(j),
      placeOfSupply: _str(j['place_of_supply']) ?? '',
      supplyType: _str(j['supply_type']) ?? 'intra',
      subtotalPaise: _int(j['subtotal_paise']),
      cgstPaise: _int(j['cgst_paise']),
      sgstPaise: _int(j['sgst_paise']),
      igstPaise: _int(j['igst_paise']),
      lines: [for (final l in _list(j['lines'])) InvoiceLine.fromJson(l)],
      payments: [
        for (final p in _list(d['payments'])) InvoicePaymentRef.fromJson(p),
      ],
      adjustments: [
        for (final a in _list(j['adjustments'])) InvoiceAdjustment.fromJson(a),
      ],
      notes: [for (final n in (d['notes'] as List? ?? const [])) '$n'],
      creditedPaise: _int(j['credited_paise']),
      netPaise: j['net_paise'] == null
          ? _int(j['total_paise'])
          : _int(j['net_paise']),
      supplier: d['supplier'] == null
          ? null
          : InvoiceParty.fromJson(_j(d['supplier'])),
      recipient: d['recipient'] == null
          ? null
          : InvoiceParty.fromJson(_j(d['recipient'])),
      bookingReference: _str(d['booking_reference']),
      equipmentTitle: _str(eq['title']),
      registrationNo: _str(eq['registration_no']),
      rentalStart: _dt(rental['start']),
      rentalEnd: _dt(rental['end']),
      site: _str(rental['site']),
      basis: _str(d['basis']),
      originalNumber: _str(d['original_invoice_number']),
      reason: _str(j['reason']),
    );
  }

  final InvoiceInfo info;
  final String placeOfSupply;
  final String supplyType;
  final int subtotalPaise;
  final int cgstPaise;
  final int sgstPaise;
  final int igstPaise;
  final List<InvoiceLine> lines;
  final List<InvoicePaymentRef> payments;
  final List<InvoiceAdjustment> adjustments;
  final List<String> notes;
  final int creditedPaise;
  final int netPaise;
  final InvoiceParty? supplier;
  final InvoiceParty? recipient;
  final String? bookingReference;
  final String? equipmentTitle;
  final String? registrationNo;
  final DateTime? rentalStart;
  final DateTime? rentalEnd;
  final String? site;
  final String? basis;
  final String? originalNumber;
  final String? reason;

  int get taxPaise => cgstPaise + sgstPaise + igstPaise;

  /// Taxable value still available for credit notes.
  int get creditableTaxablePaise {
    if (info.isCreditNote || subtotalPaise == 0) return 0;
    final credited = adjustments
        .where((a) => a.kind == 'credit_note' && a.status == 'issued')
        .fold<int>(0, (s, a) => s + a.amountPaise);
    final total = info.totalPaise == 0 ? 1 : info.totalPaise;
    final creditedTaxable = (credited * subtotalPaise / total).round();
    return (subtotalPaise - creditedTaxable).clamp(0, subtotalPaise);
  }
}

@immutable
class OperatorOption {
  const OperatorOption({
    required this.userId,
    required this.name,
    required this.onDuty,
    this.availability = 'available',
    this.phone,
    this.currentJob,
    this.rating,
  });
  factory OperatorOption.fromJson(Json j) => OperatorOption(
    userId: '${j['user_id']}',
    name: '${j['full_name']}',
    onDuty: j['on_duty'] == true,
    availability:
        _str(j['availability']) ??
        (j['on_duty'] == true ? 'available' : 'off_duty'),
    phone: _str(j['phone']),
    currentJob: (j['current_booking_reference'] as String?)?.isEmpty ?? true
        ? null
        : j['current_booking_reference'] as String,
    rating: (j['rating_avg'] as num?)?.toDouble(),
  );
  final String userId;
  final String name;
  final bool onDuty;
  final String availability;
  final String? phone;
  final String? currentJob;
  final double? rating;
}

@immutable
class DriverCompensation {
  const DriverCompensation({
    required this.id,
    required this.bookingId,
    required this.driverUserId,
    required this.driverName,
    required this.agreedAmountPaise,
    required this.status,
    this.driverPhone,
    this.transactionReference,
    this.paidAt,
  });
  factory DriverCompensation.fromJson(Json j) => DriverCompensation(
    id: '${j['id']}',
    bookingId: '${j['booking_id']}',
    driverUserId: '${j['driver_user_id']}',
    driverName: '${j['driver_name'] ?? ''}',
    driverPhone: j['driver_phone'] as String?,
    agreedAmountPaise: _int(j['agreed_amount_paise']),
    status: '${j['status']}',
    transactionReference: j['transaction_reference'] as String?,
    paidAt: _dt(j['paid_at']),
  );
  final String id, bookingId, driverUserId, driverName, status;
  final String? driverPhone, transactionReference;
  final int agreedAmountPaise;
  final DateTime? paidAt;
}

/// The settled bill after a rental (or its preview).
@immutable
class FinalBill {
  const FinalBill({
    required this.totalPaise,
    required this.charges,
    required this.saved,
  });
  factory FinalBill.fromJson(Json j) => FinalBill(
    totalPaise: _int(
      j['final_total_paise'] ?? j['total_paise'] ?? j['estimated_total_paise'],
    ),
    charges: [for (final c in _list(j['charges'])) ChargeLine.fromJson(c)],
    saved: j['saved'] == true,
  );
  final int totalPaise;
  final List<ChargeLine> charges;
  final bool saved;
}

/// Human labels for history entries.
String bookingEventLabel(String to) => switch (to) {
  'draft' => 'Saved as draft',
  'requested' => 'Booking requested',
  'accepted' => 'Accepted by the owner',
  'rejected' => 'Declined by the owner',
  'payment_pending' => 'Awaiting payment',
  'payment_processing' => 'Payment processing',
  'confirmed' => 'Booking confirmed',
  'operator_assigned' => 'Operator assigned',
  'dispatched' => 'Machine dispatched',
  'on_site' => 'Arrived on site',
  'active' => 'Rental started',
  'extension_requested' => 'Extension requested',
  'return_initiated' => 'Return requested',
  'returned' => 'Machine returned',
  'completed' => 'Rental completed',
  'closed' => 'Closed',
  'expired' => 'Expired',
  'cancellation_requested' => 'Cancellation requested',
  'cancelled' => 'Cancelled',
  'refund_pending' => 'Refund started',
  'refunded' => 'Refunded',
  'disputed' => 'Dispute raised',
  _ => to,
};

/// Human label for a driver availability value from the API.
String availabilityLabel(String a) => switch (a) {
  'available' => 'Available',
  'assigned' => 'Assigned',
  'on_job' => 'On job',
  'off_duty' => 'Off duty',
  _ => 'Unavailable',
};
