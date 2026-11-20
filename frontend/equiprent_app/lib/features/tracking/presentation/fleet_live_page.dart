import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../fleet_manager/data/fleet_api.dart';
import '../../verification/presentation/verification_widgets.dart' show AsyncBody;
import '../data/live_track.dart';
import '../data/tracking_api.dart';
import 'tracking_map.dart';

/// Live map of the organisation's machines. Fleet managers see only the
/// machines in their scope (the server filters both the board and the
/// stream). Positions come from the board's last known location and are
/// updated from the live stream.
class FleetLivePage extends ConsumerWidget {
  const FleetLivePage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final org = ref.watch(fleetOrgProvider);
    final board = ref.watch(fleetBoardProvider(null));
    final live = org == null ? const <String, LiveUpdate>{} : ref.watch(fleetLiveProvider(org));
    return AppPage(
      title: 'Live fleet map',
      subtitle: live.isEmpty ? 'Last known positions' : 'Live',
      topLevel: false,
      actions: [IconButton(tooltip: 'Refresh', icon: const Icon(AppIcons.refresh), onPressed: () => ref.invalidate(fleetBoardProvider(null)))],
      body: AsyncBody(
        value: board,
        onRetry: () => ref.invalidate(fleetBoardProvider(null)),
        builder: (machines) {
          final rows = [
            for (final m in machines)
              () {
                final u = live[m.id];
                final point = u != null ? LatLngPoint(u.lat, u.lng) : (m.lastLat == null ? null : LatLngPoint(m.lastLat!, m.lastLng!));
                return (machine: m, point: point, at: u?.recordedAt ?? m.lastSeen, source: u?.source, live: u != null);
              }(),
          ];
          final located = rows.where((r) => r.point != null).toList();
          if (machines.isEmpty) {
            return const PageBody(
              children: [EmptyState(icon: AppIcons.fleet, title: 'No machines in your fleet')],
            );
          }
          return PageBody(
            children: [
              if (located.isEmpty)
                const EmptyState(
                  key: Key('no-positions'),
                  icon: AppIcons.map,
                  title: 'No positions yet',
                  message: 'Machines appear here once an operator shares location on a job or a tracker is fitted.',
                )
              else
                TrackingMap(
                  height: 340,
                  pins: [
                    for (final r in located)
                      TrackPin(id: r.machine.id, point: r.point!, label: r.machine.title, kind: r.source == null ? PinKind.machine : pinKindFor(r.source!)),
                  ],
                ),
              const Gap(AppSpacing.md),
              SectionCard(
                title: 'Machines',
                subtitle: '${located.length} of ${machines.length} with a known position',
                icon: AppIcons.fleet,
                child: Column(
                  children: [
                    for (final r in rows)
                      ListRow(
                        key: Key('live-${r.machine.id}'),
                        dense: true,
                        title: r.machine.title,
                        subtitle: [
                          if (r.machine.currentReference != null) r.machine.currentReference!,
                          r.at == null ? 'No position' : 'Seen ${Fmt.ago(r.at!, DateTime.now())}',
                        ].join(' · '),
                        trailing: r.live ? const StatusBadge(label: 'Live', tone: AppTone.success, dot: true) : null,
                      ),
                  ],
                ),
              ),
              const Gap(AppSpacing.sm),
              const InlineBanner(
                tone: AppTone.info,
                message: "Positions from an operator's phone show where the phone is. Fit a tracker for the machine's own position.",
              ),
              const Gap(AppSpacing.xl),
            ],
          );
        },
      ),
    );
  }
}
