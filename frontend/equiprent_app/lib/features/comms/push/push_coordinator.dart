import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../app/router.dart';
import '../../../core/auth/auth_controller.dart';
import '../../../core/network/dio_provider.dart';
import '../../../core/session/active_role_provider.dart';
import '../../../core/session/user_role.dart';
import '../../../core/widgets/widgets.dart';
import '../comms_routes.dart';
import '../data/comms_api.dart';
import 'push_service.dart';

/// Connects push delivery to the signed-in session:
/// - registers this device's FCM token with the API after sign-in (and when
///   it rotates), and drops it on sign-out;
/// - refreshes the notification and chat badges when a push arrives, and
///   shows a short banner while the app is open;
/// - opens the right screen when a notification is tapped.
class PushCoordinator extends ConsumerStatefulWidget {
  const PushCoordinator({super.key, required this.child});
  final Widget child;

  @override
  ConsumerState<PushCoordinator> createState() => _PushCoordinatorState();
}

class _PushCoordinatorState extends ConsumerState<PushCoordinator> {
  final _subs = <StreamSubscription<Object?>>[];
  OverlayEntry? _banner;
  Timer? _bannerTimer;
  bool _started = false;

  @override
  void initState() {
    super.initState();
    final push = ref.read(pushServiceProvider);
    _subs
      ..add(push.foreground.listen(_onForeground))
      ..add(push.opened.listen(_open));
    WidgetsBinding.instance.addPostFrameCallback((_) => _sync(ref.read(authControllerProvider)));
  }

  @override
  void dispose() {
    for (final s in _subs) {
      s.cancel();
    }
    _bannerTimer?.cancel();
    _banner?.remove();
    super.dispose();
  }

  Future<void> _sync(AuthState auth) async {
    final push = ref.read(pushServiceProvider);
    if (!push.available) return;
    if (auth.status == AuthStatus.signedIn && !_started) {
      _started = true;
      final token = await push.start();
      if (token != null) await _register(token);
      _subs.add(push.tokenRefresh.listen(_register));
    } else if (auth.status == AuthStatus.signedOut && _started) {
      _started = false;
      await push.stop();
    }
  }

  Future<void> _register(String token) async {
    try {
      await ref.read(commsApiProvider).registerDevice(platformName(), token);
    } catch (e) {
      debugPrint('device registration failed: $e');
    }
  }

  void _refreshBadges() {
    ref.invalidate(unreadNotificationsProvider);
    ref.invalidate(unreadMessagesProvider);
    ref.invalidate(notificationListProvider);
    ref.invalidate(conversationsProvider);
  }

  void _onForeground(PushMessage m) {
    _refreshBadges();
    final overlay = ref.read(routerProvider).routerDelegate.navigatorKey.currentState?.overlay;
    if (overlay == null || m.title.isEmpty) return;
    _bannerTimer?.cancel();
    _banner?.remove();
    _banner = OverlayEntry(
      builder: (context) => Positioned(
        top: MediaQuery.paddingOf(context).top + AppSpacing.sm,
        left: AppSpacing.sm,
        right: AppSpacing.sm,
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 520),
            child: Material(
              elevation: 6,
              color: AppColors.navy900,
              borderRadius: AppRadius.card,
              child: InkWell(
                borderRadius: AppRadius.card,
                onTap: () {
                  _hideBanner();
                  _open(m);
                },
                child: Padding(
                  padding: const EdgeInsets.all(AppSpacing.ms),
                  child: Row(children: [
                    const Icon(AppIcons.bell, color: AppColors.orange500, size: 20),
                    const SizedBox(width: AppSpacing.ms),
                    Expanded(
                      child: Column(crossAxisAlignment: CrossAxisAlignment.start, mainAxisSize: MainAxisSize.min, children: [
                        Text(m.title, maxLines: 1, overflow: TextOverflow.ellipsis, style: const TextStyle(color: AppColors.white, fontWeight: FontWeight.w600)),
                        if (m.body.isNotEmpty)
                          Text(m.body, maxLines: 2, overflow: TextOverflow.ellipsis, style: const TextStyle(color: AppColors.navy200)),
                      ]),
                    ),
                  ]),
                ),
              ),
            ),
          ),
        ),
      ),
    );
    overlay.insert(_banner!);
    _bannerTimer = Timer(const Duration(seconds: 5), _hideBanner);
  }

  void _hideBanner() {
    _bannerTimer?.cancel();
    _banner?.remove();
    _banner = null;
  }

  void _open(PushMessage m) {
    _refreshBadges();
    final role = ref.read(activeRoleProvider) ?? _defaultRole();
    if (role == null) return;
    final route = routeForNotification(role, m.data['type'] ?? '', m.data);
    if (route != null) ref.read(routerProvider).go(route);
  }

  UserRole? _defaultRole() {
    final roles = ref.read(authControllerProvider).user?.roles ?? const [];
    if (roles.isEmpty) return null;
    final role = roles.first;
    ref.read(activeRoleProvider.notifier).select(role);
    return role;
  }

  @override
  Widget build(BuildContext context) {
    ref.listen(authControllerProvider, (_, next) => _sync(next));
    return widget.child;
  }
}
