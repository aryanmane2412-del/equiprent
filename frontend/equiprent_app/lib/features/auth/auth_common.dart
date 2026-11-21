import 'package:flutter/material.dart';
import '../../core/utils/validators.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/auth/auth_api.dart';
import '../../core/auth/auth_models.dart';
import '../../core/network/api_exception.dart';
import '../../core/widgets/widgets.dart';

/// Routes of the authentication screens.
abstract final class AuthRoutes {
  static const signIn = '/sign-in';
  static const register = '/register';
  static const verify = '/verify';
  static const forgotPassword = '/forgot-password';
  static const verifyEmail = '/verify-email';
  static const account = '/account';
  static const loading = '/loading';

  /// Reachable while signed out.
  static const public = {signIn, register, verify, forgotPassword, verifyEmail};
}

/// Sign-in methods offered by the server. Falls back to password-only when
/// the server cannot be reached, so the screen still renders.
final authCapabilitiesProvider = FutureProvider<AuthCapabilities>((ref) async {
  try {
    return await ref.watch(authApiProvider).capabilities();
  } on ApiException {
    return const AuthCapabilities();
  }
});

/// Details carried from registration to the verification screen.
@immutable
class PendingVerification {
  const PendingVerification({
    required this.phone,
    this.email,
    this.message,
    this.devOtp,
    this.devEmailToken,
  });

  final String phone;
  final String? email;
  final String? message;
  final String? devOtp;
  final String? devEmailToken;
}

class PendingVerificationController extends Notifier<PendingVerification?> {
  @override
  PendingVerification? build() => null;

  void set(PendingVerification? value) => state = value;
}

final pendingVerificationProvider =
    NotifierProvider<PendingVerificationController, PendingVerification?>(
      PendingVerificationController.new,
    );

/// Client-side checks mirror the server rules for fast feedback; the server
/// re-validates everything.
abstract final class AuthValidators {
  static final _email = RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$');

  /// Normalises a mobile number to E.164 (10-digit Indian numbers get +91).
  static String? normalisePhone(String raw) => Validators.normalisePhone(raw);

  static String? phone(String? v) => Validators.phone(v);

  static String? email(String? v) =>
      _email.hasMatch((v ?? '').trim()) ? null : 'Enter a valid email address';

  static String? identifier(String? v) {
    final s = (v ?? '').trim();
    if (s.contains('@')) return email(s);
    return normalisePhone(s) == null
        ? 'Enter your email or mobile number'
        : null;
  }

  /// Normalises an identifier: emails lower-cased, phones to E.164.
  static String normaliseIdentifier(String raw) {
    final s = raw.trim();
    if (s.contains('@')) return s.toLowerCase();
    return normalisePhone(s) ?? s;
  }

  static String? newPassword(String? v, {int minLength = 10}) {
    final s = v ?? '';
    if (s.length < minLength) return 'Use at least $minLength characters';
    if (s.length > 128) return 'Use at most 128 characters';
    final letters = RegExp(r'[A-Za-z]').hasMatch(s);
    final others = RegExp(r'[^A-Za-z]').hasMatch(s);
    if (!letters || !others) return 'Mix letters with numbers or symbols';
    return null;
  }

  /// Mirrors the backend's personal-information password check so users see
  /// the issue beside the password field before submitting registration.
  static String? passwordPersonalInfo(
    String? value, {
    String? fullName,
    String? email,
    String? phone,
  }) {
    final password = (value ?? '').toLowerCase();
    final nameParts = (fullName ?? '')
        .toLowerCase()
        .split(RegExp(r'[^a-z0-9]+'))
        .where((part) => part.length >= 3);
    final emailName = (email ?? '').split('@').first.toLowerCase();
    final phoneDigits = (phone ?? '').replaceAll(RegExp(r'\D'), '');
    if (nameParts.any(password.contains) ||
        (emailName.length >= 4 && password.contains(emailName)) ||
        (phoneDigits.length >= 4 && password.contains(phoneDigits))) {
      return 'Must not contain your name, email or phone number';
    }
    return null;
  }

  static String? required(String? v, String what) =>
      (v ?? '').trim().isEmpty ? 'Enter $what' : null;

  static String? otp(String? v) =>
      RegExp(r'^[0-9]{6}$').hasMatch((v ?? '').trim())
      ? null
      : 'Enter the 6-digit code';
}

/// A field-level error from the server, if any, for [field].
String? serverFieldError(Object? error, String field) {
  if (error is! ApiException) return null;
  for (final issue in error.fieldIssues) {
    if (issue.field == field) return issue.message ?? 'Invalid value';
  }
  return null;
}

/// User-facing text for an auth error.
String authErrorMessage(Object error) {
  if (error is ApiException) {
    return switch (error.code) {
      'INVALID_CREDENTIALS' =>
        'The email/mobile number or password is incorrect.',
      'VERIFICATION_REQUIRED' =>
        'Verify this email or mobile number first. Use “Mobile OTP” to verify your phone.',
      'ACCOUNT_SUSPENDED' =>
        'This account is suspended. Please contact support.',
      'ACCOUNT_TEMPORARILY_LOCKED' =>
        'Too many attempts. Please wait a few minutes and try again.',
      'OTP_INVALID' => 'That code is incorrect or has expired.',
      'OTP_ATTEMPTS_EXCEEDED' => 'Too many wrong codes. Request a new code.',
      'OTP_RESEND_TOO_SOON' =>
        'Please wait a moment before requesting another code.',
      'OTP_UNAVAILABLE' =>
        'Mobile OTP is not available yet. Sign in with your email and password.',
      'TOKEN_INVALID' => 'That link or code is invalid or has expired.',
      'CURRENT_PASSWORD_INCORRECT' => 'Your current password is incorrect.',
      _ =>
        error.fieldIssues.isNotEmpty
            ? (error.fieldIssues.first.message ?? error.message)
            : error.message,
    };
  }
  return 'Something went wrong. Please try again.';
}

/// Branded single-column layout for the auth screens.
class AuthScaffold extends StatelessWidget {
  const AuthScaffold({
    super.key,
    required this.title,
    required this.child,
    this.subtitle,
    this.onBack,
  });

  final String title;
  final String? subtitle;
  final Widget child;
  final VoidCallback? onBack;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final wide = context.windowSize.atLeast(WindowSize.expanded);
    final form = SafeArea(
      child: Center(
        child: SingleChildScrollView(
          padding: const EdgeInsets.all(AppSpacing.md),
          child: ConstrainedBox(
            constraints: const BoxConstraints(maxWidth: 440),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    if (onBack != null)
                      AppIconButton(
                        icon: AppIcons.arrowLeft,
                        tooltip: 'Back',
                        onPressed: onBack,
                      ),
                    if (onBack != null) const Gap(AppSpacing.sm),
                    if (!wide)
                      const Flexible(
                        child: FittedBox(
                          fit: BoxFit.scaleDown,
                          alignment: Alignment.centerLeft,
                          child: BrandLogo(),
                        ),
                      ),
                  ],
                ),
                const Gap(AppSpacing.lg),
                Text(title, style: theme.textTheme.headlineSmall),
                if (subtitle != null) ...[
                  const Gap(AppSpacing.xs),
                  Text(
                    subtitle!,
                    style: theme.textTheme.bodyMedium?.copyWith(
                      color: AppColors.slate600,
                    ),
                  ),
                ],
                const Gap(AppSpacing.lg),
                child,
              ],
            ),
          ),
        ),
      ),
    );
    if (!wide) return Scaffold(backgroundColor: AppColors.canvas, body: form);
    return Scaffold(
      backgroundColor: AppColors.canvas,
      body: Row(
        children: [
          Expanded(
            flex: 5,
            child: Container(
              padding: const EdgeInsets.all(AppSpacing.xxl),
              decoration: const BoxDecoration(
                gradient: LinearGradient(
                  begin: Alignment.topLeft,
                  end: Alignment.bottomRight,
                  colors: [AppColors.navy950, AppColors.navy700],
                ),
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  const BrandLogo(onDark: true),
                  const Spacer(),
                  Text(
                    'Heavy equipment,\nrented the smart way.',
                    style: theme.textTheme.displaySmall?.copyWith(
                      color: AppColors.white,
                    ),
                  ),
                  const Gap(AppSpacing.md),
                  Text(
                    'Verified machines and operators, protected payments and live tracking.',
                    style: theme.textTheme.bodyLarge?.copyWith(
                      color: AppColors.navy100,
                    ),
                  ),
                  const Spacer(),
                ],
              ),
            ),
          ),
          Expanded(flex: 6, child: form),
        ],
      ),
    );
  }
}

/// Shows a development-simulation secret (never shown in production, where
/// the server does not return it).
class DevSecretBanner extends StatelessWidget {
  const DevSecretBanner({
    super.key,
    required this.label,
    required this.value,
    this.onUse,
  });

  final String label;
  final String value;
  final VoidCallback? onUse;

  @override
  Widget build(BuildContext context) => InlineBanner(
    key: const ValueKey('dev-secret'),
    tone: AppTone.warning,
    title: 'Development simulation',
    message: '$label: $value (no SMS/email was sent)',
    action: onUse == null
        ? null
        : TextButton(onPressed: onUse, child: const Text('Use it')),
  );
}

/// Development simulation only: the seeded demo accounts (backend
/// `go run ./cmd/seed`), so a demo can sign in without SMS or email.
class DemoAccountsCard extends StatelessWidget {
  const DemoAccountsCard({super.key, required this.onPick});

  /// Fills the sign-in form with (identifier, password).
  final void Function(String identifier, String password) onPick;

  static const password = 'Tractor-Rental-2026';
  static const accounts = [
    ('Customer', 'customer@equiprent.dev'),
    ('Equipment owner', 'owner.pune@equiprent.dev'),
    ('Driver (Off Duty)', 'driver@equiprent.dev'),
    ('Admin', 'admin@equiprent.dev'),
  ];

  @override
  Widget build(BuildContext context) => Column(
    key: const ValueKey('demo-accounts'),
    crossAxisAlignment: CrossAxisAlignment.stretch,
    children: [
      const InlineBanner(
        tone: AppTone.warning,
        title: 'Demo mode: no SMS or email is sent',
        message:
            'Verification and reset codes appear on screen instead. Tap a demo account to fill the form (password $password).',
      ),
      const SizedBox(height: AppSpacing.xs),
      Wrap(
        spacing: AppSpacing.xs,
        runSpacing: AppSpacing.xs,
        children: [
          for (final (label, email) in accounts)
            ActionChip(
              key: ValueKey('demo-$email'),
              label: Text(label),
              onPressed: () => onPick(email, password),
            ),
        ],
      ),
    ],
  );
}

/// Inline error banner for a failed auth request.
class AuthErrorBanner extends StatelessWidget {
  const AuthErrorBanner({super.key, required this.error});

  final Object? error;

  @override
  Widget build(BuildContext context) {
    if (error == null) return const SizedBox.shrink();
    return Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.md),
      child: InlineBanner(
        key: const ValueKey('auth-error'),
        tone: AppTone.danger,
        message: authErrorMessage(error!),
      ),
    );
  }
}

/// Password field with a show/hide toggle.
class PasswordField extends StatefulWidget {
  const PasswordField({
    super.key,
    required this.controller,
    required this.label,
    this.validator,
    this.helper,
    this.onChanged,
    this.onSubmitted,
    this.fieldKey,
  });

  final TextEditingController controller;
  final String label;
  final String? helper;
  final FormFieldValidator<String>? validator;
  final ValueChanged<String>? onChanged;
  final ValueChanged<String>? onSubmitted;
  final Key? fieldKey;

  @override
  State<PasswordField> createState() => _PasswordFieldState();
}

class _PasswordFieldState extends State<PasswordField> {
  bool _hidden = true;

  @override
  Widget build(BuildContext context) => AppTextField(
    key: widget.fieldKey,
    label: widget.label,
    controller: widget.controller,
    obscureText: _hidden,
    helper: widget.helper,
    prefixIcon: AppIcons.lock,
    validator: widget.validator,
    onChanged: widget.onChanged,
    onSubmitted: widget.onSubmitted,
    textInputAction: TextInputAction.done,
    suffix: IconButton(
      tooltip: _hidden ? 'Show password' : 'Hide password',
      icon: Icon(_hidden ? AppIcons.eye : AppIcons.close, size: 18),
      onPressed: () => setState(() => _hidden = !_hidden),
    ),
  );
}
