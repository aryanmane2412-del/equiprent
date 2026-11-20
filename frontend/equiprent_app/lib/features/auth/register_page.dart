import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_controller.dart';
import '../../core/auth/auth_models.dart';
import '../../core/widgets/widgets.dart';
import '../../l10n/app_localizations.dart';
import 'equipment_inspection_policy.dart';
import 'auth_common.dart';

/// Self-service registration. The server maps these account types to safe
/// roles; fleet managers and admins are granted through private workflows.
class RegisterPage extends ConsumerStatefulWidget {
  const RegisterPage({super.key});

  @override
  ConsumerState<RegisterPage> createState() => _RegisterPageState();
}

class _RegisterPageState extends ConsumerState<RegisterPage> {
  final _form = GlobalKey<FormState>();
  final _name = TextEditingController();
  final _email = TextEditingController();
  final _phone = TextEditingController();
  final _password = TextEditingController();
  final _business = TextEditingController();

  AccountType _type = AccountType.customer;
  bool _accepted = false;
  bool _acceptedInspectionPolicy = false;
  bool _busy = false;
  Object? _error;
  String? _devNotice;

  @override
  void dispose() {
    for (final c in [_name, _email, _phone, _password, _business]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _submit() async {
    final valid = _form.currentState?.validate() ?? false;
    if (!_accepted) {
      setState(() => _error = null);
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Please accept the terms to continue.')),
      );
      return;
    }
    if (!valid) return;
    final phone = AuthValidators.normalisePhone(_phone.text)!;
    setState(() {
      _busy = true;
      _error = null;
      _devNotice = null;
    });
    try {
      final result = await ref
          .read(authControllerProvider.notifier)
          .register(
            accountType: _type,
            fullName: _name.text,
            email: _email.text,
            phone: phone,
            password: _password.text,
            organizationName: _type == AccountType.equipmentOwner
                ? _business.text
                : null,
            acceptInspectionPolicy: _acceptedInspectionPolicy,
            inspectionPolicyVersion: equipmentInspectionPolicyVersion,
          );
      if (result.devNotice != null) {
        // Demo mode: the details belong to an existing account, so no code
        // was issued. Say so instead of opening an empty verify screen.
        if (mounted) setState(() => _devNotice = result.devNotice);
        return;
      }
      ref
          .read(pendingVerificationProvider.notifier)
          .set(
            PendingVerification(
              phone: phone,
              email: _email.text.trim().toLowerCase(),
              message: result.message,
              devOtp: result.devOtp,
              devEmailToken: result.devEmailToken,
            ),
          );
      if (mounted) context.go(AuthRoutes.verify);
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final strings = AppLocalizations.of(context);
    final minLength =
        ref.watch(authCapabilitiesProvider).value?.passwordMinLength ?? 10;
    return AuthScaffold(
      title: 'Create your account',
      subtitle:
          'Fleet managers and administrators are added privately by EQUIPRENT or an organization owner.',
      onBack: () => context.go(AuthRoutes.signIn),
      child: Form(
        key: _form,
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Text('I want to', style: theme.textTheme.titleSmall),
            const Gap(AppSpacing.sm),
            for (final type in AccountType.values)
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.sm),
                child: AppCard(
                  key: ValueKey('account-type-${type.apiValue}'),
                  onTap: () => setState(() => _type = type),
                  borderColor: _type == type ? AppColors.navy700 : null,
                  semanticLabel: '${type.label}. ${type.description}',
                  child: Row(
                    children: [
                      Icon(
                        _type == type
                            ? Icons.radio_button_checked
                            : Icons.radio_button_off,
                        color: _type == type
                            ? AppColors.navy700
                            : AppColors.slate400,
                        size: 20,
                      ),
                      const Gap(AppSpacing.sm),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(type.label, style: theme.textTheme.titleSmall),
                            Text(
                              type.description,
                              style: theme.textTheme.bodySmall?.copyWith(
                                color: AppColors.slate600,
                              ),
                            ),
                          ],
                        ),
                      ),
                    ],
                  ),
                ),
              ),
            const Gap(AppSpacing.md),
            AuthErrorBanner(error: _error),
            if (_devNotice != null)
              Padding(
                padding: const EdgeInsets.only(bottom: AppSpacing.md),
                child: InlineBanner(
                  key: const ValueKey('dev-notice'),
                  tone: AppTone.warning,
                  title: 'Demo mode: already registered',
                  message: _devNotice!,
                  action: TextButton(
                    onPressed: () => context.go(AuthRoutes.signIn),
                    child: const Text('Sign in'),
                  ),
                ),
              ),
            AppTextField(
              key: const ValueKey('reg-name'),
              label: 'Full name',
              controller: _name,
              prefixIcon: AppIcons.user,
              textInputAction: TextInputAction.next,
              onChanged: (_) {
                if (_error != null) setState(() => _error = null);
              },
              validator: (v) => (v ?? '').trim().length < 2
                  ? 'Enter your name'
                  : serverFieldError(_error, 'full_name'),
            ),
            if (_type == AccountType.equipmentOwner) ...[
              const Gap(AppSpacing.md),
              AppTextField(
                key: const ValueKey('reg-business'),
                label: 'Business name',
                controller: _business,
                prefixIcon: AppIcons.building,
                textInputAction: TextInputAction.next,
                validator: (v) => (v ?? '').trim().length < 2
                    ? 'Enter your business name'
                    : null,
              ),
            ],
            const Gap(AppSpacing.md),
            AppTextField(
              key: const ValueKey('reg-email'),
              label: 'Email',
              controller: _email,
              prefixIcon: AppIcons.mail,
              keyboardType: TextInputType.emailAddress,
              textInputAction: TextInputAction.next,
              onChanged: (_) {
                if (_error != null) setState(() => _error = null);
              },
              validator: AuthValidators.email,
            ),
            const Gap(AppSpacing.md),
            AppTextField(
              key: const ValueKey('reg-phone'),
              label: 'Mobile number',
              controller: _phone,
              prefixText: '+91 ',
              keyboardType: TextInputType.phone,
              inputFormatters: [
                FilteringTextInputFormatter.allow(RegExp(r'[0-9+ ]')),
                LengthLimitingTextInputFormatter(15),
              ],
              textInputAction: TextInputAction.next,
              onChanged: (_) {
                if (_error != null) setState(() => _error = null);
              },
              validator: AuthValidators.phone,
            ),
            const Gap(AppSpacing.md),
            PasswordField(
              fieldKey: const ValueKey('reg-password'),
              label: 'Password',
              controller: _password,
              onChanged: (_) {
                if (_error != null) setState(() => _error = null);
              },
              helper:
                  'At least $minLength characters, mixing letters with numbers or symbols. Avoid your name or number.',
              validator: (v) =>
                  AuthValidators.newPassword(v, minLength: minLength) ??
                  AuthValidators.passwordPersonalInfo(
                    v,
                    fullName: _name.text,
                    email: _email.text,
                    phone: _phone.text,
                  ) ??
                  serverFieldError(_error, 'password'),
            ),
            const Gap(AppSpacing.md),
            CheckboxListTile(
              key: const ValueKey('reg-terms'),
              value: _accepted,
              onChanged: (v) => setState(() => _accepted = v ?? false),
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              title: const Text(
                'I accept the Terms of Service and Privacy Policy',
              ),
            ),
            CheckboxListTile(
              key: const ValueKey('reg-inspection-policy'),
              value: _acceptedInspectionPolicy,
              onChanged: (value) => setState(
                () => _acceptedInspectionPolicy = value ?? false,
              ),
              contentPadding: EdgeInsets.zero,
              controlAffinity: ListTileControlAffinity.leading,
              title: const Text(
                'I have read and agree to the EQUIPRENT Equipment Inspection Policy.',
              ),
              subtitle: TextButton(
                onPressed: () => showDialog<void>(
                  context: context,
                  builder: (context) => AlertDialog(
                    title: const Text('Equipment Inspection Policy'),
                    content: SizedBox(
                      width: 560,
                      height: 420,
                      child: SingleChildScrollView(
                        child: SelectableText(
                          equipmentInspectionPolicy(Localizations.localeOf(context)),
                        ),
                      ),
                    ),
                    actions: [
                      TextButton(
                        onPressed: () => Navigator.pop(context),
                        child: const Text('Close'),
                      ),
                    ],
                  ),
                ),
                child: Text(strings.readFullPolicy),
              ),
            ),
            const Gap(AppSpacing.md),
            AppButton(
              key: const ValueKey('register'),
              label: 'Create account',
              expand: true,
              size: AppButtonSize.large,
              variant: AppButtonVariant.accent,
              isLoading: _busy,
              onPressed: _busy || !_acceptedInspectionPolicy ? null : _submit,
            ),
            const Gap(AppSpacing.sm),
            TextButton(
              onPressed: () => context.go(AuthRoutes.signIn),
              child: const Text('Already have an account? Sign in'),
            ),
          ],
        ),
      ),
    );
  }
}
