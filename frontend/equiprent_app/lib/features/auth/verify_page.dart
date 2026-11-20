import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_api.dart';
import '../../core/auth/auth_controller.dart';
import '../../core/widgets/widgets.dart';
import 'auth_common.dart';

/// After registration: prove the phone with the SMS code (which also signs
/// in), or the email with its link/code when OTP is unavailable.
class VerifyPage extends ConsumerStatefulWidget {
  const VerifyPage({super.key});

  @override
  ConsumerState<VerifyPage> createState() => _VerifyPageState();
}

class _VerifyPageState extends ConsumerState<VerifyPage> {
  final _form = GlobalKey<FormState>();
  final _code = TextEditingController();
  final _emailToken = TextEditingController();
  bool _busy = false;
  bool _emailDone = false;
  Object? _error;
  String? _resentCode;

  @override
  void dispose() {
    _code.dispose();
    _emailToken.dispose();
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

  @override
  Widget build(BuildContext context) {
    final pending = ref.watch(pendingVerificationProvider);
    final caps = ref.watch(authCapabilitiesProvider).value;
    if (pending == null) {
      return AuthScaffold(
        title: 'Verify your account',
        onBack: () => context.go(AuthRoutes.signIn),
        child: AppButton(label: 'Go to sign in', onPressed: () => context.go(AuthRoutes.signIn)),
      );
    }
    final otp = caps?.mobileOtp ?? pending.devOtp != null;
    final devCode = _resentCode ?? pending.devOtp;

    return AuthScaffold(
      title: 'Verify your account',
      subtitle: pending.message,
      onBack: () => context.go(AuthRoutes.signIn),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        AuthErrorBanner(error: _error),
        if (otp)
          Form(
            key: _form,
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              Text('Enter the code sent to ${pending.phone}', style: Theme.of(context).textTheme.titleSmall),
              const Gap(AppSpacing.sm),
              if (devCode != null) ...[
                DevSecretBanner(label: 'Your code', value: devCode, onUse: () => _code.text = devCode),
                const Gap(AppSpacing.md),
              ],
              AppTextField(
                key: const ValueKey('verify-code'),
                label: '6-digit code',
                controller: _code,
                keyboardType: TextInputType.number,
                inputFormatters: [FilteringTextInputFormatter.digitsOnly, LengthLimitingTextInputFormatter(6)],
                validator: AuthValidators.otp,
              ),
              const Gap(AppSpacing.md),
              AppButton(
                key: const ValueKey('verify-phone'),
                label: 'Verify and continue',
                expand: true,
                size: AppButtonSize.large,
                isLoading: _busy,
                onPressed: _busy
                    ? null
                    : () {
                        if (!(_form.currentState?.validate() ?? false)) return;
                        _run(() async {
                          await ref.read(authControllerProvider.notifier).verifyOtp(pending.phone, _code.text.trim());
                          ref.read(pendingVerificationProvider.notifier).set(null);
                          if (context.mounted) context.go('/');
                        });
                      },
              ),
              TextButton(
                onPressed: _busy
                    ? null
                    : () => _run(() async {
                          final s = await ref.read(authControllerProvider.notifier).requestOtp(pending.phone);
                          setState(() => _resentCode = s.code);
                        }),
                child: const Text('Send a new code'),
              ),
            ]),
          )
        else
          const InlineBanner(
            tone: AppTone.info,
            message: 'Mobile OTP is not available yet. Verify your email below, then sign in with your email and password.',
          ),
        if (pending.email != null) ...[
          const Gap(AppSpacing.lg),
          const Divider(),
          const Gap(AppSpacing.md),
          Text('Verify ${pending.email}', style: Theme.of(context).textTheme.titleSmall),
          const Gap(AppSpacing.xs),
          Text('Paste the code from the verification email. You can also do this later from your account.',
              style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate600)),
          const Gap(AppSpacing.sm),
          if (_emailDone)
            const InlineBanner(key: ValueKey('email-verified'), tone: AppTone.success, message: 'Email verified. You can now sign in with it.')
          else ...[
            if (pending.devEmailToken != null) ...[
              DevSecretBanner(label: 'Email code', value: pending.devEmailToken!, onUse: () => _emailToken.text = pending.devEmailToken!),
              const Gap(AppSpacing.sm),
            ],
            AppTextField(key: const ValueKey('email-token'), label: 'Email verification code', controller: _emailToken, prefixIcon: AppIcons.mail),
            const Gap(AppSpacing.sm),
            AppButton(
              key: const ValueKey('verify-email'),
              label: 'Verify email',
              variant: AppButtonVariant.secondary,
              expand: true,
              onPressed: _busy
                  ? null
                  : () => _run(() async {
                        if (_emailToken.text.trim().isEmpty) throw StateError('empty');
                        await ref.read(authApiProvider).verifyEmail(_emailToken.text.trim());
                        setState(() => _emailDone = true);
                      }),
            ),
          ],
          const Gap(AppSpacing.md),
          TextButton(onPressed: () => context.go(AuthRoutes.signIn), child: const Text('Back to sign in')),
        ],
      ]),
    );
  }
}
