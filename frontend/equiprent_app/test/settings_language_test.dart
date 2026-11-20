import 'package:equiprent_app/core/localization/locale_controller.dart';
import 'package:equiprent_app/core/session/user_role.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'test_helpers.dart';

class _MemoryLocaleStore implements LocaleStore {
  String? code;

  @override
  Future<String?> read() async => code;

  @override
  Future<void> write(String value) async => code = value;
}

void main() {
  testWidgets('owner and driver can reach Settings from the mobile More menu', (
    tester,
  ) async {
    final container = await pumpApp(tester);

    await goTo(tester, container, '/owner', role: UserRole.owner);
    await tapOn(tester, find.text('More'));
    await tapOn(tester, find.text('Settings').last);
    expect(find.text('Settings'), findsOneWidget);
    expect(find.byKey(const ValueKey('app-language-selector')), findsOneWidget);

    await goTo(tester, container, '/operator', role: UserRole.operator);
    await tapOn(tester, find.text('More'));
    await tapOn(tester, find.text('Settings').last);
    expect(find.text('Settings'), findsOneWidget);
    expect(find.byKey(const ValueKey('app-language-selector')), findsOneWidget);
  });

  final panels = <(UserRole, String, String)>[
    (UserRole.customer, '/customer/profile', 'भाषा'),
    (UserRole.owner, '/owner/settings', 'सेटिंग्ज'),
    (UserRole.operator, '/operator/settings', 'सेटिंग्ज'),
    (UserRole.admin, '/admin/settings', 'सेटिंग्ज'),
    (UserRole.fleetManager, '/fleet/settings', 'सेटिंग्ज'),
  ];

  for (final (role, route, translatedMarker) in panels) {
    testWidgets('$role can change app language from its shared selector', (
      tester,
    ) async {
      final store = _MemoryLocaleStore();
      final container = await pumpApp(
        tester,
        localeStore: store,
        auth: signedInAs([role]),
      );
      await goTo(tester, container, route, role: role);

      await tapOn(tester, find.byKey(const ValueKey('app-language-selector')));
      await tapOn(tester, find.text('मराठी'));

      expect(store.code, 'mr');
      expect(container.read(appLocaleProvider).languageCode, 'mr');
      expect(find.text(translatedMarker), findsWidgets);
    });
  }
}
