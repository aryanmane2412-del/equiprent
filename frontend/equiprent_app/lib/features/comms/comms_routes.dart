import '../../core/session/user_role.dart';

/// Routes of the communication screens for each role area.
abstract final class CommsRoutes {
  static String notifications(UserRole r) => '${r.basePath}/notifications';

  /// Conversations: the customer's Chat tab, "messages" elsewhere.
  static String messages(UserRole r) =>
      r == UserRole.customer ? '/customer/chat' : '${r.basePath}/messages';
  static String thread(UserRole r, String id) => '${messages(r)}/$id';

  static String? booking(UserRole r, String id) => switch (r) {
    UserRole.customer => '/customer/bookings/$id',
    UserRole.owner => '/owner/bookings/$id',
    UserRole.operator => '/operator/jobs/$id',
    _ => null,
  };

  static String? agreement(UserRole r, String id) => switch (r) {
    UserRole.customer || UserRole.owner => '${booking(r, id)}/agreement',
    _ => null,
  };

  /// Roles with a messages screen.
  static bool hasMessages(UserRole r) =>
      r == UserRole.customer ||
      r == UserRole.owner ||
      r == UserRole.operator ||
      r == UserRole.fleetManager ||
      r == UserRole.admin;
}

/// Where a notification (or a tapped push) leads in [role]'s area.
String? routeForNotification(
  UserRole role,
  String type,
  Map<String, Object?> data,
) {
  String? s(String k) => data[k]?.toString();
  final conversation = s('conversation_id');
  final booking = s('booking_id');
  if (type == 'chat.message' &&
      conversation != null &&
      CommsRoutes.hasMessages(role)) {
    return CommsRoutes.thread(role, conversation);
  }
  if (type.startsWith('maintenance.') || type.startsWith('document.')) {
    return role == UserRole.owner ? '/owner/maintenance' : null;
  }
  if (type == 'review.received' && role == UserRole.owner) {
    return '/owner/reviews';
  }
  if (type.startsWith('agreement.') && booking != null) {
    return CommsRoutes.agreement(role, booking) ??
        CommsRoutes.booking(role, booking);
  }
  if (role == UserRole.admin) {
    if (type == 'owner.registered') return '/admin/owners';
    if (type == 'verification.submitted') return '/admin/verifications';
    if (type.startsWith('payment.')) return '/admin/payments';
  }
  if (booking != null) return CommsRoutes.booking(role, booking);
  if (type.startsWith('verification.')) {
    return switch (role) {
      UserRole.owner => '/owner/verification',
      UserRole.operator => '/operator/verification',
      _ => null,
    };
  }
  return null;
}
