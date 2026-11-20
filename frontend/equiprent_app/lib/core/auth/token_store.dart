import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

/// Persists the refresh token between app launches. Access tokens are kept
/// in memory only (they live 15 minutes).
abstract class TokenStore {
  Future<String?> readRefreshToken();
  Future<void> writeRefreshToken(String token);
  Future<void> clear();
}

/// Keychain / Keystore backed storage (encrypted at rest). On the web it
/// falls back to the browser storage flutter_secure_storage provides.
class SecureTokenStore implements TokenStore {
  SecureTokenStore([FlutterSecureStorage? storage]) : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;
  static const _key = 'equiprent.refresh_token';

  @override
  Future<String?> readRefreshToken() async {
    try {
      return await _storage.read(key: _key);
    } on Object {
      return null; // unreadable keystore (e.g. after OS restore): sign in again
    }
  }

  @override
  Future<void> writeRefreshToken(String token) => _storage.write(key: _key, value: token);

  @override
  Future<void> clear() async {
    try {
      await _storage.delete(key: _key);
    } on Object {
      // nothing stored
    }
  }
}

/// In-memory store for tests.
class MemoryTokenStore implements TokenStore {
  MemoryTokenStore([this.token]);

  String? token;

  @override
  Future<String?> readRefreshToken() async => token;

  @override
  Future<void> writeRefreshToken(String value) async => token = value;

  @override
  Future<void> clear() async => token = null;
}

final tokenStoreProvider = Provider<TokenStore>((ref) => SecureTokenStore());
