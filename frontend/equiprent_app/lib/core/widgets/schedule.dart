import 'package:flutter/material.dart';
import 'package:intl/intl.dart';

import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';

/// Month grid showing booked / blocked days, with month navigation.
class MonthCalendar extends StatelessWidget {
  const MonthCalendar({
    super.key,
    required this.month,
    required this.onMonthChanged,
    this.booked = const {},
    this.blocked = const {},
    this.selected,
    this.onDayTap,
    this.today,
  });

  final DateTime month;
  final ValueChanged<DateTime> onMonthChanged;

  /// Days (date-only) with a booking.
  final Set<DateTime> booked;

  /// Days blocked by the owner (maintenance etc.).
  final Set<DateTime> blocked;
  final DateTime? selected;
  final ValueChanged<DateTime>? onDayTap;
  final DateTime? today;

  static DateTime dateOnly(DateTime d) => DateTime(d.year, d.month, d.day);

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final first = DateTime(month.year, month.month);
    final daysInMonth = DateTime(month.year, month.month + 1, 0).day;
    final leading = first.weekday - 1; // Monday-first
    final cells = leading + daysInMonth;
    final rows = (cells / 7).ceil();
    final todayD = today == null ? null : dateOnly(today!);

    return Column(
      children: [
        Row(
          children: [
            Expanded(child: Text(DateFormat('MMMM yyyy').format(month), style: theme.textTheme.titleMedium)),
            IconButton(tooltip: 'Previous month', onPressed: () => onMonthChanged(DateTime(month.year, month.month - 1)), icon: const Icon(AppIcons.chevronLeft, size: 18)),
            IconButton(tooltip: 'Next month', onPressed: () => onMonthChanged(DateTime(month.year, month.month + 1)), icon: const Icon(AppIcons.chevronRight, size: 18)),
          ],
        ),
        const SizedBox(height: AppSpacing.sm),
        Row(
          children: [
            for (final d in const ['Mon', 'Tue', 'Wed', 'Thu', 'Fri', 'Sat', 'Sun'])
              Expanded(child: Center(child: Text(d, style: theme.textTheme.labelSmall?.copyWith(color: AppColors.slate500)))),
          ],
        ),
        const SizedBox(height: AppSpacing.xs),
        for (var r = 0; r < rows; r++)
          Row(
            children: [
              for (var c = 0; c < 7; c++)
                Expanded(
                  child: Builder(builder: (context) {
                    final dayNum = r * 7 + c - leading + 1;
                    if (dayNum < 1 || dayNum > daysInMonth) return const SizedBox(height: 44);
                    final date = DateTime(month.year, month.month, dayNum);
                    // Two states only: booked (red) or available (green). Owner blocks count as booked.
                    final isBooked = booked.contains(date) || blocked.contains(date);
                    final isSelected = selected != null && dateOnly(selected!) == date;
                    final isToday = todayD == date;
                    final bg = isSelected ? AppColors.navy700 : (isBooked ? AppColors.red100 : AppColors.green100);
                    final fg = isSelected ? AppColors.white : (isBooked ? AppColors.red700 : AppColors.green700);
                    final status = isBooked ? ', booked' : ', available';
                    return Semantics(
                      button: onDayTap != null,
                      label: '${DateFormat('d MMMM').format(date)}$status',
                      excludeSemantics: true,
                      child: Padding(
                        padding: const EdgeInsets.all(2),
                        child: InkWell(
                          borderRadius: const BorderRadius.all(Radius.circular(8)),
                          onTap: onDayTap == null ? null : () => onDayTap!(date),
                          child: Container(
                            height: 40,
                            decoration: BoxDecoration(
                              color: bg,
                              borderRadius: const BorderRadius.all(Radius.circular(8)),
                              border: isToday && !isSelected ? Border.all(color: AppColors.orange500, width: 1.5) : null,
                            ),
                            alignment: Alignment.center,
                            child: Text('$dayNum', style: theme.textTheme.labelLarge?.copyWith(color: fg, fontWeight: isToday ? FontWeight.w700 : FontWeight.w500)),
                          ),
                        ),
                      ),
                    );
                  }),
                ),
            ],
          ),
        const SizedBox(height: AppSpacing.sm),
        Wrap(
          spacing: AppSpacing.md,
          runSpacing: AppSpacing.xs,
          children: const [
            _Legend(color: AppColors.green100, label: 'Available'),
            _Legend(color: AppColors.red100, label: 'Booked'),
            _Legend(color: Colors.transparent, border: AppColors.orange500, label: 'Today'),
          ],
        ),
      ],
    );
  }
}

class _Legend extends StatelessWidget {
  const _Legend({required this.color, required this.label, this.border});

  final Color color;
  final Color? border;
  final String label;

  @override
  Widget build(BuildContext context) => Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Container(
            width: 12,
            height: 12,
            decoration: BoxDecoration(color: color, borderRadius: BorderRadius.circular(3), border: border == null ? null : Border.all(color: border!, width: 1.5)),
          ),
          const SizedBox(width: 6),
          Text(label, style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate600)),
        ],
      );
}

/// One step of a [StatusTimeline].
class TimelineStep {
  const TimelineStep({required this.title, this.subtitle, this.done = false, this.current = false});

  final String title;
  final String? subtitle;
  final bool done;
  final bool current;
}

/// Vertical progress timeline (booking lifecycle, job progress).
class StatusTimeline extends StatelessWidget {
  const StatusTimeline({super.key, required this.steps});

  final List<TimelineStep> steps;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Column(
      children: [
        for (var i = 0; i < steps.length; i++)
          IntrinsicHeight(
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                SizedBox(
                  width: 28,
                  child: Column(
                    children: [
                      Container(
                        width: 22,
                        height: 22,
                        decoration: BoxDecoration(
                          color: steps[i].done ? AppColors.green700 : (steps[i].current ? AppColors.navy700 : AppColors.white),
                          shape: BoxShape.circle,
                          border: Border.all(color: steps[i].done ? AppColors.green700 : (steps[i].current ? AppColors.navy700 : AppColors.slate300), width: 2),
                        ),
                        child: steps[i].done
                            ? const Icon(AppIcons.check, size: 12, color: AppColors.white)
                            : steps[i].current
                                ? const Center(child: SizedBox(width: 6, height: 6, child: DecoratedBox(decoration: BoxDecoration(color: AppColors.white, shape: BoxShape.circle))))
                                : null,
                      ),
                      if (i < steps.length - 1)
                        Expanded(child: Container(width: 2, color: steps[i].done ? AppColors.green600 : AppColors.slate200)),
                    ],
                  ),
                ),
                const SizedBox(width: AppSpacing.ms),
                Expanded(
                  child: Padding(
                    padding: const EdgeInsets.only(bottom: AppSpacing.md),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Text(
                          steps[i].title,
                          style: theme.textTheme.titleSmall?.copyWith(color: steps[i].done || steps[i].current ? AppColors.slate900 : AppColors.slate500),
                        ),
                        if (steps[i].subtitle != null)
                          Text(steps[i].subtitle!, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
                      ],
                    ),
                  ),
                ),
              ],
            ),
          ),
      ],
    );
  }
}

/// Horizontal numbered steps for multi-step flows (booking, add equipment).
class StepIndicator extends StatelessWidget {
  const StepIndicator({super.key, required this.steps, required this.current});

  final List<String> steps;
  final int current;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return Semantics(
      label: 'Step ${current + 1} of ${steps.length}: ${steps[current]}',
      excludeSemantics: true,
      child: Row(
        children: [
          for (var i = 0; i < steps.length; i++) ...[
            Expanded(
              child: Column(
                children: [
                  AnimatedContainer(
                    duration: AppMotion.medium,
                    height: 4,
                    decoration: BoxDecoration(
                      color: i <= current ? AppColors.navy700 : AppColors.slate200,
                      borderRadius: const BorderRadius.all(Radius.circular(2)),
                    ),
                  ),
                  const SizedBox(height: 6),
                  Text(
                    steps[i],
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: theme.textTheme.labelSmall?.copyWith(letterSpacing: 0, color: i <= current ? AppColors.navy800 : AppColors.slate500),
                  ),
                ],
              ),
            ),
            if (i < steps.length - 1) const SizedBox(width: AppSpacing.xs),
          ],
        ],
      ),
    );
  }
}
