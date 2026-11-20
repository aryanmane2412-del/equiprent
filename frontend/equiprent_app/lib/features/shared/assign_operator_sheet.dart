import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/widgets/widgets.dart';

/// Pick an operator for a booking (owner + fleet manager). Operators already
/// on a job are shown but disabled.
Future<void> showAssignOperatorSheet(BuildContext context, WidgetRef ref, Booking booking) async {
  final skill = MockData.category(booking.categoryId).name.split(' ').first.replaceAll('Rollers', 'Roller');
  final chosen = await showAppSheet<OperatorProfile>(
    context,
    title: 'Assign operator',
    subtitle: '${booking.equipmentName} · ${Fmt.dateRange(booking.start, booking.end)}',
    builder: (ctx) {
      final sorted = [...MockData.operators]
        ..sort((a, b) {
          int score(OperatorProfile o) => (o.status == OperatorStatus.available ? 0 : 2) + (o.skills.any((s) => skill.startsWith(s)) ? 0 : 1);
          return score(a).compareTo(score(b));
        });
      return Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          for (final o in sorted)
            Builder(builder: (ctx) {
              final busy = o.status != OperatorStatus.available;
              final match = o.skills.any((s) => skill.startsWith(s));
              return ListTile(
                contentPadding: EdgeInsets.zero,
                enabled: !busy,
                leading: AppAvatar(name: o.name),
                title: Text(o.name),
                subtitle: Text('${o.skills.join(', ')} · ${o.experienceYears} yrs · ★ ${o.rating}'),
                trailing: busy
                    ? StatusBadges.operator(o.status)
                    : (match ? const StatusBadge(label: 'Best match', tone: AppTone.success) : const StatusBadge(label: 'Available', tone: AppTone.neutral)),
                onTap: busy ? null : () => Navigator.of(ctx).pop(o),
              );
            }),
        ],
      );
    },
  );
  if (chosen != null && context.mounted) {
    ref.read(ownerBookingsProvider.notifier).assignOperator(booking.id, chosen.name);
    AppToast.success(context, '${chosen.name} assigned to ${booking.reference}. They’ve been notified.');
  }
}
