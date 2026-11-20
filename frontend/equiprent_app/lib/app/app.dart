import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/localization/locale_controller.dart';
import '../core/theme/app_theme.dart';
import '../features/comms/push/push_coordinator.dart';
import '../l10n/app_localizations.dart';
import 'router.dart';

class EquipRentApp extends ConsumerWidget {
  const EquipRentApp({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final locale = ref.watch(appLocaleProvider);
    return MaterialApp.router(
      title: 'EQUIPRENT',
      locale: locale,
      supportedLocales: supportedAppLocales,
      localizationsDelegates: AppLocalizations.localizationsDelegates,
      debugShowCheckedModeBanner: false,
      theme: AppTheme.light(),
      // Light theme only for now (see AppTheme); dark mode is planned.
      themeMode: ThemeMode.light,
      routerConfig: ref.watch(routerProvider),
      builder: (context, child) =>
          PushCoordinator(child: child ?? const SizedBox.shrink()),
    );
  }
}
