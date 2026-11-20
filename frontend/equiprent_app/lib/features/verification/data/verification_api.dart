import 'package:dio/dio.dart';
import 'package:file_picker/file_picker.dart' as fp;
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart' as ip;
import 'package:url_launcher/url_launcher.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/network/api_client.dart';
import '../../../core/session/user_role.dart';
import 'verification_models.dart';

/// Upload purposes accepted by POST /uploads.
enum UploadPurpose {
  identityDocument,
  businessDocument,
  operatorDocument,
  avatar,
  jobPhoto,
}

extension on UploadPurpose {
  String get wire => switch (this) {
    UploadPurpose.identityDocument => 'identity_document',
    UploadPurpose.businessDocument => 'business_document',
    UploadPurpose.operatorDocument => 'operator_document',
    UploadPurpose.avatar => 'avatar',
    UploadPurpose.jobPhoto => 'job_photo',
  };
}

/// The verification endpoints. Owner calls carry the owner organisation
/// (X-Org-Id); the server checks membership and role on every call.
abstract class VerificationApi {
  Future<String> upload(UploadPurpose purpose, PickedFile file);

  Future<CustomerStatus> customer();
  Future<CustomerStatus> setAvatar(String uploadId);
  Future<void> addAddress(String org, Json body);

  Future<OwnerVerification> owner(String? org);
  Future<OwnerVerification> saveBusiness(String? org, Json body);
  Future<VDocument> addOwnerDocument(
    String? org, {
    required String docType,
    required String uploadId,
    String? number,
    String? expiresOn,
  });
  Future<void> deleteOwnerDocument(String? org, String id);
  Future<BankAccount> setBankAccount(String? org, Json body);
  Future<VApplication> submitOwner(String? org);
  Future<OwnerVerification> reopenOwner(String? org);
  Future<List<FleetManagerSummary>> fleetManagers(String? org);
  Future<FleetManagerSummary> setFleetAccess(
    String? org,
    String userId, {
    required bool all,
    required List<String> equipmentIds,
  });
  Future<List<OwnedMachine>> ownedMachines(String? org);

  Future<OperatorVerification> operator();
  Future<OperatorVerification> saveOperatorProfile(Json body);
  Future<OperatorVerification> saveOperatorAddress(Json body);
  Future<Qualification> addQualification(Json body);
  Future<void> deleteQualification(String id);
  Future<VDocument> addOperatorDocument({
    required String docType,
    required String uploadId,
    String? number,
    String? expiresOn,
  });
  Future<void> deleteOperatorDocument(String id);
  Future<VApplication> submitOperator();

  Future<FleetManagerView> fleetManager();
  Future<SignedLink> documentLink(String documentId);

  Future<List<ApplicationSummary>> queue({
    ApplicationStatus? status,
    String? role,
  });
  Future<ApplicationDetail> application(String id);
  Future<VApplication> startReview(String id);
  Future<VDocument> reviewDocument(
    String appId,
    String docId, {
    required bool approve,
    String? reason,
  });
  Future<VApplication> approve(String id, {String? note});
  Future<VApplication> reject(String id, String reason);
  Future<void> suspendAccount(String userId, String reason);
}

class HttpVerificationApi implements VerificationApi {
  HttpVerificationApi(this._c);
  final ApiClient _c;

  static Json _j(Object? v) => (v as Map).cast<String, dynamic>();
  static Map<String, String>? _org(String? org) =>
      org == null ? null : {'X-Org-Id': org};

  Json _doc(
    String docType,
    String uploadId,
    String? number,
    String? expiresOn,
  ) => {
    'doc_type': docType,
    'upload_id': uploadId,
    if (number != null && number.isNotEmpty) 'document_number': number,
    if (expiresOn != null && expiresOn.isNotEmpty) 'expires_on': expiresOn,
  };

  @override
  Future<String> upload(UploadPurpose purpose, PickedFile file) => _c.post(
    '/uploads',
    body: FormData.fromMap({
      'purpose': purpose.wire,
      'file': MultipartFile.fromBytes(file.bytes, filename: file.name),
    }),
    decode: (j) => '${_j(j)['id']}',
  );

  @override
  Future<CustomerStatus> customer() => _c.get(
    '/verification/customer',
    decode: (j) => CustomerStatus.fromJson(_j(j)),
  );
  @override
  Future<CustomerStatus> setAvatar(String uploadId) => _c.put(
    '/me/avatar',
    body: {'upload_id': uploadId},
    decode: (j) => CustomerStatus.fromJson(_j(j)),
  );

  @override
  Future<void> addAddress(String org, Json body) =>
      _c.post('/orgs/$org/addresses', body: body, decode: (_) {});

  @override
  Future<OwnerVerification> owner(String? org) => _c.get(
    '/verification/owner',
    headers: _org(org),
    decode: (j) => OwnerVerification.fromJson(_j(j)),
  );
  @override
  Future<OwnerVerification> saveBusiness(String? org, Json body) => _c.put(
    '/verification/owner/business',
    headers: _org(org),
    body: body,
    decode: (j) => OwnerVerification.fromJson(_j(j)),
  );
  @override
  Future<VDocument> addOwnerDocument(
    String? org, {
    required String docType,
    required String uploadId,
    String? number,
    String? expiresOn,
  }) => _c.post(
    '/verification/owner/documents',
    headers: _org(org),
    body: _doc(docType, uploadId, number, expiresOn),
    decode: (j) => VDocument.fromJson(_j(j)),
  );
  @override
  Future<void> deleteOwnerDocument(String? org, String id) =>
      _c.delete('/verification/owner/documents/$id', headers: _org(org));
  @override
  Future<BankAccount> setBankAccount(String? org, Json body) => _c.put(
    '/verification/owner/bank-account',
    headers: _org(org),
    body: body,
    decode: (j) => BankAccount.fromJson(j)!,
  );
  @override
  Future<OwnerVerification> reopenOwner(String? org) => _c.post(
    '/verification/owner/reopen',
    headers: _org(org),
    decode: (j) => OwnerVerification.fromJson(_j(j)),
  );
  @override
  Future<VApplication> submitOwner(String? org) => _c.post(
    '/verification/owner/submit',
    headers: _org(org),
    decode: (j) => VApplication.fromJson(_j(j)),
  );
  @override
  Future<List<FleetManagerSummary>> fleetManagers(String? org) => _c.get(
    '/verification/owner/fleet-managers',
    headers: _org(org),
    decode: (j) => [
      for (final m in (_j(j)['data'] as List? ?? const []))
        FleetManagerSummary.fromJson(_j(m)),
    ],
  );
  @override
  Future<FleetManagerSummary> setFleetAccess(
    String? org,
    String userId, {
    required bool all,
    required List<String> equipmentIds,
  }) => _c.put(
    '/verification/owner/fleet-managers/$userId',
    headers: _org(org),
    body: {'all_equipment': all, 'equipment_ids': equipmentIds},
    decode: (j) => FleetManagerSummary.fromJson(_j(j)),
  );
  @override
  Future<List<OwnedMachine>> ownedMachines(String? org) => _c.get(
    '/owner/equipment',
    headers: _org(org),
    query: {'limit': 100},
    decode: (j) => [
      for (final e in (_j(j)['data'] as List? ?? const []))
        OwnedMachine(id: '${_j(e)['id']}', title: '${_j(e)['title']}'),
    ],
  );

  @override
  Future<OperatorVerification> operator() => _c.get(
    '/verification/operator',
    decode: (j) => OperatorVerification.fromJson(_j(j)),
  );
  @override
  Future<OperatorVerification> saveOperatorProfile(Json body) => _c.put(
    '/verification/operator/profile',
    body: body,
    decode: (j) => OperatorVerification.fromJson(_j(j)),
  );
  @override
  Future<OperatorVerification> saveOperatorAddress(Json body) => _c.put(
    '/verification/operator/address',
    body: body,
    decode: (j) => OperatorVerification.fromJson(_j(j)),
  );
  @override
  Future<Qualification> addQualification(Json body) => _c.post(
    '/verification/operator/qualifications',
    body: body,
    decode: (j) => Qualification.fromJson(_j(j)),
  );
  @override
  Future<void> deleteQualification(String id) =>
      _c.delete('/verification/operator/qualifications/$id');
  @override
  Future<VDocument> addOperatorDocument({
    required String docType,
    required String uploadId,
    String? number,
    String? expiresOn,
  }) => _c.post(
    '/verification/operator/documents',
    body: _doc(docType, uploadId, number, expiresOn),
    decode: (j) => VDocument.fromJson(_j(j)),
  );
  @override
  Future<void> deleteOperatorDocument(String id) =>
      _c.delete('/verification/operator/documents/$id');
  @override
  Future<VApplication> submitOperator() => _c.post(
    '/verification/operator/submit',
    decode: (j) => VApplication.fromJson(_j(j)),
  );

  @override
  Future<FleetManagerView> fleetManager() => _c.get(
    '/verification/fleet-manager',
    decode: (j) => FleetManagerView.fromJson(_j(j)),
  );
  @override
  Future<SignedLink> documentLink(String documentId) => _c.get(
    '/verification/documents/$documentId/url',
    decode: (j) => SignedLink.fromJson(j)!,
  );

  @override
  Future<List<ApplicationSummary>> queue({
    ApplicationStatus? status,
    String? role,
  }) => _c.get(
    '/admin/verifications',
    query: {
      if (status != null) 'status': status.wire,
      'role': ?role,
      'limit': 100,
    },
    decode: (j) => [
      for (final a in (_j(j)['data'] as List? ?? const []))
        ApplicationSummary.fromJson(_j(a)),
    ],
  );
  @override
  Future<ApplicationDetail> application(String id) => _c.get(
    '/admin/verifications/$id',
    decode: (j) => ApplicationDetail.fromJson(_j(j)),
  );
  @override
  Future<VApplication> startReview(String id) => _c.post(
    '/admin/verifications/$id/start-review',
    decode: (j) => VApplication.fromJson(_j(j)),
  );
  @override
  Future<VDocument> reviewDocument(
    String appId,
    String docId, {
    required bool approve,
    String? reason,
  }) => _c.post(
    '/admin/verifications/$appId/documents/$docId/review',
    body: {'decision': approve ? 'approve' : 'reject', 'reason': ?reason},
    decode: (j) => VDocument.fromJson(_j(j)),
  );
  @override
  Future<VApplication> approve(String id, {String? note}) => _c.post(
    '/admin/verifications/$id/approve',
    body: {if (note != null && note.isNotEmpty) 'reason': note},
    decode: (j) => VApplication.fromJson(_j(j)),
  );
  @override
  Future<VApplication> reject(String id, String reason) => _c.post(
    '/admin/verifications/$id/reject',
    body: {'reason': reason},
    decode: (j) => VApplication.fromJson(_j(j)),
  );
  @override
  Future<void> suspendAccount(String userId, String reason) => _c.post(
    '/admin/users/$userId/suspend',
    body: {'reason': reason},
    decode: (_) {},
  );
}

final verificationApiProvider = Provider<VerificationApi>(
  (ref) => HttpVerificationApi(ref.watch(apiClientProvider)),
);

/// The organisation a role acts for (from the signed-in user's role grants).
final roleOrgProvider = Provider.family<String?, UserRole>(
  (ref, role) => ref.watch(authControllerProvider).user?.orgs[role],
);

// ---- data providers -------------------------------------------------------------------

final customerStatusProvider = FutureProvider.autoDispose<CustomerStatus>(
  (ref) => ref.watch(verificationApiProvider).customer(),
);

final ownerVerificationProvider = FutureProvider.autoDispose<OwnerVerification>(
  (ref) => ref
      .watch(verificationApiProvider)
      .owner(ref.watch(roleOrgProvider(UserRole.owner))),
);

final operatorVerificationProvider =
    FutureProvider.autoDispose<OperatorVerification>(
      (ref) => ref.watch(verificationApiProvider).operator(),
    );

final fleetManagerViewProvider = FutureProvider.autoDispose<FleetManagerView>(
  (ref) => ref.watch(verificationApiProvider).fleetManager(),
);

final fleetManagersProvider =
    FutureProvider.autoDispose<List<FleetManagerSummary>>(
      (ref) => ref
          .watch(verificationApiProvider)
          .fleetManagers(ref.watch(roleOrgProvider(UserRole.owner))),
    );

final verificationQueueProvider = FutureProvider.autoDispose
    .family<List<ApplicationSummary>, (ApplicationStatus?, String?)>(
      (ref, filter) => ref
          .watch(verificationApiProvider)
          .queue(status: filter.$1, role: filter.$2),
    );

final applicationDetailProvider = FutureProvider.autoDispose
    .family<ApplicationDetail, String>(
      (ref, id) => ref.watch(verificationApiProvider).application(id),
    );

// ---- device services (overridable in tests) --------------------------------------------

/// Picks a document or image from the device.
abstract class DocumentPicker {
  /// [imagesOnly] limits to JPEG/PNG/WebP (profile images); otherwise PDFs too.
  Future<PickedFile?> pick({bool imagesOnly = false});

  /// Several JPEG/PNG/WebP images at once (listing photos). Empty when cancelled.
  Future<List<PickedFile>> pickImages();
}

class PlatformDocumentPicker implements DocumentPicker {
  const PlatformDocumentPicker();

  @override
  Future<PickedFile?> pick({bool imagesOnly = false}) async {
    final f = await fp.FilePicker.pickFile(
      type: fp.FileType.custom,
      allowedExtensions: imagesOnly
          ? const ['jpg', 'jpeg', 'png', 'webp']
          : const ['pdf', 'jpg', 'jpeg', 'png', 'webp'],
    );
    if (f == null) return null;
    return PickedFile(name: f.name, bytes: await f.readAsBytes());
  }

  @override
  Future<List<PickedFile>> pickImages() async {
    final files = await fp.FilePicker.pickFiles(
      type: fp.FileType.custom,
      allowedExtensions: const ['jpg', 'jpeg', 'png', 'webp'],
    );
    return [
      for (final f in files)
        PickedFile(name: f.name, bytes: await f.readAsBytes()),
    ];
  }
}

final documentPickerProvider = Provider<DocumentPicker>(
  (ref) => const PlatformDocumentPicker(),
);

final inspectionCameraProvider = Provider<InspectionCamera>(
  (ref) => const PlatformInspectionCamera(),
);

abstract class InspectionCamera {
  Future<PickedFile?> capture();
}

class PlatformInspectionCamera implements InspectionCamera {
  const PlatformInspectionCamera();

  @override
  Future<PickedFile?> capture() async {
    final image = await ip.ImagePicker().pickImage(
      source: ip.ImageSource.camera,
      maxWidth: 2560,
      imageQuality: 88,
    );
    if (image == null) return null;
    return PickedFile(name: image.name, bytes: await image.readAsBytes());
  }
}

/// Opens a signed download link outside the app (browser / viewer).
typedef LinkOpener = Future<bool> Function(String url);

final linkOpenerProvider = Provider<LinkOpener>(
  (ref) =>
      (url) => launchUrl(Uri.parse(url), mode: LaunchMode.externalApplication),
);
