import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/verification/data/verification_api.dart';
import 'package:equiprent_app/features/verification/data/verification_models.dart';

/// Returns a fixed file (a tiny PDF or PNG header) or null when "cancelled".
class FakePicker implements DocumentPicker {
  FakePicker({this.cancel = false});
  bool cancel;
  final picks = <bool>[];

  @override
  Future<PickedFile?> pick({bool imagesOnly = false}) async {
    picks.add(imagesOnly);
    if (cancel) return null;
    return imagesOnly
        ? const PickedFile(name: 'me.png', bytes: [0x89, 0x50, 0x4E, 0x47])
        : PickedFile(name: 'pan.pdf', bytes: '%PDF-1.4\n%%EOF'.codeUnits);
  }

  /// Number of images returned by [pickImages].
  int images = 3;

  @override
  Future<List<PickedFile>> pickImages() async {
    picks.add(true);
    if (cancel) return const [];
    return [
      for (var i = 0; i < images; i++)
        PickedFile(name: 'photo$i.jpg', bytes: const [0xFF, 0xD8, 0xFF, 0xE0]),
    ];
  }
}

ApiException _conflict(String m) => ApiException(
  kind: ApiErrorKind.conflict,
  code: 'INVALID_STATE_TRANSITION',
  message: m,
  statusCode: 409,
);

/// In-memory verification backend following the server's rules closely
/// enough for the UI tests (locking, completeness, rejection reasons).
class FakeVerificationApi implements VerificationApi {
  final calls = <String>[];
  final orgsSeen = <String?>[];
  int _ids = 0;
  String _id(String p) => '$p-${++_ids}';

  // ---- owner state
  String ownerStatus = 'draft';
  String? ownerRejection;
  Json? business;
  final ownerDocs = <Json>[];
  Json? bank;

  // ---- operator state
  String operatorStatus = 'draft';
  String? operatorRejection;
  Json? operatorProfile;
  final operatorDocs = <Json>[];
  final quals = <Json>[];

  // ---- customer
  bool avatar = false;
  int addresses = 0;

  // ---- fleet
  List<FleetManagerSummary> managers = const [
    FleetManagerSummary(
      userId: 'fm-1',
      fullName: 'Meena Kale',
      phone: '+919811111111',
      allEquipment: false,
      grantedCount: 0,
    ),
  ];
  List<OwnedMachine> machines = const [
    OwnedMachine(id: 'eq-1', title: 'Excavator A'),
    OwnedMachine(id: 'eq-2', title: 'Excavator B'),
  ];
  ({bool all, List<String> ids})? lastAccess;

  // ---- admin
  final decisions = <String>[];

  bool _editable(String s) => s == 'draft' || s == 'rejected';
  Json _app(String id, String role, String status, String? reason) => {
    'id': id,
    'role': role,
    'status': status,
    'rejection_reason': reason,
    'submitted_at': status == 'draft' ? null : '2026-11-12T10:00:00Z',
  };

  bool _has(List<Json> docs, List<String> types) => docs.any(
    (d) => types.contains(d['doc_type']) && d['status'] != 'rejected',
  );

  List<Json> _ownerChecks() {
    final company = business?['account_kind'] == 'company';
    return [
      {
        'key': 'business_details',
        'label': 'Business and contact details',
        'done': business != null,
      },
      {
        'key': 'business_address',
        'label': 'Business address',
        'done': business != null,
      },
      {
        'key': 'identity_document',
        'label': 'Identity document',
        'done': _has(ownerDocs, DocTypes.ownerIdentity),
      },
      {
        'key': 'business_document',
        'label': 'Business document',
        'done': !company || _has(ownerDocs, DocTypes.ownerBusiness),
      },
      {
        'key': 'bank_account',
        'label': 'Payout bank account',
        'done': bank != null,
      },
    ];
  }

  OwnerVerification _owner() => OwnerVerification.fromJson({
    'organization_id': 'org-owner',
    'account_kind': business?['account_kind'] ?? 'individual',
    'org_type': business?['org_type'] ?? 'individual',
    'legal_name': business?['legal_name'] ?? 'Test User',
    'business_name': business?['business_name'] ?? 'Test User',
    'pan_last4': business == null
        ? null
        : (business!['pan'] as String).substring(6),
    'gstin': business?['gstin'],
    'cin': business?['cin'],
    'contact_name': business?['contact_name'],
    'contact_phone': business?['contact_phone'],
    'contact_email': business?['contact_email'],
    'business_address': business?['business_address'],
    'kyc_status': ownerStatus == 'approved' ? 'verified' : 'pending',
    'application': _app('app-owner', 'owner', ownerStatus, ownerRejection),
    'documents': ownerDocs,
    'bank_account': bank,
    'checks': _ownerChecks(),
    'editable': _editable(ownerStatus),
  });

  List<Json> _operatorChecks() => [
    {'key': 'personal_details', 'label': 'Personal details', 'done': true},
    {
      'key': 'licence_details',
      'label': 'Valid licence details',
      'done': operatorProfile != null,
    },
    {
      'key': 'experience',
      'label': 'Experience',
      'done': operatorProfile != null,
    },
    {
      'key': 'availability',
      'label': 'Availability',
      'done': operatorProfile != null,
    },
    {
      'key': 'licence_document',
      'label': 'Licence document',
      'done': _has(operatorDocs, DocTypes.operatorLicence),
    },
    {
      'key': 'identity_document',
      'label': 'Identity document',
      'done': _has(operatorDocs, DocTypes.operatorIdentity),
    },
  ];

  OperatorVerification _operator() => OperatorVerification.fromJson({
    'licence_last4': operatorProfile == null
        ? null
        : (operatorProfile!['licence_number'] as String).substring(
            (operatorProfile!['licence_number'] as String).length - 4,
          ),
    'licence_class': operatorProfile?['licence_class'],
    'licence_expires_on': operatorProfile?['licence_expires_on'],
    'experience_years': operatorProfile?['experience_years'],
    'skills': operatorProfile?['skills'] ?? const [],
    'availability': operatorProfile?['availability'],
    'verified': operatorStatus == 'approved',
    'application': _app(
      'app-operator',
      'operator',
      operatorStatus,
      operatorRejection,
    ),
    'qualifications': quals,
    'documents': operatorDocs,
    'checks': _operatorChecks(),
    'editable': _editable(operatorStatus),
  });

  @override
  Future<String> upload(UploadPurpose purpose, PickedFile file) async {
    calls.add('upload:${purpose.name}:${file.name}');
    return _id('upload');
  }

  @override
  Future<CustomerStatus> customer() async => CustomerStatus.fromJson({
    'complete': avatar && addresses > 0,
    'checks': [
      {'key': 'personal_details', 'label': 'Add your full name', 'done': true},
      {'key': 'mobile', 'label': 'Verify your mobile number', 'done': true},
      {'key': 'email', 'label': 'Verify your email address', 'done': false},
      {'key': 'address', 'label': 'Add an address', 'done': addresses > 0},
      {'key': 'profile_image', 'label': 'Add a profile image', 'done': avatar},
    ],
  });

  @override
  Future<CustomerStatus> setAvatar(String uploadId) async {
    calls.add('setAvatar:$uploadId');
    avatar = true;
    return customer();
  }

  @override
  Future<void> addAddress(String org, Json body) async {
    calls.add('addAddress:$org:${body['pincode']}');
    addresses++;
  }

  @override
  Future<OwnerVerification> owner(String? org) async {
    orgsSeen.add(org);
    return _owner();
  }

  @override
  Future<OwnerVerification> saveBusiness(String? org, Json body) async {
    calls.add('saveBusiness:${body['pan']}');
    if (!_editable(ownerStatus)) {
      throw _conflict('verification is submitted or approved');
    }
    business = body;
    return _owner();
  }

  @override
  Future<VDocument> addOwnerDocument(
    String? org, {
    required String docType,
    required String uploadId,
    String? number,
    String? expiresOn,
  }) async {
    calls.add('addOwnerDocument:$docType');
    final d = {
      'id': _id('doc'),
      'doc_type': docType,
      'status': 'submitted',
      'content_type': 'application/pdf',
    };
    ownerDocs.add(d);
    return VDocument.fromJson(d);
  }

  @override
  Future<void> deleteOwnerDocument(String? org, String id) async {
    calls.add('deleteOwnerDocument:$id');
    ownerDocs.removeWhere((d) => d['id'] == id);
  }

  @override
  Future<BankAccount> setBankAccount(String? org, Json body) async {
    calls.add('setBankAccount:${body['ifsc']}');
    final n = body['account_number'] as String;
    if (n.endsWith('0000')) {
      throw const ApiException(
        kind: ApiErrorKind.badRequest,
        code: 'BUSINESS_RULE_VIOLATION',
        message: 'The payment provider could not verify this account',
      );
    }
    bank = {
      'account_holder_name': body['account_holder_name'],
      'account_last4': n.substring(n.length - 4),
      'ifsc': body['ifsc'],
      'verification_status': 'verified',
    };
    return BankAccount.fromJson(bank)!;
  }

  @override
  Future<OwnerVerification> reopenOwner(String? org) async {
    calls.add('reopenOwner');
    ownerStatus = 'draft';
    return _owner();
  }

  @override
  Future<VApplication> submitOwner(String? org) async {
    calls.add('submitOwner');
    ownerStatus = 'submitted';
    return _owner().application;
  }

  @override
  Future<List<FleetManagerSummary>> fleetManagers(String? org) async =>
      managers;

  @override
  Future<FleetManagerSummary> setFleetAccess(
    String? org,
    String userId, {
    required bool all,
    required List<String> equipmentIds,
  }) async {
    calls.add('setFleetAccess:$userId');
    lastAccess = (all: all, ids: equipmentIds);
    return managers.first;
  }

  @override
  Future<List<OwnedMachine>> ownedMachines(String? org) async => machines;

  @override
  Future<OperatorVerification> operator() async => _operator();

  @override
  Future<OperatorVerification> saveOperatorProfile(Json body) async {
    calls.add('saveOperatorProfile:${body['licence_number']}');
    if (!_editable(operatorStatus)) throw _conflict('locked');
    operatorProfile = body;
    return _operator();
  }

  @override
  Future<OperatorVerification> saveOperatorAddress(Json body) async {
    calls.add('saveOperatorAddress');
    if (!_editable(operatorStatus)) throw _conflict('locked');
    operatorProfile = {...?operatorProfile, ...body};
    return _operator();
  }

  @override
  Future<Qualification> addQualification(Json body) async {
    calls.add('addQualification:${body['title']}');
    final q = {'id': _id('q'), 'title': body['title']};
    quals.add(q);
    return Qualification.fromJson(q);
  }

  @override
  Future<void> deleteQualification(String id) async =>
      quals.removeWhere((q) => q['id'] == id);

  @override
  Future<VDocument> addOperatorDocument({
    required String docType,
    required String uploadId,
    String? number,
    String? expiresOn,
  }) async {
    calls.add('addOperatorDocument:$docType');
    final d = {
      'id': _id('doc'),
      'doc_type': docType,
      'status': 'submitted',
      'content_type': 'application/pdf',
    };
    operatorDocs.add(d);
    return VDocument.fromJson(d);
  }

  @override
  Future<void> deleteOperatorDocument(String id) async =>
      operatorDocs.removeWhere((d) => d['id'] == id);

  @override
  Future<VApplication> submitOperator() async {
    calls.add('submitOperator');
    operatorStatus = 'submitted';
    return _operator().application;
  }

  @override
  Future<FleetManagerView> fleetManager() async => FleetManagerView.fromJson({
    'organization_name': 'Kale Infra',
    'legal_name': 'Kale Infra Pvt Ltd',
    'company_verified': true,
    'contact_name': 'Rohan Kale',
    'all_equipment': false,
    'equipment': [
      {'equipment_id': 'eq-1', 'title': 'Excavator A', 'status': 'active'},
    ],
  });

  @override
  Future<SignedLink> documentLink(String documentId) async {
    calls.add('documentLink:$documentId');
    return SignedLink(
      url: 'https://api.test/api/v1/files?key=kyc/x&exp=1&sig=s-$documentId',
      expiresAt: DateTime.now().add(const Duration(minutes: 5)),
    );
  }

  @override
  Future<List<ApplicationSummary>> queue({
    ApplicationStatus? status,
    String? role,
  }) async {
    calls.add('queue:${status?.wire}:${role ?? 'all'}');
    final all = [
      ApplicationSummary.fromJson({
        ..._app('app-owner', 'owner', ownerStatus, null),
        'subject_name': 'Patil Earthmovers',
      }),
    ];
    return all
        .where(
          (a) =>
              (status == null || a.status == status) &&
              (role == null || a.role == role),
        )
        .toList();
  }

  @override
  Future<ApplicationDetail> application(String id) async {
    final o = _owner();
    return ApplicationDetail.fromJson({
      ..._app('app-owner', 'owner', ownerStatus, ownerRejection),
      'subject_name': 'Patil Earthmovers',
      'account_user_id': 'u-owner',
      'owner': {
        'organization_id': o.organizationId,
        'account_kind': 'company',
        'org_type': 'private_limited',
        'legal_name': 'Patil Earthmovers Pvt Ltd',
        'business_name': 'Patil Earthmovers',
        'pan_last4': '234F',
        'kyc_status': 'submitted',
        'application': _app('app-owner', 'owner', ownerStatus, ownerRejection),
        'documents': [
          for (final d in ownerDocs)
            {
              ...d,
              'download': {
                'url':
                    'https://api.test/api/v1/files?key=kyc/${d['id']}&exp=9&sig=sig',
                'expires_at': DateTime.now()
                    .add(const Duration(minutes: 5))
                    .toIso8601String(),
              },
            },
        ],
        'checks': _ownerChecks(),
        'editable': false,
      },
      'history': [
        {
          'action': 'submitted',
          'at': '2026-11-12T10:00:00Z',
          'actor_name': 'Ravi Patil',
        },
      ],
    });
  }

  @override
  Future<VApplication> startReview(String id) async {
    decisions.add('start:$id');
    ownerStatus = 'under_review';
    return _owner().application;
  }

  @override
  Future<VDocument> reviewDocument(
    String appId,
    String docId, {
    required bool approve,
    String? reason,
  }) async {
    decisions.add(
      '${approve ? 'approve' : 'reject'}-doc:$docId:${reason ?? ''}',
    );
    final d = ownerDocs.firstWhere((d) => d['id'] == docId);
    d['status'] = approve ? 'verified' : 'rejected';
    d['review_note'] = reason;
    return VDocument.fromJson(d);
  }

  @override
  Future<VApplication> approve(String id, {String? note}) async {
    decisions.add('approve:$id');
    ownerStatus = 'approved';
    return _owner().application;
  }

  @override
  Future<VApplication> reject(String id, String reason) async {
    decisions.add('reject:$id:$reason');
    ownerStatus = 'rejected';
    ownerRejection = reason;
    return _owner().application;
  }

  @override
  Future<void> suspendAccount(String userId, String reason) async =>
      decisions.add('suspend:$userId:$reason');
}

/// Stands in for the in-app evidence camera: returns a fixed image.
class FakeInspectionCamera implements InspectionCamera {
  @override
  Future<PickedFile?> capture() async =>
      const PickedFile(name: 'evidence.jpg', bytes: [0xFF, 0xD8, 0xFF, 0xE0]);
}
