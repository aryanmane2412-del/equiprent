import 'package:flutter/material.dart';

import '../mock/mock_data.dart';
import '../theme/app_tokens.dart';
import '../utils/formatters.dart';

/// Photo slot for equipment. Renders [photoUrl] when provided; otherwise a
/// branded placeholder (category glyph on a navy gradient with a blueprint
/// grid) so layouts are designed around real image proportions.
///
/// Real listing photos arrive with owner uploads (presigned S3 URLs).
class EquipmentImage extends StatelessWidget {
  const EquipmentImage({
    super.key,
    required this.categoryId,
    this.photoUrl,
    this.aspectRatio = 4 / 3,
    this.borderRadius = AppRadius.card,
    this.heroTag,
    this.label,
  });

  final String categoryId;
  final String? photoUrl;
  final double aspectRatio;
  final BorderRadius borderRadius;
  final Object? heroTag;
  final String? label;

  static const _tints = {
    'backhoe': (Color(0xFF1C3A6B), Color(0xFF2F64B0)),
    'excavator': (Color(0xFF15294B), Color(0xFF24508F)),
    'crane': (Color(0xFF0F1F3A), Color(0xFF1C3A6B)),
    'roller': (Color(0xFF24508F), Color(0xFF3B78C9)),
    'mixer': (Color(0xFF1E293B), Color(0xFF334155)),
    'tipper': (Color(0xFF15294B), Color(0xFF2F64B0)),
    'telehandler': (Color(0xFF1C3A6B), Color(0xFF475569)),
    'dozer': (Color(0xFF0F1F3A), Color(0xFF24508F)),
  };

  @override
  Widget build(BuildContext context) {
    final category = MockData.category(categoryId);
    final (c1, c2) = _tints[categoryId] ?? (AppColors.navy800, AppColors.navy600);

    Widget placeholder = DecoratedBox(
      decoration: BoxDecoration(
        gradient: LinearGradient(begin: Alignment.topLeft, end: Alignment.bottomRight, colors: [c1, c2]),
      ),
      child: Stack(
        fit: StackFit.expand,
        children: [
          const CustomPaint(painter: _BlueprintPainter()),
          Center(
            child: LayoutBuilder(
              builder: (context, c) => Icon(category.icon, size: (c.biggest.shortestSide * 0.38).clamp(20, 96), color: Colors.white.withValues(alpha: 0.9)),
            ),
          ),
          // Accent stripe referencing hazard/safety orange.
          Positioned(left: 0, right: 0, bottom: 0, child: Container(height: 3, color: AppColors.orange500)),
          if (label != null)
            Positioned(
              left: 8,
              top: 8,
              child: Container(
                padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 3),
                decoration: BoxDecoration(color: Colors.black.withValues(alpha: 0.35), borderRadius: const BorderRadius.all(Radius.circular(6))),
                child: Text(label!, style: Theme.of(context).textTheme.labelSmall?.copyWith(color: Colors.white, letterSpacing: 0)),
              ),
            ),
        ],
      ),
    );

    final Widget image = photoUrl == null
        ? placeholder
        : Image.network(photoUrl!, fit: BoxFit.cover, errorBuilder: (_, _, _) => placeholder);

    Widget result = Semantics(
      image: true,
      label: '${category.name} photo',
      child: ClipRRect(borderRadius: borderRadius, child: AspectRatio(aspectRatio: aspectRatio, child: image)),
    );
    if (heroTag != null) result = Hero(tag: heroTag!, child: result);
    return result;
  }
}

class _BlueprintPainter extends CustomPainter {
  const _BlueprintPainter();

  @override
  void paint(Canvas canvas, Size size) {
    final p = Paint()
      ..color = Colors.white.withValues(alpha: 0.06)
      ..strokeWidth = 1;
    const step = 18.0;
    for (var x = 0.0; x < size.width; x += step) {
      canvas.drawLine(Offset(x, 0), Offset(x, size.height), p);
    }
    for (var y = 0.0; y < size.height; y += step) {
      canvas.drawLine(Offset(0, y), Offset(size.width, y), p);
    }
    canvas.drawCircle(
      Offset(size.width * 0.85, size.height * 0.15),
      size.shortestSide * 0.5,
      Paint()..color = Colors.white.withValues(alpha: 0.05),
    );
  }

  @override
  bool shouldRepaint(covariant CustomPainter oldDelegate) => false;
}

/// Initials avatar with a deterministic navy/slate tint.
class AppAvatar extends StatelessWidget {
  const AppAvatar({super.key, required this.name, this.size = 40, this.online = false});

  final String name;
  final double size;
  final bool online;

  static const _bg = [AppColors.navy100, Color(0xFFE0E7FF), AppColors.orange100, AppColors.green100, AppColors.slate200];
  static const _fg = [AppColors.navy800, Color(0xFF3730A3), AppColors.orange700, AppColors.green700, AppColors.slate700];

  @override
  Widget build(BuildContext context) {
    final i = name.codeUnits.fold<int>(0, (a, b) => a + b) % _bg.length;
    return Semantics(
      label: name,
      excludeSemantics: true,
      child: SizedBox.square(
        dimension: size,
        child: Stack(
          children: [
            CircleAvatar(
              radius: size / 2,
              backgroundColor: _bg[i],
              child: Text(
                Fmt.initials(name),
                style: TextStyle(fontSize: size * 0.36, fontWeight: FontWeight.w700, color: _fg[i]),
              ),
            ),
            if (online)
              Positioned(
                right: 0,
                bottom: 0,
                child: Container(
                  width: size * 0.28,
                  height: size * 0.28,
                  decoration: BoxDecoration(
                    color: AppColors.green600,
                    shape: BoxShape.circle,
                    border: Border.all(color: AppColors.white, width: 2),
                  ),
                ),
              ),
          ],
        ),
      ),
    );
  }
}
