import 'package:equiprent_app/core/localization/locale_controller.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

class MemoryLocaleStore implements LocaleStore {
  String? code;

  @override
  Future<String?> read() async => code;

  @override
  Future<void> write(String value) async => code = value;
}

void main() {
  test(
    'language selection is restricted to supported locales and persisted',
    () async {
      final store = MemoryLocaleStore();
      final container = ProviderContainer(
        overrides: [localeStoreProvider.overrideWithValue(store)],
      );
      addTearDown(container.dispose);

      await container.read(appLocaleProvider.notifier).setLanguageCode('mr');

      expect(container.read(appLocaleProvider).languageCode, 'mr');
      expect(store.code, 'mr');
      await container.read(appLocaleProvider.notifier).setLanguageCode('fr');
      expect(container.read(appLocaleProvider).languageCode, 'mr');
    },
  );

  test('saved locale restores when the app starts again', () async {
    final store = MemoryLocaleStore()..code = 'hi';
    final container = ProviderContainer(
      overrides: [localeStoreProvider.overrideWithValue(store)],
    );
    addTearDown(container.dispose);

    container.read(appLocaleProvider);
    await Future<void>.delayed(Duration.zero);

    expect(container.read(appLocaleProvider).languageCode, 'hi');
  });
}
