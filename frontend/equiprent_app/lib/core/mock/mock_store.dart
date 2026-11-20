import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'mock_data.dart';
import 'models.dart';

/// In-memory state for the design-stage prototype. Every action a screen
/// performs mutates one of these notifiers, so the UI behaves realistically
/// without a backend. Phase 1 replaces each with a repository over the API.

final catalogProvider = Provider<List<Equipment>>((ref) => MockData.equipment);

class FavoritesNotifier extends Notifier<Set<String>> {
  @override
  Set<String> build() => {'eq-009'};

  void toggle(String id) => state = state.contains(id) ? ({...state}..remove(id)) : {...state, id};
}

final favoritesProvider = NotifierProvider<FavoritesNotifier, Set<String>>(FavoritesNotifier.new);

/// Equipment selected for side-by-side comparison (max 3).
class CompareNotifier extends Notifier<List<String>> {
  static const max = 3;

  @override
  List<String> build() => const [];

  /// Returns false when the list is full.
  bool toggle(String id) {
    if (state.contains(id)) {
      state = [...state]..remove(id);
      return true;
    }
    if (state.length >= max) return false;
    state = [...state, id];
    return true;
  }

  void clear() => state = const [];
}

final compareProvider = NotifierProvider<CompareNotifier, List<String>>(CompareNotifier.new);

class CustomerBookingsNotifier extends Notifier<List<Booking>> {
  int _seq = 0;

  @override
  List<Booking> build() => MockData.customerBookings;

  Booking create({
    required Equipment equipment,
    required DateTime start,
    required DateTime end,
    required String siteName,
    required String siteAddress,
    required String shiftLabel,
    required int total,
    required int paid,
  }) {
    _seq++;
    final booking = Booking(
      id: 'bk-new-$_seq',
      reference: 'ER-2611-N${_seq.toString().padLeft(3, '0')}',
      equipmentId: equipment.id,
      equipmentName: equipment.name,
      categoryId: equipment.categoryId,
      customerName: MockData.customerName,
      customerCompany: MockData.customerCompany,
      ownerName: equipment.ownerName,
      siteName: siteName,
      siteAddress: siteAddress,
      start: start,
      end: end,
      shiftLabel: shiftLabel,
      total: total,
      paid: paid,
      status: equipment.instantBook ? BookingStatus.confirmed : BookingStatus.requested,
      createdAt: MockData.now,
    );
    state = [booking, ...state];
    return booking;
  }

  void payBalance(String id) =>
      state = [for (final b in state) b.id == id ? b.copyWith(paid: b.total) : b];

  void cancel(String id) =>
      state = [for (final b in state) b.id == id ? b.copyWith(status: BookingStatus.cancelled) : b];
}

final customerBookingsProvider = NotifierProvider<CustomerBookingsNotifier, List<Booking>>(CustomerBookingsNotifier.new);

class ChatNotifier extends Notifier<List<ChatThread>> {
  @override
  List<ChatThread> build() => MockData.chatThreads;

  void send(String threadId, String text) {
    final trimmed = text.trim();
    if (trimmed.isEmpty) return;
    state = [
      for (final t in state)
        t.id == threadId
            ? t.copyWith(messages: [...t.messages, ChatMessage(text: trimmed, fromMe: true, time: MockData.now)])
            : t,
    ];
  }

  void markRead(String threadId) =>
      state = [for (final t in state) t.id == threadId ? t.copyWith(unread: 0) : t];
}

final chatProvider = NotifierProvider<ChatNotifier, List<ChatThread>>(ChatNotifier.new);

class OwnerListingsNotifier extends Notifier<List<Equipment>> {
  @override
  List<Equipment> build() => MockData.ownerEquipment;

  void add(Equipment e) => state = [e, ...state];

  void setStatus(String id, EquipmentStatus status) =>
      state = [for (final e in state) e.id == id ? e.copyWith(status: status) : e];
}

final ownerListingsProvider = NotifierProvider<OwnerListingsNotifier, List<Equipment>>(OwnerListingsNotifier.new);

/// Bookings for the owner organisation; shared by Owner and Fleet Manager.
class OwnerBookingsNotifier extends Notifier<List<Booking>> {
  @override
  List<Booking> build() => MockData.ownerBookings;

  void _set(String id, Booking Function(Booking) f) => state = [for (final b in state) b.id == id ? f(b) : b];

  void accept(String id) => _set(id, (b) => b.copyWith(status: BookingStatus.confirmed));
  void reject(String id) => _set(id, (b) => b.copyWith(status: BookingStatus.cancelled));
  void assignOperator(String id, String operatorName) =>
      _set(id, (b) => b.copyWith(status: BookingStatus.operatorAssigned, operatorName: operatorName));
}

final ownerBookingsProvider = NotifierProvider<OwnerBookingsNotifier, List<Booking>>(OwnerBookingsNotifier.new);

class MaintenanceNotifier extends Notifier<List<MaintenanceTask>> {
  @override
  List<MaintenanceTask> build() => MockData.maintenance;

  void markDone(String id) =>
      state = [for (final m in state) m.id == id ? m.copyWith(status: MaintenanceStatus.done) : m];
}

final maintenanceProvider = NotifierProvider<MaintenanceNotifier, List<MaintenanceTask>>(MaintenanceNotifier.new);

class OperatorJobsNotifier extends Notifier<List<OperatorJob>> {
  @override
  List<OperatorJob> build() => MockData.operatorJobs;

  void _set(String id, OperatorJob Function(OperatorJob) f) => state = [for (final j in state) j.id == id ? f(j) : j];

  void accept(String id) => _set(id, (j) => j.copyWith(status: JobStatus.accepted));
  void startTrip(String id) => _set(id, (j) => j.copyWith(status: JobStatus.enRoute));
  void arrive(String id) => _set(id, (j) => j.copyWith(status: JobStatus.onSite));
  void completeHandover(String id, int startHmr) => _set(id, (j) => j.copyWith(status: JobStatus.inProgress, startHmr: startHmr));
  void complete(String id, double hours) => _set(id, (j) => j.copyWith(status: JobStatus.completed, hoursLogged: hours));
  void decline(String id) => state = state.where((j) => j.id != id).toList();
}

final operatorJobsProvider = NotifierProvider<OperatorJobsNotifier, List<OperatorJob>>(OperatorJobsNotifier.new);

/// Whether the operator is on duty (shares availability with the fleet).
class DutyNotifier extends Notifier<bool> {
  @override
  bool build() => true;

  void toggle() => state = !state;
}

final operatorOnDutyProvider = NotifierProvider<DutyNotifier, bool>(DutyNotifier.new);

class UsersNotifier extends Notifier<List<PlatformUser>> {
  @override
  List<PlatformUser> build() => MockData.users;

  void setStatus(String id, AccountStatus status) =>
      state = [for (final u in state) u.id == id ? u.copyWith(status: status) : u];
}

final usersProvider = NotifierProvider<UsersNotifier, List<PlatformUser>>(UsersNotifier.new);

class ApprovalsNotifier extends Notifier<List<ApprovalRequest>> {
  @override
  List<ApprovalRequest> build() => MockData.approvals;

  void decide(String id, ApprovalStatus status) =>
      state = [for (final a in state) a.id == id ? a.copyWith(status: status) : a];
}

final approvalsProvider = NotifierProvider<ApprovalsNotifier, List<ApprovalRequest>>(ApprovalsNotifier.new);

class DisputesNotifier extends Notifier<List<Dispute>> {
  @override
  List<Dispute> build() => MockData.disputes;

  void startReview(String id) =>
      state = [for (final d in state) d.id == id ? d.copyWith(status: DisputeStatus.underReview) : d];

  void resolve(String id, String resolution) =>
      state = [for (final d in state) d.id == id ? d.copyWith(status: DisputeStatus.resolved, resolution: resolution) : d];
}

final disputesProvider = NotifierProvider<DisputesNotifier, List<Dispute>>(DisputesNotifier.new);

class SettlementsNotifier extends Notifier<List<Settlement>> {
  @override
  List<Settlement> build() => MockData.settlements;

  void setStatus(String id, SettlementStatus status) =>
      state = [for (final s in state) s.id == id ? s.copyWith(status: status) : s];

  void releaseAllPending() => state = [
        for (final s in state) s.status == SettlementStatus.pending ? s.copyWith(status: SettlementStatus.released) : s,
      ];
}

final settlementsProvider = NotifierProvider<SettlementsNotifier, List<Settlement>>(SettlementsNotifier.new);

class NotificationsNotifier extends Notifier<List<AppNotification>> {
  @override
  List<AppNotification> build() => MockData.notifications;

  void markAllRead() => state = [
        for (final n in state) AppNotification(title: n.title, body: n.body, time: n.time, icon: n.icon, read: true),
      ];
}

final notificationsProvider = NotifierProvider<NotificationsNotifier, List<AppNotification>>(NotificationsNotifier.new);
