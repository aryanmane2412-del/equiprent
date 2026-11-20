import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../network/api_client.dart';

/// Response of `GET /api/v1/status`, used to show backend connectivity.
@immutable
class ServerStatus {
  const ServerStatus({
    required this.service,
    required this.apiVersion,
    required this.version,
    required this.environment,
    required this.serverTime,
  });

  factory ServerStatus.fromJson(Map<String, dynamic> json) => ServerStatus(
        service: json['service'] as String,
        apiVersion: json['api_version'] as String,
        version: json['version'] as String,
        environment: json['environment'] as String,
        serverTime: DateTime.parse(json['server_time'] as String),
      );

  final String service;
  final String apiVersion;
  final String version;
  final String environment;
  final DateTime serverTime;
}

final serverStatusProvider = FutureProvider.autoDispose<ServerStatus>((ref) {
  return ref.watch(apiClientProvider).get(
        '/status',
        decode: (json) => ServerStatus.fromJson(json! as Map<String, dynamic>),
      );
});
