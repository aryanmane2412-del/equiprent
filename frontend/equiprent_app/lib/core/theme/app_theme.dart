import 'package:flutter/cupertino.dart' show CupertinoPageTransitionsBuilder;
import 'package:flutter/material.dart';

import 'app_tokens.dart';

/// Type scale (Inter, bundled in assets/fonts). Sizes in logical pixels.
abstract final class AppTypography {
  static const fontFamily = 'Inter';

  static const _tight = -0.4;

  static const textTheme = TextTheme(
    displaySmall: TextStyle(
      fontSize: 32,
      height: 1.2,
      fontWeight: FontWeight.w700,
      letterSpacing: _tight,
    ),
    headlineLarge: TextStyle(
      fontSize: 28,
      height: 1.25,
      fontWeight: FontWeight.w700,
      letterSpacing: _tight,
    ),
    headlineMedium: TextStyle(
      fontSize: 24,
      height: 1.3,
      fontWeight: FontWeight.w700,
      letterSpacing: _tight,
    ),
    headlineSmall: TextStyle(
      fontSize: 20,
      height: 1.3,
      fontWeight: FontWeight.w600,
      letterSpacing: -0.2,
    ),
    titleLarge: TextStyle(
      fontSize: 18,
      height: 1.35,
      fontWeight: FontWeight.w600,
      letterSpacing: -0.1,
    ),
    titleMedium: TextStyle(
      fontSize: 16,
      height: 1.4,
      fontWeight: FontWeight.w600,
    ),
    titleSmall: TextStyle(
      fontSize: 14,
      height: 1.4,
      fontWeight: FontWeight.w600,
    ),
    bodyLarge: TextStyle(
      fontSize: 16,
      height: 1.5,
      fontWeight: FontWeight.w400,
    ),
    bodyMedium: TextStyle(
      fontSize: 14,
      height: 1.45,
      fontWeight: FontWeight.w400,
    ),
    bodySmall: TextStyle(
      fontSize: 12,
      height: 1.4,
      fontWeight: FontWeight.w400,
    ),
    labelLarge: TextStyle(
      fontSize: 14,
      height: 1.3,
      fontWeight: FontWeight.w600,
    ),
    labelMedium: TextStyle(
      fontSize: 12,
      height: 1.3,
      fontWeight: FontWeight.w600,
    ),
    labelSmall: TextStyle(
      fontSize: 11,
      height: 1.3,
      fontWeight: FontWeight.w600,
      letterSpacing: 0.4,
    ),
  );

  /// Use for prices, counts and tables so digits align.
  static const tabular = [FontFeature.tabularFigures()];
}

abstract final class AppTheme {
  static const colorScheme = ColorScheme(
    brightness: Brightness.light,
    primary: AppColors.navy700,
    onPrimary: AppColors.white,
    primaryContainer: AppColors.navy100,
    onPrimaryContainer: AppColors.navy900,
    secondary: AppColors.orange600,
    onSecondary: AppColors.onAccent,
    secondaryContainer: AppColors.orange600,
    onSecondaryContainer: AppColors.onAccent,
    tertiary: AppColors.green700,
    onTertiary: AppColors.white,
    tertiaryContainer: AppColors.green100,
    onTertiaryContainer: Color(0xFF14532D),
    error: AppColors.red600,
    onError: AppColors.white,
    errorContainer: AppColors.red100,
    onErrorContainer: Color(0xFF7F1D1D),
    surface: AppColors.white,
    onSurface: AppColors.slate900,
    onSurfaceVariant: AppColors.slate600,
    surfaceContainerLowest: AppColors.white,
    surfaceContainerLow: AppColors.slate50,
    surfaceContainer: AppColors.slate100,
    surfaceContainerHigh: Color(0xFFEDF1F6),
    surfaceContainerHighest: AppColors.slate200,
    outline: AppColors.slate300,
    outlineVariant: AppColors.slate200,
    inverseSurface: AppColors.navy900,
    onInverseSurface: AppColors.white,
    inversePrimary: AppColors.navy200,
    shadow: Colors.black,
    scrim: Colors.black,
    surfaceTint: Colors.transparent,
  );

  /// The product ships a light theme only for now: it carries the brand
  /// (navy on white) best on bright construction sites. Dark mode is planned.
  static ThemeData light() {
    const scheme = colorScheme;
    final text = AppTypography.textTheme.apply(
      fontFamily: AppTypography.fontFamily,
      bodyColor: AppColors.textPrimary,
      displayColor: AppColors.textPrimary,
    );
    const controlShape = RoundedRectangleBorder(
      borderRadius: AppRadius.control,
    );
    const buttonPadding = EdgeInsets.symmetric(horizontal: AppSpacing.lg - 4);
    const buttonSize = Size(64, AppSizes.buttonMd);

    OutlineInputBorder inputBorder(Color color, [double width = 1]) =>
        OutlineInputBorder(
          borderRadius: AppRadius.control,
          borderSide: BorderSide(color: color, width: width),
        );

    return ThemeData(
      useMaterial3: true,
      colorScheme: scheme,
      fontFamily: AppTypography.fontFamily,
      textTheme: text,
      scaffoldBackgroundColor: AppColors.canvas,
      canvasColor: AppColors.canvas,
      visualDensity: VisualDensity.standard,
      materialTapTargetSize: MaterialTapTargetSize.padded,
      splashFactory: InkSparkle.splashFactory,
      pageTransitionsTheme: const PageTransitionsTheme(
        builders: {
          TargetPlatform.android: FadeForwardsPageTransitionsBuilder(),
          TargetPlatform.iOS: CupertinoPageTransitionsBuilder(),
          TargetPlatform.macOS: FadeForwardsPageTransitionsBuilder(),
          TargetPlatform.windows: FadeForwardsPageTransitionsBuilder(),
          TargetPlatform.linux: FadeForwardsPageTransitionsBuilder(),
        },
      ),
      appBarTheme: AppBarTheme(
        backgroundColor: AppColors.white,
        foregroundColor: AppColors.slate900,
        elevation: 0,
        scrolledUnderElevation: 0,
        surfaceTintColor: Colors.transparent,
        centerTitle: false,
        titleSpacing: AppSpacing.md,
        titleTextStyle: text.titleLarge,
        shape: const Border(bottom: BorderSide(color: AppColors.border)),
      ),
      cardTheme: const CardThemeData(
        elevation: 0,
        margin: EdgeInsets.zero,
        color: AppColors.white,
        shape: RoundedRectangleBorder(
          borderRadius: AppRadius.card,
          side: BorderSide(color: AppColors.border),
        ),
      ),
      filledButtonTheme: FilledButtonThemeData(
        style: FilledButton.styleFrom(
          minimumSize: buttonSize,
          padding: buttonPadding,
          shape: controlShape,
          textStyle: text.labelLarge,
          backgroundColor: AppColors.navy700,
          foregroundColor: AppColors.white,
          disabledBackgroundColor: AppColors.slate200,
          disabledForegroundColor: AppColors.slate500,
        ),
      ),
      outlinedButtonTheme: OutlinedButtonThemeData(
        style: OutlinedButton.styleFrom(
          minimumSize: buttonSize,
          padding: buttonPadding,
          shape: controlShape,
          textStyle: text.labelLarge,
          foregroundColor: AppColors.navy700,
          side: const BorderSide(color: AppColors.slate300),
          backgroundColor: AppColors.white,
        ),
      ),
      textButtonTheme: TextButtonThemeData(
        style: TextButton.styleFrom(
          minimumSize: const Size(48, 40),
          shape: controlShape,
          textStyle: text.labelLarge,
          foregroundColor: AppColors.navy700,
        ),
      ),
      iconButtonTheme: IconButtonThemeData(
        style: IconButton.styleFrom(
          foregroundColor: AppColors.slate700,
          minimumSize: const Size(44, 44),
        ),
      ),
      inputDecorationTheme: InputDecorationTheme(
        filled: true,
        fillColor: AppColors.white,
        isDense: false,
        contentPadding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.md,
          vertical: 14,
        ),
        hintStyle: text.bodyMedium?.copyWith(color: AppColors.slate600),
        labelStyle: text.bodyMedium?.copyWith(color: AppColors.slate600),
        floatingLabelStyle: text.bodyMedium?.copyWith(
          color: AppColors.navy700,
          fontWeight: FontWeight.w600,
        ),
        helperStyle: text.bodySmall?.copyWith(color: AppColors.slate500),
        prefixIconColor: AppColors.slate500,
        suffixIconColor: AppColors.slate500,
        border: inputBorder(AppColors.slate300),
        enabledBorder: inputBorder(AppColors.slate300),
        focusedBorder: inputBorder(AppColors.navy600, 1.6),
        errorBorder: inputBorder(AppColors.red600),
        focusedErrorBorder: inputBorder(AppColors.red600, 1.6),
        disabledBorder: inputBorder(AppColors.slate200),
      ),
      chipTheme: ChipThemeData(
        shape: const RoundedRectangleBorder(borderRadius: AppRadius.chip),
        side: const BorderSide(color: AppColors.slate300),
        backgroundColor: AppColors.white,
        selectedColor: AppColors.navy100,
        checkmarkColor: AppColors.navy700,
        labelStyle: text.labelLarge?.copyWith(color: AppColors.slate700),
        padding: const EdgeInsets.symmetric(
          horizontal: AppSpacing.xs,
          vertical: AppSpacing.xs,
        ),
      ),
      dialogTheme: DialogThemeData(
        backgroundColor: AppColors.white,
        surfaceTintColor: Colors.transparent,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(AppRadius.lg)),
        ),
        titleTextStyle: text.titleLarge,
        contentTextStyle: text.bodyMedium?.copyWith(color: AppColors.slate600),
      ),
      bottomSheetTheme: const BottomSheetThemeData(
        backgroundColor: AppColors.white,
        surfaceTintColor: Colors.transparent,
        showDragHandle: true,
        dragHandleColor: AppColors.slate300,
        shape: RoundedRectangleBorder(borderRadius: AppRadius.sheet),
        constraints: BoxConstraints(maxWidth: 640),
      ),
      snackBarTheme: SnackBarThemeData(
        behavior: SnackBarBehavior.floating,
        backgroundColor: AppColors.navy900,
        contentTextStyle: text.bodyMedium?.copyWith(color: AppColors.white),
        actionTextColor: AppColors.orange500,
        shape: const RoundedRectangleBorder(borderRadius: AppRadius.control),
        width: null,
      ),
      navigationBarTheme: NavigationBarThemeData(
        backgroundColor: AppColors.white,
        surfaceTintColor: Colors.transparent,
        indicatorColor: AppColors.orange100,
        height: 68,
        elevation: 0,
        labelBehavior: NavigationDestinationLabelBehavior.alwaysShow,
        labelTextStyle: WidgetStateProperty.resolveWith(
          (s) => text.labelSmall?.copyWith(
            letterSpacing: 0,
            color: s.contains(WidgetState.selected)
                ? AppColors.orange700
                : AppColors.slate500,
          ),
        ),
        iconTheme: WidgetStateProperty.resolveWith(
          (s) => IconThemeData(
            size: 22,
            color: s.contains(WidgetState.selected)
                ? AppColors.orange700
                : AppColors.slate500,
          ),
        ),
      ),
      tabBarTheme: TabBarThemeData(
        labelColor: AppColors.navy700,
        unselectedLabelColor: AppColors.slate500,
        labelStyle: text.labelLarge,
        unselectedLabelStyle: text.labelLarge,
        indicatorColor: AppColors.navy700,
        indicatorSize: TabBarIndicatorSize.label,
        dividerColor: AppColors.border,
      ),
      dataTableTheme: DataTableThemeData(
        headingRowColor: const WidgetStatePropertyAll(AppColors.slate50),
        headingTextStyle: text.labelMedium?.copyWith(color: AppColors.slate600),
        dataTextStyle: text.bodyMedium,
        dividerThickness: 1,
        horizontalMargin: AppSpacing.md,
        columnSpacing: AppSpacing.lg,
        headingRowHeight: 44,
        dataRowMinHeight: 52,
        dataRowMaxHeight: 60,
      ),
      dividerTheme: const DividerThemeData(
        color: AppColors.border,
        space: 1,
        thickness: 1,
      ),
      progressIndicatorTheme: const ProgressIndicatorThemeData(
        color: AppColors.navy700,
        linearTrackColor: AppColors.navy100,
        circularTrackColor: AppColors.navy100,
      ),
      tooltipTheme: TooltipThemeData(
        decoration: const BoxDecoration(
          color: AppColors.navy900,
          borderRadius: BorderRadius.all(Radius.circular(6)),
        ),
        textStyle: text.bodySmall?.copyWith(color: AppColors.white),
        waitDuration: const Duration(milliseconds: 400),
      ),
      switchTheme: SwitchThemeData(
        thumbColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected)
              ? AppColors.white
              : AppColors.slate400,
        ),
        trackColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected)
              ? AppColors.navy700
              : AppColors.slate200,
        ),
        trackOutlineColor: const WidgetStatePropertyAll(Colors.transparent),
      ),
      checkboxTheme: CheckboxThemeData(
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(4)),
        ),
        fillColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected) ? AppColors.navy700 : null,
        ),
      ),
      radioTheme: RadioThemeData(
        fillColor: WidgetStateProperty.resolveWith(
          (s) => s.contains(WidgetState.selected)
              ? AppColors.navy700
              : AppColors.slate400,
        ),
      ),
      listTileTheme: ListTileThemeData(
        iconColor: AppColors.slate600,
        titleTextStyle: text.titleSmall,
        subtitleTextStyle: text.bodySmall?.copyWith(color: AppColors.slate500),
        contentPadding: const EdgeInsets.symmetric(horizontal: AppSpacing.md),
        minVerticalPadding: AppSpacing.ms,
      ),
      segmentedButtonTheme: SegmentedButtonThemeData(
        style: SegmentedButton.styleFrom(
          selectedBackgroundColor: AppColors.navy100,
          selectedForegroundColor: AppColors.navy800,
          foregroundColor: AppColors.slate700,
          side: const BorderSide(color: AppColors.slate300),
          textStyle: text.labelLarge,
        ),
      ),
      popupMenuTheme: PopupMenuThemeData(
        color: AppColors.white,
        surfaceTintColor: Colors.transparent,
        elevation: 8,
        shape: const RoundedRectangleBorder(
          borderRadius: AppRadius.control,
          side: BorderSide(color: AppColors.border),
        ),
        textStyle: text.bodyMedium,
      ),
      badgeTheme: const BadgeThemeData(
        backgroundColor: AppColors.orange600,
        textColor: AppColors.onAccent,
      ),
      floatingActionButtonTheme: const FloatingActionButtonThemeData(
        backgroundColor: AppColors.orange600,
        foregroundColor: AppColors.onAccent,
        elevation: 2,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.all(Radius.circular(AppRadius.lg)),
        ),
      ),
      datePickerTheme: const DatePickerThemeData(
        backgroundColor: AppColors.white,
        surfaceTintColor: Colors.transparent,
        headerBackgroundColor: AppColors.navy800,
        headerForegroundColor: AppColors.white,
      ),
    );
  }
}
