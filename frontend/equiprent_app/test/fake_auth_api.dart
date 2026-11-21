import 'package:equiprent_app/core/auth/auth_api.dart';
import 'package:equiprent_app/core/auth/auth_models.dart';
import 'package:equiprent_app/core/network/api_exception.dart';

ApiException apiError(int status, String code, [String message = 'error']) =>
    ApiException(
      kind: switch (status) {
        401 => ApiErrorKind.unauthorized,
        403 => ApiErrorKind.forbidden,
        429 => ApiErrorKind.rateLimited,
        503 => ApiErrorKind.server,
        _ => ApiErrorKind.badRequest,
      },
      code: code,
      message: message,
      statusCode: status,
    );

/// A scriptable in-memory backend for widget tests (no network).
class FakeAuthApi implements AuthApi {
  /// Phones the fake treats as already registered (demo-mode duplicate notice).
  final registeredPhones = <String>{};

  FakeAuthApi({
    this.caps = const AuthCapabilities(
      mobileOtp: true,
      emailVerification: true,
      devSimulation: true,
    ),
  });

  AuthCapabilities caps;

  /// identifier → (password, roles)
  final accounts = <String, (String, List<String>)>{};
  final calls = <String>[];
  int _n = 0;
  String? lastRefreshToken;
  bool failRefresh = false;
  bool failLogout = false;
  List<String> otpRoles = const ['CUSTOMER'];

  AuthSession _session(String id, List<String> roles) {
    _n++;
    lastRefreshToken = 'refresh-token-$_n-padding-padding';
    return AuthSession.fromJson({
      'access_token': 'access-$_n',
      'refresh_token': lastRefreshToken,
      'access_expires_at': DateTime.now()
          .add(const Duration(minutes: 15))
          .toIso8601String(),
      'app_roles': roles,
      'user': {
        'id': 'user-$id',
        'phone': id.startsWith('+') ? id : '+919800000000',
        'full_name': 'Test $id',
        'phone_verified': true,
        'has_password': true,
      },
    });
  }

  @override
  Future<AuthCapabilities> capabilities() async => caps;

  @override
  Future<AuthSession> login({
    required String identifier,
    required String password,
  }) async {
    calls.add('login:$identifier');
    final acct = accounts[identifier];
    if (acct == null || acct.$1 != password) {
      throw apiError(401, 'INVALID_CREDENTIALS');
    }
    return _session(identifier, acct.$2);
  }

  @override
  Future<DevSecret> requestOtp(String phone) async {
    calls.add('otp:$phone');
    if (!caps.mobileOtp) throw apiError(503, 'OTP_UNAVAILABLE');
    return const DevSecret(code: '123456');
  }

  @override
  Future<AuthSession> verifyOtp({
    required String phone,
    required String code,
  }) async {
    calls.add('verify:$phone:$code');
    if (code != '123456') throw apiError(401, 'OTP_INVALID');
    return _session(phone, otpRoles);
  }

  @override
  Future<RegistrationResult> register({
    required AccountType accountType,
    required String fullName,
    required String email,
    required String phone,
    required String password,
    required bool acceptInspectionPolicy,
    required String inspectionPolicyVersion,
    String? organizationName,
  }) async {
    calls.add(
      'register:${accountType.apiValue}:$phone:${organizationName ?? ''}',
    );
    if (registeredPhones.contains(phone)) {
      return const RegistrationResult(
        message: 'If these details are new, your account has been created.',
        devNotice:
            'This mobile number is already registered, so no new account was created and no code was sent.',
      );
    }
    return const RegistrationResult(
      message: 'Check your phone and email.',
      devOtp: '123456',
      devEmailToken: 'email-token-abcdefghijklmnop',
    );
  }

  @override
  Future<AuthSession> refresh(String refreshToken) async {
    calls.add('refresh');
    if (failRefresh || refreshToken != lastRefreshToken) {
      throw apiError(401, 'UNAUTHENTICATED');
    }
    return _session('+919800000000', otpRoles);
  }

  @override
  Future<void> logout(String accessToken) async {
    calls.add('logout');
    if (failLogout) throw apiError(503, 'LOGOUT_UNAVAILABLE');
  }

  @override
  Future<DevSecret> forgotPassword(String identifier) async {
    calls.add('forgot:$identifier');
    return DevSecret(
      message: 'If an account matches, we have sent instructions.',
      code: identifier.contains('@') ? null : '654321',
      token: identifier.contains('@') ? 'reset-token-abcdefghijklmnop' : null,
    );
  }

  @override
  Future<void> resetPassword({
    String? token,
    String? phone,
    String? code,
    required String newPassword,
  }) async {
    calls.add('reset:${token ?? '$phone:$code'}');
  }

  @override
  Future<void> verifyEmail(String token, String email) async {
    calls.add('verify-email:$token');
    if (!token.startsWith('email-token')) throw apiError(400, 'TOKEN_INVALID');
  }

  @override
  Future<AuthUser> me(String accessToken) async => AuthUser.fromJson({
    'id': 'user-me',
    'phone': '+919800000000',
    'full_name': 'Test Me',
    'email': 'me@example.com',
    'email_verified': true,
    'phone_verified': true,
    'has_password': true,
    'app_roles': otpRoles,
  });

  @override
  Future<AuthUser> updateProfile(
    String accessToken, {
    String? fullName,
    String? email,
    String? currentPassword,
  }) => me(accessToken);

  @override
  Future<DevSecret> sendEmailVerification(String accessToken) async =>
      const DevSecret(token: 'email-token-xyz');

  @override
  Future<void> changePassword(
    String accessToken, {
    String? currentPassword,
    required String newPassword,
  }) async => calls.add('change-password');

  @override
  Future<List<DeviceSession>> sessions(String accessToken) async => [
    DeviceSession(
      id: 's1',
      createdAt: DateTime(2026, 11, 1),
      platform: 'android',
      current: true,
    ),
    DeviceSession(id: 's2', createdAt: DateTime(2026, 10, 1), platform: 'web'),
  ];

  @override
  Future<void> revokeSession(String accessToken, String sessionId) async =>
      calls.add('revoke:$sessionId');

  @override
  Future<void> revokeOtherSessions(String accessToken) async =>
      calls.add('revoke-others');
}
