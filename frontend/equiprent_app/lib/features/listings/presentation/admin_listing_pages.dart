import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/data/verification_api.dart' show linkOpenerProvider;
import '../data/listing_models.dart';
import '../data/listings_api.dart';
import 'listing_widgets.dart';

abstract final class AdminListingRoutes {
  static const queue = '/admin/listings';
  static String review(String id) => '/admin/listings/$id';
}

/// Listings by moderation state; new submissions first.
class ListingModerationPage extends ConsumerStatefulWidget {
  const ListingModerationPage({super.key});
  @override
  ConsumerState<ListingModerationPage> createState() =>
      _ListingModerationPageState();
}

class _ListingModerationPageState extends ConsumerState<ListingModerationPage> {
  ListingStatus _status = ListingStatus.pendingReview;
  static const _tabs = [
    ListingStatus.pendingReview,
    ListingStatus.active,
    ListingStatus.rejected,
    ListingStatus.suspended,
    ListingStatus.removed,
  ];

  @override
  Widget build(BuildContext context) {
    final page = ref.watch(moderationQueueProvider(_status));
    return AppPage(
      title: 'Listing moderation',
      subtitle: page.whenOrNull(
        data: (p) =>
            '${p.items.length}${p.nextCursor != null ? '+' : ''} ${_status.label.toLowerCase()}',
      ),
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
                for (final s in _tabs)
                  Padding(
                    padding: const EdgeInsets.only(right: AppSpacing.xs),
                    child: ChoiceChip(
                      key: Key('queue-${s.wire}'),
                      label: Text(s.label),
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
              onRetry: () => ref.invalidate(moderationQueueProvider(_status)),
              builder: (p) => p.items.isEmpty
                  ? EmptyState(
                      icon: AppIcons.checkCircle,
                      title: 'Nothing ${_status.label.toLowerCase()}',
                      message: _status == ListingStatus.pendingReview
                          ? 'New submissions appear here.'
                          : null,
                    )
                  : PageBody(
                      children: [
                        ResponsiveGrid(
                          columns: const Responsive(
                            compact: 1,
                            medium: 2,
                            expanded: 3,
                            large: 4,
                          ),
                          children: [
                            for (final l in p.items)
                              ListingTile(
                                listing: l,
                                showStatus: true,
                                onTap: () => context.push(
                                  AdminListingRoutes.review(l.id),
                                ),
                                trailing: l.ownerVerified
                                    ? null
                                    : const Tooltip(
                                        message: 'Owner not verified',
                                        child: Icon(
                                          AppIcons.warning,
                                          size: 18,
                                          color: AppColors.orange700,
                                        ),
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

/// One listing as the reviewer sees it, documents included.
class ListingReviewPage extends ConsumerWidget {
  const ListingReviewPage({super.key, required this.id});
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final listing = ref.watch(reviewListingProvider(id));
    return AppPage(
      title: listing.whenOrNull(data: (l) => l.title) ?? 'Review listing',
      topLevel: false,
      body: AsyncBody(
        value: listing,
        onRetry: () => ref.invalidate(reviewListingProvider(id)),
        builder: (l) => _ReviewBody(l: l),
      ),
    );
  }
}

class _ReviewBody extends ConsumerWidget {
  const _ReviewBody({required this.l});
  final Listing l;

  Future<void> _moderate(
    BuildContext context,
    WidgetRef ref,
    ModerationAction a,
  ) async {
    final needsReason = a == ModerationAction.reject;
    String? reason;
    if (needsReason) {
      reason = await showAppSheet<String>(
        context,
        title: switch (a) {
          ModerationAction.reject => 'Send back for changes',
          _ => 'Review listing',
        },
        subtitle: 'The owner sees this reason.',
        builder: (_) => _ReasonForm(action: a),
      );
      if (reason == null) return;
    } else {
      final ok = await showConfirmDialog(
        context,
        title: a == ModerationAction.approve
            ? 'Approve and publish?'
            : 'Reinstate listing?',
        message: 'Customers will be able to find and book this machine.',
        confirmLabel: a == ModerationAction.approve ? 'Approve' : 'Reinstate',
        tone: AppTone.success,
      );
      if (!ok) return;
    }
    if (!context.mounted) return;
    final done = await runVerificationAction(
      context,
      () => ref.read(listingsApiProvider).moderate(l.id, a, reason: reason),
      success: switch (a) {
        ModerationAction.approve => 'Listing approved and live',
        ModerationAction.reject => 'Sent back to the owner',
        ModerationAction.suspend => 'Listing suspended',
        ModerationAction.reinstate => 'Listing reinstated',
        ModerationAction.remove => 'Listing removed',
      },
    );
    if (done) {
      ref.invalidate(reviewListingProvider(l.id));
      ref.invalidate(moderationQueueProvider);
      ref.invalidate(browseProvider);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final s = l.status;
    final actions = <Widget>[
      if (s == ListingStatus.pendingReview) ...[
        AppButton(
          key: const Key('approve-listing'),
          label: 'Approve',
          icon: AppIcons.check,
          variant: AppButtonVariant.success,
          expand: true,
          onPressed: () => _moderate(context, ref, ModerationAction.approve),
        ),
        AppButton(
          key: const Key('reject-listing'),
          label: 'Request changes',
          icon: AppIcons.edit,
          variant: AppButtonVariant.secondary,
          expand: true,
          onPressed: () => _moderate(context, ref, ModerationAction.reject),
        ),
      ],
    ];

    final main = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ListingGallery(listing: l),
        const Gap(AppSpacing.md),
        Text(l.title, style: theme.textTheme.headlineSmall),
        Text(
          [
            l.machine,
            if (l.year != null) '${l.year}',
            if (l.place.isNotEmpty) l.place,
          ].join(' · '),
          style: theme.textTheme.bodyMedium?.copyWith(
            color: AppColors.slate600,
          ),
        ),
        if (l.description.isNotEmpty) ...[
          const Gap(AppSpacing.sm),
          Text(l.description),
        ],
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Specifications',
          icon: AppIcons.ruler,
          child: SpecsTable(listing: l),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Prices',
          icon: AppIcons.tag,
          child: PricingTable(listing: l),
        ),
      ],
    );
    final side = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionCard(
          title: 'Decision',
          icon: AppIcons.approvals,
          trailing: ListingStatusBadge(s),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.stretch,
            children: [
              if (l.moderationNote != null) ...[
                Text(
                  'Last note: ${l.moderationNote}',
                  style: theme.textTheme.bodySmall,
                ),
                const Gap(AppSpacing.sm),
              ],
              for (final (i, a) in actions.indexed) ...[
                if (i > 0) const Gap(AppSpacing.xs),
                a,
              ],
              if (actions.isEmpty) const Text('No actions for this state.'),
            ],
          ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Owner',
          icon: AppIcons.building,
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(l.ownerName, style: theme.textTheme.titleSmall),
              const Gap(AppSpacing.xs),
              OwnerTrustTag(verified: l.ownerVerified),
              if (l.registrationNo != null) ...[
                const Gap(AppSpacing.xs),
                Text('Registration ${l.registrationNo}'),
              ],
            ],
          ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Documents',
          icon: AppIcons.fileCheck,
          child: l.documents.isEmpty
              ? const Text('No machine documents uploaded.')
              : Column(
                  children: [
                    for (final d in l.documents)
                      ListRow(
                        dense: true,
                        leading: const Icon(AppIcons.receipt, size: 20),
                        title: d.label,
                        subtitle: [
                          ?d.number,
                          if (d.validUntil != null)
                            'Valid until ${d.validUntil}',
                        ].join(' · '),
                        trailing: d.downloadUrl == null
                            ? null
                            : IconButton(
                                tooltip: 'Open document',
                                icon: const Icon(AppIcons.eye, size: 18),
                                onPressed: () =>
                                    runVerificationAction(context, () async {
                                      await ref.read(linkOpenerProvider)(
                                        d.downloadUrl!,
                                      );
                                    }),
                              ),
                      ),
                  ],
                ),
        ),
      ],
    );
    return PageBody(
      children: [SplitLayout(main: main, side: side)],
    );
  }
}

class _ReasonForm extends StatefulWidget {
  const _ReasonForm({required this.action});
  final ModerationAction action;
  @override
  State<_ReasonForm> createState() => _ReasonFormState();
}

class _ReasonFormState extends State<_ReasonForm> {
  final _c = TextEditingController();
  static const _presets = {
    ModerationAction.reject: [
      'Photos are unclear or not of the actual machine',
      'Price looks incorrect',
      'Model or year does not match documents',
    ],
    ModerationAction.suspend: [
      'Customer complaints under investigation',
      'Documents expired',
    ],
    ModerationAction.remove: [
      'Prohibited equipment',
      'Fraudulent or duplicate listing',
      'Stock photos of a machine the owner does not have',
    ],
  };

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) => Column(
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      Wrap(
        spacing: 6,
        runSpacing: 6,
        children: [
          for (final p in _presets[widget.action] ?? const <String>[])
            ActionChip(
              label: Text(p),
              onPressed: () => setState(() => _c.text = p),
            ),
        ],
      ),
      const Gap(AppSpacing.sm),
      AppTextField(
        key: const Key('moderation-reason'),
        label: 'Reason',
        controller: _c,
        maxLines: 3,
        onChanged: (_) => setState(() {}),
      ),
      const Gap(AppSpacing.md),
      AppButton(
        key: const Key('confirm-moderation'),
        label: 'Confirm',
        variant: widget.action == ModerationAction.reject
            ? AppButtonVariant.primary
            : AppButtonVariant.danger,
        onPressed: _c.text.trim().length < 5
            ? null
            : () => Navigator.of(context).pop(_c.text.trim()),
      ),
    ],
  );
}

// ---- categories ------------------------------------------------------------------------------

class CategoryAdminPage extends ConsumerWidget {
  const CategoryAdminPage({super.key});

  Future<void> _edit(
    BuildContext context,
    WidgetRef ref, [
    ListingCategory? c,
  ]) async {
    final body = await showAppSheet<Json>(
      context,
      title: c == null ? 'New category' : 'Edit ${c.name}',
      builder: (_) => _CategoryForm(existing: c),
    );
    if (body == null || !context.mounted) return;
    final api = ref.read(listingsApiProvider);
    final ok = await runVerificationAction(
      context,
      () =>
          c == null ? api.createCategory(body) : api.updateCategory(c.id, body),
      success: c == null ? 'Category added' : 'Category saved',
    );
    if (ok) {
      ref.invalidate(adminCategoriesProvider);
      ref.invalidate(categoriesProvider);
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final cats = ref.watch(adminCategoriesProvider);
    return AppPage(
      title: 'Equipment categories',
      topLevel: false,
      actions: [
        Padding(
          padding: const EdgeInsets.only(right: AppSpacing.xs),
          child: AppButton(
            key: const Key('add-category'),
            label: 'Add',
            icon: AppIcons.add,
            size: AppButtonSize.small,
            variant: AppButtonVariant.accent,
            onPressed: () => _edit(context, ref),
          ),
        ),
      ],
      body: AsyncBody(
        value: cats,
        onRetry: () => ref.invalidate(adminCategoriesProvider),
        builder: (list) => PageBody(
          children: [
            AppCard(
              padding: EdgeInsets.zero,
              child: Column(
                children: [
                  for (final (i, c) in list.indexed) ...[
                    if (i > 0) const Divider(height: 1),
                    ListRow(
                      key: Key('admin-category-${c.slug}'),
                      leading: IconTile(icon: c.icon, size: 36),
                      title: c.name,
                      subtitle:
                          '${c.slug} · ${c.listingCount} live · ${c.specSchema.length} spec fields',
                      trailing: Row(
                        mainAxisSize: MainAxisSize.min,
                        children: [
                          StatusBadge(
                            label: c.isActive ? 'Active' : 'Hidden',
                            tone: c.isActive
                                ? AppTone.success
                                : AppTone.neutral,
                          ),
                          IconButton(
                            tooltip: 'Edit ${c.name}',
                            icon: const Icon(AppIcons.edit, size: 18),
                            onPressed: () => _edit(context, ref, c),
                          ),
                        ],
                      ),
                    ),
                  ],
                ],
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _CategoryForm extends StatefulWidget {
  const _CategoryForm({this.existing});
  final ListingCategory? existing;
  @override
  State<_CategoryForm> createState() => _CategoryFormState();
}

class _CategoryFormState extends State<_CategoryForm> {
  late final _name = TextEditingController(text: widget.existing?.name);
  late final _hi = TextEditingController(text: widget.existing?.nameHi);
  late final _slug = TextEditingController(text: widget.existing?.slug);
  late final _order = TextEditingController(
    text: '${widget.existing?.sortOrder ?? 100}',
  );
  late final _specs = TextEditingController(
    text: widget.existing?.specSchema.keys.join(', '),
  );
  late bool _active = widget.existing?.isActive ?? true;

  @override
  void dispose() {
    for (final c in [_name, _hi, _slug, _order, _specs]) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final creating = widget.existing == null;
    final old = widget.existing?.specSchema ?? const {};
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppTextField(
          key: const Key('category-name'),
          label: 'Name (English)',
          controller: _name,
          onChanged: (_) => setState(() {}),
        ),
        const Gap(AppSpacing.sm),
        AppTextField(label: 'Name (Hindi, optional)', controller: _hi),
        if (creating) ...[
          const Gap(AppSpacing.sm),
          AppTextField(
            key: const Key('category-slug'),
            label: 'Slug',
            hint: 'mini-excavator',
            controller: _slug,
          ),
        ],
        const Gap(AppSpacing.sm),
        AppTextField(
          label: 'Sort order',
          controller: _order,
          keyboardType: TextInputType.number,
          inputFormatters: [FilteringTextInputFormatter.digitsOnly],
        ),
        const Gap(AppSpacing.sm),
        AppTextField(
          label: 'Spec fields (comma separated)',
          hint: 'operating_weight_kg, bucket_capacity_m3',
          controller: _specs,
          maxLines: 2,
        ),
        SwitchListTile(
          contentPadding: EdgeInsets.zero,
          title: const Text('Shown to customers'),
          value: _active,
          onChanged: (v) => setState(() => _active = v),
        ),
        const Gap(AppSpacing.sm),
        AppButton(
          key: const Key('save-category'),
          label: 'Save',
          onPressed: _name.text.trim().length < 2
              ? null
              : () {
                  final keys = _specs.text
                      .split(',')
                      .map((s) => s.trim())
                      .where((s) => s.isNotEmpty);
                  Navigator.of(context).pop(<String, dynamic>{
                    'name_en': _name.text.trim(),
                    if (_hi.text.trim().isNotEmpty) 'name_hi': _hi.text.trim(),
                    if (creating && _slug.text.trim().isNotEmpty)
                      'slug': _slug.text.trim(),
                    'sort_order': int.tryParse(_order.text) ?? 100,
                    'is_active': _active,
                    'spec_schema': {
                      for (final k in keys)
                        k:
                            old[k] ??
                            (RegExp(r'_(kg|m|m3|hp|t|mm|kva|kw|l)$').hasMatch(k)
                                ? 'number'
                                : 'text'),
                    },
                  });
                },
        ),
      ],
    );
  }
}
