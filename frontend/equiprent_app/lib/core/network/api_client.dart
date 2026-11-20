import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import 'api_exception.dart';
import 'dio_provider.dart';

typedef JsonDecoder<T> = T Function(Object? json);

/// Thin typed wrapper over Dio. Paths are relative to the versioned API
/// base (e.g. `/status` → `https://api.equiprent.in/api/v1/status`).
/// All failures are rethrown as [ApiException].
class ApiClient {
  ApiClient(this._dio);

  final Dio _dio;

  Future<T> get<T>(
    String path, {
    required JsonDecoder<T> decode,
    Map<String, dynamic>? query,
    Map<String, String>? headers,
    CancelToken? cancelToken,
  }) =>
      _send(
        () => _dio.get<Object?>(path, queryParameters: query, options: Options(headers: headers), cancelToken: cancelToken),
        decode,
      );

  Future<T> post<T>(
    String path, {
    required JsonDecoder<T> decode,
    Object? body,
    Map<String, String>? headers,
    CancelToken? cancelToken,
  }) =>
      _send(
        () => _dio.post<Object?>(path, data: body, options: Options(headers: headers), cancelToken: cancelToken),
        decode,
      );

  Future<T> patch<T>(
    String path, {
    required JsonDecoder<T> decode,
    Object? body,
    Map<String, String>? headers,
    CancelToken? cancelToken,
  }) =>
      _send(
        () => _dio.patch<Object?>(path, data: body, options: Options(headers: headers), cancelToken: cancelToken),
        decode,
      );

  Future<T> put<T>(
    String path, {
    required JsonDecoder<T> decode,
    Object? body,
    Map<String, String>? headers,
    CancelToken? cancelToken,
  }) =>
      _send(
        () => _dio.put<Object?>(path, data: body, options: Options(headers: headers), cancelToken: cancelToken),
        decode,
      );

  Future<void> delete(String path, {Map<String, String>? headers, CancelToken? cancelToken}) =>
      _send(
        () => _dio.delete<Object?>(path, options: Options(headers: headers), cancelToken: cancelToken),
        (_) {},
      );

  Future<T> _send<T>(Future<Response<Object?>> Function() call, JsonDecoder<T> decode) async {
    try {
      final response = await call();
      return decode(response.data);
    } on DioException catch (e) {
      throw ApiException.fromDio(e);
    }
  }
}

final apiClientProvider = Provider<ApiClient>((ref) => ApiClient(ref.watch(dioProvider)));
