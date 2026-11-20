import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/session/user_role.dart';
import '../comms/presentation/ui.dart';
import '../comms/comms_routes.dart';
import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';

export 'booking_detail_page.dart';

UserRole _role(BookingSide side) =>
    side == BookingSide.owner ? UserRole.owner : UserRole.customer;

// ---- list ------------------------------------------------------------------------------------

enum _Filter { all, action, ongoing, past }

/// The customer's bookings or the owner's booking inbox.
class BookingListPage extends ConsumerStatefulWidget {
  const BookingListPage({super.key, required this.side});
  final BookingSide side;
  @override
  ConsumerState<BookingListPage> createState() => _BookingListPageState();
}

class _BookingListPageState extends ConsumerState<BookingListPage> {
  _Filter _filter = _Filter.all;

  static const _ongoing = {
    'confirmed',
    'operator_assigned',
    'dispatched',
    'on_site',
    'active',
    'extension_requested',
    'return_initiated',
    'returned', //
    'cancellation_requested', 'disputed',
  };
  static const _past = {
    'completed',
    'closed',
    'rejected',
    'expired',
    'cancelled',
    'refund_pending',
    'refunded',
  };

  // What waits for this side (the server repeats the check on every action).
  static const _ownerTodo = {
    'requested',
    'quoted',
    'cancellation_requested',
    'extension_requested',
    'return_initiated',
    'returned',
  };
  static const _customerTodo = {
    'draft',
    'accepted',
    'payment_pending',
    'on_site',
    'extension_requested',
  };

  bool _needsAction(BookingSummary b) =>
      (widget.side == BookingSide.owner ? _ownerTodo : _customerTodo).contains(
        b.status,
      );

  bool _matches(BookingSummary b) => switch (_filter) {
    _Filter.all => true,
    _Filter.action => _needsAction(b),
    _Filter.ongoing => _ongoing.contains(b.status),
    _Filter.past => _past.contains(b.status),
  };

  String _label(_Filter f) => switch (f) {
    _Filter.all => 'All',
    _Filter.action => 'Requests',
    _Filter.ongoing =>
      widget.side == BookingSide.owner ? 'Active rentals' : 'Ongoing',
    _Filter.past => widget.side == BookingSide.owner ? 'History' : 'Past',
  };

  @override
  Widget build(BuildContext context) {
    final list = ref.watch(bookingsProvider(widget.side));
    return AppPage(
      title: widget.side == BookingSide.owner ? 'Bookings' : 'My bookings',
      subtitle: list.whenOrNull(
        data: (l) => '${l.length} booking${l.length == 1 ? '' : 's'}',
      ),
      body: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(
              AppSpacing.md,
              AppSpacing.sm,
              AppSpacing.md,
              0,
            ),
            child: AppChoiceChips<_Filter>(
              options: [
                for (final f in _Filter.values)
                  if (f != _Filter.action || widget.side == BookingSide.owner)
                    f,
              ],
              selected: _filter,
              onSelected: (f) => setState(() => _filter = f),
              labelOf: _label,
            ),
          ),
          Expanded(
            child: AsyncBody(
              value: list,
              onRetry: () => ref.invalidate(bookingsProvider(widget.side)),
              builder: (all) {
                final items = all.where(_matches).toList();
                if (items.isEmpty) {
                  return EmptyState(
                    icon: AppIcons.bookings,
                    title: all.isEmpty ? 'No bookings yet' : 'Nothing here',
                    message: all.isEmpty
                        ? (widget.side == BookingSide.owner
                              ? 'Booking requests for your machines appear here.'
                              : 'Machines you book appear here.')
                        : 'No bookings match this filter.',
                  );
                }
                return RefreshIndicator(
                  onRefresh: () async =>
                      ref.invalidate(bookingsProvider(widget.side)),
                  child: ListView(
                    padding: const EdgeInsets.all(AppSpacing.md),
                    children: [
                      for (final b in items)
                        Padding(
                          padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                          child: _BookingCard(
                            b: b,
                            highlight: _needsAction(b),
                            onTap: () => context.push(
                              CommsRoutes.booking(_role(widget.side), b.id)!,
                            ),
                          ),
                        ),
                    ],
                  ),
                );
              },
            ),
          ),
        ],
      ),
    );
  }
}

class _BookingCard extends StatelessWidget {
  const _BookingCard({
    required this.b,
    required this.onTap,
    this.highlight = false,
  });
  final BookingSummary b;
  final VoidCallback onTap;
  final bool highlight;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      onTap: onTap,
      borderColor: highlight ? AppColors.orange500 : null,
      semanticLabel: '${b.equipmentTitle}, ${bookingStatusLabel(b.status)}',
      child: Row(
        children: [
          const IconTile(icon: AppIcons.bookings),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  b.equipmentTitle,
                  maxLines: 1,
                  overflow: TextOverflow.ellipsis,
                  style: theme.textTheme.titleSmall,
                ),
                const Gap(2),
                Text(
                  '${b.reference} · ${Fmt.dateRange(b.start, b.end)}',
                  style: theme.textTheme.bodySmall?.copyWith(
                    color: AppColors.slate500,
                  ),
                ),
              ],
            ),
          ),
          const Gap(AppSpacing.sm),
          Column(
            crossAxisAlignment: CrossAxisAlignment.end,
            children: [
              BookingStatusBadge(b.status),
              const Gap(4),
              Text(money(b.totalPaise), style: theme.textTheme.labelLarge),
            ],
          ),
        ],
      ),
    );
  }
}

// ---- detail cards (the detail page is in booking_detail_page.dart) --------------------------

class BookingAgreementCard extends ConsumerWidget {
  const BookingAgreementCard({
    super.key,
    required this.side,
    required this.booking,
  });
  final BookingSide side;
  final BookingDetail booking;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ag = ref.watch(agreementProvider((side, booking.id)));
    void open() =>
        context.push(CommsRoutes.agreement(_role(side), booking.id)!);
    return AppCard(
      onTap: open,
      child: Row(
        children: [
          const IconTile(icon: AppIcons.shield, tone: AppTone.info),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Rental agreement',
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                Text(
                  ag.when(
                    data: (a) =>
                        a.canSign ? 'Your signature is needed' : a.statusLabel,
                    loading: () => 'Loading…',
                    error: (e, _) => describeError(e),
                  ),
                  style: Theme.of(context).textTheme.bodySmall?.copyWith(
                    color: ag.value?.canSign == true
                        ? AppColors.orange700
                        : AppColors.slate500,
                  ),
                ),
              ],
            ),
          ),
          AppButton(
            key: const Key('open-agreement'),
            label: ag.value?.canSign == true ? 'Review & sign' : 'View',
            size: AppButtonSize.small,
            variant: ag.value?.canSign == true
                ? AppButtonVariant.accent
                : AppButtonVariant.secondary,
            onPressed: open,
          ),
        ],
      ),
    );
  }
}

class BookingChatCard extends ConsumerStatefulWidget {
  const BookingChatCard({super.key, required this.side, required this.booking});
  final BookingSide side;
  final BookingDetail booking;
  @override
  ConsumerState<BookingChatCard> createState() => BookingChatCardState();
}

class BookingChatCardState extends ConsumerState<BookingChatCard> {
  bool _opening = false;

  Future<void> _open() async {
    setState(() => _opening = true);
    try {
      final c = await ref
          .read(commsApiProvider)
          .openBookingChat(
            ref.read(sideOrgProvider(widget.side)),
            widget.booking.id,
          );
      if (mounted) context.push(CommsRoutes.thread(_role(widget.side), c.id));
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _opening = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final other = widget.side == BookingSide.owner
        ? 'the customer'
        : 'the owner';
    return AppCard(
      onTap: _opening ? null : _open,
      child: Row(
        children: [
          const IconTile(icon: AppIcons.chat),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('Chat', style: Theme.of(context).textTheme.titleSmall),
                Text(
                  'Message $other about this booking',
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
                ),
              ],
            ),
          ),
          AppButton(
            key: const Key('open-chat'),
            label: 'Open',
            size: AppButtonSize.small,
            variant: AppButtonVariant.secondary,
            isLoading: _opening,
            onPressed: _opening ? null : _open,
          ),
        ],
      ),
    );
  }
}
