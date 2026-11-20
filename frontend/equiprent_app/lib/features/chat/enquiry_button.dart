import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/session/user_role.dart';
import '../comms/comms_routes.dart';
import '../comms/data/comms_api.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';

/// Opens (or continues) a conversation with the machine's owner before
/// booking, optionally with a first question.
class MessageOwnerButton extends ConsumerStatefulWidget {
  const MessageOwnerButton({super.key, required this.equipmentId});
  final String equipmentId;
  @override
  ConsumerState<MessageOwnerButton> createState() => _MessageOwnerButtonState();
}

class _MessageOwnerButtonState extends ConsumerState<MessageOwnerButton> {
  bool _busy = false;

  Future<void> _open() async {
    final question = await askReason(context, title: 'Message the owner', label: 'Your question (optional)', confirm: 'Send', minLength: 0);
    if (question == null || !mounted) return;
    setState(() => _busy = true);
    try {
      final c = await ref.read(commsApiProvider).enquire(ref.read(sideOrgProvider(BookingSide.customer)), widget.equipmentId,
          message: question.isEmpty ? null : question);
      ref.invalidate(conversationsProvider);
      if (mounted) context.push(CommsRoutes.thread(UserRole.customer, c.id));
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) => AppButton(
        key: const Key('message-owner'),
        label: 'Message owner',
        icon: AppIcons.chat,
        variant: AppButtonVariant.secondary,
        size: AppButtonSize.small,
        isLoading: _busy,
        onPressed: _busy ? null : _open,
      );
}
