import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_api.dart';
import '../../core/auth/auth_controller.dart';
import '../../core/auth/auth_models.dart';
import '../../core/widgets/widgets.dart';
import 'auth_common.dart';

/// Signed-in devices of the current user.
final deviceSessionsProvider = FutureProvider.autoDispose<List<DeviceSession>>((ref) {
  ref.watch(authControllerProvider.select((s) => s.tokens?.accessToken));
  return ref.read(authControllerProvider.notifier).withToken(ref.read(authApiProvider).sessions);
});

/// Profile, verification, password and device management.
class AccountSecurityPage extends ConsumerStatefulWidget {
  const AccountSecurityPage({super.key});

  @override
  ConsumerState<AccountSecurityPage> createState() => _AccountSecurityPageState();
}

class _AccountSecurityPageState extends ConsumerState<AccountSecurityPage> {
  final _profileForm = GlobalKey<FormState>();
  final _passwordForm = GlobalKey<FormState>();
  late final TextEditingController _name;
  late final TextEditingController _email;
  final _current = TextEditingController();
  final _new = TextEditingController();
  bool _busy = false;
  Object? _error;
  String? _notice;
  String? _devToken;

  @override
  void initState() {
    super.initState();
    final user = ref.read(authControllerProvider).user;
    _name = TextEditingController(text: user?.fullName ?? '');
    _email = TextEditingController(text: user?.email ?? '');
  }

  @override
  void dispose() {
    for (final c in [_name, _email, _current, _new]) {
      c.dispose();
    }
    super.dispose();
  }

  AuthController get _auth => ref.read(authControllerProvider.notifier);
  AuthApi get _api => ref.read(authApiProvider);

  Future<void> _run(Future<void> Function() fn, {String? success}) async {
    setState(() {
      _busy = true;
      _error = null;
      _notice = null;
    });
    try {
      await fn();
      if (mounted && success != null) setState(() => _notice = success);
    } on Object catch (e) {
      if (mounted) setState(() => _error = e);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final user = ref.watch(authControllerProvider).user;
    if (user == null) return const Scaffold(body: LoadingView());
    final theme = Theme.of(context);
    final sessions = ref.watch(deviceSessionsProvider);

    return Scaffold(
      backgroundColor: AppColors.canvas,
      appBar: AppBar(
        title: const Text('Account & security'),
        leading: IconButton(tooltip: 'Back', icon: const Icon(AppIcons.arrowLeft), onPressed: () => context.go('/')),
      ),
      body: SingleChildScrollView(
        padding: const EdgeInsets.all(AppSpacing.md),
        child: Center(
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 720),
            child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
              AuthErrorBanner(error: _error),
              if (_notice != null) ...[
                InlineBanner(key: const ValueKey('account-notice'), tone: AppTone.success, message: _notice!),
                const Gap(AppSpacing.md),
              ],
              if (_devToken != null) ...[
                DevSecretBanner(label: 'Email verification code', value: _devToken!, onUse: () => context.go('${AuthRoutes.verifyEmail}?token=$_devToken')),
                const Gap(AppSpacing.md),
              ],
              AppCard(
                child: Form(
                  key: _profileForm,
                  child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                    Text('Profile', style: theme.textTheme.titleMedium),
                    const Gap(AppSpacing.sm),
                    Wrap(spacing: AppSpacing.sm, runSpacing: AppSpacing.xs, children: [
                      _Chip(ok: user.phoneVerified, label: 'Mobile'),
                      if (user.email != null) _Chip(ok: user.emailVerified, label: 'Email'),
                      for (final r in user.roles) StatusBadge(label: r.label, tone: AppTone.brand),
                    ]),
                    const Gap(AppSpacing.xs),
                    Text([user.phone, ?user.email].join(' · '), style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate600)),
                    const Gap(AppSpacing.md),
                    AppTextField(key: const ValueKey('profile-name'), label: 'Full name', controller: _name, validator: (v) => (v ?? '').trim().length < 2 ? 'Enter your name' : null),
                    const Gap(AppSpacing.md),
                    AppTextField(
                      key: const ValueKey('profile-email'),
                      label: 'Email',
                      controller: _email,
                      keyboardType: TextInputType.emailAddress,
                      helper: 'Changing it requires verifying the new address.',
                      validator: (v) => (v ?? '').trim().isEmpty ? null : AuthValidators.email(v),
                    ),
                    const Gap(AppSpacing.md),
                    Wrap(spacing: AppSpacing.sm, runSpacing: AppSpacing.sm, children: [
                      AppButton(
                        key: const ValueKey('profile-save'),
                        label: 'Save profile',
                        isLoading: _busy,
                        onPressed: _busy
                            ? null
                            : () {
                                if (!(_profileForm.currentState?.validate() ?? false)) return;
                                final email = _email.text.trim();
                                _run(() async {
                                  await _auth.withToken((t) => _api.updateProfile(t, fullName: _name.text.trim(), email: email.isEmpty ? null : email));
                                  await _auth.reloadUser();
                                }, success: 'Profile saved.');
                              },
                      ),
                      if (user.email != null && !user.emailVerified)
                        AppButton(
                          key: const ValueKey('send-email-verification'),
                          label: 'Send verification email',
                          variant: AppButtonVariant.secondary,
                          onPressed: _busy
                              ? null
                              : () => _run(() async {
                                    final s = await _auth.withToken(_api.sendEmailVerification);
                                    setState(() => _devToken = s.token);
                                  }, success: 'Verification email sent.'),
                        ),
                    ]),
                  ]),
                ),
              ),
              const Gap(AppSpacing.md),
              AppCard(
                child: Form(
                  key: _passwordForm,
                  child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                    Text(user.hasPassword ? 'Change password' : 'Set a password', style: theme.textTheme.titleMedium),
                    const Gap(AppSpacing.xs),
                    Text('Your other devices will be signed out.', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate600)),
                    const Gap(AppSpacing.md),
                    if (user.hasPassword) ...[
                      PasswordField(fieldKey: const ValueKey('current-password'), label: 'Current password', controller: _current,
                          validator: (v) => (v ?? '').isEmpty ? 'Enter your current password' : null),
                      const Gap(AppSpacing.md),
                    ],
                    PasswordField(fieldKey: const ValueKey('new-password'), label: 'New password', controller: _new, validator: AuthValidators.newPassword),
                    const Gap(AppSpacing.md),
                    AppButton(
                      key: const ValueKey('change-password'),
                      label: user.hasPassword ? 'Change password' : 'Set password',
                      isLoading: _busy,
                      onPressed: _busy
                          ? null
                          : () {
                              if (!(_passwordForm.currentState?.validate() ?? false)) return;
                              _run(() async {
                                await _auth.withToken((t) => _api.changePassword(t, currentPassword: _current.text, newPassword: _new.text));
                                _current.clear();
                                _new.clear();
                                await _auth.reloadUser();
                                ref.invalidate(deviceSessionsProvider);
                              }, success: 'Password updated. Other devices were signed out.');
                            },
                    ),
                  ]),
                ),
              ),
              const Gap(AppSpacing.md),
              AppCard(
                child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: [
                  Wrap(alignment: WrapAlignment.spaceBetween, crossAxisAlignment: WrapCrossAlignment.center, children: [
                    Text('Signed-in devices', style: theme.textTheme.titleMedium),
                    TextButton(
                      key: const ValueKey('revoke-others'),
                      onPressed: _busy
                          ? null
                          : () => _run(() async {
                                await _auth.withToken(_api.revokeOtherSessions);
                                ref.invalidate(deviceSessionsProvider);
                              }, success: 'Signed out of all other devices.'),
                      child: const Text('Sign out other devices'),
                    ),
                  ]),
                  sessions.when(
                    loading: () => const Padding(padding: EdgeInsets.all(AppSpacing.md), child: Center(child: CircularProgressIndicator())),
                    error: (e, _) => Text(authErrorMessage(e)),
                    data: (list) => Column(children: [
                      for (final s in list)
                        ListTile(
                          key: ValueKey('session-${s.id}'),
                          contentPadding: EdgeInsets.zero,
                          leading: Icon(s.platform == 'web' ? AppIcons.external : AppIcons.phone, size: 20),
                          title: Text('${s.platform ?? 'Unknown device'}${s.current ? ' · this device' : ''}'),
                          subtitle: Text('Signed in ${s.createdAt.toLocal().toString().substring(0, 16)}'),
                          trailing: s.current
                              ? null
                              : TextButton(
                                  onPressed: _busy
                                      ? null
                                      : () => _run(() async {
                                            await _auth.withToken((t) => _api.revokeSession(t, s.id));
                                            ref.invalidate(deviceSessionsProvider);
                                          }, success: 'Device signed out.'),
                                  child: const Text('Sign out'),
                                ),
                        ),
                    ]),
                  ),
                ]),
              ),
              const Gap(AppSpacing.lg),
              AppButton(
                key: const ValueKey('sign-out'),
                label: 'Sign out',
                icon: AppIcons.logout,
                variant: AppButtonVariant.danger,
                onPressed: () => _auth.logout(),
              ),
            ]),
          ),
        ),
      ),
    );
  }
}

class _Chip extends StatelessWidget {
  const _Chip({required this.ok, required this.label});

  final bool ok;
  final String label;

  @override
  Widget build(BuildContext context) =>
      StatusBadge(label: '$label · ${ok ? 'verified' : 'not verified'}', tone: ok ? AppTone.success : AppTone.warning);
}
