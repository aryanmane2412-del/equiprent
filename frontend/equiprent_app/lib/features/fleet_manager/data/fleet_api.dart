import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/network/api_client.dart';
import '../../../core/session/active_role_provider.dart';
import '../../../core/session/user_role.dart';
import '../../operator/data/operator_api.dart' show JobIssue;

Map<String, dynamic> _j(Object? v) => v == null ? <String, dynamic>{} : (v as Map).cast<String, dynamic>();
List<Map<String, dynamic>> _list(Object? v) => [for (final e in (v as List? ?? const [])) _j(e)];
DateTime? _dt(Object? v) => v == null ? null : DateTime.parse(v as String).toLocal();
String? _s(Object? v) => v == null || '$v'.isEmpty ? null : '$v';
int _i(Object? v) => (v as num?)?.toInt() ?? 0;
double _d(Object? v) => (v as num?)?.toDouble() ?? 0;

// ---- models ---------------------------------------------------------------------------------------

@immutable
class FleetOverview {
  const FleetOverview({
    required this.equipmentTotal,
    required this.listed,
    required this.onJob,
    required this.idle,
    required this.utilisationPct,
    required this.activeBookings,
    required this.operators,
    required this.operatorsOnDuty,
    required this.openMaintenance,
    required this.revenue30dPaise,
    required this.scope,
  });
  factory FleetOverview.fromJson(Map<String, dynamic> j) => FleetOverview(
    equipmentTotal: _i(j['equipment_total']),
    listed: _i(j['listed']),
    onJob: _i(j['on_job']),
    idle: _i(j['idle']),
    utilisationPct: _d(j['utilisation_pct']),
    activeBookings: _i(j['active_bookings']),
    operators: _i(j['operators']),
    operatorsOnDuty: _i(j['operators_on_duty']),
    openMaintenance: _i(j['open_maintenance']),
    revenue30dPaise: _i(j['revenue_30d_paise']),
    scope: _s(j['scope']) ?? 'organisation',
  );
  final int equipmentTotal, listed, onJob, idle, activeBookings, operators, operatorsOnDuty, openMaintenance, revenue30dPaise;
  final double utilisationPct;
  final String scope;
}

/// A machine on the status board.
@immutable
class MachineStatus {
  const MachineStatus({
    required this.id,
    required this.title,
    required this.state,
    required this.listingStatus,
    required this.condition,
    required this.hourMeter,
    required this.openMaintenance,
    this.registrationNo,
    this.fleetId,
    this.fleetName,
    this.currentReference,
    this.currentOperator,
    this.nextReference,
    this.nextStart,
    this.lastLat,
    this.lastLng,
    this.lastSeen,
  });
  factory MachineStatus.fromJson(Map<String, dynamic> j) {
    final cur = j['current_booking'] == null ? null : _j(j['current_booking']);
    final nxt = j['next_booking'] == null ? null : _j(j['next_booking']);
    final pos = j['last_position'] == null ? null : _j(j['last_position']);
    return MachineStatus(
      id: '${j['equipment_id']}',
      title: '${j['title']}',
      state: '${j['state']}',
      listingStatus: '${j['listing_status']}',
      condition: '${j['condition']}',
      hourMeter: _d(j['hour_meter']),
      openMaintenance: _i(j['open_maintenance']),
      registrationNo: _s(j['registration_no']),
      fleetId: _s(j['fleet_id']),
      fleetName: _s(j['fleet_name']),
      currentReference: _s(cur?['reference']),
      currentOperator: _s(cur?['operator']),
      nextReference: _s(nxt?['reference']),
      nextStart: _dt(nxt?['start']),
      lastLat: pos == null ? null : _d(pos['lat']),
      lastLng: pos == null ? null : _d(pos['lng']),
      lastSeen: _dt(pos?['recorded_at']),
    );
  }
  final String id, title, state, listingStatus, condition;
  final double hourMeter;
  final int openMaintenance;
  final String? registrationNo, fleetId, fleetName, currentReference, currentOperator, nextReference;
  final DateTime? nextStart, lastSeen;
  final double? lastLat, lastLng;

  String get stateLabel => switch (state) {
    'on_job' => 'On a job',
    'reserved' => 'Reserved',
    'maintenance' => 'In maintenance',
    'blocked' => 'Blocked',
    'unlisted' => 'Not listed',
    _ => 'Idle',
  };
}

@immutable
class RosterOperator {
  const RosterOperator({
    required this.userId,
    required this.name,
    required this.phone,
    required this.onDuty,
    this.availability = 'available',
    required this.verified,
    required this.blocked,
    required this.active,
    this.currentBooking,
    this.licenceExpiresOn,
  });
  factory RosterOperator.fromJson(Map<String, dynamic> j) => RosterOperator(
    userId: '${j['user_id']}',
    name: '${j['full_name']}',
    phone: '${j['phone']}',
    onDuty: j['on_duty'] == true,
    availability: _s(j['availability']) ?? (j['on_duty'] == true ? 'available' : 'off_duty'),
    verified: j['verified'] == true,
    blocked: j['assignments_blocked'] == true,
    active: j['active'] != false,
    currentBooking: _s(j['current_booking_reference']),
    licenceExpiresOn: _dt(j['licence_expires_on']),
  );
  final String userId, name, phone, availability;
  final bool onDuty, verified, blocked, active;
  final String? currentBooking;
  final DateTime? licenceExpiresOn;

  bool get assignable => verified && !blocked && active;
}

@immutable
class ScheduleItem {
  const ScheduleItem({
    required this.kind,
    required this.equipmentId,
    required this.equipmentTitle,
    required this.itemId,
    required this.label,
    required this.status,
    required this.start,
    required this.end,
    required this.needsOperator,
    this.operatorName,
  });
  factory ScheduleItem.fromJson(Map<String, dynamic> j) => ScheduleItem(
    kind: '${j['kind']}',
    equipmentId: '${j['equipment_id']}',
    equipmentTitle: '${j['equipment_title']}',
    itemId: '${j['item_id']}',
    label: '${j['label']}',
    status: '${j['status']}',
    start: DateTime.parse('${j['start']}').toLocal(),
    end: DateTime.parse('${j['end']}').toLocal(),
    needsOperator: j['needs_operator'] == true,
    operatorName: _s(j['operator']),
  );
  final String kind, equipmentId, equipmentTitle, itemId, label, status;
  final DateTime start, end;
  final bool needsOperator;
  final String? operatorName;
}

@immutable
class UtilisationRow {
  const UtilisationRow({
    required this.equipmentId,
    required this.title,
    required this.bookedHours,
    required this.bookings,
    required this.utilisationPct,
    required this.earningsPaise,
  });
  factory UtilisationRow.fromJson(Map<String, dynamic> j) => UtilisationRow(
    equipmentId: '${j['equipment_id']}',
    title: '${j['title']}',
    bookedHours: _d(j['booked_hours']),
    bookings: _i(j['bookings']),
    utilisationPct: _d(j['utilisation_pct']),
    earningsPaise: _i(j['earnings_paise']),
  );
  final String equipmentId, title;
  final double bookedHours, utilisationPct;
  final int bookings, earningsPaise;
}

@immutable
class UtilisationReport {
  const UtilisationReport({required this.rows, required this.earningsPaise, required this.bookedHours});
  factory UtilisationReport.fromJson(Map<String, dynamic> j) => UtilisationReport(
    rows: [for (final r in _list(j['data'])) UtilisationRow.fromJson(r)],
    earningsPaise: _i(j['earnings_paise']),
    bookedHours: _d(j['booked_hours']),
  );
  final List<UtilisationRow> rows;
  final int earningsPaise;
  final double bookedHours;
}

@immutable
class FleetMember {
  const FleetMember({required this.kind, required this.id, required this.name, this.detail});
  final String kind, id, name;
  final String? detail;
}

@immutable
class Fleet {
  const Fleet({
    required this.id,
    required this.name,
    required this.status,
    required this.equipmentCount,
    required this.managers,
    this.description,
    this.organizationName,
    this.members = const [],
  });
  factory Fleet.fromJson(Map<String, dynamic> j) => Fleet(
    id: '${j['id']}',
    name: '${j['name']}',
    status: '${j['status']}',
    equipmentCount: _i(j['equipment_count']),
    managers: [for (final m in (j['managers'] as List? ?? const [])) '$m'],
    description: _s(j['description']),
    organizationName: _s(j['organization_name']),
    members: [for (final m in _list(j['members'])) FleetMember(kind: '${m['kind']}', id: '${m['id']}', name: '${m['name']}', detail: _s(m['detail']))],
  );
  final String id, name, status;
  final int equipmentCount;
  final List<String> managers;
  final String? description, organizationName;
  final List<FleetMember> members;

  List<FleetMember> get machines => members.where((m) => m.kind == 'equipment').toList();
  List<FleetMember> get fleetManagers => members.where((m) => m.kind == 'manager').toList();
}

@immutable
class AccountRestriction {
  const AccountRestriction({
    required this.id,
    required this.userId,
    required this.kind,
    required this.reason,
    required this.active,
    required this.createdAt,
    this.userName,
    this.expiresAt,
  });
  factory AccountRestriction.fromJson(Map<String, dynamic> j) => AccountRestriction(
    id: '${j['id']}',
    userId: '${j['user_id']}',
    kind: '${j['kind']}',
    reason: '${j['reason']}',
    active: j['active'] == true,
    createdAt: DateTime.parse('${j['created_at']}').toLocal(),
    userName: _s(j['user_name']),
    expiresAt: _dt(j['expires_at']),
  );
  final String id, userId, kind, reason;
  final bool active;
  final DateTime createdAt;
  final String? userName;
  final DateTime? expiresAt;

  String get kindLabel => switch (kind) {
    'operator_assignments' => 'No new job assignments',
    'fleet_management' => 'No fleet management',
    'new_bookings' => 'No new bookings',
    _ => kind,
  };
}

// ---- api ------------------------------------------------------------------------------------------

/// Fleet views and management (owner / fleet manager), plus the admin's
/// fleet accounts, issue review and account restrictions.
abstract class FleetApi {
  Future<FleetOverview> overview(String org);
  Future<List<MachineStatus>> board(String org, {String? fleetId});
  Future<List<RosterOperator>> operators(String org);
  Future<List<ScheduleItem>> schedule(String org, DateTime from, DateTime to);
  Future<UtilisationReport> utilisation(String org, DateTime from, DateTime to);
  Future<List<Fleet>> fleets(String org);
  Future<Fleet> fleet(String org, String id);
  Future<Fleet> createFleet(String org, String name, {String? description});
  Future<Fleet> addMachines(String org, String fleetId, List<String> equipmentIds);
  Future<Fleet> removeMachine(String org, String fleetId, String equipmentId);
  Future<Fleet> addManager(String org, String fleetId, String userId);
  Future<Fleet> removeManager(String org, String fleetId, String userId);
  Future<Fleet> archiveFleet(String org, String fleetId, String reason);
  Future<void> inviteOperator(String org, {required String phone, required String name});

  /// The organisation's fleet managers (user id → name), for owners.
  Future<List<FleetMember>> fleetManagers(String org);
  Future<void> assignOperator(String org, String bookingId, String operatorUserId);

  // admin
  Future<List<Fleet>> adminFleets({String? status});
  Future<void> adminArchiveFleet(String fleetId, String reason);
  Future<List<JobIssue>> issues({String? status});
  Future<void> reviewIssue(String id, {required String status, String note = ''});
  Future<List<AccountRestriction>> restrictions({bool activeOnly = true});
  Future<void> restrict(String userId, {required String kind, required String reason});
  Future<void> lift(String restrictionId, String reason);
}

class HttpFleetApi implements FleetApi {
  HttpFleetApi(this._c);
  final ApiClient _c;

  static List<Map<String, dynamic>> _data(Object? v) => _list(_j(v)['data']);
  static Map<String, String> _org(String org) => {'X-Org-Id': org};
  static Map<String, String> _range(DateTime from, DateTime to) => {'from': from.toUtc().toIso8601String(), 'to': to.toUtc().toIso8601String()};

  @override
  Future<FleetOverview> overview(String org) => _c.get('/fleet/$org/overview', decode: (j) => FleetOverview.fromJson(_j(j)));
  @override
  Future<List<MachineStatus>> board(String org, {String? fleetId}) =>
      _c.get('/fleet/$org/equipment', query: {'fleet_id': ?fleetId}, decode: (j) => [for (final m in _data(j)) MachineStatus.fromJson(m)]);
  @override
  Future<List<RosterOperator>> operators(String org) => _c.get('/fleet/$org/operators', decode: (j) => [for (final o in _data(j)) RosterOperator.fromJson(o)]);
  @override
  Future<List<ScheduleItem>> schedule(String org, DateTime from, DateTime to) =>
      _c.get('/fleet/$org/schedule', query: _range(from, to), decode: (j) => [for (final s in _data(j)) ScheduleItem.fromJson(s)]);
  @override
  Future<UtilisationReport> utilisation(String org, DateTime from, DateTime to) =>
      _c.get('/fleet/$org/reports/utilisation', query: _range(from, to), decode: (j) => UtilisationReport.fromJson(_j(j)));
  @override
  Future<List<Fleet>> fleets(String org) => _c.get('/fleet/$org/fleets', decode: (j) => [for (final f in _data(j)) Fleet.fromJson(f)]);
  @override
  Future<Fleet> fleet(String org, String id) => _c.get('/fleet/$org/fleets/$id', decode: (j) => Fleet.fromJson(_j(j)));
  @override
  Future<Fleet> createFleet(String org, String name, {String? description}) =>
      _c.post('/owner/fleets', headers: _org(org), body: {'name': name, 'description': ?description}, decode: (j) => Fleet.fromJson(_j(j)));
  @override
  Future<Fleet> addMachines(String org, String fleetId, List<String> equipmentIds) =>
      _c.post('/owner/fleets/$fleetId/equipment', headers: _org(org), body: {'equipment_ids': equipmentIds}, decode: (j) => Fleet.fromJson(_j(j)));
  @override
  Future<Fleet> removeMachine(String org, String fleetId, String equipmentId) async {
    await _c.delete('/owner/fleets/$fleetId/equipment/$equipmentId', headers: _org(org));
    return fleet(org, fleetId);
  }

  @override
  Future<Fleet> addManager(String org, String fleetId, String userId) =>
      _c.post('/owner/fleets/$fleetId/managers', headers: _org(org), body: {'user_id': userId}, decode: (j) => Fleet.fromJson(_j(j)));
  @override
  Future<Fleet> removeManager(String org, String fleetId, String userId) async {
    await _c.delete('/owner/fleets/$fleetId/managers/$userId', headers: _org(org));
    return fleet(org, fleetId);
  }

  @override
  Future<Fleet> archiveFleet(String org, String fleetId, String reason) =>
      _c.post('/owner/fleets/$fleetId/archive', headers: _org(org), body: {'reason': reason}, decode: (j) => Fleet.fromJson(_j(j)));
  @override
  Future<void> inviteOperator(String org, {required String phone, required String name}) =>
      _c.post('/orgs/$org/members', body: {'phone': phone, 'role': 'operator', 'full_name': name}, decode: (_) {});
  @override
  Future<List<FleetMember>> fleetManagers(String org) => _c.get(
    '/orgs/$org/members',
    decode: (j) => [
      for (final m in _data(j))
        if (m['role'] == 'fleet_manager') FleetMember(kind: 'manager', id: '${m['user_id']}', name: '${m['full_name']}', detail: _s(m['phone'])),
    ],
  );
  @override
  Future<void> assignOperator(String org, String bookingId, String operatorUserId) =>
      _c.post('/owner/bookings/$bookingId/assign-operator', headers: _org(org), body: {'operator_user_id': operatorUserId}, decode: (_) {});

  @override
  Future<List<Fleet>> adminFleets({String? status}) =>
      _c.get('/admin/fleets', query: {'status': ?status}, decode: (j) => [for (final f in _data(j)) Fleet.fromJson(f)]);
  @override
  Future<void> adminArchiveFleet(String fleetId, String reason) => _c.post('/admin/fleets/$fleetId/archive', body: {'reason': reason}, decode: (_) {});
  @override
  Future<List<JobIssue>> issues({String? status}) =>
      _c.get('/admin/issues', query: {'status': ?status}, decode: (j) => [for (final i in _data(j)) JobIssue.fromJson(i)]);
  @override
  Future<void> reviewIssue(String id, {required String status, String note = ''}) =>
      _c.post('/admin/issues/$id/review', body: {'status': status, 'note': note}, decode: (_) {});
  @override
  Future<List<AccountRestriction>> restrictions({bool activeOnly = true}) =>
      _c.get('/admin/restrictions', query: {'active': activeOnly}, decode: (j) => [for (final r in _data(j)) AccountRestriction.fromJson(r)]);
  @override
  Future<void> restrict(String userId, {required String kind, required String reason}) =>
      _c.post('/admin/users/$userId/restrictions', body: {'kind': kind, 'reason': reason}, decode: (_) {});
  @override
  Future<void> lift(String restrictionId, String reason) => _c.post('/admin/restrictions/$restrictionId/lift', body: {'reason': reason}, decode: (_) {});
}

final fleetApiProvider = Provider<FleetApi>((ref) => HttpFleetApi(ref.watch(apiClientProvider)));

/// The organisation the fleet screens work in: the owner's org for owners,
/// the employer for fleet managers.
final fleetOrgProvider = Provider<String?>((ref) {
  final orgs = ref.watch(authControllerProvider).user?.orgs ?? const {};
  final role = ref.watch(activeRoleProvider);
  return role == UserRole.owner ? orgs[UserRole.owner] ?? orgs[UserRole.fleetManager] : orgs[UserRole.fleetManager] ?? orgs[UserRole.owner];
});

Future<T> _withOrg<T>(Ref ref, Future<T> Function(FleetApi api, String org) fn) {
  final org = ref.watch(fleetOrgProvider);
  if (org == null) return Future.error(StateError('You are not a member of an equipment owner organisation.'));
  return fn(ref.watch(fleetApiProvider), org);
}

final fleetOverviewProvider = FutureProvider.autoDispose<FleetOverview>((ref) => _withOrg(ref, (api, org) => api.overview(org)));
final fleetBoardProvider = FutureProvider.autoDispose.family<List<MachineStatus>, String?>(
  (ref, fleetId) => _withOrg(ref, (api, org) => api.board(org, fleetId: fleetId)),
);
final fleetRosterProvider = FutureProvider.autoDispose<List<RosterOperator>>((ref) => _withOrg(ref, (api, org) => api.operators(org)));
final fleetListProvider = FutureProvider.autoDispose<List<Fleet>>((ref) => _withOrg(ref, (api, org) => api.fleets(org)));
final fleetDetailProvider = FutureProvider.autoDispose.family<Fleet, String>((ref, id) => _withOrg(ref, (api, org) => api.fleet(org, id)));
final orgFleetManagersProvider = FutureProvider.autoDispose<List<FleetMember>>((ref) => _withOrg(ref, (api, org) => api.fleetManagers(org)));
final fleetScheduleProvider = FutureProvider.autoDispose<List<ScheduleItem>>((ref) {
  final now = DateTime.now();
  final from = DateTime(now.year, now.month, now.day);
  return _withOrg(ref, (api, org) => api.schedule(org, from, from.add(const Duration(days: 30))));
});
final fleetUtilisationProvider = FutureProvider.autoDispose.family<UtilisationReport, int>((ref, days) {
  final to = DateTime.now();
  return _withOrg(ref, (api, org) => api.utilisation(org, to.subtract(Duration(days: days)), to));
});

final adminFleetsProvider = FutureProvider.autoDispose<List<Fleet>>((ref) => ref.watch(fleetApiProvider).adminFleets());
final adminIssuesProvider = FutureProvider.autoDispose.family<List<JobIssue>, String?>((ref, status) => ref.watch(fleetApiProvider).issues(status: status));
final adminRestrictionsProvider = FutureProvider.autoDispose<List<AccountRestriction>>((ref) => ref.watch(fleetApiProvider).restrictions());
