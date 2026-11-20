import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/operator/data/operator_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_operations_api.dart';
import 'test_helpers.dart';

/// Types into a field and closes the keyboard (so later taps can scroll freely).
Future<void> typeIn(WidgetTester tester, String key, String text) async {
  await tester.ensureVisible(find.byKey(Key(key)).first);
  await tester.pumpAndSettle();
  await tester.enterText(
    find
        .descendant(
          of: find.byKey(Key(key)),
          matching: find.byType(EditableText),
        )
        .first,
    text,
  );
  FocusManager.instance.primaryFocus?.unfocus();
  await tester.pumpAndSettle();
}

void main() {
  group('operator', () {
    testWidgets(
      'job flow: accept → dispatch → arrive → handover checklist → start → return checklist → returned',
      (tester) async {
        final api = FakeOperatorApi();
        final opened = <String>[];
        final c = await pumpApp(
          tester,
          operator: api,
          opened: opened,
          size: desktop,
        );
        await goTo(tester, c, '/operator/assignments', role: UserRole.operator);
        expect(find.text('Needs your answer'), findsOneWidget);
        await tapOn(tester, find.byKey(const Key('job-bk-op')));

        // Before accepting: no phone numbers; accept / decline.
        expect(
          find.text('Phone shown once you accept the job.'),
          findsNWidgets(2),
        );
        expect(find.text('MH12KX4471'), findsOneWidget);
        await tapOn(tester, find.byKey(const Key('step-accept')));
        expect(api.calls, contains('accept bk-op'));
        expect(
          find.byKey(const Key('call-+919800000001')),
          findsOneWidget,
          reason: 'customer phone after accepting',
        );

        // Navigation opens directions; arrival from the navigation page.
        await tapOn(tester, find.byKey(const Key('step-dispatch')));
        expect(api.calls, contains('dispatch bk-op'));
        expect(find.text('Navigate to site'), findsOneWidget);
        await tapOn(tester, find.byKey(const Key('open-directions')));
        expect(opened.single, startsWith('https://www.google.com/maps/dir/'));
        await tapOn(tester, find.byKey(const Key('arrived-here')));
        expect(api.calls, contains('arrived bk-op'));

        // Handover checklist: photos are required.
        await tapOn(tester, find.byKey(const Key('step-handover')));
        expect(find.text('Handover checklist'), findsWidgets);
        await tapOn(tester, find.byKey(const Key('checklist-submit')));
        expect(
          api.calls.where((x) => x.startsWith('checklist')),
          isEmpty,
          reason: 'photos missing',
        );
        await tapOn(tester, find.byKey(const Key('checklist-photos')));
        await tapOn(tester, find.byKey(const Key('checklist-photos')));
        await tapOn(tester, find.byKey(const Key('checklist-submit')));
        expect(
          api.calls.where((x) => x == 'checklist handover 2'),
          hasLength(1),
        );

        await tapOn(tester, find.byKey(const Key('step-start')));
        expect(api.calls, contains('start bk-op'));
        expect(find.textContaining('Handover (before) ·'), findsOneWidget);

        // Report an issue during the rental.
        await tapOn(tester, find.byKey(const Key('report-issue')));
        await typeIn(tester, 'issue-description', 'Track tension low');
        await tapOn(tester, find.byKey(const Key('issue-submit')));
        expect(
          api.calls,
          contains('issue damage during_rental minor Track tension low'),
        );

        // The customer asks to return; the after-checklist comes first.
        api.bookingStatus = 'return_initiated';
        c.invalidate(myJobProvider('bk-op'));
        await tester.pumpAndSettle();
        await tapOn(tester, find.byKey(const Key('step-return')));
        await typeIn(tester, 'checklist-damage', 'Dent on bucket');
        await tapOn(tester, find.byKey(const Key('checklist-photos')));
        await tapOn(tester, find.byKey(const Key('checklist-photos')));
        await tapOn(tester, find.byKey(const Key('checklist-submit')));
        expect(
          api.calls.where((x) => x == 'checklist return 2'),
          hasLength(1),
        );
        await tapOn(tester, find.byKey(const Key('step-returned')));
        expect(api.calls, contains('returned bk-op'));
        expect(find.text('This job is completed'), findsOneWidget);

        // It is now history.
        await goTo(tester, c, '/operator/history', role: UserRole.operator);
        expect(find.byKey(const Key('job-bk-op')), findsOneWidget);
      },
    );

    testWidgets('declining asks for confirmation and closes the job', (
      tester,
    ) async {
      final api = FakeOperatorApi();
      final c = await pumpApp(tester, operator: api);
      await goTo(tester, c, '/operator/jobs/bk-op', role: UserRole.operator);
      await tapOn(tester, find.byKey(const Key('step-decline')));
      await tapOn(tester, find.text('Decline').last);
      expect(api.calls, contains('decline bk-op'));
      expect(find.text('This job is declined'), findsOneWidget);
    });

    testWidgets('today lists open jobs and the next two weeks', (tester) async {
      final c = await pumpApp(tester);
      await goTo(tester, c, '/operator', role: UserRole.operator);
      expect(find.byKey(const Key('job-bk-op')), findsOneWidget);
      expect(find.byKey(const Key('schedule-bk-op')), findsOneWidget);
    });
  });

  group('fleet manager', () {
    testWidgets('overview: scoped KPIs, alerts and fleets; create a fleet', (
      tester,
    ) async {
      final fleet = FakeFleetApi();
      final c = await pumpApp(tester, fleet: fleet, size: desktop);
      await goTo(tester, c, '/fleet', role: UserRole.fleetManager);
      expect(find.text('Your assigned machines'), findsOneWidget);
      expect(find.byKey(const Key('kpi-machines')), findsOneWidget);
      expect(find.text('No operator for ER-2611-BK9'), findsOneWidget);
      expect(find.text('Imran Shaikh cannot take jobs'), findsOneWidget);
      expect(find.byKey(const Key('fleet-fl-1')), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('new-fleet')));
      await tester.enterText(find.byType(TextField).last, 'Excavators');
      await tapOn(tester, find.text('Create'));
      expect(fleet.calls, contains('create-fleet Excavators'));
    });

    testWidgets(
      'equipment board shows state, filters by fleet, opens edit and maintenance',
      (tester) async {
        final c = await pumpApp(tester, size: desktop);
        await goTo(tester, c, '/fleet/equipment', role: UserRole.fleetManager);
        expect(find.text('On a job'), findsOneWidget);
        expect(find.text('In maintenance'), findsOneWidget);
        expect(
          find.textContaining('Now: ER-2611-BK1 · Suresh Kale'),
          findsOneWidget,
        );
        await tapOn(tester, find.byKey(const Key('fleet-filter-fl-1')));
        expect(
          find.byKey(const Key('machine-eq-3')),
          findsNothing,
          reason: 'filtered to the fleet',
        );
        await tapOn(tester, find.text('Maintenance').first);
        expect(find.byType(Scaffold), findsWidgets);
      },
    );

    testWidgets(
      'assignments: assign an operator; restricted operators cannot be picked',
      (tester) async {
        final fleet = FakeFleetApi();
        final c = await pumpApp(tester, fleet: fleet, size: desktop);
        await goTo(
          tester,
          c,
          '/fleet/assignments',
          role: UserRole.fleetManager,
        );
        expect(find.text('Needs an operator'), findsOneWidget);
        await tapOn(tester, find.byKey(const Key('assign-bk-9')));
        expect(find.text('Restricted'), findsOneWidget);
        await tapOn(tester, find.byKey(const Key('pick-operator-op-2')));
        expect(
          fleet.calls.where((x) => x.startsWith('assign')),
          isEmpty,
          reason: 'restricted operator is not selectable',
        );
        await tapOn(tester, find.byKey(const Key('pick-operator-op-1')));
        expect(fleet.calls, contains('assign bk-9 op-1'));
        expect(find.text('Needs an operator'), findsNothing);
        expect(
          find.byKey(const Key('assign-bk-9')),
          findsOneWidget,
          reason: 'reassign stays possible',
        );
      },
    );

    testWidgets('operators: roster badges and invite', (tester) async {
      final fleet = FakeFleetApi();
      final c = await pumpApp(tester, fleet: fleet);
      await goTo(tester, c, '/fleet/operators', role: UserRole.fleetManager);
      expect(find.text('Verified'), findsOneWidget);
      expect(find.text('Restricted'), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('invite-operator')));
      await typeIn(tester, 'invite-name', 'Vijay More');
      await typeIn(tester, 'invite-phone', '98765');
      await tapOn(tester, find.byKey(const Key('invite-submit')));
      expect(
        fleet.calls.where((x) => x.startsWith('invite')),
        isEmpty,
        reason: 'bad phone',
      );
      await typeIn(tester, 'invite-phone', '+919876543210');
      await tapOn(tester, find.byKey(const Key('invite-submit')));
      expect(fleet.calls, contains('invite Vijay More +919876543210'));
    });

    testWidgets('reports: earnings and utilisation per machine, locations', (
      tester,
    ) async {
      final c = await pumpApp(tester, size: desktop);
      await goTo(tester, c, '/fleet/reports', role: UserRole.fleetManager);
      expect(find.byKey(const Key('report-earnings')), findsOneWidget);
      expect(find.byKey(const Key('util-eq-1')), findsOneWidget);
      expect(find.byKey(const Key('location-eq-1')), findsOneWidget);
    });

    testWidgets(
      'fleet detail: managers add machines; only owners manage managers',
      (tester) async {
        final fleet = FakeFleetApi();
        final c = await pumpApp(tester, fleet: fleet, size: desktop);
        await goTo(
          tester,
          c,
          '/fleet/fleets/fl-1',
          role: UserRole.fleetManager,
        );
        expect(find.byKey(const Key('add-manager')), findsNothing);
        await tapOn(tester, find.byKey(const Key('add-machines')));
        await tapOn(tester, find.byKey(const Key('pick-machine-eq-3')));
        await tapOn(tester, find.byKey(const Key('pick-machines-done')));
        expect(fleet.calls, contains('add-machines fl-1 eq-3'));
        await tapOn(tester, find.byKey(const Key('remove-machine-eq-2')));
        expect(fleet.calls, contains('remove-machine fl-1 eq-2'));
      },
    );
  });


  testWidgets('admin operations mutation route is not exposed', (tester) async {
    final c = await pumpApp(tester, size: desktop);
    await goTo(tester, c, '/admin/operations', role: UserRole.admin);
    expect(find.text('Page not found'), findsOneWidget);
  });
}
