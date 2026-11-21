import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/auth/auth_controller.dart';
import '../../../core/widgets/widgets.dart';
import '../data/verification_api.dart';
import '../data/verification_models.dart';
import 'verification_widgets.dart';
import 'customer_verification_page.dart' show AddressForm;

/// Operator verification: personal details, licence (stored encrypted),
/// qualifications, experience and skills, availability and documents.
class OperatorVerificationPage extends ConsumerWidget {
  const OperatorVerificationPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final v = ref.watch(operatorVerificationProvider);
    return AppPage(
      title: 'Operator verification',
      body: AsyncBody(
        value: v,
        onRetry: () => ref.invalidate(operatorVerificationProvider),
        builder: (o) => _OperatorBody(o: o),
      ),
    );
  }
}

const _dayLabels = {
  'mon': 'Mon',
  'tue': 'Tue',
  'wed': 'Wed',
  'thu': 'Thu',
  'fri': 'Fri',
  'sat': 'Sat',
  'sun': 'Sun',
};

class _OperatorBody extends ConsumerWidget {
  const _OperatorBody({required this.o});
  final OperatorVerification o;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final user = ref.watch(authControllerProvider).user;
    final api = ref.read(verificationApiProvider);
    void refresh() => ref.invalidate(operatorVerificationProvider);

    final main = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionCard(
          title: 'Personal details',
          icon: AppIcons.user,
          trailing: o.editable
              ? TextButton(
                  onPressed: () => _editAddress(context, ref),
                  child: Text(
                    o.address == null
                        ? 'Add address'
                        : 'Edit address',
                  ),
                )
              : null,
          child: Column(
            children: [
              InfoRow(label: 'Name', value: user?.fullName ?? '—'),
              InfoRow(
                label: 'Mobile',
                value:
                    '${user?.phone ?? '—'}${user?.phoneVerified == true ? ' · verified' : ''}',
              ),
            ],
          ),
        ),
        const Gap(AppSpacing.md),
        if (o.address != null)
          SectionCard(
            title: 'Home address',
            icon: AppIcons.siteMarker,
            child: Text(
              [
                o.address!.line1,
                o.address!.line2,
                o.address!.city,
                o.address!.stateCode,
                o.address!.pincode,
              ].whereType<String>().join(', '),
            ),
          ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Licence, experience & availability',
          icon: AppIcons.shield,
          trailing: o.editable
              ? TextButton(
                  key: const Key('edit-operator-profile'),
                  onPressed: () => _editProfile(context, ref),
                  child: Text(o.licenceLast4 == null ? 'Add' : 'Edit'),
                )
              : null,
          child: o.licenceLast4 == null
              ? const Text(
                  'Add your driving licence, experience, the machines you operate and when you are available.',
                )
              : Column(
                  children: [
                    InfoRow(
                      label: 'Licence',
                      value: '••••${o.licenceLast4} · ${o.licenceClass ?? ''}',
                    ),
                    InfoRow(
                      label: 'Valid until',
                      value: o.licenceExpiresOn ?? '—',
                    ),
                    InfoRow(
                      label: 'Experience',
                      value: '${o.experienceYears ?? 0} years',
                    ),
                    InfoRow(
                      label: 'Machines',
                      value: o.skills.isEmpty ? '—' : o.skills.join(', '),
                    ),
                    if (o.availability != null)
                      InfoRow(
                        label: 'Availability',
                        value:
                            '${o.availability!.days.map((d) => _dayLabels[d] ?? d).join(', ')} · ${o.availability!.start}–${o.availability!.end}',
                      ),
                  ],
                ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Qualifications',
          icon: AppIcons.fileCheck,
          trailing: o.editable
              ? TextButton(
                  key: const Key('add-qualification'),
                  onPressed: () => _addQualification(context, ref),
                  child: const Text('Add'),
                )
              : null,
          child: o.qualifications.isEmpty
              ? const Text(
                  'Optional: certificates such as NSDC or OEM operator training.',
                )
              : Column(
                  children: [
                    for (final q in o.qualifications)
                      ListRow(
                        dense: true,
                        title: q.title,
                        subtitle: [
                          q.issuer,
                          if (q.issuedOn != null) 'issued ${q.issuedOn}',
                          if (q.validUntil != null)
                            'valid until ${q.validUntil}',
                        ].whereType<String>().join(' · '),
                        trailing: o.editable
                            ? IconButton(
                                tooltip: 'Remove',
                                icon: const Icon(AppIcons.delete, size: 18),
                                onPressed: () async {
                                  if (await runVerificationAction(
                                    context,
                                    () => api.deleteQualification(q.id),
                                    success: 'Removed',
                                  )) {
                                    refresh();
                                  }
                                },
                              )
                            : null,
                      ),
                  ],
                ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Verification documents',
          subtitle:
              'Driving licence and PAN (or another accepted identity document). Aadhaar numbers and images are not accepted. Documents use private storage; consent is required for verification review and retention.',
          icon: AppIcons.userCheck,
          trailing: o.editable
              ? TextButton(
                  key: const Key('add-operator-doc'),
                  onPressed: () => _addDoc(context, ref),
                  child: const Text('Add'),
                )
              : null,
          child: DocumentList(
            documents: o.documents,
            onDelete: o.editable
                ? (d) async {
                    if (await runVerificationAction(
                      context,
                      () => api.deleteOperatorDocument(d.id),
                      success: 'Removed',
                    )) {
                      refresh();
                    }
                  }
                : null,
          ),
        ),
        if (o.identityConsent) ...[
          const Gap(AppSpacing.sm),
          const InlineBanner(
            tone: AppTone.success,
            icon: AppIcons.shield,
            title: 'Document privacy consent recorded',
            message:
                'Your verification documents are stored privately and are available only to authorized reviewers through audited, short-lived access.',
          ),
        ],
      ],
    );

    final side = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        ChecklistCard(checks: o.checks),
        const Gap(AppSpacing.md),
        if (o.editable)
          AppButton(
            key: const Key('submit-verification'),
            label: o.application.status == ApplicationStatus.rejected
                ? 'Resubmit for review'
                : 'Submit for review',
            icon: AppIcons.send,
            expand: true,
            onPressed: o.checks.every((c) => c.done)
                ? () async {
                    if (await runVerificationAction(
                      context,
                      api.submitOperator,
                      success: 'Submitted for review',
                    )) {
                      refresh();
                    }
                  }
                : null,
          ),
      ],
    );

    return PageBody(
      children: [
        Wrap(
          spacing: AppSpacing.sm,
          children: [
            ApplicationStatusBadge(o.application.status),
            if (o.verified) const VerifiedTag(),
          ],
        ),
        const Gap(AppSpacing.sm),
        ApplicationBanner(
          application: o.application,
          subject: 'operator profile',
        ),
        const Gap(AppSpacing.md),
        SplitLayout(main: main, side: side, sideWidth: 320),
      ],
    );
  }

  Future<void> _addDoc(BuildContext context, WidgetRef ref) async {
    final doc = await showAddDocumentSheet(
      context,
      docTypes: [...DocTypes.operatorLicence, ...DocTypes.operatorIdentity],
    );
    if (doc == null || !context.mounted) return;
    final api = ref.read(verificationApiProvider);
    final purpose =
        DocTypes.operatorLicence.contains(doc.docType) ||
            doc.docType == 'operator_certificate'
        ? UploadPurpose.operatorDocument
        : UploadPurpose.identityDocument;
    final ok = await runVerificationAction(context, () async {
      final id = await api.upload(purpose, doc.file);
      await api.addOperatorDocument(
        docType: doc.docType,
        uploadId: id,
        number: doc.number,
        expiresOn: doc.expiresOn,
      );
    }, success: '${DocTypes.label(doc.docType)} added');
    if (ok) ref.invalidate(operatorVerificationProvider);
  }

  Future<void> _editAddress(BuildContext context, WidgetRef ref) async {
    final address = await showAppSheet<PostalAddress>(
      context,
      title: 'Driver home address',
      builder: (_) => AddressForm(initial: o.address, homeAddress: true),
    );
    if (address == null || !context.mounted) return;
    final consent =
        o.identityConsent ||
        await showConfirmDialog(
          context,
          title: 'Identity document privacy',
          message:
              'I consent to private review of my identity documents and retention according to EQUIPRENT policy. Aadhaar numbers and Aadhaar images are not collected.',
          confirmLabel: 'I consent',
          tone: AppTone.info,
        );
    if (!consent || !context.mounted) return;
    final ok = await runVerificationAction(
      context,
      () => ref.read(verificationApiProvider).saveOperatorAddress({
        ...address.toJson(),
        'privacy_consent': true,
      }),
      success: 'Address saved',
    );
    if (ok) ref.invalidate(operatorVerificationProvider);
  }

  Future<void> _editProfile(BuildContext context, WidgetRef ref) async {
    final body = await showAppSheet<Json>(
      context,
      title: 'Licence & availability',
      builder: (_) => OperatorProfileForm(initial: o),
    );
    if (body == null || !context.mounted) return;
    if (await runVerificationAction(
      context,
      () => ref.read(verificationApiProvider).saveOperatorProfile(body),
      success: 'Profile saved',
    )) {
      ref.invalidate(operatorVerificationProvider);
    }
  }

  Future<void> _addQualification(BuildContext context, WidgetRef ref) async {
    final q = await showAppSheet<_NewQualification>(
      context,
      title: 'Add a qualification',
      builder: (_) => const _QualificationForm(),
    );
    if (q == null || !context.mounted) return;
    final api = ref.read(verificationApiProvider);
    final ok = await runVerificationAction(context, () async {
      final uploadId = q.file == null
          ? null
          : await api.upload(UploadPurpose.operatorDocument, q.file!);
      await api.addQualification({...q.body, 'upload_id': ?uploadId});
    }, success: 'Qualification added');
    if (ok) ref.invalidate(operatorVerificationProvider);
  }
}

/// Machine categories an operator can be verified for (category slugs).
const operatorSkillOptions = {
  'excavator': 'Excavator',
  'backhoe-loader': 'Backhoe loader',
  'crane': 'Crane',
  'roller': 'Roller',
  'transit-mixer': 'Transit mixer',
  'tipper': 'Tipper',
  'telehandler': 'Telehandler',
  'dozer-grader': 'Dozer / grader',
};

/// Licence, experience, skills and availability (returns the request body).
class OperatorProfileForm extends StatefulWidget {
  const OperatorProfileForm({super.key, this.initial});
  final OperatorVerification? initial;
  @override
  State<OperatorProfileForm> createState() => _OperatorProfileFormState();
}

class _OperatorProfileFormState extends State<OperatorProfileForm> {
  final _form = GlobalKey<FormState>();
  final _licence = TextEditingController();
  late final _class = TextEditingController(text: widget.initial?.licenceClass);
  late final _expiry = TextEditingController(
    text: widget.initial?.licenceExpiresOn,
  );
  late final _years = TextEditingController(
    text: '${widget.initial?.experienceYears ?? ''}',
  );
  late final _start = TextEditingController(
    text: widget.initial?.availability?.start ?? '08:00',
  );
  late final _end = TextEditingController(
    text: widget.initial?.availability?.end ?? '18:00',
  );
  late final Set<String> _skills = {...?widget.initial?.skills};
  late final Set<String> _days = {
    ...(widget.initial?.availability?.days ??
        const ['mon', 'tue', 'wed', 'thu', 'fri', 'sat']),
  };
  String? _error;

  @override
  void dispose() {
    for (final c in [_licence, _class, _expiry, _years, _start, _end]) {
      c.dispose();
    }
    super.dispose();
  }

  static final _time = RegExp(r'^([01]\d|2[0-3]):[0-5]\d$');

  @override
  Widget build(BuildContext context) {
    const gap = Gap(AppSpacing.md);
    Widget chips(Map<String, String> options, Set<String> selected) => Wrap(
      spacing: 6,
      runSpacing: 6,
      children: [
        for (final e in options.entries)
          FilterChip(
            label: Text(e.value),
            selected: selected.contains(e.key),
            onSelected: (on) => setState(
              () => on ? selected.add(e.key) : selected.remove(e.key),
            ),
          ),
      ],
    );
    return Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppTextField(
            key: const Key('licence-number'),
            label: 'Driving licence number',
            controller: _licence,
            requiredField: true,
            helper: widget.initial?.licenceLast4 == null
                ? 'Stored encrypted'
                : 'Re-enter to confirm (saved: ••••${widget.initial!.licenceLast4})',
            validator: (v) => (v ?? '').trim().length < 8
                ? 'Enter the full licence number'
                : null,
          ),
          gap,
          AppTextField(
            key: const Key('licence-class'),
            label: 'Licence class',
            controller: _class,
            hint: 'HMV, HTV, earthmover…',
            requiredField: true,
            validator: (v) => (v ?? '').trim().length < 2 ? 'Required' : null,
          ),
          gap,
          AppDateField(
            key: const Key('licence-expiry'),
            label: 'Valid until',
            controller: _expiry,
            requiredField: true,
            allowPast: false,
          ),
          gap,
          AppTextField(
            key: const Key('experience'),
            label: 'Years of experience',
            controller: _years,
            keyboardType: TextInputType.number,
            requiredField: true,
            validator: (v) {
              final n = int.tryParse((v ?? '').trim());
              return n == null || n < 0 || n > 60 ? 'Enter 0–60' : null;
            },
          ),
          gap,
          Text(
            'Machines you operate',
            style: Theme.of(context).textTheme.titleSmall,
          ),
          const Gap(AppSpacing.sm),
          chips(operatorSkillOptions, _skills),
          gap,
          Text('Available on', style: Theme.of(context).textTheme.titleSmall),
          const Gap(AppSpacing.sm),
          chips(_dayLabels, _days),
          gap,
          Row(
            children: [
              Expanded(
                child: AppTextField(
                  key: const Key('start-time'),
                  label: 'From',
                  controller: _start,
                  hint: 'HH:MM',
                  validator: (v) =>
                      _time.hasMatch((v ?? '').trim()) ? null : 'HH:MM',
                ),
              ),
              const Gap(AppSpacing.md),
              Expanded(
                child: AppTextField(
                  key: const Key('end-time'),
                  label: 'To',
                  controller: _end,
                  hint: 'HH:MM',
                  validator: (v) =>
                      _time.hasMatch((v ?? '').trim()) ? null : 'HH:MM',
                ),
              ),
            ],
          ),
          gap,
          if (_error != null) ...[
            const Gap(AppSpacing.sm),
            Text(_error!, style: const TextStyle(color: AppColors.red700)),
          ],
          const Gap(AppSpacing.lg),
          AppButton(
            key: const Key('save-operator-profile'),
            label: 'Save',
            onPressed: () {
              if (!_form.currentState!.validate()) return;
              final err = _skills.isEmpty
                  ? 'Choose at least one machine'
                  : _days.isEmpty
                  ? 'Choose at least one day'
                  : _start.text.trim().compareTo(_end.text.trim()) >= 0
                  ? 'The end time must be after the start time'
                  : null;
              if (err != null) {
                setState(() => _error = err);
                return;
              }
              Navigator.of(context).pop(<String, dynamic>{
                'licence_number': _licence.text.trim().toUpperCase(),
                'licence_class': _class.text.trim(),
                'licence_expires_on': _expiry.text.trim(),
                'experience_years': int.parse(_years.text.trim()),
                'skills': _skills.toList(),
                'availability': {
                  'days': [
                    for (final d in _dayLabels.keys)
                      if (_days.contains(d)) d,
                  ],
                  'start': _start.text.trim(),
                  'end': _end.text.trim(),
                },
              });
            },
          ),
        ],
      ),
    );
  }
}

class _NewQualification {
  const _NewQualification(this.body, this.file);
  final Json body;
  final PickedFile? file;
}

class _QualificationForm extends ConsumerStatefulWidget {
  const _QualificationForm();
  @override
  ConsumerState<_QualificationForm> createState() => _QualificationFormState();
}

class _QualificationFormState extends ConsumerState<_QualificationForm> {
  final _form = GlobalKey<FormState>();
  final _title = TextEditingController();
  final _issuer = TextEditingController();
  final _issued = TextEditingController();
  final _valid = TextEditingController();
  PickedFile? _file;

  @override
  void dispose() {
    for (final c in [_title, _issuer, _issued, _valid]) {
      c.dispose();
    }
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    const gap = Gap(AppSpacing.md);
    return Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppTextField(
            key: const Key('qualification-title'),
            label: 'Title',
            controller: _title,
            requiredField: true,
            validator: (v) => (v ?? '').trim().length < 2 ? 'Required' : null,
          ),
          gap,
          AppTextField(label: 'Issued by (optional)', controller: _issuer),
          gap,
          AppDateField(label: 'Issued on (optional)', controller: _issued, lastDate: DateTime.now()),
          gap,
          AppDateField(label: 'Valid until (optional)', controller: _valid),
          gap,
          OutlinedButton.icon(
            onPressed: () async {
              final f = await ref.read(documentPickerProvider).pick();
              if (f != null) setState(() => _file = f);
            },
            icon: const Icon(AppIcons.attach, size: 18),
            label: Text(
              _file?.name ?? 'Attach certificate (optional)',
              overflow: TextOverflow.ellipsis,
            ),
          ),
          const Gap(AppSpacing.lg),
          AppButton(
            key: const Key('save-qualification'),
            label: 'Add qualification',
            onPressed: () {
              if (!_form.currentState!.validate()) return;
              String? opt(TextEditingController c) =>
                  c.text.trim().isEmpty ? null : c.text.trim();
              Navigator.of(context).pop(
                _NewQualification({
                  'title': _title.text.trim(),
                  'issuer': ?opt(_issuer),
                  'issued_on': ?opt(_issued),
                  'valid_until': ?opt(_valid),
                }, _file),
              );
            },
          ),
        ],
      ),
    );
  }
}
