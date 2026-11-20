import 'package:flutter/material.dart';

/// EQUIPRENT design tokens. "Rent Smart. Build Better."
///
/// Colour roles (see docs/DESIGN_SYSTEM.md):
/// - Navy  — brand, primary actions, navigation, headings.
/// - Orange — the ONE most important call to action on a screen
///   (Book now, Add equipment) and rare highlights. Never for errors.
/// - Green — success, money received, "available", completed.
/// - Slate neutrals — text, borders, canvas.
abstract final class AppColors {
  // Navy (brand)
  static const navy950 = Color(0xFF0A1628);
  static const navy900 = Color(0xFF0F1F3A);
  static const navy800 = Color(0xFF15294B);
  static const navy700 = Color(0xFF1C3A6B);
  static const navy600 = Color(0xFF24508F);
  static const navy500 = Color(0xFF2F64B0);
  static const navy200 = Color(0xFFC7D5EA);
  static const navy100 = Color(0xFFE3EAF5);
  static const navy50 = Color(0xFFF2F5FA);

  // Orange (accent / primary CTA)
  static const orange700 = Color(0xFFC2410C);
  static const orange600 = Color(0xFFF59E0B);
  static const orange500 = Color(0xFFFFAB10);
  static const orange100 = Color(0xFFFFEDD5);
  static const orange50 = Color(0xFFFFF7ED);

  // Green (success / money / available)
  static const green700 = Color(0xFF15803D);
  static const green600 = Color(0xFF16A34A);
  static const green100 = Color(0xFFDCFCE7);
  static const green50 = Color(0xFFF0FDF4);

  // Neutrals (slate)
  static const white = Color(0xFFFFFFFF);
  static const slate50 = Color(0xFFF8FAFC);
  static const slate100 = Color(0xFFF1F5F9);
  static const slate200 = Color(0xFFE2E8F0);
  static const slate300 = Color(0xFFCBD5E1);
  static const slate400 = Color(0xFF94A3B8);
  static const slate500 = Color(0xFF64748B);
  static const slate600 = Color(0xFF475569);
  static const slate700 = Color(0xFF334155);
  static const slate800 = Color(0xFF1E293B);
  static const slate900 = Color(0xFF0F172A);

  // Semantic
  static const red700 = Color(0xFFB91C1C);
  static const red600 = Color(0xFFDC2626);
  static const red100 = Color(0xFFFEE2E2);
  static const amber700 = Color(0xFFB45309);
  static const amber600 = Color(0xFFD97706);
  static const amber100 = Color(0xFFFEF3C7);
  static const blue700 = Color(0xFF1D4ED8);
  static const blue100 = Color(0xFFDBEAFE);

  /// App canvas behind cards.
  static const canvas = Color(0xFFF5F7FB);

  // Role aliases used by components.
  static const brand = navy700;
  static const accent = orange600;
  static const success = green700;
  static const warning = amber700;
  static const danger = red600;
  static const info = blue700;
  static const textPrimary = slate900;
  static const textSecondary = slate600;
  static const textMuted = slate500;
  static const border = slate200;
  static const borderStrong = slate300;

  /// Single-series chart marks (one hue; the current period is emphasised).
  /// A multi-series categorical palette must be validated before it is added.
  static const chartMark = navy500;
  static const chartMarkEmphasis = navy800;
  static const chartGrid = slate200;
}

/// 4-pt spacing scale.
abstract final class AppSpacing {
  static const double xxs = 2;
  static const double xs = 4;
  static const double sm = 8;
  static const double ms = 12;
  static const double md = 16;
  static const double lg = 24;
  static const double xl = 32;
  static const double xxl = 48;
  static const double xxxl = 64;
}

abstract final class AppRadius {
  static const double xs = 6;
  static const double sm = 8;
  static const double md = 12;
  static const double lg = 16;
  static const double xl = 24;
  static const double full = 999;

  static const BorderRadius card = BorderRadius.all(Radius.circular(md));
  static const BorderRadius control = BorderRadius.all(Radius.circular(10));
  static const BorderRadius chip = BorderRadius.all(Radius.circular(sm));
  static const BorderRadius sheet = BorderRadius.vertical(top: Radius.circular(20));
}

/// Navy-tinted soft elevation. Cards use borders by default; shadows are for
/// floating surfaces (menus, sheets, sticky bars, hovered cards).
abstract final class AppShadows {
  static const sm = [BoxShadow(color: Color(0x0F0F1F3A), blurRadius: 4, offset: Offset(0, 1))];
  static const md = [
    BoxShadow(color: Color(0x140F1F3A), blurRadius: 12, offset: Offset(0, 4)),
    BoxShadow(color: Color(0x0A0F1F3A), blurRadius: 2, offset: Offset(0, 1)),
  ];
  static const lg = [BoxShadow(color: Color(0x1F0F1F3A), blurRadius: 32, offset: Offset(0, 12))];
}

abstract final class AppMotion {
  static const fast = Duration(milliseconds: 150);
  static const medium = Duration(milliseconds: 250);
  static const slow = Duration(milliseconds: 400);
  static const emphasized = Curves.easeOutCubic;
  static const standard = Curves.easeInOutCubic;
}

/// Minimum interactive size. Larger than Material's default because
/// operators often use the app outdoors and with gloves.
abstract final class AppSizes {
  static const double minTapTarget = 48;
  static const double buttonSm = 36;
  static const double buttonMd = 48;
  static const double buttonLg = 56;
  static const double sideNavWidth = 248;
  static const double sideNavCollapsed = 80;
}
