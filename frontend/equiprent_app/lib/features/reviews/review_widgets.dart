import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';

String _subjectPrompt(String subject) => switch (subject) {
      'equipment' => 'How was the machine?',
      'owner' => "How was the owner's service?",
      'operator' => 'How was the operator?',
      'customer' => 'How was this customer?',
      _ => 'Your rating',
    };

// ---- on a booking ---------------------------------------------------------------------------

/// The booking's reviews and a rating form for each subject the caller may
/// still review (customers: machine, owner service, operator; owners: customer).
class BookingReviewsSection extends ConsumerWidget {
  const BookingReviewsSection({super.key, required this.side, required this.bookingId});
  final BookingSide side;
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final reviews = ref.watch(bookingReviewsProvider((side, bookingId)));
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      const SectionHeader(title: 'Ratings & reviews', subtitle: 'Shared publicly after the rental'),
      reviews.when(
        loading: () => const SkeletonCard(),
        error: (e, _) => ErrorState(message: describeError(e), onRetry: () => ref.invalidate(bookingReviewsProvider((side, bookingId)))),
        data: (r) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          for (final s in r.canReview) Padding(padding: const EdgeInsets.only(bottom: AppSpacing.sm), child: _RateCard(side: side, bookingId: bookingId, subject: s)),
          for (final rev in r.items) Padding(padding: const EdgeInsets.only(bottom: AppSpacing.sm), child: ReviewTile(review: rev, showSubject: true)),
          if (r.items.isEmpty && r.canReview.isEmpty) const Text('No reviews on this booking.'),
        ]),
      ),
    ]);
  }
}

class _RateCard extends ConsumerStatefulWidget {
  const _RateCard({required this.side, required this.bookingId, required this.subject});
  final BookingSide side;
  final String bookingId;
  final String subject;
  @override
  ConsumerState<_RateCard> createState() => _RateCardState();
}

class _RateCardState extends ConsumerState<_RateCard> {
  int _rating = 0;
  final _comment = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _comment.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() => _busy = true);
    try {
      final r = await ref.read(commsApiProvider).createReview(ref.read(sideOrgProvider(widget.side)), widget.bookingId,
          subject: widget.subject, rating: _rating, comment: _comment.text.trim().isEmpty ? null : _comment.text.trim());
      if (!mounted) return;
      AppToast.success(context, r.status == 'pending' ? 'Thanks! Your review will appear after a quick check.' : 'Thanks for your review!');
      ref.invalidate(bookingReviewsProvider((widget.side, widget.bookingId)));
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AppCard(
      key: Key('rate-${widget.subject}'),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Text(_subjectPrompt(widget.subject), style: Theme.of(context).textTheme.titleSmall),
        StarRating(value: _rating.toDouble(), onChanged: (v) => setState(() => _rating = v.round())),
        AppTextField(
          key: Key('review-comment-${widget.subject}'),
          label: 'Written feedback (optional)',
          hint: 'What went well, what could be better?',
          controller: _comment,
          maxLines: 3,
          maxLength: 2000,
        ),
        const Gap(AppSpacing.sm),
        Align(
          alignment: Alignment.centerRight,
          child: AppButton(
            key: Key('submit-review-${widget.subject}'),
            label: 'Submit',
            size: AppButtonSize.small,
            isLoading: _busy,
            onPressed: _rating == 0 || _busy ? null : _submit,
          ),
        ),
      ]),
    );
  }
}

// ---- review display -------------------------------------------------------------------------

/// Compact read-only stars (the interactive [StarRating] uses buttons).
class Stars extends StatelessWidget {
  const Stars(this.value, {super.key, this.size = 16});
  final double value;
  final double size;

  @override
  Widget build(BuildContext context) => Semantics(
        label: '${value.toStringAsFixed(1)} out of 5 stars',
        excludeSemantics: true,
        child: Row(mainAxisSize: MainAxisSize.min, children: [
          for (var i = 1; i <= 5; i++)
            Icon(value >= i ? Icons.star_rounded : (value >= i - 0.5 ? Icons.star_half_rounded : Icons.star_outline_rounded),
                size: size, color: AppColors.orange500),
        ]),
      );
}

class ReviewTile extends StatelessWidget {
  const ReviewTile({super.key, required this.review, this.showSubject = false, this.actions = const []});
  final Review review;
  final bool showSubject;
  final List<Widget> actions;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final r = review;
    return AppCard(
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Stars(r.rating.toDouble()),
          const Gap(AppSpacing.xs),
          Expanded(
            child: Text(
              [if (showSubject) reviewSubjectLabel(r.subjectType), if (r.equipmentTitle != null && !showSubject) r.equipmentTitle!, r.reviewerName ?? ''].where((s) => s.isNotEmpty).join(' · '),
              maxLines: 1,
              overflow: TextOverflow.ellipsis,
              style: theme.textTheme.labelMedium?.copyWith(color: AppColors.slate600),
            ),
          ),
          Text(Fmt.shortDate(r.createdAt), style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
        ]),
        if (r.status != null && r.status != 'published')
          Padding(
            padding: const EdgeInsets.only(top: 4),
            child: StatusBadge(label: r.status == 'pending' ? 'Awaiting moderation' : 'Not public', tone: AppTone.warning),
          ),
        if (r.comment != null) ...[const Gap(AppSpacing.xs), Text(r.comment!)],
        if (r.reply != null)
          Container(
            margin: const EdgeInsets.only(top: AppSpacing.sm),
            padding: const EdgeInsets.all(AppSpacing.sm),
            decoration: const BoxDecoration(color: AppColors.slate100, borderRadius: AppRadius.control),
            child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
              Text('Reply from the owner', style: theme.textTheme.labelSmall?.copyWith(color: AppColors.slate500)),
              Text(r.reply!),
            ]),
          ),
        if (actions.isNotEmpty) Align(alignment: Alignment.centerRight, child: Wrap(spacing: AppSpacing.sm, children: actions)),
      ]),
    );
  }
}

class RatingSummaryCard extends StatelessWidget {
  const RatingSummaryCard({super.key, required this.summary, required this.title});
  final ReviewSummary summary;
  final String title;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final max = summary.histogram.values.fold<int>(0, (m, v) => v > m ? v : m);
    return AppCard(
      child: Row(children: [
        Column(children: [
          Text(summary.count == 0 ? '–' : summary.average.toStringAsFixed(1), style: theme.textTheme.displaySmall?.copyWith(fontWeight: FontWeight.w700)),
          Stars(summary.average, size: 14),
          Text('${summary.count} review${summary.count == 1 ? '' : 's'}', style: theme.textTheme.bodySmall),
        ]),
        const Gap(AppSpacing.lg),
        Expanded(
          child: Column(children: [
            for (var star = 5; star >= 1; star--)
              Padding(
                padding: const EdgeInsets.symmetric(vertical: 2),
                child: Row(children: [
                  SizedBox(width: 14, child: Text('$star', style: theme.textTheme.bodySmall)),
                  const Gap(6),
                  Expanded(
                    child: ClipRRect(
                      borderRadius: BorderRadius.circular(4),
                      child: LinearProgressIndicator(
                        value: max == 0 ? 0 : (summary.histogram[star] ?? 0) / max,
                        minHeight: 6,
                        backgroundColor: AppColors.slate100,
                        color: AppColors.orange500,
                      ),
                    ),
                  ),
                  const Gap(6),
                  SizedBox(width: 22, child: Text('${summary.histogram[star] ?? 0}', textAlign: TextAlign.right, style: theme.textTheme.bodySmall)),
                ]),
              ),
          ]),
        ),
      ]),
    );
  }
}

const _reportReasons = {
  'spam': 'Spam or advertising',
  'abusive': 'Abusive or offensive',
  'false_information': 'False information',
  'personal_information': 'Shares personal information',
  'irrelevant': 'Not about this rental',
  'other': 'Something else',
};

Future<void> reportReview(BuildContext context, WidgetRef ref, Review r) async {
  final reason = await showActionSheet<String>(context, title: 'Report this review', actions: [
    for (final e in _reportReasons.entries) SheetAction(value: e.key, label: e.value, icon: AppIcons.warning),
  ]);
  if (reason == null || !context.mounted) return;
  try {
    final counted = await ref.read(commsApiProvider).reportReview(r.id, reason);
    if (context.mounted) AppToast.show(context, counted ? 'Thanks. Our team will look at it.' : 'You have already reported this review.');
  } catch (e) {
    if (context.mounted) AppToast.error(context, describeError(e));
  }
}

/// Public ratings and reviews of a machine (listing detail).
class ListingReviewsSection extends ConsumerWidget {
  const ListingReviewsSection({super.key, required this.equipmentId});
  final String equipmentId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final page = ref.watch(equipmentReviewsProvider(equipmentId));
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
      const SectionHeader(title: 'Ratings & reviews', subtitle: 'From customers who rented this machine'),
      page.when(
        loading: () => const SkeletonCard(),
        error: (e, _) => ErrorState(message: describeError(e), onRetry: () => ref.invalidate(equipmentReviewsProvider(equipmentId))),
        data: (p) => Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          if (p.summary != null) RatingSummaryCard(summary: p.summary!, title: 'Machine'),
          const Gap(AppSpacing.sm),
          if (p.items.isEmpty) const Text('No reviews yet.'),
          for (final r in p.items)
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: ReviewTile(review: r, actions: [
                TextButton.icon(onPressed: () => reportReview(context, ref, r), icon: const Icon(AppIcons.warning, size: 16), label: const Text('Report')),
              ]),
            ),
        ]),
      ),
    ]);
  }
}

// ---- owner -------------------------------------------------------------------------------------

/// Reviews of the owner's service and machines, with public replies.
class OwnerReviewsPage extends ConsumerWidget {
  const OwnerReviewsPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final page = ref.watch(receivedReviewsProvider);
    return AppPage(
      title: 'Reviews',
      subtitle: page.value?.summary == null ? null : '${page.value!.summary!.count} service ratings',
      topLevel: false,
      body: AsyncBody(
        value: page,
        onRetry: () => ref.invalidate(receivedReviewsProvider),
        builder: (p) => PageBody(children: [
          if (p.summary != null) RatingSummaryCard(summary: p.summary!, title: 'Owner service'),
          const Gap(AppSpacing.md),
          if (p.items.isEmpty) const EmptyState(icon: Icons.star_rounded, title: 'No reviews yet', message: 'Customers can rate your machines and service after each rental.'),
          for (final r in p.items)
            Padding(
              padding: const EdgeInsets.only(bottom: AppSpacing.sm),
              child: ReviewTile(review: r, showSubject: true, actions: [
                if (r.reply == null)
                  TextButton.icon(
                    key: Key('reply-${r.id}'),
                    onPressed: () => _reply(context, ref, r),
                    icon: const Icon(AppIcons.chat, size: 16),
                    label: const Text('Reply'),
                  ),
              ]),
            ),
        ]),
      ),
    );
  }

  Future<void> _reply(BuildContext context, WidgetRef ref, Review r) async {
    final text = await askReason(context, title: 'Reply publicly', label: 'Your reply', confirm: 'Post reply', minLength: 2);
    if (text == null || !context.mounted) return;
    final ok = await runComms(context, () => ref.read(commsApiProvider).replyToReview(ref.read(sideOrgProvider(BookingSide.owner)), r.id, text),
        success: 'Reply posted.');
    if (ok) ref.invalidate(receivedReviewsProvider);
  }
}

// ---- admin moderation ---------------------------------------------------------------------------

class AdminReviewsPage extends ConsumerStatefulWidget {
  const AdminReviewsPage({super.key});
  @override
  ConsumerState<AdminReviewsPage> createState() => _AdminReviewsPageState();
}

class _AdminReviewsPageState extends ConsumerState<AdminReviewsPage> {
  String _filter = 'pending';
  static const _filters = ['pending', 'flagged', 'reported', 'hidden', 'all'];

  String _label(String f) => switch (f) {
        'pending' => 'Held by screening',
        'flagged' => 'Hidden by reports',
        'reported' => 'Reported',
        'hidden' => 'Removed',
        _ => 'All',
      };

  Future<void> _moderate(AdminReview r, bool publish) async {
    final reason = await askReason(context, title: publish ? 'Publish this review?' : 'Hide this review?', confirm: publish ? 'Publish' : 'Hide');
    if (reason == null || !mounted) return;
    final ok = await runComms(context, () => ref.read(commsApiProvider).moderateReview(r.id, publish: publish, reason: reason),
        success: publish ? 'Review published.' : 'Review hidden.');
    if (ok) ref.invalidate(moderationQueueReviewsProvider(_filter));
  }

  @override
  Widget build(BuildContext context) {
    final queue = ref.watch(moderationQueueReviewsProvider(_filter));
    return AppPage(
      title: 'Review moderation',
      body: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        Padding(
          padding: const EdgeInsets.fromLTRB(AppSpacing.md, AppSpacing.sm, AppSpacing.md, 0),
          child: AppChoiceChips<String>(options: _filters, selected: _filter, onSelected: (f) => setState(() => _filter = f), labelOf: _label),
        ),
        Expanded(
          child: AsyncBody(
            value: queue,
            onRetry: () => ref.invalidate(moderationQueueReviewsProvider(_filter)),
            builder: (items) => items.isEmpty
                ? const EmptyState(icon: AppIcons.check, title: 'Nothing to review', message: 'Held and reported reviews appear here.')
                : ListView(padding: const EdgeInsets.all(AppSpacing.md), children: [
                    for (final r in items)
                      Padding(
                        padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                        child: _AdminReviewCard(r: r, onPublish: r.status == 'published' ? null : () => _moderate(r, true), onHide: r.status == 'hidden' ? null : () => _moderate(r, false)),
                      ),
                  ]),
          ),
        ),
      ]),
    );
  }
}

class _AdminReviewCard extends StatelessWidget {
  const _AdminReviewCard({required this.r, this.onPublish, this.onHide});
  final AdminReview r;
  final VoidCallback? onPublish;
  final VoidCallback? onHide;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
        Row(children: [
          Stars(r.rating.toDouble()),
          const Gap(AppSpacing.xs),
          Expanded(child: Text('${reviewSubjectLabel(r.subjectType)}: ${r.subjectName}', maxLines: 1, overflow: TextOverflow.ellipsis, style: theme.textTheme.titleSmall)),
          StatusBadge(label: r.status, tone: r.status == 'published' ? AppTone.success : AppTone.warning),
        ]),
        Text('By ${r.reviewerName} · ${Fmt.dateTime(r.createdAt)}', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
        if (r.comment != null) ...[const Gap(AppSpacing.xs), Text(r.comment!)],
        const Gap(AppSpacing.xs),
        Wrap(spacing: AppSpacing.xs, runSpacing: AppSpacing.xs, children: [
          for (final f in r.screeningFlags) StatusBadge(label: f.replaceAll('_', ' '), tone: AppTone.danger, icon: AppIcons.warning),
          if (r.reportCount > 0) StatusBadge(label: '${r.reportCount} report${r.reportCount == 1 ? '' : 's'}: ${r.reportReasons.join(', ').replaceAll('_', ' ')}', tone: AppTone.warning),
        ]),
        if (r.moderationNote != null) Text('Last decision: ${r.moderationNote}', style: theme.textTheme.bodySmall),
        const Gap(AppSpacing.sm),
        Row(mainAxisAlignment: MainAxisAlignment.end, children: [
          AppButton(key: Key('hide-${r.id}'), label: 'Hide', size: AppButtonSize.small, variant: AppButtonVariant.secondary, onPressed: onHide),
          const Gap(AppSpacing.sm),
          AppButton(key: Key('publish-${r.id}'), label: 'Publish', size: AppButtonSize.small, onPressed: onPublish),
        ]),
      ]),
    );
  }
}
