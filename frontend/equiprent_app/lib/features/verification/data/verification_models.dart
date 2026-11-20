import 'package:flutter/foundation.dart';

typedef Json = Map<String, dynamic>;

Json _m(Object? v) => v is Map ? v.cast<String, dynamic>() : const {};
List<Json> _l(Object? v) => [for (final e in (v as List?) ?? const []) _m(e)];
DateTime? _t(Object? v) => v is String ? DateTime.tryParse(v) : null;
String? _s(Object? v) => v is String && v.isNotEmpty ? v : null;

/// Application review states.
enum ApplicationStatus {
  draft('Not submitted'),
  submitted('Submitted'),
  underReview('Under review'),
  approved('Verified'),
  rejected('Needs changes');

  const ApplicationStatus(this.label);
  final String label;

  static ApplicationStatus parse(Object? v) => switch (v) {
    'submitted' => submitted,
    'under_review' => underReview,
    'approved' => approved,
    'rejected' => rejected,
    _ => draft,
  };

  String get wire => switch (this) {
    underReview => 'under_review',
    _ => name,
  };
}

@immutable
class VCheck {
  const VCheck({required this.key, required this.label, required this.done});
  factory VCheck.fromJson(Json j) => VCheck(
    key: '${j['key']}',
    label: '${j['label']}',
    done: j['done'] == true,
  );
  final String key;
  final String label;
  final bool done;
}

/// A short-lived signed download link.
@immutable
class SignedLink {
  const SignedLink({required this.url, required this.expiresAt});
  static SignedLink? fromJson(Object? v) {
    final j = _m(v);
    final url = _s(j['url']);
    return url == null
        ? null
        : SignedLink(
            url: url,
            expiresAt: _t(j['expires_at']) ?? DateTime.now(),
          );
  }

  final String url;
  final DateTime expiresAt;
}

/// Document kinds with display names.
abstract final class DocTypes {
  static const labels = {
    'pan': 'PAN card',
    'passport': 'Passport',
    'voter_id': 'Voter ID',
    'driving_licence': 'Driving licence',
    'gst_certificate': 'GST certificate',
    'business_registration': 'Business registration',
    'udyam': 'Udyam certificate',
    'cin': 'Certificate of incorporation',
    'address_proof': 'Business address proof',
    'operator_certificate': 'Operator certificate',
  };
  static String label(String t) => labels[t] ?? t;

  static const ownerIdentity = [
    'pan',
    'passport',
    'voter_id',
    'driving_licence',
  ];
  static const ownerBusiness = [
    'gst_certificate',
    'business_registration',
    'udyam',
    'cin',
    'address_proof',
  ];
  static const operatorLicence = ['driving_licence'];
  static const operatorIdentity = [
    'pan',
    'passport',
    'voter_id',
    'operator_certificate',
  ];
}

@immutable
class VDocument {
  const VDocument({
    required this.id,
    required this.docType,
    required this.status,
    this.numberLast4,
    this.contentType,
    this.expiresOn,
    this.reviewNote,
    this.download,
  });

  factory VDocument.fromJson(Json j) => VDocument(
    id: '${j['id']}',
    docType: '${j['doc_type']}',
    status: '${j['status']}',
    numberLast4: _s(j['number_last4']),
    contentType: _s(j['content_type']),
    expiresOn: _s(j['expires_on']),
    reviewNote: _s(j['review_note']),
    download: SignedLink.fromJson(j['download']),
  );

  final String id;
  final String docType;
  final String status; // submitted | verified | rejected
  final String? numberLast4;
  final String? contentType;
  final String? expiresOn;
  final String? reviewNote;
  final SignedLink? download;

  String get label => DocTypes.label(docType);
  bool get removable => status == 'submitted' || status == 'pending';
}

@immutable
class VApplication {
  const VApplication({
    required this.id,
    required this.role,
    required this.status,
    this.submittedAt,
    this.reviewedAt,
    this.rejectionReason,
  });
  factory VApplication.fromJson(Json j) => VApplication(
    id: '${j['id']}',
    role: '${j['role']}',
    status: ApplicationStatus.parse(j['status']),
    submittedAt: _t(j['submitted_at']),
    reviewedAt: _t(j['reviewed_at']),
    rejectionReason: _s(j['rejection_reason']),
  );
  final String id;
  final String role;
  final ApplicationStatus status;
  final DateTime? submittedAt;
  final DateTime? reviewedAt;
  final String? rejectionReason;
}

@immutable
class CustomerStatus {
  const CustomerStatus({
    required this.complete,
    required this.checks,
    this.avatar,
  });
  factory CustomerStatus.fromJson(Json j) => CustomerStatus(
    complete: j['complete'] == true,
    checks: [for (final c in _l(j['checks'])) VCheck.fromJson(c)],
    avatar: SignedLink.fromJson(j['avatar_url']),
  );
  final bool complete;
  final List<VCheck> checks;
  final SignedLink? avatar;
}

@immutable
class PostalAddress {
  const PostalAddress({
    required this.line1,
    required this.city,
    required this.stateCode,
    required this.pincode,
    this.line2,
    this.locality,
    this.district,
  });
  factory PostalAddress.fromJson(Json j) => PostalAddress(
    line1: '${j['line1'] ?? ''}',
    line2: _s(j['line2']),
    locality: _s(j['locality']),
    district: _s(j['district']),
    city: '${j['city'] ?? ''}',
    stateCode: '${j['state_code'] ?? ''}',
    pincode: '${j['pincode'] ?? ''}',
  );
  final String line1;
  final String? line2;
  final String? locality;
  final String? district;
  final String city;
  final String stateCode;
  final String pincode;
  Json toJson() => {
    'line1': line1,
    if (line2 != null) 'line2': line2,
    if (locality != null) 'locality': locality,
    if (district != null) 'district': district,
    'city': city,
    'state_code': stateCode,
    'pincode': pincode,
  };
  String get oneLine => [
    line1,
    if (line2 != null) line2,
    if (locality != null) locality,
    city,
    pincode,
  ].join(', ');
}

@immutable
class BankAccount {
  const BankAccount({
    required this.holder,
    required this.last4,
    required this.ifsc,
    required this.status,
    this.providerReference,
  });
  static BankAccount? fromJson(Object? v) {
    final j = _m(v);
    if (j.isEmpty) return null;
    return BankAccount(
      holder: '${j['account_holder_name']}',
      last4: '${j['account_last4']}',
      ifsc: '${j['ifsc']}',
      status: '${j['verification_status']}',
      providerReference: _s(j['provider_reference']),
    );
  }

  final String holder;
  final String last4;
  final String ifsc;
  final String status;
  final String? providerReference;
}

@immutable
class OwnerVerification {
  const OwnerVerification({
    required this.organizationId,
    required this.accountKind,
    required this.orgType,
    required this.legalName,
    required this.businessName,
    required this.kycStatus,
    required this.application,
    required this.documents,
    required this.checks,
    required this.editable,
    this.panLast4,
    this.gstin,
    this.udyam,
    this.cin,
    this.contactName,
    this.contactPhone,
    this.contactEmail,
    this.address,
    this.bank,
  });

  factory OwnerVerification.fromJson(Json j) => OwnerVerification(
    organizationId: '${j['organization_id']}',
    accountKind: '${j['account_kind'] ?? 'individual'}',
    orgType: '${j['org_type'] ?? 'individual'}',
    legalName: '${j['legal_name'] ?? ''}',
    businessName: '${j['business_name'] ?? ''}',
    panLast4: _s(j['pan_last4']),
    gstin: _s(j['gstin']),
    udyam: _s(j['udyam_number']),
    cin: _s(j['cin']),
    contactName: _s(j['contact_name']),
    contactPhone: _s(j['contact_phone']),
    contactEmail: _s(j['contact_email']),
    address: j['business_address'] is Map
        ? PostalAddress.fromJson(_m(j['business_address']))
        : null,
    kycStatus: '${j['kyc_status'] ?? 'pending'}',
    application: VApplication.fromJson(_m(j['application'])),
    documents: [for (final d in _l(j['documents'])) VDocument.fromJson(d)],
    bank: BankAccount.fromJson(j['bank_account']),
    checks: [for (final c in _l(j['checks'])) VCheck.fromJson(c)],
    editable: j['editable'] == true,
  );

  final String organizationId;
  final String accountKind;
  final String orgType;
  final String legalName;
  final String businessName;
  final String? panLast4;
  final String? gstin;
  final String? udyam;
  final String? cin;
  final String? contactName;
  final String? contactPhone;
  final String? contactEmail;
  final PostalAddress? address;
  final String kycStatus;
  final VApplication application;
  final List<VDocument> documents;
  final BankAccount? bank;
  final List<VCheck> checks;
  final bool editable;

  bool get hasBusinessDetails => panLast4 != null;
}

@immutable
class OperatorAvailability {
  const OperatorAvailability({
    required this.days,
    required this.start,
    required this.end,
  });
  static OperatorAvailability? fromJson(Object? v) {
    final j = _m(v);
    if (j.isEmpty) return null;
    return OperatorAvailability(
      days: [for (final d in (j['days'] as List?) ?? const []) '$d'],
      start: '${j['start']}',
      end: '${j['end']}',
    );
  }

  final List<String> days;
  final String start;
  final String end;
  Json toJson() => {'days': days, 'start': start, 'end': end};
}

@immutable
class Qualification {
  const Qualification({
    required this.id,
    required this.title,
    this.issuer,
    this.issuedOn,
    this.validUntil,
    this.documentId,
  });
  factory Qualification.fromJson(Json j) => Qualification(
    id: '${j['id']}',
    title: '${j['title']}',
    issuer: _s(j['issuer']),
    issuedOn: _s(j['issued_on']),
    validUntil: _s(j['valid_until']),
    documentId: _s(j['document_id']),
  );
  final String id;
  final String title;
  final String? issuer;
  final String? issuedOn;
  final String? validUntil;
  final String? documentId;
}

@immutable
class OperatorVerification {
  const OperatorVerification({
    required this.skills,
    required this.verified,
    required this.application,
    required this.qualifications,
    required this.documents,
    required this.checks,
    required this.editable,
    this.licenceLast4,
    this.licenceClass,
    this.licenceExpiresOn,
    this.experienceYears,
    this.availability,
    this.address,
    this.identityConsent = false,
  });

  factory OperatorVerification.fromJson(Json j) => OperatorVerification(
    licenceLast4: _s(j['licence_last4']),
    licenceClass: _s(j['licence_class']),
    licenceExpiresOn: _s(j['licence_expires_on']),
    experienceYears: (j['experience_years'] as num?)?.toInt(),
    skills: [for (final s in (j['skills'] as List?) ?? const []) '$s'],
    availability: OperatorAvailability.fromJson(j['availability']),
    address: j['address'] is Map
        ? PostalAddress.fromJson(_m(j['address']))
        : null,
    identityConsent: j['identity_consent'] == true,
    verified: j['verified'] == true,
    application: VApplication.fromJson(_m(j['application'])),
    qualifications: [
      for (final q in _l(j['qualifications'])) Qualification.fromJson(q),
    ],
    documents: [for (final d in _l(j['documents'])) VDocument.fromJson(d)],
    checks: [for (final c in _l(j['checks'])) VCheck.fromJson(c)],
    editable: j['editable'] == true,
  );

  final String? licenceLast4;
  final String? licenceClass;
  final String? licenceExpiresOn;
  final int? experienceYears;
  final List<String> skills;
  final OperatorAvailability? availability;
  final PostalAddress? address;
  final bool identityConsent;
  final bool verified;
  final VApplication application;
  final List<Qualification> qualifications;
  final List<VDocument> documents;
  final List<VCheck> checks;
  final bool editable;
}

@immutable
class GrantedMachine {
  const GrantedMachine({
    required this.id,
    required this.title,
    required this.status,
  });
  factory GrantedMachine.fromJson(Json j) => GrantedMachine(
    id: '${j['equipment_id']}',
    title: '${j['title']}',
    status: '${j['status']}',
  );
  final String id;
  final String title;
  final String status;
}

@immutable
class FleetManagerView {
  const FleetManagerView({
    required this.organizationName,
    required this.legalName,
    required this.companyVerified,
    required this.allEquipment,
    required this.equipment,
    this.contactName,
    this.contactPhone,
    this.contactEmail,
  });
  factory FleetManagerView.fromJson(Json j) => FleetManagerView(
    organizationName: '${j['organization_name'] ?? ''}',
    legalName: '${j['legal_name'] ?? ''}',
    companyVerified: j['company_verified'] == true,
    contactName: _s(j['contact_name']),
    contactPhone: _s(j['contact_phone']),
    contactEmail: _s(j['contact_email']),
    allEquipment: j['all_equipment'] == true,
    equipment: [for (final e in _l(j['equipment'])) GrantedMachine.fromJson(e)],
  );
  final String organizationName;
  final String legalName;
  final bool companyVerified;
  final String? contactName;
  final String? contactPhone;
  final String? contactEmail;
  final bool allEquipment;
  final List<GrantedMachine> equipment;
}

@immutable
class FleetManagerSummary {
  const FleetManagerSummary({
    required this.userId,
    required this.fullName,
    required this.phone,
    required this.allEquipment,
    required this.grantedCount,
  });
  factory FleetManagerSummary.fromJson(Json j) => FleetManagerSummary(
    userId: '${j['user_id']}',
    fullName: '${j['full_name']}',
    phone: '${j['phone']}',
    allEquipment: j['all_equipment'] == true,
    grantedCount: (j['granted_count'] as num?)?.toInt() ?? 0,
  );
  final String userId;
  final String fullName;
  final String phone;
  final bool allEquipment;
  final int grantedCount;
}

/// A machine the owner can grant (from the owner listings API).
@immutable
class OwnedMachine {
  const OwnedMachine({required this.id, required this.title});
  final String id;
  final String title;
}

@immutable
class ApplicationSummary {
  const ApplicationSummary({
    required this.id,
    required this.role,
    required this.status,
    required this.subjectName,
    this.submittedAt,
  });
  factory ApplicationSummary.fromJson(Json j) => ApplicationSummary(
    id: '${j['id']}',
    role: '${j['role']}',
    status: ApplicationStatus.parse(j['status']),
    subjectName: '${j['subject_name'] ?? ''}',
    submittedAt: _t(j['submitted_at']),
  );
  final String id;
  final String role;
  final ApplicationStatus status;
  final String subjectName;
  final DateTime? submittedAt;
}

@immutable
class HistoryEntry {
  const HistoryEntry({
    required this.action,
    required this.at,
    this.actorName,
    this.reason,
  });
  factory HistoryEntry.fromJson(Json j) => HistoryEntry(
    action: '${j['action']}',
    at: _t(j['at']) ?? DateTime.now(),
    actorName: _s(j['actor_name']),
    reason: _s(j['reason']),
  );
  final String action;
  final DateTime at;
  final String? actorName;
  final String? reason;

  String get label => switch (action) {
    'submitted' => 'Submitted for review',
    'review_started' => 'Review started',
    'document_approved' => 'Document approved',
    'document_rejected' => 'Document rejected',
    'approved' => 'Approved',
    'rejected' => 'Rejected',
    'revoked' => 'Verification revoked',
    'bank_account_verified' => 'Payout account verified',
    _ => action.replaceAll('_', ' '),
  };
}

@immutable
class ApplicationDetail {
  const ApplicationDetail({
    required this.application,
    required this.subjectName,
    required this.history,
    this.owner,
    this.operator,
    this.accountUserId,
  });
  factory ApplicationDetail.fromJson(Json j) => ApplicationDetail(
    application: VApplication.fromJson(j),
    subjectName: '${j['subject_name'] ?? ''}',
    accountUserId: _s(j['account_user_id']),
    owner: j['owner'] is Map
        ? OwnerVerification.fromJson(_m(j['owner']))
        : null,
    operator: j['operator'] is Map
        ? OperatorVerification.fromJson(_m(j['operator']))
        : null,
    history: [for (final h in _l(j['history'])) HistoryEntry.fromJson(h)],
  );
  final VApplication application;
  final String subjectName;
  final OwnerVerification? owner;
  final OperatorVerification? operator;
  final List<HistoryEntry> history;

  /// The applicant's account (for suspension).
  final String? accountUserId;

  List<VDocument> get documents =>
      owner?.documents ?? operator?.documents ?? const [];
  List<VCheck> get checks => owner?.checks ?? operator?.checks ?? const [];
}

/// A file chosen by the user, read into memory for upload.
@immutable
class PickedFile {
  const PickedFile({required this.name, required this.bytes});
  final String name;
  final List<int> bytes;
}
