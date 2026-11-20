import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/discovery/data/discovery_api.dart';
import 'package:equiprent_app/features/discovery/data/discovery_models.dart';
import 'package:equiprent_app/features/listings/data/listing_models.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_discovery_api.dart';
import 'test_helpers.dart';

Future<void> tapKey(WidgetTester tester, String key) => tapOn(tester, find.byKey(ValueKey(key)));

Future<void> search(WidgetTester tester, String text) async {
  await tester.tap(find.byKey(const ValueKey('discover-search')));
  await tester.pump();
  await tester.enterText(find.byKey(const ValueKey('discover-search')), text);
  await tester.testTextInput.receiveAction(TextInputAction.search);
  await settle(tester);
}

List<String> shownTitles(FakeDiscoveryApi api) => [for (final m in api.machines) if (find.text(m.title).evaluate().isNotEmpty) m.title];

void main() {
  testWidgets('query model round-trips to API parameters', (tester) async {
    final q = DiscoverQuery(
      text: ' jcb ',
      categories: const ['crane', 'excavator'],
      place: const SearchPlace.pincode('411057', '411057 · Hinjewadi'),
      radiusKm: 30,
      priceMinRupees: 5000,
      priceMaxRupees: 20000,
      ratingMin: 4,
      availableFrom: DateTime(2027, 3, 1),
      availableTo: DateTime(2027, 3, 5),
      conditions: const [MachineCondition.excellent, MachineCondition.needsRepair],
      deliveryOnly: true,
      operator: OperatorFilter.machineOnly,
      verifiedOnly: true,
      sort: DiscoverSort.priceAsc,
      page: 3,
    );
    expect(q.toQuery(), {
      'q': 'jcb', 'category': 'crane,excavator', 'pincode': '411057', 'radius_km': 30.0, 'price_min': 500000, 'price_max': 2000000,
      'rating_min': 4.0, 'available_from': '2027-03-01', 'available_to': '2027-03-05', 'condition': 'excellent,needs_repair',
      'delivery': true, 'operator': 'without', 'verified_only': true, 'sort': 'price_asc', 'page': 3,
    });
    expect(q.activeFilterCount, 9);
    expect(q.copyWith(ratingMin: null).page, 1, reason: 'changing a filter returns to page one');
    final back = DiscoverQuery.fromParams({'q': 'jcb', 'category': 'crane,excavator', 'pincode': '411057', 'price_max': 2000000, 'delivery': true});
    expect(back.categories, ['crane', 'excavator']);
    expect(back.priceMaxRupees, 20000);
    expect(back.place?.pincode, '411057');
    expect(DiscoverQuery(text: 'x', place: SearchPlace.city('c1', 'Pune'), radiusKm: 10).toQuery().containsKey('radius_km'), isFalse,
        reason: 'a city filters by city, not radius');
  });

  testWidgets('searches by text and shows the newest approved machines by default', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    expect(shownTitles(api), hasLength(5));
    expect(find.text('Recently listed'), findsOneWidget);

    await search(tester, 'excavator');
    expect(shownTitles(api), ['Tata Hitachi EX200 excavator']);
    expect(api.queries.last.text, 'excavator');
    expect(find.text('Best match'), findsOneWidget, reason: 'text searches rank by relevance');
  });

  testWidgets('suggestions apply a category, manufacturer or PIN code', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);

    await tester.tap(find.byKey(const ValueKey('discover-search')));
    await tester.pump();
    await tester.enterText(find.byKey(const ValueKey('discover-search')), 'ex');
    await tester.pump(const Duration(milliseconds: 300));
    await settle(tester);
    await tapKey(tester, 'suggestion-category-excavator');
    expect(api.queries.last.categories, ['excavator']);
    expect(shownTitles(api), ['Tata Hitachi EX200 excavator']);

    await tester.tap(find.byKey(const ValueKey('discover-search')));
    await tester.pump();
    await tester.enterText(find.byKey(const ValueKey('discover-search')), '4110');
    await tester.pump(const Duration(milliseconds: 300));
    await settle(tester);
    await tapKey(tester, 'suggestion-pincode-411057');
    expect(api.queries.last.place?.pincode, '411057');
    expect(find.text('411057 · Hinjewadi, Pune'), findsWidgets);
  });

  testWidgets('nearby search shows distances and sorts nearest first', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await tapKey(tester, 'place-chip');
    await tapKey(tester, 'use-my-location');
    final q = api.queries.last;
    expect(q.place?.isPoint, isTrue);
    expect(q.place?.lat, 18.5308);
    expect(find.text('Nearest'), findsOneWidget);
    // Pune machines within 50 km; Mumbai and Nagpur excluded.
    expect(shownTitles(api)..sort(), ['Dynapac vibratory roller', 'JCB 3DX backhoe loader', 'Tata Hitachi EX200 excavator']);
    expect(find.byKey(const ValueKey('distance-m-exc')), findsOneWidget);
    expect(find.textContaining('km away'), findsNWidgets(3));

    // Clearing the place drops distance sorting.
    await tester.tap(find.descendant(of: find.byKey(const ValueKey('place-chip')), matching: find.byIcon(Icons.clear)).first);
    await settle(tester);
    expect(api.queries.last.place, isNull);
    expect(shownTitles(api), hasLength(5));
  });

  testWidgets('location errors are explained', (tester) async {
    final c = await pumpApp(tester, size: desktop, locator: () async => throw const LocationUnavailable('Location permission was denied.'));
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await tapKey(tester, 'place-chip');
    await tapKey(tester, 'use-my-location');
    expect(find.text('Location permission was denied.'), findsOneWidget);
    await tapKey(tester, 'place-411057'.replaceAll('411057', 'city-pune'));
    expect(find.text('Pune'), findsWidgets);
  });

  testWidgets('filters, removable filter chips and sorting', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);

    await tapKey(tester, 'filters-button');
    await tapKey(tester, 'filter-verified');
    await tapKey(tester, 'filter-delivery');
    await tester.enterText(find.descendant(of: find.byKey(const ValueKey('filter-price-max')), matching: find.byType(EditableText)), '20000');
    await tapKey(tester, 'filter-rating-4.0');
    await tapKey(tester, 'filters-apply');
    final q = api.queries.last;
    expect((q.verifiedOnly, q.deliveryOnly, q.priceMaxRupees, q.ratingMin), (true, true, 20000, 4.0));
    expect(shownTitles(api), ['JCB 3DX backhoe loader']);
    expect(find.text('Filters · 4'), findsOneWidget);

    // Remove one filter from its chip.
    await tester.tap(find.descendant(of: find.widgetWithText(InputChip, '★ 4.0+'), matching: find.byIcon(Icons.clear)));
    await settle(tester);
    expect(api.queries.last.ratingMin, isNull);
    expect(shownTitles(api)..sort(), ['Dynapac vibratory roller', 'JCB 3DX backhoe loader']);

    await tapKey(tester, 'clear-all-filters');
    expect(api.queries.last.activeFilterCount, 0);

    await tapKey(tester, 'sort-menu');
    await tapOn(tester, find.text('Lowest price').last);
    expect(api.queries.last.sort, DiscoverSort.priceAsc);
    final first = tester.getTopLeft(find.text('Dynapac vibratory roller')).dy;
    final last = tester.getTopLeft(find.text('CAT D6R bulldozer')).dy;
    expect(first, lessThan(last), reason: 'cheapest first');
  });

  testWidgets('pagination moves between pages', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    // Shrink pages via a history entry carrying page_size is not exposed; drive the query directly.
    api.machines.addAll([
      for (var i = 0; i < 20; i++) FakeMachine('x-$i', 'Extra machine $i', 'tractor', 'Mahindra', 'Arjun', 'Pune', 18.6, 73.8, 3000 + i, created: -i),
    ]);
    await goTo(tester, c, '/customer/search?q=');
    await tapKey(tester, 'sort-menu');
    await tapOn(tester, find.text('Recently listed').last);
    expect(find.byKey(const ValueKey('page-2')), findsOneWidget);
    await tapKey(tester, 'page-2');
    expect(api.queries.last.page, 2);
    expect(find.text('Extra machine 19'), findsOneWidget);
    await tapKey(tester, 'page-prev');
    expect(api.queries.last.page, 1);
  });

  testWidgets('saves equipment and shows it on the saved page', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await tapKey(tester, 'save-m-exc');
    expect(api.savedSet, {'m-exc'});
    expect(find.text('Saved'), findsOneWidget);
    await goTo(tester, c, '/customer/saved', role: UserRole.customer);
    expect(find.text('Saved equipment'), findsWidgets);
    expect(find.text('Tata Hitachi EX200 excavator'), findsOneWidget);
    await tapKey(tester, 'save-m-exc');
    expect(api.savedSet, isEmpty);
    expect(find.text('Nothing saved yet'), findsOneWidget);
  });

  testWidgets('a failed save is rolled back and reported', (tester) async {
    final api = FakeDiscoveryApi()..failSave = true;
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await tapKey(tester, 'save-m-exc');
    expect(c.read(savedIdsProvider).value, isEmpty);
    expect(find.text('Could not save'), findsOneWidget);
  });

  testWidgets('compares up to four machines side by side', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await tapKey(tester, 'compare-l-live');
    expect(find.text('1 of 4 selected to compare'), findsOneWidget);
    await tapKey(tester, 'compare-l-crane');
    await tapKey(tester, 'open-compare');
    expect(find.byKey(const ValueKey('compare-col-l-live')), findsOneWidget);
    expect(find.byKey(const ValueKey('compare-col-l-crane')), findsOneWidget);
    expect(find.text('Price per day'), findsOneWidget);
    expect(find.text('₹18,000'), findsOneWidget);
    expect(find.text('Operating weight'), findsOneWidget, reason: 'specs from every listing');

    for (final id in ['m-exc', 'm-roller', 'm-dozer']) {
      c.read(compareSelectionProvider.notifier).toggle(id);
    }
    expect(c.read(compareSelectionProvider), hasLength(4), reason: 'the fifth is refused');
  });

  testWidgets('map view shows pins and searches the visible area', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await tapKey(tester, 'view-toggle');
    expect(find.byKey(const ValueKey('discovery-map')), findsOneWidget);
    // India-wide, the three Pune machines overlap and are clustered.
    expect(find.bySemanticsLabel(RegExp(r'3 machines here')), findsOneWidget);
    await tapKey(tester, 'place-chip');
    await tapKey(tester, 'place-city-pune');
    expect(find.byKey(const ValueKey('pin-l-live')), findsOneWidget);
    expect(find.text('Approximate locations (±1 km)'), findsOneWidget);
    await tapKey(tester, 'pin-l-live');
    expect(find.byKey(const ValueKey('map-card-l-live')), findsOneWidget);

    await tester.tap(find.byTooltip('Zoom in'));
    await settle(tester);
    await tapKey(tester, 'search-area');
    final b = api.queries.last.bounds;
    expect(b, isNotNull);
    expect(b!.maxLat - b.minLat, lessThan(30));
    expect(find.byKey(const ValueKey('map-area-chip')), findsOneWidget);

    await tapKey(tester, 'view-toggle');
    expect(api.queries.last.bounds, isNull, reason: 'back to the list drops the map area');
  });

  testWidgets('recent searches can be repeated and cleared', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/search', role: UserRole.customer);
    await search(tester, 'roller');
    await search(tester, 'crane');
    expect(api.historyList.map((h) => h.label), ['"crane"', '"roller"']);

    // An empty search box shows recent searches.
    await tester.tap(find.byKey(const ValueKey('discover-search')));
    await tester.enterText(find.byKey(const ValueKey('discover-search')), '');
    await tester.pump(const Duration(milliseconds: 300));
    await settle(tester);
    expect(find.byKey(const ValueKey('history')), findsOneWidget);
    await tapOn(tester, find.text('"roller"'));
    expect(api.queries.last.text, 'roller');
    expect(shownTitles(api), ['Dynapac vibratory roller']);

    await tester.tap(find.byKey(const ValueKey('discover-search')));
    await tester.enterText(find.byKey(const ValueKey('discover-search')), '');
    await tester.pump(const Duration(milliseconds: 300));
    await settle(tester);
    await tapKey(tester, 'clear-history');
    expect(api.historyList, isEmpty);
  });

  testWidgets('listing detail offers save and compare and says the location is approximate', (tester) async {
    final api = FakeDiscoveryApi();
    final c = await pumpApp(tester, size: desktop, discovery: api);
    await goTo(tester, c, '/customer/equipment/l-live', role: UserRole.customer);
    expect(find.byKey(const ValueKey('approximate-location')), findsOneWidget);
    await tapKey(tester, 'save-l-live');
    expect(api.savedSet, {'l-live'});
    await tapKey(tester, 'compare-l-live');
    expect(c.read(compareSelectionProvider), ['l-live']);
  });

  for (final (name, size) in const [('phone', phone), ('tablet', tablet)]) {
    testWidgets('discovery screens fit on $name', (tester) async {
      final c = await pumpApp(tester, size: size);
      await goTo(tester, c, '/customer/search', role: UserRole.customer);
      await tapKey(tester, 'filters-button');
      expect(tester.takeException(), isNull);
      await tapKey(tester, 'filters-apply');
      await tapKey(tester, 'view-toggle');
      expect(tester.takeException(), isNull);
      await goTo(tester, c, '/customer/compare?ids=l-live,l-crane');
      expect(tester.takeException(), isNull);
    });
  }
}
