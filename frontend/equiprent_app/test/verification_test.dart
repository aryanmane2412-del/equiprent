import 'package:equiprent_app/core/session/user_role.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_verification_api.dart';
import 'test_helpers.dart';

Finder field(String key) => find.descendant(
  of: find.byKey(ValueKey(key)),
  matching: find.byType(EditableText),
);

Future<void> type(WidgetTester tester, String key, String text) async {
  await tester.ensureVisible(field(key));
  await tester.enterText(field(key), text);
  await tester.pump();
}

Future<void> tapKey(WidgetTester tester, String key) =>
    tapOn(tester, find.byKey(ValueKey(key)));

Future<void> fillBusiness(
  WidgetTester tester, {
  String pan = 'ABCDE1234F',
}) async {
  await type(tester, 'legal-name', 'Patil Earthmovers');
  await type(tester, 'business-name', 'Patil Earthmovers');
  await type(tester, 'pan', pan);
  await type(tester, 'contact-name', 'Ravi Patil');
  await type(tester, 'contact-phone', '+919800000000');
  await type(tester, 'contact-email', 'ravi@example.com');
  await type(tester, 'address-line1', 'Plot 7, MIDC Bhosari');
  await type(tester, 'address-city', 'Pune');
  await type(tester, 'address-pin', '411026');
}

/// Lets success toasts expire (they cover the bottom of a phone screen).
Future<void> clearToasts(WidgetTester tester) async {
  await tester.pump(const Duration(seconds: 5));
  await tester.pumpAndSettle();
}

bool submitEnabled(WidgetTester tester) {
  final btn = tester.widget<TextButton>(
    find.descendant(
      of: find.byKey(const ValueKey("submit-verification")),
      matching: find.byType(TextButton),
    ),
  );
  return btn.onPressed != null;
}

void main() {
  testWidgets('owner completes verification on a phone, then details lock', (
    tester,
  ) async {
    final api = FakeVerificationApi();
    final picker = FakePicker();
    final c = await pumpApp(tester, verification: api, picker: picker);
    await goTo(tester, c, '/owner/verification', role: UserRole.owner);

    expect(find.text('Complete your business verification'), findsOneWidget);
    expect(
      api.orgsSeen.last,
      'org-owner',
      reason: 'owner calls carry the owner organisation',
    );
    expect(
      submitEnabled(tester),
      isFalse,
      reason: 'cannot submit an incomplete application',
    );

    // Business details: client-side validation first.
    await tapKey(tester, 'edit-business');
    await fillBusiness(tester, pan: '1234');
    await tapKey(tester, 'save-business');
    expect(find.text('Enter a valid PAN (ABCDE1234F)'), findsOneWidget);
    expect(api.calls.where((c) => c.startsWith('saveBusiness')), isEmpty);
    await type(tester, 'pan', 'abcde1234f');
    await tapKey(tester, 'save-business');
    expect(api.calls, contains('saveBusiness:ABCDE1234F'));
    expect(
      find.text('••••••234F'),
      findsOneWidget,
      reason: 'only the last 4 of the PAN are shown',
    );

    // Identity document through the private upload flow.
    await tapKey(tester, 'add-identity-doc');
    await tapKey(tester, 'pick-file');
    await tapKey(tester, 'add-document');
    expect(
      api.calls,
      containsAllInOrder([
        'upload:identityDocument:pan.pdf',
        'addOwnerDocument:pan',
      ]),
    );

    // Payout account: the provider refuses an account, then accepts another.
    Future<void> bankForm(String number) async {
      await tapKey(tester, 'set-bank');
      await type(tester, 'bank-holder', 'Patil Earthmovers');
      await type(tester, 'bank-number', number);
      await type(tester, 'bank-confirm', number);
      await type(tester, 'bank-ifsc', 'hdfc0001234');
      await tapKey(tester, 'save-bank');
    }

    await bankForm('50100123450000');
    expect(find.textContaining('could not verify'), findsOneWidget);
    await bankForm('50100123456789');
    expect(find.text('•••• 6789'), findsOneWidget);
    expect(find.textContaining('50100123456789'), findsNothing);

    expect(submitEnabled(tester), isTrue);
    await clearToasts(tester);
    await tapKey(tester, 'submit-verification');
    expect(api.calls.last, 'submitOwner');
    expect(find.text('Submitted for review'), findsWidgets);
    expect(
      find.byKey(const ValueKey('edit-business')),
      findsOneWidget,
      reason: 'Edit stays available; it reopens the business for re-verification',
    );
    expect(find.byKey(const ValueKey('submit-verification')), findsNothing);
  });

  testWidgets(
    'owner sees the rejection reason and can view documents through signed links',
    (tester) async {
      final api = FakeVerificationApi()
        ..ownerStatus = 'rejected'
        ..ownerRejection = 'Upload a clear PAN card'
        ..ownerDocs.add({
          'id': 'doc-9',
          'doc_type': 'pan',
          'status': 'rejected',
          'review_note': 'Blurred',
          'content_type': 'application/pdf',
        });
      final opened = <String>[];
      final c = await pumpApp(
        tester,
        size: tablet,
        verification: api,
        opened: opened,
      );
      await goTo(tester, c, '/owner/verification', role: UserRole.owner);

      expect(find.text('Changes needed'), findsOneWidget);
      expect(find.text('Upload a clear PAN card'), findsOneWidget);
      expect(
        find.byKey(const ValueKey('edit-business')),
        findsOneWidget,
        reason: 'editable again after rejection',
      );
      await tapOn(tester, find.byTooltip('View document'));
      expect(
        api.calls,
        contains('documentLink:doc-9'),
        reason: 'a fresh signed link is requested each time',
      );
      expect(opened.single, contains('sig='));
      expect(
        find.byTooltip('Remove'),
        findsNothing,
        reason: 'reviewed documents stay in the history',
      );
    },
  );

  testWidgets('admin can open the driver verification queue', (tester) async {
    final c = await pumpApp(tester, size: desktop);
    await goTo(tester, c, '/admin/verifications', role: UserRole.admin);
    expect(find.text('Verifications'), findsOneWidget);
    expect(find.text('Driver applications'), findsOneWidget);
    expect(find.text('Approve'), findsNothing);
  });
  testWidgets('admin can open a driver verification detail', (tester) async {
    final c = await pumpApp(tester, size: desktop);
    await goTo(
      tester,
      c,
      '/admin/verifications/app-owner',
      role: UserRole.admin,
    );
    expect(find.text('Review application'), findsOneWidget);
    expect(find.byKey(const ValueKey('suspend-account')), findsNothing);
  });
  testWidgets('operator saves licence details and documents, then submits', (
    tester,
  ) async {
    final api = FakeVerificationApi();
    final c = await pumpApp(tester, verification: api);
    await goTo(tester, c, '/operator/verification', role: UserRole.operator);

    await tapKey(tester, 'edit-operator-profile');
    await type(tester, 'licence-number', 'mh1220150012345');
    await type(tester, 'licence-class', 'HTV');
    await type(tester, 'experience', '7');
    await tapKey(tester, 'save-operator-profile');
    expect(find.text('Pick a date'), findsOneWidget); // typed dates are gone: the calendar is the only way
    await tapKey(tester, 'licence-expiry');
    await tester.pumpAndSettle();
    await tester.tap(find.text('OK'));
    await tester.pumpAndSettle();
    await tapKey(tester, 'save-operator-profile');
    expect(find.text('Choose at least one machine'), findsOneWidget);
    await tapOn(tester, find.widgetWithText(FilterChip, 'Excavator'));
    await tapKey(tester, 'save-operator-profile');
    expect(api.calls, contains('saveOperatorProfile:MH1220150012345'));
    expect(find.text('••••2345 · HTV'), findsOneWidget);

    for (final type in ['driving_licence', 'pan']) {
      await tapKey(tester, 'add-operator-doc');
      await tapOn(tester, find.byType(DropdownButtonFormField<String>));
      await tapOn(
        tester,
        find.text(type == 'pan' ? 'PAN card' : 'Driving licence').last,
      );
      await tapKey(tester, 'pick-file');
      await tapKey(tester, 'add-document');
    }
    expect(
      api.calls,
      containsAllInOrder([
        'upload:operatorDocument:pan.pdf',
        'addOperatorDocument:driving_licence',
        'upload:identityDocument:pan.pdf',
        'addOperatorDocument:pan',
      ]),
    );

    await clearToasts(tester);
    await tapKey(tester, 'submit-verification');
    expect(api.calls.last, 'submitOperator');
    expect(find.byKey(const ValueKey('edit-operator-profile')), findsNothing);
  });

  testWidgets('customer adds a profile image and an address', (tester) async {
    final api = FakeVerificationApi();
    final picker = FakePicker();
    final c = await pumpApp(tester, verification: api, picker: picker);
    await goTo(tester, c, '/customer/verification', role: UserRole.customer);

    expect(find.text('Complete your profile'), findsOneWidget);
    await tapKey(tester, 'change-avatar');
    expect(
      picker.picks.single,
      isTrue,
      reason: 'only images are offered for avatars',
    );
    expect(
      api.calls,
      containsAllInOrder(['upload:avatar:me.png', 'setAvatar:upload-1']),
    );

    await tapKey(tester, 'add-address');
    await tapKey(tester, 'save-address');
    expect(find.text('Enter a 6-digit PIN code'), findsOneWidget);
    await tester.enterText(
      find.byType(EditableText).at(0),
      'Flat 3, Shanti Nagar',
    );
    await tester.enterText(find.byType(EditableText).at(2), 'Pune');
    await tester.enterText(find.byType(EditableText).at(3), '411001');
    await tapKey(tester, 'save-address');
    expect(api.calls.last, 'addAddress:org-customer:411001');
    expect(find.text('Profile complete'), findsOneWidget);
  });


  testWidgets('fleet manager sees the company and only authorised machines', (
    tester,
  ) async {
    final c = await pumpApp(tester);
    await goTo(tester, c, '/fleet', role: UserRole.fleetManager);
    await tapKey(tester, 'fleet-company');
    expect(find.text('Kale Infra Pvt Ltd'), findsOneWidget);
    expect(find.text('Excavator A'), findsOneWidget);
    expect(find.text('1 machines'), findsOneWidget);
  });

  for (final size in [phone, tablet, desktop]) {
    testWidgets('verification pages lay out at ${size.width.toInt()} px', (
      tester,
    ) async {
      final c = await pumpApp(tester, size: size);
      for (final (path, role) in [
        ('/owner/verification', UserRole.owner),
        ('/operator/verification', UserRole.operator),
        ('/customer/verification', UserRole.customer),
        ('/owner/team', UserRole.owner),
        ('/admin/verifications', UserRole.admin),
        ('/admin/verifications/app-owner', UserRole.admin),
      ]) {
        await goTo(tester, c, path, role: role);
        expect(tester.takeException(), isNull, reason: path);
      }
    });
  }
}
