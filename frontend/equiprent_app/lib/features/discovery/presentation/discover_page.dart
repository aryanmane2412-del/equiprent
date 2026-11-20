import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart';
import '../../listings/data/listing_models.dart';
import '../../listings/data/listings_api.dart';
import '../../listings/presentation/listing_widgets.dart';
import '../data/discovery_api.dart';
import '../data/discovery_models.dart';
import 'discovery_map.dart';
import 'discovery_widgets.dart';

/// Customer discovery routes.
abstract final class DiscoverRoutes {
  static const search = '/customer/search';
  static const saved = '/customer/saved';
  static const compare = '/customer/compare';
  static String category(String slug) => '$search?category=$slug';
  static String detail(String id) => '/customer/equipment/$id';
  static String compareIds(List<String> ids) => '$compare?ids=${ids.join(',')}';
}

/// Search approved equipment: text with suggestions and history, location
/// (city, PIN code or device), filters, sorting, pages, and list or map view.
class DiscoverPage extends ConsumerStatefulWidget {
  const DiscoverPage({super.key});
  @override
  ConsumerState<DiscoverPage> createState() => _DiscoverPageState();
}

class _DiscoverPageState extends ConsumerState<DiscoverPage> {
  final _text = TextEditingController();
  final _focus = FocusNode();
  DiscoverQuery _q = const DiscoverQuery();
  String _typed = '';
  Timer? _debounce;
  bool _map = false;
  String? _selected; // map pin
  Uri? _appliedUri;

  @override
  void initState() {
    super.initState();
    _focus.addListener(() => setState(() {}));
  }

  @override
  void didChangeDependencies() {
    super.didChangeDependencies();
    // Deep links (home, categories): ?category=slug&q=text
    final uri = GoRouterState.of(context).uri;
    if (uri == _appliedUri) return;
    _appliedUri = uri;
    final p = uri.queryParameters;
    if (p.containsKey('category') || p.containsKey('q')) {
      final text = p['q'] ?? '';
      _text.text = text;
      _q = DiscoverQuery(text: text, categories: [?p['category']], place: _q.place);
    }
  }

  @override
  void dispose() {
    _debounce?.cancel();
    _text.dispose();
    _focus.dispose();
    super.dispose();
  }

  void _set(DiscoverQuery q) => setState(() {
        _q = q;
        _selected = null;
      });

  void _onTyping(String v) {
    _debounce?.cancel();
    _debounce = Timer(const Duration(milliseconds: 250), () {
      if (mounted) setState(() => _typed = v);
    });
  }

  void _submitText(String text) {
    _focus.unfocus();
    _set(_q.copyWith(text: text.trim(), sort: _q.sort == DiscoverSort.relevance && text.trim().isEmpty ? null : _q.sort));
  }

  void _applySuggestion(Suggestion s) {
    _focus.unfocus();
    _text.clear();
    _typed = '';
    switch (s.kind) {
      case 'category':
        _set(_q.copyWith(text: '', categories: [s.value]));
      case 'make':
        _set(_q.copyWith(text: '', make: s.value, model: null));
      case 'model':
        _text.text = s.label;
        _set(_q.copyWith(text: s.label));
      case 'city':
        _set(_q.copyWith(place: SearchPlace.city(s.value, s.label), radiusKm: null));
      case 'pincode':
        _set(_q.copyWith(place: SearchPlace.pincode(s.value, s.label), radiusKm: null));
    }
  }

  void _repeat(HistoryEntry h) {
    _focus.unfocus();
    final q = h.toQuery();
    _text.text = q.text;
    _set(q);
  }

  Future<void> _pickPlace() async {
    final picked = await showAppSheet<_PlaceChoice>(context, title: 'Search near', builder: (_) => _PlacePicker(current: _q.place));
    if (picked == null || !mounted) return;
    _set(_q.copyWith(place: picked.place, radiusKm: picked.place == null ? null : _q.radiusKm, bounds: null,
        sort: picked.place == null && _q.sort == DiscoverSort.distance ? null : _q.sort));
  }

  Future<void> _openFilters() async {
    final q = await showAppSheet<DiscoverQuery>(context, title: 'Filters', builder: (_) => _FilterSheet(query: _q));
    if (q != null) _set(q);
  }

  @override
  Widget build(BuildContext context) {
    final q = _map ? _q.copyWith(page: 1, pageSize: _q.bounds == null ? 50 : 200) : _q;
    final results = ref.watch(discoverProvider(q));
    final compare = ref.watch(compareSelectionProvider);
    final searching = _focus.hasFocus;

    return AppPage(
      title: 'Find equipment',
      subtitle: results.whenOrNull(data: (p) => '${p.total} ${p.total == 1 ? 'machine' : 'machines'}${p.origin == null ? '' : ' near ${p.origin!.label}'}'),
      actions: [
        IconButton(
          key: const Key('view-toggle'),
          tooltip: _map ? 'Show as list' : 'Show on map',
          icon: Icon(_map ? AppIcons.list : AppIcons.map),
          onPressed: () => setState(() {
            _map = !_map;
            _selected = null;
            if (!_map) _q = _q.copyWith(bounds: null);
          }),
        ),
      ],
      bottomBar: compare.isEmpty
          ? null
          : BottomActionBar.split(
              summary: Text('${compare.length} of ${CompareSelection.max} selected to compare', key: const Key('compare-count')),
              actions: [
                TextButton(onPressed: () => ref.read(compareSelectionProvider.notifier).clear(), child: const Text('Clear')),
                AppButton(
                  key: const Key('open-compare'),
                  label: 'Compare',
                  icon: AppIcons.compare,
                  onPressed: compare.length < 2 ? null : () => context.push(DiscoverRoutes.compareIds(compare)),
                ),
              ],
            ),
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(AppSpacing.md, AppSpacing.sm, AppSpacing.md, 0),
            child: TextField(
              key: const Key('discover-search'),
              controller: _text,
              focusNode: _focus,
              textInputAction: TextInputAction.search,
              decoration: InputDecoration(
                hintText: 'Machine, manufacturer, model, area or PIN',
                prefixIcon: const Icon(AppIcons.search, size: 20),
                suffixIcon: _text.text.isEmpty && _q.text.isEmpty
                    ? null
                    : IconButton(
                        tooltip: 'Clear search',
                        icon: const Icon(AppIcons.close, size: 18),
                        onPressed: () {
                          _text.clear();
                          _typed = '';
                          _submitText('');
                        },
                      ),
              ),
              onChanged: _onTyping,
              onSubmitted: _submitText,
            ),
          ),
          _Toolbar(
            query: _q,
            onPlace: _pickPlace,
            onClearPlace: () => _set(_q.copyWith(place: null, radiusKm: null, sort: _q.sort == DiscoverSort.distance ? null : _q.sort)),
            onFilters: _openFilters,
            onSort: (s) => _set(_q.copyWith(sort: s)),
            effectiveSort: results.value?.sort,
            onRemoveFilter: _set,
          ),
          Expanded(
            child: searching
                ? _SuggestPanel(typed: _typed, onSuggestion: _applySuggestion, onSubmit: () => _submitText(_text.text), onHistory: _repeat)
                : AsyncBody(
                    value: results,
                    onRetry: () => ref.invalidate(discoverProvider(q)),
                    builder: (page) => _map ? _mapView(page) : _listView(page),
                  ),
          ),
        ],
      ),
    );
  }

  Widget _listView(DiscoverResults page) {
    if (page.items.isEmpty) {
      return EmptyState(
        icon: AppIcons.search,
        title: 'No machines match',
        message: _q.place != null && _q.place!.cityId == null ? 'Try a wider distance, another location or fewer filters.' : 'Try other words or fewer filters.',
        action: AppButton(
          key: const Key('clear-filters'),
          label: 'Clear filters',
          variant: AppButtonVariant.secondary,
          onPressed: () {
            _text.clear();
            _set(const DiscoverQuery());
          },
        ),
      );
    }
    return PageBody(
      children: [
        if (page.fuzzy)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.md),
            child: InlineBanner(
              key: const Key('fuzzy-banner'),
              tone: AppTone.info,
              message: 'No exact matches for "${_q.text}". Showing machines with similar names.',
            ),
          ),
        ResponsiveGrid(
          columns: const Responsive(compact: 1, medium: 2, expanded: 3, large: 4),
          children: [for (final i in page.items) DiscoverTile(item: i, onTap: () => context.push(DiscoverRoutes.detail(i.id)))],
        ),
        const Gap(AppSpacing.md),
        Pager(page: page.page, pages: page.pages, onPage: (n) => _set(_q.copyWith(page: n))),
      ],
    );
  }

  Widget _mapView(DiscoverResults page) {
    final selected = page.items.where((i) => i.id == _selected).firstOrNull;
    return Stack(
      children: [
        Positioned.fill(
          child: DiscoveryMap(
            key: const Key('discovery-map'),
            items: page.items,
            origin: page.origin,
            radiusKm: page.origin == null || page.origin!.source == 'city' ? null : (_q.radiusKm ?? (page.origin!.source == 'pincode' ? 25 : 50)),
            selectedId: _selected,
            onSelect: (i) => setState(() => _selected = i.id),
            onSearchArea: (b) => _set(_q.copyWith(bounds: b)),
          ),
        ),
        if (_q.bounds != null)
          Positioned(
            top: AppSpacing.sm,
            left: AppSpacing.sm,
            child: InputChip(
              key: const Key('map-area-chip'),
              label: Text('This map area · ${page.total}'),
              onDeleted: () => _set(_q.copyWith(bounds: null)),
            ),
          ),
        if (selected != null)
          Positioned(
            left: AppSpacing.sm,
            right: AppSpacing.sm,
            bottom: AppSpacing.sm,
            child: Center(
              child: ConstrainedBox(
                constraints: const BoxConstraints(maxWidth: 520),
                child: _MapCard(item: selected, onClose: () => setState(() => _selected = null)),
              ),
            ),
          ),
      ],
    );
  }
}

class _MapCard extends StatelessWidget {
  const _MapCard({required this.item, required this.onClose});
  final DiscoverItem item;
  final VoidCallback onClose;

  @override
  Widget build(BuildContext context) {
    final l = item.card;
    final theme = Theme.of(context);
    return AppCard(
      key: Key('map-card-${item.id}'),
      padding: const EdgeInsets.all(AppSpacing.sm),
      onTap: () => context.push(DiscoverRoutes.detail(item.id)),
      child: Row(
        children: [
          SizedBox(width: 88, child: ListingImage(categorySlug: l.categorySlug, url: l.coverUrl, aspectRatio: 1, borderRadius: BorderRadius.circular(8))),
          const Gap(AppSpacing.sm),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                Text(l.title, maxLines: 2, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall),
                Text([l.place, if (item.distanceKm != null) distanceLabel(item.distanceKm!)].where((s) => s.isNotEmpty).join(' · '),
                    maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.bodySmall),
                Text(l.priceLabel ?? 'Price on request', style: theme.textTheme.titleSmall?.copyWith(color: AppColors.navy800)),
              ],
            ),
          ),
          Column(
            children: [
              IconButton(tooltip: 'Close', icon: const Icon(AppIcons.close, size: 18), onPressed: onClose),
              SaveButton(listingId: item.id),
            ],
          ),
        ],
      ),
    );
  }
}

// ---- toolbar ----------------------------------------------------------------------------------

class _Toolbar extends ConsumerWidget {
  const _Toolbar({
    required this.query,
    required this.onPlace,
    required this.onClearPlace,
    required this.onFilters,
    required this.onSort,
    required this.onRemoveFilter,
    this.effectiveSort,
  });

  final DiscoverQuery query;
  final VoidCallback onPlace;
  final VoidCallback onClearPlace;
  final VoidCallback onFilters;
  final ValueChanged<DiscoverSort> onSort;
  final ValueChanged<DiscoverQuery> onRemoveFilter;
  final DiscoverSort? effectiveSort;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final q = query;
    final cats = ref.watch(categoriesProvider).value ?? const <ListingCategory>[];
    final n = q.activeFilterCount;
    final sort = q.sort ?? effectiveSort ?? DiscoverSort.newest;
    String catName(String slug) => cats.where((c) => c.slug == slug).firstOrNull?.name ?? slug;

    final active = <(String, DiscoverQuery)>[
      for (final c in q.categories) (catName(c), q.copyWith(categories: [...q.categories]..remove(c))),
      if (q.make != null) (q.make!, q.copyWith(make: null)),
      if (q.priceMinRupees != null || q.priceMaxRupees != null)
        ('${q.priceMinRupees == null ? 'Up to' : rupees(q.priceMinRupees! * 100)}${q.priceMinRupees != null && q.priceMaxRupees != null ? '–' : ' '}${q.priceMaxRupees == null ? 'and up' : rupees(q.priceMaxRupees! * 100)}/${q.unit}',
            q.copyWith(priceMinRupees: null, priceMaxRupees: null)),
      if (q.radiusKm != null) ('Within ${q.radiusKm!.round()} km', q.copyWith(radiusKm: null)),
      if (q.ratingMin != null) ('★ ${q.ratingMin}+', q.copyWith(ratingMin: null)),
      if (q.availableFrom != null) ('Free ${Fmt.date(q.availableFrom!)}${q.availableTo == null ? '' : ' – ${Fmt.date(q.availableTo!)}'}', q.copyWith(availableFrom: null, availableTo: null)),
      for (final c in q.conditions) (c.label, q.copyWith(conditions: [...q.conditions]..remove(c))),
      if (q.deliveryOnly) ('Delivery', q.copyWith(deliveryOnly: false)),
      if (q.operator != OperatorFilter.any) (q.operator.label, q.copyWith(operator: OperatorFilter.any)),
      if (q.verifiedOnly) ('Verified owners', q.copyWith(verifiedOnly: false)),
    ];

    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SizedBox(
          height: 52,
          child: ListView(
            scrollDirection: Axis.horizontal,
            padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md, vertical: AppSpacing.xs),
            children: [
              InputChip(
                key: const Key('place-chip'),
                avatar: Icon(q.place?.isPoint ?? false ? AppIcons.locate : AppIcons.siteMarker, size: 16),
                label: Text(q.place?.label ?? 'Anywhere in India', overflow: TextOverflow.ellipsis),
                selected: q.place != null,
                showCheckmark: false,
                onPressed: onPlace,
                onDeleted: q.place == null ? null : onClearPlace,
              ),
              const Gap(AppSpacing.xs),
              ActionChip(
                key: const Key('filters-button'),
                avatar: const Icon(AppIcons.filter, size: 16),
                label: Text(n == 0 ? 'Filters' : 'Filters · $n'),
                onPressed: onFilters,
              ),
              const Gap(AppSpacing.xs),
              PopupMenuButton<DiscoverSort>(
                key: const Key('sort-menu'),
                tooltip: 'Sort',
                onSelected: onSort,
                itemBuilder: (_) => [
                  for (final s in DiscoverSort.values)
                    PopupMenuItem(
                      value: s,
                      enabled: switch (s) {
                        DiscoverSort.distance => q.place != null,
                        DiscoverSort.relevance => q.text.isNotEmpty,
                        _ => true,
                      },
                      child: Row(children: [
                        Expanded(child: Text(s.label)),
                        if (s == sort) const Icon(AppIcons.check, size: 16),
                      ]),
                    ),
                ],
                child: Chip(avatar: const Icon(AppIcons.sort, size: 16), label: Text(sort.label)),
              ),
            ],
          ),
        ),
        if (active.isNotEmpty)
          SizedBox(
            height: 40,
            child: ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
              children: [
                for (final (label, without) in active)
                  Padding(
                    padding: const EdgeInsets.only(right: AppSpacing.xs),
                    child: InputChip(label: Text(label), onDeleted: () => onRemoveFilter(without), visualDensity: VisualDensity.compact),
                  ),
                TextButton(key: const Key('clear-all-filters'), onPressed: () => onRemoveFilter(q.clearFilters()), child: const Text('Clear all')),
              ],
            ),
          ),
      ],
    );
  }
}

// ---- suggestions and history ------------------------------------------------------------------

class _SuggestPanel extends ConsumerWidget {
  const _SuggestPanel({required this.typed, required this.onSuggestion, required this.onSubmit, required this.onHistory});
  final String typed;
  final ValueChanged<Suggestion> onSuggestion;
  final VoidCallback onSubmit;
  final ValueChanged<HistoryEntry> onHistory;

  static IconData _icon(String kind) => switch (kind) {
        'category' => AppIcons.categories,
        'make' => AppIcons.building,
        'model' => AppIcons.excavator,
        'city' => AppIcons.siteMarker,
        _ => AppIcons.flag,
      };

  static String _kind(String kind) => switch (kind) {
        'category' => 'Category',
        'make' => 'Manufacturer',
        'model' => 'Model',
        'city' => 'City',
        _ => 'PIN code',
      };

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (typed.trim().length >= 2) {
      final s = ref.watch(suggestionsProvider(typed));
      return ListView(
        key: const Key('suggestions'),
        children: [
          ListTile(leading: const Icon(AppIcons.search), title: Text('Search for "${typed.trim()}"'), onTap: onSubmit),
          ...?s.value?.map((x) => ListTile(
                key: Key('suggestion-${x.kind}-${x.value}'),
                leading: Icon(_icon(x.kind)),
                title: Text(x.label),
                subtitle: Text([_kind(x.kind), ?x.detail].join(' · ')),
                onTap: () => onSuggestion(x),
              )),
          if (s.isLoading) const LinearProgressIndicator(),
        ],
      );
    }
    final history = ref.watch(searchHistoryProvider).value ?? const <HistoryEntry>[];
    if (history.isEmpty) {
      return const EmptyState(icon: AppIcons.search, title: 'Search equipment', message: 'Try "JCB 3DX", "crane Thane" or a PIN code like 411057.', compact: true);
    }
    return ListView(
      key: const Key('history'),
      children: [
        ListTile(
          title: Text('Recent searches', style: Theme.of(context).textTheme.titleSmall),
          trailing: TextButton(
            key: const Key('clear-history'),
            onPressed: () async {
              await runVerificationAction(context, () => ref.read(discoveryApiProvider).clearHistory());
              ref.invalidate(searchHistoryProvider);
            },
            child: const Text('Clear'),
          ),
        ),
        for (final h in history)
          ListTile(
            key: Key('history-${h.id}'),
            leading: const Icon(AppIcons.history),
            title: Text(h.label, maxLines: 1, overflow: TextOverflow.ellipsis),
            subtitle: Text('${h.resultCount} ${h.resultCount == 1 ? 'result' : 'results'}'),
            onTap: () => onHistory(h),
            trailing: IconButton(
              tooltip: 'Remove from history',
              icon: const Icon(AppIcons.close, size: 16),
              onPressed: () async {
                await runVerificationAction(context, () => ref.read(discoveryApiProvider).forget(h.id));
                ref.invalidate(searchHistoryProvider);
              },
            ),
          ),
      ],
    );
  }
}

// ---- location picker --------------------------------------------------------------------------

class _PlaceChoice {
  const _PlaceChoice(this.place);
  final SearchPlace? place;
}

class _PlacePicker extends ConsumerStatefulWidget {
  const _PlacePicker({this.current});
  final SearchPlace? current;
  @override
  ConsumerState<_PlacePicker> createState() => _PlacePickerState();
}

class _PlacePickerState extends ConsumerState<_PlacePicker> {
  String _q = '';
  bool _locating = false;
  String? _error;

  Future<void> _useDevice() async {
    setState(() {
      _locating = true;
      _error = null;
    });
    try {
      final p = await ref.read(deviceLocatorProvider)();
      if (mounted) Navigator.of(context).pop(_PlaceChoice(SearchPlace.point(p.lat, p.lng)));
    } catch (e) {
      if (mounted) setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _locating = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final places = ref.watch(placesProvider(_q));
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      mainAxisSize: MainAxisSize.min,
      children: [
        AppButton(
          key: const Key('use-my-location'),
          label: 'Use my location',
          icon: AppIcons.locate,
          variant: AppButtonVariant.secondary,
          isLoading: _locating,
          onPressed: _useDevice,
        ),
        if (_error != null) ...[
          const Gap(AppSpacing.xs),
          Text(_error!, key: const Key('location-error'), style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.red700)),
        ],
        const Gap(AppSpacing.sm),
        TextField(
          key: const Key('place-search'),
          autofocus: false,
          decoration: const InputDecoration(hintText: 'City, area or 6-digit PIN code', prefixIcon: Icon(AppIcons.search, size: 18)),
          inputFormatters: [LengthLimitingTextInputFormatter(40)],
          onChanged: (v) => setState(() => _q = v),
        ),
        const Gap(AppSpacing.xs),
        SizedBox(
          height: 280,
          child: places.when(
            loading: () => const LoadingView(),
            error: (e, _) => Text(describeError(e)),
            data: (list) => list.isEmpty
                ? const EmptyState(icon: AppIcons.siteMarker, title: 'No match', message: 'Try a city name or PIN code.', compact: true)
                : ListView(
                    children: [
                      for (final p in list)
                        ListTile(
                          key: Key('place-${p.value}'),
                          dense: true,
                          leading: Icon(p.kind == 'pincode' ? AppIcons.flag : AppIcons.building, size: 18),
                          title: Text(p.label),
                          onTap: () => Navigator.of(context).pop(_PlaceChoice(p.toPlace())),
                        ),
                    ],
                  ),
          ),
        ),
        if (widget.current != null)
          TextButton(key: const Key('place-anywhere'), onPressed: () => Navigator.of(context).pop(const _PlaceChoice(null)), child: const Text('Search anywhere')),
      ],
    );
  }
}

// ---- filters ----------------------------------------------------------------------------------

class _FilterSheet extends ConsumerStatefulWidget {
  const _FilterSheet({required this.query});
  final DiscoverQuery query;
  @override
  ConsumerState<_FilterSheet> createState() => _FilterSheetState();
}

class _FilterSheetState extends ConsumerState<_FilterSheet> {
  late DiscoverQuery _d = widget.query;
  late final _min = TextEditingController(text: widget.query.priceMinRupees?.toString());
  late final _max = TextEditingController(text: widget.query.priceMaxRupees?.toString());

  @override
  void dispose() {
    _min.dispose();
    _max.dispose();
    super.dispose();
  }

  Widget _title(String t) => Padding(
        padding: const EdgeInsets.only(top: AppSpacing.md, bottom: AppSpacing.xs),
        child: Text(t, style: Theme.of(context).textTheme.titleSmall),
      );

  @override
  Widget build(BuildContext context) {
    final cats = ref.watch(categoriesProvider).value ?? const <ListingCategory>[];
    final canDistance = _d.place != null && _d.place!.cityId == null;
    final digits = [FilteringTextInputFormatter.digitsOnly];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _title('Equipment category'),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final c in cats)
              FilterChip(
                key: Key('filter-cat-${c.slug}'),
                label: Text(c.name),
                selected: _d.categories.contains(c.slug),
                onSelected: (on) => setState(() => _d = _d.copyWith(categories: on ? [..._d.categories, c.slug] : ([..._d.categories]..remove(c.slug)))),
              ),
          ],
        ),
        _title('Rental price'),
        Row(
          children: [
            Expanded(child: AppTextField(key: const Key('filter-price-min'), label: 'Min ₹', controller: _min, keyboardType: TextInputType.number, inputFormatters: digits)),
            const Gap(AppSpacing.sm),
            Expanded(child: AppTextField(key: const Key('filter-price-max'), label: 'Max ₹', controller: _max, keyboardType: TextInputType.number, inputFormatters: digits)),
            const Gap(AppSpacing.sm),
            SizedBox(
              width: 110,
              child: AppDropdown<String>(
                label: 'Per',
                value: _d.unit,
                items: const ['hour', 'day', 'week', 'month'],
                onChanged: (u) => setState(() => _d = _d.copyWith(unit: u)),
              ),
            ),
          ],
        ),
        _title(canDistance ? 'Distance: within ${(_d.radiusKm ?? (_d.place!.pincode != null ? 25 : 50)).round()} km' : 'Distance'),
        if (canDistance)
          Slider(
            key: const Key('filter-distance'),
            value: (_d.radiusKm ?? (_d.place!.pincode != null ? 25 : 50)).clamp(5, 300).toDouble(),
            min: 5,
            max: 300,
            divisions: 59,
            label: '${(_d.radiusKm ?? 50).round()} km',
            onChanged: (v) => setState(() => _d = _d.copyWith(radiusKm: v.roundToDouble())),
          )
        else
          Text('Choose a PIN code or your location to filter by distance.', style: Theme.of(context).textTheme.bodySmall),
        _title('Rating'),
        Wrap(
          spacing: 6,
          children: [
            for (final r in const <double?>[null, 3.5, 4.0, 4.5])
              ChoiceChip(
                key: Key('filter-rating-${r ?? 'any'}'),
                label: Text(r == null ? 'Any' : '★ $r+'),
                selected: _d.ratingMin == r,
                onSelected: (_) => setState(() => _d = _d.copyWith(ratingMin: r)),
              ),
          ],
        ),
        _title('Availability'),
        PickerField(
          label: 'Free on',
          icon: AppIcons.calendar,
          placeholder: 'Any dates',
          value: _d.availableFrom == null ? null : '${Fmt.date(_d.availableFrom!)}${_d.availableTo == null ? '' : ' – ${Fmt.date(_d.availableTo!)}'}',
          onTap: () async {
            final now = DateTime.now();
            final r = await showDateRangePicker(context: context, firstDate: DateTime(now.year, now.month, now.day), lastDate: now.add(const Duration(days: 365)));
            if (r != null) setState(() => _d = _d.copyWith(availableFrom: r.start, availableTo: r.end));
          },
        ),
        if (_d.availableFrom != null)
          Align(
            alignment: Alignment.centerLeft,
            child: TextButton(onPressed: () => setState(() => _d = _d.copyWith(availableFrom: null, availableTo: null)), child: const Text('Any dates')),
          ),
        _title('Condition'),
        Wrap(
          spacing: 6,
          runSpacing: 6,
          children: [
            for (final c in MachineCondition.values)
              FilterChip(
                key: Key('filter-cond-${c.wire}'),
                label: Text(c.label),
                selected: _d.conditions.contains(c),
                onSelected: (on) => setState(() => _d = _d.copyWith(conditions: on ? [..._d.conditions, c] : ([..._d.conditions]..remove(c)))),
              ),
          ],
        ),
        _title('Operator'),
        Wrap(
          spacing: 6,
          children: [
            for (final o in OperatorFilter.values)
              ChoiceChip(
                key: Key('filter-op-${o.name}'),
                label: Text(o.label),
                selected: _d.operator == o,
                onSelected: (_) => setState(() => _d = _d.copyWith(operator: o)),
              ),
          ],
        ),
        SwitchListTile(
          key: const Key('filter-delivery'),
          contentPadding: EdgeInsets.zero,
          title: const Text('Delivery to site available'),
          value: _d.deliveryOnly,
          onChanged: (v) => setState(() => _d = _d.copyWith(deliveryOnly: v)),
        ),
        SwitchListTile(
          key: const Key('filter-verified'),
          contentPadding: EdgeInsets.zero,
          title: const Text('Verified owners only'),
          value: _d.verifiedOnly,
          onChanged: (v) => setState(() => _d = _d.copyWith(verifiedOnly: v)),
        ),
        const Gap(AppSpacing.md),
        Row(
          children: [
            Expanded(
              child: AppButton(
                key: const Key('filters-reset'),
                label: 'Reset',
                variant: AppButtonVariant.secondary,
                onPressed: () => Navigator.of(context).pop(widget.query.clearFilters()),
              ),
            ),
            const Gap(AppSpacing.sm),
            Expanded(
              child: AppButton(
                key: const Key('filters-apply'),
                label: 'Show results',
                onPressed: () {
                  final min = int.tryParse(_min.text), max = int.tryParse(_max.text);
                  if (min != null && max != null && min > max) {
                    AppToast.error(context, 'The minimum price is above the maximum.');
                    return;
                  }
                  Navigator.of(context).pop(_d.copyWith(priceMinRupees: min, priceMaxRupees: max));
                },
              ),
            ),
          ],
        ),
      ],
    );
  }
}
