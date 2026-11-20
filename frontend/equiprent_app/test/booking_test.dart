import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/core/widgets/buttons.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_booking_api.dart';
import 'fake_comms_api.dart';
import 'test_helpers.dart';

/// The booking screens against a fake that moves bookings through the same
/// lifecycle as the backend (see fake_booking_api.dart). Each role sees only
/// the actions the server offers it.
void main() {
  Future<void> as(WidgetTester tester, dynamic c, UserRole role, String path) => goTo(tester, c, path, role: role);

  Future<void> act(WidgetTester tester, String action) => tapOn(tester, find.byKey(Key('action-$action')));

  Future<void> confirm(WidgetTester tester) => tapOn(tester, find.byKey(const Key('confirm-action')));

  Future<void> giveReason(WidgetTester tester, String reason) async {
    await tester.enterText(find.byKey(const Key('reason-field')), reason);
    await tester.pump();
    await tapOn(tester, find.byKey(const Key('reason-confirm')));
  }

  group('request flow', () {
    testWidgets('customer picks dates, site and hire options, sees the server estimate and sends the request', (tester) async {
      final comms = FakeCommsApi();
      final api = FakeBookingApi(comms);
      final c = await pumpApp(tester, size: desktop, bookings: api);
      await as(tester, c, UserRole.customer, '/customer/equipment/l-live');
      await tapOn(tester, find.byKey(const Key('request-booking')));

      expect(find.text('Book equipment'), findsOneWidget);
      expect(find.text('Comes with the owner\'s operator.'), findsOneWidget, reason: 'l-live is wet hire only');
      expect(find.text('Choose a work site'), findsOneWidget);
      final submit = find.byKey(const Key('submit-booking'));
      expect(tester.widget<AppButton>(submit).onPressed, isNull, reason: 'no site chosen yet');

      await tapOn(tester, find.byKey(const Key('site-site-1')));
      // 3 days x 15,000 + 3,000 delivery + 3 x 800 operator, + 18% GST
      expect(find.text('₹59,472'), findsWidgets);
      expect(find.text('Rental (3 day)'), findsOneWidget);
      expect(tester.widget<AppButton>(submit).onPressed, isNotNull);

      await tapOn(tester, find.byKey(const Key('delivery-switch')));
      expect(find.text('Delivery'), findsNothing, reason: 're-priced without delivery');

      await tapOn(tester, find.byKey(const Key('pay-offline')));
      await tester.enterText(find.byKey(const Key('work-description')), 'Trenching for Block B');
      await tester.ensureVisible(find.byKey(const Key('cust-email')));
      await tester.enterText(find.byKey(const Key('cust-email')), 'ravi@example.com');
      await tapOn(tester, submit);

      expect(api.calls, ['create l-live site-1 3d op=true delivery=false offline']);
      expect(find.text('ER-2611-BKNEW1'), findsWidgets, reason: 'opened the new booking');
      expect(find.byKey(const Key('status-banner')), findsOneWidget);
      expect(find.textContaining('Waiting for Patil Earthmovers to respond'), findsOneWidget);
    });

    testWidgets('taken dates are refused with the next free start, and moving the dates fixes it', (tester) async {
      final comms = FakeCommsApi();
      final api = FakeBookingApi(comms);
      final t = DateTime.now().add(const Duration(days: 1));
      api.busyOn = DateTime(t.year, t.month, t.day);
      final c = await pumpApp(tester, size: desktop, bookings: api);
      await as(tester, c, UserRole.customer, '/customer/book/l-live');
      await tapOn(tester, find.byKey(const Key('site-site-1')));

      expect(find.byKey(const Key('dates-unavailable')), findsOneWidget);
      expect(find.text('The machine is already booked for part of these dates.'), findsOneWidget);
      expect(tester.widget<AppButton>(find.byKey(const Key('submit-booking'))).onPressed, isNull);

      await tapOn(tester, find.byKey(const Key('next-free-start')));
      expect(find.byKey(const Key('dates-unavailable')), findsNothing);
      expect(tester.widget<AppButton>(find.byKey(const Key('submit-booking'))).onPressed, isNotNull);
    });

    testWidgets('a new work site is saved with the device location', (tester) async {
      final api = FakeBookingApi(FakeCommsApi());
      final c = await pumpApp(tester, size: desktop, bookings: api);
      await as(tester, c, UserRole.customer, '/customer/book/l-live');
      await tapOn(tester, find.byKey(const Key('add-site')));
      await tester.enterText(find.byKey(const Key('site-label')), 'Hinjewadi basement');
      await tester.enterText(find.byKey(const Key('site-line1')), 'Plot 7, Phase 3');
      await tester.enterText(find.byKey(const Key('site-city')), 'Pune');
      await tester.enterText(find.byKey(const Key('site-pincode')), '411057');
      await tester.pump();
      await tapOn(tester, find.text('Use my location'));
      expect(find.textContaining('18.5308'), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('save-site')));

      expect(api.calls, ['site Hinjewadi basement 411057']);
      expect(find.text('Hinjewadi basement'), findsOneWidget);
    });
  });

  testWidgets('the whole rental: request → accept → pay → operator → dispatch → track → handover → extend → return → bill → invoice → complete → review',
      (tester) async {
    final comms = FakeCommsApi();
    final api = FakeBookingApi(comms);
    final opened = <String>[];
    final c = await pumpApp(tester, size: desktop, bookings: api, opened: opened);

    // Customer requests.
    await as(tester, c, UserRole.customer, '/customer/book/l-live');
    await tapOn(tester, find.byKey(const Key('site-site-1')));
    await tester.ensureVisible(find.byKey(const Key('cust-email')));
    await tester.enterText(find.byKey(const Key('cust-email')), 'ravi@example.com');
    await tapOn(tester, find.byKey(const Key('submit-booking')));
    const id = 'bk-new-1';
    expect(comms.statusOf[id], isNull, reason: 'starts as requested');

    // Owner sees the customer, the availability check, and accepts.
    await as(tester, c, UserRole.owner, '/owner/bookings/$id');
    expect(find.text('Ravi Deshmukh'), findsOneWidget);
    expect(find.text('Dates are free'), findsOneWidget);
    await tapOn(tester, find.byKey(const Key('accept-booking')));
    expect(comms.statusOf[id], 'payment_pending');

    // Customer pays in the sandbox checkout → confirmed.
    await as(tester, c, UserRole.customer, '/customer/bookings/$id');
    expect(find.textContaining('to confirm, or the dates are released'), findsOneWidget);
    await act(tester, 'pay');
    await tapOn(tester, find.byKey(const Key('confirm-payment')));
    expect(api.calls, containsAllInOrder(['pay $id advance', 'verify $id']));
    expect(comms.statusOf[id], 'confirmed');
    expect(find.text('₹29,470'), findsWidgets, reason: 'paid amount shown');

    // Owner assigns an operator and dispatches.
    await as(tester, c, UserRole.owner, '/owner/bookings/$id');
    await act(tester, 'assign_operator');
    await tapOn(tester, find.byKey(const Key('operator-op-1')));
    expect(comms.statusOf[id], 'operator_assigned');
    await act(tester, 'dispatch');
    expect(comms.statusOf[id], 'dispatched');

    // Customer tracks the delivery.
    await as(tester, c, UserRole.customer, '/customer/bookings/$id');
    expect(find.byKey(const Key('action-request_cancellation')), findsOneWidget, reason: 'after dispatch the owner must approve');
    expect(find.byKey(const Key('action-cancel')), findsNothing);
    await tapOn(tester, find.byKey(const Key('open-tracking')));
    expect(find.text('5.4 km'), findsOneWidget);
    expect(find.text('Suresh Jadhav'), findsOneWidget);

    // Owner marks arrival; customer confirms the handover.
    await as(tester, c, UserRole.owner, '/owner/bookings/$id');
    await act(tester, 'arrive');
    await as(tester, c, UserRole.customer, '/customer/bookings/$id');
    await act(tester, 'confirm_handover');
    expect(comms.statusOf[id], 'active');

    // Customer asks for one more day; owner approves.
    await act(tester, 'request_extension');
    expect(find.byKey(const Key('extension-quote')), findsOneWidget);
    await tapOn(tester, find.byKey(const Key('send-extension')));
    expect(comms.statusOf[id], 'extension_requested');
    expect(find.byKey(const Key('withdraw-extension')), findsOneWidget, reason: 'the requester can only withdraw');
    expect(find.byKey(const Key('approve-extension')), findsNothing);
    await as(tester, c, UserRole.owner, '/owner/bookings/$id');
    await tapOn(tester, find.byKey(const Key('approve-extension')));
    expect(comms.statusOf[id], 'active');

    // Customer releases; owner takes it back, bills, invoices, completes.
    await as(tester, c, UserRole.customer, '/customer/bookings/$id');
    await act(tester, 'request_return');
    await confirm(tester);
    await as(tester, c, UserRole.owner, '/owner/bookings/$id');
    await act(tester, 'mark_returned');
    await act(tester, 'settle_final');
    await tester.enterText(find.byKey(const Key('final-hours')), '30');
    await tapOn(tester, find.byKey(const Key('save-final')));
    await act(tester, 'issue_invoice');
    expect(find.text('ER/2627/00001'), findsOneWidget);
    await act(tester, 'complete');
    await confirm(tester);
    expect(comms.statusOf[id], 'completed');
    expect(api.calls, containsAllInOrder(['mark-returned $id', 'final $id 30.0 0', 'invoice $id', 'complete $id']));

    // Customer downloads the invoice and can review; the history has every step.
    await as(tester, c, UserRole.customer, '/customer/bookings/$id');
    await tapOn(tester, find.byKey(const Key('download-inv-1')));
    expect(opened, ['https://files.test/inv-1.pdf']);
    expect(find.text('How was the machine?'), findsOneWidget);
    for (final step in ['Awaiting payment', 'Booking confirmed', 'Operator assigned', 'Machine dispatched', 'Arrived on site', 'Rental started',
      'Extension requested', 'Return requested', 'Machine returned', 'Rental completed']) {
      expect(find.text(step), findsWidgets, reason: step);
    }
  });

  group('cancellations', () {
    testWidgets('a paid booking cancelled before dispatch shows the fee and refunds the rest', (tester) async {
      final comms = FakeCommsApi()..statusOf['bk-1'] = 'confirmed';
      comms.paidOf['bk-1'] = 2947000;
      final api = FakeBookingApi(comms);
      final c = await pumpApp(tester, size: desktop, bookings: api);
      await as(tester, c, UserRole.customer, '/customer/bookings/bk-1');
      await act(tester, 'cancel');
      expect(find.text('Cancellation fee: ₹5,000. ₹24,470 will be refunded.'), findsOneWidget);
      await giveReason(tester, 'Project postponed');
      expect(comms.statusOf['bk-1'], 'refund_pending');
      expect(find.textContaining('is being refunded'), findsWidgets);
    });

    testWidgets('after dispatch the customer asks and the owner declines; the booking carries on', (tester) async {
      final comms = FakeCommsApi()..moveTo('bk-1', 'dispatched');
      final api = FakeBookingApi(comms);
      final c = await pumpApp(tester, size: desktop, bookings: api);
      await as(tester, c, UserRole.customer, '/customer/bookings/bk-1');
      await act(tester, 'request_cancellation');
      expect(find.textContaining('the owner must approve'), findsOneWidget);
      await giveReason(tester, 'Site not ready');
      expect(comms.statusOf['bk-1'], 'cancellation_requested');

      await as(tester, c, UserRole.owner, '/owner/bookings/bk-1');
      expect(find.text('The customer asked to cancel. Approve or decline.'), findsOneWidget);
      await act(tester, 'decline_cancellation');
      await giveReason(tester, 'Machine already on the road');
      expect(api.calls, contains('cancellation decline bk-1 Machine already on the road'));
      expect(comms.statusOf['bk-1'], 'dispatched');
    });

    testWidgets('a step the server refuses shows its message', (tester) async {
      final comms = FakeCommsApi()..moveTo('bk-1', 'confirmed');
      final api = FakeBookingApi(comms);
      final c = await pumpApp(tester, size: desktop, bookings: api);
      await as(tester, c, UserRole.owner, '/owner/bookings/bk-1');
      comms.statusOf['bk-1'] = 'cancelled'; // someone else cancelled meanwhile
      await act(tester, 'dispatch');
      expect(find.text('cannot dispatch a cancelled booking'), findsOneWidget);
      expect(find.byKey(const Key('action-dispatch')), findsNothing, reason: 'refreshed to the new state');
    });
  });

  testWidgets('owner booking inbox groups requests, active rentals and history', (tester) async {
    final comms = FakeCommsApi()
      ..moveTo('bk-1', 'active')
      ..created['bk-new-9'] = 'Escorts crane';
    comms.moveTo('bk-new-9', 'cancelled');
    final c = await pumpApp(tester, size: desktop, comms: comms);
    await as(tester, c, UserRole.owner, '/owner/bookings');
    await tapOn(tester, find.text('Requests'));
    expect(find.text('Tata Hitachi EX200'), findsOneWidget);
    expect(find.text('Liugong 835H loader'), findsNothing);
    await tapOn(tester, find.text('Active rentals'));
    expect(find.text('Liugong 835H loader'), findsOneWidget);
    await tapOn(tester, find.text('History'));
    expect(find.text('Escorts crane'), findsOneWidget);
    expect(find.text('JCB 3DX backhoe'), findsOneWidget);
  });
}
