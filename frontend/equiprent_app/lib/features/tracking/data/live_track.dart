import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../comms/data/comms_api.dart' show BookingSide, sideOrgProvider;
import 'tracking_api.dart';

/// How often the screen re-fetches when the live stream is unavailable.
/// Null switches polling off (tests).
final trackPollProvider = Provider<Duration?>((ref) => const Duration(seconds: 20));

enum LiveMode { connecting, live, polling }

@immutable
class LiveTrack {
  const LiveTrack(this.view, this.mode, {this.lastUpdate});
  final TrackView view;
  final LiveMode mode;
  final DateTime? lastUpdate;
}

typedef TrackKey = (BookingSide, String);

/// A booking's tracking view kept current: loads it, then follows the
/// WebSocket stream; falls back to polling if the stream cannot be opened or
/// drops. Positions are never shown when the server hides them.
class LiveTrackController extends AsyncNotifier<LiveTrack> {
  LiveTrackController(this.key);
  final TrackKey key;

  StreamSubscription<LiveUpdate>? _sub;
  Timer? _poll;

  String? get _org => ref.read(sideOrgProvider(key.$1));
  TrackingApi get _api => ref.read(trackingApiProvider);

  @override
  Future<LiveTrack> build() async {
    ref.onDispose(_close);
    final view = await ref.watch(trackingApiProvider).view(ref.watch(sideOrgProvider(key.$1)), key.$2);
    if (view.hidden) return LiveTrack(view, LiveMode.polling);
    unawaited(_connect());
    return LiveTrack(view, LiveMode.connecting, lastUpdate: view.equipment?.recordedAt);
  }

  Future<void> _connect() async {
    try {
      final ticket = await _api.bookingTicket(_org, key.$2);
      _sub = ref.read(liveChannelProvider).connect(ticket).listen(_onUpdate, onError: (_) => _fallback(), onDone: _fallback, cancelOnError: true);
      _set((t) => LiveTrack(t.view, LiveMode.live, lastUpdate: t.lastUpdate));
    } catch (_) {
      _fallback();
    }
  }

  void _onUpdate(LiveUpdate u) {
    if (u.bookingId != null && u.bookingId != key.$2) return;
    if (u.type == 'snapshot' || u.type == 'position') {
      _set((t) => LiveTrack(t.view.withUpdate(u), LiveMode.live, lastUpdate: u.recordedAt ?? DateTime.now()));
    } else if (u.type == 'session_ended') {
      _set((t) => LiveTrack(t.view.withUpdate(u), t.mode, lastUpdate: t.lastUpdate));
      unawaited(refresh());
    }
  }

  void _fallback() {
    _sub = null;
    _set((t) => LiveTrack(t.view, LiveMode.polling, lastUpdate: t.lastUpdate));
    final every = ref.read(trackPollProvider);
    if (every != null && _poll == null) _poll = Timer.periodic(every, (_) => refresh());
  }

  /// Reloads the view (keeps the current mode).
  Future<void> refresh() async {
    try {
      final view = await _api.view(_org, key.$2);
      _set((t) => LiveTrack(view, t.mode, lastUpdate: view.equipment?.recordedAt ?? t.lastUpdate));
    } catch (_) {
      // Keep the last good view; the next poll or update retries.
    }
  }

  void _set(LiveTrack Function(LiveTrack) f) {
    final cur = state.value;
    if (cur != null) state = AsyncData(f(cur));
  }

  void _close() {
    unawaited(_sub?.cancel());
    _sub = null;
    _poll?.cancel();
    _poll = null;
  }
}

final liveTrackProvider = AsyncNotifierProvider.autoDispose.family<LiveTrackController, LiveTrack, TrackKey>(LiveTrackController.new);

final tripHistoryProvider = FutureProvider.autoDispose.family<List<HistoryPoint>, (BookingSide, String, String)>(
  (ref, k) => ref.watch(trackingApiProvider).history(ref.watch(sideOrgProvider(k.$1)), k.$2, k.$3),
);

/// Latest positions of an organisation's machines (those the viewer may see),
/// from the fleet stream. Empty when the stream is unavailable; the fleet
/// board's last known positions are shown instead.
class FleetLiveController extends Notifier<Map<String, LiveUpdate>> {
  FleetLiveController(this.org);
  final String org;
  StreamSubscription<LiveUpdate>? _sub;

  @override
  Map<String, LiveUpdate> build() {
    ref.onDispose(() => unawaited(_sub?.cancel()));
    unawaited(_connect());
    return const {};
  }

  Future<void> _connect() async {
    try {
      final ticket = await ref.read(trackingApiProvider).fleetTicket(org);
      _sub = ref
          .read(liveChannelProvider)
          .connect(ticket)
          .listen(
            (u) {
              if (u.type == 'snapshot' || u.type == 'position') state = {...state, u.equipmentId: u};
            },
            onError: (_) {},
            cancelOnError: true,
          );
    } catch (_) {
      // Not authorised or offline: the board's last known positions remain.
    }
  }
}

final fleetLiveProvider = NotifierProvider.autoDispose.family<FleetLiveController, Map<String, LiveUpdate>, String>(FleetLiveController.new);
