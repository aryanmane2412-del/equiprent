import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/network/api_client.dart';
import '../../../core/session/user_role.dart';

typedef Json = Map<String, dynamic>;

class OwnerDashboard {
  const OwnerDashboard({this.equipmentTotal = 0, this.equipmentActive = 0, this.equipmentPendingReview = 0,
    this.bookingsAwaitingResponse = 0, this.bookingsInProgress = 0, this.bookingsCompleted = 0, this.capturedLast30dPaise = 0});
  factory OwnerDashboard.fromJson(Json j) => OwnerDashboard(
    equipmentTotal: _i(j['equipment_total']), equipmentActive: _i(j['equipment_active']),
    equipmentPendingReview: _i(j['equipment_pending_review']), bookingsAwaitingResponse: _i(j['bookings_awaiting_response']),
    bookingsInProgress: _i(j['bookings_in_progress']), bookingsCompleted: _i(j['bookings_completed']),
    capturedLast30dPaise: _i(j['captured_last_30d_paise']));
  final int equipmentTotal, equipmentActive, equipmentPendingReview, bookingsAwaitingResponse, bookingsInProgress, bookingsCompleted, capturedLast30dPaise;
}

class OwnerEarnings {
  const OwnerEarnings({this.paidOutPaise = 0, this.pendingPaise = 0, this.onHoldPaise = 0, this.grossPaise = 0,
    this.commissionPaise = 0, this.withheldPaise = 0, this.pendingCount = 0, this.monthly = const []});
  factory OwnerEarnings.fromJson(Json j) => OwnerEarnings(
    paidOutPaise: _i(j['paid_out_paise']), pendingPaise: _i(j['pending_paise']), onHoldPaise: _i(j['on_hold_paise']),
    grossPaise: _i(j['gross_paise']), commissionPaise: _i(j['commission_paise']), withheldPaise: _i(j['withheld_paise']),
    pendingCount: _i(j['pending_count']), monthly: [for (final x in (j['monthly'] as List? ?? const [])) OwnerEarningMonth.fromJson((x as Map).cast<String, dynamic>())]);
  final int paidOutPaise, pendingPaise, onHoldPaise, grossPaise, commissionPaise, withheldPaise, pendingCount;
  final List<OwnerEarningMonth> monthly;
}

class OwnerEarningMonth {
  const OwnerEarningMonth(this.month, this.netPaise, this.settlements);
  factory OwnerEarningMonth.fromJson(Json j) => OwnerEarningMonth('${j['month']}', _i(j['net_paise']), _i(j['settlements']));
  final String month;
  final int netPaise, settlements;
}

int _i(Object? v) => (v as num?)?.toInt() ?? 0;
Json _j(Object? v) => (v as Map).cast<String, dynamic>();

class OwnerApi {
  const OwnerApi(this.client);
  final ApiClient client;
  Map<String, String> _headers(String org) => {'X-Org-Id': org};
  Future<OwnerDashboard> dashboard(String org) => client.get('/owners/me/dashboard', headers: _headers(org), decode: (j) => OwnerDashboard.fromJson(_j(j)));
  Future<OwnerEarnings> earnings(String org) => client.get('/owners/me/earnings', headers: _headers(org), decode: (j) => OwnerEarnings.fromJson(_j(j)));
}

final ownerApiProvider = Provider<OwnerApi>((ref) => OwnerApi(ref.watch(apiClientProvider)));
final ownerDashboardProvider = FutureProvider.autoDispose<OwnerDashboard>((ref) {
  final org = ref.watch(authControllerProvider).user?.orgs[UserRole.owner];
  if (org == null) throw StateError('Owner organisation is unavailable.');
  return ref.watch(ownerApiProvider).dashboard(org);
});
final ownerEarningsProvider = FutureProvider.autoDispose<OwnerEarnings>((ref) {
  final org = ref.watch(authControllerProvider).user?.orgs[UserRole.owner];
  if (org == null) throw StateError('Owner organisation is unavailable.');
  return ref.watch(ownerApiProvider).earnings(org);
});
