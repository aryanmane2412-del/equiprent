import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/operator/data/operator_api.dart';
import 'package:equiprent_app/features/operator/presentation/operator_pages.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_operations_api.dart';
import 'test_helpers.dart';

/// Two open jobs: the first is the active dispatch, the second is what comes next.
class _TwoJobsApi extends FakeOperatorApi {
  @override
  Future<List<JobSummary>> jobs({required String scope}) async {
    final first = await super.jobs(scope: scope);
    if (first.isEmpty) return first;
    final j = first.first;
    final next = JobSummary(
      bookingId: 'bk-next',
      reference: 'EQ-NEXT-2',
      assignmentStatus: 'accepted',
      bookingStatus: 'confirmed',
      start: j.start.add(const Duration(days: 2)),
      end: j.end.add(const Duration(days: 2)),
      equipmentTitle: 'JLG 600S Boom Lift',
      site: 'Downtown Lot 4, Pune',
      customerName: 'Sunrise Builders',
    );
    return [j, next];
  }
}

void main() {
  for (final (name, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'driver Today follows the design: active dispatch, safety check, up next ($name)',
      (tester) async {
        final api = _TwoJobsApi();
        final c = await pumpApp(tester, operator: api, size: size);
        await goTo(tester, c, '/operator', role: UserRole.operator);

        final first = (await api.jobs(scope: 'current')).first;
        expect(find.byKey(Key('job-${first.bookingId}')), findsOneWidget);
        expect(find.textContaining('ACTIVE DISPATCH'), findsOneWidget);
        expect(find.textContaining(first.reference), findsWidgets);
        expect(find.text(first.equipmentTitle), findsWidgets);
        expect(find.text('DROP-OFF'), findsOneWidget);
        expect(find.text('Open GPS'), findsOneWidget);
        expect(find.text('Pre-trip safety check'), findsOneWidget);
        expect(find.text('Open safety checklist'), findsOneWidget);

        // The second job is listed after the active one.
        expect(find.byKey(const Key('up-next-bk-next')), findsOneWidget);
        expect(find.text('JLG 600S Boom Lift'), findsOneWidget);
      },
    );
  }

  testWidgets(
    'the active dispatch buttons lead to the GPS route, the job and the safety checklist',
    (tester) async {
      final api = _TwoJobsApi();
      final c = await pumpApp(tester, operator: api, size: desktop);
      final id = (await api.jobs(scope: 'current')).first.bookingId;

      await goTo(tester, c, '/operator', role: UserRole.operator);
      await tapOn(tester, find.byKey(Key('active-gps-$id')));
      expect(
        find.byType(OperatorNavigationPage),
        findsOneWidget,
        reason: 'Open GPS shows the route page',
      );

      await goTo(tester, c, '/operator', role: UserRole.operator);
      await tapOn(tester, find.byKey(Key('safety-open-$id')));
      expect(
        find.byType(OperatorChecklistPage),
        findsOneWidget,
        reason: 'the safety check opens the handover checklist',
      );

      await goTo(tester, c, '/operator', role: UserRole.operator);
      await tapOn(tester, find.byKey(Key('active-open-$id')));
      expect(
        find.byType(OperatorJobPage),
        findsOneWidget,
        reason: 'Open job shows the job',
      );

      await goTo(tester, c, '/operator', role: UserRole.operator);
      await tapOn(tester, find.byKey(Key('job-$id')));
      expect(
        find.byType(OperatorJobPage),
        findsOneWidget,
        reason: 'tapping the card itself opens the job',
      );
    },
  );

  testWidgets('with no jobs the driver still sees the friendly empty state', (
    tester,
  ) async {
    final api = FakeOperatorApi()..assignmentStatus = 'declined';
    final c = await pumpApp(tester, operator: api, size: phone);
    await goTo(tester, c, '/operator', role: UserRole.operator);
    expect(find.text('No open jobs'), findsOneWidget);
    expect(find.textContaining('ACTIVE DISPATCH'), findsNothing);
  });
}
