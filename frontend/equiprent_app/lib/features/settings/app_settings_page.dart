import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/localization/locale_controller.dart';
import '../../core/widgets/widgets.dart';
import '../../l10n/app_localizations.dart';
import '../auth/equipment_inspection_policy.dart';

/// Shared language control used by every role, so preferences are consistent
/// and update the app-level locale rather than a role-specific screen.
class LanguageSelector extends ConsumerWidget {
  const LanguageSelector({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final locale = ref.watch(appLocaleProvider);
    final l = AppLocalizations.of(context);
    final value = switch (locale.languageCode) {
      'hi' => l.hindi,
      'mr' => l.marathi,
      _ => l.english,
    };

    return PickerField(
      key: const ValueKey('app-language-selector'),
      label: l.language,
      value: value,
      icon: AppIcons.signal,
      onTap: () async {
        final selected = await showActionSheet<String>(
          context,
          title: l.language,
          actions: [
            SheetAction(value: 'en', label: l.english, icon: AppIcons.signal),
            SheetAction(value: 'hi', label: l.hindi, icon: AppIcons.signal),
            SheetAction(value: 'mr', label: l.marathi, icon: AppIcons.signal),
          ],
        );
        if (!context.mounted || selected == null) return;
        await ref.read(appLocaleProvider.notifier).setLanguageCode(selected);
      },
    );
  }
}

/// Role-independent settings surface. The selected locale is stored by
/// [AppLocaleController] and therefore survives sign-out and app restarts.
class AppSettingsPage extends StatelessWidget {
  const AppSettingsPage({super.key});

  @override
  Widget build(BuildContext context) {
    final l = AppLocalizations.of(context);
    return AppPage(
      title: l.settings,
      body: PageBody(
        children: [
          SectionCard(
            title: l.preferences,
            subtitle: l.languagePreferenceDescription,
            icon: AppIcons.settings,
            child: const LanguageSelector(),
          ),
          SectionCard(
            title: l.policiesAndTerms,
            subtitle: l.inspectionPolicyTitle,
            icon: AppIcons.fileText,
            child: ListTile(
              contentPadding: EdgeInsets.zero,
              title: Text(l.inspectionPolicyTitle),
              trailing: const Icon(Icons.chevron_right),
              onTap: () => Navigator.of(context).push(
                MaterialPageRoute<void>(
                  builder: (_) => const EquipmentInspectionPolicyPage(),
                ),
              ),
            ),
          ),
        ],
      ),
    );
  }
}
