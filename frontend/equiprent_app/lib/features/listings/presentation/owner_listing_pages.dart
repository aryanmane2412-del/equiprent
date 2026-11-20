import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/data/verification_api.dart'
    show documentPickerProvider, linkOpenerProvider;
import '../../verification/data/verification_models.dart' show PickedFile;
import '../data/listing_models.dart';
import '../data/listings_api.dart';
import 'listing_widgets.dart';

abstract final class OwnerListingRoutes {
  static const list = '/owner/listings';
  static const create = '/owner/listings/new';
  static String manage(String id) => '/owner/listings/$id';
  static String edit(String id) => '/owner/listings/$id/edit';
}

// ---- my equipment --------------------------------------------------------------------------

class MyListingsPage extends ConsumerStatefulWidget {
  const MyListingsPage({super.key});
  @override
  ConsumerState<MyListingsPage> createState() => _MyListingsPageState();
}

class _MyListingsPageState extends ConsumerState<MyListingsPage> {
  ListingStatus? _status;

  static const _filters = <ListingStatus?>[
    null,
    ListingStatus.draft,
    ListingStatus.pendingReview,
    ListingStatus.active,
    ListingStatus.rejected,
    ListingStatus.paused,
    ListingStatus.suspended,
  ];

  @override
  Widget build(BuildContext context) {
    final page = ref.watch(myListingsProvider(_status));
    void add() => context.push(OwnerListingRoutes.create);
    return AppPage(
      title: 'My equipment',
      subtitle: page.whenOrNull(
        data: (p) =>
            '${p.items.length} ${_status == null ? 'listings' : _status!.label.toLowerCase()}',
      ),
      actions: [
        Padding(
          padding: const EdgeInsets.only(right: AppSpacing.xs),
          child: AppButton(
            key: const Key('add-listing'),
            label: 'Add',
            icon: AppIcons.add,
            variant: AppButtonVariant.accent,
            size: AppButtonSize.small,
            onPressed: add,
          ),
        ),
      ],
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SizedBox(
            height: 48,
            child: ListView(
              scrollDirection: Axis.horizontal,
              padding: const EdgeInsets.symmetric(
                horizontal: AppSpacing.md,
                vertical: AppSpacing.xs,
              ),
              children: [
                for (final s in _filters)
                  Padding(
                    padding: const EdgeInsets.only(right: AppSpacing.xs),
                    child: ChoiceChip(
                      label: Text(s?.label ?? 'All'),
                      selected: _status == s,
                      onSelected: (_) => setState(() => _status = s),
                    ),
                  ),
              ],
            ),
          ),
          Expanded(
            child: AsyncBody(
              value: page,
              onRetry: () => ref.invalidate(myListingsProvider(_status)),
              builder: (p) => p.items.isEmpty
                  ? EmptyState(
                      icon: AppIcons.listings,
                      title: _status == null
                          ? 'List your first machine'
                          : 'Nothing here',
                      message: _status == null
                          ? 'Add details, photos and a price, then submit it for review.'
                          : 'No listings with this status.',
                      action: _status == null
                          ? AppButton(
                              label: 'Add equipment',
                              variant: AppButtonVariant.accent,
                              onPressed: add,
                            )
                          : null,
                    )
                  : PageBody(
                      children: [
                        ResponsiveGrid(
                          columns: const Responsive(
                            compact: 1,
                            medium: 2,
                            expanded: 3,
                          ),
                          children: [
                            for (final l in p.items)
                              ListingTile(
                                listing: l,
                                showStatus: true,
                                onTap: () => context.push(
                                  OwnerListingRoutes.manage(l.id),
                                ),
                              ),
                          ],
                        ),
                      ],
                    ),
            ),
          ),
        ],
      ),
    );
  }
}

// ---- create / edit ---------------------------------------------------------------------------

/// Creates a draft (when [id] is null) or edits a listing's details.
class ListingEditorPage extends ConsumerWidget {
  const ListingEditorPage({super.key, this.id});
  final String? id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cats = ref.watch(categoriesProvider);
    final cities = ref.watch(citiesProvider);
    final existing = id == null
        ? const AsyncData<Listing?>(null)
        : ref.watch(myListingProvider(id!)).whenData<Listing?>((l) => l);
    final all = switch ((cats, cities, existing)) {
      (
        AsyncData(value: final c),
        AsyncData(value: final ci),
        AsyncData(value: final e),
      ) =>
        AsyncData((c, ci, e)),
      (AsyncError(:final error, :final stackTrace), _, _) ||
      (_, AsyncError(:final error, :final stackTrace), _) ||
      (
        _,
        _,
        AsyncError(:final error, :final stackTrace),
      ) => AsyncError<(List<ListingCategory>, List<City>, Listing?)>(
        error,
        stackTrace,
      ),
      _ => const AsyncLoading<(List<ListingCategory>, List<City>, Listing?)>(),
    };
    return AppPage(
      title: id == null ? 'Add equipment' : 'Edit details',
      topLevel: false,
      body: AsyncBody(
        value: all,
        onRetry: () {
          ref.invalidate(categoriesProvider);
          ref.invalidate(citiesProvider);
          if (id != null) ref.invalidate(myListingProvider(id!));
        },
        builder: (v) =>
            _EditorForm(categories: v.$1, cities: v.$2, existing: v.$3),
      ),
    );
  }
}

class _EditorForm extends ConsumerStatefulWidget {
  const _EditorForm({
    required this.categories,
    required this.cities,
    this.existing,
  });
  final List<ListingCategory> categories;
  final List<City> cities;
  final Listing? existing;
  @override
  ConsumerState<_EditorForm> createState() => _EditorFormState();
}

class _EditorFormState extends ConsumerState<_EditorForm> {
  final _form = GlobalKey<FormState>();
  late final TextEditingController _title,
      _make,
      _model,
      _year,
      _reg,
      _desc,
      _area,
      _radius;
  ListingCategory? _category;
  City? _city;
  MachineCondition _condition = MachineCondition.good;
  bool _delivery = true, _wet = true, _dry = false;
  bool _saving = false;

  @override
  void initState() {
    super.initState();
    final e = widget.existing;
    _title = TextEditingController(text: e?.title);
    _make = TextEditingController(text: e?.makeName);
    _model = TextEditingController(text: e?.modelName);
    _year = TextEditingController(text: e?.year?.toString());
    _reg = TextEditingController(text: e?.registrationNo);
    _desc = TextEditingController(text: e?.description);
    _area = TextEditingController(text: e?.locationLabel);
    _radius = TextEditingController(text: '${e?.serviceRadiusKm ?? 50}');
    _category = widget.categories
        .where((c) => c.slug == e?.categorySlug)
        .firstOrNull;
    _city = widget.cities.where((c) => c.id == e?.cityId).firstOrNull;
    if (e != null) {
      _condition = e.condition;
      _delivery = e.delivery;
      _wet = e.wetHire;
      _dry = e.dryHire;
    }
  }

  @override
  void dispose() {
    for (final c in [
      _title,
      _make,
      _model,
      _year,
      _reg,
      _desc,
      _area,
      _radius,
    ]) {
      c.dispose();
    }
    super.dispose();
  }

  String? _required(String? v) =>
      (v == null || v.trim().isEmpty) ? 'Required' : null;

  Json _body() {
    final year = int.tryParse(_year.text.trim());
    final reg = _reg.text.replaceAll(RegExp(r'[\s-]'), '').toUpperCase();
    return {
      'category': _category!.slug,
      'manufacturer': _make.text.trim(),
      'model': _model.text.trim(),
      'title': _title.text.trim(),
      'description': _desc.text.trim(),
      'manufacture_year': ?year,
      if (reg.isNotEmpty) 'registration_no': reg,
      'condition': _condition.wire,
      'delivery_available': _delivery,
      'wet_hire': _wet,
      'dry_hire': _dry,
      'location_label': _area.text.trim(),
      'city_id': _city!.id,
      'service_radius_km': int.tryParse(_radius.text.trim()) ?? 50,
    };
  }

  Future<void> _save() async {
    if (!(_form.currentState?.validate() ?? false)) return;
    if (!_wet && !_dry) {
      AppToast.error(
        context,
        'Offer the machine with an operator, without one, or both.',
      );
      return;
    }
    setState(() => _saving = true);
    final api = ref.read(listingsApiProvider);
    final org = ref.read(listingOrgProvider);
    final e = widget.existing;
    try {
      final body = _body();
      final cityMoved = e == null || e.cityId != _city!.id;
      if (cityMoved) {
        body['base_location'] = GeoPoint(_city!.lat, _city!.lng).toJson();
      }
      final saved = e == null
          ? await api.create(org, body)
          : await api.update(org, e.id, e.version, body);
      ref.invalidate(myListingsProvider);
      ref.invalidate(myListingProvider(saved.id));
      if (!mounted) return;
      AppToast.success(
        context,
        e == null
            ? 'Draft saved. Add photos and a price next.'
            : 'Details updated',
      );
      if (e == null) {
        context.pushReplacement(OwnerListingRoutes.manage(saved.id));
      } else {
        context.pop();
      }
    } catch (err) {
      if (mounted) AppToast.error(context, describeError(err));
    } finally {
      if (mounted) setState(() => _saving = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final digits = [FilteringTextInputFormatter.digitsOnly];
    final machine = SectionCard(
      title: 'Machine',
      icon: AppIcons.excavator,
      child: Column(
        children: [
          AppDropdown<ListingCategory>(
            key: const Key('field-category'),
            label: 'Category',
            value: _category,
            items: widget.categories,
            itemLabel: (c) => c.name,
            onChanged: (c) => setState(() => _category = c),
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-make'),
            label: 'Manufacturer',
            hint: 'e.g. JCB, Tata Hitachi',
            controller: _make,
            validator: _required,
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-model'),
            label: 'Model',
            hint: 'e.g. 3DX Super',
            controller: _model,
            validator: _required,
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-year'),
            label: 'Manufacturing year',
            controller: _year,
            keyboardType: TextInputType.number,
            inputFormatters: digits,
            validator: (v) {
              if (v == null || v.isEmpty) return null;
              final y = int.tryParse(v) ?? 0;
              return y < 1980 || y > DateTime.now().year + 1
                  ? 'Enter a year between 1980 and ${DateTime.now().year + 1}'
                  : null;
            },
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-reg'),
            label: 'Registration number (optional)',
            hint: 'MH12KX4471',
            controller: _reg,
          ),
          const Gap(AppSpacing.ms),
          Align(
            alignment: Alignment.centerLeft,
            child: Text(
              'Condition',
              style: Theme.of(context).textTheme.titleSmall,
            ),
          ),
          const Gap(AppSpacing.xs),
          Align(
            alignment: Alignment.centerLeft,
            child: AppChoiceChips<MachineCondition>(
              options: MachineCondition.values,
              selected: _condition,
              labelOf: (c) => c.label,
              onSelected: (c) => setState(() => _condition = c),
            ),
          ),
        ],
      ),
    );
    final listing = SectionCard(
      title: 'Listing',
      icon: AppIcons.listings,
      child: Column(
        children: [
          AppTextField(
            key: const Key('field-title'),
            label: 'Listing title',
            hint: 'JCB 3DX backhoe loader with operator',
            controller: _title,
            validator: (v) =>
                (v ?? '').trim().length < 3 ? 'At least 3 characters' : null,
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-desc'),
            label: 'Description',
            controller: _desc,
            maxLines: 4,
          ),
        ],
      ),
    );
    final where = SectionCard(
      title: 'Location & delivery',
      icon: AppIcons.siteMarker,
      child: Column(
        children: [
          AppDropdown<City>(
            key: const Key('field-city'),
            label: 'City',
            value: _city,
            items: widget.cities,
            itemLabel: (c) => c.name,
            onChanged: (c) => setState(() => _city = c),
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-area'),
            label: 'Area shown to customers',
            hint: 'Hinjewadi, Pune',
            controller: _area,
            validator: (v) => (v ?? '').trim().length < 2 ? 'Required' : null,
          ),
          const Gap(AppSpacing.ms),
          AppTextField(
            key: const Key('field-radius'),
            label: 'Service radius (km)',
            controller: _radius,
            keyboardType: TextInputType.number,
            inputFormatters: digits,
          ),
          SwitchListTile(
            key: const Key('field-delivery'),
            contentPadding: EdgeInsets.zero,
            title: const Text('Delivery to site available'),
            value: _delivery,
            onChanged: (v) => setState(() => _delivery = v),
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Rent with operator'),
            value: _wet,
            onChanged: (v) => setState(() => _wet = v),
          ),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Rent machine only'),
            value: _dry,
            onChanged: (v) => setState(() => _dry = v),
          ),
        ],
      ),
    );
    return Form(
      key: _form,
      child: PageBody(
        children: [
          SplitLayout(
            main: Column(
              children: [machine, const Gap(AppSpacing.md), listing],
            ),
            side: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                where,
                const Gap(AppSpacing.md),
                AppButton(
                  key: const Key('save-listing'),
                  label: widget.existing == null
                      ? 'Save draft'
                      : 'Save changes',
                  icon: AppIcons.check,
                  expand: true,
                  isLoading: _saving,
                  onPressed: _category == null || _city == null ? null : _save,
                ),
                if (_category == null || _city == null)
                  Padding(
                    padding: const EdgeInsets.only(top: AppSpacing.xs),
                    child: Text(
                      'Choose a category and city to continue.',
                      style: Theme.of(context).textTheme.bodySmall,
                    ),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

// ---- manage ----------------------------------------------------------------------------------

/// One listing: status, photos, documents, specifications, prices and the
/// lifecycle actions (submit, pause, resume, delete).
class ManageListingPage extends ConsumerWidget {
  const ManageListingPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final l = ref.watch(myListingProvider(id));
    return AppPage(
      title: l.whenOrNull(data: (l) => l.title) ?? 'Listing',
      topLevel: false,
      body: AsyncBody(
        value: l,
        onRetry: () => ref.invalidate(myListingProvider(id)),
        builder: (l) => _ManageBody(l: l),
      ),
    );
  }
}

class _ManageBody extends ConsumerStatefulWidget {
  const _ManageBody({required this.l});
  final Listing l;
  @override
  ConsumerState<_ManageBody> createState() => _ManageBodyState();
}

class _ManageBodyState extends ConsumerState<_ManageBody> {
  bool _uploading = false;

  Listing get l => widget.l;
  ListingsApi get _api => ref.read(listingsApiProvider);
  String? get _org => ref.read(listingOrgProvider);

  void _refresh() {
    ref.invalidate(myListingProvider(l.id));
    ref.invalidate(myListingsProvider);
  }

  Future<void> _run(Future<void> Function() action, {String? success}) async {
    if (await runVerificationAction(context, action, success: success)) {
      _refresh();
    }
  }

  Future<void> _addPhotos() async {
    final files = await ref.read(documentPickerProvider).pickImages();
    if (files.isEmpty || !mounted) return;
    setState(() => _uploading = true);
    var added = 0;
    String? failure;
    for (final f in files) {
      try {
        final upload = await _api.upload('equipment_photo', f);
        await _api.addPhoto(_org, l.id, upload);
        added++;
      } catch (e) {
        failure = '${f.name}: ${describeError(e)}';
      }
    }
    if (!mounted) return;
    setState(() => _uploading = false);
    _refresh();
    if (failure != null) {
      AppToast.error(context, added == 0 ? failure : '$added added. $failure');
    } else {
      AppToast.success(
        context,
        added == 1 ? 'Photo added' : '$added photos added',
      );
    }
  }

  Future<void> _addDocument() async {
    final doc = await showAppSheet<_NewDoc>(
      context,
      title: 'Add equipment document',
      subtitle: 'Stored privately; only you and reviewers can open it.',
      builder: (_) => const _DocForm(),
    );
    if (doc == null || !mounted) return;
    await _run(() async {
      final upload = await _api.upload('equipment_document', doc.file);
      await _api.addDocument(
        _org,
        l.id,
        docType: doc.type,
        uploadId: upload,
        number: doc.number,
        validUntil: doc.validUntil,
      );
    }, success: 'Document added');
  }

  Future<void> _editPricing() async {
    final result = await showAppSheet<(List<PricingPlan>, Mobilisation?)>(
      context,
      title: 'Rental prices',
      subtitle: 'In rupees, before GST',
      builder: (_) => _PricingForm(listing: l),
    );
    if (result == null) return;
    await _run(
      () => _api.setPricing(_org, l.id, result.$1, result.$2),
      success: 'Prices saved',
    );
  }

  Future<void> _editSpecs() async {
    final cats =
        ref.read(categoriesProvider).value ?? const <ListingCategory>[];
    final schema =
        cats.where((c) => c.slug == l.categorySlug).firstOrNull?.specSchema ??
        const {};
    final specs = await showAppSheet<List<SpecValue>>(
      context,
      title: 'Specifications',
      builder: (_) => _SpecsForm(listing: l, schema: schema),
    );
    if (specs == null) return;
    await _run(
      () => _api.setSpecs(_org, l.id, specs),
      success: 'Specifications saved',
    );
  }

  Future<void> _act(OwnerAction a) async {
    final success = switch (a) {
      OwnerAction.submit => 'Submitted for review',
      OwnerAction.pause => 'Hidden from customers',
      OwnerAction.resume => 'Visible to customers again',
    };
    await _run(() => _api.act(_org, l.id, a), success: success);
  }

  Future<void> _delete() async {
    final ok = await showConfirmDialog(
      context,
      title: 'Delete this listing?',
      message:
          'Customers will no longer see it. Past bookings and invoices are kept.',
      confirmLabel: 'Delete',
      tone: AppTone.danger,
    );
    if (!ok || !mounted) return;
    if (await runVerificationAction(
      context,
      () => _api.delete(_org, l.id),
      success: 'Listing deleted',
    )) {
      ref.invalidate(myListingsProvider);
      if (mounted) context.go(OwnerListingRoutes.list);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final editable = l.editable && l.status != ListingStatus.suspended;
    final checks = [
      ('Machine details', true),
      ('At least one rental price', l.pricing.isNotEmpty),
      ('At least one photo', l.photos.isNotEmpty),
    ];
    final ready = checks.every((c) => c.$2);

    final banner = switch (l.status) {
      ListingStatus.draft => const InlineBanner(
        title: 'Draft',
        message:
            'Only you can see this. Complete the checklist and submit it for review.',
        tone: AppTone.neutral,
        icon: AppIcons.edit,
      ),
      ListingStatus.pendingReview => const InlineBanner(
        title: 'In review',
        message:
            'Our team checks new listings within one working day. Changes send it back for review.',
        tone: AppTone.info,
        icon: AppIcons.hourglass,
      ),
      ListingStatus.active => const InlineBanner(
        title: 'Live',
        message: 'Customers can find and book this machine.',
        tone: AppTone.success,
        icon: AppIcons.checkCircle,
      ),
      ListingStatus.paused => const InlineBanner(
        title: 'Paused',
        message: 'Hidden from search until you resume it.',
        tone: AppTone.neutral,
        icon: AppIcons.pause,
      ),
      ListingStatus.rejected => InlineBanner(
        title: 'Changes needed',
        message: l.moderationNote ?? 'Update the listing and submit it again.',
        tone: AppTone.warning,
        icon: AppIcons.warning,
      ),
      ListingStatus.suspended => InlineBanner(
        title: 'Suspended',
        message: l.moderationNote ?? 'Contact support.',
        tone: AppTone.danger,
        icon: AppIcons.ban,
      ),
      ListingStatus.removed || ListingStatus.retired => InlineBanner(
        title: l.status.label,
        message: l.moderationNote ?? 'This listing is no longer available.',
        tone: AppTone.danger,
        icon: AppIcons.ban,
      ),
    };

    final photos = SectionCard(
      title: 'Photos',
      subtitle: '${l.photos.length} of 20 · JPEG, PNG or WebP up to 10 MB',
      icon: AppIcons.camera,
      trailing: editable
          ? AppButton(
              key: const Key('add-photos'),
              label: 'Add photos',
              icon: AppIcons.upload,
              size: AppButtonSize.small,
              variant: AppButtonVariant.secondary,
              isLoading: _uploading,
              onPressed: _addPhotos,
            )
          : null,
      child: l.photos.isEmpty
          ? const EmptyState(
              icon: AppIcons.camera,
              title: 'No photos yet',
              message:
                  'Front, side and cabin photos get the most requests.',
              compact: true,
            )
          : ResponsiveGrid(
              columns: const Responsive(compact: 2, medium: 3, expanded: 4),
              spacing: AppSpacing.sm,
              children: [
                for (final p in l.photos)
                  Stack(
                    key: Key('photo-${p.id}'),
                    children: [
                      ListingImage(
                        categorySlug: l.categorySlug,
                        url: p.url,
                        borderRadius: BorderRadius.circular(8),
                      ),
                      if (p.isCover)
                        const Positioned(
                          left: 6,
                          top: 6,
                          child: StatusBadge(
                            label: 'Cover',
                            tone: AppTone.accent,
                          ),
                        ),
                      if (editable)
                        Positioned(
                          right: 0,
                          top: 0,
                          child: PopupMenuButton<String>(
                            tooltip: 'Photo actions',
                            icon: const Icon(
                              AppIcons.more,
                              color: Colors.white,
                              size: 18,
                            ),
                            onSelected: (v) => v == 'cover'
                                ? _run(
                                    () => _api.setCover(_org, l.id, p.id),
                                    success: 'Cover photo updated',
                                  )
                                : _run(
                                    () => _api.deletePhoto(_org, l.id, p.id),
                                    success: 'Photo removed',
                                  ),
                            itemBuilder: (_) => [
                              if (!p.isCover)
                                const PopupMenuItem(
                                  value: 'cover',
                                  child: Text('Make cover photo'),
                                ),
                              const PopupMenuItem(
                                value: 'delete',
                                child: Text('Remove'),
                              ),
                            ],
                          ),
                        ),
                    ],
                  ),
              ],
            ),
    );

    final details = SectionCard(
      title: 'Details',
      icon: AppIcons.excavator,
      trailing: editable
          ? TextButton(
              key: const Key('edit-details'),
              onPressed: () => context.push(OwnerListingRoutes.edit(l.id)),
              child: const Text('Edit'),
            )
          : null,
      child: Column(
        children: [
          InfoRow(label: 'Location', value: l.place.isEmpty ? '—' : l.place),
          InfoRow(
            label: 'Delivery',
            value: l.delivery
                ? 'Available within ${l.serviceRadiusKm} km'
                : 'Pick up from yard',
          ),
          InfoRow(
            label: 'Hire',
            value: [
              if (l.wetHire) 'With operator',
              if (l.dryHire) 'Machine only',
            ].join(' · '),
          ),
          if (l.registrationNo != null)
            InfoRow(label: 'Registration', value: l.registrationNo!),
          SpecsTable(listing: l),
          if (editable)
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                key: const Key('edit-specs'),
                onPressed: _editSpecs,
                icon: const Icon(AppIcons.ruler, size: 16),
                label: const Text('Edit specifications'),
              ),
            ),
        ],
      ),
    );

    final pricing = SectionCard(
      title: 'Rental prices',
      icon: AppIcons.tag,
      trailing: editable
          ? TextButton(
              key: const Key('edit-pricing'),
              onPressed: _editPricing,
              child: Text(l.pricing.isEmpty ? 'Add' : 'Edit'),
            )
          : null,
      child: PricingTable(listing: l),
    );

    final docs = SectionCard(
      title: 'Documents',
      subtitle:
          'At least one supporting equipment document is required for review.',
      icon: AppIcons.fileCheck,
      trailing: editable
          ? TextButton(
              key: const Key('add-document'),
              onPressed: _addDocument,
              child: const Text('Add'),
            )
          : null,
      child: l.documents.isEmpty
          ? const Text(
              'Add at least one current equipment document before submitting for review.',
            )
          : Column(
              children: [
                for (final d in l.documents)
                  ListRow(
                    key: Key('doc-${d.id}'),
                    dense: true,
                    leading: const Icon(AppIcons.receipt, size: 20),
                    title: d.label,
                    subtitle: [
                      ?d.number,
                      if (d.validUntil != null) 'Valid until ${d.validUntil}',
                      d.status == 'verified' ? 'Verified' : 'Awaiting review',
                    ].join(' · '),
                    trailing: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        IconButton(
                          tooltip: 'View document',
                          icon: const Icon(AppIcons.eye, size: 18),
                          onPressed: () =>
                              runVerificationAction(context, () async {
                                final url = await _api.documentLink(
                                  _org,
                                  l.id,
                                  d.id,
                                );
                                await ref.read(linkOpenerProvider)(url);
                              }),
                        ),
                        if (editable)
                          IconButton(
                            tooltip: 'Remove document',
                            icon: const Icon(AppIcons.delete, size: 18),
                            onPressed: () => _run(
                              () => _api.deleteDocument(_org, l.id, d.id),
                              success: 'Document removed',
                            ),
                          ),
                      ],
                    ),
                  ),
              ],
            ),
    );

    final canSubmit =
        l.status == ListingStatus.draft || l.status == ListingStatus.rejected;
    final side = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionCard(
          title: 'Status',
          icon: AppIcons.shield,
          trailing: ListingStatusBadge(l.status),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              for (final c in checks)
                Padding(
                  padding: const EdgeInsets.symmetric(vertical: 3),
                  child: Row(
                    children: [
                      Icon(
                        c.$2 ? AppIcons.checkCircle : AppIcons.hourglass,
                        size: 18,
                        color: c.$2 ? AppColors.green700 : AppColors.slate500,
                      ),
                      const Gap(AppSpacing.sm),
                      Expanded(child: Text(c.$1)),
                    ],
                  ),
                ),
              const Gap(AppSpacing.xs),
              Text(
                'Your business verification must be approved before a listing goes live.',
                style: theme.textTheme.bodySmall?.copyWith(
                  color: AppColors.slate500,
                ),
              ),
              const Gap(AppSpacing.sm),
              if (canSubmit)
                AppButton(
                  key: const Key('submit-listing'),
                  label: l.status == ListingStatus.rejected
                      ? 'Resubmit for review'
                      : 'Submit for review',
                  icon: AppIcons.send,
                  expand: true,
                  onPressed: ready ? () => _act(OwnerAction.submit) : null,
                ),
              if (l.status == ListingStatus.active)
                AppButton(
                  key: const Key('pause-listing'),
                  label: 'Pause listing',
                  icon: AppIcons.pause,
                  variant: AppButtonVariant.secondary,
                  expand: true,
                  onPressed: () => _act(OwnerAction.pause),
                ),
              if (l.status == ListingStatus.paused)
                AppButton(
                  key: const Key('resume-listing'),
                  label: 'Resume listing',
                  icon: AppIcons.play,
                  expand: true,
                  onPressed: () => _act(OwnerAction.resume),
                ),
              if (l.editable) ...[
                const Gap(AppSpacing.xs),
                AppButton(
                  key: const Key('delete-listing'),
                  label: 'Delete listing',
                  icon: AppIcons.delete,
                  variant: AppButtonVariant.ghost,
                  expand: true,
                  onPressed: _delete,
                ),
              ],
            ],
          ),
        ),
        const Gap(AppSpacing.md),
        pricing,
        const Gap(AppSpacing.md),
        docs,
      ],
    );

    return PageBody(
      children: [
        banner,
        const Gap(AppSpacing.md),
        SplitLayout(
          main: Column(children: [photos, const Gap(AppSpacing.md), details]),
          side: side,
        ),
      ],
    );
  }
}

// ---- sheets ----------------------------------------------------------------------------------

class _NewDoc {
  const _NewDoc(this.type, this.file, this.number, this.validUntil);
  final String type;
  final PickedFile file;
  final String? number;
  final String? validUntil;
}

class _DocForm extends ConsumerStatefulWidget {
  const _DocForm();
  @override
  ConsumerState<_DocForm> createState() => _DocFormState();
}

class _DocFormState extends ConsumerState<_DocForm> {
  String _type = 'rc';
  final _number = TextEditingController();
  DateTime? _until;
  PickedFile? _file;

  @override
  void dispose() {
    _number.dispose();
    super.dispose();
  }

  String _iso(DateTime d) =>
      '${d.year.toString().padLeft(4, '0')}-${d.month.toString().padLeft(2, '0')}-${d.day.toString().padLeft(2, '0')}';

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppDropdown<String>(
          label: 'Document type',
          value: _type,
          items: equipmentDocTypes.keys.toList(),
          itemLabel: (k) => equipmentDocTypes[k]!,
          onChanged: (v) => setState(() => _type = v ?? _type),
        ),
        const Gap(AppSpacing.ms),
        AppTextField(
          key: const Key('doc-number'),
          label: 'Document number (optional)',
          controller: _number,
        ),
        const Gap(AppSpacing.ms),
        PickerField(
          label: 'Valid until (optional)',
          value: _until == null ? null : Fmt.date(_until!),
          icon: AppIcons.calendar,
          onTap: () async {
            final now = DateTime.now();
            final d = await showDatePicker(
              context: context,
              firstDate: now,
              lastDate: DateTime(now.year + 15),
              initialDate: _until ?? now.add(const Duration(days: 365)),
            );
            if (d != null) setState(() => _until = d);
          },
        ),
        const Gap(AppSpacing.ms),
        AppButton(
          key: const Key('pick-doc-file'),
          label: _file?.name ?? 'Choose file (PDF or image)',
          icon: AppIcons.attach,
          variant: AppButtonVariant.secondary,
          onPressed: () async {
            final f = await ref.read(documentPickerProvider).pick();
            if (f != null) setState(() => _file = f);
          },
        ),
        const Gap(AppSpacing.md),
        AppButton(
          key: const Key('save-doc'),
          label: 'Upload document',
          icon: AppIcons.upload,
          onPressed: _file == null
              ? null
              : () => Navigator.of(context).pop(
                  _NewDoc(
                    _type,
                    _file!,
                    _number.text.trim().isEmpty ? null : _number.text.trim(),
                    _until == null ? null : _iso(_until!),
                  ),
                ),
        ),
      ],
    );
  }
}

class _PricingForm extends StatefulWidget {
  const _PricingForm({required this.listing});
  final Listing listing;
  @override
  State<_PricingForm> createState() => _PricingFormState();
}

class _PricingFormState extends State<_PricingForm> {
  static const _units = ['hour', 'day', 'week', 'month'];
  late final Map<String, TextEditingController> _rates;
  late final TextEditingController _delivery, _perKm, _freeKm, _bata;
  late bool _withOperator;

  @override
  void initState() {
    super.initState();
    final l = widget.listing;
    _withOperator = l.wetHire;
    String rate(String unit) {
      final p = l.pricing.where((p) => p.unit == unit).firstOrNull;
      return p == null ? '' : '${p.ratePaise ~/ 100}';
    }

    _rates = {for (final u in _units) u: TextEditingController(text: rate(u))};
    final bata = l.pricing
        .map((p) => p.operatorBataPerDayPaise)
        .fold<int>(0, (a, b) => a > b ? a : b);
    _bata = TextEditingController(text: bata == 0 ? '' : '${bata ~/ 100}');
    final m = l.mobilisation;
    _delivery = TextEditingController(
      text: m == null || m.fixedPaise == 0 ? '' : '${m.fixedPaise ~/ 100}',
    );
    _perKm = TextEditingController(
      text: m == null || m.perKmPaise == 0 ? '' : '${m.perKmPaise ~/ 100}',
    );
    _freeKm = TextEditingController(text: m == null ? '' : '${m.freeKm}');
  }

  @override
  void dispose() {
    for (final c in [..._rates.values, _delivery, _perKm, _freeKm, _bata]) {
      c.dispose();
    }
    super.dispose();
  }

  int _paise(TextEditingController c) =>
      (int.tryParse(c.text.trim()) ?? 0) * 100;

  @override
  Widget build(BuildContext context) {
    final digits = [FilteringTextInputFormatter.digitsOnly];
    final plans = [
      for (final u in _units)
        if (_paise(_rates[u]!) >= 100)
          PricingPlan(
            unit: u,
            withOperator: _withOperator,
            ratePaise: _paise(_rates[u]!),
            operatorBataPerDayPaise: _withOperator ? _paise(_bata) : 0,
          ),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (widget.listing.wetHire && widget.listing.dryHire)
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            title: const Text('Prices include an operator'),
            value: _withOperator,
            onChanged: (v) => setState(() => _withOperator = v),
          ),
        for (final u in _units) ...[
          AppTextField(
            key: Key('rate-$u'),
            label: 'Per $u (₹)',
            controller: _rates[u],
            prefixText: '₹ ',
            keyboardType: TextInputType.number,
            inputFormatters: digits,
            onChanged: (_) => setState(() {}),
          ),
          const Gap(AppSpacing.sm),
        ],
        if (_withOperator) ...[
          AppTextField(
            label: 'Operator bata per day (₹, optional)',
            controller: _bata,
            prefixText: '₹ ',
            keyboardType: TextInputType.number,
            inputFormatters: digits,
          ),
          const Gap(AppSpacing.sm),
        ],
        const Divider(),
        AppTextField(
          label: 'Delivery charge (₹, optional)',
          controller: _delivery,
          prefixText: '₹ ',
          keyboardType: TextInputType.number,
          inputFormatters: digits,
        ),
        const Gap(AppSpacing.sm),
        Row(
          children: [
            Expanded(
              child: AppTextField(
                label: 'Per km (₹)',
                controller: _perKm,
                keyboardType: TextInputType.number,
                inputFormatters: digits,
              ),
            ),
            const Gap(AppSpacing.sm),
            Expanded(
              child: AppTextField(
                label: 'Free km',
                controller: _freeKm,
                keyboardType: TextInputType.number,
                inputFormatters: digits,
              ),
            ),
          ],
        ),
        const Gap(AppSpacing.md),
        AppButton(
          key: const Key('save-pricing'),
          label: 'Save prices',
          onPressed: plans.isEmpty
              ? null
              : () {
                  final hasMob =
                      _delivery.text.isNotEmpty || _perKm.text.isNotEmpty;
                  Navigator.of(context).pop((
                    plans,
                    hasMob
                        ? Mobilisation(
                            fixedPaise: _paise(_delivery),
                            perKmPaise: _paise(_perKm),
                            freeKm: int.tryParse(_freeKm.text) ?? 0,
                          )
                        : null,
                  ));
                },
        ),
        if (plans.isEmpty)
          Padding(
            padding: const EdgeInsets.only(top: AppSpacing.xs),
            child: Text(
              'Enter at least one price.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ),
      ],
    );
  }
}

class _SpecsForm extends StatefulWidget {
  const _SpecsForm({required this.listing, required this.schema});
  final Listing listing;
  final Map<String, String> schema;
  @override
  State<_SpecsForm> createState() => _SpecsFormState();
}

class _SpecsFormState extends State<_SpecsForm> {
  late final List<(String, String, TextEditingController)>
  _rows; // key, type, value

  @override
  void initState() {
    super.initState();
    final existing = {for (final s in widget.listing.specs) s.key: s};
    final keys = {...widget.schema.keys, ...existing.keys};
    _rows = [
      for (final k in keys)
        (
          k,
          widget.schema[k] ?? (existing[k]?.text != null ? 'text' : 'number'),
          TextEditingController(
            text: existing[k] == null
                ? ''
                : (existing[k]!.text ?? _num(existing[k]!.number)),
          ),
        ),
    ];
  }

  static String _num(double? n) =>
      n == null ? '' : (n == n.roundToDouble() ? '${n.toInt()}' : '$n');

  @override
  void dispose() {
    for (final r in _rows) {
      r.$3.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        if (_rows.isEmpty)
          const Text('This category has no suggested specifications.'),
        for (final r in _rows) ...[
          AppTextField(
            key: Key('spec-${r.$1}'),
            label:
                SpecValue(key: r.$1).label +
                switch (SpecValue(key: r.$1, number: 0).value.split(' ')) {
                  [_, final u] => ' ($u)',
                  _ => '',
                },
            controller: r.$3,
            keyboardType: r.$2 == 'number'
                ? const TextInputType.numberWithOptions(decimal: true)
                : TextInputType.text,
          ),
          const Gap(AppSpacing.sm),
        ],
        AppButton(
          key: const Key('save-specs'),
          label: 'Save specifications',
          onPressed: () => Navigator.of(context).pop([
            for (final r in _rows)
              if (r.$3.text.trim().isNotEmpty)
                r.$2 == 'number' && double.tryParse(r.$3.text.trim()) != null
                    ? SpecValue(
                        key: r.$1,
                        number: double.parse(r.$3.text.trim()),
                      )
                    : SpecValue(key: r.$1, text: r.$3.text.trim()),
          ]),
        ),
      ],
    );
  }
}
