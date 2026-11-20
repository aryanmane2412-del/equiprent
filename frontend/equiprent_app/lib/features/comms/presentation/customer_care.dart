import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/session/user_role.dart';
import '../comms_routes.dart';
import '../data/comms_api.dart';
import 'comms_widgets.dart';

Future<void> openCustomerCare(
  BuildContext context,
  WidgetRef ref,
  UserRole role,
) async {
  await runComms(context, () async {
    final conversation = await ref
        .read(commsApiProvider)
        .openSupportConversation();
    if (context.mounted) {
      context.push(CommsRoutes.thread(role, conversation.id));
    }
  });
}
