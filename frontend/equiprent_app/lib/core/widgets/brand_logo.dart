import 'package:flutter/material.dart';

import '../theme/app_tokens.dart';

/// The EQUIPRENT logo (assets/images/logo.png) on a white tile so the navy
/// lettering stays readable on dark navigation and in dark surroundings.
class BrandLogo extends StatelessWidget {
  const BrandLogo({super.key, this.showTagline = true, this.compact = false, this.onDark = false});

  static const name = 'EQUIPRENT';
  static const tagline = 'Rent Smart. Build Better.';

  // Kept for call-site compatibility; the logo image already carries its own tagline.
  final bool showTagline;
  final bool compact;
  final bool onDark;

  @override
  Widget build(BuildContext context) {
    return Semantics(
      label: '$name. $tagline',
      image: true,
      excludeSemantics: true,
      child: Container(
        padding: EdgeInsets.symmetric(horizontal: compact ? 6 : 10, vertical: compact ? 3 : 6),
        decoration: BoxDecoration(
          color: AppColors.white,
          borderRadius: BorderRadius.circular(AppRadius.sm + 2),
        ),
        child: Image.asset(
          'assets/images/logo.png',
          height: compact ? 30 : 56,
          fit: BoxFit.contain,
          errorBuilder: (_, _, _) => Text(name, style: const TextStyle(fontWeight: FontWeight.w800, color: AppColors.navy900)),
        ),
      ),
    );
  }
}
