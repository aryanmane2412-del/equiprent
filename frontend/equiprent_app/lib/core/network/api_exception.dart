import 'dart:convert';

import 'package:dio/dio.dart';
import 'package:flutter/foundation.dart';

/// Coarse error categories the UI can react to.
enum ApiErrorKind {
  network,
  timeout,
  cancelled,
  badRequest,
  unauthorized,
  forbidden,
  notFound,
  conflict,
  rateLimited,
  server,
  unknown,
}

@immutable
class FieldIssue {
  const FieldIssue({required this.field, required this.code, this.message});

  final String field;
  final String code;
  final String? message;
}

/// Every failed API call surfaces as an [ApiException]. It is parsed from the
/// backend's RFC 9457 problem+json body when available.
class ApiException implements Exception {
  const ApiException({
    required this.kind,
    required this.code,
    required this.message,
    this.statusCode,
    this.requestId,
    this.fieldIssues = const [],
  });

  factory ApiException.fromDio(DioException e) {
    switch (e.type) {
      case DioExceptionType.connectionTimeout:
      case DioExceptionType.sendTimeout:
      case DioExceptionType.receiveTimeout:
      case DioExceptionType.transformTimeout:
        return const ApiException(
          kind: ApiErrorKind.timeout,
          code: 'TIMEOUT',
          message: 'The server is taking too long to respond. Please try again.',
        );
      case DioExceptionType.cancel:
        return const ApiException(kind: ApiErrorKind.cancelled, code: 'CANCELLED', message: 'Request cancelled.');
      case DioExceptionType.connectionError:
        return const ApiException(
          kind: ApiErrorKind.network,
          code: 'NETWORK',
          message: "Can't reach EQUIPRENT. Check your internet connection.",
        );
      case DioExceptionType.badCertificate:
        return const ApiException(
          kind: ApiErrorKind.network,
          code: 'BAD_CERTIFICATE',
          message: 'Secure connection failed.',
        );
      case DioExceptionType.badResponse:
      case DioExceptionType.unknown:
        break;
    }

    final response = e.response;
    if (response == null) {
      return const ApiException(
        kind: ApiErrorKind.network,
        code: 'NETWORK',
        message: "Can't reach EQUIPRENT. Check your internet connection.",
      );
    }

    final status = response.statusCode ?? 0;
    final problem = _asMap(response.data);
    final issues = <FieldIssue>[
      for (final item in (problem?['errors'] as List?) ?? const [])
        if (item is Map)
          FieldIssue(
            field: '${item['field'] ?? ''}',
            code: '${item['code'] ?? ''}',
            message: item['message'] as String?,
          ),
    ];

    return ApiException(
      kind: _kindForStatus(status),
      statusCode: status,
      code: (problem?['code'] as String?) ?? 'HTTP_$status',
      message: (problem?['detail'] as String?) ??
          (problem?['title'] as String?) ??
          'Something went wrong. Please try again.',
      requestId: (problem?['request_id'] as String?) ?? response.headers.value('x-request-id'),
      fieldIssues: issues,
    );
  }

  final ApiErrorKind kind;
  final String code;

  /// User-safe message, suitable for display.
  final String message;
  final int? statusCode;

  /// Quote this to support to find the server logs.
  final String? requestId;
  final List<FieldIssue> fieldIssues;

  bool get isRetryable =>
      kind == ApiErrorKind.network ||
      kind == ApiErrorKind.timeout ||
      kind == ApiErrorKind.server ||
      kind == ApiErrorKind.rateLimited;

  static ApiErrorKind _kindForStatus(int status) => switch (status) {
        400 || 422 => ApiErrorKind.badRequest,
        401 => ApiErrorKind.unauthorized,
        403 => ApiErrorKind.forbidden,
        404 => ApiErrorKind.notFound,
        409 || 412 => ApiErrorKind.conflict,
        429 => ApiErrorKind.rateLimited,
        >= 500 => ApiErrorKind.server,
        _ => ApiErrorKind.unknown,
      };

  static Map<String, dynamic>? _asMap(Object? data) {
    if (data is Map<String, dynamic>) return data;
    if (data is String && data.isNotEmpty) {
      try {
        final decoded = jsonDecode(data);
        if (decoded is Map<String, dynamic>) return decoded;
      } on FormatException {
        return null;
      }
    }
    return null;
  }

  @override
  String toString() => 'ApiException($code, status: $statusCode, requestId: $requestId): $message';
}
