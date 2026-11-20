import 'package:dio/dio.dart';

/// Attaches the bearer token to API requests and, when the server answers
/// 401 because the access token expired, refreshes it once and retries.
/// Auth endpoints use a separate Dio (see authDioProvider), so a failing
/// refresh can never loop.
class AuthInterceptor extends Interceptor {
  AuthInterceptor({required this.dio, required this.accessToken, required this.refresh});

  final Dio dio;

  /// Current access token, or null when signed out.
  final String? Function() accessToken;

  /// Refreshes the session; returns the new access token or null.
  final Future<String?> Function() refresh;

  static const _retried = 'auth_retried';

  @override
  void onRequest(RequestOptions options, RequestInterceptorHandler handler) {
    final token = accessToken();
    if (token != null && !options.headers.containsKey('Authorization')) {
      options.headers['Authorization'] = 'Bearer $token';
    }
    handler.next(options);
  }

  @override
  Future<void> onError(DioException err, ErrorInterceptorHandler handler) async {
    final options = err.requestOptions;
    final hadToken = options.headers.containsKey('Authorization');
    if (err.response?.statusCode != 401 || options.extra[_retried] == true || !hadToken) {
      handler.next(err);
      return;
    }
    final fresh = await refresh();
    if (fresh == null) {
      handler.next(err);
      return;
    }
    try {
      options.headers['Authorization'] = 'Bearer $fresh';
      options.extra[_retried] = true;
      handler.resolve(await dio.fetch<Object?>(options));
    } on DioException catch (e) {
      handler.next(e);
    }
  }
}
