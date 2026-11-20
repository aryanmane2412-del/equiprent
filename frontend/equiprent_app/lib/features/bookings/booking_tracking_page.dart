import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart' show bookingStatusLabel;
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../tracking/data/live_track.dart';
import '../tracking/data/tracking_api.dart';
import '../tracking/presentation/share_location_card.dart';
import '../tracking/presentation/tracking_map.dart';

export '../tracking/data/live_track.dart' show trackPollProvider;

/// Booking statuses during which the machine's location is shared.
const trackableStatuses = {
  'dispatched',
  'on_site',
  'active',
  'extension_requested',
  'cancellation_requested',
  'return_initiated',
};

String distanceLabel(double m) =>
    m < 1000 ? '${m.round()} m' : '${(m / 1000).toStringAsFixed(1)} km';
String durationLabel(double s) {
  final minutes = (s / 60).ceil();
  return minutes < 60
      ? '$minutes min'
      : '${minutes ~/ 60} h ${minutes % 60} min';
}

String sourceLabel(String s) => switch (s) {
  'telematics' => 'Tracker on the machine',
  'simulated' => 'Simulated (development)',
  _ => "Operator's phone",
};

String endReasonLabel(String? r) => switch (r) {
  null => '',
  'arrived' => 'arrived',
  'stopped_by_user' => 'sharing stopped',
  'session_ended' || 'returned' => 'job ended',
  'consent_withdrawn' => 'consent withdrawn',
  'idle_timeout' => 'no updates (timed out)',
  'cancelled' => 'booking cancelled',
  _ => r.replaceAll('_', ' '),
};

/// Live tracking for a booking: where the machine is (and how we know),
/// the route and ETA while it travels, the sharing session and past trips.
class BookingTrackingPage extends ConsumerWidget {
  const BookingTrackingPage({
    super.key,
    required this.side,
    required this.bookingId,
  });
  final BookingSide side;
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final key = (side, bookingId);
    final live = ref.watch(liveTrackProvider(key));
    final booking = ref.watch(bookingDetailProvider(key)).value;
    return AppPage(
      title: 'Track machine',
      subtitle: booking?.reference,
      topLevel: false,
      actions: [
        IconButton(
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () => ref.read(liveTrackProvider(key).notifier).refresh(),
        ),
      ],
      body: AsyncBody(
        value: live,
        onRetry: () => ref.invalidate(liveTrackProvider(key)),
        builder: (l) {
          final t = l.view;
          if (t.hidden) {
            return PageBody(
              children: [
                EmptyState(
                  key: const Key('tracking-hidden'),
                  icon: AppIcons.lock,
                  title: 'Location not shared yet',
                  message:
                      'You can follow the machine from dispatch until it is returned. Current stage: ${bookingStatusLabel(t.status)}.',
                ),
              ],
            );
          }
          final eq = t.equipment;
          final op = t.operator;
          final pins = <TrackPin>[
            if (eq != null)
              TrackPin(
                id: 'machine',
                point: eq.point,
                label: 'Machine',
                kind: pinKindFor(eq.source),
              ),
            if (op != null && eq?.source == 'telematics')
              TrackPin(
                id: 'phone',
                point: op.point,
                label: 'Operator',
                kind: PinKind.phone,
              ),
            TrackPin(
              id: 'site',
              point: t.destination,
              label: 'Site',
              kind: PinKind.site,
            ),
          ];
          final canShare =
              side == BookingSide.owner &&
              booking != null &&
              booking.operator == null &&
              trackableStatuses.contains(t.status);
          return PageBody(
            children: [
              TrackingMap(
                pins: pins,
                travelled: t.route,
                planned: t.eta?.polyline ?? const [],
                followId: 'machine',
              ),
              const Gap(AppSpacing.md),
              SectionCard(
                title: bookingStatusLabel(t.status),
                subtitle: booking == null
                    ? null
                    : '${booking.equipmentTitle} → ${booking.siteLabel.isEmpty ? 'site' : booking.siteLabel}',
                icon: AppIcons.tracking,
                trailing: _ModeBadge(mode: l.mode),
                child: Column(
                  children: [
                    if (eq == null)
                      const InfoRow(
                        key: Key('no-position'),
                        label: 'Position',
                        value: 'Waiting for the first position',
                        icon: AppIcons.signal,
                      )
                    else ...[
                      InfoRow(
                        key: const Key('position-source'),
                        label: 'Location from',
                        value: sourceLabel(eq.source),
                        icon: eq.source == 'telematics'
                            ? AppIcons.signal
                            : AppIcons.phone,
                      ),
                      InfoRow(
                        key: const Key('last-update'),
                        label: 'Last update',
                        value: eq.recordedAt == null
                            ? '—'
                            : '${Fmt.dateTime(eq.recordedAt!)} (${Fmt.ago(eq.recordedAt!, DateTime.now())})',
                        icon: AppIcons.clock,
                      ),
                      if (eq.accuracyM != null)
                        InfoRow(
                          label: 'Accuracy',
                          value: '± ${eq.accuracyM!.round()} m',
                          icon: AppIcons.locate,
                        ),
                    ],
                    if (t.eta != null) ...[
                      InfoRow(
                        key: const Key('distance-to-site'),
                        label: 'Distance to site',
                        value: distanceLabel(t.eta!.distanceM),
                        icon: AppIcons.route,
                        emphasize: true,
                      ),
                      InfoRow(
                        key: const Key('eta'),
                        label: 'Estimated arrival',
                        value: durationLabel(t.eta!.durationS),
                        icon: AppIcons.timer,
                      ),
                      InfoRow(
                        key: const Key('eta-method'),
                        label: 'Estimate',
                        value: t.eta!.roadRoute
                            ? 'Road route with traffic (Google)'
                            : 'Approximate (straight line, heavy-transport speed)',
                      ),
                    ],
                    if (booking?.operator != null)
                      InfoRow(
                        label: 'Operator',
                        value: booking!.operator!.name,
                        icon: AppIcons.operator,
                      ),
                    if (booking?.operator?.phone != null)
                      InfoRow(
                        label: 'Operator phone',
                        value: booking!.operator!.phone!,
                        icon: AppIcons.phone,
                      ),
                  ],
                ),
              ),
              if (t.sourceNote.isNotEmpty) ...[
                const Gap(AppSpacing.sm),
                InlineBanner(
                  key: const Key('source-note'),
                  tone: AppTone.info,
                  icon: AppIcons.info,
                  message: t.sourceNote,
                ),
              ],
              const Gap(AppSpacing.md),
              SectionCard(
                title: 'Location sharing',
                icon: AppIcons.signal,
                child: InfoRow(
                  key: const Key('session-state'),
                  label: t.session.active ? 'Sharing now' : 'Not sharing',
                  value: t.session.active
                      ? 'Since ${t.session.startedAt == null ? '—' : Fmt.time(t.session.startedAt!)}'
                      : 'Positions are recorded only while a session is open',
                  valueColor: t.session.active ? AppColors.green700 : null,
                ),
              ),
              if (canShare) ...[
                const Gap(AppSpacing.md),
                ShareLocationCard(
                  bookingId: bookingId,
                  destination: t.destination,
                  onChanged: () =>
                      ref.read(liveTrackProvider(key).notifier).refresh(),
                ),
              ],
              if (t.trips.isNotEmpty) ...[
                const Gap(AppSpacing.md),
                SectionCard(
                  title: 'Trips',
                  subtitle: 'Location history by leg',
                  icon: AppIcons.history,
                  child: Column(
                    children: [
                      for (final trip in t.trips)
                        ListRow(
                          key: Key('trip-${trip.id}'),
                          dense: true,
                          title: trip.label,
                          subtitle: [
                            Fmt.dateTime(trip.startedAt),
                            trip.endedAt == null
                                ? 'in progress'
                                : 'ended ${Fmt.time(trip.endedAt!)}${trip.endReason == null ? '' : ' · ${endReasonLabel(trip.endReason)}'}',
                            if (trip.distanceM != null)
                              distanceLabel(trip.distanceM!),
                            '${trip.points} points',
                          ].join(' · '),
                          trailing: const Icon(AppIcons.chevronRight),
                          onTap: trip.points == 0
                              ? null
                              : () => _history(context, trip),
                        ),
                    ],
                  ),
                ),
              ],
              const Gap(AppSpacing.xl),
            ],
          );
        },
      ),
    );
  }

  Future<void> _history(BuildContext context, TripRecord trip) =>
      showAppSheet<void>(
        context,
        title: trip.label,
        subtitle: Fmt.dateTime(trip.startedAt),
        builder: (_) =>
            _TripHistory(side: side, bookingId: bookingId, trip: trip),
      );
}

class _ModeBadge extends StatelessWidget {
  const _ModeBadge({required this.mode});
  final LiveMode mode;
  @override
  Widget build(BuildContext context) => switch (mode) {
    LiveMode.live => const StatusBadge(
      key: Key('mode-live'),
      label: 'Live',
      tone: AppTone.success,
      dot: true,
    ),
    LiveMode.connecting => const StatusBadge(
      key: Key('mode-connecting'),
      label: 'Connecting',
      tone: AppTone.neutral,
    ),
    LiveMode.polling => const StatusBadge(
      key: Key('mode-polling'),
      label: 'Refreshing',
      tone: AppTone.warning,
    ),
  };
}

class _TripHistory extends ConsumerWidget {
  const _TripHistory({
    required this.side,
    required this.bookingId,
    required this.trip,
  });
  final BookingSide side;
  final String bookingId;
  final TripRecord trip;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final h = ref.watch(tripHistoryProvider((side, bookingId, trip.id)));
    return h.when(
      loading: () => const SizedBox(height: 200, child: LoadingView()),
      error: (e, _) => ErrorState(
        message: '$e',
        onRetry: () =>
            ref.invalidate(tripHistoryProvider((side, bookingId, trip.id))),
      ),
      data: (pts) {
        if (pts.isEmpty) {
          return const EmptyState(
            icon: AppIcons.route,
            title: 'No positions',
            compact: true,
          );
        }
        final path = [for (final p in pts) LatLngPoint(p.lat, p.lng)];
        return Column(
          key: const Key('trip-history'),
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            TrackingMap(
              height: 220,
              travelled: path,
              pins: [
                TrackPin(
                  id: 'start',
                  point: path.first,
                  label: 'Start',
                  kind: PinKind.machine,
                ),
                TrackPin(
                  id: 'end',
                  point: path.last,
                  label: 'Last',
                  kind: pinKindFor(pts.last.source),
                ),
              ],
            ),
            const Gap(AppSpacing.sm),
            InfoRow(label: 'Positions', value: '${pts.length}'),
            InfoRow(label: 'From', value: Fmt.dateTime(pts.first.recordedAt)),
            InfoRow(label: 'To', value: Fmt.dateTime(pts.last.recordedAt)),
            InfoRow(label: 'Source', value: sourceLabel(pts.last.source)),
          ],
        );
      },
    );
  }
}
