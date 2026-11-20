import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

const supportedAppLocales = <Locale>[Locale('en'), Locale('hi'), Locale('mr')];

abstract interface class LocaleStore {
  Future<String?> read();
  Future<void> write(String code);
}

class SecureLocaleStore implements LocaleStore {
  SecureLocaleStore([FlutterSecureStorage? storage])
    : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;
  static const _key = 'equiprent.app_locale';

  @override
  Future<String?> read() => _storage.read(key: _key);

  @override
  Future<void> write(String code) => _storage.write(key: _key, value: code);
}

final localeStoreProvider = Provider<LocaleStore>((ref) => SecureLocaleStore());

class AppLocaleController extends Notifier<Locale> {
  @override
  Locale build() {
    Future.microtask(_restore);
    return const Locale('en');
  }

  Future<void> _restore() async {
    try {
      final code = await ref.read(localeStoreProvider).read();
      if (code != null &&
          supportedAppLocales.any((locale) => locale.languageCode == code)) {
        state = Locale(code);
      }
    } on Object {
      // Keep English as the safe fallback when browser/device storage is unavailable.
    }
  }

  Future<void> setLanguageCode(String code) async {
    if (!supportedAppLocales.any((locale) => locale.languageCode == code)) {
      return;
    }
    state = Locale(code);
    try {
      await ref.read(localeStoreProvider).write(code);
    } on Object {
      // The selection applies for this run even when persistence is unavailable.
    }
  }
}

final appLocaleProvider = NotifierProvider<AppLocaleController, Locale>(
  AppLocaleController.new,
);
