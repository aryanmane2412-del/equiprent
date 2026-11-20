import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/comms/data/comms_api.dart' show BookingSide;
import 'package:equiprent_app/features/tracking/data/live_track.dart';
import 'package:equiprent_app/features/tracking/data/location_source.dart';
import 'package:equiprent_app/features/tracking/data/tracking_api.dart';
import 'package:equiprent_app/features/tracking/presentation/tracking_map.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_operations_api.dart';
import 'fake_tracking_api.dart';
import 'test_helpers.dart';

void main() {
  group('booking tracking view', () {
    testWidgets('customers see no location before dispatch', (tester) async {
      final api = FakeTrackingApi(statusOf: (_) => 'confirmed');
      final live = FakeLiveChannel();
      final c = await pumpApp(tester, tracking: api, live: live);
      await goTo(tester, c, '/customer/bookings/bk-1/track', role: UserRole.customer);
      expect(find.byKey(const Key('tracking-hidden')), findsOneWidget);
      expect(find.byType(TrackingMap), findsNothing);
      expect(api.calls.where((x) => x.startsWith('ticket')), isEmpty, reason: 'no live stream for hidden locations');
      expect(live.tickets, isEmpty);
    });

    testWidgets('shows where the location comes from, ETA method, and follows live updates', (tester) async {
      final api = FakeTrackingApi();
      final live = FakeLiveChannel();
      final c = await pumpApp(tester, tracking: api, live: live);
      await goTo(tester, c, '/customer/bookings/bk-1/track', role: UserRole.customer);

      expect(find.text('5.4 km'), findsOneWidget);
      expect(find.text('18 min'), findsOneWidget);
      expect(find.textContaining('Approximate (straight line'), findsOneWidget);
      expect(find.text("Operator's phone"), findsOneWidget);
      expect(find.byKey(const Key('source-note')), findsOneWidget);
      // No Google key in tests: the built-in map with an explanation.
      expect(find.byKey(const Key('builtin-map')), findsOneWidget);
      expect(find.byKey(const Key('map-note')), findsOneWidget);
      expect(live.tickets, ['tk-bk-1']);
      expect(find.byKey(const Key('mode-live')), findsOneWidget);

      // A tracker on the machine reports: the machine position switches source.
      live.push(LiveUpdate(type: 'position', equipmentId: 'eq-1', bookingId: 'bk-1', lat: 18.58, lng: 73.75, recordedAt: DateTime.now(), source: 'telematics'));
      await settle(tester);
      final v = c.read(liveTrackProvider((BookingSide.customer, 'bk-1'))).value!.view;
      expect(v.equipment!.lat, 18.58);
      expect(v.equipment!.source, 'telematics');
      expect(find.text('Tracker on the machine'), findsOneWidget);

      // A later phone update does not override the tracker's machine position.
      live.push(LiveUpdate(type: 'position', equipmentId: 'eq-1', bookingId: 'bk-1', lat: 18.50, lng: 73.70, recordedAt: DateTime.now(), source: 'app'));
      await settle(tester);
      final v2 = c.read(liveTrackProvider((BookingSide.customer, 'bk-1'))).value!.view;
      expect(v2.equipment!.lat, 18.58);
      expect(v2.operator!.lat, 18.50);

      // Updates for another booking on the same machine are ignored.
      live.push(LiveUpdate(type: 'position', equipmentId: 'eq-1', bookingId: 'bk-other', lat: 1, lng: 1, recordedAt: DateTime.now()));
      await settle(tester);
      expect(c.read(liveTrackProvider((BookingSide.customer, 'bk-1'))).value!.view.operator!.lat, 18.50);
      await live.closeAll();
      await settle(tester);
    });

    testWidgets('falls back to refreshing when the live stream is unavailable', (tester) async {
      final api = FakeTrackingApi()..failTickets = true;
      final c = await pumpApp(tester, tracking: api);
      await goTo(tester, c, '/owner/bookings/bk-1/track', role: UserRole.owner);
      expect(find.byKey(const Key('mode-polling')), findsOneWidget);
      expect(find.text('5.4 km'), findsOneWidget);
    });

    testWidgets('session end from the server and trip history', (tester) async {
      final api = FakeTrackingApi()..sessions.add('bk-1');
      final live = FakeLiveChannel();
      final c = await pumpApp(tester, tracking: api, live: live, size: tablet);
      await goTo(tester, c, '/customer/bookings/bk-1/track', role: UserRole.customer);
      expect(find.text('Sharing now'), findsOneWidget);

      api.sessions.clear();
      live.push(const LiveUpdate(type: 'session_ended', equipmentId: 'eq-1', bookingId: 'bk-1'));
      await settle(tester);
      expect(find.text('Not sharing'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('trip-trip-1')));
      expect(find.byKey(const Key('trip-history')), findsOneWidget);
      expect(api.calls, contains('history trip-1'));
      await live.closeAll();
      await settle(tester);
    });
  });

  group('sharing location (operator)', () {
    Future<(FakeTrackingApi, FakeLocationSource)> openJob(
      WidgetTester tester, {
      LocationAccess access = LocationAccess.granted,
      String status = 'dispatched',
    }) async {
      final op = FakeOperatorApi()
        ..assignmentStatus = 'accepted'
        ..bookingStatus = status;
      final api = FakeTrackingApi(statusOf: (_) => status);
      final loc = FakeLocationSource(access: access);
      final c = await pumpApp(tester, operator: op, tracking: api, location: loc, size: tablet);
      await goTo(tester, c, '/operator/jobs/bk-op', role: UserRole.operator);
      return (api, loc);
    }

    testWidgets('consent first, then permission, uploads in batches, and stops', (tester) async {
      final (api, loc) = await openJob(tester);
      expect(find.byKey(const Key('share-location')), findsOneWidget);

      // Declining the notice asks for no permission and starts nothing.
      await tapOn(tester, find.byKey(const Key('start-sharing')));
      expect(find.byKey(const Key('tracking-notice')), findsOneWidget);
      expect(find.descendant(of: find.byKey(const Key('tracking-notice')), matching: find.textContaining('stops automatically')), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('decline-tracking')));
      expect(loc.askedBackground, isNull);
      expect(api.calls.where((x) => x.startsWith('start')), isEmpty);

      // Agree with background sharing.
      await tapOn(tester, find.byKey(const Key('start-sharing')));
      await tapOn(tester, find.byKey(const Key('background-switch')));
      await tapOn(tester, find.byKey(const Key('accept-tracking')));
      expect(loc.askedBackground, isTrue);
      expect(api.calls, contains('start bk-op bg=true v=2026-11'));
      expect(find.byKey(const Key('sharing-on')), findsOneWidget);
      expect(loc.watching, isTrue);

      loc
        ..emit(18.55, 73.80)
        ..emit(18.56, 73.79);
      await tester.pump();
      await tester.pump(); // stream delivery, then rebuild
      expect(find.text('0 sent · 2 waiting'), findsOneWidget);
      await tester.pump(const Duration(seconds: 30)); // upload tick
      await settle(tester);
      expect(api.calls, contains('upload bk-op 2'));
      expect(api.uploads['bk-op'], hasLength(2));
      expect(find.text('2 sent · 0 waiting'), findsOneWidget);
      expect(api.simulatedUploads, isEmpty, reason: 'real device positions are not flagged simulated');

      await tapOn(tester, find.byKey(const Key('stop-sharing')));
      expect(api.calls, contains('stop bk-op'));
      expect(loc.watching, isFalse, reason: 'GPS stops with the session');
      expect(find.byKey(const Key('start-sharing')), findsOneWidget);
    });

    testWidgets('blocked permission explains and starts no session', (tester) async {
      final (api, loc) = await openJob(tester, access: LocationAccess.deniedForever);
      await tapOn(tester, find.byKey(const Key('start-sharing')));
      await tapOn(tester, find.byKey(const Key('accept-tracking')));
      expect(find.byKey(const Key('share-message')), findsOneWidget);
      expect(find.textContaining('system settings'), findsOneWidget);
      expect(api.calls.where((x) => x.startsWith('start')), isEmpty);
      expect(loc.watching, isFalse);
    });

    testWidgets('sharing stops when the server closes the session', (tester) async {
      final (api, loc) = await openJob(tester);
      await tapOn(tester, find.byKey(const Key('start-sharing')));
      await tapOn(tester, find.byKey(const Key('accept-tracking')));
      expect(find.byKey(const Key('sharing-on')), findsOneWidget);

      api.sessions.clear(); // e.g. the job was returned
      loc.emit(18.55, 73.80);
      await tester.pump(const Duration(seconds: 30));
      await settle(tester);
      expect(find.byKey(const Key('sharing-on')), findsNothing);
      expect(find.textContaining('Sharing stopped'), findsOneWidget);
      expect(loc.watching, isFalse);
    });

    testWidgets('no share card outside the trackable stages', (tester) async {
      await openJob(tester, status: 'operator_assigned');
      expect(find.byKey(const Key('share-location')), findsNothing);
    });
  });

  testWidgets('fleet live map merges last known positions with the stream', (tester) async {
    final live = FakeLiveChannel();
    final api = FakeTrackingApi();
    final c = await pumpApp(tester, tracking: api, live: live, size: tablet);
    await goTo(tester, c, '/fleet', role: UserRole.fleetManager);
    await tapOn(tester, find.byKey(const Key('fleet-live')));
    expect(find.text('Live fleet map'), findsOneWidget);
    expect(api.calls, contains('fleet-ticket org-fleetManager'));
    expect(find.byType(TrackingMap), findsOneWidget);
    expect(find.text('Live'), findsNothing);

    live.push(LiveUpdate(type: 'snapshot', equipmentId: 'eq-1', lat: 18.6, lng: 73.7, recordedAt: DateTime.now(), source: 'telematics'));
    await settle(tester);
    expect(find.descendant(of: find.byKey(const Key('live-eq-1')), matching: find.text('Live')), findsOneWidget);
    await live.closeAll();
    await settle(tester);
  });

  group('simulated GPS', () {
    test('drives towards the site and stays there', () {
      final sim = SimulatedLocationSource(steps: 10);
      const site = LatLngPoint(18.59, 73.74);
      final first = sim.fixAt(site, 0);
      final last = sim.fixAt(site, 10);
      final after = sim.fixAt(site, 25);
      expect(sim.simulated, isTrue);
      expect((first.lat - site.lat).abs(), greaterThan(0.02));
      expect(last.lat, closeTo(site.lat, 1e-9));
      expect(last.lng, closeTo(site.lng, 1e-9));
      expect(after.lat, last.lat);
      expect(after.speedMps, 0);
    });

    test('live channel builds a ws URL from the API base', () {
      final ch = WebSocketLiveChannel('https://api.example.in/api/v1');
      expect(ch.apiUrl, 'https://api.example.in/api/v1');
    });

    test('track view parses the server payload', () {
      final v = TrackView.fromJson({
        'booking_id': 'b',
        'status': 'dispatched',
        'destination': {'lat': 18.5, 'lng': 73.8},
        'equipment_position': {'lat': 18.4, 'lng': 73.7, 'recorded_at': '2026-11-15T08:00:00Z', 'source': 'telematics', 'accuracy_m': 4},
        'source_note': 'tracker',
        'eta': {
          'distance_m': 1200,
          'duration_s': 300,
          'method': 'google_directions',
          'polyline': [
            {'lat': 18.4, 'lng': 73.7},
            {'lat': 18.5, 'lng': 73.8},
          ],
        },
        'route': [],
        'session': {'active': true, 'interval_seconds': 10, 'upload_seconds': 30},
        'trips': [
          {'id': 't', 'kind': 'demobilisation', 'source': 'app', 'started_at': '2026-11-15T07:00:00Z', 'points': 3, 'end_reason': 'idle_timeout'},
        ],
        'hidden': false,
      });
      expect(v.equipment!.source, 'telematics');
      expect(v.eta!.roadRoute, isTrue);
      expect(v.eta!.polyline, hasLength(2));
      expect(v.trips.single.label, 'Return to yard');
      expect(v.session.active, isTrue);
    });
  });
}
