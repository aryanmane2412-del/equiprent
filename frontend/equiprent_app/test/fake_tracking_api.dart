import 'dart:async';

import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/features/tracking/data/location_source.dart';
import 'package:equiprent_app/features/tracking/data/tracking_api.dart';

const _trackable = {'dispatched', 'on_site', 'active', 'extension_requested', 'cancellation_requested', 'return_initiated'};

/// Server-side tracking state for tests: sessions need consent and a
/// trackable status, customers see nothing outside the trackable stages, and
/// uploads without an open session are refused as the backend does.
class FakeTrackingApi implements TrackingApi {
  FakeTrackingApi({String Function(String id)? statusOf}) : _statusOf = statusOf ?? ((_) => 'dispatched');
  final String Function(String id) _statusOf;

  final calls = <String>[];
  final sessions = <String>{};
  final uploads = <String, List<GeoFix>>{};
  final simulatedUploads = <String>{};
  bool failTickets = false;

  static const site = LatLngPoint(18.5913, 73.7389);
  static const policyV = '2026-11';

  @override
  Future<TrackingPolicy> policy() async => const TrackingPolicy(
    version: policyV,
    title: 'Location sharing for this job',
    points: [
      "Your phone's location is shared with the customer and owner of this booking only.",
      'Sharing stops automatically when the job ends, or when you stop it.',
      'Who viewed the location is logged.',
    ],
  );

  @override
  Future<TrackView> view(String? org, String bookingId) async {
    calls.add('view $bookingId');
    final status = _statusOf(bookingId);
    final customer = org == 'org-customer';
    final hidden = customer && !_trackable.contains(status);
    final active = sessions.contains(bookingId);
    final last = uploads[bookingId]?.last;
    return TrackView(
      bookingId: bookingId,
      status: status,
      destination: site,
      sourceNote: hidden ? '' : "Location comes from the operator's phone. It shows where the phone is, which is usually with the machine.",
      route: hidden ? const [] : const [LatLngPoint(18.55, 73.80), LatLngPoint(18.56, 73.79)],
      session: TrackingSession(active: active, startedAt: active ? DateTime.now() : null),
      trips: hidden
          ? const []
          : [TripRecord(id: 'trip-1', kind: 'mobilisation', source: 'app', startedAt: DateTime.now().subtract(const Duration(minutes: 20)), points: 2)],
      hidden: hidden,
      equipment: hidden
          ? null
          : Fix(
              lat: last?.lat ?? 18.56,
              lng: last?.lng ?? 73.79,
              source: 'app',
              recordedAt: DateTime.now().subtract(const Duration(minutes: 1)),
              accuracyM: 12,
            ),
      eta: hidden || !{'dispatched', 'return_initiated'}.contains(status) ? null : const Eta(distanceM: 5400, durationS: 1080, method: 'straight_line'),
    );
  }

  @override
  Future<List<HistoryPoint>> history(String? org, String bookingId, String tripId) async {
    calls.add('history $tripId');
    final t = DateTime.now();
    return [
      HistoryPoint(lat: 18.55, lng: 73.80, recordedAt: t.subtract(const Duration(minutes: 20)), source: 'app'),
      HistoryPoint(lat: 18.56, lng: 73.79, recordedAt: t.subtract(const Duration(minutes: 1)), source: 'app'),
    ];
  }

  @override
  Future<TrackingSession> startSession(String bookingId, {required String policyVersion, required bool background, String? platform}) async {
    calls.add('start $bookingId bg=$background v=$policyVersion');
    if (policyVersion != policyV) throw const ApiException(kind: ApiErrorKind.conflict, statusCode: 409, code: 'POLICY_CHANGED', message: 'The notice changed');
    if (!_trackable.contains(_statusOf(bookingId))) {
      throw const ApiException(kind: ApiErrorKind.conflict, statusCode: 409, code: 'INVALID_TRANSITION', message: 'Not in a trackable stage');
    }
    sessions.add(bookingId);
    return TrackingSession(active: true, tripId: 'trip-1', kind: 'mobilisation', startedAt: DateTime.now(), intervalSeconds: 10, uploadSeconds: 30);
  }

  @override
  Future<TrackingSession> stopSession(String bookingId) async {
    calls.add('stop $bookingId');
    sessions.remove(bookingId);
    return const TrackingSession(active: false);
  }

  @override
  Future<void> upload(String bookingId, List<GeoFix> points, {required bool simulated}) async {
    calls.add('upload $bookingId ${points.length}');
    if (!sessions.contains(bookingId)) {
      throw const ApiException(kind: ApiErrorKind.forbidden, statusCode: 403, code: 'TRACKING_CONSENT_REQUIRED', message: 'Start a tracking session first');
    }
    (uploads[bookingId] ??= []).addAll(points);
    if (simulated) simulatedUploads.add(bookingId);
  }

  @override
  Future<Ticket> bookingTicket(String? org, String bookingId) async {
    calls.add('ticket $bookingId');
    if (failTickets) throw const ApiException(kind: ApiErrorKind.network, code: 'NETWORK', message: 'offline');
    return Ticket('tk-$bookingId', '/ws/track');
  }

  @override
  Future<Ticket> fleetTicket(String org) async {
    calls.add('fleet-ticket $org');
    return Ticket('tk-fleet-$org', '/ws/track');
  }
}

/// A live stream the test pushes updates into.
class FakeLiveChannel implements LiveChannel {
  final tickets = <String>[];
  final _controllers = <StreamController<LiveUpdate>>[];

  @override
  Stream<LiveUpdate> connect(Ticket ticket) {
    tickets.add(ticket.ticket);
    final c = StreamController<LiveUpdate>();
    _controllers.add(c);
    return c.stream;
  }

  void push(LiveUpdate u) {
    for (final c in _controllers) {
      if (!c.isClosed) c.add(u);
    }
  }

  Future<void> closeAll() async {
    for (final c in _controllers) {
      await c.close();
    }
  }
}

/// A location source the test feeds fixes into.
class FakeLocationSource implements LocationSource {
  FakeLocationSource({this.access = LocationAccess.granted, this.simulated = false});
  LocationAccess access;
  @override
  final bool simulated;
  bool? askedBackground;
  StreamController<GeoFix>? _c;

  bool get watching => _c != null && _c!.hasListener;

  @override
  Future<LocationAccess> prepare({required bool background}) async {
    askedBackground = background;
    return access;
  }

  @override
  Stream<GeoFix> watch({required Duration interval, required bool background, LatLngPoint? towards}) {
    _c = StreamController<GeoFix>(onCancel: () => _c = null);
    return _c!.stream;
  }

  void emit(double lat, double lng) => _c?.add(GeoFix(lat: lat, lng: lng, at: DateTime.now(), accuracyM: 5));
}
