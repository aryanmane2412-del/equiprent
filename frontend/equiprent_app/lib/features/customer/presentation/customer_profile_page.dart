import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/session/user_role.dart';
import '../../../core/widgets/widgets.dart'
    hide Review, ChatMessage, AppNotification;
import '../../../l10n/app_localizations.dart';
import '../../settings/app_settings_page.dart' show LanguageSelector;
import '../../comms/data/comms_api.dart';
import '../../comms/data/comms_models.dart' show BookingSummary;
import '../../comms/presentation/customer_care.dart';
import '../../discovery/data/discovery_api.dart';
import '../../listings/presentation/listing_widgets.dart';
import '../customer_module.dart';

class CustomerProfilePage extends ConsumerStatefulWidget {
  const CustomerProfilePage({super.key});

  @override
  ConsumerState<CustomerProfilePage> createState() =>
      _CustomerProfilePageState();
}

class _CustomerProfilePageState extends ConsumerState<CustomerProfilePage> {
  bool _pushBooking = true;
  bool _whatsapp = true;
  bool _promo = false;
  @override
  Widget build(BuildContext context) {
    final l = AppLocalizations.of(context);
    final theme = Theme.of(context);
    final bookings =
        ref.watch(bookingsProvider(BookingSide.customer)).value ??
        const <BookingSummary>[];
    final spent = bookings
        .where((b) => b.status == 'completed' || b.status == 'closed')
        .fold<int>(0, (s, b) => s + b.totalPaise);
    final user = ref.watch(authControllerProvider).user;
    final name = (user?.fullName.isNotEmpty ?? false)
        ? user!.fullName
        : 'Your account';

    final header = AppCard(
      child: Row(
        children: [
          AppAvatar(name: name, size: 64),
          const Gap(AppSpacing.md),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(name, style: theme.textTheme.titleLarge),
                Text(
                  user?.phone ?? '',
                  style: theme.textTheme.bodyMedium?.copyWith(
                    color: AppColors.slate600,
                  ),
                ),
                const Gap(AppSpacing.sm),
                Wrap(
                  spacing: 6,
                  runSpacing: 6,
                  children: [
                    StatusBadges.kyc(KycStatus.verified),
                    const StatusBadge(
                      label: 'GST registered',
                      tone: AppTone.brand,
                    ),
                  ],
                ),
              ],
            ),
          ),
          AppIconButton(
            icon: AppIcons.edit,
            tooltip: 'Profile editing unavailable',
            onPressed: null,
          ),
        ],
      ),
    );

    return AppPage(
      title: 'Profile',
      body: PageBody(
        children: [
          header,
          const Gap(AppSpacing.md),
          AppCard(
            child: ListRow(
              key: const Key('open-customer-verification'),
              title: 'Profile & verification',
              subtitle: 'Mobile, email, address and profile image',
              leading: const Icon(AppIcons.shield, size: 20),
              trailing: const Icon(AppIcons.chevronRight, size: 18),
              onTap: () => context.push('/customer/verification'),
            ),
          ),
          const Gap(AppSpacing.md),
          ResponsiveGrid(
            columns: const Responsive(compact: 1, medium: 3),
            children: [
              StatCard(
                label: 'BOOKINGS',
                value: '${bookings.length}',
                icon: AppIcons.bookings,
                onTap: () => context.go(CustomerRoutes.bookings),
              ),
              StatCard(
                label: 'SPENT ON COMPLETED',
                value: Fmt.inrCompact(spent / 100),
                icon: AppIcons.wallet,
                tone: AppTone.accent,
                onTap: () => context.push(CustomerRoutes.payments),
              ),
              StatCard(
                label: 'SAVED MACHINES',
                value: '${ref.watch(savedIdsProvider).value?.length ?? 0}',
                icon: AppIcons.heart,
                tone: AppTone.danger,
                onTap: () => context.push(CustomerRoutes.saved),
              ),
            ],
          ),
          const Gap(AppSpacing.md),
          SplitLayout(
            breakpoint: 900,
            sideWidth: 380,
            main: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SectionCard(
                  title: 'Company & GST',
                  icon: AppIcons.building,
                  trailing: TextButton(
                    onPressed: null,
                    child: const Text('Edit'),
                  ),
                  child: const Column(
                    children: [
                      InfoRow(
                        label: 'Legal name',
                        value: 'Deshmukh Constructions',
                      ),
                      InfoRow(label: 'GSTIN', value: '27AAKFD4412M1Z9'),
                      InfoRow(label: 'PAN', value: 'AAKFD••••M'),
                      InfoRow(
                        label: 'Billing address',
                        value: 'Wakad, Pune 411057',
                      ),
                      InfoRow(label: 'TDS (194-I)', value: 'Deducted by us'),
                    ],
                  ),
                ),
                const Gap(AppSpacing.md),
                SectionCard(
                  title: 'Saved sites',
                  icon: AppIcons.siteMarker,
                  trailing: TextButton(
                    onPressed: null,
                    child: const Text('Add'),
                  ),
                  child: const Column(
                    children: [
                      ListRow(
                        title: 'Wakad Residency – Plot 14',
                        subtitle: 'Survey 112, Bhumkar Chowk, Wakad',
                        leading: IconTile(icon: AppIcons.siteMarker, size: 36),
                        dense: true,
                      ),
                      ListRow(
                        title: 'Hinjewadi Phase 3 – Basement',
                        subtitle: 'Plot 7, Maan Road, Hinjewadi',
                        leading: IconTile(icon: AppIcons.siteMarker, size: 36),
                        dense: true,
                      ),
                      ListRow(
                        title: 'Baner Commercial – Slab L4',
                        subtitle: 'Baner–Pashan Link Road',
                        leading: IconTile(icon: AppIcons.siteMarker, size: 36),
                        dense: true,
                      ),
                    ],
                  ),
                ),
                const Gap(AppSpacing.md),
                SectionCard(
                  title: 'Saved equipment',
                  icon: AppIcons.heart,
                  trailing: TextButton(
                    key: const Key('profile-saved'),
                    onPressed: () => context.push(CustomerRoutes.saved),
                    child: const Text('View all'),
                  ),
                  child: ref
                      .watch(savedListProvider)
                      .when(
                        loading: () => const LinearProgressIndicator(),
                        error: (e, _) => Text(describeError(e)),
                        data: (saved) => saved.isEmpty
                            ? const EmptyState(
                                icon: AppIcons.heart,
                                title: 'Nothing saved',
                                message:
                                    'Tap the heart on a listing to save it.',
                                compact: true,
                              )
                            : Column(
                                children: [
                                  for (final i in saved.take(3))
                                    ListRow(
                                      title: i.card.title,
                                      subtitle: [
                                        i.card.priceLabel ?? 'Price on request',
                                        i.card.place,
                                      ].where((s) => s.isNotEmpty).join(' · '),
                                      leading: SizedBox(
                                        width: 48,
                                        child: ListingImage(
                                          categorySlug: i.card.categorySlug,
                                          url: i.card.coverUrl,
                                          aspectRatio: 1,
                                          borderRadius: const BorderRadius.all(
                                            Radius.circular(6),
                                          ),
                                        ),
                                      ),
                                      onTap: () => context.push(
                                        CustomerRoutes.equipment(i.id),
                                      ),
                                    ),
                                ],
                              ),
                      ),
                ),
              ],
            ),
            side: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SectionCard(
                  title: l.account,
                  icon: AppIcons.settings,
                  child: Column(
                    children: [
                      ListRow(
                        title: l.paymentsInvoices,
                        leading: const Icon(AppIcons.payments, size: 20),
                        onTap: () => context.push(CustomerRoutes.payments),
                        dense: true,
                      ),
                      ListRow(
                        title: l.helpSupport,
                        leading: const Icon(AppIcons.support, size: 20),
                        onTap: () =>
                            openCustomerCare(context, ref, UserRole.customer),
                        dense: true,
                      ),
                      ListRow(
                        title: l.privacyData,
                        subtitle: l.downloadDeleteData,
                        leading: const Icon(AppIcons.lock, size: 20),
                        onTap: null,
                        dense: true,
                      ),
                    ],
                  ),
                ),
                const Gap(AppSpacing.md),
                SectionCard(
                  title: l.preferences,
                  icon: AppIcons.bell,
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.stretch,
                    children: [
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(l.bookingUpdates),
                        value: _pushBooking,
                        onChanged: (v) => setState(() => _pushBooking = v),
                      ),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(l.whatsappUpdates),
                        value: _whatsapp,
                        onChanged: (v) => setState(() => _whatsapp = v),
                      ),
                      SwitchListTile(
                        contentPadding: EdgeInsets.zero,
                        title: Text(l.offersTips),
                        value: _promo,
                        onChanged: (v) => setState(() => _promo = v),
                      ),
                      const Gap(AppSpacing.sm),
                      const LanguageSelector(),
                    ],
                  ),
                ),
                const Gap(AppSpacing.md),
                AppButton(
                  label: l.signOut,
                  icon: AppIcons.logout,
                  variant: AppButtonVariant.secondary,
                  expand: true,
                  onPressed: () =>
                      ref.read(authControllerProvider.notifier).logout(),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
