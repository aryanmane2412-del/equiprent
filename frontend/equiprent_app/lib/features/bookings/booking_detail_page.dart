import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../documents/document_pages.dart' show DocumentRoutes;
import '../reviews/review_widgets.dart';
import '../verification/data/verification_api.dart'
    show inspectionCameraProvider, linkOpenerProvider;
import '../verification/data/verification_api.dart'
    show verificationApiProvider, UploadPurpose;
import 'booking_pages.dart' show BookingAgreementCard, BookingChatCard;
import 'data/booking_api.dart';
import 'data/booking_models.dart';
import 'data/gateway_checkout.dart';
import '../tracking/data/live_track.dart' show liveTrackProvider;

/// One booking as the customer or the owner sees it: where it stands, what
/// the caller can do next (the server's `actions` list, re-checked on every
/// call), money, extensions, history, agreement, chat, tracking, invoices
/// and reviews.
class BookingDetailPage extends ConsumerWidget {
  const BookingDetailPage({super.key, required this.side, required this.id});
  final BookingSide side;
  final String id;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final detail = ref.watch(bookingDetailProvider((side, id)));
    return AppPage(
      title: detail.value?.reference ?? 'Booking',
      subtitle: detail.value?.equipmentTitle,
      topLevel: false,
      actions: [
        IconButton(
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () => refreshBooking(ref, side, id),
        ),
      ],
      body: AsyncBody(
        value: detail,
        onRetry: () => refreshBooking(ref, side, id),
        builder: (b) => RefreshIndicator(
          onRefresh: () async => refreshBooking(ref, side, id),
          child: PageBody(
            children: [
              _StatusCard(b: b, side: side),
              const Gap(AppSpacing.md),
              if (side == BookingSide.owner && b.can('accept')) ...[
                _RespondCard(b: b),
                const Gap(AppSpacing.md),
              ],
              if (b.pendingExtension != null) ...[
                _ExtensionCard(b: b, side: side),
                const Gap(AppSpacing.md),
              ],
              _ActionsCard(b: b, side: side),
              ResponsiveGrid(
                columns: const Responsive(compact: 1, medium: 2),
                children: [
                  _Summary(b: b, side: side),
                  if (side == BookingSide.owner && b.customer != null)
                    _CustomerCard(b: b)
                  else
                    _MoneyCard(b: b, side: side),
                  if (side == BookingSide.owner && b.customer != null)
                    _MoneyCard(b: b, side: side),
                  if (b.price != null) _PriceCard(b: b),
                ],
              ),
              if (side == BookingSide.customer) ...[
                const Gap(AppSpacing.md),
                _PaymentReceiptsCard(bookingId: b.id),
              ],
              if (b.delivery &&
                  const {
                    'on_site',
                    'active',
                    'extension_requested',
                    'return_initiated',
                    'returned',
                    'completed',
                    'closed',
                  }.contains(b.status)) ...[
                const Gap(AppSpacing.md),
                _DeliveryPhotosCard(
                  side: side,
                  bookingId: b.id,
                  canUpload:
                      side == BookingSide.customer && b.status == 'on_site',
                ),
              ],
              const Gap(AppSpacing.md),
              ResponsiveGrid(
                columns: const Responsive(compact: 1, medium: 2),
                children: [
                  if (b.hasAgreement)
                    BookingAgreementCard(side: side, booking: b),
                  BookingChatCard(side: side, booking: b),
                  if (b.can('track')) _TrackCard(b: b, side: side),
                ],
              ),
              if (b.can('invoices')) ...[
                const Gap(AppSpacing.md),
                _InvoicesCard(b: b, side: side),
              ],
              const Gap(AppSpacing.md),
              _HistoryCard(b: b),
              if (b.finished) ...[
                const Gap(AppSpacing.lg),
                BookingReviewsSection(side: side, bookingId: b.id),
              ],
              const Gap(AppSpacing.xl),
            ],
          ),
        ),
      ),
    );
  }
}

/// Reloads everything that depends on a booking.
void refreshBooking(WidgetRef ref, BookingSide side, String id) {
  ref.invalidate(bookingDetailProvider((side, id)));
  ref.invalidate(bookingsProvider(side));
  ref.invalidate(agreementProvider((side, id)));
  ref.invalidate(bookingInvoicesProvider((side, id)));
  ref.invalidate(liveTrackProvider((side, id)));
}

// ---- where the booking stands ------------------------------------------------------------------

const _milestones = [
  'requested',
  'confirmed',
  'dispatched',
  'active',
  'completed',
];
const _milestoneLabels = [
  'Requested',
  'Confirmed',
  'On the way',
  'In use',
  'Completed',
];

int _milestoneOf(String status) => switch (status) {
  'draft' ||
  'requested' ||
  'quoted' ||
  'accepted' ||
  'payment_pending' ||
  'payment_processing' => 0,
  'confirmed' || 'operator_assigned' => 1,
  'dispatched' || 'on_site' || 'cancellation_requested' => 2,
  'active' ||
  'extension_requested' ||
  'return_initiated' ||
  'returned' ||
  'disputed' => 3,
  'completed' || 'closed' => 4,
  _ => -1,
};

class _StatusCard extends StatelessWidget {
  const _StatusCard({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;

  (String, AppTone)? _banner() {
    final owner = side == BookingSide.owner;
    final m = b.money;
    switch (b.status) {
      case 'draft':
        return (
          'This booking is a draft. Submit it to send it to the owner.',
          AppTone.info,
        );
      case 'requested':
      case 'quoted':
        final due = b.ownerResponseDueAt == null
            ? ''
            : ' by ${Fmt.dateTime(b.ownerResponseDueAt!)}';
        return (
          owner
              ? 'Accept or decline this request$due.'
              : 'Waiting for ${b.ownerName} to respond$due.',
          AppTone.warning,
        );
      case 'accepted':
      case 'payment_pending':
        if (b.paymentMode == 'offline') {
          return (
            'Accepted. Payment is settled directly with the owner.',
            AppTone.info,
          );
        }
        final due = b.paymentDueAt ?? m?.paymentDueAt;
        final amount = m == null
            ? ''
            : ' ${money(m.advanceDuePaise + m.depositPaise)}';
        final by = due == null ? '' : ' by ${Fmt.dateTime(due)}';
        return (
          owner
              ? 'Waiting for the customer to pay$by. The dates are held until then.'
              : 'Pay$amount$by to confirm, or the dates are released.',
          AppTone.warning,
        );
      case 'payment_processing':
        return (
          'The payment is being processed. This usually takes a moment.',
          AppTone.info,
        );
      case 'confirmed':
        return (
          b.withOperator && b.operator == null && owner
              ? 'Confirmed. Assign an operator, then dispatch the machine.'
              : 'Confirmed. The owner will dispatch the machine before the start.',
          AppTone.success,
        );
      case 'operator_assigned':
        return (
          'Operator ${b.operator?.name ?? ''} is assigned (${b.operator?.status ?? 'assigned'}).',
          AppTone.info,
        );
      case 'dispatched':
        return (
          'The machine has left the yard and is on its way to ${b.siteLabel.isEmpty ? 'the site' : b.siteLabel}.',
          AppTone.brand,
        );
      case 'on_site':
        return (
          owner
              ? 'The machine is on site. The customer confirms the handover to start the rental.'
              : 'The machine is on site. Confirm the handover to start the rental.',
          AppTone.brand,
        );
      case 'active':
        return (
          'The rental is running until ${Fmt.dateTime(b.end)}.',
          AppTone.brand,
        );
      case 'extension_requested':
        return (
          'An extension to ${Fmt.dateTime(b.pendingExtension?.newEnd ?? b.end)} is waiting for an answer.',
          AppTone.warning,
        );
      case 'return_initiated':
        return (
          'The customer released the machine. It is on its way back.',
          AppTone.info,
        );
      case 'returned':
        if (!owner &&
            b.paymentMode == 'online' &&
            b.finalizedAt != null &&
            (m?.balanceDuePaise ?? 0) > 0) {
          return (
            'Work is complete. Final payment of ${money(m!.balanceDuePaise)} is pending.',
            AppTone.warning,
          );
        }
        if (!owner &&
            b.paymentMode == 'online' &&
            b.finalizedAt != null &&
            m?.balanceDuePaise == 0) {
          return (
            'Final payment received. The owner will complete the booking.',
            AppTone.success,
          );
        }
        return (
          owner
              ? 'The machine is back. Settle the final bill, issue the invoice and complete the booking.'
              : 'The machine is back with the owner. The final bill follows.',
          AppTone.info,
        );
      case 'completed':
      case 'closed':
        return (
          'Rental completed. Thank you for using EQUIPRENT.',
          AppTone.success,
        );
      case 'cancellation_requested':
        return (
          owner
              ? 'The customer asked to cancel. Approve or decline.'
              : 'Your cancellation request is waiting for the owner.',
          AppTone.warning,
        );
      case 'cancelled':
        return (
          'Cancelled${b.cancelReason == null ? '' : ': ${b.cancelReason}'}.',
          AppTone.danger,
        );
      case 'rejected':
        return (
          'The owner declined this request${b.cancelReason == null ? '' : ': ${b.cancelReason}'}.',
          AppTone.danger,
        );
      case 'expired':
        return (
          'This booking expired without a response or payment.',
          AppTone.danger,
        );
      case 'refund_pending':
        return (
          'Cancelled. ${m == null ? 'The refund' : money(m.refundedPaise)} is being refunded to the original payment method.',
          AppTone.warning,
        );
      case 'refunded':
        return (
          'Cancelled and refunded${m == null ? '' : ' (${money(m.refundedPaise)})'}.',
          AppTone.success,
        );
      case 'disputed':
        return (
          'A dispute is open. Our team will contact both parties.',
          AppTone.danger,
        );
    }
    return null;
  }

  @override
  Widget build(BuildContext context) {
    final at = _milestoneOf(b.status);
    final banner = _banner();
    final owner = side == BookingSide.owner;
    final title =
        b.status == 'returned' &&
            !owner &&
            b.paymentMode == 'online' &&
            b.finalizedAt != null &&
            (b.money?.balanceDuePaise ?? 0) > 0
        ? 'Final payment pending'
        : bookingStatusLabel(b.status);
    return SectionCard(
      title: title,
      subtitle: '${b.reference} · ${Fmt.dateRange(b.start, b.end)}',
      icon: AppIcons.bookings,
      trailing: BookingStatusBadge(b.status),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (at >= 0) ...[
            Row(
              children: [
                for (var i = 0; i < _milestones.length; i++)
                  Expanded(
                    child: Column(
                      children: [
                        Container(
                          height: 6,
                          margin: const EdgeInsets.symmetric(horizontal: 2),
                          decoration: BoxDecoration(
                            color: i <= at
                                ? AppColors.navy700
                                : AppColors.slate200,
                            borderRadius: BorderRadius.circular(3),
                          ),
                        ),
                        const Gap(4),
                        Text(
                          _milestoneLabels[i],
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          textAlign: TextAlign.center,
                          style: Theme.of(context).textTheme.labelSmall
                              ?.copyWith(
                                color: i <= at
                                    ? AppColors.navy700
                                    : AppColors.slate500,
                              ),
                        ),
                      ],
                    ),
                  ),
              ],
            ),
            const Gap(AppSpacing.ms),
          ],
          if (banner != null)
            InlineBanner(
              key: const Key('status-banner'),
              message: banner.$1,
              tone: banner.$2,
            ),
        ],
      ),
    );
  }
}

class _Summary extends StatelessWidget {
  const _Summary({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;

  @override
  Widget build(BuildContext context) {
    return SectionCard(
      title: b.equipmentTitle,
      subtitle: side == BookingSide.owner
          ? 'Customer: ${b.customerName}'
          : 'Owner: ${b.ownerName}',
      icon: AppIcons.calendar,
      child: Column(
        children: [
          InfoRow(
            label: 'Starts',
            value: Fmt.dateTime(b.start),
            icon: AppIcons.calendar,
          ),
          InfoRow(
            label: 'Ends',
            value: Fmt.dateTime(b.end),
            icon: AppIcons.calendarDays,
          ),
          if (b.siteLabel.isNotEmpty || b.siteAddress.isNotEmpty)
            InfoRow(
              label: 'Site',
              value: [
                b.siteLabel,
                b.siteAddress,
              ].where((s) => s.isNotEmpty).join(' · '),
              icon: AppIcons.siteMarker,
            ),
          InfoRow(
            label: 'Transport',
            value: b.delivery
                ? 'Owner delivers${b.pickup ? ' and collects' : ''}'
                : 'Customer collects',
            icon: AppIcons.route,
          ),
          InfoRow(
            label: 'Hire',
            value: b.withOperator
                ? 'With operator${b.operator == null ? '' : ' (${b.operator!.name})'}'
                : 'Without operator',
            icon: AppIcons.operator,
          ),
          InfoRow(
            label: 'Payment',
            value: b.paymentMode == 'offline'
                ? 'Directly to the owner'
                : 'Online',
            icon: AppIcons.payments,
          ),
          if (b.workDescription.isNotEmpty)
            InfoRow(
              label: 'Work',
              value: b.workDescription,
              icon: AppIcons.info,
            ),
          if (side == BookingSide.customer && b.operator != null) ...[
            InfoRow(
              label: 'Assigned driver',
              value: b.operator!.name,
              icon: AppIcons.operator,
            ),
            if (b.operator!.phone != null)
              InfoRow(
                label: 'Driver contact',
                value: b.operator!.phone!,
                icon: AppIcons.phone,
              ),
          ],
        ],
      ),
    );
  }
}

class _PaymentReceiptsCard extends ConsumerWidget {
  const _PaymentReceiptsCard({required this.bookingId});
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final payments = ref.watch(
      bookingPaymentsProvider((BookingSide.customer, bookingId)),
    );
    return SectionCard(
      title: 'Payment history & receipts',
      icon: AppIcons.receipt,
      child: AsyncBody(
        value: payments,
        onRetry: () => ref.invalidate(
          bookingPaymentsProvider((BookingSide.customer, bookingId)),
        ),
        builder: (items) => items.isEmpty
            ? const Text(
                'Payment receipts appear here after a payment is captured.',
              )
            : Column(
                children: [
                  for (final payment in items)
                    ListRow(
                      dense: true,
                      leading: IconTile(
                        icon: payment.status == 'captured'
                            ? AppIcons.checkCircle
                            : AppIcons.payments,
                      ),
                      title:
                          '${payment.purpose[0].toUpperCase()}${payment.purpose.substring(1)} · ${money(payment.amountPaise)} · ${payment.status}',
                      subtitle: [
                        if (payment.receiptNumber != null)
                          payment.receiptNumber!,
                        if (payment.capturedAt != null)
                          Fmt.dateTime(payment.capturedAt!),
                        if (payment.status == 'failed')
                          'Payment was not captured; the booking is not confirmed.',
                      ].join(' · '),
                    ),
                ],
              ),
      ),
    );
  }
}

class _DeliveryPhotosCard extends ConsumerStatefulWidget {
  const _DeliveryPhotosCard({
    required this.side,
    required this.bookingId,
    required this.canUpload,
  });
  final BookingSide side;
  final String bookingId;
  final bool canUpload;

  @override
  ConsumerState<_DeliveryPhotosCard> createState() =>
      _DeliveryPhotosCardState();
}

class _DeliveryPhotosCardState extends ConsumerState<_DeliveryPhotosCard> {
  bool _uploading = false;

  Future<void> _approve(String photoId) async {
    try {
      await ref
          .read(bookingApiProvider)
          .approveDeliveryPhoto(
            ref.read(sideOrgProvider(widget.side)),
            widget.bookingId,
            photoId,
          );
      ref.invalidate(
        bookingDeliveryPhotosProvider((widget.side, widget.bookingId)),
      );
      if (mounted) AppToast.success(context, 'Photo approved.');
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    }
  }

  Future<void> _upload() async {
    final file = await ref.read(inspectionCameraProvider).capture();
    if (file == null || !mounted) return;
    setState(() => _uploading = true);
    try {
      final api = ref.read(verificationApiProvider);
      final capturedAt = DateTime.now().toUtc();
      final uploadId = await api.upload(UploadPurpose.jobPhoto, file);
      await ref
          .read(bookingApiProvider)
          .addDeliveryPhoto(
            ref.read(sideOrgProvider(widget.side)),
            widget.bookingId,
            uploadId: uploadId,
            capturedAt: capturedAt,
            clientId: newClientId(),
          );
      ref.invalidate(
        bookingDeliveryPhotosProvider((widget.side, widget.bookingId)),
      );
      if (mounted) {
        AppToast.success(
          context,
          'Delivery photos shared with the equipment owner.',
        );
      }
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _uploading = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final photos = ref.watch(
      bookingDeliveryPhotosProvider((widget.side, widget.bookingId)),
    );
    return SectionCard(
      title: 'Delivery photos',
      subtitle:
          'Private evidence shared with this booking’s customer and equipment owner',
      icon: AppIcons.camera,
      trailing: widget.canUpload
          ? AppButton(
              label: 'Add photos',
              icon: AppIcons.upload,
              size: AppButtonSize.small,
              variant: AppButtonVariant.secondary,
              isLoading: _uploading,
              onPressed: _uploading ? null : _upload,
            )
          : null,
      child: AsyncBody(
        value: photos,
        onRetry: () => ref.invalidate(
          bookingDeliveryPhotosProvider((widget.side, widget.bookingId)),
        ),
        builder: (items) => items.isEmpty
            ? Text(
                widget.canUpload
                    ? 'Add timestamped photos of the machine as it is delivered.'
                    : 'No customer delivery photos have been added.',
              )
            : Wrap(
                spacing: AppSpacing.sm,
                runSpacing: AppSpacing.sm,
                children: [
                  for (final photo in items)
                    SizedBox(
                      width: 170,
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          ClipRRect(
                            borderRadius: BorderRadius.circular(12),
                            child: Image.network(
                              photo.url,
                              width: 170,
                              height: 120,
                              fit: BoxFit.cover,
                              errorBuilder: (_, _, _) => const SizedBox(
                                width: 170,
                                height: 120,
                                child: Icon(AppIcons.image),
                              ),
                            ),
                          ),
                          const Gap(AppSpacing.xs),
                          Text(
                            'Captured ${Fmt.dateTime(photo.capturedAt)}',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                          Text(
                            'Received ${Fmt.dateTime(photo.submittedAt)}',
                            style: Theme.of(context).textTheme.bodySmall,
                          ),
                          if (photo.approved)
                            const StatusBadge(
                              label: 'Approved by owner',
                              tone: AppTone.success,
                              dot: true,
                            )
                          else if (widget.side == BookingSide.owner)
                            AppButton(
                              key: Key('approve-photo-${photo.id}'),
                              label: 'Approve',
                              icon: AppIcons.check,
                              size: AppButtonSize.small,
                              variant: AppButtonVariant.success,
                              onPressed: () => _approve(photo.id),
                            ),
                        ],
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}

class _CustomerCard extends StatelessWidget {
  const _CustomerCard({required this.b});
  final BookingDetail b;

  @override
  Widget build(BuildContext context) {
    final c = b.customer!;
    final a = b.availability;
    return SectionCard(
      title: 'Customer',
      subtitle: c.companyName ?? c.orgName,
      icon: AppIcons.user,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          InfoRow(
            label: 'Rental type',
            value: c.isCompany ? 'Company / Business' : 'Individual / Personal',
          ),
          if (c.isCompany && c.gstin != null)
            InfoRow(label: 'GSTIN', value: c.gstin!),
          if (c.email != null) InfoRow(label: 'Email', value: c.email!),
          if (c.isCompany && c.address != null)
            InfoRow(
              label: 'Business address',
              value: [
                c.address,
                c.city,
                c.state,
                c.pinCode,
              ].whereType<String>().join(', '),
            ),
          InfoRow(
            label: 'Contact',
            value: c.contactName.isEmpty ? '—' : c.contactName,
            icon: AppIcons.profile,
          ),
          InfoRow(
            label: 'Phone',
            value: c.phone ?? 'Shared once you accept',
            icon: AppIcons.phone,
          ),
          InfoRow(
            label: 'KYC',
            value: c.kycStatus.isEmpty
                ? 'Not started'
                : c.kycStatus.replaceAll('_', ' '),
            icon: AppIcons.verified,
          ),
          InfoRow(
            label: 'Completed rentals',
            value: '${c.completedRentals}',
            icon: AppIcons.history,
          ),
          if (a != null) ...[
            const Gap(AppSpacing.sm),
            InlineBanner(
              key: const Key('availability-check'),
              tone: a.available ? AppTone.success : AppTone.danger,
              title: a.available ? 'Dates are free' : 'Dates clash',
              message: a.available
                  ? 'Nothing else holds the machine for these dates.'
                  : (a.problem ?? 'Another booking or block overlaps.'),
            ),
          ],
        ],
      ),
    );
  }
}

class _MoneyCard extends StatelessWidget {
  const _MoneyCard({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;

  @override
  Widget build(BuildContext context) {
    final m = b.money;
    return SectionCard(
      title: 'Payments',
      icon: AppIcons.wallet,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          InfoRow(
            label: 'Estimated total',
            value: money(b.totalPaise),
            emphasize: b.finalTotalPaise == null,
          ),
          if (b.finalTotalPaise != null)
            InfoRow(
              key: const Key('final-total'),
              label: 'Final bill',
              value: money(b.finalTotalPaise!),
              emphasize: true,
            ),
          if (b.depositPaise > 0)
            InfoRow(
              label: 'Refundable deposit',
              value: money(b.depositPaise),
              icon: AppIcons.shield,
            ),
          if (m != null) ...[
            if (m.advanceDuePaise > 0 && b.paymentMode == 'online')
              InfoRow(
                label: 'Advance to confirm',
                value: money(m.advanceDuePaise),
              ),
            InfoRow(
              key: const Key('paid-amount'),
              label: 'Paid',
              value: money(m.paidPaise),
              valueColor: AppColors.green700,
            ),
            if (m.refundedPaise > 0)
              InfoRow(label: 'Refunded', value: money(m.refundedPaise)),
            if (m.balanceDuePaise > 0)
              InfoRow(
                label: 'Balance due',
                value: money(m.balanceDuePaise),
                valueColor: AppColors.orange700,
              ),
            for (final r in m.refunds)
              Text(
                'Refund ${money(r.amountPaise)} · ${r.status}${r.reason.isEmpty ? '' : ' · ${r.reason}'}',
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: AppColors.slate600),
              ),
          ],
          if (b.cancellation != null && b.cancellation!.summary.isNotEmpty) ...[
            const Gap(AppSpacing.sm),
            Text(
              'Cancellation policy',
              style: Theme.of(context).textTheme.labelLarge,
            ),
            Text(
              b.cancellation!.summary,
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ],
      ),
    );
  }
}

class _PriceCard extends StatelessWidget {
  const _PriceCard({required this.b});
  final BookingDetail b;

  @override
  Widget build(BuildContext context) {
    final p = b.price!;
    return SectionCard(
      title: 'Price breakdown',
      subtitle: 'Frozen when the booking was made',
      icon: AppIcons.receipt,
      child: Column(
        children: [
          for (final c in p.charges)
            InfoRow(
              label: c.detail.isEmpty ? c.label : '${c.label} (${c.detail})',
              value: money(c.paise),
            ),
          const Divider(),
          InfoRow(label: 'Total', value: money(p.totalPaise), emphasize: true),
        ],
      ),
    );
  }
}

// ---- owner response -------------------------------------------------------------------------------

class _RespondCard extends ConsumerStatefulWidget {
  const _RespondCard({required this.b});
  final BookingDetail b;
  @override
  ConsumerState<_RespondCard> createState() => _RespondCardState();
}

class _RespondCardState extends ConsumerState<_RespondCard> {
  bool _busy = false;

  Future<void> _respond(bool accept) async {
    String? reason;
    if (!accept) {
      reason = await askReason(
        context,
        title: 'Decline this request?',
        label: 'Reason (shown to the customer)',
        confirm: 'Decline',
      );
      if (reason == null) return;
    }
    if (!mounted) return;
    setState(() => _busy = true);
    final ok = await runComms(
      context,
      () => ref
          .read(commsApiProvider)
          .respondToBooking(
            ref.read(sideOrgProvider(BookingSide.owner)),
            widget.b.id,
            accept: accept,
            reason: reason,
          ),
      success: accept
          ? (widget.b.paymentMode == 'online'
                ? 'Accepted. The customer has been asked to pay.'
                : 'Accepted and confirmed.')
          : 'Request declined.',
    );
    if (mounted) setState(() => _busy = false);
    if (ok) refreshBooking(ref, BookingSide.owner, widget.b.id);
  }

  @override
  Widget build(BuildContext context) {
    final clash = widget.b.availability?.available == false;
    return AppCard(
      borderColor: AppColors.orange500,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            'New booking request',
            style: Theme.of(context).textTheme.titleSmall,
          ),
          const Gap(4),
          Text(
            clash
                ? 'These dates now clash with another booking or block, so accepting will fail. Decline with a reason.'
                : 'Accept to hold these dates for the customer, or decline with a reason. Other requests for the same dates are declined automatically.',
          ),
          const Gap(AppSpacing.ms),
          Row(
            children: [
              Expanded(
                child: AppButton(
                  key: const Key('decline-booking'),
                  label: 'Decline',
                  variant: AppButtonVariant.secondary,
                  onPressed: _busy ? null : () => _respond(false),
                ),
              ),
              const Gap(AppSpacing.sm),
              Expanded(
                child: AppButton(
                  key: const Key('accept-booking'),
                  label: 'Accept',
                  isLoading: _busy,
                  onPressed: _busy || clash ? null : () => _respond(true),
                ),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

// ---- actions --------------------------------------------------------------------------------------

/// Buttons for what the caller may do now.
class _ActionsCard extends ConsumerStatefulWidget {
  const _ActionsCard({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;
  @override
  ConsumerState<_ActionsCard> createState() => _ActionsCardState();
}

class _ActionsCardState extends ConsumerState<_ActionsCard> {
  String? _busy;
  late final GatewayCheckout _gatewayCheckout;

  @override
  void initState() {
    super.initState();
    _gatewayCheckout = createGatewayCheckout();
  }

  @override
  void dispose() {
    _gatewayCheckout.dispose();
    super.dispose();
  }

  BookingDetail get b => widget.b;
  String? get _org => ref.read(sideOrgProvider(widget.side));
  BookingApi get _api => ref.read(bookingApiProvider);

  Future<void> _run(
    String key,
    Future<void> Function() action, {
    String? success,
  }) async {
    setState(() => _busy = key);
    await runComms(context, action, success: success);
    if (!mounted) return;
    setState(() => _busy = null);
    // Refresh either way: a failure (e.g. VERSION_MISMATCH) means the booking moved on.
    refreshBooking(ref, widget.side, b.id);
  }

  Future<void> _step(
    String key,
    String path,
    String success, {
    String? confirm,
  }) async {
    if (confirm != null &&
        !await confirmAction(
          context,
          title: confirm,
          confirm: 'Yes, continue',
        )) {
      return;
    }
    await _run(
      key,
      () => _api.act(_org, path, version: b.version),
      success: success,
    );
  }

  Future<void> _pay(String purpose) async {
    setState(() => _busy = purpose);
    var waitingForGateway = false;
    try {
      final checkout = await _api.startPayment(_org, b.id, purpose);
      if (!mounted) return;
      if (!checkout.sandbox) {
        if (!_gatewayCheckout.supported) {
          AppToast.show(
            context,
            'This platform does not support the configured payment checkout yet.',
            tone: AppTone.warning,
          );
          return;
        }
        if (checkout.options['key'] == null ||
            checkout.options['order_id'] == null) {
          AppToast.error(
            context,
            'The payment gateway returned incomplete checkout details.',
          );
          return;
        }
        waitingForGateway = true;
        _gatewayCheckout.open(
          checkout.options,
          onSuccess: ({required paymentId, required signature}) {
            _verifyGatewayPayment(checkout.payment.id, paymentId, signature);
          },
          onFailure: (message) {
            if (!mounted) return;
            setState(() => _busy = null);
            AppToast.show(context, message, tone: AppTone.warning);
            refreshBooking(ref, widget.side, b.id);
          },
        );
        return;
      }
      final go = await showDialog<bool>(
        context: context,
        builder: (ctx) => AppDialog(
          title: 'Test checkout',
          icon: AppIcons.payments,
          content: Text(
            'Pay ${money(checkout.payment.amountPaise)} (${purpose == 'advance' ? 'advance and deposit' : purpose}) with the sandbox gateway. '
            'No real money moves.',
          ),
          actions: [
            AppButton(
              label: 'Cancel',
              variant: AppButtonVariant.secondary,
              onPressed: () => Navigator.of(ctx).pop(false),
            ),
            AppButton(
              key: const Key('confirm-payment'),
              label: 'Pay ${money(checkout.payment.amountPaise)}',
              onPressed: () => Navigator.of(ctx).pop(true),
            ),
          ],
        ),
      );
      if (go != true) return;
      await _api.verifyPayment(
        _org,
        checkout.payment.id,
        gatewayPaymentId: checkout.sandboxPaymentId!,
        signature: checkout.sandboxSignature!,
      );
      if (mounted) {
        AppToast.success(
          context,
          'Payment received. Your booking is confirmed.',
        );
      }
    } catch (e) {
      waitingForGateway = false;
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted && !waitingForGateway) {
        setState(() => _busy = null);
        refreshBooking(ref, widget.side, b.id);
      }
    }
  }

  Future<void> _verifyGatewayPayment(
    String paymentRecordId,
    String gatewayPaymentId,
    String signature,
  ) async {
    if (!mounted) return;
    try {
      await _api.verifyPayment(
        _org,
        paymentRecordId,
        gatewayPaymentId: gatewayPaymentId,
        signature: signature,
      );
      final captured = await _awaitCapture(paymentRecordId);
      if (mounted) {
        if (captured) {
          AppToast.success(context, 'Payment received. Your booking is confirmed.');
        } else {
          AppToast.show(
            context,
            'Payment submitted. We are waiting for the gateway to confirm it; this page updates by itself.',
            tone: AppTone.info,
          );
        }
      }
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) {
        setState(() => _busy = null);
        refreshBooking(ref, widget.side, b.id);
      }
    }
  }

  /// A browser or app callback is not proof: the booking is confirmed only once the backend has
  /// the gateway's own confirmation (direct check or webhook). Polls briefly for it.
  Future<bool> _awaitCapture(String paymentRecordId) async {
    for (var i = 0; i < 20 && mounted; i++) {
      try {
        final list = await _api.payments(_org, b.id);
        final p = list.where((x) => x.id == paymentRecordId).firstOrNull;
        if (p?.status == 'captured') return true;
        if (p?.status == 'failed') return false;
      } catch (_) {
        // Transient network error while waiting for the gateway: retry.
      }
      await Future<void>.delayed(const Duration(seconds: 3));
    }
    return false;
  }

  Future<void> _cancel() async {
    CancelOutcome preview;
    try {
      preview = await _api.cancel(_org, b.id, dryRun: true);
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
      return;
    }
    if (!mounted) return;
    final terms = [
      if (preview.requiresApproval)
        'The machine has left the yard, so the owner must approve the cancellation.',
      if (preview.feePaise > 0)
        'Cancellation fee: ${money(preview.feePaise)}.'
      else
        'No cancellation fee.',
      if (preview.refundPaise > 0)
        '${money(preview.refundPaise)} will be refunded.',
    ].join(' ');
    final reason = await askReason(
      context,
      title: preview.requiresApproval
          ? 'Request cancellation?'
          : 'Cancel this booking?',
      label: 'Reason',
      confirm: preview.requiresApproval ? 'Send request' : 'Cancel booking',
      message: terms,
    );
    if (reason == null) return;
    await _run('cancel', () async {
      final r = await _api.cancel(
        _org,
        b.id,
        reason: reason,
        version: b.version,
      );
      if (mounted) {
        AppToast.success(context, switch (r.status) {
          'cancellation_requested' =>
            'Cancellation requested. The owner will respond.',
          'refund_pending' =>
            'Cancelled. ${money(r.refundPaise)} is being refunded.',
          _ => 'Booking cancelled.',
        });
      }
    });
  }

  Future<void> _answerCancellation(String decision) async {
    String? reason;
    if (decision == 'decline') {
      reason = await askReason(
        context,
        title: 'Decline the cancellation?',
        label: 'Reason (shown to the customer)',
        confirm: 'Decline',
      );
      if (reason == null) return;
    } else if (decision == 'approve' &&
        !await confirmAction(
          context,
          title: 'Approve the cancellation?',
          message: 'Paid money minus the policy fee is refunded.',
          confirm: 'Approve',
        )) {
      return;
    }
    await _run(
      'cancellation-$decision',
      () => _api.answerCancellation(_org, b.id, decision, reason: reason),
      success: {
        'approve': 'Cancellation approved.',
        'decline': 'Cancellation declined; the booking continues.',
        'withdraw': 'Cancellation request withdrawn.',
      }[decision],
    );
  }

  Future<void> _assignOperator() async {
    final picked = await showModalBottomSheet<OperatorOption>(
      context: context,
      showDragHandle: true,
      builder: (_) => const _OperatorSheet(),
    );
    if (picked == null) return;
    await _run(
      'assign',
      () => _api.assignOperator(_org, b.id, picked.userId),
      success: '${picked.name} is assigned. They will be notified.',
    );
  }

  Future<void> _agreeDriverPay() async {
    final amount = TextEditingController();
    final saved = await showDialog<int>(
      context: context,
      builder: (ctx) => AppDialog(
        title: 'Agreed driver payment',
        icon: AppIcons.payments,
        content: AppTextField(
          label: 'Amount in rupees',
          controller: amount,
          keyboardType: const TextInputType.numberWithOptions(decimal: true),
        ),
        actions: [
          AppButton(
            label: 'Cancel',
            variant: AppButtonVariant.secondary,
            onPressed: () => Navigator.of(ctx).pop(),
          ),
          AppButton(
            label: 'Save agreed amount',
            onPressed: () {
              final rupees = double.tryParse(amount.text.trim());
              if (rupees == null || rupees <= 0) return;
              Navigator.of(ctx).pop((rupees * 100).round());
            },
          ),
        ],
      ),
    );
    amount.dispose();
    if (saved == null) return;
    await _run('driver_pay', () async {
      await _api.recordDriverCompensation(_org, b.id, saved);
      if (mounted) AppToast.success(context, 'Agreed driver payment saved.');
    });
  }

  Future<void> _extend() async {
    final done = await showDialog<bool>(
      context: context,
      builder: (_) => _ExtensionDialog(b: b, side: widget.side),
    );
    if (done == true && mounted) refreshBooking(ref, widget.side, b.id);
  }

  Future<void> _settleFinal() async {
    final done = await showDialog<bool>(
      context: context,
      builder: (_) => _FinalBillDialog(b: b),
    );
    if (done == true && mounted) refreshBooking(ref, widget.side, b.id);
  }

  Future<void> _dispute() async {
    final done = await showDialog<bool>(
      context: context,
      builder: (_) => _DisputeDialog(b: b, side: widget.side),
    );
    if (done == true && mounted) refreshBooking(ref, widget.side, b.id);
  }

  @override
  Widget build(BuildContext context) {
    final id = b.id;
    final m = b.money;
    final driverComp = ref
        .watch(driverCompensationForBookingProvider((_org, b.id)))
        .value;
    final buttons = <Widget>[];
    void add(
      String action,
      String label,
      VoidCallback onTap, {
      AppButtonVariant variant = AppButtonVariant.secondary,
      IconData? icon,
    }) {
      if (!b.can(action)) return;
      buttons.add(
        AppButton(
          key: Key('action-$action'),
          label: label,
          icon: icon,
          variant: variant,
          isLoading: _busy == action,
          onPressed: _busy != null ? null : onTap,
        ),
      );
    }

    final customer = widget.side == BookingSide.customer;
    if (customer) {
      final due = m == null
          ? ''
          : ' ${money(m.advanceDuePaise + m.depositPaise)}';
      add(
        'pay',
        'Pay$due',
        () => _pay('advance'),
        variant: AppButtonVariant.accent,
        icon: AppIcons.payments,
      );
      add(
        'pay_balance',
        'Pay balance${m == null ? '' : ' ${money(m.balanceDuePaise)}'}',
        () => _pay('balance'),
        variant: AppButtonVariant.accent,
        icon: AppIcons.payments,
      );
      add(
        'confirm_handover',
        'Confirm handover',
        () async {
          await _run(
            'ack_handover',
            () => _api.acknowledgeInspection(_org, id, 'handover'),
          );
          if (!mounted) return;
          await _step(
            'confirm_handover',
            'bookings/$id/handover/confirm',
            'Rental started.',
          );
        },
        variant: AppButtonVariant.primary,
        icon: AppIcons.handover,
      );
      add(
        'request_return',
        'Release machine',
        () => _step(
          'request_return',
          'bookings/$id/return-request',
          'The owner has been told to collect the machine.',
          confirm:
              'Release the machine now? The owner will arrange the return.',
        ),
        icon: AppIcons.arrowLeft,
      );
      add(
        'withdraw_cancellation',
        'Withdraw cancellation',
        () => _answerCancellation('withdraw'),
      );
    } else {
      add(
        'assign_operator',
        b.operator == null ? 'Assign operator' : 'Change operator',
        _assignOperator,
        variant: AppButtonVariant.primary,
        icon: AppIcons.operator,
      );
      if (b.operator != null &&
          b.withOperator &&
          const {'returned', 'completed', 'closed'}.contains(b.status)) {
        add(
          'agree_driver_pay',
          driverComp == null
              ? 'Record driver payment'
              : 'Update agreed driver payment',
          _agreeDriverPay,
          icon: AppIcons.payments,
        );
        if (driverComp != null && driverComp.status == 'agreed') {
          add(
            'mark_driver_paid',
            'Record paid with transaction reference',
            () => _payDriver(driverComp),
            icon: AppIcons.checkCircle,
          );
        }
      }
      add(
        'dispatch',
        'Dispatch machine',
        () => _step(
          'dispatch',
          'owner/bookings/$id/dispatch',
          'Dispatched. The customer can track the machine.',
        ),
        variant: AppButtonVariant.primary,
        icon: AppIcons.tracking,
      );
      add(
        'arrive',
        'Mark arrived on site',
        () =>
            _step('arrive', 'operator/jobs/$id/arrived', 'Marked as on site.'),
        icon: AppIcons.siteMarker,
      );
      add(
        'approve_cancellation',
        'Approve cancellation',
        () => _answerCancellation('approve'),
        variant: AppButtonVariant.danger,
      );
      add(
        'decline_cancellation',
        'Decline cancellation',
        () => _answerCancellation('decline'),
      );
      add(
        'mark_returned',
        'Machine is back',
        () => _step(
          'mark_returned',
          'owner/bookings/$id/mark-returned',
          'Marked as returned.',
        ),
        variant: AppButtonVariant.primary,
      );
      add(
        'settle_final',
        'Settle final bill',
        _settleFinal,
        icon: AppIcons.receipt,
      );
      add(
        'issue_invoice',
        'Issue invoice',
        () => _run(
          'issue_invoice',
          () => _api.issueInvoice(_org, id),
          success: 'Tax invoice issued.',
        ),
        icon: AppIcons.fileCheck,
      );
      add(
        'complete',
        'Complete booking',
        () => _step(
          'complete',
          'owner/bookings/$id/complete',
          'Booking completed.',
          confirm: 'Complete this booking?',
        ),
        variant: AppButtonVariant.success,
        icon: AppIcons.checkCircle,
      );
    }
    add(
      'request_extension',
      'Request extension',
      _extend,
      icon: AppIcons.timer,
    );
    add(
      'track',
      'Track delivery',
      () => context.push(
        '/${customer ? 'customer' : 'owner'}/bookings/$id/track',
      ),
      icon: AppIcons.tracking,
    );
    add(
      'cancel',
      'Cancel booking',
      _cancel,
      variant: AppButtonVariant.ghost,
      icon: AppIcons.closeCircle,
    );
    add(
      'request_cancellation',
      'Request cancellation',
      _cancel,
      variant: AppButtonVariant.ghost,
      icon: AppIcons.closeCircle,
    );
    add(
      'dispute',
      'Raise a dispute',
      _dispute,
      variant: AppButtonVariant.ghost,
      icon: AppIcons.flag,
    );

    if (buttons.isEmpty) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.md),
      child: SectionCard(
        title: 'Next steps',
        icon: AppIcons.arrowRight,
        child: Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          children: buttons,
        ),
      ),
    );
  }

  Future<void> _payDriver(DriverCompensation compensation) async {
    final reference = TextEditingController();
    final saved = await showDialog<String>(
      context: context,
      builder: (ctx) => AppDialog(
        title: 'Record driver payment',
        icon: AppIcons.payments,
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            InfoRow(label: 'Driver', value: compensation.driverName),
            InfoRow(
              label: 'Agreed amount',
              value: money(compensation.agreedAmountPaise),
            ),
            AppTextField(
              label: 'Transaction reference',
              controller: reference,
              maxLength: 120,
            ),
          ],
        ),
        actions: [
          AppButton(
            label: 'Cancel',
            variant: AppButtonVariant.secondary,
            onPressed: () => Navigator.of(ctx).pop(),
          ),
          AppButton(
            label: 'Record payment',
            onPressed: () {
              if (reference.text.trim().isNotEmpty) {
                Navigator.of(ctx).pop(reference.text.trim());
              }
            },
          ),
        ],
      ),
    );
    reference.dispose();
    if (saved == null) return;
    await _run('driver_pay', () async {
      await _api.payDriverCompensation(_org, compensation.id, saved);
      ref.invalidate(driverCompensationForBookingProvider((_org, b.id)));
      if (mounted) AppToast.success(context, 'Driver payment recorded.');
    });
  }
}

/// Yes/no confirmation.
Future<bool> confirmAction(
  BuildContext context, {
  required String title,
  String? message,
  String confirm = 'Confirm',
}) async {
  final ok = await showDialog<bool>(
    context: context,
    builder: (ctx) => AppDialog(
      title: title,
      content: Text(message ?? 'This cannot be undone.'),
      actions: [
        AppButton(
          label: 'Back',
          variant: AppButtonVariant.secondary,
          onPressed: () => Navigator.of(ctx).pop(false),
        ),
        AppButton(
          key: const Key('confirm-action'),
          label: confirm,
          onPressed: () => Navigator.of(ctx).pop(true),
        ),
      ],
    ),
  );
  return ok == true;
}

class _OperatorSheet extends ConsumerWidget {
  const _OperatorSheet();

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ops = ref.watch(ownerOperatorsProvider);
    return SafeArea(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(
          AppSpacing.md,
          0,
          AppSpacing.md,
          AppSpacing.md,
        ),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'Assign an operator',
              style: Theme.of(context).textTheme.titleMedium,
            ),
            const Gap(AppSpacing.sm),
            ops.when(
              loading: () => const LinearProgressIndicator(),
              error: (e, _) => Text(describeError(e)),
              data: (list) => list.isEmpty
                  ? const Text(
                      'No operators in your team yet. Add drivers under Profile → My drivers.',
                    )
                  : ConstrainedBox(
                      constraints: const BoxConstraints(maxHeight: 420),
                      child: ListView(
                        shrinkWrap: true,
                        children: [
                          for (final o in list)
                            ListTile(
                              key: Key('operator-${o.userId}'),
                              contentPadding: EdgeInsets.zero,
                              leading: AppAvatar(name: o.name),
                              title: Text(o.name),
                              subtitle: Text(
                                [
                                  availabilityLabel(o.availability),
                                  if (o.currentJob != null)
                                    'on ${o.currentJob}',
                                  if (o.rating != null)
                                    '★ ${o.rating!.toStringAsFixed(1)}',
                                ].join(' · '),
                              ),
                              enabled: (o.availability == 'available' || o.availability == 'off_duty'),
                              onTap: (o.availability == 'available' || o.availability == 'off_duty')
                                  ? () => Navigator.of(context).pop(o)
                                  : null,
                            ),
                        ],
                      ),
                    ),
            ),
          ],
        ),
      ),
    );
  }
}

// ---- extensions -----------------------------------------------------------------------------------

class _ExtensionCard extends ConsumerStatefulWidget {
  const _ExtensionCard({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;
  @override
  ConsumerState<_ExtensionCard> createState() => _ExtensionCardState();
}

class _ExtensionCardState extends ConsumerState<_ExtensionCard> {
  bool _busy = false;

  Future<void> _answer(String decision) async {
    String? reason;
    if (decision == 'decline') {
      reason = await askReason(
        context,
        title: 'Decline the extension?',
        confirm: 'Decline',
      );
      if (reason == null) return;
    }
    if (!mounted) return;
    setState(() => _busy = true);
    final x = widget.b.pendingExtension!;
    await runComms(
      context,
      () => ref
          .read(bookingApiProvider)
          .answerExtension(
            ref.read(sideOrgProvider(widget.side)),
            widget.b.id,
            x.id,
            decision,
            reason: reason,
          ),
      success: {
        'approve':
            'Extension approved. The new end is ${Fmt.dateTime(x.newEnd)}.',
        'decline': 'Extension declined.',
        'withdraw': 'Extension request withdrawn.',
      }[decision],
    );
    if (mounted) setState(() => _busy = false);
    refreshBooking(ref, widget.side, widget.b.id);
  }

  @override
  Widget build(BuildContext context) {
    final x = widget.b.pendingExtension!;
    final b = widget.b;
    final fromOther =
        (x.requestedSide == 'customer') !=
        (widget.side == BookingSide.customer);
    return AppCard(
      key: const Key('extension-card'),
      borderColor: AppColors.orange500,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(
            fromOther
                ? 'Extension requested by the ${x.requestedSide}'
                : 'Your extension request',
            style: Theme.of(context).textTheme.titleSmall,
          ),
          const Gap(4),
          Text(
            'New end ${Fmt.dateTime(x.newEnd)} (was ${Fmt.dateTime(x.previousEnd)}). Extra charge about ${money(x.amountPaise)}.',
          ),
          if (x.note.isNotEmpty)
            Text('“${x.note}”', style: Theme.of(context).textTheme.bodySmall),
          const Gap(AppSpacing.ms),
          Wrap(
            spacing: AppSpacing.sm,
            runSpacing: AppSpacing.sm,
            children: [
              if (b.can('approve_extension'))
                AppButton(
                  key: const Key('approve-extension'),
                  label: 'Approve',
                  isLoading: _busy,
                  onPressed: _busy ? null : () => _answer('approve'),
                ),
              if (b.can('decline_extension'))
                AppButton(
                  key: const Key('decline-extension'),
                  label: 'Decline',
                  variant: AppButtonVariant.secondary,
                  onPressed: _busy ? null : () => _answer('decline'),
                ),
              if (b.can('withdraw_extension'))
                AppButton(
                  key: const Key('withdraw-extension'),
                  label: 'Withdraw',
                  variant: AppButtonVariant.secondary,
                  onPressed: _busy ? null : () => _answer('withdraw'),
                ),
            ],
          ),
        ],
      ),
    );
  }
}

class _ExtensionDialog extends ConsumerStatefulWidget {
  const _ExtensionDialog({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;
  @override
  ConsumerState<_ExtensionDialog> createState() => _ExtensionDialogState();
}

class _ExtensionDialogState extends ConsumerState<_ExtensionDialog> {
  late DateTime _newEnd = widget.b.end.add(const Duration(days: 1));
  final _note = TextEditingController();
  ExtensionQuote? _quote;
  String? _error;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    _price();
  }

  @override
  void dispose() {
    _note.dispose();
    super.dispose();
  }

  Future<void> _price() async {
    setState(() {
      _quote = null;
      _error = null;
    });
    try {
      final q = await ref
          .read(bookingApiProvider)
          .quoteExtension(
            ref.read(sideOrgProvider(widget.side)),
            widget.b.id,
            _newEnd,
          );
      if (mounted) setState(() => _quote = q);
    } catch (e) {
      if (mounted) setState(() => _error = describeError(e));
    }
  }

  Future<void> _pickDate() async {
    final end = widget.b.end;
    final picked = await showDatePicker(
      context: context,
      initialDate: _newEnd,
      firstDate: DateTime(
        end.year,
        end.month,
        end.day,
      ).add(const Duration(days: 1)),
      lastDate: end.add(const Duration(days: 90)),
      helpText: 'New end date',
    );
    if (picked == null) return;
    setState(
      () => _newEnd = DateTime(
        picked.year,
        picked.month,
        picked.day,
        end.hour,
        end.minute,
      ),
    );
    await _price();
  }

  Future<void> _send() async {
    setState(() => _busy = true);
    try {
      await ref
          .read(bookingApiProvider)
          .requestExtension(
            ref.read(sideOrgProvider(widget.side)),
            widget.b.id,
            _newEnd,
            note: _note.text.trim(),
          );
      if (mounted) {
        AppToast.success(
          context,
          'Extension requested. The other party will answer.',
        );
        Navigator.of(context).pop(true);
      }
    } catch (e) {
      if (mounted) setState(() => _error = describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final q = _quote;
    return AppDialog(
      title: 'Extend the rental',
      icon: AppIcons.timer,
      content: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text('Current end: ${Fmt.dateTime(widget.b.end)}'),
          const Gap(AppSpacing.sm),
          PickerField(
            key: const Key('extension-date'),
            label: 'New end',
            value: Fmt.dateTime(_newEnd),
            icon: AppIcons.calendar,
            onTap: _pickDate,
          ),
          const Gap(AppSpacing.sm),
          if (q == null && _error == null) const LinearProgressIndicator(),
          if (q != null)
            InlineBanner(
              key: const Key('extension-quote'),
              tone: q.available ? AppTone.info : AppTone.danger,
              message: q.available
                  ? 'Extra charge about ${money(q.amountPaise)} incl. taxes.'
                  : (q.problem ?? 'The machine is not free then.'),
            ),
          if (_error != null)
            InlineBanner(tone: AppTone.danger, message: _error!),
          const Gap(AppSpacing.sm),
          AppTextField(
            label: 'Note (optional)',
            controller: _note,
            maxLength: 500,
          ),
        ],
      ),
      actions: [
        AppButton(
          label: 'Cancel',
          variant: AppButtonVariant.secondary,
          onPressed: () => Navigator.of(context).pop(false),
        ),
        AppButton(
          key: const Key('send-extension'),
          label: 'Send request',
          isLoading: _busy,
          onPressed: q?.available == true && !_busy ? _send : null,
        ),
      ],
    );
  }
}

// ---- final bill, dispute --------------------------------------------------------------------------

class _FinalBillDialog extends ConsumerStatefulWidget {
  const _FinalBillDialog({required this.b});
  final BookingDetail b;
  @override
  ConsumerState<_FinalBillDialog> createState() => _FinalBillDialogState();
}

class _FinalBillDialogState extends ConsumerState<_FinalBillDialog> {
  /// What really happened: the handover and the return from the status
  /// history, else the booked times (never in the future; the server refuses that).
  late final (DateTime, DateTime) _actual = () {
    DateTime? at(String status) => widget.b.timeline
        .where((e) => e.to == status)
        .map((e) => e.at)
        .lastOrNull;
    final now = DateTime.now();
    final end =
        at('returned') ?? (widget.b.end.isAfter(now) ? now : widget.b.end);
    var start = at('active') ?? widget.b.start;
    if (!start.isBefore(end)) start = end.subtract(const Duration(hours: 1));
    return (start, end);
  }();

  final _hours = TextEditingController();
  final _damage = TextEditingController();
  final _note = TextEditingController();
  FinalBill? _preview;
  String? _error;
  bool _busy = false;

  @override
  void dispose() {
    for (final c in [_hours, _damage, _note]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _go({required bool save}) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final bill = await ref
          .read(bookingApiProvider)
          .settleFinal(
            ref.read(sideOrgProvider(BookingSide.owner)),
            widget.b.id,
            actualStart: _actual.$1,
            actualEnd: _actual.$2,
            operatingHours: double.tryParse(_hours.text.trim()) ?? 0,
            damagePaise: ((double.tryParse(_damage.text.trim()) ?? 0) * 100)
                .round(),
            damageNote: _note.text.trim(),
            dryRun: !save,
          );
      if (!mounted) return;
      if (save) {
        AppToast.success(
          context,
          'Final bill saved: ${money(bill.totalPaise)}.',
        );
        Navigator.of(context).pop(true);
      } else {
        setState(() => _preview = bill);
      }
    } catch (e) {
      if (mounted) setState(() => _error = describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final p = _preview;
    return AppDialog(
      title: 'Final bill',
      icon: AppIcons.receipt,
      content: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text(
              'Uses the rates frozen in the booking. Worked from ${Fmt.dateTime(_actual.$1)} to ${Fmt.dateTime(_actual.$2)}; '
              'the booked period (${Fmt.dateRange(widget.b.start, widget.b.end)}) is the minimum charge.',
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              key: const Key('final-hours'),
              label: 'Hours worked (optional)',
              controller: _hours,
              keyboardType: TextInputType.number,
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              key: const Key('final-damage'),
              label: 'Damage recovery ₹ (optional)',
              controller: _damage,
              keyboardType: TextInputType.number,
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              label: 'Damage note',
              controller: _note,
              maxLength: 500,
            ),
            if (p != null) ...[
              for (final c in p.charges)
                InfoRow(label: c.label, value: money(c.paise)),
              InfoRow(
                key: const Key('final-preview-total'),
                label: 'Final total',
                value: money(p.totalPaise),
                emphasize: true,
              ),
            ],
            if (_error != null)
              InlineBanner(tone: AppTone.danger, message: _error!),
          ],
        ),
      ),
      actions: [
        AppButton(
          label: 'Preview',
          variant: AppButtonVariant.secondary,
          onPressed: _busy ? null : () => _go(save: false),
        ),
        AppButton(
          key: const Key('save-final'),
          label: 'Save bill',
          isLoading: _busy,
          onPressed: _busy ? null : () => _go(save: true),
        ),
      ],
    );
  }
}

class _DisputeDialog extends ConsumerStatefulWidget {
  const _DisputeDialog({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;
  @override
  ConsumerState<_DisputeDialog> createState() => _DisputeDialogState();
}

class _DisputeDialogState extends ConsumerState<_DisputeDialog> {
  static const _categories = {
    'hour_meter': 'Hour meter',
    'damage': 'Damage',
    'payment': 'Payment',
    'no_show': 'No show',
    'late_arrival': 'Late arrival',
    'quality': 'Machine quality',
    'other': 'Other',
  };
  String _category = 'other';
  final _summary = TextEditingController();
  bool _busy = false;

  @override
  void dispose() {
    _summary.dispose();
    super.dispose();
  }

  Future<void> _send() async {
    setState(() => _busy = true);
    final ok = await runComms(
      context,
      () => ref
          .read(bookingApiProvider)
          .dispute(
            ref.read(sideOrgProvider(widget.side)),
            widget.b.id,
            category: _category,
            summary: _summary.text.trim(),
          ),
      success: 'Dispute raised. Our team will contact both parties.',
    );
    if (mounted) {
      setState(() => _busy = false);
      if (ok) Navigator.of(context).pop(true);
    }
  }

  @override
  Widget build(BuildContext context) => AppDialog(
    title: 'Raise a dispute',
    icon: AppIcons.flag,
    tone: AppTone.danger,
    content: Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppChoiceChips<String>(
          options: _categories.keys.toList(),
          selected: _category,
          labelOf: (c) => _categories[c]!,
          onSelected: (c) => setState(() => _category = c),
        ),
        const Gap(AppSpacing.sm),
        AppTextField(
          label: 'What happened? (at least 10 characters)',
          controller: _summary,
          maxLines: 4,
          maxLength: 2000,
          onChanged: (_) => setState(() {}),
        ),
      ],
    ),
    actions: [
      AppButton(
        label: 'Cancel',
        variant: AppButtonVariant.secondary,
        onPressed: () => Navigator.of(context).pop(false),
      ),
      AppButton(
        label: 'Raise dispute',
        variant: AppButtonVariant.danger,
        isLoading: _busy,
        onPressed: _summary.text.trim().length >= 10 && !_busy ? _send : null,
      ),
    ],
  );
}

// ---- tracking, invoices, history ------------------------------------------------------------------

class _TrackCard extends StatelessWidget {
  const _TrackCard({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;

  @override
  Widget build(BuildContext context) {
    void open() => context.push(
      '/${side == BookingSide.customer ? 'customer' : 'owner'}/bookings/${b.id}/track',
    );
    return AppCard(
      onTap: open,
      child: Row(
        children: [
          const IconTile(icon: AppIcons.tracking, tone: AppTone.brand),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(
                  'Live tracking',
                  style: Theme.of(context).textTheme.titleSmall,
                ),
                Text(
                  'Where the machine is and how far from the site',
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
                ),
              ],
            ),
          ),
          AppButton(
            key: const Key('open-tracking'),
            label: 'Track',
            size: AppButtonSize.small,
            variant: AppButtonVariant.secondary,
            onPressed: open,
          ),
        ],
      ),
    );
  }
}

class _InvoicesCard extends ConsumerWidget {
  const _InvoicesCard({required this.b, required this.side});
  final BookingDetail b;
  final BookingSide side;

  Future<void> _download(
    BuildContext context,
    WidgetRef ref,
    InvoiceInfo inv,
  ) async {
    try {
      final url = await ref
          .read(bookingApiProvider)
          .invoiceLink(ref.read(sideOrgProvider(side)), inv.id);
      final opened = await ref.read(linkOpenerProvider)(url);
      if (!opened && context.mounted) {
        AppToast.error(context, 'Could not open the invoice.');
      }
    } catch (e) {
      if (context.mounted) AppToast.error(context, describeError(e));
    }
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final list = ref.watch(bookingInvoicesProvider((side, b.id)));
    return SectionCard(
      title: 'Invoices',
      icon: AppIcons.receipt,
      child: list.when(
        loading: () => const LinearProgressIndicator(),
        error: (e, _) => Text(describeError(e)),
        data: (items) => items.isEmpty
            ? Text(
                side == BookingSide.owner
                    ? 'Issue the tax invoice once the final bill is settled.'
                    : 'The owner issues the tax invoice after the return.',
              )
            : Column(
                children: [
                  for (final i in items)
                    ListRow(
                      key: Key('invoice-${i.id}'),
                      title: i.number,
                      onTap: () =>
                          context.push(DocumentRoutes.invoice(side, i.id)),
                      subtitle:
                          '${i.title} · ${money(i.totalPaise)}${i.issuedAt == null ? '' : ' · ${Fmt.date(i.issuedAt!)}'}',
                      leading: const IconTile(icon: AppIcons.receipt),
                      trailing: AppButton(
                        key: Key('download-${i.id}'),
                        label: 'Download',
                        size: AppButtonSize.small,
                        variant: AppButtonVariant.secondary,
                        icon: AppIcons.download,
                        onPressed: () => _download(context, ref, i),
                      ),
                    ),
                ],
              ),
      ),
    );
  }
}

class _HistoryCard extends StatelessWidget {
  const _HistoryCard({required this.b});
  final BookingDetail b;

  @override
  Widget build(BuildContext context) {
    if (b.timeline.isEmpty) return const SizedBox.shrink();
    final events = b.timeline.reversed.toList();
    return SectionCard(
      title: 'Status history',
      icon: AppIcons.history,
      child: StatusTimeline(
        steps: [
          for (var i = 0; i < events.length; i++)
            TimelineStep(
              title: bookingEventLabel(events[i].to),
              subtitle: [
                Fmt.dateTime(events[i].at),
                if (events[i].actorRole != null) 'by ${events[i].actorRole}',
                if (events[i].reason != null && events[i].reason!.isNotEmpty)
                  events[i].reason!,
              ].join(' · '),
              done: i > 0,
              current: i == 0,
            ),
        ],
      ),
    );
  }
}
