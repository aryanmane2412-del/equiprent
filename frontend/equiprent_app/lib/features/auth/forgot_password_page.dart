import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_api.dart';
import '../../core/widgets/widgets.dart';
import 'auth_common.dart';

/// Password reset: request a code (email link code or SMS code), then set a
/// new password. The first step answers the same way for every identifier.
class ForgotPasswordPage extends ConsumerStatefulWidget {
  const ForgotPasswordPage({super.key});

  @override
  ConsumerState<ForgotPasswordPage> createState() => _ForgotPasswordPageState();
}

class _ForgotPasswordPageState extends ConsumerState<ForgotPasswordPage> {
  final _requestForm = GlobalKey<FormState>();
  final _resetForm = GlobalKey<FormState>();
  final _identifier = TextEditingController();
  final _secret = TextEditingController();
  final _password = TextEditingController();

  bool _busy = false;
  bool _requested = false;
  bool _done = false;
  String? _message;
  String? _devSecret;
  Object? _error;

  bool get _byPhone => !_identifier.text.contains('@');

  @override
  void dispose() {
    for (final c in [_identifier, _secret, _password]) {
      c.dispose();
    }
    super.dispose();
  }

  Future<void> _run(Future<void> Function() fn) async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await fn();
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  void _request() {
    if (!(_requestForm.currentState?.validate() ?? false)) return;
    _run(() async {
      final res = await ref.read(authApiProvider).forgotPassword(AuthValidators.normaliseIdentifier(_identifier.text));
      setState(() {
        _requested = true;
        _message = res.message;
        _devSecret = res.code ?? res.token;
      });
    });
  }

  void _reset() {
    if (!(_resetForm.currentState?.validate() ?? false)) return;
    _run(() async {
      final api = ref.read(authApiProvider);
      if (_byPhone) {
        await api.resetPassword(phone: AuthValidators.normalisePhone(_identifier.text), code: _secret.text.trim(), newPassword: _password.text);
      } else {
        await api.resetPassword(token: _secret.text.trim(), newPassword: _password.text);
      }
      setState(() => _done = true);
    });
  }

  @override
  Widget build(BuildContext context) {
    final minLength = ref.watch(authCapabilitiesProvider).value?.passwordMinLength ?? 10;
    return AuthScaffold(
      title: 'Reset your password',
      subtitle: 'We will send a code to your verified email address or mobile number.',
      onBack: () => context.go(AuthRoutes.signIn),
      child: _done
          ? Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              const InlineBanner(
                key: ValueKey('reset-done'),
                tone: AppTone.success,
                message: 'Your password was changed and you were signed out on every device.',
              ),
              const Gap(AppSpacing.md),
              AppButton(label: 'Sign in', expand: true, onPressed: () => context.go(AuthRoutes.signIn)),
            ])
          : Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              AuthErrorBanner(error: _error),
              Form(
                key: _requestForm,
                child: AppTextField(
                  key: const ValueKey('forgot-identifier'),
                  label: 'Email or mobile number',
                  controller: _identifier,
                  prefixIcon: AppIcons.user,
                  enabled: !_requested,
                  validator: AuthValidators.identifier,
                ),
              ),
              const Gap(AppSpacing.md),
              if (!_requested)
                AppButton(key: const ValueKey('forgot-send'), label: 'Send reset code', expand: true, size: AppButtonSize.large, isLoading: _busy, onPressed: _busy ? null : _request)
              else
                Form(
                  key: _resetForm,
                  child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                    InlineBanner(tone: AppTone.info, message: _message ?? ''),
                    const Gap(AppSpacing.md),
                    if (_devSecret != null) ...[
                      DevSecretBanner(label: 'Reset code', value: _devSecret!, onUse: () => _secret.text = _devSecret!),
                      const Gap(AppSpacing.md),
                    ],
                    AppTextField(
                      key: const ValueKey('reset-secret'),
                      label: _byPhone ? '6-digit SMS code' : 'Code from the email',
                      controller: _secret,
                      keyboardType: _byPhone ? TextInputType.number : TextInputType.text,
                      inputFormatters: _byPhone ? [FilteringTextInputFormatter.digitsOnly, LengthLimitingTextInputFormatter(6)] : null,
                      validator: _byPhone ? AuthValidators.otp : (v) => AuthValidators.required(v, 'the code'),
                    ),
                    const Gap(AppSpacing.md),
                    PasswordField(
                      fieldKey: const ValueKey('reset-password'),
                      label: 'New password',
                      controller: _password,
                      validator: (v) => AuthValidators.newPassword(v, minLength: minLength),
                    ),
                    const Gap(AppSpacing.md),
                    AppButton(key: const ValueKey('reset-submit'), label: 'Set new password', expand: true, size: AppButtonSize.large, isLoading: _busy, onPressed: _busy ? null : _reset),
                  ]),
                ),
            ]),
    );
  }
}
