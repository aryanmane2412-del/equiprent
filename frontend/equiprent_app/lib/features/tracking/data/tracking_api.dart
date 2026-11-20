import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import '../../../core/config/app_config.dart';
import '../../../core/network/api_client.dart';

Map<String, dynamic> _j(Object? v) => v == null ? <String, dynamic>{} : (v as Map).cast<String, dynamic>();
List<Map<String, dynamic>> _list(Object? v) => [for (final e in (v as List? ?? const [])) _j(e)];
DateTime? _dt(Object? v) => v == null || '$v'.startsWith('0001-') ? null : DateTime.parse('$v').toLocal();
double _d(Object? v) => (v as num?)?.toDouble() ?? 0;
String? _s(Object? v) => v == null || '$v'.isEmpty ? null : '$v';

// ---- models ---------------------------------------------------------------------------------------

@immutable
class LatLngPoint {
  const LatLngPoint(this.lat, this.lng);
  factory LatLngPoint.fromJson(Map<String, dynamic> j) => LatLngPoint(_d(j['lat']), _d(j['lng']));
  final double lat;
  final double lng;
}

/// A position with where it came from: a phone travelling with the machine
/// (app), a tracker on the machine (telematics) or the simulator.
@immutable
class Fix {
  const Fix({required this.lat, required this.lng, required this.source, this.recordedAt, this.accuracyM});
  factory Fix.fromJson(Map<String, dynamic> j) => Fix(
    lat: _d(j['lat']),
    lng: _d(j['lng']),
    source: _s(j['source']) ?? 'app',
    recordedAt: _dt(j['recorded_at']),
    accuracyM: (j['accuracy_m'] as num?)?.toDouble(),
  );
  final double lat;
  final double lng;
  final String source;
  final DateTime? recordedAt;
  final double? accuracyM;

  LatLngPoint get point => LatLngPoint(lat, lng);
  Fix copyWith({double? lat, double? lng, String? source, DateTime? recordedAt}) =>
      Fix(lat: lat ?? this.lat, lng: lng ?? this.lng, source: source ?? this.source, recordedAt: recordedAt ?? this.recordedAt, accuracyM: accuracyM);
}

@immutable
class Eta {
  const Eta({required this.distanceM, required this.durationS, required this.method, this.polyline = const []});
  factory Eta.fromJson(Map<String, dynamic> j) => Eta(
    distanceM: _d(j['distance_m']),
    durationS: _d(j['duration_s']),
    method: _s(j['method']) ?? 'straight_line',
    polyline: [for (final p in _list(j['polyline'])) LatLngPoint.fromJson(p)],
  );
  final double distanceM;
  final double durationS;
  final String method;
  final List<LatLngPoint> polyline;

  bool get roadRoute => method == 'google_directions';
}

@immutable
class TrackingSession {
  const TrackingSession({required this.active, this.tripId, this.kind, this.startedAt, this.endReason, this.intervalSeconds = 10, this.uploadSeconds = 30});
  factory TrackingSession.fromJson(Map<String, dynamic> j) => TrackingSession(
    active: j['active'] == true,
    tripId: _s(j['trip_id']),
    kind: _s(j['kind']),
    startedAt: _dt(j['started_at']),
    endReason: _s(j['end_reason']),
    intervalSeconds: (j['interval_seconds'] as num?)?.toInt() ?? 10,
    uploadSeconds: (j['upload_seconds'] as num?)?.toInt() ?? 30,
  );
  final bool active;
  final String? tripId;
  final String? kind;
  final DateTime? startedAt;
  final String? endReason;
  final int intervalSeconds;
  final int uploadSeconds;
}

@immutable
class TripRecord {
  const TripRecord({
    required this.id,
    required this.kind,
    required this.source,
    required this.startedAt,
    required this.points,
    this.endedAt,
    this.endReason,
    this.distanceM,
  });
  factory TripRecord.fromJson(Map<String, dynamic> j) => TripRecord(
    id: '${j['id']}',
    kind: '${j['kind']}',
    source: _s(j['source']) ?? 'app',
    startedAt: _dt(j['started_at'])!,
    points: (j['points'] as num?)?.toInt() ?? 0,
    endedAt: _dt(j['ended_at']),
    endReason: _s(j['end_reason']),
    distanceM: (j['distance_m'] as num?)?.toDouble(),
  );
  final String id;
  final String kind;
  final String source;
  final DateTime startedAt;
  final int points;
  final DateTime? endedAt;
  final String? endReason;
  final double? distanceM;

  String get label => switch (kind) {
    'mobilisation' => 'Delivery to site',
    'demobilisation' => 'Return to yard',
    _ => 'On site',
  };
}

/// The tracking screen's data.
@immutable
class TrackView {
  const TrackView({
    required this.bookingId,
    required this.status,
    required this.destination,
    required this.sourceNote,
    required this.route,
    required this.session,
    required this.trips,
    required this.hidden,
    this.equipment,
    this.operator,
    this.eta,
  });
  factory TrackView.fromJson(Map<String, dynamic> j) => TrackView(
    bookingId: '${j['booking_id']}',
    status: '${j['status']}',
    destination: LatLngPoint.fromJson(_j(j['destination'])),
    sourceNote: _s(j['source_note']) ?? '',
    route: [for (final p in _list(j['route'])) LatLngPoint.fromJson(p)],
    session: TrackingSession.fromJson(_j(j['session'])),
    trips: [for (final t in _list(j['trips'])) TripRecord.fromJson(t)],
    hidden: j['hidden'] == true,
    equipment: j['equipment_position'] == null ? null : Fix.fromJson(_j(j['equipment_position'])),
    operator: j['operator_position'] == null ? null : Fix.fromJson(_j(j['operator_position'])),
    eta: j['eta'] == null ? null : Eta.fromJson(_j(j['eta'])),
  );
  final String bookingId;
  final String status;
  final LatLngPoint destination;
  final String sourceNote;
  final List<LatLngPoint> route;
  final TrackingSession session;
  final List<TripRecord> trips;
  final bool hidden;
  final Fix? equipment;
  final Fix? operator;
  final Eta? eta;

  /// Applies a live update from the stream.
  TrackView withUpdate(LiveUpdate u) {
    if (u.type == 'session_ended') {
      return TrackView(
        bookingId: bookingId,
        status: status,
        destination: destination,
        sourceNote: sourceNote,
        route: route,
        session: const TrackingSession(active: false),
        trips: trips,
        hidden: hidden,
        equipment: equipment,
        operator: operator,
        eta: eta,
      );
    }
    final f = Fix(lat: u.lat, lng: u.lng, source: u.source, recordedAt: u.recordedAt, accuracyM: u.accuracyM);
    final isPhone = u.source != 'telematics';
    final keepTracker = equipment?.source == 'telematics' && isPhone;
    return TrackView(
      bookingId: bookingId,
      status: status,
      destination: destination,
      sourceNote: sourceNote,
      route: u.type == 'position' && session.active ? [...route, f.point] : route,
      session: session,
      trips: trips,
      hidden: hidden,
      equipment: keepTracker ? equipment : f,
      operator: isPhone ? f : operator,
      eta: eta,
    );
  }
}

@immutable
class HistoryPoint {
  const HistoryPoint({required this.lat, required this.lng, required this.recordedAt, required this.source, this.speedMps});
  factory HistoryPoint.fromJson(Map<String, dynamic> j) => HistoryPoint(
    lat: _d(j['lat']),
    lng: _d(j['lng']),
    recordedAt: _dt(j['recorded_at'])!,
    source: _s(j['source']) ?? 'app',
    speedMps: (j['speed_mps'] as num?)?.toDouble(),
  );
  final double lat;
  final double lng;
  final DateTime recordedAt;
  final String source;
  final double? speedMps;
}

@immutable
class TrackingPolicy {
  const TrackingPolicy({required this.version, required this.title, required this.points});
  factory TrackingPolicy.fromJson(Map<String, dynamic> j) =>
      TrackingPolicy(version: '${j['version']}', title: '${j['title']}', points: [for (final p in (j['points'] as List? ?? const [])) '$p']);
  final String version;
  final String title;
  final List<String> points;
}

/// One message from the live stream.
@immutable
class LiveUpdate {
  const LiveUpdate({
    required this.type,
    required this.equipmentId,
    this.bookingId,
    this.lat = 0,
    this.lng = 0,
    this.recordedAt,
    this.source = 'app',
    this.accuracyM,
  });
  factory LiveUpdate.fromJson(Map<String, dynamic> j) => LiveUpdate(
    type: '${j['type']}',
    equipmentId: '${j['equipment_id']}',
    bookingId: _s(j['booking_id']),
    lat: _d(j['lat']),
    lng: _d(j['lng']),
    recordedAt: _dt(j['recorded_at']),
    source: _s(j['source']) ?? 'app',
    accuracyM: (j['accuracy_m'] as num?)?.toDouble(),
  );
  final String type;
  final String equipmentId;
  final String? bookingId;
  final double lat;
  final double lng;
  final DateTime? recordedAt;
  final String source;
  final double? accuracyM;
}

/// A recorded GPS fix waiting to be uploaded.
@immutable
class GeoFix {
  const GeoFix({required this.lat, required this.lng, required this.at, this.accuracyM, this.speedMps, this.heading});
  final double lat;
  final double lng;
  final DateTime at;
  final double? accuracyM;
  final double? speedMps;
  final int? heading;

  Map<String, dynamic> toJson() => {
    'lat': lat,
    'lng': lng,
    'recorded_at': at.toUtc().toIso8601String(),
    if (accuracyM != null) 'accuracy_m': accuracyM,
    if (speedMps != null && speedMps! >= 0) 'speed_mps': speedMps,
    if (heading != null && heading! >= 0 && heading! < 360) 'heading': heading,
  };
}

class Ticket {
  const Ticket(this.ticket, this.path);
  final String ticket;
  final String path;
}

// ---- api ------------------------------------------------------------------------------------------

/// Tracking endpoints. [org] is sent as X-Org-Id.
abstract class TrackingApi {
  Future<TrackingPolicy> policy();
  Future<TrackView> view(String? org, String bookingId);
  Future<List<HistoryPoint>> history(String? org, String bookingId, String tripId);
  Future<TrackingSession> startSession(String bookingId, {required String policyVersion, required bool background, String? platform});
  Future<TrackingSession> stopSession(String bookingId);
  Future<void> upload(String bookingId, List<GeoFix> points, {required bool simulated});
  Future<Ticket> bookingTicket(String? org, String bookingId);
  Future<Ticket> fleetTicket(String org);
}

class HttpTrackingApi implements TrackingApi {
  HttpTrackingApi(this._c);
  final ApiClient _c;
  static Map<String, String>? _h(String? org) => org == null ? null : {'X-Org-Id': org};

  @override
  Future<TrackingPolicy> policy() => _c.get('/tracking/policy', decode: (j) => TrackingPolicy.fromJson(_j(j)));
  @override
  Future<TrackView> view(String? org, String bookingId) => _c.get('/bookings/$bookingId/track', headers: _h(org), decode: (j) => TrackView.fromJson(_j(j)));
  @override
  Future<List<HistoryPoint>> history(String? org, String bookingId, String tripId) => _c.get(
    '/bookings/$bookingId/track/history',
    headers: _h(org),
    query: {'trip_id': tripId},
    decode: (j) => [for (final p in _list(_j(j)['points'])) HistoryPoint.fromJson(p)],
  );
  @override
  Future<TrackingSession> startSession(String bookingId, {required String policyVersion, required bool background, String? platform}) => _c.post(
    '/bookings/$bookingId/tracking/session',
    body: {'consent': true, 'policy_version': policyVersion, 'background': background, 'platform': ?platform},
    decode: (j) => TrackingSession.fromJson(_j(j)),
  );
  @override
  Future<TrackingSession> stopSession(String bookingId) async {
    await _c.delete('/bookings/$bookingId/tracking/session');
    return const TrackingSession(active: false);
  }

  @override
  Future<void> upload(String bookingId, List<GeoFix> points, {required bool simulated}) => _c.post(
    '/operator/jobs/$bookingId/tracking/batch',
    body: {
      'points': [for (final p in points) p.toJson()],
      'simulated': simulated,
    },
    decode: (_) {},
  );
  @override
  Future<Ticket> bookingTicket(String? org, String bookingId) =>
      _c.post('/bookings/$bookingId/track/ticket', headers: _h(org), decode: (j) => Ticket('${_j(j)['ticket']}', '${_j(j)['path']}'));
  @override
  Future<Ticket> fleetTicket(String org) => _c.post('/fleet/$org/live/ticket', decode: (j) => Ticket('${_j(j)['ticket']}', '${_j(j)['path']}'));
}

final trackingApiProvider = Provider<TrackingApi>((ref) => HttpTrackingApi(ref.watch(apiClientProvider)));

// ---- live channel ---------------------------------------------------------------------------------

/// Opens the live position stream for a ticket. Overridable in tests.
abstract class LiveChannel {
  Stream<LiveUpdate> connect(Ticket ticket);
}

class WebSocketLiveChannel implements LiveChannel {
  WebSocketLiveChannel(this.apiUrl);

  /// The versioned API base, e.g. `http://localhost:8080/api/v1`.
  final String apiUrl;

  @override
  Stream<LiveUpdate> connect(Ticket ticket) {
    final base = Uri.parse(apiUrl);
    final uri = base.replace(scheme: base.scheme == 'https' ? 'wss' : 'ws', path: '${base.path}${ticket.path}', queryParameters: {'ticket': ticket.ticket});
    final channel = WebSocketChannel.connect(uri);
    return channel.stream.map((raw) => LiveUpdate.fromJson(_j(jsonDecode('$raw'))));
  }
}

final liveChannelProvider = Provider<LiveChannel>((ref) => WebSocketLiveChannel(ref.watch(appConfigProvider).apiUrl));
