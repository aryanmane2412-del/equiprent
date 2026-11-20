import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/listings/data/listing_models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_listings_api.dart';
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

Future<void> choose<T>(WidgetTester tester, String key, String option) async {
  await tapOn(
    tester,
    find.descendant(
      of: find.byKey(ValueKey(key)),
      matching: find.byType(DropdownButtonFormField<T>),
    ),
  );
  await tapOn(tester, find.text(option).last);
}

void main() {
  group('owner', () {
    testWidgets(
      'creates a listing, uploads several photos, prices it and submits it',
      (tester) async {
        final api = FakeListingsApi();
        final picker = FakePicker()..images = 3;
        final c = await pumpApp(
          tester,
          size: desktop,
          listings: api,
          picker: picker,
        );
        await goTo(tester, c, '/owner/listings', role: UserRole.owner);
        expect(find.text('Tata Hitachi EX 200 excavator'), findsOneWidget);
        expect(
          find.text('JCB 3DX backhoe loader'),
          findsNothing,
          reason: 'another organisation owns it',
        );

        await tapKey(tester, 'add-listing');
        await choose<ListingCategory>(
          tester,
          'field-category',
          'JCB / Backhoe Loader',
        );
        await type(tester, 'field-make', 'JCB');
        await type(tester, 'field-model', '3DX Super');
        await type(tester, 'field-year', '2021');
        await type(tester, 'field-title', 'JCB 3DX Super with operator');
        await choose<City>(tester, 'field-city', 'Pune');
        await type(tester, 'field-area', 'Wakad, Pune');
        await tapKey(tester, 'save-listing');

        expect(api.calls, contains('create'));
        final created = api.listings.values.firstWhere(
          (r) => r.id.startsWith('l-new'),
        );
        expect(created.body['manufacture_year'], 2021);
        expect(created.body['base_location'], {
          'lat': 18.52,
          'lng': 73.85,
        }, reason: 'located at the chosen city');
        expect(find.text('Draft'), findsWidgets);

        // Submit stays disabled until a price and a photo exist.
        expect(
          tester
              .widget<TextButton>(
                find.descendant(
                  of: find.byKey(const ValueKey('submit-listing')),
                  matching: find.byType(TextButton),
                ),
              )
              .onPressed,
          isNull,
        );

        await tapKey(tester, 'add-photos');
        expect(
          api.calls
              .where(
                (c) =>
                    c == 'upload equipment_photo photo0.jpg' ||
                    c.startsWith('photo '),
              )
              .length,
          4,
        );
        expect(created.photos, hasLength(3));
        expect(created.photos.first.isCover, isTrue);
        expect(find.text('3 photos added'), findsOneWidget);

        await tapKey(tester, 'edit-pricing');
        await type(tester, 'rate-day', '18000');
        await type(tester, 'rate-hour', '1500');
        await tapKey(tester, 'save-pricing');
        expect(
          created.pricing.map((p) => (p.unit, p.ratePaise)),
          containsAll([('day', 1800000), ('hour', 150000)]),
        );

        await tapKey(tester, 'edit-specs');
        await type(tester, 'spec-operating_weight_kg', '7660');
        await tapKey(tester, 'save-specs');
        expect(created.specs.single.number, 7660);

        await tapKey(tester, 'submit-listing');
        expect(created.status, ListingStatus.pendingReview);
        expect(find.text('Submitted for review'), findsOneWidget);
        expect(
          api.orgsSeen.toSet(),
          {FakeListingsApi.ownerOrg},
          reason: 'every owner call carries the owner organisation',
        );
      },
    );

    testWidgets('edits details with the version it read', (tester) async {
      final api = FakeListingsApi();
      final c = await pumpApp(tester, size: desktop, listings: api);
      await goTo(tester, c, '/owner/listings/l-draft', role: UserRole.owner);
      await tapKey(tester, 'edit-details');
      expect(find.text('Edit details'), findsOneWidget);
      await type(tester, 'field-title', 'Tata Hitachi EX 200 LC excavator');
      await choose<City>(tester, 'field-city', 'Pune');
      await type(tester, 'field-area', 'Chakan, Pune');
      await tapKey(tester, 'save-listing');

      expect(api.calls, contains('update l-draft v1'));
      expect(
        api.listings['l-draft']!.title,
        'Tata Hitachi EX 200 LC excavator',
      );
      expect(api.listings['l-draft']!.version, 2);
      expect(find.text('Details updated'), findsOneWidget);
    });

    testWidgets('a stale version is reported, not silently overwritten', (
      tester,
    ) async {
      final api = FakeListingsApi();
      final c = await pumpApp(tester, size: desktop, listings: api);
      await goTo(
        tester,
        c,
        '/owner/listings/l-draft/edit',
        role: UserRole.owner,
      );
      api.listings['l-draft']!.version = 5; // someone else saved meanwhile
      await choose<City>(tester, 'field-city', 'Pune');
      await type(tester, 'field-area', 'Chakan, Pune');
      await tapKey(tester, 'save-listing');
      expect(
        find.textContaining('Someone else changed this listing'),
        findsOneWidget,
      );
      expect(api.listings['l-draft']!.title, 'Tata Hitachi EX 200 excavator');
    });

    testWidgets('adds an equipment document and removes a photo', (
      tester,
    ) async {
      final api = FakeListingsApi();
      final c = await pumpApp(tester, size: desktop, listings: api);
      await goTo(tester, c, '/owner/listings/l-review', role: UserRole.owner);

      await tapKey(tester, 'add-document');
      await tapOn(tester, find.byType(DropdownButtonFormField<String>));
      await tapOn(tester, find.text('Insurance').last);
      await tapKey(tester, 'pick-doc-file');
      await tapKey(tester, 'save-doc');
      expect(
        api.calls,
        containsAllInOrder([
          'upload equipment_document pan.pdf',
          'document l-review insurance',
        ]),
      );
      expect(find.text('Insurance'), findsOneWidget);

      await tapOn(tester, find.byTooltip('Photo actions'));
      await tapOn(tester, find.text('Remove'));
      expect(api.listings['l-review']!.photos, isEmpty);
    });

    testWidgets('pauses, resumes and deletes a live listing', (tester) async {
      final api = FakeListingsApi();
      api.listings['l-draft']!
        ..status = ListingStatus.active
        ..pricing = [
          const PricingPlan(
            unit: 'day',
            withOperator: true,
            ratePaise: 1000000,
          ),
        ];
      final c = await pumpApp(tester, size: desktop, listings: api);
      await goTo(tester, c, '/owner/listings/l-draft', role: UserRole.owner);
      await tapKey(tester, 'pause-listing');
      expect(api.listings['l-draft']!.status, ListingStatus.paused);
      await tapKey(tester, 'resume-listing');
      expect(api.listings['l-draft']!.status, ListingStatus.active);

      await tapKey(tester, 'delete-listing');
      await tapOn(tester, find.text('Delete').last);
      expect(api.listings['l-draft']!.status, ListingStatus.retired);
      expect(find.text('My equipment'), findsOneWidget);
      expect(find.text('Tata Hitachi EX 200 excavator'), findsNothing);
    });

    testWidgets('suspended listings are read-only for the owner', (
      tester,
    ) async {
      final api = FakeListingsApi();
      api.listings['l-review']!
        ..status = ListingStatus.suspended
        ..note = 'Documents expired';
      final c = await pumpApp(tester, size: desktop, listings: api);
      await goTo(tester, c, '/owner/listings/l-review', role: UserRole.owner);
      expect(find.text('Documents expired'), findsOneWidget);
      for (final key in [
        'add-photos',
        'edit-details',
        'edit-pricing',
        'add-document',
        'submit-listing',
      ]) {
        expect(find.byKey(ValueKey(key)), findsNothing, reason: key);
      }
    });

    testWidgets("another organisation's listing is not found", (tester) async {
      final c = await pumpApp(tester, size: desktop);
      await goTo(tester, c, '/owner/listings/l-live', role: UserRole.owner);
      expect(find.text('Listing not found'), findsOneWidget);
      expect(find.byKey(const ValueKey('add-photos')), findsNothing);
    });
  });

  group('customer', () {
    testWidgets('a category opens the search filtered to it', (tester) async {
      final c = await pumpApp(tester, size: desktop);
      await goTo(tester, c, '/customer/categories', role: UserRole.customer);
      await tapKey(tester, 'category-crane');
      expect(find.text('Escorts 15 t pick-and-carry crane'), findsOneWidget);
      expect(find.text('JCB 3DX backhoe loader'), findsNothing);
    });

    testWidgets(
      'sees photos, specifications, prices, owner verification and availability',
      (tester) async {
        final c = await pumpApp(tester, size: desktop);
        await goTo(tester, c, '/customer/search', role: UserRole.customer);
        await tapOn(tester, find.text('JCB 3DX backhoe loader'));
        expect(find.byKey(const ValueKey('gallery-main-0')), findsOneWidget);
        await tapKey(tester, 'thumb-1');
        expect(find.byKey(const ValueKey('gallery-main-1')), findsOneWidget);
        expect(find.text('7660 kg'), findsOneWidget);
        expect(find.text('₹18,000'), findsOneWidget);
        expect(find.text('Verified owner'), findsWidgets);
        expect(find.text('Booked on 2 of the next 42 days.'), findsOneWidget);
      },
    );

    testWidgets('a listing that is not approved cannot be opened', (
      tester,
    ) async {
      final c = await pumpApp(tester, size: desktop);
      await goTo(
        tester,
        c,
        '/customer/equipment/l-review',
        role: UserRole.customer,
      );
      expect(find.text('This listing is not available'), findsOneWidget);
    });
  });

  group('admin', () {
    testWidgets(
      'admin can review equipment but cannot use unrelated admin pages',
      (tester) async {
        final c = await pumpApp(tester, size: desktop);
        await goTo(tester, c, '/admin/listings', role: UserRole.admin);
        expect(find.text('Listing moderation'), findsOneWidget);
        await goTo(tester, c, '/admin/listings/l-review', role: UserRole.admin);
        expect(find.text('JCB 3DX Xtra backhoe loader'), findsWidgets);
        expect(find.byKey(const Key('approve-listing')), findsOneWidget);
        expect(find.byKey(const Key('reject-listing')), findsOneWidget);
        expect(find.byKey(const Key('suspend-listing')), findsNothing);
        await goTo(tester, c, '/admin/categories', role: UserRole.admin);
        expect(find.text('Page not found'), findsOneWidget);
      },
    );
  });
}
