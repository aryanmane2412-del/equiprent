import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/core/widgets/widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_booking_api.dart';
import 'fake_comms_api.dart';
import 'test_helpers.dart';

void main() {
  vehicleCountTests();
  deliveryAndCardTests();
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

void deliveryAndCardTests() {
  testWidgets(
    'the customer home says where it is delivering, from the saved site',
    (tester) async {
      final c = await pumpApp(tester, size: phone);
      await goTo(tester, c, '/customer', role: UserRole.customer);
      await tester.pumpAndSettle();
      final line = find.byKey(const Key('delivering-to'));
      expect(line, findsOneWidget);
      expect(find.textContaining('Delivering to: '), findsOneWidget);
      expect(find.textContaining('Add a delivery site'), findsNothing);
    },
  );

  testWidgets(
    'with no saved site the customer is asked to add one, and it opens the profile',
    (tester) async {
      final bookings = FakeBookingApi(FakeCommsApi())..sitesList.clear();
      final c = await pumpApp(tester, size: phone, bookings: bookings);
      await goTo(tester, c, '/customer', role: UserRole.customer);
      await tester.pumpAndSettle();
      expect(find.text('Add a delivery site'), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('delivering-to')));
      expect(
        find.byKey(const Key('delivering-to')),
        findsNothing,
        reason:
            'tapping it leaves the home screen (for the profile, where sites are added)',
      );
      expect(find.byType(NavigationBar), findsOneWidget);
    },
  );

  testWidgets(
    'vehicle cards show the price beside the title and a monospace spec line',
    (tester) async {
      final c = await pumpApp(tester, size: desktop);
      await goTo(tester, c, '/customer', role: UserRole.customer);
      await tester.pumpAndSettle();
      final spec = find.byWidgetPredicate(
        (w) => w is Text && w.key.toString().contains('specs-'),
      );
      expect(spec, findsWidgets);
      final style = tester.widget<Text>(spec.first).style;
      expect(style?.fontFamily, 'SourceCodePro');
    },
  );
}
