import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../data/verification_api.dart';
import '../data/verification_models.dart';
import 'customer_verification_page.dart' show stateCodes;
import 'verification_widgets.dart';

/// Owner (individual or company) verification: business and contact
/// details, address, identity and business documents, payout account
/// verified by the payment provider, and the review status.
class OwnerVerificationPage extends ConsumerWidget {
  const OwnerVerificationPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final v = ref.watch(ownerVerificationProvider);
    return AppPage(
      title: 'Business verification',
      topLevel: false,
      body: AsyncBody(
        value: v,
        onRetry: () => ref.invalidate(ownerVerificationProvider),
        builder: (o) => _OwnerBody(o: o),
      ),
    );
  }
}

class _OwnerBody extends ConsumerWidget {
  const _OwnerBody({required this.o});
  final OwnerVerification o;

  String? get _org => o.organizationId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final api = ref.read(verificationApiProvider);
    void refresh() => ref.invalidate(ownerVerificationProvider);
    final company = o.accountKind == 'company';
    final ready = o.checks.every((c) => c.done);

    final main = Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        SectionCard(
          title: 'Business details',
          icon: AppIcons.building,
          trailing: TextButton(key: const Key('edit-business'), onPressed: () => _editBusiness(context, ref), child: Text(o.hasBusinessDetails ? 'Edit' : 'Add')),
          child: o.hasBusinessDetails
              ? Column(
                  children: [
                    InfoRow(label: 'Account', value: company ? 'Company' : 'Individual'),
                    InfoRow(label: 'Legal name', value: o.legalName),
                    InfoRow(label: 'Business name', value: o.businessName),
                    InfoRow(label: 'PAN', value: '••••••${o.panLast4}'),
                    if (o.gstin != null) InfoRow(label: 'GSTIN', value: o.gstin!),
                    if (o.cin != null) InfoRow(label: 'CIN', value: o.cin!),
                    if (o.udyam != null) InfoRow(label: 'Udyam', value: o.udyam!),
                    InfoRow(label: 'Contact', value: [o.contactName, o.contactPhone, o.contactEmail].whereType<String>().join(' · ')),
                    if (o.address != null) InfoRow(label: 'Address', value: o.address!.oneLine),
                  ],
                )
              : const Text('Add your legal name, PAN, contact details and business address.'),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Identity documents',
          subtitle: 'PAN, passport, voter ID or driving licence of the proprietor / director',
          icon: AppIcons.userCheck,
          trailing: o.editable
              ? TextButton(
                  key: const Key('add-identity-doc'),
                  onPressed: () => _addDoc(context, ref, DocTypes.ownerIdentity, UploadPurpose.identityDocument),
                  child: const Text('Add'),
                )
              : null,
          child: DocumentList(
            documents: o.documents.where((d) => DocTypes.ownerIdentity.contains(d.docType)).toList(),
            onDelete: o.editable ? (d) => _deleteDoc(context, ref, d) : null,
          ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Business documents',
          subtitle: company ? 'GST, registration, Udyam, incorporation or address proof' : 'Optional for individual owners',
          icon: AppIcons.fileCheck,
          trailing: o.editable
              ? TextButton(
                  key: const Key('add-business-doc'),
                  onPressed: () => _addDoc(context, ref, DocTypes.ownerBusiness, UploadPurpose.businessDocument),
                  child: const Text('Add'),
                )
              : null,
          child: DocumentList(
            documents: o.documents.where((d) => DocTypes.ownerBusiness.contains(d.docType)).toList(),
            onDelete: o.editable ? (d) => _deleteDoc(context, ref, d) : null,
          ),
        ),
        const Gap(AppSpacing.md),
        SectionCard(
          title: 'Payout account',
          subtitle: 'Verified by our payment partner before any payout',
          icon: AppIcons.banknote,
          trailing: o.editable
              ? TextButton(key: const Key('set-bank'), onPressed: () => _setBank(context, ref), child: Text(o.bank == null ? 'Add' : 'Change'))
              : null,
          child: o.bank == null
              ? const Text('Add the account your rental earnings are paid into.')
              : Column(
                  children: [
                    InfoRow(label: 'Account holder', value: o.bank!.holder),
                    InfoRow(label: 'Account', value: '•••• ${o.bank!.last4}'),
                    InfoRow(label: 'IFSC', value: o.bank!.ifsc),
                    InfoRow(label: 'Status', value: o.bank!.status == 'verified' ? 'Verified by payment provider' : o.bank!.status),
                  ],
                ),
        ),
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
            label: o.application.status == ApplicationStatus.rejected ? 'Resubmit for review' : 'Submit for review',
            icon: AppIcons.send,
            expand: true,
            onPressed: ready
                ? () async {
                    if (await runVerificationAction(context, () => api.submitOwner(_org), success: 'Submitted for review')) refresh();
                  }
                : null,
          ),
      ],
    );

    return PageBody(
      children: [
        Wrap(
          spacing: AppSpacing.sm,
          runSpacing: AppSpacing.sm,
          crossAxisAlignment: WrapCrossAlignment.center,
          children: [
            ApplicationStatusBadge(o.application.status),
            StatusBadge(label: 'KYC: ${o.kycStatus}', tone: o.kycStatus == 'verified' ? AppTone.success : AppTone.neutral),
          ],
        ),
        const Gap(AppSpacing.sm),
        ApplicationBanner(application: o.application, subject: 'business'),
        const Gap(AppSpacing.md),
        SplitLayout(main: main, side: side, sideWidth: 320),
      ],
    );
  }

  Future<void> _addDoc(BuildContext context, WidgetRef ref, List<String> types, UploadPurpose purpose) async {
    final doc = await showAddDocumentSheet(context, docTypes: types);
    if (doc == null || !context.mounted) return;
    final api = ref.read(verificationApiProvider);
    final ok = await runVerificationAction(context, () async {
      final id = await api.upload(purpose, doc.file);
      await api.addOwnerDocument(_org, docType: doc.docType, uploadId: id, number: doc.number, expiresOn: doc.expiresOn);
    }, success: '${DocTypes.label(doc.docType)} added');
    if (ok) ref.invalidate(ownerVerificationProvider);
  }

  Future<void> _deleteDoc(BuildContext context, WidgetRef ref, VDocument d) async {
    final yes = await showConfirmDialog(
      context,
      title: 'Remove ${d.label}?',
      message: 'You can upload it again later.',
      confirmLabel: 'Remove',
      tone: AppTone.danger,
    );
    if (!yes || !context.mounted) return;
    if (await runVerificationAction(context, () => ref.read(verificationApiProvider).deleteOwnerDocument(_org, d.id), success: 'Removed')) {
      ref.invalidate(ownerVerificationProvider);
    }
  }

  Future<void> _editBusiness(BuildContext context, WidgetRef ref) async {
    var current = o;
    if (!o.editable) {
      final go = await showDialog<bool>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('Edit business details?'),
          content: const Text('Your business goes back to Pending and must be submitted and verified again after you save your changes.'),
          actions: [
            TextButton(onPressed: () => Navigator.of(ctx).pop(false), child: const Text('Cancel')),
            FilledButton(key: const Key('confirm-reopen'), onPressed: () => Navigator.of(ctx).pop(true), child: const Text('Edit')),
          ],
        ),
      );
      if (go != true || !context.mounted) return;
      OwnerVerification? reopened;
      final ok = await runVerificationAction(context, () async => reopened = await ref.read(verificationApiProvider).reopenOwner(_org));
      if (!ok || reopened == null || !context.mounted) return;
      current = reopened!;
      ref.invalidate(ownerVerificationProvider);
    }
    final body = await showAppSheet<Json>(
      context,
      title: 'Business details',
      builder: (_) => BusinessForm(initial: current),
    );
    if (body == null || !context.mounted) return;
    if (await runVerificationAction(context, () => ref.read(verificationApiProvider).saveBusiness(_org, body), success: 'Business details saved')) {
      ref.invalidate(ownerVerificationProvider);
    }
  }

  Future<void> _setBank(BuildContext context, WidgetRef ref) async {
    final body = await showAppSheet<Json>(
      context,
      title: 'Payout account',
      subtitle: 'We verify the account with our payment partner (a small test credit). Only the last 4 digits are kept visible.',
      builder: (_) => const BankAccountForm(),
    );
    if (body == null || !context.mounted) return;
    if (await runVerificationAction(context, () => ref.read(verificationApiProvider).setBankAccount(_org, body), success: 'Payout account verified')) {
      ref.invalidate(ownerVerificationProvider);
    }
  }
}

const _orgTypes = {
  'individual': 'Individual',
  'proprietorship': 'Sole proprietorship',
  'partnership': 'Partnership',
  'llp': 'LLP',
  'private_limited': 'Private limited',
  'public_limited': 'Public limited',
  'other': 'Other',
};

/// Business, contact and address details (returns the request body).
class BusinessForm extends StatefulWidget {
  const BusinessForm({super.key, this.initial});
  final OwnerVerification? initial;
  @override
  State<BusinessForm> createState() => _BusinessFormState();
}

class _BusinessFormState extends State<BusinessForm> {
  final _form = GlobalKey<FormState>();
  late String _kind = widget.initial?.accountKind ?? 'individual';
  late String _type = widget.initial?.orgType ?? 'individual';
  late final _legal = TextEditingController(text: widget.initial?.legalName);
  late final _name = TextEditingController(text: widget.initial?.businessName);
  final _pan = TextEditingController();
  late final _gstin = TextEditingController(text: widget.initial?.gstin);
  late final _cin = TextEditingController(text: widget.initial?.cin);
  late final _udyam = TextEditingController(text: widget.initial?.udyam);
  late final _contact = TextEditingController(text: widget.initial?.contactName);
  late final _phone = TextEditingController(text: widget.initial?.contactPhone ?? '+91');
  late final _email = TextEditingController(text: widget.initial?.contactEmail);
  late final _line1 = TextEditingController(text: widget.initial?.address?.line1);
  late final _city = TextEditingController(text: widget.initial?.address?.city);
  late final _pin = TextEditingController(text: widget.initial?.address?.pincode);
  late String _state = widget.initial?.address?.stateCode ?? '27';

  List<String> get _types =>
      _kind == 'individual' ? const ['individual', 'proprietorship'] : const ['partnership', 'llp', 'private_limited', 'public_limited', 'other'];

  @override
  void dispose() {
    for (final c in [_legal, _name, _pan, _gstin, _cin, _udyam, _contact, _phone, _email, _line1, _city, _pin]) {
      c.dispose();
    }
    super.dispose();
  }

  String? _req(String? v, [int min = 2]) => (v ?? '').trim().length < min ? 'Required' : null;

  @override
  Widget build(BuildContext context) {
    final limited = _type == 'private_limited' || _type == 'public_limited';
    const gap = Gap(AppSpacing.md);
    return Form(
      key: _form,
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppChoiceChips<String>(
            options: const ['individual', 'company'],
            selected: _kind,
            labelOf: (k) => k == 'individual' ? 'Individual' : 'Company',
            onSelected: (k) => setState(() {
              _kind = k;
              if (!_types.contains(_type)) _type = _types.first;
            }),
          ),
          gap,
          AppDropdown<String>(
            key: ValueKey(_kind),
            label: 'Business type',
            value: _type,
            items: _types,
            itemLabel: (t) => _orgTypes[t] ?? t,
            onChanged: (t) => setState(() => _type = t ?? _type),
          ),
          gap,
          AppTextField(key: const Key('legal-name'), label: 'Legal name (as on PAN)', controller: _legal, requiredField: true, validator: _req),
          gap,
          AppTextField(key: const Key('business-name'), label: 'Business name shown to customers', controller: _name, requiredField: true, validator: _req),
          gap,
          AppTextField(
            key: const Key('pan'),
            label: 'PAN',
            controller: _pan,
            requiredField: true,
            helper: widget.initial?.panLast4 == null ? 'Stored encrypted' : 'Re-enter to confirm (saved: ••••••${widget.initial!.panLast4})',
            validator: (v) => RegExp(r'^[A-Z]{5}[0-9]{4}[A-Z]$').hasMatch((v ?? '').trim().toUpperCase()) ? null : 'Enter a valid PAN (ABCDE1234F)',
          ),
          if (_kind == 'company') ...[
            gap,
            AppTextField(
              label: 'GSTIN (optional)',
              controller: _gstin,
              validator: (v) {
                final g = (v ?? '').trim().toUpperCase();
                if (g.isEmpty) return null;
                if (g.length != 15) return 'GSTIN has 15 characters';
                if (g.substring(2, 12) != _pan.text.trim().toUpperCase()) return 'GSTIN must belong to this PAN';
                return null;
              },
            ),
            gap,
            AppTextField(
              label: limited ? 'CIN' : 'CIN (optional)',
              controller: _cin,
              requiredField: limited,
              validator: (v) => limited && (v ?? '').trim().length != 21 ? 'Enter the 21-character CIN' : null,
            ),
          ],
          gap,
          AppTextField(label: 'Udyam number (optional)', controller: _udyam, hint: 'UDYAM-MH-00-0000000'),
          gap,
          AppTextField(key: const Key('contact-name'), label: 'Contact person', controller: _contact, requiredField: true, validator: _req),
          gap,
          AppTextField(
            key: const Key('contact-phone'),
            label: 'Contact mobile',
            controller: _phone,
            keyboardType: TextInputType.phone,
            requiredField: true,
            validator: (v) => RegExp(r'^\+[1-9]\d{9,14}$').hasMatch((v ?? '').trim()) ? null : 'Use the +91 format',
          ),
          gap,
          AppTextField(
            key: const Key('contact-email'),
            label: 'Contact email',
            controller: _email,
            keyboardType: TextInputType.emailAddress,
            requiredField: true,
            validator: (v) => RegExp(r'^[^@\s]+@[^@\s]+\.[^@\s]+$').hasMatch((v ?? '').trim()) ? null : 'Enter a valid email',
          ),
          gap,
          Text('Business address', style: Theme.of(context).textTheme.titleSmall),
          gap,
          AppTextField(key: const Key('address-line1'), label: 'Address', controller: _line1, requiredField: true, validator: (v) => _req(v, 3)),
          gap,
          AppTextField(key: const Key('address-city'), label: 'City', controller: _city, requiredField: true, validator: _req),
          gap,
          AppTextField(
            key: const Key('address-pin'),
            label: 'PIN code',
            controller: _pin,
            keyboardType: TextInputType.number,
            requiredField: true,
            validator: (v) => RegExp(r'^[1-9]\d{5}$').hasMatch((v ?? '').trim()) ? null : 'Enter a 6-digit PIN code',
          ),
          gap,
          AppDropdown<String>(
            label: 'State',
            value: stateCodes.containsKey(_state) ? _state : '27',
            items: stateCodes.keys.toList(),
            itemLabel: (c) => stateCodes[c] ?? c,
            onChanged: (v) => setState(() => _state = v ?? _state),
          ),
          const Gap(AppSpacing.lg),
          AppButton(
            key: const Key('save-business'),
            label: 'Save details',
            onPressed: () {
              if (!_form.currentState!.validate()) return;
              String? opt(TextEditingController c) => c.text.trim().isEmpty ? null : c.text.trim().toUpperCase();
              Navigator.of(context).pop(<String, dynamic>{
                'account_kind': _kind,
                'org_type': _type,
                'legal_name': _legal.text.trim(),
                'business_name': _name.text.trim(),
                'pan': _pan.text.trim().toUpperCase(),
                if (_kind == 'company') 'gstin': ?opt(_gstin),
                if (_kind == 'company') 'cin': ?opt(_cin),
                'udyam_number': ?opt(_udyam),
                'contact_name': _contact.text.trim(),
                'contact_phone': _phone.text.trim(),
                'contact_email': _email.text.trim(),
                'business_address': {'line1': _line1.text.trim(), 'city': _city.text.trim(), 'state_code': _state, 'pincode': _pin.text.trim()},
              });
            },
          ),
        ],
      ),
    );
  }
}

/// Payout account form (returns the request body).
class BankAccountForm extends StatefulWidget {
  const BankAccountForm({super.key});
  @override
  State<BankAccountForm> createState() => _BankAccountFormState();
}

class _BankAccountFormState extends State<BankAccountForm> {
  final _form = GlobalKey<FormState>();
  final _holder = TextEditingController();
  final _number = TextEditingController();
  final _confirm = TextEditingController();
  final _ifsc = TextEditingController();

  @override
  void dispose() {
    for (final c in [_holder, _number, _confirm, _ifsc]) {
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
            key: const Key('bank-holder'),
            label: 'Account holder name',
            controller: _holder,
            requiredField: true,
            validator: (v) => (v ?? '').trim().length < 2 ? 'Required' : null,
          ),
          gap,
          AppTextField(
            key: const Key('bank-number'),
            label: 'Account number',
            controller: _number,
            obscureText: true,
            keyboardType: TextInputType.number,
            requiredField: true,
            validator: (v) => RegExp(r'^\d{9,18}$').hasMatch((v ?? '').trim()) ? null : '9 to 18 digits',
          ),
          gap,
          AppTextField(
            key: const Key('bank-confirm'),
            label: 'Confirm account number',
            controller: _confirm,
            keyboardType: TextInputType.number,
            requiredField: true,
            validator: (v) => (v ?? '').trim() == _number.text.trim() ? null : 'Account numbers do not match',
          ),
          gap,
          AppTextField(
            key: const Key('bank-ifsc'),
            label: 'IFSC',
            controller: _ifsc,
            requiredField: true,
            validator: (v) => RegExp(r'^[A-Z]{4}0[A-Z0-9]{6}$').hasMatch((v ?? '').trim().toUpperCase()) ? null : 'Enter a valid IFSC (HDFC0001234)',
          ),
          const Gap(AppSpacing.lg),
          AppButton(
            key: const Key('save-bank'),
            label: 'Verify account',
            icon: AppIcons.shield,
            onPressed: () {
              if (!_form.currentState!.validate()) return;
              Navigator.of(context).pop(<String, dynamic>{
                'account_holder_name': _holder.text.trim(),
                'account_number': _number.text.trim(),
                'confirm_account_number': _confirm.text.trim(),
                'ifsc': _ifsc.text.trim().toUpperCase(),
              });
            },
          ),
        ],
      ),
    );
  }
}
