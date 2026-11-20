import 'package:flutter/material.dart';

import 'app_tokens.dart';

/// Semantic colour sets shared by badges, banners, toasts and icon tiles.
enum AppTone {
  neutral(AppColors.slate700, AppColors.slate100, AppColors.slate200),
  brand(AppColors.navy700, AppColors.navy100, AppColors.navy200),
  accent(AppColors.orange700, AppColors.orange100, Color(0xFFFED7AA)),
  info(AppColors.blue700, AppColors.blue100, Color(0xFFBFDBFE)),
  success(AppColors.green700, AppColors.green100, Color(0xFFBBF7D0)),
  warning(AppColors.amber700, AppColors.amber100, Color(0xFFFDE68A)),
  danger(AppColors.red700, AppColors.red100, Color(0xFFFECACA));

  const AppTone(this.foreground, this.background, this.border);

  final Color foreground;
  final Color background;
  final Color border;
}
