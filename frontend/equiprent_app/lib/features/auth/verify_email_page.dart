import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_api.dart';
import '../../core/auth/auth_controller.dart';
import '../../core/widgets/widgets.dart';
import 'auth_common.dart';

/// Opens from the email link (`/verify-email?token=...`) or lets the user
/// paste the code. Works signed in or out.
class VerifyEmailPage extends ConsumerStatefulWidget {
  const VerifyEmailPage({super.key, this.token});

  final String? token;

  @override
  ConsumerState<VerifyEmailPage> createState() => _VerifyEmailPageState();
}

class _VerifyEmailPageState extends ConsumerState<VerifyEmailPage> {
  late final _token = TextEditingController(text: widget.token ?? '');
  late final _email = TextEditingController(text: ref.read(authControllerProvider).user?.email ?? '');
  bool _busy = false;
  bool _done = false;
  Object? _error;

  @override
  void initState() {
    super.initState();
    if ((widget.token ?? '').isNotEmpty && _email.text.isNotEmpty) WidgetsBinding.instance.addPostFrameCallback((_) => _verify());
  }

  @override
  void dispose() {
    _token.dispose();
    _email.dispose();
    super.dispose();
  }

  Future<void> _verify() async {
    if (_token.text.trim().isEmpty || _email.text.trim().isEmpty) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(authApiProvider).verifyEmail(_token.text.trim(), _email.text.trim());
      if (ref.read(authControllerProvider).isSignedIn) await ref.read(authControllerProvider.notifier).reloadUser();
      if (mounted) setState(() => _done = true);
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final signedIn = ref.watch(authControllerProvider).isSignedIn;
    return AuthScaffold(
      title: 'Verify your email',
      onBack: () => context.go(signedIn ? AuthRoutes.account : AuthRoutes.signIn),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
        AuthErrorBanner(error: _error),
        if (_done) ...[
          const InlineBanner(key: ValueKey('email-verified'), tone: AppTone.success, message: 'Your email address is verified.'),
          const Gap(AppSpacing.md),
          AppButton(label: signedIn ? 'Back to my account' : 'Sign in', expand: true, onPressed: () => context.go(signedIn ? AuthRoutes.account : AuthRoutes.signIn)),
        ] else ...[
          AppTextField(key: const ValueKey('email-address'), label: 'Email address', controller: _email, prefixIcon: AppIcons.mail),
          const Gap(AppSpacing.sm),
          AppTextField(key: const ValueKey('email-token'), label: 'Verification code', controller: _token, prefixIcon: AppIcons.mail),
          const Gap(AppSpacing.md),
          AppButton(label: 'Verify', expand: true, size: AppButtonSize.large, isLoading: _busy, onPressed: _busy ? null : _verify),
        ],
      ]),
    );
  }
}
