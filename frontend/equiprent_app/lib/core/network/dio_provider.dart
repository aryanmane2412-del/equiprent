import 'dart:math';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../auth/auth_controller.dart';
import '../auth/auth_interceptor.dart';
import '../config/app_config.dart';

/// Shared, configured Dio instance. Feature code should use [ApiClient]
/// (api_client.dart) instead of Dio directly so errors are mapped uniformly.
final dioProvider = Provider<Dio>((ref) {
  final config = ref.watch(appConfigProvider);
  final dio = Dio(
    BaseOptions(
      baseUrl: config.apiUrl,
      connectTimeout: config.connectTimeout,
      receiveTimeout: config.receiveTimeout,
      responseType: ResponseType.json,
      headers: {
        'Accept': 'application/json',
        'X-App-Version': config.appVersion,
        'X-Platform': platformName(),
      },
    ),
  );

  dio.interceptors.add(RequestIdInterceptor());
  // Bearer token + one refresh-and-retry on 401 (read lazily: no provider cycle).
  dio.interceptors.add(AuthInterceptor(
    dio: dio,
    accessToken: () => ref.read(authControllerProvider).tokens?.accessToken,
    refresh: () => ref.read(authControllerProvider.notifier).refreshTokens(),
  ));
  if (config.enableNetworkLogs) {
    dio.interceptors.add(
      LogInterceptor(
        requestHeader: false,
        responseHeader: false,
        logPrint: (line) => debugPrint(line.toString()),
      ),
    );
  }

  ref.onDispose(() => dio.close(force: true));
  return dio;
});

/// Value of the X-Platform header expected by the backend.
String platformName() {
  if (kIsWeb) return 'web';
  return switch (defaultTargetPlatform) {
    TargetPlatform.android => 'android',
    TargetPlatform.iOS => 'ios',
    _ => defaultTargetPlatform.name.toLowerCase(),
  };
}

/// Adds an X-Request-ID to each request so client reports can be matched
/// with server logs. The server echoes it back on the response.
class RequestIdInterceptor extends Interceptor {
  RequestIdInterceptor({Random? random}) : _random = random ?? Random.secure();

  final Random _random;

  static const header = 'X-Request-ID';

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    options.headers.putIfAbsent(header, _newId);
    handler.next(options);
  }

  String _newId() {
    final bytes = List<int>.generate(12, (_) => _random.nextInt(256));
    return 'app-${bytes.map((b) => b.toRadixString(16).padLeft(2, '0')).join()}';
  }
}
