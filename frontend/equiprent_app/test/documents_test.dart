import 'package:equiprent_app/core/session/user_role.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_booking_api.dart';
import 'fake_comms_api.dart';
import 'test_helpers.dart';

void main() {
  testWidgets(
    'agreement: acceptance (not a signature), server consent text, version and terms review',
    (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(
        tester,
        c,
        '/customer/bookings/bk-1/agreement',
        role: UserRole.customer,
      );

      expect(find.text('Accept for your side'), findsOneWidget);
      expect(find.textContaining('not a digital signature'), findsWidgets);
      expect(
        find.text(FakeCommsApi.consent),
        findsOneWidget,
        reason: 'the checkbox shows the consent text the server stores',
      );
      expect(find.text('Version 1'), findsOneWidget);
      expect(find.text('Terms not yet legally reviewed'), findsOneWidget);
      expect(
        find.byKey(const Key('agreement-versions')),
        findsNothing,
        reason: 'one version only',
      );

      await tapOn(tester, find.byKey(const Key('sign-consent')));
      await tapOn(tester, find.byKey(const Key('sign-agreement')));
      expect(comms.calls.where((x) => x.startsWith('sign bk-1')), hasLength(1));
      expect(find.text('Accepted'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('document-history')));
      expect(find.text('Document history'), findsOneWidget);
      expect(find.text('Acceptance recorded for the customer'), findsOneWidget);
      expect(find.text('Agreement version 1 issued'), findsOneWidget);
    },
  );

  testWidgets(
    'agreement versions: superseded versions keep their text and acceptance records',
    (tester) async {
      final comms = FakeCommsApi()..agreementVersionNo = 2;
      final opened = <String>[];
      final c = await pumpApp(tester, comms: comms, opened: opened);
      await goTo(
        tester,
        c,
        '/customer/bookings/bk-1/agreement',
        role: UserRole.customer,
      );
      expect(find.text('Version 2'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('agreement-versions')));
      expect(find.text('Agreement versions'), findsOneWidget);
      expect(find.byKey(const Key('version-2')), findsOneWidget);
      expect(find.text('Superseded'), findsOneWidget);
      expect(find.textContaining('re-issued'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('version-1')));
      expect(find.text('Agreement v1'), findsOneWidget);
      expect(find.text('Superseded by a newer version'), findsOneWidget);
      expect(find.text('Acceptance records'), findsOneWidget);
      expect(find.textContaining('Acceptance record ·'), findsOneWidget);
      expect(
        tester
            .widget<SelectableText>(find.byKey(const Key('version-terms')))
            .data,
        contains('1. PARTIES'),
      );

      await tapOn(tester, find.byKey(const Key('version-pdf')));
      expect(opened, ['https://files.test/agreement-bk-1-v1.pdf']);
    },
  );

  testWidgets(
    'invoice detail: parties, lines with GST, payments, PDF and a credit note',
    (tester) async {
      final comms = FakeCommsApi()..statusOf['bk-2'] = 'completed';
      final api = FakeBookingApi(comms);
      await api.issueInvoice(null, 'bk-2');
      final opened = <String>[];
      final c = await pumpApp(
        tester,
        bookings: api,
        opened: opened,
        size: desktop,
      );
      await goTo(tester, c, '/owner/invoices/inv-1', role: UserRole.owner);

      expect(find.byKey(const Key('invoice-number')), findsOneWidget);
      expect(find.text('ER/2627/00001'), findsWidgets);
      expect(find.text('Patil Earthmovers Pvt Ltd'), findsOneWidget);
      expect(find.text('GSTIN 27ABCDE1234F1Z5'), findsOneWidget);
      expect(find.text('JCB 3DX backhoe loader'), findsOneWidget);
      expect(find.textContaining('SAC 997313'), findsOneWidget);
      expect(find.textContaining('GST 18%'), findsOneWidget);
      expect(find.text('CGST'), findsOneWidget);
      expect(find.textContaining('RCPT-2026-0000001'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('invoice-download')));
      expect(opened, ['https://files.test/inv-1.pdf']);

      await tapOn(tester, find.byKey(const Key('credit-note')));
      await tapOn(tester, find.byKey(const Key('credit-submit')));
      expect(
        api.calls.where((x) => x.startsWith('credit')),
        isEmpty,
        reason: 'amount and reason are required',
      );
      await tester.enterText(
        find.byKey(const Key('credit-amount')).hitTestable().first,
        '1000',
      );
      await tester.enterText(
        find.byKey(const Key('credit-reason')).hitTestable().first,
        'Machine idle for half a day',
      );
      await tapOn(tester, find.byKey(const Key('credit-submit')));
      expect(
        api.calls,
        contains('credit inv-1 100000 Machine idle for half a day'),
      );
      expect(
        find.text('Credit note'),
        findsWidgets,
        reason: 'the new credit note opens',
      );
      expect(find.textContaining('Against invoice'), findsOneWidget);
      expect(
        find.byKey(const Key('credit-note')),
        findsNothing,
        reason: 'no credit notes against a credit note',
      );
    },
  );

  testWidgets(
    'customers open invoices from Payments & invoices but cannot issue credit notes',
    (tester) async {
      final comms = FakeCommsApi()..statusOf['bk-2'] = 'completed';
      final api = FakeBookingApi(comms);
      await api.issueInvoice(null, 'bk-2');
      final c = await pumpApp(tester, bookings: api);
      await goTo(tester, c, '/customer/payments', role: UserRole.customer);
      await tapOn(tester, find.text('ER/2627/00001'));
      expect(api.calls, contains('invoice-detail inv-1'));
      expect(find.text('Charges'), findsOneWidget);
      expect(find.byKey(const Key('credit-note')), findsNothing);
    },
  );

  testWidgets('admin compliance mutation route is not exposed', (tester) async {
    final c = await pumpApp(tester, size: desktop);
    await goTo(tester, c, '/admin/compliance', role: UserRole.admin);
    expect(find.text('Page not found'), findsOneWidget);
  });
}
