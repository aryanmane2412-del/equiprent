import 'package:flutter/material.dart';
import '../../core/utils/validators.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter/services.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_controller.dart';
import '../comms/data/comms_api.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../discovery/data/discovery_api.dart';
import '../discovery/data/discovery_models.dart' show LocationDetails;
import '../listings/data/listing_models.dart' show BusyRange, Listing, priceUnits;
import '../listings/data/listings_api.dart';
import '../verification/presentation/customer_verification_page.dart'
    show stateCodes;
import 'data/booking_api.dart';
import 'data/booking_models.dart';

/// Booking a machine: dates, work site, operator and delivery choices, a
/// live estimate computed by the server, payment mode and submission. The
/// request is sent with an idempotency key, so a retried tap never creates
/// two bookings.
class BookingRequestPage extends ConsumerStatefulWidget {
  const BookingRequestPage({super.key, required this.equipmentId});
  final String equipmentId;
  @override
  ConsumerState<BookingRequestPage> createState() => _BookingRequestPageState();
}

class _BookingRequestPageState extends ConsumerState<BookingRequestPage> {
  static const _startHours = [6, 7, 8, 9, 10];

  late RentalDates _dates;
  bool? _withOperator;
  bool? _delivery;
  String _unit = 'auto';
  SiteAddress? _site;
  String _paymentMode = 'online';
  final _work = TextEditingController();
  String _customerType = 'individual';
  final _cf = <String, TextEditingController>{
    for (final k in [
      'name',
      'company_name',
      'contact_name',
      'phone',
      'email',
      'address',
      'city',
      'state',
      'pin_code',
      'gstin',
    ])
      k: TextEditingController(),
  };
  bool _submitting = false;

  /// One key per distinct request body: unchanged retries replay the first
  /// response; any change makes a new request.
  String _key = newClientId();

  @override
  void initState() {
    super.initState();
    final t = DateTime.now().add(const Duration(days: 1));
    final start = DateTime(t.year, t.month, t.day);
    _dates = RentalDates(start: start, end: start.add(const Duration(days: 2)));
    // Prefill from the signed-in account's own details (editable).
    final u = ref.read(authControllerProvider).user;
    if (u != null) {
      _cf['name']!.text = u.fullName;
      _cf['contact_name']!.text = u.fullName;
      _cf['phone']!.text = u.phone.replaceAll(RegExp(r'^\+?91'), '');
      _cf['email']!.text = u.email ?? '';
    }
  }

  @override
  void dispose() {
    _work.dispose();
    for (final c in _cf.values) {
      c.dispose();
    }
    super.dispose();
  }

  void _change(VoidCallback fn) => setState(() {
    fn();
    _key = newClientId();
  });

  bool _operatorFor(Listing l) => _withOperator ?? (l.wetHire && !l.dryHire);
  bool _deliveryFor(Listing l) => l.delivery && (_delivery ?? true);

  BookingChoice _choice(Listing l) => BookingChoice(
    dates: _dates,
    unit: _unit,
    withOperator: _operatorFor(l),
    delivery: _deliveryFor(l),
    pickup: _deliveryFor(l),
    site: _site?.point,
  );

  Future<void> _pickDates() async {
    final today = DateTime.now();
    final first = DateTime(today.year, today.month, today.day);
    final picked = await showDateRangePicker(
      context: context,
      firstDate: first,
      lastDate: first.add(const Duration(days: 180)),
      initialDateRange: DateTimeRange(start: _dates.start, end: _dates.end),
      helpText: 'Rental dates',
    );
    if (picked != null) {
      _change(
        () => _dates = RentalDates(
          start: picked.start,
          end: picked.end,
          startHour: _dates.startHour,
        ),
      );
    }
  }

  Future<void> _addSite() async {
    final site = await showDialog<SiteAddress>(
      context: context,
      builder: (_) => const _NewSiteDialog(),
    );
    if (site != null) {
      ref.invalidate(customerSitesProvider);
      _change(() => _site = site);
      if (mounted) AppToast.success(context, 'Work site saved.');
    }
  }

  Map<String, String> _customerBody() => {
    for (final k in _fieldsFor(_customerType)) k: _cf[k]!.text.trim(),
  };

  static List<String> _fieldsFor(String type) => type == 'company'
      ? [
          'company_name',
          'contact_name',
          'phone',
          'email',
          'address',
          'city',
          'state',
          'pin_code',
          'gstin',
        ]
      : ['name', 'phone', 'email'];

  /// First problem with the customer block for the chosen type, or null.
  String? _customerError() {
    final v = _customerBody();
    final company = _customerType == 'company';
    for (final k in _fieldsFor(_customerType)) {
      if (k != 'gstin' && v[k]!.isEmpty) {
        return 'Fill in all required customer details.';
      }
    }
    if (Validators.phone(v['phone']) != null) {
      return 'Enter a valid 10-digit mobile number.';
    }
    if (!RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch(v['email']!)) {
      return 'Enter a valid email address.';
    }
    if (company && !RegExp(r'^[1-9][0-9]{5}$').hasMatch(v['pin_code']!)) {
      return 'Enter a 6-digit PIN code.';
    }
    if (company &&
        v['gstin']!.isNotEmpty &&
        !RegExp(
          r'^[0-9]{2}[A-Za-z]{5}[0-9]{4}[A-Za-z][1-9A-Za-z][Zz][0-9A-Za-z]$',
        ).hasMatch(v['gstin']!)) {
      return 'Enter a valid 15-character GSTIN.';
    }
    return null;
  }

  Future<void> _submit(Listing l) async {
    final org = ref.read(sideOrgProvider(BookingSide.customer));
    if (_site == null) return;
    final problem = _customerError();
    if (problem != null) {
      AppToast.error(context, problem);
      return;
    }
    setState(() => _submitting = true);
    try {
      final id = await ref
          .read(bookingApiProvider)
          .create(
            org,
            equipmentId: l.id,
            siteAddressId: _site!.id,
            choice: _choice(l),
            workDescription: _work.text.trim(),
            paymentMode: _paymentMode,
            idempotencyKey: _key,
            customerType: _customerType,
            customer: _customerBody(),
          );
      ref.invalidate(bookingsProvider(BookingSide.customer));
      if (!mounted) return;
      AppToast.success(
        context,
        l.instantBook
            ? 'Booking created. Complete the payment to confirm it.'
            : 'Request sent to ${l.ownerName}.',
      );
      // Land on the bookings list with the new booking on top, so Back works.
      context.go('/customer/bookings');
      context.push('/customer/bookings/$id');
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _submitting = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final listing = ref.watch(listingProvider(widget.equipmentId));
    return AppPage(
      title: 'Book equipment',
      subtitle: listing.value?.title,
      topLevel: false,
      bottomBar: listing.whenOrNull(data: (l) => _bottomBar(context, l)),
      body: AsyncBody(
        value: listing,
        onRetry: () => ref.invalidate(listingProvider(widget.equipmentId)),
        builder: (l) => PageBody(
          children: [
            _equipment(context, l),
            const Gap(AppSpacing.md),
            _datesSection(context, l),
            const Gap(AppSpacing.md),
            _siteSection(context),
            const Gap(AppSpacing.md),
            _hireSection(context, l),
            const Gap(AppSpacing.md),
            _EstimateCard(
              equipmentId: l.id,
              choice: _choice(l),
              needsSite: _site == null && _deliveryFor(l),
              onShift: _shiftTo,
            ),
            const Gap(AppSpacing.md),
            _customerSection(context),
            _paymentSection(context, l),
            const Gap(AppSpacing.xl),
          ],
        ),
      ),
    );
  }

  void _shiftTo(DateTime start) {
    final s = DateTime(start.year, start.month, start.day);
    _change(
      () => _dates = RentalDates(
        start: s,
        end: s.add(Duration(days: _dates.days - 1)),
        startHour: _dates.startHour,
      ),
    );
  }

  Widget _equipment(BuildContext context, Listing l) => SectionCard(
    title: '1. Equipment',
    icon: AppIcons.bookings,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(l.title, style: Theme.of(context).textTheme.titleSmall),
        const Gap(2),
        Text(
          [
            l.machine,
            if (l.place.isNotEmpty) l.place,
            'Owner: ${l.ownerName}',
          ].join(' · '),
          style: Theme.of(
            context,
          ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
        ),
        if (l.instantBook) ...[
          const Gap(AppSpacing.sm),
          const InlineBanner(
            tone: AppTone.success,
            message:
                'Instant book: no owner approval needed. Your dates are held once you pay.',
          ),
        ],
      ],
    ),
  );

  Widget _datesSection(BuildContext context, Listing l) {
    final availability = ref.watch(availabilityProvider(l.id));
    final busy = availability.value ?? const <BusyRange>[];
    final upcoming = busy
        .where((b) => b.end.isAfter(DateTime.now()))
        .take(4)
        .toList();
    return SectionCard(
      title: '2. Rental dates',
      icon: AppIcons.calendar,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (availability.hasError) ...[
            LoadFailedBanner(what: 'availability', onRetry: () => ref.invalidate(availabilityProvider(l.id))),
            const InlineBanner(
              tone: AppTone.warning,
              message: 'Booked dates are unknown right now. The server re-checks your dates when you send the request.',
            ),
            const Gap(AppSpacing.ms),
          ],
          PickerField(
            key: const Key('rental-dates'),
            label: 'Dates (${_dates.days} day${_dates.days == 1 ? '' : 's'})',
            value: Fmt.dateRange(_dates.start, _dates.end),
            icon: AppIcons.calendarDays,
            onTap: _pickDates,
          ),
          const Gap(AppSpacing.ms),
          Text('Start time', style: Theme.of(context).textTheme.labelLarge),
          const Gap(AppSpacing.xs),
          AppChoiceChips<int>(
            options: _startHours,
            selected: _dates.startHour,
            labelOf: (h) => hhmm(h),
            onSelected: (h) => _change(
              () => _dates = RentalDates(
                start: _dates.start,
                end: _dates.end,
                startHour: h,
              ),
            ),
          ),
          if (upcoming.isNotEmpty) ...[
            const Gap(AppSpacing.ms),
            Text(
              'Already taken',
              style: Theme.of(context).textTheme.labelLarge,
            ),
            for (final b in upcoming)
              Text(
                '• ${Fmt.dateRange(b.start, b.end)} (booked)',
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: AppColors.slate600),
              ),
          ],
        ],
      ),
    );
  }

  Widget _siteSection(BuildContext context) {
    final sites = ref.watch(customerSitesProvider);
    return SectionCard(
      title: '3. Delivery location',
      subtitle: 'The work site the machine goes to',
      icon: AppIcons.siteMarker,
      trailing: AppButton(
        key: const Key('add-site'),
        label: 'Add site',
        size: AppButtonSize.small,
        variant: AppButtonVariant.secondary,
        icon: AppIcons.add,
        onPressed: _addSite,
      ),
      child: sites.when(
        loading: () => const LinearProgressIndicator(),
        error: (e, _) => Text(describeError(e)),
        data: (list) {
          if (list.isEmpty) {
            return const Text(
              'Add your work site to see delivery charges and send the request.',
            );
          }
          return Column(
            children: [
              for (final s in list)
                _OptionTile(
                  key: Key('site-${s.id}'),
                  selected: _site?.id == s.id,
                  title: s.label,
                  subtitle: s.oneLine,
                  onTap: () => _change(() => _site = s),
                ),
            ],
          );
        },
      ),
    );
  }

  Widget _hireSection(BuildContext context, Listing l) {
    final withOp = _operatorFor(l);
    final units = {
      for (final p in l.pricing)
        if (p.withOperator == withOp) p.unit,
    };
    return SectionCard(
      title: '4. Operator and transport',
      icon: AppIcons.operator,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          if (l.wetHire && l.dryHire)
            AppChoiceChips<bool>(
              options: const [true, false],
              selected: withOp,
              labelOf: (b) => b ? 'With operator' : 'Without operator',
              onSelected: (b) => _change(() {
                _withOperator = b;
                _unit = 'auto';
              }),
            )
          else
            Text(
              withOp
                  ? 'Comes with the owner\'s operator.'
                  : 'You provide the operator (dry hire).',
            ),
          if (units.length > 1) ...[
            const Gap(AppSpacing.ms),
            Text('Charge by', style: Theme.of(context).textTheme.labelLarge),
            const Gap(AppSpacing.xs),
            AppChoiceChips<String>(
              options: ['auto', ...units],
              selected: _unit,
              labelOf: (u) =>
                  u == 'auto' ? 'Best price' : 'Per ${priceUnits[u] ?? u}',
              onSelected: (u) => _change(() => _unit = u),
            ),
          ],
          const Gap(AppSpacing.ms),
          if (l.delivery)
            SwitchListTile.adaptive(
              key: const Key('delivery-switch'),
              contentPadding: EdgeInsets.zero,
              value: _deliveryFor(l),
              title: const Text('Delivery and pickup by the owner'),
              subtitle: const Text(
                'Otherwise you collect the machine from the yard',
              ),
              onChanged: (v) => _change(() => _delivery = v),
            )
          else
            const Text(
              'Collect from the owner\'s yard (no delivery for this machine).',
            ),
        ],
      ),
    );
  }

  Widget _customerSection(BuildContext context) {
    final company = _customerType == 'company';
    Widget f(
      String k,
      String label, {
      TextInputType? type,
      bool req = true,
      int? max,
    }) => Padding(
      padding: const EdgeInsets.only(bottom: AppSpacing.ms),
      child: AppTextField(
        key: Key('cust-$k'),
        label: req ? label : '$label (optional)',
        controller: _cf[k],
        keyboardType: type,
        maxLength: max,
        onChanged: (_) => _key = newClientId(),
      ),
    );
    return SectionCard(
      title: '5. Who is renting?',
      icon: AppIcons.user,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          _OptionTile(
            key: const Key('type-individual'),
            selected: !company,
            title: 'Individual / Personal use',
            subtitle: 'No company registration needed',
            onTap: () => _change(() => _customerType = 'individual'),
          ),
          _OptionTile(
            key: const Key('type-company'),
            selected: company,
            title: 'Company / Business use',
            subtitle: 'Business details, GSTIN where applicable',
            onTap: () => _change(() => _customerType = 'company'),
          ),
          const Gap(AppSpacing.ms),
          if (company) ...[
            f('company_name', 'Company name'),
            f('contact_name', 'Contact person name'),
            f(
              'phone',
              'Business phone number',
              type: TextInputType.phone,
              max: 10,
            ),
            f('email', 'Business email', type: TextInputType.emailAddress),
            f('address', 'Business address'),
            f('city', 'City'),
            f('state', 'State'),
            f('pin_code', 'PIN code', type: TextInputType.number, max: 6),
            f('gstin', 'GSTIN', req: false, max: 15),
          ] else ...[
            f('name', 'Customer name'),
            f('phone', 'Mobile number', type: TextInputType.phone, max: 10),
            f('email', 'Email', type: TextInputType.emailAddress),
            Text(
              'Your work site address, city, state and PIN code are taken from the work site chosen above.',
              style: Theme.of(context).textTheme.bodySmall,
            ),
          ],
        ],
      ),
    );
  }

  Widget _paymentSection(BuildContext context, Listing l) => SectionCard(
    title: '6. Payment and notes',
    icon: AppIcons.payments,
    child: Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        _OptionTile(
          key: const Key('pay-online'),
          selected: _paymentMode == 'online',
          title: 'Pay online',
          subtitle: l.instantBook
              ? 'Pay the advance now to confirm'
              : 'Pay the advance once the owner accepts',
          onTap: () => _change(() => _paymentMode = 'online'),
        ),
        _OptionTile(
          key: const Key('pay-offline'),
          selected: _paymentMode == 'offline',
          title: 'Pay the owner directly',
          subtitle: 'Cash, cheque or bank transfer agreed with the owner',
          onTap: () => _change(() => _paymentMode = 'offline'),
        ),
        const Gap(AppSpacing.ms),
        AppTextField(
          key: const Key('work-description'),
          label: 'Work to be done (optional)',
          hint: 'e.g. Foundation trenching for Block B',
          controller: _work,
          maxLines: 3,
          maxLength: 2000,
          onChanged: (_) => _key = newClientId(),
        ),
      ],
    ),
  );

  Widget _bottomBar(BuildContext context, Listing l) {
    final est = ref.watch(estimateProvider((l.id, _choice(l))));
    final ok = est.value?.availability?.available ?? false;
    final ready = _site != null && ok && !est.isLoading;
    return BottomActionBar.split(
      summary: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text(
            est.value == null
                ? 'Estimated total'
                : money(est.value!.totalPaise),
            style: Theme.of(context).textTheme.titleMedium,
          ),
          Text(
            _site == null
                ? 'Choose a work site'
                : (ok
                      ? 'incl. taxes · final bill after return'
                      : 'Dates unavailable'),
            style: Theme.of(context).textTheme.bodySmall,
          ),
        ],
      ),
      actions: [
        AppButton(
          key: const Key('submit-booking'),
          label: l.instantBook ? 'Book now' : 'Send request',
          variant: AppButtonVariant.accent,
          isLoading: _submitting,
          onPressed: ready && !_submitting ? () => _submit(l) : null,
        ),
      ],
    );
  }
}

/// The server's estimate for the current choices.
class _EstimateCard extends ConsumerWidget {
  const _EstimateCard({
    required this.equipmentId,
    required this.choice,
    required this.needsSite,
    required this.onShift,
  });
  final String equipmentId;
  final BookingChoice choice;
  final bool needsSite;
  final ValueChanged<DateTime> onShift;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final est = ref.watch(estimateProvider((equipmentId, choice)));
    return SectionCard(
      title: 'Estimated price',
      subtitle: 'Computed by EQUIPRENT from the owner\'s rates',
      icon: AppIcons.wallet,
      child: est.when(
        loading: () => const Padding(
          padding: EdgeInsets.all(AppSpacing.sm),
          child: LinearProgressIndicator(),
        ),
        error: (e, _) => Text(
          needsSite
              ? 'Choose a work site to price delivery.'
              : describeError(e),
        ),
        data: (q) => Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            if (q.availability?.problem != null) ...[
              InlineBanner(
                key: const Key('dates-unavailable'),
                tone: AppTone.danger,
                message: q.availability!.problem!,
                action: q.availability!.nextAvailableStart == null
                    ? null
                    : AppButton(
                        key: const Key('next-free-start'),
                        label:
                            'Start ${Fmt.shortDate(q.availability!.nextAvailableStart!)}',
                        size: AppButtonSize.small,
                        variant: AppButtonVariant.secondary,
                        onPressed: () =>
                            onShift(q.availability!.nextAvailableStart!),
                      ),
              ),
              const Gap(AppSpacing.sm),
            ],
            if (q.start != null && q.end != null)
              InfoRow(
                label: 'Period',
                value: '${Fmt.dateTime(q.start!)} – ${Fmt.dateTime(q.end!)}',
                icon: AppIcons.clock,
              ),
            for (final c in q.charges)
              InfoRow(
                label: c.detail.isEmpty ? c.label : '${c.label} (${c.detail})',
                value: money(c.paise),
              ),
            const Divider(),
            InfoRow(
              key: const Key('estimate-total'),
              label: 'Estimated total',
              value: money(q.totalPaise),
              emphasize: true,
            ),
            if (q.depositPaise > 0)
              InfoRow(
                label: 'Refundable deposit',
                value: money(q.depositPaise),
                icon: AppIcons.shield,
              ),
            if (q.advanceDuePaise > 0)
              InfoRow(
                label: 'Advance to confirm',
                value: money(q.advanceDuePaise),
                icon: AppIcons.payments,
              ),
            for (final n in q.notes)
              Text(
                '• $n',
                style: Theme.of(
                  context,
                ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
              ),
          ],
        ),
      ),
    );
  }
}

class _OptionTile extends StatelessWidget {
  const _OptionTile({
    super.key,
    required this.selected,
    required this.title,
    required this.subtitle,
    required this.onTap,
  });
  final bool selected;
  final String title;
  final String subtitle;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) => Padding(
    padding: const EdgeInsets.only(bottom: AppSpacing.sm),
    child: AppCard(
      onTap: onTap,
      borderColor: selected ? AppColors.navy700 : null,
      semanticLabel: '$title${selected ? ', selected' : ''}',
      child: Row(
        children: [
          Icon(
            selected
                ? Icons.radio_button_checked
                : Icons.radio_button_unchecked,
            color: selected ? AppColors.navy700 : AppColors.slate400,
            size: 20,
          ),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text(title, style: Theme.of(context).textTheme.titleSmall),
                Text(
                  subtitle,
                  style: Theme.of(
                    context,
                  ).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
                ),
              ],
            ),
          ),
        ],
      ),
    ),
  );
}

/// Adds a work site. Its location comes from the device, or from the PIN
/// code when location is unavailable.
class _NewSiteDialog extends ConsumerStatefulWidget {
  const _NewSiteDialog();
  @override
  ConsumerState<_NewSiteDialog> createState() => _NewSiteDialogState();
}

class _NewSiteDialogState extends ConsumerState<_NewSiteDialog> {
  final _form = GlobalKey<FormState>();
  final _label = TextEditingController();
  final _line1 = TextEditingController();
  final _city = TextEditingController();
  final _district = TextEditingController();
  final _pincode = TextEditingController();
  String? _state = '27';
  ({double lat, double lng})? _point;
  bool _busy = false, _lookingUp = false, _locating = false;
  int _lookupGeneration = 0;
  String _resolvedPin = '';
  String? _error;

  @override
  void dispose() {
    for (final c in [_label, _line1, _city, _district, _pincode]) {
      c.dispose();
    }
    super.dispose();
  }

  static final _pinPattern = RegExp(r'^[1-9][0-9]{5}$');

  Future<void> _onPinChanged(String value) async {
    _lookupGeneration++;
    setState(() {
      _point = null;
      _error = null;
      if (_resolvedPin.isNotEmpty && _resolvedPin != value.trim()) {
        _city.clear();
        _district.clear();
        _resolvedPin = '';
      }
      if (value.trim().isNotEmpty && _resolvedPin != value.trim()) {
        _state = null;
      }
    });
    if (value.trim().length == 6 && _pinPattern.hasMatch(value.trim())) {
      await _lookupPin();
    }
  }

  Future<void> _applyLocation(
    LocationDetails location, {
    bool replaceAddress = false,
  }) async {
    if (!mounted) return;
    setState(() {
      if (location.pincode.isNotEmpty) _pincode.text = location.pincode;
      if (location.area.isNotEmpty && _line1.text.trim().isEmpty) {
        _line1.text = location.area;
      }
      if (replaceAddress && location.addressLine.isNotEmpty) {
        _line1.text = location.addressLine;
      }
      if (location.city.isNotEmpty) _city.text = location.city;
      if (location.district.isNotEmpty) _district.text = location.district;
      _state = stateCodes.containsKey(location.stateCode)
          ? location.stateCode
          : null;
      _resolvedPin = location.pincode;
      _point = (lat: location.lat, lng: location.lng);
      _error = null;
    });
  }

  Future<void> _lookupPin() async {
    final pin = _pincode.text.trim();
    if (!_pinPattern.hasMatch(pin)) {
      setState(
        () => _error =
            'Enter a valid 6-digit Indian PIN code before looking up its location.',
      );
      return;
    }
    final generation = ++_lookupGeneration;
    setState(() {
      _lookingUp = true;
      _error = null;
    });
    try {
      final result = await ref.read(discoveryApiProvider).lookupPincode(pin);
      if (!mounted ||
          generation != _lookupGeneration ||
          _pincode.text.trim() != pin) {
        return;
      }
      await _applyLocation(result);
      if (result.city.isEmpty || result.stateCode.isEmpty) {
        setState(
          () => _error =
              'PIN location found, but city or state could not be verified. Please complete those fields manually.',
        );
      }
    } catch (e) {
      if (mounted && generation == _lookupGeneration) {
        setState(() => _error = describeError(e));
      }
    } finally {
      if (mounted && generation == _lookupGeneration) {
        setState(() => _lookingUp = false);
      }
    }
  }

  Future<void> _locate() async {
    setState(() {
      _locating = true;
      _error = null;
    });
    try {
      final point = await ref.read(deviceLocatorProvider)();
      final address = await ref
          .read(discoveryApiProvider)
          .reverseGeocode(point.lat, point.lng);
      if (address.city.isEmpty ||
          address.pincode.isEmpty ||
          address.stateCode.isEmpty) {
        throw const LocationUnavailable(
          'Your GPS position was found, but a complete postal address could not be verified. Enter the missing address fields manually.',
        );
      }
      await _applyLocation(address, replaceAddress: true);
    } catch (e) {
      if (mounted) {
        setState(
          () =>
              _error = e is LocationUnavailable ? e.message : describeError(e),
        );
      }
    } finally {
      if (mounted) setState(() => _locating = false);
    }
  }

  Future<void> _save() async {
    if (_busy || !_form.currentState!.validate()) return;
    final org = ref.read(sideOrgProvider(BookingSide.customer));
    if (org == null) {
      setState(
        () => _error =
            'Choose your customer organisation before saving a work site.',
      );
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      var point = _point;
      if (point == null) {
        final location = await ref
            .read(discoveryApiProvider)
            .lookupPincode(_pincode.text.trim());
        if (location.city.isNotEmpty) _city.text = location.city;
        if (location.district.isNotEmpty) _district.text = location.district;
        if (location.stateCode.isNotEmpty &&
            stateCodes.containsKey(location.stateCode)) {
          _state = location.stateCode;
        }
        point = (lat: location.lat, lng: location.lng);
      }
      if (_state == null || !stateCodes.containsKey(_state)) {
        setState(() => _error = 'Select a state before saving this work site.');
        return;
      }
      final site = await ref
          .read(bookingApiProvider)
          .addSite(
            org,
            NewSite(
              label: _label.text.trim(),
              line1: _line1.text.trim(),
              city: _city.text.trim(),
              district: _district.text.trim().isEmpty
                  ? null
                  : _district.text.trim(),
              stateCode: _state!,
              pincode: _pincode.text.trim(),
              lat: point.lat,
              lng: point.lng,
            ),
          );
      if (mounted) Navigator.of(context).pop(site);
    } catch (e) {
      if (mounted) setState(() => _error = describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return AppDialog(
      title: 'Add a work site',
      icon: AppIcons.siteMarker,
      content: Form(
        key: _form,
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            AppTextField(
              key: const Key('site-label'),
              label: 'Name',
              hint: 'Wakad tower site',
              controller: _label,
              requiredField: true,
              validator: (v) => (v ?? '').trim().isEmpty
                  ? 'Enter a name for this work site.'
                  : null,
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              key: const Key('site-line1'),
              label: 'Address',
              controller: _line1,
              requiredField: true,
              validator: (v) => (v ?? '').trim().length < 3
                  ? 'Enter a site address of at least 3 characters.'
                  : null,
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              key: const Key('site-city'),
              label: 'City',
              controller: _city,
              requiredField: true,
              validator: (v) => (v ?? '').trim().isEmpty
                  ? 'Enter a city or use location lookup.'
                  : null,
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              key: const Key('site-district'),
              label: 'District',
              controller: _district,
            ),
            const Gap(AppSpacing.sm),
            AppTextField(
              key: const Key('site-pincode'),
              label: 'PIN code',
              controller: _pincode,
              keyboardType: TextInputType.number,
              inputFormatters: [FilteringTextInputFormatter.digitsOnly],
              maxLength: 6,
              requiredField: true,
              suffix: _lookingUp
                  ? const Padding(
                      padding: EdgeInsets.all(12),
                      child: SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2),
                      ),
                    )
                  : null,
              validator: (v) => _pinPattern.hasMatch((v ?? '').trim())
                  ? null
                  : 'Enter a valid 6-digit Indian PIN code.',
              onChanged: _onPinChanged,
            ),
            const Gap(AppSpacing.xs),
            AppButton(
              label: 'Location from PIN code',
              size: AppButtonSize.small,
              variant: AppButtonVariant.secondary,
              isLoading: _lookingUp,
              onPressed: _lookingUp || _busy ? null : _lookupPin,
            ),
            const Gap(AppSpacing.sm),
            DropdownButtonFormField<String>(
              initialValue: _state,
              decoration: const InputDecoration(labelText: 'State'),
              items: [
                for (final e in stateCodes.entries)
                  DropdownMenuItem(value: e.key, child: Text(e.value)),
              ],
              validator: (v) => v == null ? 'Select a state.' : null,
              onChanged: (v) => setState(() => _state = v),
            ),
            const Gap(AppSpacing.sm),
            AppButton(
              label: 'Use my location',
              size: AppButtonSize.small,
              variant: AppButtonVariant.ghost,
              icon: AppIcons.locate,
              isLoading: _locating,
              onPressed: _locating || _busy ? null : _locate,
            ),
            if (_point != null) ...[
              const Gap(AppSpacing.xs),
              Text(
                'Verified location · ${_point!.lat.toStringAsFixed(5)}, ${_point!.lng.toStringAsFixed(5)}',
                style: Theme.of(context).textTheme.bodySmall,
              ),
            ],
            if (_error != null) ...[
              const Gap(AppSpacing.sm),
              InlineBanner(tone: AppTone.danger, message: _error!),
            ],
          ],
        ),
      ),
      actions: [
        AppButton(
          label: 'Cancel',
          variant: AppButtonVariant.secondary,
          onPressed: () => Navigator.of(context).pop(),
        ),
        AppButton(
          key: const Key('save-site'),
          label: 'Save site',
          isLoading: _busy,
          onPressed: _busy || _lookingUp || _locating ? null : _save,
        ),
      ],
    );
  }
}
