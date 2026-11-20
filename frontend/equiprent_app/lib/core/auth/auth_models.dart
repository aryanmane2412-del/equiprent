import 'package:flutter/foundation.dart';

import '../session/user_role.dart';

/// Maps the server's app roles (always derived server-side from stored role
/// assignments) to the app's role areas. The client never decides what roles
/// a user has; it only reads them to show the right screens. Every request is
/// still authorised by the API.
UserRole? userRoleFromApp(String appRole) => switch (appRole) {
      'CUSTOMER' => UserRole.customer,
      'EQUIPMENT_OWNER' => UserRole.owner,
      'OPERATOR' => UserRole.operator,
      'FLEET_MANAGER' => UserRole.fleetManager,
      'ADMIN' => UserRole.admin,
      _ => null,
    };

@immutable
class AuthUser {
  const AuthUser({
    required this.id,
    required this.phone,
    required this.fullName,
    this.email,
    this.phoneVerified = false,
    this.emailVerified = false,
    this.hasPassword = false,
    this.roles = const [],
    this.orgs = const {},
  });

  factory AuthUser.fromJson(Map<String, dynamic> json, {List<UserRole>? roles, Map<UserRole, String>? orgs}) => AuthUser(
        id: json['id'] as String,
        phone: json['phone'] as String? ?? '',
        fullName: json['full_name'] as String? ?? '',
        email: json['email'] as String?,
        phoneVerified: json['phone_verified'] as bool? ?? false,
        emailVerified: json['email_verified'] as bool? ?? false,
        hasPassword: json['has_password'] as bool? ?? false,
        roles: roles ?? rolesFromJson(json['app_roles']),
        orgs: orgs ?? orgsFromJson(json['roles']),
      );

  final String id;
  final String phone;
  final String fullName;
  final String? email;
  final bool phoneVerified;
  final bool emailVerified;
  final bool hasPassword;

  /// Role areas this account may open, in display order.
  final List<UserRole> roles;

  /// Organisation each role acts for (sent as X-Org-Id). Informational only:
  /// the API checks membership on every request.
  final Map<UserRole, String> orgs;

  String get displayName => fullName.isNotEmpty ? fullName : phone;

  AuthUser copyWith({String? fullName, String? email, bool? emailVerified, bool? hasPassword, List<UserRole>? roles}) => AuthUser(
        id: id,
        phone: phone,
        fullName: fullName ?? this.fullName,
        email: email ?? this.email,
        phoneVerified: phoneVerified,
        emailVerified: emailVerified ?? this.emailVerified,
        hasPassword: hasPassword ?? this.hasPassword,
        roles: roles ?? this.roles,
        orgs: orgs,
      );

  /// First organisation per role from the server's role assignments
  /// (`[{role, organization_id}]`).
  static Map<UserRole, String> orgsFromJson(Object? roles) {
    final out = <UserRole, String>{};
    for (final r in (roles as List?) ?? const []) {
      if (r is! Map) continue;
      final org = r['organization_id'];
      final role = switch (r['role']) {
        'customer' || 'org_admin' => UserRole.customer,
        'owner' => UserRole.owner,
        'operator' => UserRole.operator,
        'fleet_manager' => UserRole.fleetManager,
        _ => null,
      };
      if (role != null && org is String && org.isNotEmpty) out.putIfAbsent(role, () => org);
    }
    return out;
  }

  static List<UserRole> rolesFromJson(Object? appRoles) {
    final out = <UserRole>[];
    for (final r in (appRoles as List?) ?? const []) {
      final role = userRoleFromApp('$r');
      if (role != null && !out.contains(role)) out.add(role);
    }
    return out;
  }
}

/// Tokens of a signed-in session.
@immutable
class AuthTokens {
  const AuthTokens({required this.accessToken, required this.refreshToken, required this.accessExpiresAt});

  factory AuthTokens.fromJson(Map<String, dynamic> json) => AuthTokens(
        accessToken: json['access_token'] as String,
        refreshToken: json['refresh_token'] as String,
        accessExpiresAt: DateTime.tryParse(json['access_expires_at'] as String? ?? '') ?? DateTime.now(),
      );

  final String accessToken;
  final String refreshToken;
  final DateTime accessExpiresAt;
}

/// Result of a successful sign-in or refresh.
@immutable
class AuthSession {
  const AuthSession({required this.tokens, required this.user, this.isNew = false});

  factory AuthSession.fromJson(Map<String, dynamic> json) {
    final roles = AuthUser.rolesFromJson(json['app_roles']);
    return AuthSession(
      tokens: AuthTokens.fromJson(json),
      user: AuthUser.fromJson(json['user'] as Map<String, dynamic>, roles: roles, orgs: AuthUser.orgsFromJson(json['roles'])),
      isNew: json['is_new'] as bool? ?? false,
    );
  }

  final AuthTokens tokens;
  final AuthUser user;
  final bool isNew;
}

/// Sign-in methods the server offers (GET /auth/capabilities).
@immutable
class AuthCapabilities {
  const AuthCapabilities({
    this.passwordLogin = true,
    this.mobileOtp = false,
    this.emailVerification = false,
    this.devSimulation = false,
    this.passwordMinLength = 10,
  });

  factory AuthCapabilities.fromJson(Map<String, dynamic> json) => AuthCapabilities(
        passwordLogin: json['password_login'] as bool? ?? true,
        mobileOtp: json['mobile_otp'] as bool? ?? false,
        emailVerification: json['email_verification'] as bool? ?? false,
        devSimulation: json['dev_simulation'] as bool? ?? false,
        passwordMinLength: json['password_min_length'] as int? ?? 10,
      );

  final bool passwordLogin;
  final bool mobileOtp;
  final bool emailVerification;

  /// The server returns OTPs / email tokens in responses (development only).
  final bool devSimulation;
  final int passwordMinLength;
}

/// One signed-in device.
@immutable
class DeviceSession {
  const DeviceSession({required this.id, required this.createdAt, this.platform, this.userAgent, this.lastUsedAt, this.current = false});

  factory DeviceSession.fromJson(Map<String, dynamic> json) => DeviceSession(
        id: json['id'] as String,
        createdAt: DateTime.tryParse(json['created_at'] as String? ?? '') ?? DateTime.now(),
        platform: json['platform'] as String?,
        userAgent: json['user_agent'] as String?,
        lastUsedAt: DateTime.tryParse(json['last_used_at'] as String? ?? ''),
        current: json['current'] as bool? ?? false,
      );

  final String id;
  final DateTime createdAt;
  final String? platform;
  final String? userAgent;
  final DateTime? lastUsedAt;
  final bool current;
}

/// Public account types. Administrative and fleet-management roles are never
/// selectable here; the server grants them through private workflows.
enum AccountType {
  customer('customer', 'Rent equipment', 'Book machines and operators for your sites'),
  equipmentOwner('equipment_owner', 'List my equipment', 'Earn from your machines; you can add fleet managers and operators later'),
  driver('driver', 'Drive equipment', 'Create an operator profile and complete licence verification');

  const AccountType(this.apiValue, this.label, this.description);

  final String apiValue;
  final String label;
  final String description;
}

/// Outcome of registration: the server answers the same way whether or not
/// the details were new (no account enumeration).
@immutable
class RegistrationResult {
  const RegistrationResult({required this.message, this.devOtp, this.devEmailToken, this.devNotice});

  final String message;
  final String? devOtp;
  final String? devEmailToken;

  /// Development simulation only: why no code was issued (details already
  /// registered). Production servers never send it.
  final String? devNotice;
}

/// Outcome of an OTP request / forgot-password request in dev simulation.
@immutable
class DevSecret {
  const DevSecret({this.code, this.token, this.message});

  final String? code;
  final String? token;
  final String? message;
}
