import 'dart:async';

import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/comms/data/comms_api.dart';
import 'package:equiprent_app/features/comms/data/comms_models.dart';
import 'package:equiprent_app/features/bookings/data/booking_models.dart'
    hide Json;
import 'package:equiprent_app/features/comms/push/push_service.dart';
import 'package:equiprent_app/features/verification/data/verification_models.dart'
    show PickedFile;

/// In-memory notifications, bookings, agreements, chat, maintenance and
/// reviews. Records every write in [calls] ("method arg…").
class FakeCommsApi implements CommsApi {
  FakeCommsApi() {
    final now = DateTime.now();
    _notifications.addAll([
      AppNotification(
        id: 'n-1',
        type: 'booking.accepted',
        category: 'bookings',
        title: 'Booking accepted',
        body: 'ER-2611-AAAA was accepted by the owner.',
        data: const {'booking_id': 'bk-1'},
        createdAt: now.subtract(const Duration(minutes: 5)),
      ),
      AppNotification(
        id: 'n-2',
        type: 'maintenance.reminder',
        category: 'maintenance',
        title: 'Maintenance coming up',
        body: 'EX200: 500 h service is scheduled tomorrow.',
        data: const {'equipment_id': 'l-live'},
        createdAt: now.subtract(const Duration(hours: 2)),
      ),
      AppNotification(
        id: 'n-3',
        type: 'payment.captured',
        category: 'payments',
        title: 'Payment confirmed',
        body: 'We received ₹19,470.00 for ER-2611-BBBB.',
        data: const {'booking_id': 'bk-2'},
        createdAt: now.subtract(const Duration(days: 1)),
        readAt: now,
      ),
    ]);
    _messages['conv-1'] = [
      ChatMessage(
        id: 'm-1',
        conversationId: 'conv-1',
        mine: false,
        senderName: 'Ravi Patil',
        body: 'Machine leaves the yard at 7.',
        createdAt: now.subtract(const Duration(minutes: 30)),
      ),
      ChatMessage(
        id: 'm-2',
        conversationId: 'conv-1',
        mine: true,
        senderName: 'Test User',
        body: 'Great, gate 2 please.',
        createdAt: now.subtract(const Duration(minutes: 20)),
      ),
    ];
    _records.addAll([
      MaintenanceRecord(
        id: 'r-open',
        equipmentId: 'l-draft',
        equipmentTitle: 'Liugong 835H loader',
        kind: 'service',
        task: '500 hour service',
        status: 'scheduled',
        blocksCalendar: true,
        hasInvoice: false,
        createdAt: now,
        scheduledFor: now.add(const Duration(days: 1)),
        downtimeStart: now.add(const Duration(days: 1)),
        downtimeEnd: now.add(const Duration(days: 2)),
        vendor: 'Liugong service centre',
      ),
      MaintenanceRecord(
        id: 'r-done',
        equipmentId: 'l-draft',
        equipmentTitle: 'Liugong 835H loader',
        kind: 'repair',
        task: 'Starter motor rewinding',
        status: 'done',
        blocksCalendar: false,
        hasInvoice: true,
        createdAt: now.subtract(const Duration(days: 10)),
        performedAt: now.subtract(const Duration(days: 9)),
        costPaise: 820000,
        partsCostPaise: 500000,
        labourCostPaise: 200000,
      ),
    ]);
  }

  final calls = <String>[];
  final _notifications = <AppNotification>[];
  final _messages = <String, List<ChatMessage>>{};
  final _records = <MaintenanceRecord>[];
  var _prefs = [
    for (final c in PushPreference.labels.keys)
      PushPreference(category: c, push: true),
  ];
  var _agreementSigned = false;
  var _bookingReviewed = <String>{};
  var _replied = false;
  var _moderated = false;
  int _seq = 0;

  /// Makes the next call fail with a business rule error.
  String? failNext;

  void _maybeFail() {
    final m = failNext;
    if (m != null) {
      failNext = null;
      throw ApiException(
        kind: ApiErrorKind.conflict,
        code: 'BUSINESS_RULE_VIOLATION',
        message: m,
        statusCode: 422,
      );
    }
  }

  // ---- notifications
  @override
  Future<NotificationPage> notifications({
    bool unreadOnly = false,
    String? cursor,
  }) async => NotificationPage(items: List.of(_notifications));
  @override
  Future<int> unreadNotifications() async =>
      _notifications.where((n) => !n.read).length;
  @override
  Future<void> markNotificationRead(String id) async {
    calls.add('read $id');
    final i = _notifications.indexWhere((n) => n.id == id);
    if (i >= 0) _notifications[i] = _notifications[i].markedRead();
  }

  @override
  Future<void> markAllNotificationsRead() async {
    calls.add('read-all');
    for (var i = 0; i < _notifications.length; i++) {
      _notifications[i] = _notifications[i].markedRead();
    }
  }

  @override
  Future<List<PushPreference>> pushPreferences() async => _prefs;
  @override
  Future<List<PushPreference>> setPushPreferences(
    List<PushPreference> prefs,
  ) async {
    calls.add('prefs ${prefs.map((p) => '${p.category}=${p.push}').join(',')}');
    _prefs = [
      for (final p in _prefs)
        prefs.where((x) => x.category == p.category).firstOrNull ?? p,
    ];
    return _prefs;
  }

  @override
  Future<void> registerDevice(String platform, String token) async =>
      calls.add('device $platform $token');
  @override
  Future<void> unregisterDevice(String token) async =>
      calls.add('unregister $token');

  // ---- bookings
  BookingSummary _summary(String id, String status, String title) {
    final start = DateTime.now().add(const Duration(days: 4));
    return BookingSummary(
      id: id,
      reference: 'ER-2611-${id.toUpperCase().replaceAll('-', '')}',
      status: status,
      equipmentId: 'l-live',
      equipmentTitle: title,
      start: start,
      end: start.add(const Duration(days: 3)),
      totalPaise: 3894000,
    );
  }

  // Simulated server state, moved by FakeBookingApi and respondToBooking.
  /// Status per booking id (overrides the defaults below).
  final statusOf = <String, String>{};

  /// Status history per booking id (oldest first).
  final historyOf = <String, List<String>>{};

  /// Bookings created through FakeBookingApi: id -> equipment title.
  final created = <String, String>{};

  /// The open extension request per booking id.
  final extensionOf = <String, ExtensionInfo>{};

  /// Money captured per booking (paise).
  final paidOf = <String, int>{};

  /// Operator name per booking id.
  final operatorOf = <String, String>{};

  void moveTo(String id, String status) {
    historyOf.putIfAbsent(id, () => [_defaultStatus(id)]).add(status);
    statusOf[id] = status;
  }

  String _defaultStatus(String id) => switch (id) {
    'bk-req' => 'requested',
    'bk-2' => 'completed',
    _ => created.containsKey(id) ? 'requested' : 'payment_pending',
  };
  String _status(String id) => statusOf[id] ?? _defaultStatus(id);
  String _title(String id) =>
      created[id] ??
      (id == 'bk-2'
          ? 'JCB 3DX backhoe'
          : (id == 'bk-req' ? 'Tata Hitachi EX200' : 'Liugong 835H loader'));

  @override
  Future<List<BookingSummary>> myBookings(
    String? org, {
    String? status,
  }) async => [
    for (final id in created.keys) _summary(id, _status(id), _title(id)),
    _summary('bk-1', _status('bk-1'), 'Liugong 835H loader'),
    _summary('bk-2', _status('bk-2'), 'JCB 3DX backhoe'),
  ];
  @override
  Future<List<BookingSummary>> ownerBookings(
    String? org, {
    String? status,
  }) async => [
    for (final id in created.keys) _summary(id, _status(id), _title(id)),
    _summary('bk-req', _status('bk-req'), 'Tata Hitachi EX200'),
    _summary('bk-1', _status('bk-1'), 'Liugong 835H loader'),
    _summary('bk-2', _status('bk-2'), 'JCB 3DX backhoe'),
  ];

  @override
  Future<BookingDetail> booking(String? org, String id) async {
    if (!const {'bk-1', 'bk-2', 'bk-req'}.contains(id) &&
        !created.containsKey(id)) {
      throw const ApiException(
        kind: ApiErrorKind.notFound,
        code: 'NOT_FOUND',
        message: 'Not found',
        statusCode: 404,
      );
    }
    final owner = org == 'org-owner' || org == 'org-fleetManager';
    final st = _status(id);
    final s = _summary(id, st, _title(id));
    final paid = paidOf[id] ?? 0;
    final history = historyOf[id] ?? [st];
    final ext = st == 'extension_requested' ? extensionOf[id] : null;
    final op = operatorOf[id];
    const dead = {
      'cancelled',
      'refund_pending',
      'refunded',
      'rejected',
      'expired',
    };
    return BookingDetail(
      id: id,
      reference: s.reference,
      status: st,
      equipmentId: 'l-live',
      equipmentTitle: s.equipmentTitle,
      ownerName: 'Patil Earthmovers',
      customerName: 'Shree Builders',
      start: s.start,
      end: s.end,
      withOperator: true,
      totalPaise: s.totalPaise,
      depositPaise: 1000000,
      paymentMode: 'online',
      workDescription: 'Site levelling',
      ownerResponseDueAt: st == 'requested'
          ? DateTime.now().add(const Duration(hours: 2))
          : null,
      version: history.length,
      actions: fakeActions(
        st,
        owner: owner,
        withOperator: true,
        online: true,
        extension: ext,
      ),
      timeline: [
        for (var i = 0; i < history.length; i++)
          BookingEvent(
            from: i == 0 ? null : history[i - 1],
            to: history[i],
            at: DateTime.now().subtract(Duration(minutes: history.length - i)),
            actorRole: 'customer',
          ),
      ],
      operator: op == null
          ? null
          : OperatorOnJob(
              userId: 'op-1',
              name: op,
              status: 'assigned',
              phone: '+919800000111',
            ),
      customer: owner
          ? const CustomerInfo(
              orgName: 'Shree Builders',
              contactName: 'Ravi Deshmukh',
              kycStatus: 'verified',
              completedRentals: 4,
              phone: '+919800000222',
            )
          : null,
      money: MoneySummary(
        advanceDuePaise: 1947000,
        depositPaise: 1000000,
        paidPaise: paid,
        refundedPaise: st == 'refund_pending' ? paid : 0,
        balanceDuePaise: dead.contains(st)
            ? 0
            : (3894000 + 1000000 - paid).clamp(0, 1 << 40),
      ),
      pendingExtension: ext,
      availability: owner && st == 'requested'
          ? const Availability(available: true)
          : null,
      cancellation: const CancellationView(
        summary:
            'Free with 48 hours or more notice; 25% fee at the last minute.',
        feeNowPaise: 0,
      ),
      siteLabel: 'Wakad tower',
      siteAddress: 'Plot 14, Wakad, Pune 411057',
    );
  }

  @override
  Future<void> respondToBooking(
    String? org,
    String id, {
    required bool accept,
    String? reason,
  }) async {
    _maybeFail();
    calls.add(
      '${accept ? 'accept' : 'reject'} $id${reason == null ? '' : ' $reason'}',
    );
    moveTo(id, accept ? 'payment_pending' : 'rejected');
  }

  // ---- agreements
  static const terms =
      'EQUIPMENT RENTAL AGREEMENT\nBooking reference: ER-2611-BK1.\n\n1. PARTIES\nOwner (lessor): Patil Earthmovers Pvt Ltd\n'
      'Customer (lessee): Shree Builders LLP\n\n4. CHARGES\nEstimated total including taxes: ₹38,940.00\n';

  static const consent =
      'I have read this agreement and accept it on behalf of my organisation.';

  /// The current agreement version; bump it to simulate a re-issue.
  int agreementVersionNo = 1;

  Agreement _agreement(String bookingId, int version) {
    final current = version == agreementVersionNo;
    final signed = _agreementSigned && current;
    return Agreement(
      id: 'ag-$bookingId-v$version',
      reference: 'ER-2611-BK1',
      templateVersion: 'rental-v3',
      termsSha256: (current ? 'a' : 'b') * 64,
      terms: terms,
      status: current
          ? (signed ? 'partially_signed' : 'awaiting_signatures')
          : 'superseded',
      signatures: [
        if (signed || !current)
          AgreementSignature(
            party: 'customer',
            signerName: 'Asha Rao',
            method: 'typed_name',
            provider: 'equiprent',
            signedAt: DateTime.now(),
            consentText: consent,
          ),
      ],
      canSign: current && !signed,
      signatureProvider: 'equiprent',
      documentKinds: const ['draft'],
      yourParty: 'customer',
      owner: const AgreementParty(
        legalName: 'Patil Earthmovers Pvt Ltd',
        displayName: 'Patil Earthmovers',
        gstin: '27ABCDE1234F1Z5',
      ),
      customer: const AgreementParty(
        legalName: 'Shree Builders LLP',
        displayName: 'Shree Builders',
      ),
      version: version,
      current: current,
      supersedeReason: current
          ? null
          : "re-issued: the booking's dates or price changed before both parties accepted",
      legalTerms: const LegalTermsInfo(
        version: 'rental-terms-2026.1',
        title: 'Equipment rental terms',
        reviewed: false,
      ),
      consentText: consent,
      assuranceNote: 'This is an acceptance record, not a digital signature.',
    );
  }

  @override
  Future<Agreement> agreement(String? org, String bookingId) async =>
      _agreement(bookingId, agreementVersionNo);

  @override
  Future<SignResult> signAgreement(
    String? org,
    String bookingId, {
    required String termsSha256,
    required String signerName,
    String? designation,
  }) async {
    _maybeFail();
    calls.add(
      'sign $bookingId $termsSha256 $signerName${designation == null ? '' : ' $designation'}',
    );
    _agreementSigned = true;
    return SignResult(agreement: await agreement(org, bookingId));
  }

  @override
  Future<DocumentLink> agreementDocument(String? org, String bookingId) async {
    calls.add('pdf $bookingId');
    return DocumentLink(
      kind: 'draft',
      fileName: 'agreement.pdf',
      url: 'https://files.test/agreement-$bookingId.pdf',
      expiresAt: DateTime.now().add(const Duration(minutes: 5)),
    );
  }

  @override
  Future<List<AgreementVersionInfo>> agreementVersions(
    String? org,
    String bookingId,
  ) async => [
    for (var v = agreementVersionNo; v >= 1; v--)
      AgreementVersionInfo(
        version: v,
        status: v == agreementVersionNo ? 'current' : 'superseded',
        termsSha256: (v == agreementVersionNo ? 'a' : 'b') * 64,
        createdAt: DateTime(2026, 11, 14, 9 + v),
        legalTermsVersion: 'rental-terms-2026.1',
        supersedeReason: v == agreementVersionNo
            ? null
            : "re-issued: the booking's dates or price changed before both parties accepted",
        customerAcceptedAt: v == agreementVersionNo
            ? null
            : DateTime(2026, 11, 14, 9 + v, 30),
      ),
  ];

  @override
  Future<Agreement> agreementVersion(
    String? org,
    String bookingId,
    int version,
  ) async => _agreement(bookingId, version);

  @override
  Future<DocumentLink> agreementVersionDocument(
    String? org,
    String bookingId,
    int version,
  ) async {
    calls.add('pdf $bookingId v$version');
    return DocumentLink(
      kind: 'draft',
      fileName: 'agreement-v$version.pdf',
      url: 'https://files.test/agreement-$bookingId-v$version.pdf',
      expiresAt: DateTime.now(),
    );
  }

  @override
  Future<List<DocumentHistoryEntry>> documentHistory(
    String? org,
    String bookingId,
  ) async => [
    if (_agreementSigned)
      DocumentHistoryEntry(
        at: DateTime(2026, 11, 14, 10),
        document: 'acceptance',
        action: 'accepted',
        description: 'Acceptance recorded for the customer',
        actorName: 'Asha Rao',
      ),
    DocumentHistoryEntry(
      at: DateTime(2026, 11, 14, 9),
      document: 'agreement',
      action: 'issued',
      description: 'Agreement version 1 issued',
    ),
  ];

  // ---- chat
  Conversation _conv(String id) => Conversation(
    id: id,
    kind: id == 'conv-enq'
        ? 'direct'
        : (id == 'conv-support' ? 'support' : 'booking'),
    bookingId: id == 'conv-1' ? 'bk-1' : null,
    equipmentId: id == 'conv-enq' ? 'l-live' : null,
    subject: id == 'conv-enq'
        ? 'Enquiry · Liugong 835H loader'
        : 'ER-2611-BK1 · Liugong 835H loader',
    updatedAt: DateTime.now(),
    lastMessage: (_messages[id] ?? const []).lastOrNull?.body ?? '',
    unread: id == 'conv-1' && !calls.contains('chat-read conv-1') ? 1 : 0,
    bookingReference: id == 'conv-1' ? 'ER-2611-BK1' : null,
    bookingStatus: id == 'conv-1' ? 'payment_pending' : null,
  );

  @override
  Future<List<Conversation>> conversations() async => [
    for (final id in _messages.keys) _conv(id),
  ];
  @override
  Future<int> unreadMessages() async =>
      calls.contains('chat-read conv-1') ? 0 : 1;
  @override
  Future<Conversation> conversation(String id) async => _conv(id);
  @override
  Future<Conversation> openSupportConversation() async {
    calls.add('open-support-chat');
    _messages.putIfAbsent('conv-support', () => []);
    return _conv('conv-support');
  }

  @override
  Future<Conversation> openBookingChat(String? org, String bookingId) async {
    calls.add('open-chat $bookingId');
    _messages.putIfAbsent('conv-1', () => []);
    return _conv('conv-1');
  }

  @override
  Future<Conversation> enquire(
    String? org,
    String equipmentId, {
    String? message,
  }) async {
    calls.add('enquire $equipmentId${message == null ? '' : ' $message'}');
    _messages.putIfAbsent('conv-enq', () => []);
    if (message != null) {
      _messages['conv-enq']!.add(
        ChatMessage(
          id: 'm-e${_seq++}',
          conversationId: 'conv-enq',
          mine: true,
          body: message,
          createdAt: DateTime.now(),
        ),
      );
    }
    return _conv('conv-enq');
  }

  @override
  Future<List<ChatMessage>> messages(
    String conversationId, {
    String? before,
    String? after,
    int limit = 40,
  }) async {
    final all = _messages[conversationId] ?? const <ChatMessage>[];
    if (after != null) {
      final i = all.indexWhere((m) => m.id == after);
      return all.sublist(i + 1);
    }
    return all.reversed.toList(); // newest first
  }

  @override
  Future<ChatMessage> sendMessage(
    String conversationId, {
    String? body,
    String? uploadId,
    required String clientId,
  }) async {
    _maybeFail();
    calls.add(
      'send $conversationId ${body ?? ''}${uploadId == null ? '' : ' +$uploadId'}',
    );
    final m = ChatMessage(
      id: 'm-${_seq++}',
      conversationId: conversationId,
      mine: true,
      senderName: 'Test User',
      body: body,
      createdAt: DateTime.now(),
      attachment: uploadId == null
          ? null
          : ChatAttachment(
              id: uploadId,
              contentType: 'image/jpeg',
              url: 'https://files.test/$uploadId.jpg',
            ),
    );
    _messages.putIfAbsent(conversationId, () => []).add(m);
    return m;
  }

  /// Simulates the other party writing (picked up by polling).
  void receive(String conversationId, String body) =>
      _messages[conversationId]!.add(
        ChatMessage(
          id: 'm-in${_seq++}',
          conversationId: conversationId,
          mine: false,
          senderName: 'Ravi Patil',
          body: body,
          createdAt: DateTime.now(),
        ),
      );

  @override
  Future<void> deleteMessage(String conversationId, String messageId) async {
    calls.add('delete $messageId');
    final list = _messages[conversationId]!;
    final i = list.indexWhere((m) => m.id == messageId);
    list[i] = ChatMessage(
      id: messageId,
      conversationId: conversationId,
      mine: true,
      createdAt: list[i].createdAt,
      deleted: true,
    );
  }

  @override
  Future<void> markConversationRead(String conversationId) async {
    if (!calls.contains('chat-read $conversationId')) {
      calls.add('chat-read $conversationId');
    }
  }

  @override
  Future<String> upload(String purpose, PickedFile file) async {
    calls.add('upload $purpose ${file.name}');
    return 'up-${_seq++}';
  }

  // ---- maintenance
  @override
  Future<List<MaintenanceDue>> maintenanceDue(String? org) async => [
    MaintenanceDue(
      planId: 'p-1',
      task: 'Engine oil and filters',
      equipmentId: 'l-draft',
      equipmentTitle: 'Liugong 835H loader',
      currentHmr: 2410,
      everyHours: 250,
    ),
  ];
  @override
  Future<List<MaintenanceRecord>> maintenanceHistory(
    String? org, {
    String? equipmentId,
    String? kind,
    String? status,
  }) async => List.of(_records);
  @override
  Future<CostReport> maintenanceCosts(
    String? org, {
    String? from,
    String? to,
  }) async => const CostReport(
    from: '2025-11-15',
    to: '2026-11-14',
    totalPaise: 1420000,
    jobs: 2,
    downtimeHours: 12,
    byEquipment: [
      CostTotal(
        key: 'l-live',
        label: 'Liugong 835H loader',
        jobs: 2,
        costPaise: 1420000,
        downtimeHours: 12,
      ),
    ],
    byKind: [
      CostTotal(
        key: 'repair',
        label: 'Repair',
        jobs: 1,
        costPaise: 820000,
        downtimeHours: 12,
      ),
      CostTotal(
        key: 'service',
        label: 'Service',
        jobs: 1,
        costPaise: 600000,
        downtimeHours: 0,
      ),
    ],
    byMonth: [
      CostTotal(
        key: '2026-11',
        label: '2026-11',
        jobs: 2,
        costPaise: 1420000,
        downtimeHours: 12,
      ),
    ],
  );
  final _plans = <MaintenancePlan>[
    const MaintenancePlan(
      id: 'p-1',
      equipmentId: 'l-draft',
      task: 'Engine oil and filters',
      active: true,
      everyHours: 250,
    ),
  ];
  @override
  Future<List<MaintenancePlan>> maintenancePlans(
    String? org,
    String equipmentId,
  ) async => _plans.where((p) => p.equipmentId == equipmentId).toList();
  @override
  Future<MaintenancePlan> createPlan(
    String? org,
    String equipmentId,
    Json body,
  ) async {
    calls.add(
      'plan $equipmentId ${body['task']} ${body['every_hours']} ${body['every_days']}',
    );
    final p = MaintenancePlan(
      id: 'p-${_seq++}',
      equipmentId: equipmentId,
      task: body['task'] as String,
      active: true,
      everyHours: body['every_hours'] as int?,
      everyDays: body['every_days'] as int?,
    );
    _plans.add(p);
    return p;
  }

  @override
  Future<MaintenancePlan> updatePlan(
    String? org,
    String equipmentId,
    String planId,
    Json body,
  ) async => _plans.first;

  @override
  Future<MaintenanceRecord> createRecord(
    String? org,
    String equipmentId,
    Json body,
  ) async {
    _maybeFail();
    calls.add(
      'record $equipmentId ${body['kind']} ${body['status']} ${body['task']}${body['downtime'] == null ? '' : ' downtime'}',
    );
    final r = MaintenanceRecord(
      id: 'r-${_seq++}',
      equipmentId: equipmentId,
      equipmentTitle: 'Liugong 835H loader',
      kind: body['kind'] as String,
      task: body['task'] as String,
      status: body['status'] as String,
      blocksCalendar: body['block_calendar'] == true,
      hasInvoice: false,
      createdAt: DateTime.now(),
      scheduledFor: DateTime.now().add(const Duration(days: 1)),
    );
    _records.insert(0, r);
    return r;
  }

  @override
  Future<MaintenanceRecord> updateRecord(
    String? org,
    String equipmentId,
    String recordId,
    Json body,
  ) async {
    calls.add(
      'update $recordId ${body['status']} parts=${body['parts_cost_paise']} labour=${body['labour_cost_paise']}',
    );
    final i = _records.indexWhere((r) => r.id == recordId);
    final r = _records[i];
    _records[i] = MaintenanceRecord(
      id: r.id,
      equipmentId: r.equipmentId,
      equipmentTitle: r.equipmentTitle,
      kind: r.kind,
      task: r.task,
      status: body['status'] as String,
      blocksCalendar: false,
      hasInvoice: false,
      createdAt: r.createdAt,
      performedAt: DateTime.now(),
    );
    return _records[i];
  }

  @override
  Future<String> invoiceLink(
    String? org,
    String equipmentId,
    String recordId,
  ) async {
    calls.add('invoice $recordId');
    return 'https://files.test/invoice-$recordId.pdf';
  }

  // ---- reviews
  @override
  Future<ReviewPage> equipmentReviews(
    String equipmentId, {
    String? cursor,
  }) async => ReviewPage(
    items: [
      Review(
        id: 'rv-1',
        bookingId: 'bk-0',
        subjectType: 'equipment',
        rating: 4,
        comment: 'Strong loader, a little slow to start.',
        reviewerName: 'Shree Builders',
        reply: 'Thanks! Starter is fixed now.',
        createdAt: DateTime.now().subtract(const Duration(days: 3)),
      ),
    ],
    summary: const ReviewSummary(
      average: 4.0,
      count: 1,
      histogram: {1: 0, 2: 0, 3: 0, 4: 1, 5: 0},
    ),
  );
  @override
  Future<ReviewPage> ownerReviews(String orgId, {String? cursor}) async =>
      const ReviewPage(items: []);
  @override
  Future<BookingReviews> bookingReviews(String? org, String bookingId) async =>
      BookingReviews(
        items: [
          for (final s in _bookingReviewed)
            Review(
              id: 'rv-$s',
              bookingId: bookingId,
              subjectType: s,
              rating: 5,
              status: s == 'owner' ? 'pending' : 'published',
              reviewerName: 'Shree Builders',
              createdAt: DateTime.now(),
            ),
        ],
        canReview: [
          for (final s in const ['equipment', 'owner'])
            if (!_bookingReviewed.contains(s)) s,
        ],
        open: bookingId == 'bk-2',
      );

  @override
  Future<Review> createReview(
    String? org,
    String bookingId, {
    required String subject,
    required int rating,
    String? comment,
  }) async {
    calls.add(
      'review $bookingId $subject $rating${comment == null ? '' : ' $comment'}',
    );
    _bookingReviewed = {..._bookingReviewed, subject};
    return Review(
      id: 'rv-$subject',
      bookingId: bookingId,
      subjectType: subject,
      rating: rating,
      comment: comment,
      status: (comment ?? '').contains('98765') ? 'pending' : 'published',
      createdAt: DateTime.now(),
    );
  }

  @override
  Future<ReviewPage> receivedReviews(String? org, {String? cursor}) async =>
      ReviewPage(
        items: [
          Review(
            id: 'rv-9',
            bookingId: 'bk-0',
            subjectType: 'owner',
            rating: 5,
            comment: 'Very helpful owner.',
            reviewerName: 'Shree Builders',
            reply: _replied ? 'Thank you!' : null,
            createdAt: DateTime.now(),
          ),
        ],
        summary: const ReviewSummary(average: 5, count: 1, histogram: {5: 1}),
      );

  @override
  Future<void> replyToReview(String? org, String reviewId, String reply) async {
    calls.add('reply $reviewId $reply');
    _replied = true;
  }

  @override
  Future<bool> reportReview(String reviewId, String reason) async {
    calls.add('report $reviewId $reason');
    return true;
  }

  @override
  Future<List<AdminReview>> moderationQueue({
    String? status,
    bool reported = false,
  }) async => [
    if (!_moderated)
      AdminReview(
        id: 'rv-held',
        subjectType: 'owner',
        subjectName: 'Patil Earthmovers',
        rating: 5,
        comment: 'Call Ravi on 98765 43210',
        status: 'pending',
        screeningFlags: const ['contact_details'],
        reportCount: 0,
        reportReasons: const [],
        reviewerName: 'Shree Builders',
        createdAt: DateTime.now(),
      ),
  ];

  @override
  Future<void> moderateReview(
    String reviewId, {
    required bool publish,
    required String reason,
  }) async {
    calls.add('moderate $reviewId ${publish ? 'publish' : 'hide'} $reason');
    _moderated = true;
  }
}

/// A push service that is "configured", for coordinator tests.
class FakePushService implements PushService {
  FakePushService({this.token = 'fcm-test-token-0001'});
  final String token;
  final foregroundCtl = StreamController<PushMessage>.broadcast();
  final openedCtl = StreamController<PushMessage>.broadcast();
  int starts = 0;
  int stops = 0;

  @override
  bool get available => true;
  @override
  Future<String?> start() async {
    starts++;
    return token;
  }

  @override
  Future<void> stop() async => stops++;
  @override
  Stream<String> get tokenRefresh => const Stream.empty();
  @override
  Stream<PushMessage> get foreground => foregroundCtl.stream;
  @override
  Stream<PushMessage> get opened => openedCtl.stream;
}

/// The actions the server would offer (a port of bookings.actionsFor for
/// the statuses the tests use).
Set<String> fakeActions(
  String st, {
  required bool owner,
  required bool withOperator,
  required bool online,
  ExtensionInfo? extension,
}) {
  final out = <String>{};
  void add(bool when, List<String> names) {
    if (when) out.addAll(names);
  }

  if (!owner) {
    add(
      online &&
          const {
            'accepted',
            'payment_pending',
            'payment_processing',
          }.contains(st),
      ['pay'],
    );
    add(
      const {
        'draft',
        'requested',
        'quoted',
        'accepted',
        'payment_pending',
        'payment_processing',
        'confirmed',
        'operator_assigned',
      }.contains(st),
      ['cancel'],
    );
    add(const {'dispatched', 'on_site'}.contains(st), ['request_cancellation']);
    add(st == 'cancellation_requested', ['withdraw_cancellation']);
    add(st == 'on_site', ['confirm_handover']);
    add(st == 'active', ['request_return']);
  } else {
    add(const {'requested', 'quoted'}.contains(st), ['accept', 'reject']);
    add(st == 'confirmed' && withOperator, ['assign_operator']);
    add(const {'confirmed', 'operator_assigned'}.contains(st), ['dispatch']);
    add(
      const {
        'confirmed',
        'operator_assigned',
        'dispatched',
        'on_site',
      }.contains(st),
      ['cancel'],
    );
    add(st == 'cancellation_requested', [
      'approve_cancellation',
      'decline_cancellation',
    ]);
    add(st == 'dispatched', ['arrive']);
    add(st == 'return_initiated', ['mark_returned']);
    add(st == 'returned', ['complete', 'issue_invoice', 'settle_final']);
  }
  add(st == 'active', ['request_extension']);
  if (extension != null) {
    final mine = (extension.requestedSide == 'customer') != owner;
    add(mine, ['withdraw_extension']);
    add(!mine, ['approve_extension', 'decline_extension']);
  }
  add(
    const {
      'dispatched',
      'on_site',
      'active',
      'extension_requested',
      'return_initiated',
    }.contains(st),
    ['track'],
  );
  add(const {'active', 'returned', 'completed'}.contains(st), ['dispute']);
  add(const {'returned', 'completed', 'closed', 'disputed'}.contains(st), [
    'invoices',
  ]);
  add(const {'completed', 'closed'}.contains(st), ['review']);
  return out;
}
