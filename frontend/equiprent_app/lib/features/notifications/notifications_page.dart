import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/session/active_role_provider.dart';
import '../comms/presentation/ui.dart';
import '../comms/comms_routes.dart';
import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/push/push_service.dart';

/// Notification centre: every notification for the signed-in user, newest
/// first. Tapping one marks it read and opens what it is about.
class NotificationsPage extends ConsumerWidget {
  const NotificationsPage({super.key});

  Future<void> _refresh(WidgetRef ref) async {
    ref.invalidate(notificationListProvider);
    ref.invalidate(unreadNotificationsProvider);
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final page = ref.watch(notificationListProvider);
    final unread = page.value?.items.where((n) => !n.read).length ?? 0;
    return AppPage(
      title: 'Notifications',
      subtitle: page.hasValue ? (unread == 0 ? 'All caught up' : '$unread unread') : null,
      topLevel: false,
      actions: [
        AppIconButton(
          key: const Key('mark-all-read'),
          icon: AppIcons.check,
          tooltip: 'Mark all as read',
          onPressed: unread == 0
              ? null
              : () => runComms(context, () async {
                    await ref.read(commsApiProvider).markAllNotificationsRead();
                    await _refresh(ref);
                  }),
        ),
        AppIconButton(key: const Key('notification-settings'), icon: AppIcons.settings, tooltip: 'Push settings', onPressed: () => showPushSettings(context)),
      ],
      body: AsyncBody(
        value: page,
        onRetry: () => _refresh(ref),
        builder: (p) => p.items.isEmpty
            ? const EmptyState(icon: AppIcons.bell, title: 'No notifications yet', message: 'Booking updates, payments, messages and reminders appear here.')
            : RefreshIndicator(
                onRefresh: () => _refresh(ref),
                child: ListView.separated(
                  padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
                  itemCount: p.items.length,
                  separatorBuilder: (_, _) => const Divider(height: 1, indent: 72),
                  itemBuilder: (context, i) => _NotificationTile(n: p.items[i]),
                ),
              ),
      ),
    );
  }
}

class _NotificationTile extends ConsumerWidget {
  const _NotificationTile({required this.n});
  final AppNotification n;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    return Material(
      color: n.read ? Colors.transparent : AppColors.navy50,
      child: InkWell(
        onTap: () async {
          if (!n.read) {
            try {
              await ref.read(commsApiProvider).markNotificationRead(n.id);
            } catch (_) {
              // Best effort: it is shown as read again on the next refresh.
            }
            ref.invalidate(notificationListProvider);
            ref.invalidate(unreadNotificationsProvider);
          }
          final role = ref.read(activeRoleProvider);
          final route = role == null ? null : routeForNotification(role, n.type, n.data);
          if (route != null && context.mounted) context.push(route);
        },
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: AppSpacing.md, vertical: AppSpacing.ms),
          child: Row(crossAxisAlignment: CrossAxisAlignment.start, children: [
            IconTile(icon: categoryIcon(n.category), tone: categoryTone(n.category)),
            const Gap(AppSpacing.ms),
            Expanded(
              child: Column(crossAxisAlignment: CrossAxisAlignment.start, children: [
                Row(children: [
                  Expanded(
                    child: Text(n.title,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: theme.textTheme.titleSmall?.copyWith(fontWeight: n.read ? FontWeight.w500 : FontWeight.w700)),
                  ),
                  Text(Fmt.ago(n.createdAt, DateTime.now()), style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
                ]),
                const Gap(2),
                Text(n.body, style: theme.textTheme.bodyMedium?.copyWith(color: AppColors.slate700)),
              ]),
            ),
            if (!n.read)
              const Padding(
                padding: EdgeInsets.only(left: AppSpacing.sm, top: 6),
                child: SizedBox(width: 8, height: 8, child: DecoratedBox(decoration: BoxDecoration(color: AppColors.orange500, shape: BoxShape.circle))),
              ),
          ]),
        ),
      ),
    );
  }
}

/// Push on/off per category. In-app notifications are always kept.
Future<void> showPushSettings(BuildContext context) => showAppSheet<void>(
      context,
      title: 'Push notifications',
      subtitle: 'Choose what may alert this phone. Everything still appears in the app.',
      builder: (_) => const _PushSettings(),
    );

class _PushSettings extends ConsumerStatefulWidget {
  const _PushSettings();
  @override
  ConsumerState<_PushSettings> createState() => _PushSettingsState();
}

class _PushSettingsState extends ConsumerState<_PushSettings> {
  final _saving = <String>{};

  Future<void> _toggle(List<PushPreference> current, PushPreference p, bool on) async {
    setState(() => _saving.add(p.category));
    await runComms(context, () async {
      await ref.read(commsApiProvider).setPushPreferences([PushPreference(category: p.category, push: on)]);
      ref.invalidate(pushPreferencesProvider);
    });
    if (mounted) setState(() => _saving.remove(p.category));
  }

  @override
  Widget build(BuildContext context) {
    final prefs = ref.watch(pushPreferencesProvider);
    final available = ref.watch(pushServiceProvider).available;
    return Column(crossAxisAlignment: CrossAxisAlignment.stretch, mainAxisSize: MainAxisSize.min, children: [
      if (!available)
        const Padding(
          padding: EdgeInsets.only(bottom: AppSpacing.sm),
          child: InlineBanner(
            tone: AppTone.info,
            message: 'Push is not set up in this build. You will still see every notification here and on the bell.',
          ),
        ),
      prefs.when(
        loading: () => const Padding(padding: EdgeInsets.all(AppSpacing.lg), child: Center(child: CircularProgressIndicator())),
        error: (e, _) => ErrorState(message: describeError(e), onRetry: () => ref.invalidate(pushPreferencesProvider)),
        data: (list) => Column(children: [
          for (final p in list)
            SwitchListTile(
              key: Key('push-${p.category}'),
              contentPadding: EdgeInsets.zero,
              title: Text(p.label),
              subtitle: Text(p.description),
              value: p.push,
              onChanged: _saving.contains(p.category) ? null : (on) => _toggle(list, p, on),
            ),
        ]),
      ),
    ]);
  }
}
