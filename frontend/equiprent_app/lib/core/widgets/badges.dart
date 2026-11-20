import 'package:flutter/material.dart';

import '../mock/models.dart';
import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';
import '../theme/app_tone.dart';

/// Pill label for statuses.
class StatusBadge extends StatelessWidget {
  const StatusBadge({super.key, required this.label, this.tone = AppTone.neutral, this.icon, this.dot = false});

  final String label;
  final AppTone tone;
  final IconData? icon;

  /// Show a leading dot instead of an icon (e.g. live states).
  final bool dot;

  @override
  Widget build(BuildContext context) {
    return DecoratedBox(
      decoration: BoxDecoration(
        color: tone.background,
        borderRadius: const BorderRadius.all(Radius.circular(AppRadius.full)),
      ),
      child: Padding(
        padding: const EdgeInsets.symmetric(horizontal: AppSpacing.sm + 2, vertical: 3),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (dot) ...[
              Container(width: 6, height: 6, decoration: BoxDecoration(color: tone.foreground, shape: BoxShape.circle)),
              const SizedBox(width: 6),
            ] else if (icon != null) ...[
              Icon(icon, size: 12, color: tone.foreground),
              const SizedBox(width: AppSpacing.xs),
            ],
            Flexible(
              child: Text(
                label,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: Theme.of(context).textTheme.labelMedium?.copyWith(color: tone.foreground),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

/// Badge mapping for each domain status, so colours stay consistent app-wide.
abstract final class StatusBadges {
  static StatusBadge booking(BookingStatus s) => StatusBadge(
        label: s.label,
        dot: s.isLive,
        tone: switch (s) {
          BookingStatus.requested => AppTone.warning,
          BookingStatus.confirmed || BookingStatus.operatorAssigned => AppTone.brand,
          BookingStatus.enRoute || BookingStatus.active => AppTone.info,
          BookingStatus.completed => AppTone.success,
          BookingStatus.cancelled => AppTone.neutral,
          BookingStatus.disputed => AppTone.danger,
        },
      );

  static StatusBadge equipment(EquipmentStatus s) => switch (s) {
        EquipmentStatus.available => const StatusBadge(label: 'Available', tone: AppTone.success, dot: true),
        EquipmentStatus.onRent => const StatusBadge(label: 'On rent', tone: AppTone.info, dot: true),
        EquipmentStatus.maintenance => const StatusBadge(label: 'Maintenance', tone: AppTone.warning, dot: true),
        EquipmentStatus.paused => const StatusBadge(label: 'Paused', tone: AppTone.neutral, dot: true),
      };

  static StatusBadge payment(PaymentStatus s) => switch (s) {
        PaymentStatus.success => const StatusBadge(label: 'Paid', tone: AppTone.success),
        PaymentStatus.pending => const StatusBadge(label: 'Pending', tone: AppTone.warning),
        PaymentStatus.refunded => const StatusBadge(label: 'Refunded', tone: AppTone.info),
        PaymentStatus.failed => const StatusBadge(label: 'Failed', tone: AppTone.danger),
      };

  static StatusBadge job(JobStatus s) => StatusBadge(
        label: s.label,
        dot: s == JobStatus.enRoute || s == JobStatus.inProgress,
        tone: switch (s) {
          JobStatus.assigned => AppTone.accent,
          JobStatus.accepted => AppTone.brand,
          JobStatus.enRoute || JobStatus.onSite || JobStatus.inProgress => AppTone.info,
          JobStatus.completed => AppTone.success,
        },
      );

  static StatusBadge operator(OperatorStatus s) => switch (s) {
        OperatorStatus.onJob => const StatusBadge(label: 'On job', tone: AppTone.info, dot: true),
        OperatorStatus.available => const StatusBadge(label: 'Available', tone: AppTone.success, dot: true),
        OperatorStatus.offDuty => const StatusBadge(label: 'Off duty', tone: AppTone.neutral, dot: true),
      };

  static StatusBadge maintenance(MaintenanceStatus s) => switch (s) {
        MaintenanceStatus.overdue => const StatusBadge(label: 'Overdue', tone: AppTone.danger),
        MaintenanceStatus.dueSoon => const StatusBadge(label: 'Due soon', tone: AppTone.warning),
        MaintenanceStatus.scheduled => const StatusBadge(label: 'Scheduled', tone: AppTone.brand),
        MaintenanceStatus.done => const StatusBadge(label: 'Done', tone: AppTone.success),
      };

  static StatusBadge account(AccountStatus s) => switch (s) {
        AccountStatus.active => const StatusBadge(label: 'Active', tone: AppTone.success),
        AccountStatus.pending => const StatusBadge(label: 'Pending', tone: AppTone.warning),
        AccountStatus.suspended => const StatusBadge(label: 'Suspended', tone: AppTone.danger),
      };

  static StatusBadge kyc(KycStatus s) => switch (s) {
        KycStatus.verified => const StatusBadge(label: 'KYC verified', tone: AppTone.success, icon: AppIcons.verified),
        KycStatus.pending => const StatusBadge(label: 'KYC pending', tone: AppTone.warning),
        KycStatus.rejected => const StatusBadge(label: 'KYC rejected', tone: AppTone.danger),
      };

  static StatusBadge approval(ApprovalStatus s) => switch (s) {
        ApprovalStatus.pending => const StatusBadge(label: 'Pending', tone: AppTone.warning),
        ApprovalStatus.approved => const StatusBadge(label: 'Approved', tone: AppTone.success),
        ApprovalStatus.rejected => const StatusBadge(label: 'Rejected', tone: AppTone.danger),
      };

  static StatusBadge dispute(DisputeStatus s) => switch (s) {
        DisputeStatus.open => const StatusBadge(label: 'Open', tone: AppTone.danger),
        DisputeStatus.underReview => const StatusBadge(label: 'Under review', tone: AppTone.warning),
        DisputeStatus.resolved => const StatusBadge(label: 'Resolved', tone: AppTone.success),
      };

  static StatusBadge settlement(SettlementStatus s) => switch (s) {
        SettlementStatus.pending => const StatusBadge(label: 'Ready to release', tone: AppTone.brand),
        SettlementStatus.onHold => const StatusBadge(label: 'On hold', tone: AppTone.warning),
        SettlementStatus.released => const StatusBadge(label: 'Released', tone: AppTone.success),
      };
}

/// ★ 4.8 (126)
class RatingLabel extends StatelessWidget {
  const RatingLabel({super.key, required this.rating, this.count, this.compact = false});

  final double rating;
  final int? count;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final style = Theme.of(context).textTheme.labelMedium;
    return Semantics(
      label: 'Rated ${rating.toStringAsFixed(1)} out of 5${count == null ? '' : ' from $count reviews'}',
      excludeSemantics: true,
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          const Icon(Icons.star_rounded, size: 16, color: AppColors.orange500),
          const SizedBox(width: 2),
          Text(rating.toStringAsFixed(1), style: style?.copyWith(color: AppColors.slate800)),
          if (count != null && !compact) Text(' ($count)', style: style?.copyWith(color: AppColors.slate500, fontWeight: FontWeight.w500)),
        ],
      ),
    );
  }
}

/// Five interactive or read-only stars.
class StarRating extends StatelessWidget {
  const StarRating({super.key, required this.value, this.onChanged, this.size = 28});

  final double value;
  final ValueChanged<double>? onChanged;
  final double size;

  @override
  Widget build(BuildContext context) {
    return Row(
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 1; i <= 5; i++)
          IconButton(
            tooltip: '$i star${i == 1 ? '' : 's'}',
            visualDensity: VisualDensity.compact,
            onPressed: onChanged == null ? null : () => onChanged!(i.toDouble()),
            icon: Icon(
              value >= i ? Icons.star_rounded : (value >= i - 0.5 ? Icons.star_half_rounded : Icons.star_outline_rounded),
              color: AppColors.orange500,
              size: size,
            ),
          ),
      ],
    );
  }
}

class VerifiedTag extends StatelessWidget {
  const VerifiedTag({super.key});

  @override
  Widget build(BuildContext context) =>
      const StatusBadge(label: 'Verified', tone: AppTone.success, icon: AppIcons.verified);
}
