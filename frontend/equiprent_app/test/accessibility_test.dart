import 'dart:ui' show Color;
import 'dart:convert';
import 'dart:io';
import 'dart:math' as math;

import 'package:equiprent_app/core/theme/app_tokens.dart';
import 'package:flutter_test/flutter_test.dart';

double _lum(Color c) {
  double f(double v) => v <= 0.03928 ? v / 12.92 : math.pow((v + 0.055) / 1.055, 2.4).toDouble();
  return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
}

double contrast(Color a, Color b) {
  final x = _lum(a), y = _lum(b);
  return (math.max(x, y) + 0.05) / (math.min(x, y) + 0.05);
}

void main() {
  test('accent (amber) buttons use text that meets WCAG AA 4.5:1', () {
    expect(contrast(AppColors.navy950, AppColors.orange600), greaterThanOrEqualTo(4.5));
  });
  test('hint and helper greys meet AA on white', () {
    expect(contrast(AppColors.slate600, AppColors.white), greaterThanOrEqualTo(4.5));
    expect(contrast(AppColors.slate500, AppColors.white), greaterThanOrEqualTo(4.5));
  });
  test('en, hi and mr localisation files have identical keys', () {
    Set<String> keys(String l) => (jsonDecode(File('lib/l10n/app_$l.arb').readAsStringSync()) as Map<String, dynamic>)
        .keys.where((k) => !k.startsWith('@')).toSet();
    expect(keys('hi'), keys('en'));
    expect(keys('mr'), keys('en'));
  });
}
