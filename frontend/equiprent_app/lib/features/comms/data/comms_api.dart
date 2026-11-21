import '../../../core/utils/app_lifecycle.dart';
import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/network/api_client.dart';
import '../../../core/session/active_role_provider.dart';
import '../../../core/session/user_role.dart';
import '../../verification/data/verification_models.dart' show PickedFile;
import 'comms_models.dart';

/// Endpoints of the communication and operations modules. Calls that act
/// for an organisation take [org] (sent as X-Org-Id); the server checks the
/// caller's role and access on every request.
abstract class CommsApi {
  // notifications
  Future<NotificationPage> notifications({
    bool unreadOnly = false,
    String? cursor,
  });
  Future<int> unreadNotifications();
  Future<void> markNotificationRead(String id);
  Future<void> markAllNotificationsRead();
  Future<List<PushPreference>> pushPreferences();
  Future<List<PushPreference>> setPushPreferences(List<PushPreference> prefs);
  Future<void> registerDevice(String platform, String token);
  Future<void> unregisterDevice(String token);

  // bookings (as the parties see them)
  Future<List<BookingSummary>> myBookings(String? org, {String? status});
  Future<List<BookingSummary>> ownerBookings(String? org, {String? status});
  Future<BookingDetail> booking(String? org, String id);
  Future<void> respondToBooking(
    String? org,
    String id, {
    required bool accept,
    String? reason,
  });

  // agreements
  Future<Agreement> agreement(String? org, String bookingId);
  Future<SignResult> signAgreement(
    String? org,
    String bookingId, {
    required String termsSha256,
    required String signerName,
    String? designation,
  });
  Future<DocumentLink> agreementDocument(String? org, String bookingId);
  Future<List<AgreementVersionInfo>> agreementVersions(
    String? org,
    String bookingId,
  );
  Future<Agreement> agreementVersion(
    String? org,
    String bookingId,
    int version,
  );
  Future<DocumentLink> agreementVersionDocument(
    String? org,
    String bookingId,
    int version,
  );
  Future<List<DocumentHistoryEntry>> documentHistory(
    String? org,
    String bookingId,
  );

  // chat
  Future<List<Conversation>> conversations();
  Future<int> unreadMessages();
  Future<Conversation> conversation(String id);
  Future<Conversation> openSupportConversation();
  Future<Conversation> openBookingChat(String? org, String bookingId);
  Future<Conversation> enquire(
    String? org,
    String equipmentId, {
    String? message,
  });
  Future<List<ChatMessage>> messages(
    String conversationId, {
    String? before,
    String? after,
    int limit = 40,
  });
  Future<ChatMessage> sendMessage(
    String conversationId, {
    String? body,
    String? uploadId,
    required String clientId,
  });
  Future<void> deleteMessage(String conversationId, String messageId);
  Future<void> markConversationRead(String conversationId);
  Future<String> upload(String purpose, PickedFile file);

  // maintenance
  Future<List<MaintenanceDue>> maintenanceDue(String? org);
  Future<List<MaintenanceRecord>> maintenanceHistory(
    String? org, {
    String? equipmentId,
    String? kind,
    String? status,
  });
  Future<CostReport> maintenanceCosts(String? org, {String? from, String? to});
  Future<List<MaintenancePlan>> maintenancePlans(
    String? org,
    String equipmentId,
  );
  Future<MaintenancePlan> createPlan(
    String? org,
    String equipmentId,
    Json body,
  );
  Future<MaintenancePlan> updatePlan(
    String? org,
    String equipmentId,
    String planId,
    Json body,
  );
  Future<MaintenanceRecord> createRecord(
    String? org,
    String equipmentId,
    Json body,
  );
  Future<MaintenanceRecord> updateRecord(
    String? org,
    String equipmentId,
    String recordId,
    Json body,
  );
  Future<String> invoiceLink(String? org, String equipmentId, String recordId);

  // reviews
  Future<ReviewPage> equipmentReviews(String equipmentId, {String? cursor});
  Future<ReviewPage> ownerReviews(String orgId, {String? cursor});
  Future<BookingReviews> bookingReviews(String? org, String bookingId);
  Future<Review> createReview(
    String? org,
    String bookingId, {
    required String subject,
    required int rating,
    String? comment,
  });
  Future<ReviewPage> receivedReviews(String? org, {String? cursor});
  Future<void> replyToReview(String? org, String reviewId, String reply);
  Future<bool> reportReview(String reviewId, String reason);
  Future<List<AdminReview>> moderationQueue({
    String? status,
    bool reported = false,
  });
  Future<void> moderateReview(
    String reviewId, {
    required bool publish,
    required String reason,
  });
}

class HttpCommsApi implements CommsApi {
  HttpCommsApi(this._c);
  final ApiClient _c;

  static Json _j(Object? v) => (v as Map).cast<String, dynamic>();
  static List<Json> _data(Object? v) => [
    for (final e in (_j(v)['data'] as List? ?? const [])) _j(e),
  ];
  static Map<String, String>? _org(String? org) =>
      org == null ? null : {'X-Org-Id': org};

  // ---- notifications
  @override
  Future<NotificationPage> notifications({
    bool unreadOnly = false,
    String? cursor,
  }) => _c.get(
    '/notifications',
    query: {
      'limit': 50,
      if (unreadOnly) 'unread_only': true,
      'cursor': ?cursor,
    },
    decode: (j) => NotificationPage.fromJson(_j(j)),
  );
  @override
  Future<int> unreadNotifications() => _c.get(
    '/notifications/unread-count',
    decode: (j) => (_j(j)['unread'] as num).toInt(),
  );
  @override
  Future<void> markNotificationRead(String id) =>
      _c.post('/notifications/$id/read', decode: (_) {});
  @override
  Future<void> markAllNotificationsRead() =>
      _c.post('/notifications/read-all', decode: (_) {});
  @override
  Future<List<PushPreference>> pushPreferences() => _c.get(
    '/me/notification-preferences',
    decode: (j) => [for (final p in _data(j)) PushPreference.fromJson(p)],
  );
  @override
  Future<List<PushPreference>> setPushPreferences(List<PushPreference> prefs) =>
      _c.put(
        '/me/notification-preferences',
        body: {
          'preferences': [for (final p in prefs) p.toJson()],
        },
        decode: (j) => [for (final p in _data(j)) PushPreference.fromJson(p)],
      );
  @override
  Future<void> registerDevice(String platform, String token) => _c.post(
    '/me/devices',
    body: {'platform': platform, 'push_token': token},
    decode: (_) {},
  );
  @override
  Future<void> unregisterDevice(String token) => _c.post(
    '/me/devices/unregister',
    body: {'push_token': token},
    decode: (_) {},
  );

  // ---- bookings
  @override
  Future<List<BookingSummary>> myBookings(String? org, {String? status}) =>
      _c.get(
        '/bookings',
        headers: _org(org),
        query: {'limit': 100, 'status': ?status},
        decode: (j) => [for (final b in _data(j)) BookingSummary.fromJson(b)],
      );
  @override
  Future<List<BookingSummary>> ownerBookings(String? org, {String? status}) =>
      _c.get(
        '/owner/bookings',
        headers: _org(org),
        query: {'limit': 100, 'status': ?status},
        decode: (j) => [for (final b in _data(j)) BookingSummary.fromJson(b)],
      );
  @override
  Future<BookingDetail> booking(String? org, String id) => _c.get(
    '/bookings/$id',
    headers: _org(org),
    decode: (j) => BookingDetail.fromJson(_j(j)),
  );
  @override
  Future<void> respondToBooking(
    String? org,
    String id, {
    required bool accept,
    String? reason,
  }) => _c.post(
    '/owner/bookings/$id/${accept ? 'accept' : 'reject'}',
    headers: _org(org),
    body: {'reason': ?reason},
    decode: (_) {},
  );

  // ---- agreements
  @override
  Future<Agreement> agreement(String? org, String bookingId) => _c.get(
    '/bookings/$bookingId/agreement',
    headers: _org(org),
    decode: (j) => Agreement.fromJson(_j(j)),
  );
  @override
  Future<SignResult> signAgreement(
    String? org,
    String bookingId, {
    required String termsSha256,
    required String signerName,
    String? designation,
  }) => _c.post(
    '/bookings/$bookingId/agreement/sign',
    headers: _org(org),
    body: {
      'terms_sha256': termsSha256,
      'signer_name': signerName,
      'designation': ?designation,
      'consent': true,
    },
    decode: (j) => SignResult.fromJson(_j(j)),
  );
  @override
  Future<DocumentLink> agreementDocument(String? org, String bookingId) =>
      _c.get(
        '/bookings/$bookingId/agreement/document',
        headers: _org(org),
        decode: (j) => DocumentLink.fromJson(_j(j)),
      );
  @override
  Future<List<AgreementVersionInfo>> agreementVersions(
    String? org,
    String bookingId,
  ) => _c.get(
    '/bookings/$bookingId/agreement/versions',
    headers: _org(org),
    decode: (j) => [for (final v in _data(j)) AgreementVersionInfo.fromJson(v)],
  );
  @override
  Future<Agreement> agreementVersion(
    String? org,
    String bookingId,
    int version,
  ) => _c.get(
    '/bookings/$bookingId/agreement/versions/$version',
    headers: _org(org),
    decode: (j) => Agreement.fromJson(_j(j)),
  );
  @override
  Future<DocumentLink> agreementVersionDocument(
    String? org,
    String bookingId,
    int version,
  ) => _c.get(
    '/bookings/$bookingId/agreement/versions/$version/document',
    headers: _org(org),
    decode: (j) => DocumentLink.fromJson(_j(j)),
  );
  @override
  Future<List<DocumentHistoryEntry>> documentHistory(
    String? org,
    String bookingId,
  ) => _c.get(
    '/bookings/$bookingId/documents/history',
    headers: _org(org),
    decode: (j) => [for (final e in _data(j)) DocumentHistoryEntry.fromJson(e)],
  );

  // ---- chat
  @override
  Future<List<Conversation>> conversations() => _c.get(
    '/conversations',
    decode: (j) => [for (final c in _data(j)) Conversation.fromJson(c)],
  );
  @override
  Future<int> unreadMessages() => _c.get(
    '/conversations/unread-count',
    decode: (j) => (_j(j)['unread'] as num).toInt(),
  );
  @override
  Future<Conversation> conversation(String id) =>
      _c.get('/conversations/$id', decode: (j) => Conversation.fromJson(_j(j)));
  @override
  Future<Conversation> openSupportConversation() => _c.post(
    '/support/conversation',
    decode: (j) => Conversation.fromJson(_j(j)),
  );
  @override
  Future<Conversation> openBookingChat(String? org, String bookingId) =>
      _c.post(
        '/bookings/$bookingId/conversation',
        headers: _org(org),
        decode: (j) => Conversation.fromJson(_j(j)),
      );
  @override
  Future<Conversation> enquire(
    String? org,
    String equipmentId, {
    String? message,
  }) => _c.post(
    '/equipment/$equipmentId/enquiry',
    headers: _org(org),
    body: {'message': ?message},
    decode: (j) => Conversation.fromJson(_j(j)),
  );
  @override
  Future<List<ChatMessage>> messages(
    String conversationId, {
    String? before,
    String? after,
    int limit = 40,
  }) => _c.get(
    '/conversations/$conversationId/messages',
    query: {'limit': limit, 'before': ?before, 'after': ?after},
    decode: (j) => [for (final m in _data(j)) ChatMessage.fromJson(m)],
  );
  @override
  Future<ChatMessage> sendMessage(
    String conversationId, {
    String? body,
    String? uploadId,
    required String clientId,
  }) => _c.post(
    '/conversations/$conversationId/messages',
    body: {
      'body': ?body,
      'attachment_upload_id': ?uploadId,
      'client_id': clientId,
    },
    decode: (j) => ChatMessage.fromJson(_j(j)),
  );
  @override
  Future<void> deleteMessage(String conversationId, String messageId) =>
      _c.delete('/conversations/$conversationId/messages/$messageId');
  @override
  Future<void> markConversationRead(String conversationId) =>
      _c.post('/conversations/$conversationId/read', decode: (_) {});
  @override
  Future<String> upload(String purpose, PickedFile file) => _c.post(
    '/uploads',
    body: FormData.fromMap({
      'purpose': purpose,
      'file': MultipartFile.fromBytes(file.bytes, filename: file.name),
    }),
    decode: (j) => _j(j)['id'] as String,
  );

  // ---- maintenance
  @override
  Future<List<MaintenanceDue>> maintenanceDue(String? org) => _c.get(
    '/owner/maintenance/due',
    headers: _org(org),
    decode: (j) => [for (final d in _data(j)) MaintenanceDue.fromJson(d)],
  );
  @override
  Future<List<MaintenanceRecord>> maintenanceHistory(
    String? org, {
    String? equipmentId,
    String? kind,
    String? status,
  }) => _c.get(
    '/owner/maintenance/records',
    headers: _org(org),
    query: {
      'limit': 100,
      'equipment_id': ?equipmentId,
      'kind': ?kind,
      'status': ?status,
    },
    decode: (j) => [for (final r in _data(j)) MaintenanceRecord.fromJson(r)],
  );
  @override
  Future<CostReport> maintenanceCosts(
    String? org, {
    String? from,
    String? to,
  }) => _c.get(
    '/owner/maintenance/costs',
    headers: _org(org),
    query: {'from': ?from, 'to': ?to},
    decode: (j) => CostReport.fromJson(_j(j)),
  );
  @override
  Future<List<MaintenancePlan>> maintenancePlans(
    String? org,
    String equipmentId,
  ) => _c.get(
    '/owner/equipment/$equipmentId/maintenance/plans',
    headers: _org(org),
    decode: (j) => [for (final p in _data(j)) MaintenancePlan.fromJson(p)],
  );
  @override
  Future<MaintenancePlan> createPlan(
    String? org,
    String equipmentId,
    Json body,
  ) => _c.post(
    '/owner/equipment/$equipmentId/maintenance/plans',
    headers: _org(org),
    body: body,
    decode: (j) => MaintenancePlan.fromJson(_j(j)),
  );
  @override
  Future<MaintenancePlan> updatePlan(
    String? org,
    String equipmentId,
    String planId,
    Json body,
  ) => _c.patch(
    '/owner/equipment/$equipmentId/maintenance/plans/$planId',
    headers: _org(org),
    body: body,
    decode: (j) => MaintenancePlan.fromJson(_j(j)),
  );
  @override
  Future<MaintenanceRecord> createRecord(
    String? org,
    String equipmentId,
    Json body,
  ) => _c.post(
    '/owner/equipment/$equipmentId/maintenance/records',
    headers: _org(org),
    body: body,
    decode: (j) => MaintenanceRecord.fromJson(_j(j)),
  );
  @override
  Future<MaintenanceRecord> updateRecord(
    String? org,
    String equipmentId,
    String recordId,
    Json body,
  ) => _c.patch(
    '/owner/equipment/$equipmentId/maintenance/records/$recordId',
    headers: _org(org),
    body: body,
    decode: (j) => MaintenanceRecord.fromJson(_j(j)),
  );
  @override
  Future<String> invoiceLink(
    String? org,
    String equipmentId,
    String recordId,
  ) => _c.get(
    '/owner/equipment/$equipmentId/maintenance/records/$recordId/invoice',
    headers: _org(org),
    decode: (j) => _j(j)['url'] as String,
  );

  // ---- reviews
  @override
  Future<ReviewPage> equipmentReviews(String equipmentId, {String? cursor}) =>
      _c.get(
        '/equipment/$equipmentId/reviews',
        query: {'limit': 20, 'cursor': ?cursor},
        decode: (j) => ReviewPage.fromJson(_j(j)),
      );
  @override
  Future<ReviewPage> ownerReviews(String orgId, {String? cursor}) => _c.get(
    '/owners/$orgId/reviews',
    query: {'limit': 20, 'cursor': ?cursor},
    decode: (j) => ReviewPage.fromJson(_j(j)),
  );
  @override
  Future<BookingReviews> bookingReviews(String? org, String bookingId) =>
      _c.get(
        '/bookings/$bookingId/reviews',
        headers: _org(org),
        decode: (j) => BookingReviews.fromJson(_j(j)),
      );
  @override
  Future<Review> createReview(
    String? org,
    String bookingId, {
    required String subject,
    required int rating,
    String? comment,
  }) => _c.post(
    '/bookings/$bookingId/reviews',
    headers: _org(org),
    body: {'subject': subject, 'rating': rating, 'comment': ?comment},
    decode: (j) => Review.fromJson(_j(j)),
  );
  @override
  Future<ReviewPage> receivedReviews(String? org, {String? cursor}) => _c.get(
    '/owner/reviews',
    headers: _org(org),
    query: {'limit': 50, 'cursor': ?cursor},
    decode: (j) => ReviewPage.fromJson(_j(j)),
  );
  @override
  Future<void> replyToReview(String? org, String reviewId, String reply) =>
      _c.post(
        '/reviews/$reviewId/reply',
        headers: _org(org),
        body: {'reply': reply},
        decode: (_) {},
      );
  @override
  Future<bool> reportReview(String reviewId, String reason) => _c.post(
    '/reviews/$reviewId/report',
    body: {'reason': reason},
    decode: (j) => _j(j)['reported'] == true,
  );
  @override
  Future<List<AdminReview>> moderationQueue({
    String? status,
    bool reported = false,
  }) => _c.get(
    '/admin/reviews',
    query: {'limit': 100, 'status': ?status, if (reported) 'reported': true},
    decode: (j) => [for (final r in _data(j)) AdminReview.fromJson(r)],
  );
  @override
  Future<void> moderateReview(
    String reviewId, {
    required bool publish,
    required String reason,
  }) => _c.post(
    '/admin/reviews/$reviewId/moderate',
    body: {'action': publish ? 'publish' : 'hide', 'reason': reason},
    decode: (_) {},
  );
}

final commsApiProvider = Provider<CommsApi>(
  (ref) => HttpCommsApi(ref.watch(apiClientProvider)),
);

// ---- acting organisation ------------------------------------------------------------------

/// Which side of a booking a screen shows.
enum BookingSide { customer, owner }

/// The organisation the caller acts for on [side].
final sideOrgProvider = Provider.family<String?, BookingSide>((ref, side) {
  final orgs = ref.watch(authControllerProvider).user?.orgs ?? const {};
  return switch (side) {
    BookingSide.customer => orgs[UserRole.customer],
    BookingSide.owner => orgs[UserRole.owner] ?? orgs[UserRole.fleetManager],
  };
});

/// The side matching the active role (owners and fleet managers see the owner side).
final activeSideProvider = Provider<BookingSide>((ref) {
  final role = ref.watch(activeRoleProvider);
  return role == UserRole.owner || role == UserRole.fleetManager
      ? BookingSide.owner
      : BookingSide.customer;
});

/// The organisation the active role acts for (owner side or customer side).
final activeOrgProvider = Provider<String?>(
  (ref) => ref.watch(sideOrgProvider(ref.watch(activeSideProvider))),
);

// ---- data providers ---------------------------------------------------------------------

/// How often badges refresh while the app is open (push wakes the app
/// sooner). Null switches polling off (tests).
final badgePollProvider = Provider<Duration?>(
  (ref) => const Duration(seconds: 45),
);

/// How often an open chat thread checks for new messages. Null = off.
final chatPollProvider = Provider<Duration?>(
  (ref) => const Duration(seconds: 4),
);

void _refreshAfter(Ref ref, Duration? every) {
  if (every == null) return;
  Timer? timer;
  void schedule() {
    timer = Timer(every, () {
      if (appInBackground) {
        schedule(); // do not poll while the app is not visible
      } else {
        ref.invalidateSelf();
      }
    });
  }
  schedule();
  ref.onDispose(() => timer?.cancel());
}

final unreadNotificationsProvider = FutureProvider.autoDispose<int>((
  ref,
) async {
  _refreshAfter(ref, ref.watch(badgePollProvider));
  try {
    return await ref.watch(commsApiProvider).unreadNotifications();
  } catch (_) {
    return 0; // the badge is best effort
  }
});

final unreadMessagesProvider = FutureProvider.autoDispose<int>((ref) async {
  _refreshAfter(ref, ref.watch(badgePollProvider));
  try {
    return await ref.watch(commsApiProvider).unreadMessages();
  } catch (_) {
    return 0;
  }
});

final notificationListProvider = FutureProvider.autoDispose<NotificationPage>(
  (ref) => ref.watch(commsApiProvider).notifications(),
);
final pushPreferencesProvider =
    FutureProvider.autoDispose<List<PushPreference>>(
      (ref) => ref.watch(commsApiProvider).pushPreferences(),
    );

final bookingsProvider = FutureProvider.autoDispose
    .family<List<BookingSummary>, BookingSide>((ref, side) {
      final api = ref.watch(commsApiProvider);
      final org = ref.watch(sideOrgProvider(side));
      return side == BookingSide.owner
          ? api.ownerBookings(org)
          : api.myBookings(org);
    });
final bookingDetailProvider = FutureProvider.autoDispose
    .family<BookingDetail, (BookingSide, String)>(
      (ref, k) => ref
          .watch(commsApiProvider)
          .booking(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );
final agreementProvider = FutureProvider.autoDispose
    .family<Agreement, (BookingSide, String)>(
      (ref, k) => ref
          .watch(commsApiProvider)
          .agreement(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );
final agreementVersionsProvider = FutureProvider.autoDispose
    .family<List<AgreementVersionInfo>, (BookingSide, String)>(
      (ref, k) => ref
          .watch(commsApiProvider)
          .agreementVersions(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );
final agreementVersionProvider = FutureProvider.autoDispose
    .family<Agreement, (BookingSide, String, int)>(
      (ref, k) => ref
          .watch(commsApiProvider)
          .agreementVersion(ref.watch(sideOrgProvider(k.$1)), k.$2, k.$3),
    );
final documentHistoryProvider = FutureProvider.autoDispose
    .family<List<DocumentHistoryEntry>, (BookingSide, String)>(
      (ref, k) => ref
          .watch(commsApiProvider)
          .documentHistory(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );
final bookingReviewsProvider = FutureProvider.autoDispose
    .family<BookingReviews, (BookingSide, String)>(
      (ref, k) => ref
          .watch(commsApiProvider)
          .bookingReviews(ref.watch(sideOrgProvider(k.$1)), k.$2),
    );

final conversationsProvider = FutureProvider.autoDispose<List<Conversation>>(
  (ref) => ref.watch(commsApiProvider).conversations(),
);
final conversationProvider = FutureProvider.autoDispose
    .family<Conversation, String>(
      (ref, id) => ref.watch(commsApiProvider).conversation(id),
    );

final maintenanceDueProvider = FutureProvider.autoDispose<List<MaintenanceDue>>(
  (ref) => ref
      .watch(commsApiProvider)
      .maintenanceDue(ref.watch(sideOrgProvider(BookingSide.owner))),
);
final maintenanceHistoryProvider =
    FutureProvider.autoDispose<List<MaintenanceRecord>>(
      (ref) => ref
          .watch(commsApiProvider)
          .maintenanceHistory(ref.watch(sideOrgProvider(BookingSide.owner))),
    );
final maintenanceCostsProvider = FutureProvider.autoDispose<CostReport>(
  (ref) => ref
      .watch(commsApiProvider)
      .maintenanceCosts(ref.watch(sideOrgProvider(BookingSide.owner))),
);
final maintenancePlansProvider = FutureProvider.autoDispose
    .family<List<MaintenancePlan>, String>(
      (ref, eq) => ref
          .watch(commsApiProvider)
          .maintenancePlans(ref.watch(sideOrgProvider(BookingSide.owner)), eq),
    );

final equipmentReviewsProvider = FutureProvider.autoDispose
    .family<ReviewPage, String>(
      (ref, id) => ref.watch(commsApiProvider).equipmentReviews(id),
    );
final receivedReviewsProvider = FutureProvider.autoDispose<ReviewPage>(
  (ref) => ref
      .watch(commsApiProvider)
      .receivedReviews(ref.watch(sideOrgProvider(BookingSide.owner))),
);
final moderationQueueReviewsProvider = FutureProvider.autoDispose
    .family<List<AdminReview>, String>((ref, filter) {
      final api = ref.watch(commsApiProvider);
      return filter == 'reported'
          ? api.moderationQueue(reported: true)
          : api.moderationQueue(status: filter == 'all' ? null : filter);
    });
