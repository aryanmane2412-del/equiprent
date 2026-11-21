import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/core/widgets/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'test_helpers.dart';

void main() {
  vehicleCountTests();
  // The customer home shows the app logo where the time-of-day greeting used to be.
  for (final (name, size) in [
    ('phone', phone),
    ('tablet', tablet),
    ('desktop', desktop),
  ]) {
    testWidgets(
      'customer home: no greeting, exactly one logo in the header ($name)',
      (tester) async {
        final c = await pumpApp(tester, size: size);
        await goTo(tester, c, '/customer', role: UserRole.customer);

        for (final greeting in [
          'Good morning',
          'Good afternoon',
          'Good evening',
        ]) {
          expect(find.textContaining(greeting), findsNothing, reason: greeting);
        }
        final header = find.descendant(
          of: find.byType(AppBar),
          matching: find.byType(BrandLogo),
        );
        expect(
          header,
          findsOneWidget,
          reason: 'one logo in the header, not two',
        );
      },
    );
  }

  testWidgets('other pages keep their text title and the small phone logo', (
    tester,
  ) async {
    final c = await pumpApp(tester, size: phone);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    final header = find.descendant(
      of: find.byType(AppBar),
      matching: find.byType(BrandLogo),
    );
    expect(
      header,
      findsOneWidget,
      reason: 'phone top-level pages still show the logo on the left',
    );
  });
}

// The design shows how many vehicles the list holds.
void vehicleCountTests() {
  testWidgets('the customer home says how many vehicles it is showing', (
    tester,
  ) async {
    final c = await pumpApp(tester, size: phone);
    await goTo(tester, c, '/customer', role: UserRole.customer);
    await tester.pumpAndSettle();
    expect(find.textContaining('Showing '), findsOneWidget);
    expect(find.textContaining(' vehicles'), findsOneWidget);
  });
}
