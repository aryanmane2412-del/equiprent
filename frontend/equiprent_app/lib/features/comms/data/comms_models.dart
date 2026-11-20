// Models for the communication and operations modules: notifications,
// bookings (as seen by the parties), rental agreements, chat, maintenance
// and reviews. Amounts are in paise, as the API sends them.

import '../../bookings/data/booking_models.dart' as be;

typedef Json = Map<String, dynamic>;

Json _j(Object? v) => (v as Map).cast<String, dynamic>();
List<Json> _list(Object? v) => [for (final e in (v as List? ?? const [])) _j(e)];
DateTime? _dt(Object? v) => v == null ? null : DateTime.parse(v as String).toLocal();
DateTime _dtReq(Object? v) => DateTime.parse(v as String).toLocal();
int _int(Object? v) => (v as num?)?.toInt() ?? 0;
int? _intOrNull(Object? v) => (v as num?)?.toInt();
double _dbl(Object? v) => (v as num?)?.toDouble() ?? 0;
String? _str(Object? v) => v?.toString();

/// Rupees from paise.
double rupees(int paise) => paise / 100;

// ---- notifications -----------------------------------------------------------------------

class AppNotification {
  const AppNotification({
    required this.id,
    required this.type,
    required this.category,
    required this.title,
    required this.body,
    required this.data,
    required this.createdAt,
    this.readAt,
  });

  factory AppNotification.fromJson(Json j) => AppNotification(
        id: j['id'] as String,
        type: j['type'] as String,
        category: (j['category'] as String?) ?? 'account',
        title: j['title'] as String,
        body: j['body'] as String,
        data: j['data'] == null ? const {} : _j(j['data']),
        readAt: _dt(j['read_at']),
        createdAt: _dtReq(j['created_at']),
      );

  final String id;
  final String type;
  final String category;
  final String title;
  final String body;
  final Json data;
  final DateTime? readAt;
  final DateTime createdAt;

  bool get read => readAt != null;
  String? get bookingId => _str(data['booking_id']);
  String? get conversationId => _str(data['conversation_id']);
  String? get equipmentId => _str(data['equipment_id']);

  AppNotification markedRead() => AppNotification(
      id: id, type: type, category: category, title: title, body: body, data: data, createdAt: createdAt, readAt: DateTime.now());
}

class NotificationPage {
  const NotificationPage({required this.items, this.nextCursor});
  factory NotificationPage.fromJson(Json j) => NotificationPage(
      items: [for (final n in _list(j['data'])) AppNotification.fromJson(n)], nextCursor: j['next_cursor'] as String?);
  final List<AppNotification> items;
  final String? nextCursor;
}

/// Push preference for one category (in-app notifications are always kept).
class PushPreference {
  const PushPreference({required this.category, required this.push});
  factory PushPreference.fromJson(Json j) => PushPreference(category: j['category'] as String, push: j['push'] as bool);
  final String category;
  final bool push;
  Json toJson() => {'category': category, 'push': push};

  static const labels = {
    'bookings': ('Bookings', 'Requests, acceptance, operator, start and completion'),
    'payments': ('Payments', 'Payment confirmations, final bills and settlements'),
    'agreements': ('Agreements', 'Signatures and completed agreements'),
    'chat': ('Chat', 'New messages from the other party'),
    'maintenance': ('Maintenance', 'Service reminders, overdue jobs and expiring documents'),
    'reviews': ('Reviews', 'New ratings and reviews'),
    'account': ('Account', 'Verification and account updates'),
  };
  String get label => labels[category]?.$1 ?? category;
  String get description => labels[category]?.$2 ?? '';
}

// ---- bookings -------------------------------------------------------------------------------

/// Booking status as shown to people.
String bookingStatusLabel(String s) => switch (s) {
      'requested' => 'Requested',
      'accepted' => 'Accepted',
      'payment_pending' => 'Payment pending',
      'payment_processing' => 'Payment processing',
      'confirmed' => 'Confirmed',
      'operator_assigned' => 'Operator assigned',
      'dispatched' => 'On the way',
      'on_site' => 'On site',
      'active' => 'In progress',
      'extension_requested' => 'Extension requested',
      'return_initiated' => 'Return requested',
      'returned' => 'Returned',
      'completed' => 'Completed',
      'closed' => 'Closed',
      'rejected' => 'Declined',
      'expired' => 'Expired',
      'cancelled' => 'Cancelled',
      'cancellation_requested' => 'Cancellation requested',
      'refund_pending' => 'Refund pending',
      'refunded' => 'Refunded',
      'disputed' => 'Disputed',
      'draft' => 'Draft',
      _ => s,
    };

class BookingSummary {
  const BookingSummary({
    required this.id,
    required this.reference,
    required this.status,
    required this.equipmentId,
    required this.equipmentTitle,
    required this.start,
    required this.end,
    required this.totalPaise,
  });

  factory BookingSummary.fromJson(Json j) => BookingSummary(
        id: j['id'] as String,
        reference: j['reference'] as String,
        status: j['status'] as String,
        equipmentId: j['equipment_id'] as String,
        equipmentTitle: j['equipment_title'] as String,
        start: _dtReq(_j(j['period'])['start']),
        end: _dtReq(_j(j['period'])['end']),
        totalPaise: _int(j['estimated_total_paise']),
      );

  final String id;
  final String reference;
  final String status;
  final String equipmentId;
  final String equipmentTitle;
  final DateTime start;
  final DateTime end;
  final int totalPaise;
}

class BookingDetail {
  const BookingDetail({
    required this.id,
    required this.reference,
    required this.status,
    required this.equipmentId,
    required this.equipmentTitle,
    required this.ownerName,
    required this.customerName,
    required this.start,
    required this.end,
    required this.withOperator,
    required this.totalPaise,
    required this.depositPaise,
    required this.paymentMode,
    required this.workDescription,
    this.finalTotalPaise,
    this.operatorName,
    this.ownerResponseDueAt,
    this.version = 1,
    this.actions = const {},
    this.timeline = const [],
    this.operator,
    this.customer,
    this.money,
    this.pendingExtension,
    this.availability,
    this.cancellation,
    this.price,
    this.siteLabel = '',
    this.siteAddress = '',
    this.cancelReason,
    this.paymentDueAt,
    this.delivery = true,
    this.pickup = true,
    this.finalizedAt,
  });

  factory BookingDetail.fromJson(Json j) => BookingDetail(
        id: j['id'] as String,
        reference: j['reference'] as String,
        status: j['status'] as String,
        equipmentId: j['equipment_id'] as String,
        equipmentTitle: j['equipment_title'] as String,
        ownerName: (j['owner_name'] as String?) ?? '',
        customerName: (j['customer_name'] as String?) ?? '',
        start: _dtReq(_j(j['period'])['start']),
        end: _dtReq(_j(j['period'])['end']),
        withOperator: j['with_operator'] == true,
        totalPaise: _int(j['estimated_total_paise']),
        finalTotalPaise: _intOrNull(j['final_total_paise']),
        depositPaise: _int(j['deposit_paise']),
        paymentMode: (j['payment_mode'] as String?) ?? 'online',
        workDescription: (j['work_description'] as String?) ?? '',
        operatorName: j['operator'] == null ? null : _j(j['operator'])['name'] as String?,
        ownerResponseDueAt: _dt(j['owner_response_due_at']),
        version: _int(j['version']),
        actions: {for (final a in (j['actions'] as List? ?? const [])) '$a'},
        timeline: [for (final e in _list(j['timeline'])) be.BookingEvent.fromJson(e)],
        operator: j['operator'] == null ? null : be.OperatorOnJob.fromJson(_j(j['operator'])),
        customer: j['customer'] == null ? null : be.CustomerInfo.fromJson(_j(j['customer'])),
        money: j['money'] == null ? null : be.MoneySummary.fromJson(_j(j['money'])),
        pendingExtension: j['pending_extension'] == null ? null : be.ExtensionInfo.fromJson(_j(j['pending_extension'])),
        availability: j['availability'] == null ? null : be.Availability.fromJson(_j(j['availability'])),
        cancellation: j['cancellation_policy'] == null ? null : be.CancellationView.fromJson(_j(j['cancellation_policy'])),
        price: j['price'] is Map ? be.Estimate.fromJson(_j(j['price'])) : null,
        siteLabel: (j['site_label'] as String?) ?? '',
        siteAddress: (j['site_address'] as String?) ?? '',
        cancelReason: _str(j['cancel_reason']),
        paymentDueAt: _dt(j['payment_due_at']),
        delivery: j['delivery'] != false,
        pickup: j['pickup'] != false,
        finalizedAt: _dt(j['finalized_at']),
      );

  final String id;
  final String reference;
  final String status;
  final String equipmentId;
  final String equipmentTitle;
  final String ownerName;
  final String customerName;
  final DateTime start;
  final DateTime end;
  final bool withOperator;
  final int totalPaise;
  final int? finalTotalPaise;
  final int depositPaise;
  final String paymentMode;
  final String workDescription;
  final String? operatorName;
  final DateTime? ownerResponseDueAt;

  /// Optimistic-concurrency version (sent back with lifecycle actions).
  final int version;

  /// What the caller may do now; the names match the lifecycle endpoints
  /// and the server re-checks each one.
  final Set<String> actions;
  final List<be.BookingEvent> timeline;
  final be.OperatorOnJob? operator;
  final be.CustomerInfo? customer;
  final be.MoneySummary? money;
  final be.ExtensionInfo? pendingExtension;
  final be.Availability? availability;
  final be.CancellationView? cancellation;
  final be.Estimate? price;
  final String siteLabel;
  final String siteAddress;
  final String? cancelReason;
  final DateTime? paymentDueAt;
  final bool delivery;
  final bool pickup;
  final DateTime? finalizedAt;

  bool can(String action) => actions.contains(action);

  /// The owner has accepted and the rental has not ended badly: an
  /// agreement exists or can be generated.
  bool get hasAgreement => !const {'requested', 'quoted', 'rejected', 'expired', 'draft'}.contains(status);
  bool get finished => status == 'completed' || status == 'closed';
}

// ---- agreements ---------------------------------------------------------------------------

class AgreementParty {
  const AgreementParty({required this.legalName, required this.displayName, this.gstin, this.address, this.contact});
  factory AgreementParty.fromJson(Json j) => AgreementParty(
        legalName: j['legal_name'] as String,
        displayName: (j['display_name'] as String?) ?? '',
        gstin: j['gstin'] as String?,
        address: j['address'] as String?,
        contact: [j['contact_name'], j['contact_phone']].whereType<String>().join(' ').trim(),
      );
  final String legalName;
  final String displayName;
  final String? gstin;
  final String? address;
  final String? contact;
}

/// One party's acceptance record (typed name + consent) or, through an
/// external provider, electronic signature.
class AgreementSignature {
  const AgreementSignature({
    required this.party,
    required this.signerName,
    required this.method,
    required this.provider,
    required this.signedAt,
    this.designation,
    this.kind = 'acceptance_record',
    this.consentText,
  });
  factory AgreementSignature.fromJson(Json j) => AgreementSignature(
        party: j['party'] as String,
        signerName: j['signer_name'] as String,
        designation: j['designation'] as String?,
        method: j['method'] as String,
        provider: j['provider'] as String,
        signedAt: _dtReq(j['signed_at']),
        kind: (j['kind'] as String?) ?? (j['method'] == 'provider' ? 'electronic_signature' : 'acceptance_record'),
        consentText: j['consent_text'] as String?,
      );
  final String party;
  final String signerName;
  final String? designation;
  final String method;
  final String provider;
  final DateTime signedAt;
  final String kind;
  final String? consentText;

  bool get isElectronicSignature => kind == 'electronic_signature';
}

/// The legal template an agreement's clauses come from.
class LegalTermsInfo {
  const LegalTermsInfo({required this.version, required this.title, required this.reviewed, this.reviewedBy, this.reviewedAt});
  factory LegalTermsInfo.fromJson(Json j) => LegalTermsInfo(
        version: (j['version'] as String?) ?? '',
        title: (j['title'] as String?) ?? '',
        reviewed: j['reviewed'] == true,
        reviewedBy: j['reviewed_by'] as String?,
        reviewedAt: _dt(j['reviewed_at']),
      );
  final String version;
  final String title;
  final bool reviewed;
  final String? reviewedBy;
  final DateTime? reviewedAt;
}

class Agreement {
  const Agreement({
    required this.id,
    required this.reference,
    required this.templateVersion,
    required this.termsSha256,
    required this.terms,
    required this.status,
    required this.signatures,
    required this.canSign,
    required this.signatureProvider,
    required this.documentKinds,
    this.yourParty,
    this.owner,
    this.customer,
    this.version = 1,
    this.current = true,
    this.supersedeReason,
    this.legalTerms,
    this.consentText = defaultConsentText,
    this.assurance = 'acceptance_record',
    this.assuranceNote = '',
  });

  static const defaultConsentText =
      'I have read this agreement and accept it on behalf of my organisation. I understand that my acceptance is recorded electronically.';

  factory Agreement.fromJson(Json j) => Agreement(
        id: j['id'] as String,
        reference: (j['reference'] as String?) ?? '',
        templateVersion: j['template_version'] as String,
        termsSha256: j['terms_sha256'] as String,
        terms: (j['terms'] as String?) ?? '',
        status: (j['status'] as String?) ?? 'awaiting_signatures',
        signatures: [for (final s in _list(j['signatures'])) AgreementSignature.fromJson(s)],
        canSign: j['can_sign'] == true,
        signatureProvider: (j['signature_provider'] as String?) ?? 'equiprent',
        documentKinds: [for (final d in _list(j['documents'])) d['kind'] as String],
        yourParty: j['your_party'] as String?,
        owner: j['owner'] == null ? null : AgreementParty.fromJson(_j(j['owner'])),
        customer: j['customer'] == null ? null : AgreementParty.fromJson(_j(j['customer'])),
        version: (j['version'] as num?)?.toInt() ?? 1,
        current: j['current'] != false,
        supersedeReason: j['supersede_reason'] as String?,
        legalTerms: j['legal_terms'] == null ? null : LegalTermsInfo.fromJson(_j(j['legal_terms'])),
        consentText: (j['consent_text'] as String?)?.trim().isNotEmpty == true ? j['consent_text'] as String : defaultConsentText,
        assurance: (j['assurance'] as String?) ?? 'acceptance_record',
        assuranceNote: (j['assurance_note'] as String?) ?? '',
      );

  final String id;
  final String reference;
  final String templateVersion;
  final String termsSha256;
  final String terms;
  final String status;
  final List<AgreementSignature> signatures;
  final bool canSign;
  final String signatureProvider;
  final List<String> documentKinds;
  final String? yourParty;
  final AgreementParty? owner;
  final AgreementParty? customer;
  final int version;
  final bool current;
  final String? supersedeReason;
  final LegalTermsInfo? legalTerms;
  final String consentText;
  final String assurance;
  final String assuranceNote;

  bool get executed => status == 'executed';
  bool get superseded => status == 'superseded';

  /// An external e-sign provider signs; otherwise parties record acceptance.
  bool get usesElectronicSignature => assurance == 'electronic_signature';
  AgreementSignature? signatureOf(String party) => signatures.where((s) => s.party == party).firstOrNull;

  String get statusLabel => switch (status) {
        'executed' => usesElectronicSignature ? 'Signed by both parties' : 'Accepted by both parties',
        'partially_signed' => 'Waiting for the other party',
        'outdated' => 'Outdated',
        'superseded' => 'Superseded by a newer version',
        _ => usesElectronicSignature ? 'Awaiting signatures' : 'Awaiting acceptance',
      };
}

/// One issued version of a booking's agreement.
class AgreementVersionInfo {
  const AgreementVersionInfo({
    required this.version,
    required this.status,
    required this.termsSha256,
    required this.createdAt,
    this.legalTermsVersion,
    this.supersededAt,
    this.supersedeReason,
    this.customerAcceptedAt,
    this.ownerAcceptedAt,
  });
  factory AgreementVersionInfo.fromJson(Json j) => AgreementVersionInfo(
        version: (j['version'] as num).toInt(),
        status: (j['status'] as String?) ?? 'current',
        termsSha256: (j['terms_sha256'] as String?) ?? '',
        createdAt: _dtReq(j['created_at']),
        legalTermsVersion: j['legal_terms_version'] as String?,
        supersededAt: _dt(j['superseded_at']),
        supersedeReason: j['supersede_reason'] as String?,
        customerAcceptedAt: _dt(j['customer_accepted_at']),
        ownerAcceptedAt: _dt(j['owner_accepted_at']),
      );
  final int version;
  final String status; // current | executed | superseded
  final String termsSha256;
  final DateTime createdAt;
  final String? legalTermsVersion;
  final DateTime? supersededAt;
  final String? supersedeReason;
  final DateTime? customerAcceptedAt;
  final DateTime? ownerAcceptedAt;
}

/// One audited event on a booking's documents.
class DocumentHistoryEntry {
  const DocumentHistoryEntry({required this.at, required this.document, required this.action, required this.description, this.actorName, this.actorRole});
  factory DocumentHistoryEntry.fromJson(Json j) => DocumentHistoryEntry(
        at: _dtReq(j['at']),
        document: (j['document'] as String?) ?? '',
        action: (j['action'] as String?) ?? '',
        description: (j['description'] as String?) ?? '',
        actorName: (j['actor_name'] as String?)?.trim().isEmpty ?? true ? null : j['actor_name'] as String,
        actorRole: j['actor_role'] as String?,
      );
  final DateTime at;
  final String document; // agreement | acceptance | invoice
  final String action;
  final String description;
  final String? actorName;
  final String? actorRole;
}

class SignResult {
  const SignResult({required this.agreement, this.redirectUrl});
  factory SignResult.fromJson(Json j) => SignResult(
        agreement: Agreement.fromJson(_j(j['agreement'])),
        redirectUrl: j['redirect'] == null ? null : _j(j['redirect'])['url'] as String?,
      );
  final Agreement agreement;
  final String? redirectUrl;
}

class DocumentLink {
  const DocumentLink({required this.kind, required this.fileName, required this.url, required this.expiresAt});
  factory DocumentLink.fromJson(Json j) => DocumentLink(
      kind: (j['kind'] as String?) ?? 'document', fileName: (j['file_name'] as String?) ?? 'document.pdf',
      url: j['url'] as String, expiresAt: _dtReq(j['expires_at']));
  final String kind;
  final String fileName;
  final String url;
  final DateTime expiresAt;
}

// ---- chat ------------------------------------------------------------------------------------

class Conversation {
  const Conversation({
    required this.id,
    required this.kind,
    required this.updatedAt,
    this.bookingId,
    this.equipmentId,
    this.subject,
    this.lastMessage = '',
    this.unread = 0,
    this.bookingReference,
    this.bookingStatus,
  });

  factory Conversation.fromJson(Json j) => Conversation(
        id: j['id'] as String,
        kind: j['kind'] as String,
        bookingId: j['booking_id'] as String?,
        equipmentId: j['equipment_id'] as String?,
        subject: j['subject'] as String?,
        updatedAt: _dtReq(j['updated_at']),
        lastMessage: (j['last_message'] as String?) ?? '',
        unread: _int(j['unread']),
        bookingReference: (j['booking_reference'] as String?)?.isEmpty == true ? null : j['booking_reference'] as String?,
        bookingStatus: (j['booking_status'] as String?)?.isEmpty == true ? null : j['booking_status'] as String?,
      );

  final String id;
  final String kind;
  final String? bookingId;
  final String? equipmentId;
  final String? subject;
  final DateTime updatedAt;
  final String lastMessage;
  final int unread;
  final String? bookingReference;
  final String? bookingStatus;

  bool get isEnquiry => kind == 'direct' && equipmentId != null;
  String get title => subject ?? (isEnquiry ? 'Enquiry' : 'Conversation');
}

class ChatAttachment {
  const ChatAttachment({required this.id, required this.contentType, required this.url});
  factory ChatAttachment.fromJson(Json j) => ChatAttachment(id: j['id'] as String, contentType: (j['content_type'] as String?) ?? 'image/jpeg', url: j['url'] as String);
  final String id;
  final String contentType;
  final String url;
}

class ChatMessage {
  const ChatMessage({
    required this.id,
    required this.conversationId,
    required this.mine,
    required this.createdAt,
    this.senderName,
    this.body,
    this.attachment,
    this.deleted = false,
    this.clientId,
    this.sending = false,
    this.failed = false,
    this.localImage,
  });

  factory ChatMessage.fromJson(Json j) => ChatMessage(
        id: j['id'] as String,
        conversationId: j['conversation_id'] as String,
        senderName: j['sender_name'] as String?,
        mine: j['mine'] == true,
        body: j['body'] as String?,
        attachment: j['attachment'] == null ? null : ChatAttachment.fromJson(_j(j['attachment'])),
        createdAt: _dtReq(j['created_at']),
        deleted: j['deleted'] == true,
      );

  final String id;
  final String conversationId;
  final bool mine;
  final DateTime createdAt;
  final String? senderName;
  final String? body;
  final ChatAttachment? attachment;
  final bool deleted;

  /// Client-side state for messages being sent.
  final String? clientId;
  final bool sending;
  final bool failed;
  final List<int>? localImage;

  bool get canDelete => mine && !deleted && !sending && DateTime.now().difference(createdAt) < const Duration(minutes: 15);
}

// ---- maintenance ---------------------------------------------------------------------------

const maintenanceKinds = ['service', 'repair', 'inspection', 'breakdown'];

String kindLabel(String k) => switch (k) {
      'service' => 'Service',
      'repair' => 'Repair',
      'inspection' => 'Inspection',
      'breakdown' => 'Breakdown',
      _ => k,
    };

String maintenanceStatusLabel(String s) => switch (s) {
      'scheduled' => 'Scheduled',
      'in_progress' => 'In progress',
      'done' => 'Done',
      'cancelled' => 'Cancelled',
      _ => s,
    };

class MaintenancePlan {
  const MaintenancePlan({required this.id, required this.equipmentId, required this.task, required this.active, this.everyHours, this.everyDays, this.lastDoneAt, this.lastDoneHmr});
  factory MaintenancePlan.fromJson(Json j) => MaintenancePlan(
        id: j['id'] as String,
        equipmentId: j['equipment_id'] as String,
        task: j['task'] as String,
        active: j['active'] != false,
        everyHours: _intOrNull(j['every_hours']),
        everyDays: _intOrNull(j['every_days']),
        lastDoneAt: _dt(j['last_done_at']),
        lastDoneHmr: (j['last_done_hmr'] as num?)?.toDouble(),
      );
  final String id;
  final String equipmentId;
  final String task;
  final bool active;
  final int? everyHours;
  final int? everyDays;
  final DateTime? lastDoneAt;
  final double? lastDoneHmr;

  String get interval => [if (everyHours != null) 'every $everyHours h', if (everyDays != null) 'every $everyDays days'].join(' or ');
}

class MaintenanceRecord {
  const MaintenanceRecord({
    required this.id,
    required this.equipmentId,
    required this.equipmentTitle,
    required this.kind,
    required this.task,
    required this.status,
    required this.blocksCalendar,
    required this.hasInvoice,
    required this.createdAt,
    this.planId,
    this.scheduledFor,
    this.performedAt,
    this.hmrAt,
    this.costPaise,
    this.partsCostPaise,
    this.labourCostPaise,
    this.vendor,
    this.notes,
    this.downtimeStart,
    this.downtimeEnd,
  });

  factory MaintenanceRecord.fromJson(Json j) => MaintenanceRecord(
        id: j['id'] as String,
        equipmentId: j['equipment_id'] as String,
        equipmentTitle: (j['equipment_title'] as String?) ?? '',
        planId: j['plan_id'] as String?,
        kind: (j['kind'] as String?) ?? 'service',
        task: j['task'] as String,
        status: j['status'] as String,
        scheduledFor: _dt(j['scheduled_for']),
        performedAt: _dt(j['performed_at']),
        hmrAt: (j['hmr_at'] as num?)?.toDouble(),
        costPaise: _intOrNull(j['cost_paise']),
        partsCostPaise: _intOrNull(j['parts_cost_paise']),
        labourCostPaise: _intOrNull(j['labour_cost_paise']),
        vendor: j['vendor'] as String?,
        notes: j['notes'] as String?,
        downtimeStart: j['downtime'] == null ? null : _dt(_j(j['downtime'])['start']),
        downtimeEnd: j['downtime'] == null ? null : _dt(_j(j['downtime'])['end']),
        blocksCalendar: j['blocks_calendar'] == true,
        hasInvoice: j['has_invoice'] == true,
        createdAt: _dtReq(j['created_at']),
      );

  final String id;
  final String equipmentId;
  final String equipmentTitle;
  final String? planId;
  final String kind;
  final String task;
  final String status;
  final DateTime? scheduledFor;
  final DateTime? performedAt;
  final double? hmrAt;
  final int? costPaise;
  final int? partsCostPaise;
  final int? labourCostPaise;
  final String? vendor;
  final String? notes;
  final DateTime? downtimeStart;
  final DateTime? downtimeEnd;
  final bool blocksCalendar;
  final bool hasInvoice;
  final DateTime createdAt;

  bool get open => status == 'scheduled' || status == 'in_progress';
  bool get overdue {
    if (status != 'scheduled' || scheduledFor == null) return false;
    final now = DateTime.now();
    return scheduledFor!.isBefore(DateTime(now.year, now.month, now.day));
  }
}

class MaintenanceDue {
  const MaintenanceDue({required this.planId, required this.task, required this.equipmentId, required this.equipmentTitle, required this.currentHmr, this.everyHours, this.everyDays, this.lastDoneAt, this.lastDoneHmr});
  factory MaintenanceDue.fromJson(Json j) => MaintenanceDue(
        planId: j['plan_id'] as String,
        task: j['task'] as String,
        equipmentId: j['equipment_id'] as String,
        equipmentTitle: (j['equipment_title'] as String?) ?? '',
        currentHmr: _dbl(j['current_hmr']),
        everyHours: _intOrNull(j['every_hours']),
        everyDays: _intOrNull(j['every_days']),
        lastDoneAt: _dt(j['last_done_at']),
        lastDoneHmr: (j['last_done_hmr'] as num?)?.toDouble(),
      );
  final String planId;
  final String task;
  final String equipmentId;
  final String equipmentTitle;
  final double currentHmr;
  final int? everyHours;
  final int? everyDays;
  final DateTime? lastDoneAt;
  final double? lastDoneHmr;
}

class CostTotal {
  const CostTotal({required this.key, required this.label, required this.jobs, required this.costPaise, required this.downtimeHours});
  factory CostTotal.fromJson(Json j) => CostTotal(
      key: j['key'] as String, label: j['label'] as String, jobs: _int(j['jobs']), costPaise: _int(j['cost_paise']), downtimeHours: _dbl(j['downtime_hours']));
  final String key;
  final String label;
  final int jobs;
  final int costPaise;
  final double downtimeHours;
}

class CostReport {
  const CostReport({required this.from, required this.to, required this.totalPaise, required this.jobs, required this.downtimeHours, required this.byEquipment, required this.byKind, required this.byMonth});
  factory CostReport.fromJson(Json j) => CostReport(
        from: j['from'] as String,
        to: j['to'] as String,
        totalPaise: _int(j['total_paise']),
        jobs: _int(j['jobs']),
        downtimeHours: _dbl(j['downtime_hours']),
        byEquipment: [for (final t in _list(j['by_equipment'])) CostTotal.fromJson(t)],
        byKind: [for (final t in _list(j['by_kind'])) CostTotal.fromJson(t)],
        byMonth: [for (final t in _list(j['by_month'])) CostTotal.fromJson(t)],
      );
  final String from;
  final String to;
  final int totalPaise;
  final int jobs;
  final double downtimeHours;
  final List<CostTotal> byEquipment;
  final List<CostTotal> byKind;
  final List<CostTotal> byMonth;
}

// ---- reviews ---------------------------------------------------------------------------------

String reviewSubjectLabel(String s) => switch (s) {
      'equipment' => 'Machine',
      'owner' => 'Owner service',
      'operator' => 'Operator',
      'customer' => 'Customer',
      _ => s,
    };

class Review {
  const Review({
    required this.id,
    required this.bookingId,
    required this.subjectType,
    required this.rating,
    required this.createdAt,
    this.comment,
    this.status,
    this.reviewerName,
    this.equipmentTitle,
    this.reply,
  });

  factory Review.fromJson(Json j) => Review(
        id: j['id'] as String,
        bookingId: j['booking_id'] as String,
        subjectType: j['subject_type'] as String,
        rating: _int(j['rating']),
        comment: j['comment'] as String?,
        status: j['status'] as String?,
        reviewerName: j['reviewer_name'] as String?,
        equipmentTitle: j['equipment_title'] as String?,
        reply: j['reply'] as String?,
        createdAt: _dtReq(j['created_at']),
      );

  final String id;
  final String bookingId;
  final String subjectType;
  final int rating;
  final String? comment;
  final String? status;
  final String? reviewerName;
  final String? equipmentTitle;
  final String? reply;
  final DateTime createdAt;
}

class ReviewSummary {
  const ReviewSummary({required this.average, required this.count, required this.histogram});
  factory ReviewSummary.fromJson(Json j) => ReviewSummary(
        average: _dbl(j['average']),
        count: _int(j['count']),
        histogram: {for (final e in _j(j['histogram'] ?? const {}).entries) int.parse(e.key): _int(e.value)},
      );
  final double average;
  final int count;
  final Map<int, int> histogram;
}

class ReviewPage {
  const ReviewPage({required this.items, this.summary, this.nextCursor});
  factory ReviewPage.fromJson(Json j) => ReviewPage(
        items: [for (final r in _list(j['data'])) Review.fromJson(r)],
        summary: j['summary'] == null ? null : ReviewSummary.fromJson(_j(j['summary'])),
        nextCursor: j['next_cursor'] as String?,
      );
  final List<Review> items;
  final ReviewSummary? summary;
  final String? nextCursor;
}

class BookingReviews {
  const BookingReviews({required this.items, required this.canReview, required this.open});
  factory BookingReviews.fromJson(Json j) => BookingReviews(
        items: [for (final r in _list(j['data'])) Review.fromJson(r)],
        canReview: [for (final s in (j['can_review'] as List? ?? const [])) s as String],
        open: j['review_open'] == true,
      );
  final List<Review> items;
  final List<String> canReview;
  final bool open;
}

class AdminReview {
  const AdminReview({
    required this.id,
    required this.subjectType,
    required this.subjectName,
    required this.rating,
    required this.status,
    required this.screeningFlags,
    required this.reportCount,
    required this.reportReasons,
    required this.reviewerName,
    required this.createdAt,
    this.comment,
    this.moderationNote,
  });

  factory AdminReview.fromJson(Json j) => AdminReview(
        id: j['id'] as String,
        subjectType: j['subject_type'] as String,
        subjectName: (j['subject_name'] as String?) ?? '',
        rating: _int(j['rating']),
        comment: j['comment'] as String?,
        status: j['status'] as String,
        screeningFlags: [for (final f in (j['screening_flags'] as List? ?? const [])) f as String],
        reportCount: _int(j['report_count']),
        reportReasons: [for (final f in (j['report_reasons'] as List? ?? const [])) f as String],
        reviewerName: (j['reviewer_name'] as String?) ?? '',
        moderationNote: j['moderation_note'] as String?,
        createdAt: _dtReq(j['created_at']),
      );

  final String id;
  final String subjectType;
  final String subjectName;
  final int rating;
  final String? comment;
  final String status;
  final List<String> screeningFlags;
  final int reportCount;
  final List<String> reportReasons;
  final String reviewerName;
  final String? moderationNote;
  final DateTime createdAt;
}
