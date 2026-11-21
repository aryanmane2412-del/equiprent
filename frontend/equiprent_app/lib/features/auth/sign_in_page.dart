import 'package:flutter/foundation.dart' show kReleaseMode;
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_controller.dart';
import '../../core/widgets/widgets.dart';
import '../../l10n/app_localizations.dart';
import 'auth_common.dart';

enum _Method { password, otp }

/// Sign in with email/phone + password, or with a mobile OTP when the server
/// has an SMS provider (or dev simulation).
class SignInPage extends ConsumerStatefulWidget {
  const SignInPage({super.key});

  @override
  ConsumerState<SignInPage> createState() => _SignInPageState();
}

class _SignInPageState extends ConsumerState<SignInPage> {
  final _passwordForm = GlobalKey<FormState>();
  final _otpForm = GlobalKey<FormState>();
  final _identifier = TextEditingController();
  final _password = TextEditingController();
  final _phone = TextEditingController();
  final _code = TextEditingController();

  _Method _method = _Method.password;
  bool _busy = false;
  bool _codeSent = false;
  String? _devCode;
  Object? _error;

  @override
  void dispose() {
    for (final c in [_identifier, _password, _phone, _code]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _run(Future<void> Function() action) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await action();
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _signInWithPassword() async {
    if (!(_passwordForm.currentState?.validate() ?? false)) return;
    await _run(
      () => ref
          .read(authControllerProvider.notifier)
          .loginWithPassword(
            AuthValidators.normaliseIdentifier(_identifier.text),
            _password.text,
          ),
    );
  }

  Future<void> _sendCode() async {
    final phone = AuthValidators.normalisePhone(_phone.text);
    if (phone == null) {
      _otpForm.currentState?.validate();
      return;
    }
    await _run(() async {
      final secret = await ref
          .read(authControllerProvider.notifier)
          .requestOtp(phone);
      setState(() {
        _codeSent = true;
        _devCode = secret.code;
      });
    });
  }

  Future<void> _verifyCode() async {
    if (!(_otpForm.currentState?.validate() ?? false)) return;
    final phone = AuthValidators.normalisePhone(_phone.text)!;
    await _run(
      () => ref
          .read(authControllerProvider.notifier)
          .verifyOtp(phone, _code.text.trim()),
    );
  }

  @override
  Widget build(BuildContext context) {
    final l = AppLocalizations.of(context);
    final caps = ref.watch(authCapabilitiesProvider).value;
    final otpAvailable = caps?.mobileOtp ?? false;

    return AuthScaffold(
      title: l.signInToApp,
      subtitle:
          'Customers, equipment owners, operators, fleet managers and admins all sign in here.',
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          SegmentedButton<_Method>(
            key: const ValueKey('sign-in-method'),
            segments: [
              ButtonSegment(
                value: _Method.password,
                label: Text(l.password),
                icon: const Icon(AppIcons.lock, size: 16),
              ),
              ButtonSegment(
                value: _Method.otp,
                label: const Text('Mobile OTP'),
                icon: const Icon(AppIcons.phone, size: 16),
                enabled: otpAvailable,
              ),
            ],
            selected: {_method},
            onSelectionChanged: (s) => setState(() {
              _method = s.first;
              _error = null;
            }),
          ),
          if (caps != null && !otpAvailable) ...[
            const Gap(AppSpacing.sm),
            const InlineBanner(
              tone: AppTone.info,
              message:
                  'Mobile OTP sign-in is switched off until an SMS provider is configured. Use your email or mobile number and password.',
            ),
          ],
          if (!kReleaseMode && (caps?.devSimulation ?? false)) ...[
            const Gap(AppSpacing.md),
            DemoAccountsCard(
              onPick: (id, pw) {
                setState(() {
                  _method = _Method.password;
                  _identifier.text = id;
                  _password.text = pw;
                  _error = null;
                });
              },
            ),
          ],
          const Gap(AppSpacing.lg),
          AuthErrorBanner(error: _error),
          if (_method == _Method.password)
            _passwordSection()
          else
            _otpSection(caps?.devSimulation ?? false),
          const Gap(AppSpacing.lg),
          const Divider(),
          const Gap(AppSpacing.sm),
          Wrap(
            alignment: WrapAlignment.center,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text(l.newToApp),
              TextButton(
                key: const ValueKey('go-register'),
                onPressed: () => context.go(AuthRoutes.register),
                child: Text(l.createAccount),
              ),
            ],
          ),
        ],
      ),
    );
  }

  Widget _passwordSection() => Form(
    key: _passwordForm,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppTextField(
          key: const ValueKey('identifier'),
          label: AppLocalizations.of(context).emailOrMobile,
          controller: _identifier,
          prefixIcon: AppIcons.user,
          keyboardType: TextInputType.emailAddress,
          textInputAction: TextInputAction.next,
          validator: AuthValidators.identifier,
        ),
        const Gap(AppSpacing.md),
        PasswordField(
          fieldKey: const ValueKey('password'),
          label: AppLocalizations.of(context).password,
          controller: _password,
          validator: (v) => (v ?? '').isEmpty
              ? AppLocalizations.of(context).enterPassword
              : null,
          onSubmitted: (_) => _signInWithPassword(),
        ),
        Align(
          alignment: Alignment.centerRight,
          child: TextButton(
            key: const ValueKey('go-forgot'),
            onPressed: () => context.go(AuthRoutes.forgotPassword),
            child: Text(AppLocalizations.of(context).forgotPassword),
          ),
        ),
        AppButton(
          key: const ValueKey('sign-in'),
          label: AppLocalizations.of(context).signIn,
          expand: true,
          size: AppButtonSize.large,
          isLoading: _busy,
          onPressed: _busy ? null : _signInWithPassword,
        ),
      ],
    ),
  );

  Widget _otpSection(bool devSimulation) => Form(
    key: _otpForm,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppTextField(
          key: const ValueKey('otp-phone'),
          label: 'Mobile number',
          controller: _phone,
          prefixText: '+91 ',
          keyboardType: TextInputType.phone,
          inputFormatters: [
            FilteringTextInputFormatter.allow(RegExp(r'[0-9+ ]')),
            LengthLimitingTextInputFormatter(15),
          ],
          validator: AuthValidators.phone,
          enabled: !_codeSent,
        ),
        const Gap(AppSpacing.md),
        if (!_codeSent)
          AppButton(
            key: const ValueKey('send-code'),
            label: 'Send code',
            expand: true,
            size: AppButtonSize.large,
            isLoading: _busy,
            onPressed: _busy ? null : _sendCode,
          )
        else ...[
          if (_devCode != null) ...[
            DevSecretBanner(
              label: 'Your code',
              value: _devCode!,
              onUse: () => _code.text = _devCode!,
            ),
            const Gap(AppSpacing.md),
          ],
          AppTextField(
            key: const ValueKey('otp-code'),
            label: '6-digit code',
            controller: _code,
            keyboardType: TextInputType.number,
            inputFormatters: [
              FilteringTextInputFormatter.digitsOnly,
              LengthLimitingTextInputFormatter(6),
            ],
            validator: AuthValidators.otp,
            onSubmitted: (_) => _verifyCode(),
          ),
          const Gap(AppSpacing.md),
          AppButton(
            key: const ValueKey('verify-code'),
            label: 'Verify and sign in',
            expand: true,
            size: AppButtonSize.large,
            isLoading: _busy,
            onPressed: _busy ? null : _verifyCode,
          ),
          TextButton(
            onPressed: _busy
                ? null
                : () => setState(() {
                    _codeSent = false;
                    _code.clear();
                    _devCode = null;
                  }),
            child: const Text('Use a different number'),
          ),
        ],
        if (!devSimulation) ...[
          const Gap(AppSpacing.sm),
          Text(
            'New numbers get a customer account automatically.',
            style: Theme.of(
              context,
            ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
          ),
        ],
      ],
    ),
  );
}
