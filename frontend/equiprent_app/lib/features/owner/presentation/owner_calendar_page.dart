import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../listings/data/listing_models.dart';
import '../../listings/data/listings_api.dart';
import '../../comms/presentation/comms_widgets.dart' show runComms;
import '../../verification/presentation/verification_widgets.dart';

/// Per-machine availability from the backend: green = available, red = booked.
/// Same source as the customer's calendar, so both panels always agree.
class OwnerCalendarPage extends ConsumerStatefulWidget {
  const OwnerCalendarPage({super.key});

  @override
  ConsumerState<OwnerCalendarPage> createState() => _OwnerCalendarPageState();
}

class _OwnerCalendarPageState extends ConsumerState<OwnerCalendarPage> {
  String? _equipmentId;
  DateTime _month = DateTime(DateTime.now().year, DateTime.now().month);

  void _refresh(String id) {
    ref.invalidate(availabilityProvider(id));
    ref.invalidate(ownerBlocksProvider(id));
  }

  Future<void> _block(String id) async {
    final today = MonthCalendar.dateOnly(DateTime.now());
    final range = await showDateRangePicker(
      context: context,
      firstDate: today,
      lastDate: today.add(const Duration(days: 365)),
      helpText: 'Block dates',
      saveText: 'Block',
    );
    if (range == null || !mounted) return;
    final end = range.end.add(const Duration(days: 1)); // inclusive last day
    final ok = await runComms(
      context,
      () => ref.read(listingsApiProvider).addBlock(ref.read(listingOrgProvider), id, range.start, end),
    );
    if (ok && mounted) {
      _refresh(id);
      AppToast.success(context, 'Dates blocked. Customers see them as booked.');
    }
  }

  Future<void> _unblock(String id, OwnerBlock b) async {
    final last = b.end.toLocal().subtract(const Duration(days: 1));
    final yes = await showConfirmDialog(
      context,
      title: 'Unblock these dates?',
      message: '${Fmt.shortDate(b.start.toLocal())} – ${Fmt.shortDate(last)} will become available again.',
      confirmLabel: 'Unblock',
    );
    if (!yes || !mounted) return;
    final ok = await runComms(context, () => ref.read(listingsApiProvider).removeBlock(ref.read(listingOrgProvider), id, b.id));
    if (ok && mounted) {
      _refresh(id);
      AppToast.success(context, 'Dates are available again.');
    }
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final listings = ref.watch(myListingsProvider(null));
    return AppPage(
      title: 'Availability',
      subtitle: 'Green = available, red = booked. Tap a blocked day to unblock it.',
      body: AsyncBody(
        value: listings,
        onRetry: () => ref.invalidate(myListingsProvider(null)),
        builder: (page) {
          final items = page.items;
          if (items.isEmpty) {
            return const EmptyState(
              icon: AppIcons.calendar,
              title: 'No equipment yet',
              message: 'List a machine to see its availability.',
            );
          }
          final id = _equipmentId ?? items.first.id;
          final busy = ref.watch(availabilityProvider(id));
          final blocks = ref.watch(ownerBlocksProvider(id)).value ?? const <OwnerBlock>[];
          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(availabilityProvider(id)),
            child: PageBody(
              children: [
                SizedBox(
                  height: 40,
                  child: ListView(
                    scrollDirection: Axis.horizontal,
                    children: [
                      for (final x in items)
                        Padding(
                          padding: const EdgeInsets.only(right: AppSpacing.sm),
                          child: ChoiceChip(
                            label: Text(x.title),
                            selected: x.id == id,
                            showCheckmark: false,
                            selectedColor: AppColors.navy700,
                            labelStyle: theme.textTheme.labelLarge?.copyWith(
                              color: x.id == id ? AppColors.white : AppColors.slate700,
                            ),
                            onSelected: (_) => setState(() => _equipmentId = x.id),
                          ),
                        ),
                    ],
                  ),
                ),
                const Gap(AppSpacing.sm),
                Align(
                  alignment: Alignment.centerLeft,
                  child: AppButton(
                    key: const Key('block-dates'),
                    label: 'Block dates',
                    icon: AppIcons.lock,
                    size: AppButtonSize.small,
                    variant: AppButtonVariant.secondary,
                    onPressed: () => _block(id),
                  ),
                ),
                const Gap(AppSpacing.md),
                AsyncBody(
                  value: busy,
                  onRetry: () => ref.invalidate(availabilityProvider(id)),
                  builder: (ranges) {
                    final booked = <DateTime>{};
                    for (final r in ranges) {
                      var d = MonthCalendar.dateOnly(r.start.toLocal());
                      final end = r.end.toLocal();
                      while (d.isBefore(end)) {
                        booked.add(d);
                        d = d.add(const Duration(days: 1));
                      }
                    }
                    return AppCard(
                      child: MonthCalendar(
                        key: const Key('owner-calendar'),
                        month: _month,
                        today: DateTime.now(),
                        booked: booked,
                        onMonthChanged: (m) => setState(() => _month = m),
                        onDayTap: (d) {
                          for (final b in blocks) {
                            final s = MonthCalendar.dateOnly(b.start.toLocal());
                            if (!d.isBefore(s) && d.isBefore(b.end.toLocal())) {
                              _unblock(id, b);
                              return;
                            }
                          }
                          AppToast.show(context, '${Fmt.shortDate(d)}: ${booked.contains(d) ? 'booked' : 'available'}.');
                        },
                      ),
                    );
                  },
                ),
              ],
            ),
          );
        },
      ),
    );
  }
}
