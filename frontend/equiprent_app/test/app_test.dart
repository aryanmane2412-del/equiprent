import 'package:equiprent_app/app/router.dart';
import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/core/widgets/layout.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'test_helpers.dart';

/// Every screen of every role, plus detail routes.
final _routes = <(UserRole?, String)>[
  for (final m in roleModules)
    for (final d in m.destinations) (m.role, d.path),
  (UserRole.customer, '/customer/categories'),
  (UserRole.customer, '/customer/equipment/l-live'),
  (UserRole.customer, '/customer/equipment/l-crane'),
  (UserRole.customer, '/customer/book/l-live'),
  (UserRole.customer, '/customer/compare'),
  (UserRole.customer, '/customer/compare?ids=l-live,l-crane'),
  (UserRole.customer, '/customer/saved'),
  (UserRole.customer, '/customer/payments'),
  (UserRole.customer, '/customer/bookings/bk-1/track'),
  (UserRole.owner, '/owner/bookings/bk-1/track'),
  (UserRole.owner, '/owner/bookings/bk-1'),
  (UserRole.customer, '/customer/chat/conv-1'),
  (UserRole.customer, '/customer/bookings/bk-1'),
  (UserRole.customer, '/customer/bookings/bk-2'),
  (UserRole.customer, '/customer/bookings/bk-1/agreement'),
  (UserRole.customer, '/customer/notifications'),
  (UserRole.owner, '/owner/bookings/bk-req'),
  (UserRole.owner, '/owner/bookings/bk-1/agreement'),
  (UserRole.owner, '/owner/bookings/bk-1/agreement/versions'),
  (UserRole.customer, '/customer/bookings/bk-1/agreement/versions/1'),
  (UserRole.customer, '/customer/bookings/bk-1/documents'),
  (UserRole.owner, '/owner/invoices/inv-9'),
  (UserRole.customer, '/customer/invoices/inv-9'),
  (UserRole.owner, '/owner/messages'),
  (UserRole.owner, '/owner/messages/conv-1'),
  (UserRole.owner, '/owner/reviews'),
  (UserRole.owner, '/owner/notifications'),
  (UserRole.operator, '/operator/notifications'),
  (UserRole.operator, '/operator/messages'),
  (UserRole.fleetManager, '/fleet/notifications'),
  (UserRole.admin, '/admin/notifications'),
  (UserRole.customer, '/customer/search?category=crane'),
  (UserRole.owner, '/owner/listings/new'),
  (UserRole.owner, '/owner/listings/l-draft'),
  (UserRole.owner, '/owner/listings/l-review'),
  (UserRole.owner, '/owner/listings/l-draft/edit'),
  (UserRole.operator, '/operator/jobs/bk-op'),
  (UserRole.operator, '/operator/jobs/bk-op/navigate'),
  (UserRole.operator, '/operator/jobs/bk-op/checklist/handover'),
  (UserRole.operator, '/operator/jobs/bk-op/checklist/return'),
  (UserRole.fleetManager, '/fleet/fleets/fl-1'),
  (UserRole.fleetManager, '/fleet/maintenance'),
  (UserRole.owner, '/owner/fleets'),
  (UserRole.owner, '/owner/fleets/fl-1'),
  (null, '/design-system'),
];

void main() {
  testWidgets('role selection lists all five roles', (tester) async {
    await pumpApp(tester);
    expect(find.text('How will you use EQUIPRENT?'), findsOneWidget);
    for (final role in UserRole.values) {
      expect(
        find.byKey(ValueKey('role-${role.name}')),
        findsOneWidget,
        reason: role.label,
      );
    }
  });

  for (final (name, size) in const [
    ('phone', phone),
    ('tablet', tablet),
    ('desktop', desktop),
  ]) {
    testWidgets('every screen renders without layout errors on $name', (
      tester,
    ) async {
      final c = await pumpApp(tester, size: size);
      for (final (role, path) in _routes) {
        await goTo(tester, c, path, role: role);
        expect(tester.takeException(), isNull, reason: '$path on $name');
        expect(find.byType(Scaffold), findsWidgets, reason: path);
        // Sticky action bars must never swallow the page (regression guard).
        for (final bar in find.byType(BottomActionBar).evaluate()) {
          final h = (bar.renderObject! as RenderBox).size.height;
          expect(
            h,
            lessThan(size.height * 0.4),
            reason: 'bottom bar on $path ($name) is ${h}px tall',
          );
        }
      }
    });
  }

  testWidgets(
    'choosing a role opens its navigation; wide screens use the side nav',
    (tester) async {
      await pumpApp(tester, size: desktop);
      await tapOn(tester, find.byKey(const ValueKey('role-admin')));
      expect(find.text('Read-only platform monitor'), findsOneWidget);
      expect(find.byType(NavigationBar), findsNothing);
      expect(find.text('Switch role'), findsNothing);
      expect(find.text('Read-only platform monitor'), findsOneWidget);
    },
  );

  testWidgets('customer bottom tabs navigate to their own destinations', (
    tester,
  ) async {
    final c = await pumpApp(tester, size: phone);
    await goTo(tester, c, '/customer', role: UserRole.customer);

    for (final (label, path, pageMarker) in const [
      ('Home', '/customer', 'Recently listed'),
      ('Search', '/customer/search', 'Find equipment'),
      ('Bookings', '/customer/bookings', 'My bookings'),
      ('Chat', '/customer/chat', 'Messages'),
      ('Profile', '/customer/profile', 'Profile & verification'),
    ]) {
      await tapOn(tester, find.text(label));
      expect(
        c.read(routerProvider).routeInformationProvider.value.uri.path,
        path,
        reason: '$label tab should open $path',
      );
      expect(
        find.text(pageMarker),
        findsOneWidget,
        reason: '$label should show its own page',
      );
    }
  });

  testWidgets('phones collapse extra sections into a More sheet', (
    tester,
  ) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/owner', role: UserRole.owner);
    expect(find.byType(NavigationBar), findsOneWidget);
    await tapOn(tester, find.text('More'));
    await tapOn(tester, find.text('Maintenance').last);
    expect(find.text('Scheduled & in progress'), findsOneWidget);
  });

  testWidgets('admin finance screens expose no mutation controls', (
    tester,
  ) async {
    final c = await pumpApp(tester, size: desktop);
    await goTo(tester, c, '/admin/payments', role: UserRole.admin);
    expect(find.text('Issue refund'), findsNothing);
    expect(find.text('Release all ready'), findsNothing);
    await goTo(tester, c, '/admin/settlements', role: UserRole.admin);
    expect(find.text('Release all ready'), findsNothing);
  });

  testWidgets('unavailable sharing action is disabled', (tester) async {
    final c = await pumpApp(tester);
    await goTo(
      tester,
      c,
      '/customer/equipment/l-live',
      role: UserRole.customer,
    );
    final share = tester.widget<IconButton>(
      find.ancestor(
        of: find.byTooltip('Sharing unavailable'),
        matching: find.byType(IconButton),
      ),
    );
    expect(share.onPressed, isNull);
  });
}
