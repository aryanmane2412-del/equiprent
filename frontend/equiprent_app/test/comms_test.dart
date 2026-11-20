import 'package:equiprent_app/app/router.dart';
import 'package:equiprent_app/core/auth/auth_controller.dart';
import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/core/widgets/buttons.dart';
import 'package:equiprent_app/features/comms/comms_routes.dart';
import 'package:equiprent_app/features/comms/push/push_service.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_comms_api.dart';
import 'test_helpers.dart';

void main() {
  // ---- notifications ------------------------------------------------------------------------

  group('notifications', () {
    testWidgets(
      'the bell shows unread; tapping a notification marks it read and opens the booking',
      (tester) async {
        final comms = FakeCommsApi();
        final c = await pumpApp(tester, comms: comms);
        await goTo(tester, c, '/customer', role: UserRole.customer);
        expect(find.byTooltip('Notifications, 2 unread'), findsOneWidget);

        await tapOn(tester, find.byKey(const Key('notifications-button')));
        expect(find.text('Notifications'), findsOneWidget);
        expect(find.text('Maintenance coming up'), findsOneWidget);
        await tapOn(tester, find.text('Booking accepted'));
        expect(comms.calls, contains('read n-1'));
        expect(
          find.text('Rental agreement'),
          findsOneWidget,
          reason: 'booking detail opened',
        );
      },
    );

    testWidgets('mark all read and per-category push settings', (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms);
      await goTo(tester, c, '/customer/notifications', role: UserRole.customer);
      expect(find.text('2 unread'), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('mark-all-read')));
      expect(comms.calls, contains('read-all'));
      expect(find.text('All caught up'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('notification-settings')));
      expect(
        find.textContaining('Push is not set up in this build'),
        findsOneWidget,
      );
      await tapOn(tester, find.byKey(const Key('push-chat')));
      expect(comms.calls, contains('prefs chat=false'));
    });

    test('notifications deep-link to the right screen per role', () {
      expect(
        routeForNotification(UserRole.customer, 'booking.accepted', {
          'booking_id': 'b1',
        }),
        '/customer/bookings/b1',
      );
      expect(
        routeForNotification(UserRole.owner, 'booking.requested', {
          'booking_id': 'b1',
        }),
        '/owner/bookings/b1',
      );
      expect(
        routeForNotification(UserRole.owner, 'agreement.signed', {
          'booking_id': 'b1',
        }),
        '/owner/bookings/b1/agreement',
      );
      expect(
        routeForNotification(UserRole.customer, 'chat.message', {
          'conversation_id': 'c1',
          'booking_id': 'b1',
        }),
        '/customer/chat/c1',
      );
      expect(
        routeForNotification(UserRole.owner, 'chat.message', {
          'conversation_id': 'c1',
        }),
        '/owner/messages/c1',
      );
      expect(
        routeForNotification(UserRole.owner, 'maintenance.reminder', {
          'equipment_id': 'e1',
        }),
        '/owner/maintenance',
      );
      expect(
        routeForNotification(UserRole.owner, 'review.received', {
          'booking_id': 'b1',
        }),
        '/owner/reviews',
      );
      expect(
        routeForNotification(UserRole.operator, 'booking.operator_assigned', {
          'booking_id': 'b1',
        }),
        '/operator/jobs/b1',
      );
      expect(
        routeForNotification(UserRole.fleetManager, 'booking.confirmed', {
          'booking_id': 'b1',
        }),
        isNull,
      );
    });
  });

  // ---- push (FCM) -------------------------------------------------------------------------------

  testWidgets(
    'push: the device token is registered at sign-in, taps open the screen, sign-out drops it',
    (tester) async {
      final comms = FakeCommsApi();
      final push = FakePushService();
      final c = await pumpApp(tester, comms: comms, push: push);
      await settle(tester);
      expect(push.starts, 1);
      expect(comms.calls, contains('device android fcm-test-token-0001'));

      await goTo(tester, c, '/customer', role: UserRole.customer);
      push.foregroundCtl.add(
        const PushMessage(
          title: 'Payment confirmed',
          body: 'We received ₹19,470.00.',
          data: {'type': 'payment.captured'},
        ),
      );
      await tester.pump();
      await tester.pump();
      expect(
        find.text('Payment confirmed'),
        findsOneWidget,
        reason: 'in-app banner',
      );
      await tester.pump(const Duration(seconds: 6)); // banner hides
      expect(find.text('Payment confirmed'), findsNothing);

      push.openedCtl.add(
        const PushMessage(
          title: 'Ravi',
          body: 'Gate 2?',
          data: {'type': 'chat.message', 'conversation_id': 'conv-1'},
        ),
      );
      await settle(tester);
      expect(
        find.text('Machine leaves the yard at 7.'),
        findsOneWidget,
        reason: 'opened the chat thread',
      );

      await c.read(authControllerProvider.notifier).logout();
      await settle(tester);
      expect(push.stops, 1);
    },
  );

  // ---- bookings (entry point for agreements, chat, reviews) ----------------------------------

  group('bookings', () {
    testWidgets('owner sees requests and accepts one', (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(tester, c, '/owner/bookings', role: UserRole.owner);
      expect(find.text('Tata Hitachi EX200'), findsOneWidget);
      await tapOn(tester, find.text('Requests'));
      expect(find.text('Liugong 835H loader'), findsNothing);
      await tapOn(tester, find.text('Tata Hitachi EX200'));
      expect(find.text('New booking request'), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('accept-booking')));
      expect(comms.calls, contains('accept bk-req'));
      expect(find.text('New booking request'), findsNothing);
    });

    testWidgets('owner declines with a reason', (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(tester, c, '/owner/bookings/bk-req', role: UserRole.owner);
      await tapOn(tester, find.byKey(const Key('decline-booking')));
      await tester.enterText(
        find.byType(TextField).last,
        'Machine under repair',
      );
      await tester.pump();
      await tapOn(tester, find.widgetWithText(AppButton, 'Decline').last);
      expect(comms.calls, contains('reject bk-req Machine under repair'));
    });
  });

  // ---- agreements ---------------------------------------------------------------------------

  testWidgets(
    'agreement: read the terms, sign with consent, download the PDF',
    (tester) async {
      final comms = FakeCommsApi();
      final opened = <String>[];
      final c = await pumpApp(
        tester,
        comms: comms,
        opened: opened,
        size: desktop,
      );
      await goTo(
        tester,
        c,
        '/customer/bookings/bk-1/agreement',
        role: UserRole.customer,
      );

      final terms = tester
          .widget<SelectableText>(find.byKey(const Key('agreement-terms')))
          .data!;
      expect(terms, contains('Owner (lessor): Patil Earthmovers Pvt Ltd'));
      expect(terms, contains('Estimated total including taxes'));
      expect(find.text('GSTIN 27ABCDE1234F1Z5'), findsOneWidget);
      expect(find.text('Awaiting acceptance'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('sign-agreement')));
      expect(
        comms.calls.where((x) => x.startsWith('sign')),
        isEmpty,
        reason: 'consent is required',
      );
      await tester.enterText(
        find.byKey(const Key('signer-designation')).hitTestable().first,
        'Director',
      );
      await tapOn(tester, find.byKey(const Key('sign-consent')));
      await tapOn(tester, find.byKey(const Key('sign-agreement')));
      expect(comms.calls, contains('sign bk-1 ${'a' * 64} Test User Director'));
      expect(find.text('Waiting for the other party'), findsOneWidget);
      expect(find.byKey(const Key('sign-agreement')), findsNothing);

      await tapOn(tester, find.byKey(const Key('agreement-pdf')));
      expect(opened, ['https://files.test/agreement-bk-1.pdf']);
    },
  );

  // ---- chat -----------------------------------------------------------------------------------

  group('chat', () {
    testWidgets('booking chat: history, send, photo, delete own message', (
      tester,
    ) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms);
      await goTo(tester, c, '/customer/bookings/bk-1', role: UserRole.customer);
      await tapOn(tester, find.byKey(const Key('open-chat')));
      expect(comms.calls, contains('open-chat bk-1'));
      expect(find.text('Machine leaves the yard at 7.'), findsOneWidget);
      expect(find.text('Ravi Patil'), findsOneWidget);
      expect(comms.calls, contains('chat-read conv-1'));

      await tester.enterText(find.byKey(const Key('chat-input')), 'On my way');
      await tapOn(tester, find.byKey(const Key('chat-send')));
      expect(comms.calls, contains('send conv-1 On my way'));
      expect(find.text('On my way'), findsOneWidget);

      await tapOn(tester, find.byKey(const Key('chat-attach')));
      expect(comms.calls, contains('upload chat_image me.png'));
      expect(
        comms.calls.any(
          (x) => x.startsWith('send conv-1') && x.contains('+up-'),
        ),
        isTrue,
      );

      await tester.longPress(find.text('On my way'));
      await settle(tester);
      await tapOn(tester, find.text('Remove'));
      expect(comms.calls.any((x) => x.startsWith('delete m-')), isTrue);
      expect(find.text('Message removed'), findsOneWidget);
    });

    testWidgets('a failed send can be retried with the same message', (
      tester,
    ) async {
      final comms = FakeCommsApi()
        ..failNext = 'You are not part of this conversation';
      final c = await pumpApp(tester, comms: comms);
      await goTo(tester, c, '/customer/chat/conv-1', role: UserRole.customer);
      await tester.enterText(find.byKey(const Key('chat-input')), 'Hello?');
      await tapOn(tester, find.byKey(const Key('chat-send')));
      expect(find.text('Not sent · tap to retry'), findsOneWidget);
      await tapOn(tester, find.text('Hello?'));
      expect(comms.calls, contains('send conv-1 Hello?'));
      expect(find.text('Not sent · tap to retry'), findsNothing);
    });

    testWidgets('conversation list and a listing enquiry', (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(tester, c, '/customer/chat', role: UserRole.customer);
      expect(find.text('ER-2611-BK1 · Liugong 835H loader'), findsOneWidget);
      expect(find.text('1 unread'), findsOneWidget);

      await goTo(
        tester,
        c,
        '/customer/equipment/l-live',
        role: UserRole.customer,
      );
      await tapOn(tester, find.byKey(const Key('message-owner')));
      await tester.enterText(find.byType(TextField).last, 'Free next week?');
      await tester.pump();
      await tapOn(tester, find.text('Send'));
      expect(comms.calls, contains('enquire l-live Free next week?'));
      expect(find.text('Free next week?'), findsOneWidget);
      expect(find.text('Listing enquiry'), findsOneWidget);
    });

    testWidgets('owners reach messages from the app bar', (tester) async {
      final c = await pumpApp(tester, comms: FakeCommsApi(), size: desktop);
      await goTo(tester, c, '/owner', role: UserRole.owner);
      await tapOn(tester, find.byKey(const Key('messages-button')));
      expect(find.text('Messages'), findsWidgets);
      expect(find.text('ER-2611-BK1 · Liugong 835H loader'), findsOneWidget);
    });
  });

  // ---- maintenance ----------------------------------------------------------------------------

  testWidgets(
    'maintenance: due plans, schedule, complete, history, costs, plans',
    (tester) async {
      final comms = FakeCommsApi();
      final opened = <String>[];
      final c = await pumpApp(
        tester,
        comms: comms,
        opened: opened,
        size: desktop,
      );
      await goTo(tester, c, '/owner/maintenance', role: UserRole.owner);
      expect(find.text('500 hour service'), findsOneWidget);
      expect(find.text('Engine oil and filters'), findsOneWidget);

      // Schedule the due plan.
      await tapOn(tester, find.text('Schedule').first);
      await tapOn(tester, find.byKey(const Key('record-downtime')));
      await tapOn(tester, find.byKey(const Key('save-record')));
      expect(
        comms.calls,
        contains(
          'record l-draft service scheduled Engine oil and filters downtime',
        ),
      );

      // Complete the open job with its costs.
      await tapOn(tester, find.byKey(const Key('complete-r-open')));
      await tester.enterText(find.byKey(const Key('complete-parts')), '4500');
      await tester.enterText(find.byKey(const Key('complete-labour')), '1500');
      await tapOn(tester, find.byKey(const Key('save-complete')));
      expect(
        comms.calls,
        contains('update r-open done parts=450000 labour=150000'),
      );

      await tapOn(tester, find.text('History'));
      expect(find.text('Starter motor rewinding'), findsOneWidget);
      await tapOn(tester, find.text('Invoice'));
      expect(opened, ['https://files.test/invoice-r-done.pdf']);

      await tapOn(tester, find.text('Costs'));
      expect(
        find.descendant(
          of: find.byKey(const Key('cost-total')),
          matching: find.text('₹14,200'),
        ),
        findsOneWidget,
      );
      expect(find.text('12 hrs'), findsOneWidget);

      await tapOn(tester, find.text('Plans'));
      await tapOn(tester, find.byKey(const Key('add-plan')));
      await tester.enterText(find.byKey(const Key('plan-task')), 'Greasing');
      await tester.enterText(find.byKey(const Key('plan-days')), '7');
      await tapOn(tester, find.byKey(const Key('save-plan')));
      expect(comms.calls, contains('plan l-draft Greasing null 7'));
    },
  );

  // ---- reviews --------------------------------------------------------------------------------

  group('reviews', () {
    testWidgets('customer rates a finished booking', (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(tester, c, '/customer/bookings/bk-2', role: UserRole.customer);
      expect(find.text('How was the machine?'), findsOneWidget);
      expect(find.text("How was the owner's service?"), findsOneWidget);
      await tapOn(
        tester,
        find.descendant(
          of: find.byKey(const Key('rate-equipment')),
          matching: find.byTooltip('4 stars'),
        ),
      );
      await tester.enterText(
        find.byKey(const Key('review-comment-equipment')),
        'Strong machine',
      );
      await tapOn(tester, find.byKey(const Key('submit-review-equipment')));
      expect(comms.calls, contains('review bk-2 equipment 4 Strong machine'));
      expect(find.text('How was the machine?'), findsNothing);
    });

    testWidgets(
      'listing shows the rating summary and reviews can be reported',
      (tester) async {
        final comms = FakeCommsApi();
        final c = await pumpApp(tester, comms: comms, size: desktop);
        await goTo(
          tester,
          c,
          '/customer/equipment/l-live',
          role: UserRole.customer,
        );
        expect(
          find.text('Strong loader, a little slow to start.'),
          findsOneWidget,
        );
        expect(find.text('Thanks! Starter is fixed now.'), findsOneWidget);
        expect(find.text('4.0'), findsOneWidget);
        await tapOn(tester, find.text('Report'));
        await tapOn(tester, find.text('Spam or advertising'));
        expect(comms.calls, contains('report rv-1 spam'));
      },
    );

    testWidgets('owner replies to a review', (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(tester, c, '/owner/reviews', role: UserRole.owner);
      expect(find.text('Very helpful owner.'), findsOneWidget);
      await tapOn(tester, find.byKey(const Key('reply-rv-9')));
      await tester.enterText(find.byType(TextField).last, 'Thank you!');
      await tester.pump();
      await tapOn(tester, find.text('Post reply'));
      expect(comms.calls, contains('reply rv-9 Thank you!'));
      expect(find.byKey(const Key('reply-rv-9')), findsNothing);
    });

    testWidgets('admin review route is unavailable to read-only accounts', (
      tester,
    ) async {
      final c = await pumpApp(tester, size: desktop);
      await goTo(tester, c, '/admin/reviews', role: UserRole.admin);
      expect(find.text('Page not found'), findsOneWidget);
    });
  });

  // ---- together -------------------------------------------------------------------------------

  testWidgets(
    'together: a notification leads to the booking, its agreement and its chat',
    (tester) async {
      final comms = FakeCommsApi();
      final c = await pumpApp(tester, comms: comms, size: desktop);
      await goTo(tester, c, '/customer/notifications', role: UserRole.customer);
      await tapOn(tester, find.text('Booking accepted'));
      await tapOn(tester, find.byKey(const Key('open-agreement')));
      await tapOn(tester, find.byKey(const Key('sign-consent')));
      await tapOn(tester, find.byKey(const Key('sign-agreement')));
      expect(comms.calls.any((x) => x.startsWith('sign bk-1')), isTrue);
      c.read(routerProvider).pop();
      await settle(tester);
      await tapOn(tester, find.byKey(const Key('open-chat')));
      await tester.enterText(
        find.byKey(const Key('chat-input')),
        'Signed the agreement.',
      );
      await tapOn(tester, find.byKey(const Key('chat-send')));
      expect(
        comms.calls,
        containsAllInOrder([
          'read n-1',
          'sign bk-1 ${'a' * 64} Test User',
          'open-chat bk-1',
          'send conv-1 Signed the agreement.',
        ]),
      );
    },
  );
}
