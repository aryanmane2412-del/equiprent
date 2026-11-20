import 'package:flutter/foundation.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../data/share_controller.dart';
import '../data/tracking_api.dart';

String? _platform() {
  if (kIsWeb) return 'web';
  return switch (defaultTargetPlatform) {
    TargetPlatform.android => 'android',
    TargetPlatform.iOS => 'ios',
    _ => 'other',
  };
}

/// Lets the operator (or the owner when no operator is assigned) share this
/// device's location for one job. Sharing starts only after reading the
/// tracking notice and agreeing; it can be stopped at any time and stops by
/// itself when the job leaves the trackable stages.
class ShareLocationCard extends ConsumerWidget {
  const ShareLocationCard({super.key, required this.bookingId, required this.destination, this.onChanged});
  final String bookingId;
  final LatLngPoint destination;
  final VoidCallback? onChanged;

  Future<void> _start(BuildContext context, WidgetRef ref) async {
    final TrackingPolicy policy;
    try {
      policy = await ref.read(trackingPolicyProvider.future);
    } catch (e) {
      if (context.mounted) AppToast.show(context, 'Could not load the tracking notice: $e', tone: AppTone.danger);
      return;
    }
    if (!context.mounted) return;
    final background = await showDialog<bool>(
      context: context,
      builder: (_) => ConsentDialog(policy: policy),
    );
    if (background == null) return;
    await ref.read(shareControllerProvider(bookingId).notifier).start(policy: policy, background: background, destination: destination, platform: _platform());
    onChanged?.call();
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final s = ref.watch(shareControllerProvider(bookingId));
    final ctrl = ref.read(shareControllerProvider(bookingId).notifier);
    return SectionCard(
      key: const Key('share-location'),
      title: 'Share my location',
      subtitle: 'Shows the customer and owner where the machine is during this job',
      icon: AppIcons.locate,
      trailing: s.simulated ? const StatusBadge(key: Key('simulated-gps'), label: 'Simulated GPS', tone: AppTone.warning) : null,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (s.sharing) ...[
            InfoRow(key: const Key('sharing-on'), label: 'Sharing', value: 'On — this job only', icon: AppIcons.signal, valueColor: AppColors.green700),
            InfoRow(
              label: 'Last position',
              value: s.lastFix == null
                  ? 'Waiting for GPS…'
                  : '${Fmt.time(s.lastFix!.at)}${s.lastFix!.accuracyM == null ? '' : ' · ± ${s.lastFix!.accuracyM!.round()} m'}',
              icon: AppIcons.clock,
            ),
            InfoRow(key: const Key('upload-state'), label: 'Uploaded', value: '${s.uploaded} sent · ${s.pending} waiting', icon: AppIcons.upload),
          ] else
            const Text('Your location is shared only while you choose to share during an active job, and stops automatically when the job ends.'),
          if (s.message != null) ...[
            const Gap(AppSpacing.sm),
            InlineBanner(key: const Key('share-message'), tone: s.phase == SharePhase.blocked ? AppTone.danger : AppTone.warning, message: s.message!),
          ],
          const Gap(AppSpacing.md),
          if (s.sharing)
            AppButton(
              key: const Key('stop-sharing'),
              label: 'Stop sharing',
              icon: AppIcons.pause,
              variant: AppButtonVariant.danger,
              onPressed: () async {
                await ctrl.stop();
                onChanged?.call();
              },
            )
          else
            AppButton(
              key: const Key('start-sharing'),
              label: 'Start sharing',
              icon: AppIcons.play,
              isLoading: s.phase == SharePhase.starting,
              onPressed: s.phase == SharePhase.starting ? null : () => _start(context, ref),
            ),
        ],
      ),
    );
  }
}

/// The tracking notice. Pops with the background choice when the person
/// agrees, or null when they decline.
class ConsentDialog extends StatefulWidget {
  const ConsentDialog({super.key, required this.policy});
  final TrackingPolicy policy;
  @override
  State<ConsentDialog> createState() => _ConsentDialogState();
}

class _ConsentDialogState extends State<ConsentDialog> {
  bool _background = false;

  @override
  Widget build(BuildContext context) {
    final p = widget.policy;
    return AppDialog(
      title: p.title,
      icon: AppIcons.shield,
      content: SingleChildScrollView(
        child: Column(
          key: const Key('tracking-notice'),
          crossAxisAlignment: CrossAxisAlignment.start,
          mainAxisSize: MainAxisSize.min,
          children: [
            for (final point in p.points)
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.xs),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    const Padding(
                      padding: EdgeInsets.only(top: 2),
                      child: Icon(AppIcons.check, size: 16, color: AppColors.green700),
                    ),
                    const Gap(AppSpacing.xs),
                    Expanded(child: Text(point)),
                  ],
                ),
              ),
            if (!kIsWeb)
              SwitchListTile.adaptive(
                key: const Key('background-switch'),
                contentPadding: EdgeInsets.zero,
                value: _background,
                onChanged: (v) => setState(() => _background = v),
                title: const Text('Keep sharing when the app is in the background'),
                subtitle: const Text('Android shows a notification while sharing; your phone may ask for "Allow all the time".'),
              ),
            Text('Notice version ${p.version}', style: Theme.of(context).textTheme.bodySmall),
          ],
        ),
      ),
      actions: [
        AppButton(key: const Key('decline-tracking'), label: 'Not now', variant: AppButtonVariant.secondary, onPressed: () => Navigator.of(context).pop()),
        AppButton(key: const Key('accept-tracking'), label: 'I agree, share', onPressed: () => Navigator.of(context).pop(_background)),
      ],
    );
  }
}
