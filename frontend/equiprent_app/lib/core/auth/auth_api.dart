import 'package:dio/dio.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../config/app_config.dart';
import '../network/api_client.dart';
import '../network/dio_provider.dart';
import 'auth_models.dart';

/// The /auth and /me endpoints. Calls that need a session take the access
/// token explicitly, so this client never goes through the refresh
/// interceptor (no refresh loops).
abstract class AuthApi {
  Future<AuthCapabilities> capabilities();
  Future<AuthSession> login({required String identifier, required String password});
  Future<DevSecret> requestOtp(String phone);
  Future<AuthSession> verifyOtp({required String phone, required String code});
  Future<RegistrationResult> register({
    required AccountType accountType,
    required String fullName,
    required String email,
    required String phone,
    required String password,
    String? organizationName,
    required bool acceptInspectionPolicy,
    required String inspectionPolicyVersion,
  });
  Future<AuthSession> refresh(String refreshToken);
  Future<void> logout(String accessToken);
  Future<DevSecret> forgotPassword(String identifier);
  Future<void> resetPassword({String? token, String? phone, String? code, required String newPassword});
  Future<void> verifyEmail(String token);

  Future<AuthUser> me(String accessToken);
  Future<AuthUser> updateProfile(String accessToken, {String? fullName, String? email});
  Future<DevSecret> sendEmailVerification(String accessToken);
  Future<void> changePassword(String accessToken, {String? currentPassword, required String newPassword});
  Future<List<DeviceSession>> sessions(String accessToken);
  Future<void> revokeSession(String accessToken, String sessionId);
  Future<void> revokeOtherSessions(String accessToken);
}

class HttpAuthApi implements AuthApi {
  HttpAuthApi(this._client, {this.platform = ''});

  final ApiClient _client;
  final String platform;

  static Map<String, dynamic> _map(Object? json) => (json as Map).cast<String, dynamic>();
  static Map<String, String> _bearer(String token) => {'Authorization': 'Bearer $token'};

  @override
  Future<AuthCapabilities> capabilities() =>
      _client.get('/auth/capabilities', decode: (j) => AuthCapabilities.fromJson(_map(j)));

  @override
  Future<AuthSession> login({required String identifier, required String password}) => _client.post(
        '/auth/login',
        body: {'identifier': identifier, 'password': password, if (_platformOk) 'platform': platform},
        decode: (j) => AuthSession.fromJson(_map(j)),
      );

  bool get _platformOk => const ['android', 'ios', 'web'].contains(platform);

  @override
  Future<DevSecret> requestOtp(String phone) => _client.post(
        '/auth/otp/request',
        body: {'phone': phone},
        decode: (j) => DevSecret(code: _map(j)['dev_code'] as String?),
      );

  @override
  Future<AuthSession> verifyOtp({required String phone, required String code}) => _client.post(
        '/auth/otp/verify',
        body: {'phone': phone, 'code': code, if (_platformOk) 'platform': platform},
        decode: (j) => AuthSession.fromJson(_map(j)),
      );

  @override
  Future<RegistrationResult> register({
    required AccountType accountType,
    required String fullName,
    required String email,
    required String phone,
    required String password,
    String? organizationName,
    required bool acceptInspectionPolicy,
    required String inspectionPolicyVersion,
  }) =>
      _client.post(
        '/auth/register',
        body: {
          'account_type': accountType.apiValue,
          'full_name': fullName,
          'email': email,
          'phone': phone,
          'password': password,
          if (organizationName != null && organizationName.isNotEmpty) 'organization_name': organizationName,
          'accept_terms': true,
          'accept_inspection_policy': acceptInspectionPolicy,
          'inspection_policy_version': inspectionPolicyVersion,
        },
        decode: (j) {
          final m = _map(j);
          return RegistrationResult(
            message: m['message'] as String? ?? '',
            devOtp: m['dev_otp'] as String?,
            devEmailToken: m['dev_email_token'] as String?,
            devNotice: m['dev_notice'] as String?,
          );
        },
      );

  @override
  Future<AuthSession> refresh(String refreshToken) => _client.post(
        '/auth/refresh',
        body: {'refresh_token': refreshToken},
        decode: (j) => AuthSession.fromJson(_map(j)),
      );

  @override
  Future<void> logout(String accessToken) => _client.post('/auth/logout', headers: _bearer(accessToken), decode: (_) {});

  @override
  Future<DevSecret> forgotPassword(String identifier) => _client.post(
        '/auth/password/forgot',
        body: {'identifier': identifier},
        decode: (j) {
          final m = _map(j);
          return DevSecret(code: m['dev_code'] as String?, token: m['dev_token'] as String?, message: m['message'] as String?);
        },
      );

  @override
  Future<void> resetPassword({String? token, String? phone, String? code, required String newPassword}) => _client.post(
        '/auth/password/reset',
        body: {
          'token': ?token,
          'phone': ?phone,
          'code': ?code,
          'new_password': newPassword,
        },
        decode: (_) {},
      );

  @override
  Future<void> verifyEmail(String token) => _client.post('/auth/email/verify', body: {'token': token}, decode: (_) {});

  @override
  Future<AuthUser> me(String accessToken) =>
      _client.get('/me', headers: _bearer(accessToken), decode: (j) => AuthUser.fromJson(_map(j)));

  @override
  Future<AuthUser> updateProfile(String accessToken, {String? fullName, String? email}) => _client.patch(
        '/me',
        headers: _bearer(accessToken),
        body: {'full_name': ?fullName, 'email': ?email},
        decode: (j) => AuthUser.fromJson(_map(j)),
      );

  @override
  Future<DevSecret> sendEmailVerification(String accessToken) => _client.post(
        '/auth/email/verification',
        headers: _bearer(accessToken),
        decode: (j) => DevSecret(token: _map(j)['dev_token'] as String?),
      );

  @override
  Future<void> changePassword(String accessToken, {String? currentPassword, required String newPassword}) => _client.post(
        '/auth/password/change',
        headers: _bearer(accessToken),
        body: {'current_password': currentPassword ?? '', 'new_password': newPassword},
        decode: (_) {},
      );

  @override
  Future<List<DeviceSession>> sessions(String accessToken) => _client.get(
        '/auth/sessions',
        headers: _bearer(accessToken),
        decode: (j) => [for (final s in (_map(j)['data'] as List? ?? const [])) DeviceSession.fromJson(_map(s))],
      );

  @override
  Future<void> revokeSession(String accessToken, String sessionId) =>
      _client.delete('/auth/sessions/$sessionId', headers: _bearer(accessToken));

  @override
  Future<void> revokeOtherSessions(String accessToken) =>
      _client.post('/auth/sessions/revoke-others', headers: _bearer(accessToken), decode: (_) {});
}

/// A Dio without the auth interceptor, used only by [AuthApi].
final authDioProvider = Provider<Dio>((ref) {
  final config = ref.watch(appConfigProvider);
  final dio = Dio(BaseOptions(
    baseUrl: config.apiUrl,
    connectTimeout: config.connectTimeout,
    receiveTimeout: config.receiveTimeout,
    responseType: ResponseType.json,
    headers: {'Accept': 'application/json', 'X-App-Version': config.appVersion, 'X-Platform': platformName()},
  ));
  dio.interceptors.add(RequestIdInterceptor());
  ref.onDispose(() => dio.close(force: true));
  return dio;
});

final authApiProvider = Provider<AuthApi>((ref) => HttpAuthApi(ApiClient(ref.watch(authDioProvider)), platform: platformName()));
