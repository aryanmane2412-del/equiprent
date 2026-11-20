import 'dart:math';

import 'package:flutter/material.dart';

import 'ui.dart';
import '../../verification/presentation/verification_widgets.dart' show describeError;
import '../data/comms_models.dart';

export '../../verification/presentation/verification_widgets.dart' show AsyncBody, describeError;

/// ₹ amount from paise.
String money(int paise) => Fmt.inr(rupees(paise));

/// Runs [action]; shows [success] or the error as a toast. Returns success.
Future<bool> runComms(BuildContext context, Future<void> Function() action, {String? success}) async {
  try {
    await action();
    if (context.mounted && success != null) AppToast.success(context, success);
    return true;
  } catch (e) {
    if (context.mounted) AppToast.error(context, describeError(e));
    return false;
  }
}

/// A random RFC 4122 v4 id, for idempotent message sends.
String newClientId() {
  final r = Random.secure();
  final b = List<int>.generate(16, (_) => r.nextInt(256));
  b[6] = (b[6] & 0x0f) | 0x40;
  b[8] = (b[8] & 0x3f) | 0x80;
  String h(int i) => b[i].toRadixString(16).padLeft(2, '0');
  final s = List.generate(16, h).join();
  return '${s.substring(0, 8)}-${s.substring(8, 12)}-${s.substring(12, 16)}-${s.substring(16, 20)}-${s.substring(20)}';
}

AppTone bookingTone(String status) => switch (status) {
      'requested' || 'payment_pending' || 'payment_processing' || 'extension_requested' || 'cancellation_requested' || 'return_initiated' => AppTone.warning,
      'accepted' || 'confirmed' || 'operator_assigned' => AppTone.info,
      'dispatched' || 'on_site' || 'active' => AppTone.brand,
      'returned' || 'completed' || 'closed' => AppTone.success,
      'rejected' || 'expired' || 'cancelled' || 'disputed' => AppTone.danger,
      _ => AppTone.neutral,
    };

class BookingStatusBadge extends StatelessWidget {
  const BookingStatusBadge(this.status, {super.key});
  final String status;
  @override
  Widget build(BuildContext context) => StatusBadge(label: bookingStatusLabel(status), tone: bookingTone(status), dot: true);
}

IconData categoryIcon(String category) => switch (category) {
      'bookings' => AppIcons.bookings,
      'payments' => AppIcons.payments,
      'agreements' => AppIcons.shield,
      'chat' => AppIcons.chat,
      'maintenance' => AppIcons.maintenance,
      'reviews' => Icons.star_rounded,
      _ => AppIcons.bell,
    };

AppTone categoryTone(String category) => switch (category) {
      'payments' => AppTone.success,
      'maintenance' => AppTone.warning,
      'reviews' => AppTone.brand,
      'agreements' => AppTone.info,
      _ => AppTone.neutral,
    };

/// Asks for a short reason (declines, moderation).
Future<String?> askReason(BuildContext context, {required String title, String label = 'Reason', String confirm = 'Confirm', int minLength = 3, String? message}) {
  final controller = TextEditingController();
  return showDialog<String>(
    context: context,
    builder: (ctx) => StatefulBuilder(
      builder: (ctx, setState) => AppDialog(
        title: title,
        content: Column(mainAxisSize: MainAxisSize.min, crossAxisAlignment: CrossAxisAlignment.stretch, children: [
          if (message != null) ...[Text(message, key: const Key('reason-message')), const SizedBox(height: AppSpacing.sm)],
          AppTextField(key: const Key('reason-field'), label: label, controller: controller, maxLines: 3, maxLength: 500, onChanged: (_) => setState(() {})),
        ]),
        actions: [
          AppButton(label: 'Cancel', variant: AppButtonVariant.secondary, onPressed: () => Navigator.of(ctx).pop()),
          AppButton(
            key: const Key('reason-confirm'),
            label: confirm,
            onPressed: controller.text.trim().length < minLength ? null : () => Navigator.of(ctx).pop(controller.text.trim()),
          ),
        ],
      ),
    ),
  );
}
