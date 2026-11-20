import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/fleet_manager/data/fleet_api.dart';
import 'package:equiprent_app/features/operator/data/operator_api.dart';

/// The operator's server state for one job ('bk-op'): the booking moves
/// through the lifecycle and refuses steps the server would refuse.
class FakeOperatorApi implements OperatorApi {
  final calls = <String>[];
  String bookingStatus = 'operator_assigned';
  String assignmentStatus = 'assigned';
  final checklists = <JobChecklist>[];
  final issues = <JobIssue>[];

  bool get _current =>
      (assignmentStatus == 'assigned' || assignmentStatus == 'accepted') &&
      bookingStatus != 'returned';

  List<String> get _next {
    if (!_current) return const [];
    if (assignmentStatus == 'assigned') return const ['accept', 'decline'];
    bool has(String k) => checklists.any((c) => c.kind == k);
    return switch (bookingStatus) {
      'operator_assigned' => const ['dispatch'],
      'dispatched' => const ['arrived'],
      'on_site' =>
        has('handover') ? const ['start'] : const ['handover_checklist'],
      'active' || 'return_initiated' =>
        has('return')
            ? (bookingStatus == 'return_initiated'
                  ? const ['returned']
                  : const [])
            : const ['return_checklist'],
      _ => const [],
    };
  }

  JobSummary get _summary => JobSummary(
    bookingId: 'bk-op',
    reference: 'ER-2611-OP1',
    assignmentStatus: assignmentStatus,
    bookingStatus: bookingStatus,
    start: DateTime(2026, 11, 20, 8),
    end: DateTime(2026, 11, 23, 18),
    equipmentTitle: 'JCB 3DX backhoe loader',
    site: 'Plot 12, Hinjewadi, Pune',
    customerName: 'Shree Builders',
    registrationNo: 'MH12KX4471',
  );

  @override
  Future<List<JobSummary>> jobs({required String scope}) async =>
      scope == 'history'
      ? (_current ? [] : [_summary])
      : (_current ? [_summary] : []);

  @override
  Future<DriverPaymentHistory> payments() async =>
      const DriverPaymentHistory(data: [], events: []);

  @override
  Future<List<JobSummary>> schedule(DateTime from, DateTime to) async => [
    _summary,
  ];

  @override
  Future<JobDetail> job(String bookingId) async {
    if (bookingId != 'bk-op') {
      throw const ApiException(
        kind: ApiErrorKind.notFound,
        statusCode: 404,
        code: 'NOT_FOUND',
        message: 'Not found',
      );
    }
    final accepted = assignmentStatus == 'accepted' && _current;
    return JobDetail(
      bookingId: 'bk-op',
      reference: 'ER-2611-OP1',
      bookingStatus: bookingStatus,
      assignmentStatus: assignmentStatus,
      current: _current,
      start: DateTime(2026, 11, 20, 8),
      end: DateTime(2026, 11, 23, 18),
      workDescription: 'Trenching for the boundary wall',
      customer: JobContact(
        name: 'Shree Builders',
        contact: 'Asha Rao',
        phone: accepted ? '+919800000001' : null,
      ),
      owner: JobContact(
        name: 'Patil Earthmovers',
        phone: accepted ? '+919800000002' : null,
      ),
      equipmentTitle: 'JCB 3DX backhoe loader',
      equipmentLine: 'JCB · 3DX · Backhoe Loader',
      registrationNo: 'MH12KX4471',
      hourMeter: 2150,
      siteAddress: 'Plot 12, Phase 2, Hinjewadi, Pune 411057',
      siteLabel: 'Hinjewadi site',
      siteLat: 18.5913,
      siteLng: 73.7389,
      navigationUrl:
          'https://www.google.com/maps/dir/?api=1&destination=18.591300,73.738900',
      checklists: List.of(checklists),
      issues: List.of(issues),
      nextSteps: _next,
    );
  }

  @override
  Future<void> step(String bookingId, String step) async {
    calls.add('$step $bookingId');
    void need(bool ok) {
      if (!ok) {
        throw ApiException(
          kind: ApiErrorKind.badRequest,
          statusCode: 422,
          code: 'BUSINESS_RULE_VIOLATION',
          message: 'cannot $step now',
        );
      }
    }

    switch (step) {
      case 'accept':
        need(assignmentStatus == 'assigned');
        assignmentStatus = 'accepted';
      case 'decline':
        need(assignmentStatus == 'assigned');
        assignmentStatus = 'declined';
      case 'dispatch':
        need(bookingStatus == 'operator_assigned');
        bookingStatus = 'dispatched';
      case 'arrived':
        need(bookingStatus == 'dispatched');
        bookingStatus = 'on_site';
      case 'start':
        need(
          bookingStatus == 'on_site' &&
              checklists.any((c) => c.kind == 'handover'),
        );
        bookingStatus = 'active';
      case 'returned':
        need(
          bookingStatus == 'return_initiated' &&
              checklists.any((c) => c.kind == 'return'),
        );
        bookingStatus = 'returned';
        assignmentStatus = 'completed';
    }
  }

  @override
  Future<JobChecklist> submitChecklist(
    String bookingId,
    ChecklistInput input,
  ) async {
    calls.add(
      'checklist ${input.kind} ${input.photoIds.length}',
    );
    final c = JobChecklist(
      id: 'cl-${checklists.length + 1}',
      kind: input.kind,
      hourMeter: 0,
      items: input.items,
      photos: [
        for (final p in input.photoIds)
          JobPhoto(url: 'https://files.test/$p.jpg', id: p),
      ],
      capturedAt: DateTime.now(),
      submittedBy: 'Test User',
      fuelPct: input.fuelPct,
      damageNotes: input.damageNotes,
    );
    checklists.add(c);
    return c;
  }

  @override
  Future<JobIssue> reportIssue(
    String bookingId, {
    required String kind,
    required String stage,
    required String severity,
    required String description,
    List<String> photoIds = const [],
  }) async {
    calls.add('issue $kind $stage $severity $description');
    final i = JobIssue(
      id: 'is-${issues.length + 1}',
      kind: kind,
      stage: stage,
      severity: severity,
      description: description,
      status: 'open',
      reporterRole: 'operator',
      createdAt: DateTime.now(),
    );
    issues.add(i);
    return i;
  }

  @override
  Future<bool> setOnDuty(bool onDuty) async => onDuty;
}

/// Fleet views over three machines, two operators and one fleet.
class FakeFleetApi implements FleetApi {
  final calls = <String>[];
  final fleetList = <Fleet>[
    const Fleet(
      id: 'fl-1',
      name: 'North yard',
      status: 'active',
      equipmentCount: 2,
      managers: ['Fleet Manager'],
      members: [
        FleetMember(
          kind: 'equipment',
          id: 'eq-1',
          name: 'JCB 3DX backhoe loader',
          detail: 'MH12KX4471',
        ),
        FleetMember(
          kind: 'equipment',
          id: 'eq-2',
          name: 'Tata Hitachi EX200',
          detail: 'MH12AB1234',
        ),
        FleetMember(
          kind: 'manager',
          id: 'u-fm',
          name: 'Fleet Manager',
          detail: '+919800000010',
        ),
      ],
    ),
  ];
  final restrictionList = <AccountRestriction>[];
  final issueList = <JobIssue>[
    JobIssue(
      id: 'is-9',
      kind: 'breakdown',
      stage: 'during_rental',
      severity: 'major',
      description: 'Hydraulic hose burst',
      status: 'open',
      reporterRole: 'customer',
      createdAt: DateTime(2026, 11, 14, 10),
      bookingReference: 'ER-2611-BK1',
      equipmentTitle: 'JCB 3DX backhoe loader',
      ownerName: 'Patil Earthmovers',
      customerName: 'Shree Builders',
    ),
  ];
  bool assigned = false;

  @override
  Future<FleetOverview> overview(String org) async => const FleetOverview(
    equipmentTotal: 3,
    listed: 3,
    onJob: 1,
    idle: 2,
    utilisationPct: 33,
    activeBookings: 2,
    operators: 2,
    operatorsOnDuty: 1,
    openMaintenance: 1,
    revenue30dPaise: 12500000,
    scope: 'assigned',
  );

  @override
  Future<List<MachineStatus>> board(String org, {String? fleetId}) async => [
    MachineStatus(
      id: 'eq-1',
      title: 'JCB 3DX backhoe loader',
      state: 'on_job',
      listingStatus: 'active',
      condition: 'good',
      hourMeter: 2150,
      openMaintenance: 0,
      registrationNo: 'MH12KX4471',
      fleetId: 'fl-1',
      fleetName: 'North yard',
      currentReference: 'ER-2611-BK1',
      currentOperator: 'Suresh Kale',
      lastLat: 18.59,
      lastLng: 73.73,
      lastSeen: DateTime(2026, 11, 14, 9),
    ),
    if (fleetId == null)
      const MachineStatus(
        id: 'eq-3',
        title: 'Liugong 835H loader',
        state: 'maintenance',
        listingStatus: 'active',
        condition: 'fair',
        hourMeter: 5400,
        openMaintenance: 1,
      ),
  ];

  @override
  Future<List<RosterOperator>> operators(String org) async => [
    RosterOperator(
      userId: 'op-1',
      name: 'Suresh Kale',
      phone: '+919800000020',
      onDuty: true,
      verified: true,
      blocked: false,
      active: true,
      licenceExpiresOn: DateTime(2026, 12, 1),
    ),
    const RosterOperator(
      userId: 'op-2',
      name: 'Imran Shaikh',
      phone: '+919800000021',
      onDuty: false,
      verified: true,
      blocked: true,
      active: true,
    ),
  ];

  @override
  Future<List<ScheduleItem>> schedule(
    String org,
    DateTime from,
    DateTime to,
  ) async => [
    ScheduleItem(
      kind: 'booking',
      equipmentId: 'eq-2',
      equipmentTitle: 'Tata Hitachi EX200',
      itemId: 'bk-9',
      label: 'ER-2611-BK9',
      status: assigned ? 'operator_assigned' : 'confirmed',
      start: DateTime(2026, 11, 25, 8),
      end: DateTime(2026, 11, 27, 18),
      needsOperator: !assigned,
      operatorName: assigned ? 'Suresh Kale' : null,
    ),
    ScheduleItem(
      kind: 'block',
      equipmentId: 'eq-3',
      equipmentTitle: 'Liugong 835H loader',
      itemId: 'blk-1',
      label: 'Engine service',
      status: 'maintenance',
      start: DateTime(2026, 11, 18),
      end: DateTime(2026, 11, 19),
      needsOperator: false,
    ),
  ];

  @override
  Future<UtilisationReport> utilisation(
    String org,
    DateTime from,
    DateTime to,
  ) async => const UtilisationReport(
    earningsPaise: 9800000,
    bookedHours: 140,
    rows: [
      UtilisationRow(
        equipmentId: 'eq-1',
        title: 'JCB 3DX backhoe loader',
        bookedHours: 96,
        bookings: 2,
        utilisationPct: 13.3,
        earningsPaise: 7000000,
      ),
      UtilisationRow(
        equipmentId: 'eq-2',
        title: 'Tata Hitachi EX200',
        bookedHours: 44,
        bookings: 1,
        utilisationPct: 6.1,
        earningsPaise: 2800000,
      ),
    ],
  );

  @override
  Future<List<Fleet>> fleets(String org) async => List.of(fleetList);
  @override
  Future<Fleet> fleet(String org, String id) async =>
      fleetList.firstWhere((f) => f.id == id);
  @override
  Future<Fleet> createFleet(
    String org,
    String name, {
    String? description,
  }) async {
    calls.add('create-fleet $name');
    final f = Fleet(
      id: 'fl-${fleetList.length + 1}',
      name: name,
      status: 'active',
      equipmentCount: 0,
      managers: const [],
    );
    fleetList.add(f);
    return f;
  }

  @override
  Future<Fleet> addMachines(
    String org,
    String fleetId,
    List<String> equipmentIds,
  ) async {
    calls.add('add-machines $fleetId ${equipmentIds.join(',')}');
    return fleet(org, fleetId);
  }

  @override
  Future<Fleet> removeMachine(
    String org,
    String fleetId,
    String equipmentId,
  ) async {
    calls.add('remove-machine $fleetId $equipmentId');
    return fleet(org, fleetId);
  }

  @override
  Future<Fleet> addManager(String org, String fleetId, String userId) async {
    calls.add('add-manager $fleetId $userId');
    return fleet(org, fleetId);
  }

  @override
  Future<Fleet> removeManager(String org, String fleetId, String userId) async {
    calls.add('remove-manager $fleetId $userId');
    return fleet(org, fleetId);
  }

  @override
  Future<Fleet> archiveFleet(String org, String fleetId, String reason) async {
    calls.add('archive $fleetId $reason');
    return fleet(org, fleetId);
  }

  @override
  Future<void> inviteOperator(
    String org, {
    required String phone,
    required String name,
  }) async => calls.add('invite $name $phone');

  @override
  Future<List<FleetMember>> fleetManagers(String org) async => const [
    FleetMember(kind: 'manager', id: 'u-fm', name: 'Fleet Manager'),
    FleetMember(
      kind: 'manager',
      id: 'u-fm2',
      name: 'Second Manager',
      detail: '+919800000011',
    ),
  ];

  @override
  Future<void> assignOperator(
    String org,
    String bookingId,
    String operatorUserId,
  ) async {
    calls.add('assign $bookingId $operatorUserId');
    if (operatorUserId == 'op-busy') {
      throw const ApiException(
        kind: ApiErrorKind.conflict,
        statusCode: 409,
        code: 'CONFLICT',
        message: 'this operator already has a job that overlaps these dates',
      );
    }
    assigned = true;
  }

  @override
  Future<List<Fleet>> adminFleets({String? status}) async => [
    for (final f in fleetList)
      Fleet(
        id: f.id,
        name: f.name,
        status: f.status,
        equipmentCount: f.equipmentCount,
        managers: f.managers,
        organizationName: 'Patil Earthmovers',
      ),
  ];
  @override
  Future<void> adminArchiveFleet(String fleetId, String reason) async =>
      calls.add('admin-archive $fleetId $reason');
  @override
  Future<List<JobIssue>> issues({String? status}) async => List.of(issueList);
  @override
  Future<void> reviewIssue(
    String id, {
    required String status,
    String note = '',
  }) async {
    calls.add('review $id $status $note'.trim());
    if (status != 'under_review') issueList.removeWhere((i) => i.id == id);
  }

  @override
  Future<List<AccountRestriction>> restrictions({
    bool activeOnly = true,
  }) async => List.of(restrictionList);
  @override
  Future<void> restrict(
    String userId, {
    required String kind,
    required String reason,
  }) async {
    calls.add('restrict $userId $kind $reason');
    restrictionList.add(
      AccountRestriction(
        id: 'rs-${restrictionList.length + 1}',
        userId: userId,
        kind: kind,
        reason: reason,
        active: true,
        createdAt: DateTime.now(),
      ),
    );
  }

  @override
  Future<void> lift(String restrictionId, String reason) async {
    calls.add('lift $restrictionId $reason');
    restrictionList.removeWhere((r) => r.id == restrictionId);
  }
}
