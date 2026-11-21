import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/operator/data/operator_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_operations_api.dart';
import 'test_helpers.dart';

/// Counts how often the driver's screens ask the server for data.
class _CountingOperatorApi extends FakeOperatorApi {
  int jobsReads = 0;
  int scheduleReads = 0;
  int paymentReads = 0;
  int jobReads = 0;

  @override
  Future<List<JobSummary>> jobs({required String scope}) {
    jobsReads++;
    return super.jobs(scope: scope);
  }

  @override
  Future<List<JobSummary>> schedule(DateTime from, DateTime to) {
    scheduleReads++;
    return super.schedule(from, to);
  }

  @override
  Future<DriverPaymentHistory> payments() {
    paymentReads++;
    return super.payments();
  }

  @override
  Future<JobDetail> job(String bookingId) {
    jobReads++;
    return super.job(bookingId);
  }
}

void main() {
  const refresh = Key('operator-refresh');

  group('driver refresh button', () {
    testWidgets('Today reloads the jobs and the schedule', (tester) async {
      final api = _CountingOperatorApi();
      final c = await pumpApp(tester, operator: api, size: desktop);
      await goTo(tester, c, '/operator', role: UserRole.operator);
      expect(find.byKey(refresh), findsOneWidget);
      expect(find.byTooltip('Refresh'), findsOneWidget);
      final jobs = api.jobsReads, schedule = api.scheduleReads;
      await tapOn(tester, find.byKey(refresh));
      expect(api.jobsReads, greaterThan(jobs), reason: 'jobs were fetched again');
      expect(api.scheduleReads, greaterThan(schedule), reason: 'the schedule was fetched again');
    });

    testWidgets('Assignments and History reload their list', (tester) async {
      final api = _CountingOperatorApi();
      final c = await pumpApp(tester, operator: api, size: desktop);
      for (final path in ['/operator/assignments', '/operator/history']) {
        await goTo(tester, c, path, role: UserRole.operator);
        expect(find.byKey(refresh), findsOneWidget, reason: path);
        final before = api.jobsReads;
        await tapOn(tester, find.byKey(refresh));
        expect(api.jobsReads, greaterThan(before), reason: '$path refetched');
      }
    });

    testWidgets('Payments reloads the payment history', (tester) async {
      final api = _CountingOperatorApi();
      final c = await pumpApp(tester, operator: api, size: desktop);
      await goTo(tester, c, '/operator/payments', role: UserRole.operator);
      expect(find.byKey(refresh), findsOneWidget);
      final before = api.paymentReads;
      await tapOn(tester, find.byKey(refresh));
      expect(api.paymentReads, greaterThan(before));
    });

    testWidgets('a job and its navigation page reload that job', (tester) async {
      final api = _CountingOperatorApi();
      final c = await pumpApp(tester, operator: api, size: desktop);
      for (final path in ['/operator/jobs/bk-op', '/operator/jobs/bk-op/navigate']) {
        await goTo(tester, c, path, role: UserRole.operator);
        expect(find.byKey(refresh), findsOneWidget, reason: path);
        final before = api.jobReads;
        await tapOn(tester, find.byKey(refresh));
        expect(api.jobReads, greaterThan(before), reason: '$path refetched the job');
      }
    });

    testWidgets('the button is not shown to other roles', (tester) async {
      final c = await pumpApp(tester, operator: _CountingOperatorApi(), size: desktop);
      await goTo(tester, c, '/customer', role: UserRole.customer);
      expect(find.byKey(refresh), findsNothing);
    });
  });
}
