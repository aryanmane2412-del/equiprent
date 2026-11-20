import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';

typedef _J = Map<String, dynamic>;

_J _j(Object? v) =>
    v == null ? <String, dynamic>{} : (v as Map).cast<String, dynamic>();
List<_J> _list(Object? v) => [for (final e in (v as List? ?? const [])) _j(e)];
DateTime _dtReq(Object? v) => DateTime.parse(v as String).toLocal();
String? _s(Object? v) => v == null || '$v'.isEmpty ? null : '$v';
double _d(Object? v) => (v as num?)?.toDouble() ?? 0;

// ---- models ---------------------------------------------------------------------------------------

/// One job in the operator's lists.
@immutable
class JobSummary {
  const JobSummary({
    required this.bookingId,
    required this.reference,
    required this.assignmentStatus,
    required this.bookingStatus,
    required this.start,
    required this.end,
    required this.equipmentTitle,
    required this.site,
    required this.customerName,
    this.registrationNo,
    this.driverPaymentPaise = 0,
    this.driverPaymentStatus = 'not_set',
  });
  factory JobSummary.fromJson(Map<String, dynamic> j) => JobSummary(
    bookingId: '${j['booking_id']}',
    reference: '${j['reference']}',
    assignmentStatus: '${j['assignment_status']}',
    bookingStatus: '${j['booking_status']}',
    start: _dtReq(j['start']),
    end: _dtReq(j['end']),
    equipmentTitle: '${j['equipment_title']}',
    site: [
      j['site_line1'],
      j['site_city'],
    ].whereType<String>().where((s) => s.isNotEmpty).join(', '),
    customerName: _s(j['customer_name']) ?? '',
    registrationNo: _s(j['registration_no']),
    driverPaymentPaise: (j['driver_payment_paise'] as num?)?.toInt() ?? 0,
    driverPaymentStatus: '${j['driver_payment_status'] ?? 'not_set'}',
  );
  final String bookingId;
  final String reference;
  final String assignmentStatus;
  final String bookingStatus;
  final DateTime start;
  final DateTime end;
  final String equipmentTitle;
  final String site;
  final String customerName;
  final String? registrationNo;
  final int driverPaymentPaise;
  final String driverPaymentStatus;

  bool get awaitingAnswer => assignmentStatus == 'assigned';
}

@immutable
class DriverPayment {
  const DriverPayment({
    required this.id,
    required this.bookingId,
    required this.amountPaise,
    required this.status,
    required this.createdAt,
    this.reference,
    this.paidAt,
  });
  factory DriverPayment.fromJson(Map<String, dynamic> j) => DriverPayment(
    id: '${j['id']}',
    bookingId: '${j['booking_id']}',
    amountPaise: (j['agreed_amount_paise'] as num?)?.toInt() ?? 0,
    status: '${j['status']}',
    createdAt: _dtReq(j['created_at']),
    reference: _s(j['transaction_reference']),
    paidAt: j['paid_at'] == null ? null : _dtReq(j['paid_at']),
  );
  final String id, bookingId, status;
  final int amountPaise;
  final DateTime createdAt;
  final String? reference;
  final DateTime? paidAt;
}

@immutable
class DriverPaymentEvent {
  const DriverPaymentEvent({
    required this.status,
    required this.amountPaise,
    required this.createdAt,
    this.reference,
  });
  factory DriverPaymentEvent.fromJson(Map<String, dynamic> j) =>
      DriverPaymentEvent(
        status: '${j['status']}',
        amountPaise: (j['amount_paise'] as num?)?.toInt() ?? 0,
        createdAt: _dtReq(j['created_at']),
        reference: _s(j['transaction_reference']),
      );
  final String status;
  final int amountPaise;
  final DateTime createdAt;
  final String? reference;
}

@immutable
class DriverPaymentHistory {
  const DriverPaymentHistory({required this.data, required this.events});
  factory DriverPaymentHistory.fromJson(Object? value) {
    final j = _j(value);
    return DriverPaymentHistory(
      data: [for (final x in _list(j['data'])) DriverPayment.fromJson(x)],
      events: [
        for (final x in _list(j['history'])) DriverPaymentEvent.fromJson(x),
      ],
    );
  }
  final List<DriverPayment> data;
  final List<DriverPaymentEvent> events;
}

@immutable
class JobPhoto {
  const JobPhoto({required this.url, this.id});
  factory JobPhoto.fromJson(Map<String, dynamic> j) =>
      JobPhoto(url: '${j['url']}', id: _s(j['id']));
  final String url;
  final String? id;
}

/// Handover (before) or return (after) evidence.
@immutable
class JobChecklist {
  const JobChecklist({
    required this.id,
    required this.kind,
    required this.hourMeter,
    required this.items,
    required this.photos,
    required this.capturedAt,
    required this.submittedBy,
    this.fuelPct,
    this.damageNotes,
    this.notes,
  });
  factory JobChecklist.fromJson(Map<String, dynamic> j) => JobChecklist(
    id: '${j['id']}',
    kind: '${j['kind']}',
    hourMeter: _d(j['hour_meter']),
    fuelPct: (j['fuel_level_pct'] as num?)?.toInt(),
    items: {for (final e in _j(j['items']).entries) e.key: e.value == true},
    photos: [for (final p in _list(j['photos'])) JobPhoto.fromJson(p)],
    damageNotes: _s(j['damage_notes']),
    notes: _s(j['notes']),
    capturedAt: _dtReq(j['captured_at']),
    submittedBy: _s(j['submitted_by']) ?? '',
  );
  final String id;
  final String kind;
  final double hourMeter;
  final int? fuelPct;
  final Map<String, bool> items;
  final List<JobPhoto> photos;
  final String? damageNotes;
  final String? notes;
  final DateTime capturedAt;
  final String submittedBy;
}

/// A reported problem on a job.
@immutable
class JobIssue {
  const JobIssue({
    required this.id,
    required this.kind,
    required this.stage,
    required this.severity,
    required this.description,
    required this.status,
    required this.reporterRole,
    required this.createdAt,
    this.photos = const [],
    this.reporterName,
    this.resolutionNote,
    this.bookingId,
    this.bookingReference,
    this.equipmentTitle,
    this.ownerName,
    this.customerName,
  });
  factory JobIssue.fromJson(Map<String, dynamic> j) => JobIssue(
    id: '${j['id']}',
    kind: '${j['kind']}',
    stage: '${j['stage']}',
    severity: '${j['severity']}',
    description: '${j['description']}',
    status: '${j['status']}',
    reporterRole: '${j['reporter_role']}',
    createdAt: _dtReq(j['created_at']),
    photos: [for (final p in _list(j['photos'])) JobPhoto.fromJson(p)],
    reporterName: _s(j['reporter_name']),
    resolutionNote: _s(j['resolution_note']),
    bookingId: _s(j['booking_id']),
    bookingReference: _s(j['booking_reference']),
    equipmentTitle: _s(j['equipment_title']),
    ownerName: _s(j['owner_name']),
    customerName: _s(j['customer_name']),
  );
  final String id;
  final String kind;
  final String stage;
  final String severity;
  final String description;
  final String status;
  final String reporterRole;
  final DateTime createdAt;
  final List<JobPhoto> photos;
  final String? reporterName;
  final String? resolutionNote;
  final String? bookingId;
  final String? bookingReference;
  final String? equipmentTitle;
  final String? ownerName;
  final String? customerName;

  bool get open => status == 'open' || status == 'under_review';
  String get title =>
      '${severity[0].toUpperCase()}${severity.substring(1)} ${kind.replaceAll('_', ' ')}';
}

@immutable
class JobContact {
  const JobContact({required this.name, this.contact, this.phone});
  factory JobContact.fromJson(Map<String, dynamic> j) => JobContact(
    name: '${j['name'] ?? ''}',
    contact: _s(j['contact']),
    phone: _s(j['phone']),
  );
  final String name;
  final String? contact;
  final String? phone;
}

/// Everything the operator needs for one job.
@immutable
class JobDetail {
  const JobDetail({
    required this.bookingId,
    required this.reference,
    required this.bookingStatus,
    required this.assignmentStatus,
    required this.current,
    required this.start,
    required this.end,
    required this.workDescription,
    required this.customer,
    required this.owner,
    required this.equipmentTitle,
    required this.equipmentLine,
    required this.hourMeter,
    required this.siteAddress,
    required this.siteLat,
    required this.siteLng,
    required this.navigationUrl,
    required this.checklists,
    required this.issues,
    required this.nextSteps,
    this.registrationNo,
    this.serialNo,
    this.siteLabel,
    this.driverPaymentPaise = 0,
    this.driverPaymentStatus = 'not_set',
  });
  factory JobDetail.fromJson(Map<String, dynamic> j) {
    final eq = _j(j['equipment']);
    final site = _j(j['site']);
    return JobDetail(
      bookingId: '${j['booking_id']}',
      reference: '${j['reference']}',
      bookingStatus: '${j['booking_status']}',
      assignmentStatus: '${j['assignment_status']}',
      current: j['current'] == true,
      start: _dtReq(j['start']),
      end: _dtReq(j['end']),
      workDescription: _s(j['work_description']) ?? '',
      customer: JobContact.fromJson(_j(j['customer'])),
      owner: JobContact.fromJson(_j(j['owner'])),
      equipmentTitle: '${eq['title'] ?? ''}',
      equipmentLine: [
        eq['make'],
        eq['model'],
        eq['category'],
      ].whereType<String>().where((s) => s.isNotEmpty).join(' · '),
      registrationNo: _s(eq['registration_no']),
      serialNo: _s(eq['serial_no']),
      hourMeter: _d(eq['hour_meter']),
      siteAddress: '${site['address'] ?? ''}',
      siteLabel: _s(site['label']),
      driverPaymentPaise: (j['driver_payment_paise'] as num?)?.toInt() ?? 0,
      driverPaymentStatus: '${j['driver_payment_status'] ?? 'not_set'}',
      siteLat: _d(site['lat']),
      siteLng: _d(site['lng']),
      navigationUrl: '${_j(j['navigation'])['url'] ?? ''}',
      checklists: [
        for (final c in _list(j['checklists'])) JobChecklist.fromJson(c),
      ],
      issues: [for (final i in _list(j['issues'])) JobIssue.fromJson(i)],
      nextSteps: [for (final s in (j['next_steps'] as List? ?? const [])) '$s'],
    );
  }
  final String bookingId;
  final String reference;
  final String bookingStatus;
  final String assignmentStatus;
  final bool current;
  final DateTime start;
  final DateTime end;
  final String workDescription;
  final JobContact customer;
  final JobContact owner;
  final String equipmentTitle;
  final String equipmentLine;
  final String? registrationNo;
  final String? serialNo;
  final double hourMeter;
  final String siteAddress;
  final String? siteLabel;
  final int driverPaymentPaise;
  final String driverPaymentStatus;
  final double siteLat;
  final double siteLng;
  final String navigationUrl;
  final List<JobChecklist> checklists;
  final List<JobIssue> issues;
  final List<String> nextSteps;

  JobChecklist? checklist(String kind) =>
      checklists.where((c) => c.kind == kind).firstOrNull;
}

/// What the operator records in a handover/return checklist.
@immutable
class ChecklistInput {
  const ChecklistInput({
    required this.kind,
    required this.photoIds,
    required this.items,
    required this.clientId,
    this.fuelPct,
    this.damageNotes,
    this.notes,
  });
  final String kind;
  final List<String> photoIds;
  final Map<String, bool> items;
  final String clientId;
  final int? fuelPct;
  final String? damageNotes;
  final String? notes;

  Map<String, dynamic> toJson() => {
    'kind': kind,
    'photo_upload_ids': photoIds,
    'items': items,
    'fuel_level_pct': ?fuelPct,
    'damage_notes': ?damageNotes,
    'notes': ?notes,
    'captured_at': DateTime.now().toUtc().toIso8601String(),
    'client_id': clientId,
  };
}

// ---- api ------------------------------------------------------------------------------------------

/// The operator's job endpoints. Lifecycle steps (accept, dispatch, arrived,
/// start, returned) are the booking engine's; evidence and issues are the
/// operations module's.
abstract class OperatorApi {
  Future<DriverPaymentHistory> payments();
  Future<List<JobSummary>> jobs({required String scope});
  Future<List<JobSummary>> schedule(DateTime from, DateTime to);
  Future<JobDetail> job(String bookingId);
  Future<void> step(
    String bookingId,
    String step,
  ); // accept | decline | dispatch | arrived | start | returned
  Future<JobChecklist> submitChecklist(String bookingId, ChecklistInput input);
  Future<JobIssue> reportIssue(
    String bookingId, {
    required String kind,
    required String stage,
    required String severity,
    required String description,
    List<String> photoIds = const [],
  });
  Future<bool> setOnDuty(bool onDuty);
}

class HttpOperatorApi implements OperatorApi {
  HttpOperatorApi(this._c);
  final ApiClient _c;

  static List<_J> _data(Object? v) => _list(_j(v)['data']);

  @override
  Future<DriverPaymentHistory> payments() => _c.get(
    '/operator/driver-compensations',
    decode: DriverPaymentHistory.fromJson,
  );

  @override
  Future<List<JobSummary>> jobs({required String scope}) => _c.get(
    '/operator/jobs',
    query: {'scope': scope, 'limit': 100},
    decode: (j) => [for (final x in _data(j)) JobSummary.fromJson(x)],
  );

  @override
  Future<List<JobSummary>> schedule(DateTime from, DateTime to) => _c.get(
    '/operator/schedule',
    query: {
      'from': from.toUtc().toIso8601String(),
      'to': to.toUtc().toIso8601String(),
    },
    decode: (j) => [
      for (final x in _data(j))
        JobSummary.fromJson({...x, 'site_line1': x['site'], 'site_city': null}),
    ],
  );

  @override
  Future<JobDetail> job(String bookingId) => _c.get(
    '/operator/jobs/$bookingId',
    decode: (j) => JobDetail.fromJson(_j(j)),
  );

  @override
  Future<void> step(String bookingId, String step) =>
      _c.post('/operator/jobs/$bookingId/$step', decode: (_) {});

  @override
  Future<JobChecklist> submitChecklist(
    String bookingId,
    ChecklistInput input,
  ) => _c.post(
    '/operator/jobs/$bookingId/checklists',
    body: input.toJson(),
    decode: (j) => JobChecklist.fromJson(_j(j)),
  );

  @override
  Future<JobIssue> reportIssue(
    String bookingId, {
    required String kind,
    required String stage,
    required String severity,
    required String description,
    List<String> photoIds = const [],
  }) => _c.post(
    '/operator/jobs/$bookingId/issues',
    body: {
      'kind': kind,
      'stage': stage,
      'severity': severity,
      'description': description,
      'photo_upload_ids': photoIds,
    },
    decode: (j) => JobIssue.fromJson(_j(j)),
  );

  @override
  Future<bool> setOnDuty(bool onDuty) => _c.post(
    '/operator/availability',
    body: {'on_duty': onDuty},
    decode: (j) => _j(j)['on_duty'] == true,
  );
}

final operatorApiProvider = Provider<OperatorApi>(
  (ref) => HttpOperatorApi(ref.watch(apiClientProvider)),
);

final myJobsProvider = FutureProvider.autoDispose
    .family<List<JobSummary>, String>(
      (ref, scope) => ref.watch(operatorApiProvider).jobs(scope: scope),
    );
final myDriverPaymentsProvider =
    FutureProvider.autoDispose<DriverPaymentHistory>(
      (ref) => ref.watch(operatorApiProvider).payments(),
    );
final myJobProvider = FutureProvider.autoDispose.family<JobDetail, String>(
  (ref, id) => ref.watch(operatorApiProvider).job(id),
);
final myScheduleProvider = FutureProvider.autoDispose<List<JobSummary>>((ref) {
  final now = DateTime.now();
  final from = DateTime(now.year, now.month, now.day);
  return ref
      .watch(operatorApiProvider)
      .schedule(from, from.add(const Duration(days: 14)));
});
