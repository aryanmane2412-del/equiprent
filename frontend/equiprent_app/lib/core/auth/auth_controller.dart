import 'dart:async';

import 'package:flutter/foundation.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../network/api_exception.dart';
import '../session/active_role_provider.dart';
import 'auth_api.dart';
import 'auth_models.dart';
import 'token_store.dart';

enum AuthStatus { restoring, signedOut, signedIn }

@immutable
class AuthState {
  const AuthState._(this.status, {this.user, this.tokens});

  const AuthState.restoring() : this._(AuthStatus.restoring);
  const AuthState.signedOut() : this._(AuthStatus.signedOut);
  const AuthState.signedIn(AuthUser user, AuthTokens tokens)
    : this._(AuthStatus.signedIn, user: user, tokens: tokens);

  final AuthStatus status;
  final AuthUser? user;
  final AuthTokens? tokens;

  bool get isSignedIn => status == AuthStatus.signedIn;
}

/// Owns the session: sign-in (password, OTP), token refresh with rotation,
/// restore on launch and sign-out. The refresh token is persisted in secure
/// storage; the access token only in memory.
class AuthController extends Notifier<AuthState> {
  AuthController({this.initial});

  /// Starting state (tests). When null the controller restores a stored
  /// session on first use.
  final AuthState? initial;

  Future<String?>? _refreshing;

  AuthApi get _api => ref.read(authApiProvider);
  TokenStore get _store => ref.read(tokenStoreProvider);

  @override
  AuthState build() {
    if (initial != null) return initial!;
    Future.microtask(restore);
    return const AuthState.restoring();
  }

  /// Signs in again from the stored refresh token, if any.
  Future<void> restore() async {
    try {
      final refresh = await _store.readRefreshToken();
      if (refresh == null) {
        state = const AuthState.signedOut();
        return;
      }
      await _apply(await _api.refresh(refresh));
    } on ApiException catch (e) {
      if (e.kind == ApiErrorKind.unauthorized ||
          e.kind == ApiErrorKind.forbidden) {
        await _store.clear();
      }
      state = const AuthState.signedOut();
    } on Object catch (e) {
      // Unreadable storage, a malformed response or any other surprise: the app must still
      // open on the sign-in screen rather than hang on "Signing you in".
      debugPrint('session restore failed: $e');
      state = const AuthState.signedOut();
    }
  }

  Future<void> _apply(AuthSession session) async {
    try {
      await _store.writeRefreshToken(session.tokens.refreshToken);
    } on Object catch (e) {
      // The session works in memory; it just will not survive a restart.
      debugPrint('could not persist the session: $e');
    }
    final previous = state.user?.id;
    state = AuthState.signedIn(session.user, session.tokens);
    if (previous != session.user.id) {
      ref.read(activeRoleProvider.notifier).clear();
    }
  }

  Future<void> loginWithPassword(String identifier, String password) async =>
      _apply(
        await _api.login(identifier: identifier.trim(), password: password),
      );

  Future<DevSecret> requestOtp(String phone) => _api.requestOtp(phone);

  Future<AuthSession> verifyOtp(String phone, String code) async {
    final session = await _api.verifyOtp(phone: phone, code: code);
    await _apply(session);
    return session;
  }

  Future<RegistrationResult> register({
    required AccountType accountType,
    required String fullName,
    required String email,
    required String phone,
    required String password,
    String? organizationName,
    required bool acceptInspectionPolicy,
    required String inspectionPolicyVersion,
  }) => _api.register(
    accountType: accountType,
    fullName: fullName.trim(),
    email: email.trim(),
    phone: phone,
    password: password,
    organizationName: organizationName?.trim(),
    acceptInspectionPolicy: acceptInspectionPolicy,
    inspectionPolicyVersion: inspectionPolicyVersion,
  );

  /// Rotates the refresh token. Concurrent callers share one request, so a
  /// burst of 401s cannot trigger refresh-token reuse detection. Returns the
  /// new access token, or null when the session is over (signed out).
  Future<String?> refreshTokens() {
    return _refreshing ??= _doRefresh().whenComplete(() => _refreshing = null);
  }

  Future<String?> _doRefresh() async {
    final tokens = state.tokens;
    if (tokens == null) return null;
    try {
      final session = await _api.refresh(tokens.refreshToken);
      await _apply(session);
      return session.tokens.accessToken;
    } on ApiException catch (e) {
      if (e.kind == ApiErrorKind.unauthorized ||
          e.kind == ApiErrorKind.forbidden) {
        await _signOutLocally();
      }
      return null;
    }
  }

  /// Runs an authenticated call, refreshing once if the access token expired.
  Future<T> withToken<T>(Future<T> Function(String accessToken) call) async {
    final token = state.tokens?.accessToken;
    if (token == null) {
      throw const ApiException(
        kind: ApiErrorKind.unauthorized,
        code: 'UNAUTHENTICATED',
        message: 'Please sign in again.',
      );
    }
    try {
      return await call(token);
    } on ApiException catch (e) {
      if (e.kind != ApiErrorKind.unauthorized) rethrow;
      final fresh = await refreshTokens();
      if (fresh == null) rethrow;
      return call(fresh);
    }
  }

  /// Reloads the profile (verification flags, roles).
  Future<void> reloadUser() async {
    final user = await withToken(_api.me);
    final tokens = state.tokens;
    if (tokens != null) state = AuthState.signedIn(user, tokens);
  }

  Future<void> updateProfile({String? fullName, String? email}) async {
    final updated = await withToken(
      (token) => _api.updateProfile(token, fullName: fullName, email: email),
    );
    final tokens = state.tokens;
    if (tokens != null) state = AuthState.signedIn(updated, tokens);
  }

  Future<void> logout() async {
    final token = state.tokens?.accessToken;
    // Clear the local session first so sign-out always completes immediately,
    // including when offline or when the server's logout endpoint is slow.
    await _signOutLocally();
    if (token == null) return;

    // The local session is already closed. Best-effort server revocation must
    // never keep the user on a signed-in screen.
    try {
      await _api.logout(token).timeout(const Duration(seconds: 3));
    } on Object {
      // The refresh token was removed locally; an offline server cannot keep
      // this device signed in. Its normal expiry limits any stale access token.
    }
  }

  Future<void> _signOutLocally() async {
    await _store.clear();
    ref.read(activeRoleProvider.notifier).clear();
    state = const AuthState.signedOut();
  }
}

final authControllerProvider = NotifierProvider<AuthController, AuthState>(
  AuthController.new,
);
