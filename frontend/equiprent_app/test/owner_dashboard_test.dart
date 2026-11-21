import 'package:equiprent_app/core/session/user_role.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_comms_api.dart';
import 'fake_listings_api.dart';
import 'test_helpers.dart';

void main() {
  for (final (name, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'owner dashboard follows the design: stats, requests, inventory ($name)',
      (tester) async {
        final c = await pumpApp(tester, size: size);
        await goTo(tester, c, '/owner', role: UserRole.owner);

        // Header actions: a full-width button on phones, in the app bar on wide screens.
        expect(find.byKey(const Key('add-equipment')), findsOneWidget);
        // The three headline numbers.
        expect(find.byKey(const Key('stat-revenue')), findsOneWidget);
        expect(find.byKey(const Key('stat-fleet')), findsOneWidget);
        expect(find.byKey(const Key('stat-payout')), findsOneWidget);
        expect(find.text('Ready for payout'), findsOneWidget);
        // The old floating button is gone.
        expect(find.byType(FloatingActionButton), findsNothing);

        // Requests and inventory sections.
        expect(find.textContaining('booking requests'), findsWidgets);
        expect(find.text('Fleet inventory'), findsOneWidget);
      },
    );
  }

  testWidgets(
    'the fleet inventory shows the owner\'s real listings, never demo machines',
    (tester) async {
      final c = await pumpApp(
        tester,
        size: desktop,
        listings: FakeListingsApi(),
      );
      await goTo(tester, c, '/owner', role: UserRole.owner);
      await tester.pumpAndSettle();

      // The two listings the fake server holds for this owner.
      expect(find.byKey(const Key('inventory-l-draft')), findsOneWidget);
      expect(find.byKey(const Key('inventory-l-review')), findsOneWidget);
      expect(find.text('Tata Hitachi EX 200 excavator'), findsOneWidget);
      // Other owners' machines are not shown.
      expect(find.text('JCB 3DX backhoe loader'), findsNothing);
      expect(find.text('Escorts 15 t pick-and-carry crane'), findsNothing);
    },
  );

  testWidgets('a new request can be approved right from the dashboard', (
    tester,
  ) async {
    final comms = FakeCommsApi();
    final c = await pumpApp(tester, size: desktop, comms: comms);
    await goTo(tester, c, '/owner', role: UserRole.owner);
    await tester.pumpAndSettle();

    expect(find.byKey(const Key('request-bk-req')), findsOneWidget);
    expect(find.text('Needs approval'), findsOneWidget);
    await tapOn(tester, find.byKey(const Key('approve-bk-req')));
    expect(comms.calls, contains('accept bk-req'));
  });

  testWidgets('declining asks for a reason first, then tells the customer', (
    tester,
  ) async {
    final comms = FakeCommsApi();
    final c = await pumpApp(tester, size: desktop, comms: comms);
    await goTo(tester, c, '/owner', role: UserRole.owner);
    await tester.pumpAndSettle();

    await tapOn(tester, find.byKey(const Key('decline-bk-req')));
    expect(find.text('Decline this request?'), findsOneWidget);
    expect(
      comms.calls.where((x) => x.startsWith('reject')),
      isEmpty,
      reason: 'nothing is sent before a reason is given',
    );

    await tester.enterText(
      find.byType(TextField).last,
      'Machine is in service those days',
    );
    await tester.pumpAndSettle();
    await tapOn(tester, find.text('Decline').last);
    expect(
      comms.calls,
      contains('reject bk-req Machine is in service those days'),
    );
  });
}
