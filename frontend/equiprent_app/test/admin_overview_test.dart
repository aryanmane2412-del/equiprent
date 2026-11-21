import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/admin/presentation/admin_read_only_dashboard.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'test_helpers.dart';

const _stats = AdminStats(
  gmv30dPaise: 9420000,
  bookingsInProgress: 142,
  bookingsRequested: 7,
  openDisputes: 2,
);

final _disputes = [
  AdminDisputeRow(
    id: 'd-1',
    category: 'Late return fee',
    summary: 'Late return fee dispute',
    status: 'open',
    claimedPaise: 24000,
    openedAt: DateTime(2026, 11, 20),
  ),
];

void main() {
  for (final (name, size) in [('phone', phone), ('desktop', desktop)]) {
    testWidgets(
      'admin overview shows the design\'s headline cards and queues ($name)',
      (tester) async {
        final c = await pumpApp(
          tester,
          size: size,
          adminStats: _stats,
          adminDisputes: _disputes,
        );
        await goTo(tester, c, '/admin', role: UserRole.admin);
        await tester.pumpAndSettle();

        // The page keeps its read-only identity.
        expect(find.text('Read-only platform monitor'), findsOneWidget);
        expect(find.text('Monitoring access only'), findsOneWidget);

        // Four headline cards with real numbers.
        for (final k in ['volume', 'rentals', 'approvals', 'disputes']) {
          expect(find.byKey(Key('admin-kpi-$k')), findsOneWidget, reason: k);
        }
        expect(find.text('142'), findsOneWidget);
        expect(find.text('7 requests waiting'), findsOneWidget);
        expect(find.text('Needs review'), findsOneWidget);

        // The disputes list.
        expect(find.text('Active disputes'), findsOneWidget);
        expect(find.byKey(const Key('dispute-d-1')), findsOneWidget);
        expect(find.textContaining('Late return fee'), findsWidgets);
      },
    );
  }

  testWidgets('the admin overview has no mutation controls', (tester) async {
    final c = await pumpApp(
      tester,
      size: desktop,
      adminStats: _stats,
      adminDisputes: _disputes,
    );
    await goTo(tester, c, '/admin', role: UserRole.admin);
    await tester.pumpAndSettle();

    // The design's action buttons that would change data are deliberately not offered.
    for (final label in [
      'Resolve Dispute',
      'Verify Owner',
      'Approve',
      'Reject',
      'Suspend',
      'Delete',
    ]) {
      expect(
        find.text(label),
        findsNothing,
        reason: '"$label" would be a mutation',
      );
    }
  });
}
