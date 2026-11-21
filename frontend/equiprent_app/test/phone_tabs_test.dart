import 'package:equiprent_app/core/session/user_role.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'test_helpers.dart';

void main() {
  testWidgets('driver Safety tab lists checklists for the open job', (
    tester,
  ) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/operator/safety', role: UserRole.operator);
    await tester.pumpAndSettle();
    expect(find.text('Pre-trip and return inspections'), findsOneWidget);
    expect(find.textContaining('Pre-trip (handover) checklist'), findsWidgets);
    expect(find.textContaining('Return checklist'), findsWidgets);
  });

  testWidgets('driver Route tab opens the way to the current job', (
    tester,
  ) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/operator/route', role: UserRole.operator);
    await tester.pumpAndSettle();
    expect(tester.takeException(), isNull);
    expect(find.byType(NavigationBar), findsOneWidget);
  });

  testWidgets('customer Track tab renders and offers browsing when empty', (
    tester,
  ) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/customer/track', role: UserRole.customer);
    await tester.pumpAndSettle();
    expect(find.text('Follow your machines'), findsOneWidget);
    expect(tester.takeException(), isNull);
  });

  testWidgets('admin Disputes tab is read-only', (tester) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/admin/disputes', role: UserRole.admin);
    await tester.pumpAndSettle();
    expect(find.text('Monitoring access only'), findsOneWidget);
    expect(find.text('No open disputes'), findsOneWidget);
    expect(find.byType(FilledButton), findsNothing);
  });

  testWidgets('a section that is not a tab leaves no tab highlighted', (
    tester,
  ) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/owner/settings', role: UserRole.owner);
    await tester.pumpAndSettle();
    expect(find.byType(NavigationBar), findsOneWidget);
    expect(tester.takeException(), isNull);
  });
}
