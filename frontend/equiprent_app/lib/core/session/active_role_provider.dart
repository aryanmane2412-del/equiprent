import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'user_role.dart';

/// The role the user is currently acting as.
///
/// Foundation stage: chosen on the role selection screen and kept in memory.
/// Phase 1 replaces this with the roles granted to the signed-in account
/// (GET /api/v1/me) and persists the last active role.
class ActiveRoleController extends Notifier<UserRole?> {
  @override
  UserRole? build() => null;

  void select(UserRole role) => state = role;

  void clear() => state = null;
}

final activeRoleProvider = NotifierProvider<ActiveRoleController, UserRole?>(ActiveRoleController.new);
