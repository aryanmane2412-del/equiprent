import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_exception.dart';
import 'location_source.dart';
import 'tracking_api.dart';

enum SharePhase { off, starting, sharing, blocked }

@immutable
class ShareState {
  const ShareState({
    this.phase = SharePhase.off,
    this.session,
    this.lastFix,
    this.lastUploadAt,
    this.pending = 0,
    this.uploaded = 0,
    this.message,
    this.simulated = false,
  });
  final SharePhase phase;
  final TrackingSession? session;
  final GeoFix? lastFix;
  final DateTime? lastUploadAt;
  final int pending;
  final int uploaded;

  /// Why sharing stopped or could not start (permission, server refusal).
  final String? message;
  final bool simulated;

  bool get sharing => phase == SharePhase.sharing;

  ShareState copyWith({
    SharePhase? phase,
    TrackingSession? session,
    GeoFix? lastFix,
    DateTime? lastUploadAt,
    int? pending,
    int? uploaded,
    String? message,
    bool clearMessage = false,
    bool? simulated,
  }) => ShareState(
    phase: phase ?? this.phase,
    session: session ?? this.session,
    lastFix: lastFix ?? this.lastFix,
    lastUploadAt: lastUploadAt ?? this.lastUploadAt,
    pending: pending ?? this.pending,
    uploaded: uploaded ?? this.uploaded,
    message: clearMessage ? null : (message ?? this.message),
    simulated: simulated ?? this.simulated,
  );
}

/// Shares this device's location for one booking while a tracking session is
/// open: records a fix every `interval_seconds`, uploads the buffer every
/// `upload_seconds`, and stops for good when the person stops or the server
/// says the session is over (job finished, consent withdrawn, idle timeout).
class ShareController extends Notifier<ShareState> {
  ShareController(this.bookingId);
  final String bookingId;

  StreamSubscription<GeoFix>? _sub;
  Timer? _timer;
  final _buffer = <GeoFix>[];
  bool _flushing = false;

  @override
  ShareState build() {
    ref.onDispose(_teardown);
    return ShareState(simulated: ref.read(locationSourceProvider).simulated);
  }

  TrackingApi get _api => ref.read(trackingApiProvider);
  LocationSource get _source => ref.read(locationSourceProvider);

  /// Starts sharing after the person accepted [policy]. Permission is asked
  /// only now, never before consent.
  Future<void> start({required TrackingPolicy policy, required bool background, required LatLngPoint destination, String? platform}) async {
    if (state.phase == SharePhase.starting || state.sharing) return;
    state = state.copyWith(phase: SharePhase.starting, clearMessage: true);
    final access = await _source.prepare(background: background);
    if (access != LocationAccess.granted) {
      state = state.copyWith(phase: SharePhase.blocked, message: _accessMessage(access));
      return;
    }
    try {
      final session = await _api.startSession(bookingId, policyVersion: policy.version, background: background, platform: platform);
      state = state.copyWith(phase: SharePhase.sharing, session: session);
      _sub = _source
          .watch(
            interval: Duration(seconds: session.intervalSeconds),
            background: background,
            towards: destination,
          )
          .listen(_onFix, onError: (Object e) => state = state.copyWith(message: 'Location unavailable: $e'));
      _timer = Timer.periodic(Duration(seconds: session.uploadSeconds), (_) => flush());
    } on ApiException catch (e) {
      state = state.copyWith(phase: SharePhase.blocked, message: e.message);
    }
  }

  void _onFix(GeoFix f) {
    _buffer.add(f);
    state = state.copyWith(lastFix: f, pending: _buffer.length);
    if (_buffer.length >= 50) flush();
  }

  /// Uploads buffered positions. Kept on failure (offline) and retried on
  /// the next tick; dropped only when the server refuses them for good.
  Future<void> flush() async {
    if (_flushing || _buffer.isEmpty || !state.sharing) return;
    _flushing = true;
    final batch = List<GeoFix>.of(_buffer);
    try {
      await _api.upload(bookingId, batch, simulated: _source.simulated);
      _buffer.removeRange(0, batch.length);
      state = state.copyWith(lastUploadAt: DateTime.now(), pending: _buffer.length, uploaded: state.uploaded + batch.length);
    } on ApiException catch (e) {
      // 403/409/422: no consent, job not in a trackable stage, session over.
      final terminal = e.kind == ApiErrorKind.forbidden || e.kind == ApiErrorKind.conflict || e.statusCode == 422 || e.kind == ApiErrorKind.notFound;
      if (terminal) {
        _teardown();
        _buffer.clear();
        state = state.copyWith(phase: SharePhase.off, pending: 0, message: 'Sharing stopped: ${e.message}');
      } else {
        state = state.copyWith(message: 'Will retry upload: ${e.message}');
      }
    } finally {
      _flushing = false;
    }
  }

  /// Stops sharing: uploads what is left, then closes the session on the server.
  Future<void> stop() async {
    if (!state.sharing) return;
    await flush();
    _teardown();
    _buffer.clear();
    try {
      await _api.stopSession(bookingId);
    } on ApiException catch (_) {
      // Already closed server-side; nothing else to do.
    }
    state = state.copyWith(phase: SharePhase.off, pending: 0, session: const TrackingSession(active: false));
  }

  /// The server ended the session (job moved on, idle timeout).
  void endedRemotely() {
    if (!state.sharing) return;
    _teardown();
    _buffer.clear();
    state = state.copyWith(phase: SharePhase.off, pending: 0, message: 'The tracking session ended.');
  }

  void _teardown() {
    _timer?.cancel();
    _timer = null;
    unawaited(_sub?.cancel());
    _sub = null;
  }

  static String _accessMessage(LocationAccess a) => switch (a) {
    LocationAccess.serviceOff => 'Turn on location services to share your position.',
    LocationAccess.denied => 'Location permission was not granted.',
    LocationAccess.deniedForever => 'Location permission is blocked. Allow it in system settings to share.',
    LocationAccess.unsupported => 'Location is not available on this device.',
    LocationAccess.granted => '',
  };
}

final shareControllerProvider = NotifierProvider.family<ShareController, ShareState, String>(ShareController.new);

final trackingPolicyProvider = FutureProvider<TrackingPolicy>((ref) => ref.watch(trackingApiProvider).policy());
