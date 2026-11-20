import 'dart:async';

import 'package:firebase_core/firebase_core.dart';
import 'package:firebase_messaging/firebase_messaging.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// A push notification as the app sees it.
class PushMessage {
  const PushMessage({required this.title, required this.body, this.data = const {}});
  final String title;
  final String body;
  final Map<String, String> data;
}

/// Push delivery on this device (Firebase Cloud Messaging).
abstract class PushService {
  /// Whether push can work in this build (Firebase is configured).
  bool get available;

  /// Asks for permission, returns this device's token (null when refused or
  /// unavailable) and starts listening.
  Future<String?> start();

  /// Forgets this device's token (sign-out): no more pushes reach it.
  Future<void> stop();

  Stream<String> get tokenRefresh;

  /// Messages received while the app is in the foreground.
  Stream<PushMessage> get foreground;

  /// Notifications the user tapped (app in the background or closed).
  Stream<PushMessage> get opened;
}

/// Used when Firebase is not configured (and in tests): in-app notifications
/// and badge polling still work.
class NoPushService implements PushService {
  const NoPushService();
  @override
  bool get available => false;
  @override
  Future<String?> start() async => null;
  @override
  Future<void> stop() async {}
  @override
  Stream<String> get tokenRefresh => const Stream.empty();
  @override
  Stream<PushMessage> get foreground => const Stream.empty();
  @override
  Stream<PushMessage> get opened => const Stream.empty();
}

/// Firebase options from build-time defines, so no google-services files
/// are committed:
///
///   flutter run --dart-define=FIREBASE_API_KEY=... --dart-define=FIREBASE_APP_ID=...
///     --dart-define=FIREBASE_MESSAGING_SENDER_ID=... --dart-define=FIREBASE_PROJECT_ID=...
///     [--dart-define=FIREBASE_VAPID_KEY=...  (web push certificate key)]
///
/// The app id differs per platform (Android, iOS, web), so each build passes
/// its own.
abstract final class FirebaseConfig {
  static const apiKey = String.fromEnvironment('FIREBASE_API_KEY');
  static const appId = String.fromEnvironment('FIREBASE_APP_ID');
  static const senderId = String.fromEnvironment('FIREBASE_MESSAGING_SENDER_ID');
  static const projectId = String.fromEnvironment('FIREBASE_PROJECT_ID');
  static const vapidKey = String.fromEnvironment('FIREBASE_VAPID_KEY');

  static bool get configured => apiKey.isNotEmpty && appId.isNotEmpty && senderId.isNotEmpty && projectId.isNotEmpty;

  static FirebaseOptions get options =>
      const FirebaseOptions(apiKey: apiKey, appId: appId, messagingSenderId: senderId, projectId: projectId);
}

class FirebasePushService implements PushService {
  FirebasePushService();

  final _foreground = StreamController<PushMessage>.broadcast();
  final _opened = StreamController<PushMessage>.broadcast();
  final _subs = <StreamSubscription<Object?>>[];
  Future<void>? _init;

  @override
  bool get available => FirebaseConfig.configured;

  Future<void> _ensureInit() => _init ??= () async {
        if (Firebase.apps.isEmpty) await Firebase.initializeApp(options: FirebaseConfig.options);
      }();

  static PushMessage _convert(RemoteMessage m) => PushMessage(
        title: m.notification?.title ?? '',
        body: m.notification?.body ?? '',
        data: {for (final e in m.data.entries) e.key: '${e.value}'},
      );

  @override
  Future<String?> start() async {
    if (!available) return null;
    try {
      await _ensureInit();
      final messaging = FirebaseMessaging.instance;
      final perm = await messaging.requestPermission();
      if (perm.authorizationStatus == AuthorizationStatus.denied) return null;
      // Show notifications while the app is open on iOS too.
      await messaging.setForegroundNotificationPresentationOptions(alert: true, badge: true, sound: true);
      if (_subs.isEmpty) {
        _subs
          ..add(FirebaseMessaging.onMessage.listen((m) => _foreground.add(_convert(m))))
          ..add(FirebaseMessaging.onMessageOpenedApp.listen((m) => _opened.add(_convert(m))));
        final initial = await messaging.getInitialMessage();
        if (initial != null) scheduleMicrotask(() => _opened.add(_convert(initial)));
      }
      // The HTTP v1 send API the server uses targets registration tokens;
      // register()/onRegistered return installation ids for direct send.
      // ignore: deprecated_member_use
      return await messaging.getToken(vapidKey: kIsWeb && FirebaseConfig.vapidKey.isNotEmpty ? FirebaseConfig.vapidKey : null);
    } catch (e) {
      // Missing service worker (web), Play services, or permission: push is
      // optional; in-app notifications keep working.
      debugPrint('push unavailable: $e');
      return null;
    }
  }

  @override
  Future<void> stop() async {
    if (!available || _init == null) return;
    try {
      // ignore: deprecated_member_use
      await FirebaseMessaging.instance.deleteToken();
    } catch (e) {
      debugPrint('push token not deleted: $e');
    }
  }

  @override
  Stream<String> get tokenRefresh =>
      // ignore: deprecated_member_use
      available && _init != null ? FirebaseMessaging.instance.onTokenRefresh : const Stream.empty();

  @override
  Stream<PushMessage> get foreground => _foreground.stream;

  @override
  Stream<PushMessage> get opened => _opened.stream;
}

final pushServiceProvider = Provider<PushService>((ref) => FirebaseConfig.configured ? FirebasePushService() : const NoPushService());
