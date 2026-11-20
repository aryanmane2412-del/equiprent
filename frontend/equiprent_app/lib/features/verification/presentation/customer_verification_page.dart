import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/session/user_role.dart';
import '../../../core/widgets/widgets.dart';
import '../../auth/auth_common.dart' show AuthRoutes;
import '../data/verification_api.dart';
import '../data/verification_models.dart';
import 'verification_widgets.dart';

/// Customer profile: personal details, verified mobile and email, an
/// address and a profile image (a checklist; no manual review).
class CustomerVerificationPage extends ConsumerWidget {
  const CustomerVerificationPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final status = ref.watch(customerStatusProvider);
    final user = ref.watch(authControllerProvider).user;
    return AppPage(
      title: 'Profile & verification',
      topLevel: false,
      body: AsyncBody(
        value: status,
        onRetry: () => ref.invalidate(customerStatusProvider),
        builder: (s) => PageBody(
          children: [
            if (s.complete)
              const InlineBanner(
                title: 'Profile complete',
                message: 'Owners see you as a verified customer.',
                tone: AppTone.success,
                icon: AppIcons.verified,
              )
            else
              const InlineBanner(
                title: 'Complete your profile',
                message:
                    'A complete profile helps owners accept your bookings faster.',
                tone: AppTone.info,
              ),
            const Gap(AppSpacing.md),
            ResponsiveGrid(
              columns: const Responsive(compact: 1, medium: 2),
              children: [
                SectionCard(
                  title: 'Profile image',
                  icon: AppIcons.camera,
                  child: Row(
                    children: [
                      _Avatar(link: s.avatar, name: user?.displayName ?? ''),
                      const Gap(AppSpacing.md),
                      Expanded(
                        child: AppButton(
                          key: const Key('change-avatar'),
                          label: s.avatar == null
                              ? 'Add photo'
                              : 'Change photo',
                          icon: AppIcons.upload,
                          variant: AppButtonVariant.secondary,
                          onPressed: () => _changeAvatar(context, ref),
                        ),
                      ),
                    ],
                  ),
                ),
                SectionCard(
                  title: 'Personal details',
                  icon: AppIcons.user,
                  trailing: TextButton(
                    onPressed: () => context.push(AuthRoutes.account),
                    child: const Text('Edit'),
                  ),
                  child: Column(
                    children: [
                      InfoRow(
                        label: 'Name',
                        value: user?.fullName.isNotEmpty == true
                            ? user!.fullName
                            : '—',
                      ),
                      InfoRow(
                        label: 'Mobile',
                        value:
                            '${user?.phone ?? '—'}${user?.phoneVerified == true ? ' · verified' : ''}',
                      ),
                      InfoRow(
                        label: 'Email',
                        value:
                            '${user?.email ?? '—'}${user?.emailVerified == true ? ' · verified' : ''}',
                      ),
                    ],
                  ),
                ),
                ChecklistCard(checks: s.checks, title: 'Profile checklist'),
                SectionCard(
                  title: 'Address',
                  icon: AppIcons.siteMarker,
                  trailing: TextButton(
                    key: const Key('add-address'),
                    onPressed: () => _addAddress(context, ref),
                    child: const Text('Add'),
                  ),
                  child: Text(
                    s.checks.any((c) => c.key == 'address' && c.done)
                        ? 'You have a saved address.'
                        : 'Add your home or site address.',
                  ),
                ),
              ],
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _changeAvatar(BuildContext context, WidgetRef ref) async {
    final file = await ref.read(documentPickerProvider).pick(imagesOnly: true);
    if (file == null || !context.mounted) return;
    final ok = await runVerificationAction(context, () async {
      final api = ref.read(verificationApiProvider);
      final id = await api.upload(UploadPurpose.avatar, file);
      await api.setAvatar(id);
    }, success: 'Profile image updated');
    if (ok) ref.invalidate(customerStatusProvider);
  }

  Future<void> _addAddress(BuildContext context, WidgetRef ref) async {
    final org = ref.read(roleOrgProvider(UserRole.customer));
    if (org == null) {
      AppToast.error(context, 'No customer account found');
      return;
    }
    final address = await showAppSheet<PostalAddress>(
      context,
      title: 'Add an address',
      builder: (_) => const AddressForm(),
    );
    if (address == null || !context.mounted) return;
    final ok = await runVerificationAction(
      context,
      () => ref.read(verificationApiProvider).addAddress(org, {
        'label': 'Home',
        ...address.toJson(),
      }),
      success: 'Address saved',
    );
    if (ok) ref.invalidate(customerStatusProvider);
  }
}

class _Avatar extends StatelessWidget {
  const _Avatar({required this.link, required this.name});
  final SignedLink? link;
  final String name;

  @override
  Widget build(BuildContext context) {
    if (link == null) return AppAvatar(name: name, size: 64);
    return ClipOval(
      child: Image.network(
        link!.url,
        width: 64,
        height: 64,
        fit: BoxFit.cover,
        errorBuilder: (_, _, _) => AppAvatar(name: name, size: 64),
      ),
    );
  }
}

/// Indian state codes (GST) for address forms.
const stateCodes = {
  '27': 'Maharashtra',
  '29': 'Karnataka',
  '33': 'Tamil Nadu',
  '24': 'Gujarat',
  '07': 'Delhi',
  '09': 'Uttar Pradesh',
  '36': 'Telangana',
  '37': 'Andhra Pradesh',
  '32': 'Kerala',
  '08': 'Rajasthan',
  '23': 'Madhya Pradesh',
  '19': 'West Bengal',
  '06': 'Haryana',
  '03': 'Punjab',
  '21': 'Odisha',
  '10': 'Bihar',
  '30': 'Goa',
};

/// Postal address form (returns a [PostalAddress]).
class AddressForm extends StatefulWidget {
  const AddressForm({
    super.key,
    this.initial,
    this.submitLabel = 'Save address',
    this.homeAddress = false,
  });
  final PostalAddress? initial;
  final String submitLabel;
  final bool homeAddress;
  @override
  State<AddressForm> createState() => _AddressFormState();
}

class _AddressFormState extends State<AddressForm> {
  final _form = GlobalKey<FormState>();
  late final _line1 = TextEditingController(text: widget.initial?.line1);
  late final _line2 = TextEditingController(text: widget.initial?.line2);
  late final _locality = TextEditingController(text: widget.initial?.locality);
  late final _city = TextEditingController(text: widget.initial?.city);
  late final _pincode = TextEditingController(text: widget.initial?.pincode);
  late String? _state = widget.initial?.stateCode.isNotEmpty == true
      ? widget.initial!.stateCode
      : '27';

  @override
  void dispose() {
    for (final c in [_line1, _line2, _locality, _city, _pincode]) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppTextField(
            label: 'Address line 1',
            controller: _line1,
            requiredField: true,
            validator: (v) =>
                (v ?? '').trim().length < 3 ? 'Enter the address' : null,
          ),
          const Gap(AppSpacing.md),
          AppTextField(label: 'Address line 2 (optional)', controller: _line2),
          if (widget.homeAddress) ...[
            const Gap(AppSpacing.md),
            AppTextField(
              label: 'Area / locality (optional)',
              controller: _locality,
            ),
          ],
          const Gap(AppSpacing.md),
          AppTextField(
            label: 'City',
            controller: _city,
            requiredField: true,
            validator: (v) =>
                (v ?? '').trim().isEmpty ? 'Enter the city' : null,
          ),
          const Gap(AppSpacing.md),
          AppDropdown<String>(
            label: 'State',
            value: _state,
            items: stateCodes.keys.toList(),
            itemLabel: (c) => stateCodes[c] ?? c,
            onChanged: (v) => setState(() => _state = v),
          ),
          const Gap(AppSpacing.md),
          AppTextField(
            label: 'PIN code',
            controller: _pincode,
            keyboardType: TextInputType.number,
            requiredField: true,
            validator: (v) => RegExp(r'^[1-9]\d{5}$').hasMatch((v ?? '').trim())
                ? null
                : 'Enter a 6-digit PIN code',
          ),
          const Gap(AppSpacing.lg),
          AppButton(
            key: const Key('save-address'),
            label: widget.submitLabel,
            onPressed: () {
              if (!_form.currentState!.validate() || _state == null) return;
              Navigator.of(context).pop(
                PostalAddress(
                  line1: _line1.text.trim(),
                  line2: _line2.text.trim().isEmpty ? null : _line2.text.trim(),
                  locality: _locality.text.trim().isEmpty
                      ? null
                      : _locality.text.trim(),
                  city: _city.text.trim(),
                  stateCode: _state!,
                  pincode: _pincode.text.trim(),
                ),
              );
            },
          ),
        ],
      ),
    );
  }
}
