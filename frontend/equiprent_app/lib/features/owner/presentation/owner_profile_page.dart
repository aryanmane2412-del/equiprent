import '../owner_routes.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart'
    show describeError;

class OwnerProfilePage extends ConsumerWidget {
  const OwnerProfilePage({super.key});

  Future<void> _editProfile(BuildContext context, WidgetRef ref) async {
    final user = ref.read(authControllerProvider).user;
    if (user == null) return;
    final name = TextEditingController(text: user.fullName);
    final email = TextEditingController(text: user.email ?? '');
    final saved = await showDialog<bool>(
      context: context,
      builder: (ctx) => AppDialog(
        title: 'Owner contact details',
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            AppTextField(label: 'Full name', controller: name),
            const Gap(AppSpacing.sm),
            AppTextField(
              label: 'Email',
              controller: email,
              keyboardType: TextInputType.emailAddress,
            ),
          ],
        ),
        actions: [
          AppButton(
            label: 'Cancel',
            variant: AppButtonVariant.secondary,
            onPressed: () => Navigator.of(ctx).pop(false),
          ),
          AppButton(
            label: 'Save',
            onPressed: () => Navigator.of(ctx).pop(true),
          ),
        ],
      ),
    );
    if (saved == true) {
      try {
        await ref
            .read(authControllerProvider.notifier)
            .updateProfile(
              fullName: name.text.trim(),
              email: email.text.trim().isEmpty ? null : email.text.trim(),
            );
        if (context.mounted) {
          AppToast.success(
            context,
            'Profile updated. Verify your email if you changed it.',
          );
        }
      } catch (e) {
        if (context.mounted) AppToast.error(context, describeError(e));
      }
    }
    name.dispose();
    email.dispose();
  }

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final theme = Theme.of(context);
    final user = ref.watch(authControllerProvider).user;
    return AppPage(
      title: 'Business profile',
      body: PageBody(
        children: [
          AppCard(
            child: Column(
              children: [
                ListRow(
                  key: const Key('open-owner-verification'),
                  title: 'Business verification',
                  subtitle: 'Business details, documents and payout account',
                  leading: const Icon(AppIcons.shield, size: 20),
                  trailing: const Icon(AppIcons.chevronRight, size: 18),
                  onTap: () => context.push('/owner/verification'),
                ),
                ListRow(
                  key: const Key('open-owner-reviews'),
                  title: 'Ratings & reviews',
                  subtitle:
                      'What customers say about your machines and service',
                  leading: const Icon(AppIcons.star, size: 20),
                  trailing: const Icon(AppIcons.chevronRight, size: 18),
                  onTap: () => context.push('/owner/reviews'),
                ),
                ListRow(
                  key: const Key('open-owner-messages'),
                  title: 'Messages',
                  subtitle: 'Booking chats and listing enquiries',
                  leading: const Icon(AppIcons.chat, size: 20),
                  trailing: const Icon(AppIcons.chevronRight, size: 18),
                  onTap: () => context.push('/owner/messages'),
                ),
              ],
            ),
          ),
          const Gap(AppSpacing.md),
          AppCard(
            child: Row(
              children: [
                AppAvatar(name: user?.fullName ?? 'Equipment owner', size: 64),
                const Gap(AppSpacing.md),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text(
                        user?.fullName ?? 'Equipment owner',
                        style: theme.textTheme.titleLarge,
                      ),
                      Text(
                        [user?.phone, user?.email]
                            .whereType<String>()
                            .where((v) => v.isNotEmpty)
                            .join(' / '),
                        style: theme.textTheme.bodyMedium?.copyWith(
                          color: AppColors.slate600,
                        ),
                      ),
                      Align(
                        alignment: Alignment.centerRight,
                        child: TextButton(
                          onPressed: () => _editProfile(context, ref),
                          child: const Text('Edit contact details'),
                        ),
                      ),
                      const Gap(AppSpacing.sm),
                      Wrap(
                        spacing: 6,
                        runSpacing: 6,
                        children: [
                          const StatusBadge(
                            label: 'View business verification',
                            tone: AppTone.neutral,
                          ),
                          const StatusBadge(
                            label:
                                'Rating information is shown from customer reviews',
                            tone: AppTone.accent,
                          ),
                          const StatusBadge(
                            label: 'Response information is unavailable here',
                            tone: AppTone.brand,
                          ),
                        ],
                      ),
                    ],
                  ),
                ),
              ],
            ),
          ),
          const Gap(AppSpacing.md),
          SectionCard(
            title: 'Business and account details',
            icon: AppIcons.shield,
            child: Column(
              children: [
                ListRow(
                  title: 'Business verification',
                  subtitle: 'View submitted business and tax details.',
                  leading: const Icon(AppIcons.shield, size: 20),
                  onTap: () => context.push('/owner/verification'),
                ),
                ListRow(
                  title: 'Payout account',
                  subtitle: 'Review account details in business verification.',
                  leading: const Icon(AppIcons.settlements, size: 20),
                  onTap: () => context.push('/owner/verification'),
                ),
                ListRow(
                  key: const Key('open-owner-drivers'),
                  title: 'My drivers',
                  subtitle: 'Add drivers and see who is available for jobs.',
                  leading: const Icon(AppIcons.operator, size: 20),
                  trailing: const Icon(AppIcons.chevronRight, size: 18),
                  onTap: () => context.push(OwnerRoutes.drivers),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
