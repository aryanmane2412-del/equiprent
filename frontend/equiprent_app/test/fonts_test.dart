import 'package:equiprent_app/core/theme/app_theme.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'every text style falls back to Inter, which has the rupee sign the main font lacks',
    () {
      final t = AppTheme.light().textTheme;
      for (final style in [
        t.bodyMedium,
        t.titleMedium,
        t.labelLarge,
        t.headlineLarge,
        t.displaySmall,
      ]) {
        expect(
          style?.fontFamilyFallback,
          contains('Inter'),
          reason: '${style?.fontFamily}',
        );
      }
    },
  );

  test(
    'the monospace style uses Source Code Pro (with the Inter fallback)',
    () {
      final s = AppTypography.mono(AppTheme.light().textTheme.bodySmall);
      expect(s.fontFamily, 'SourceCodePro');
      expect(s.fontFamilyFallback, contains('Inter'));
    },
  );
}
