import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

/// Deployment environment, selected at build time with
/// `--dart-define=APP_ENV=staging`.
enum AppEnvironment {
  development,
  staging,
  production;

  static AppEnvironment parse(String value) => AppEnvironment.values.firstWhere(
        (e) => e.name == value.toLowerCase(),
        orElse: () => AppEnvironment.development,
      );
}

/// Immutable client configuration. Values come from `--dart-define` so no
/// secrets or URLs are hard-coded per environment:
///
/// ```
/// flutter run --dart-define=API_BASE_URL=http://192.168.1.20:8080
/// ```
@immutable
class AppConfig {
  const AppConfig({
    required this.environment,
    required this.apiBaseUrl,
    required this.appVersion,
    this.connectTimeout = const Duration(seconds: 10),
    this.receiveTimeout = const Duration(seconds: 20),
    this.enableNetworkLogs = false,
  });

  factory AppConfig.fromEnvironment() {
    const envName = String.fromEnvironment('APP_ENV', defaultValue: 'development');
    const apiUrl = String.fromEnvironment('API_BASE_URL');
    const version = String.fromEnvironment('APP_VERSION', defaultValue: '0.1.0');

    final environment = AppEnvironment.parse(envName);
    if (environment != AppEnvironment.development && apiUrl.isEmpty) {
      throw StateError('API_BASE_URL must be provided for ${environment.name} builds');
    }

    return AppConfig(
      environment: environment,
      apiBaseUrl: normalizeBaseUrl(apiUrl.isNotEmpty ? apiUrl : defaultLocalApiUrl()),
      appVersion: version,
      enableNetworkLogs: kDebugMode && environment == AppEnvironment.development,
    );
  }

  final AppEnvironment environment;

  /// Server origin without the API prefix, e.g. `https://api.equiprent.in`.
  final String apiBaseUrl;
  final String appVersion;
  final Duration connectTimeout;
  final Duration receiveTimeout;
  final bool enableNetworkLogs;

  /// Current REST API major version (see docs/API_DESIGN.md).
  static const apiVersion = 'v1';

  /// Base URL for all versioned endpoints.
  String get apiUrl => '$apiBaseUrl/api/$apiVersion';

  bool get isProduction => environment == AppEnvironment.production;

  /// Local backend address as seen from each platform during development.
  /// The Android emulator reaches the host machine via 10.0.2.2. A physical
  /// phone needs the PC's LAN IP passed as API_BASE_URL.
  static String defaultLocalApiUrl() {
    if (kIsWeb) return 'http://localhost:8080';
    if (defaultTargetPlatform == TargetPlatform.android) return 'http://10.0.2.2:8080';
    return 'http://localhost:8080';
  }

  static String normalizeBaseUrl(String url) =>
      url.endsWith('/') ? url.substring(0, url.length - 1) : url;
}

final appConfigProvider = Provider<AppConfig>((ref) => AppConfig.fromEnvironment());
