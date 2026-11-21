import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart'
    show AsyncBody;
import 'admin_read_only_dashboard.dart';

/// The Disputes tab: disputes waiting for a decision. Read-only, like the rest of the admin panel:
/// the operations team decides them elsewhere.
class AdminDisputesPage extends ConsumerWidget {
  const AdminDisputesPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final disputes = ref.watch(adminOpenDisputesProvider);
    return AppPage(
      title: 'Disputes',
      subtitle: 'Waiting for a decision',
      actions: [
        IconButton(
          key: const Key('disputes-refresh'),
          tooltip: 'Refresh',
          icon: const Icon(AppIcons.refresh),
          onPressed: () => ref.invalidate(adminOpenDisputesProvider),
        ),
      ],
      body: AsyncBody(
        value: disputes,
        onRetry: () => ref.invalidate(adminOpenDisputesProvider),
        builder: (list) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(adminOpenDisputesProvider),
          child: PageBody(
            children: [
              const InlineBanner(
                tone: AppTone.neutral,
                icon: AppIcons.shield,
                title: 'Monitoring access only',
                message:
                    'This list has no controls: disputes are decided by the operations team.',
              ),
              const Gap(AppSpacing.md),
              if (list.isEmpty)
                const EmptyState(
                  icon: AppIcons.checkCircle,
                  title: 'No open disputes',
                  message: 'Disputes raised on bookings appear here.',
                )
              else
                for (final d in list) AdminDisputeTile(dispute: d),
            ],
          ),
        ),
      ),
    );
  }
}
