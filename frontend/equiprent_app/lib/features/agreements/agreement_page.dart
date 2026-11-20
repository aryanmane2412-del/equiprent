import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../../core/auth/auth_controller.dart';
import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../documents/document_pages.dart';
import '../verification/data/verification_api.dart' show linkOpenerProvider;

/// The rental agreement of a booking: who, what, when and for how much,
/// the full terms, acceptance status, accepting (or e-signing through a
/// provider), earlier versions, the document history and the PDF.
class AgreementPage extends ConsumerWidget {
  const AgreementPage({super.key, required this.side, required this.bookingId});
  final BookingSide side;
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final ag = ref.watch(agreementProvider((side, bookingId)));
    return AppPage(
      title: 'Rental agreement',
      subtitle: ag.value?.reference,
      topLevel: false,
      actions: [
        if (ag.hasValue && ag.value!.terms.isNotEmpty)
          AppIconButton(key: const Key('agreement-pdf'), icon: AppIcons.download, tooltip: 'Download PDF', onPressed: () => _download(context, ref)),
      ],
      body: AsyncBody(
        value: ag,
        onRetry: () => ref.invalidate(agreementProvider((side, bookingId))),
        builder: (a) => PageBody(
          children: [
            _StatusBanner(a: a),
            const Gap(AppSpacing.sm),
            _VersionBar(side: side, bookingId: bookingId, a: a),
            const Gap(AppSpacing.md),
            ResponsiveGrid(
              columns: const Responsive(compact: 1, medium: 2),
              children: [
                if (a.owner != null) _PartyCard(title: 'Owner (lessor)', party: a.owner!, signature: a.signatureOf('owner')),
                if (a.customer != null) _PartyCard(title: 'Customer (lessee)', party: a.customer!, signature: a.signatureOf('customer')),
              ],
            ),
            const Gap(AppSpacing.md),
            SectionCard(
              title: 'Terms',
              subtitle: 'Version ${a.version} · fingerprint ${shortHash(a.termsSha256)} · ${a.legalTerms?.version ?? a.templateVersion}',
              icon: AppIcons.shield,
              child: a.terms.isEmpty
                  ? const Text('The booking changed after this agreement was prepared, so it can no longer be accepted. Contact support.')
                  : SelectableText(a.terms, key: const Key('agreement-terms'), style: const TextStyle(height: 1.45, fontSize: 13)),
            ),
            if (a.canSign) ...[const Gap(AppSpacing.md), _SignForm(side: side, bookingId: bookingId, agreement: a)],
            const Gap(AppSpacing.md),
            if (a.assuranceNote.isNotEmpty) ...[
              Text(
                a.assuranceNote,
                key: const Key('assurance-note'),
                style: Theme.of(context).textTheme.bodySmall?.copyWith(color: AppColors.slate500),
              ),
              const Gap(AppSpacing.md),
            ],
            AppButton(
              label: a.executed ? (a.usesElectronicSignature ? 'Download signed PDF' : 'Download accepted PDF') : 'Download draft PDF',
              icon: AppIcons.download,
              variant: AppButtonVariant.secondary,
              expand: true,
              onPressed: a.terms.isEmpty ? null : () => _download(context, ref),
            ),
          ],
        ),
      ),
    );
  }

  Future<void> _download(BuildContext context, WidgetRef ref) async {
    try {
      final link = await ref.read(commsApiProvider).agreementDocument(ref.read(sideOrgProvider(side)), bookingId);
      final opened = await ref.read(linkOpenerProvider)(link.url);
      if (!opened && context.mounted) AppToast.error(context, 'Could not open the PDF.');
    } catch (e) {
      if (context.mounted) AppToast.error(context, describeError(e));
    }
  }
}

class _StatusBanner extends StatelessWidget {
  const _StatusBanner({required this.a});
  final Agreement a;

  @override
  Widget build(BuildContext context) {
    final (tone, icon, text) = switch (a.status) {
      'executed' => (AppTone.success, AppIcons.check, 'Both parties are on record. The final PDF is stored securely.'),
      'outdated' => (AppTone.danger, AppIcons.warning, 'This agreement is out of date and cannot be accepted.'),
      _ when a.canSign => (
        AppTone.warning,
        AppIcons.shield,
        a.usesElectronicSignature ? 'Please read the terms and sign for your side.' : 'Please read the terms and accept them for your side.',
      ),
      'partially_signed' => (AppTone.info, AppIcons.clock, 'Waiting for the other party.'),
      _ => (AppTone.info, AppIcons.clock, 'Waiting for both parties.'),
    };
    return InlineBanner(key: const Key('agreement-status'), tone: tone, icon: icon, title: a.statusLabel, message: text);
  }
}

class _PartyCard extends StatelessWidget {
  const _PartyCard({required this.title, required this.party, this.signature});
  final String title;
  final AgreementParty party;
  final AgreementSignature? signature;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    return AppCard(
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text(title, style: theme.textTheme.labelMedium?.copyWith(color: AppColors.slate500)),
              ),
              signature == null
                  ? const StatusBadge(label: 'Pending', tone: AppTone.neutral)
                  : StatusBadge(label: signature!.isElectronicSignature ? 'Signed' : 'Accepted', tone: AppTone.success, icon: AppIcons.check),
            ],
          ),
          const Gap(4),
          Text(party.legalName, style: theme.textTheme.titleSmall),
          if (party.gstin != null) Text('GSTIN ${party.gstin}', style: theme.textTheme.bodySmall),
          if (party.address != null && party.address!.isNotEmpty) Text(party.address!, style: theme.textTheme.bodySmall),
          if (signature != null) ...[
            const Gap(AppSpacing.sm),
            Text(
              '${signature!.isElectronicSignature ? 'Signed' : 'Accepted'} by ${signature!.signerName}${signature!.designation == null ? '' : ', ${signature!.designation}'} on ${Fmt.dateTime(signature!.signedAt)}'
              '${signature!.isElectronicSignature ? ' via ${signature!.provider}' : ''}',
              style: theme.textTheme.bodySmall?.copyWith(color: AppColors.green700),
            ),
          ],
        ],
      ),
    );
  }
}

class _SignForm extends ConsumerStatefulWidget {
  const _SignForm({required this.side, required this.bookingId, required this.agreement});
  final BookingSide side;
  final String bookingId;
  final Agreement agreement;
  @override
  ConsumerState<_SignForm> createState() => _SignFormState();
}

class _SignFormState extends ConsumerState<_SignForm> {
  late final _name = TextEditingController(text: ref.read(authControllerProvider).user?.fullName ?? '');
  final _designation = TextEditingController();
  bool _consent = false;
  bool _busy = false;

  @override
  void dispose() {
    _name.dispose();
    _designation.dispose();
    super.dispose();
  }

  bool get _valid => _consent && _name.text.trim().length >= 2;

  Future<void> _sign() async {
    setState(() => _busy = true);
    try {
      final res = await ref
          .read(commsApiProvider)
          .signAgreement(
            ref.read(sideOrgProvider(widget.side)),
            widget.bookingId,
            termsSha256: widget.agreement.termsSha256,
            signerName: _name.text.trim(),
            designation: _designation.text.trim().isEmpty ? null : _designation.text.trim(),
          );
      if (!mounted) return;
      if (res.redirectUrl != null) {
        await ref.read(linkOpenerProvider)(res.redirectUrl!);
        if (mounted) AppToast.show(context, 'Finish signing in the e-signature window, then come back here.');
      } else {
        AppToast.success(context, res.agreement.executed ? 'Accepted. The agreement is complete.' : 'Accepted. Waiting for the other party.');
      }
      ref.invalidate(agreementProvider((widget.side, widget.bookingId)));
    } catch (e) {
      if (mounted) AppToast.error(context, describeError(e));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final external = widget.agreement.usesElectronicSignature;
    return SectionCard(
      title: external ? 'Sign for your side' : 'Accept for your side',
      subtitle: external
          ? 'You will finish signing with ${widget.agreement.signatureProvider}.'
          : 'Your typed name and this consent are stored as an acceptance record. This is not a digital signature.',
      icon: AppIcons.edit,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          AppTextField(key: const Key('signer-name'), label: 'Full name', controller: _name, requiredField: true, onChanged: (_) => setState(() {})),
          const Gap(AppSpacing.sm),
          AppTextField(key: const Key('signer-designation'), label: 'Designation (optional)', hint: 'e.g. Director, Site engineer', controller: _designation),
          const Gap(AppSpacing.sm),
          CheckboxListTile(
            key: const Key('sign-consent'),
            contentPadding: EdgeInsets.zero,
            controlAffinity: ListTileControlAffinity.leading,
            value: _consent,
            onChanged: (v) => setState(() => _consent = v ?? false),
            title: Text(widget.agreement.consentText),
          ),
          const Gap(AppSpacing.sm),
          AppButton(
            key: const Key('sign-agreement'),
            label: external ? 'Continue to e-sign' : 'Accept agreement',
            icon: AppIcons.edit,
            variant: AppButtonVariant.accent,
            isLoading: _busy,
            onPressed: _valid && !_busy ? _sign : null,
          ),
        ],
      ),
    );
  }
}

/// Version, terms and review information with links to earlier versions and
/// the document history.
class _VersionBar extends StatelessWidget {
  const _VersionBar({required this.side, required this.bookingId, required this.a});
  final BookingSide side;
  final String bookingId;
  final Agreement a;

  @override
  Widget build(BuildContext context) {
    final lt = a.legalTerms;
    return Wrap(
      spacing: AppSpacing.sm,
      runSpacing: AppSpacing.sm,
      crossAxisAlignment: WrapCrossAlignment.center,
      children: [
        StatusBadge(key: const Key('agreement-version'), label: 'Version ${a.version}', tone: AppTone.info),
        if (lt != null)
          StatusBadge(
            key: const Key('terms-review'),
            label: lt.reviewed ? 'Terms reviewed by ${lt.reviewedBy ?? 'counsel'}' : 'Terms not yet legally reviewed',
            tone: lt.reviewed ? AppTone.success : AppTone.warning,
            icon: lt.reviewed ? AppIcons.verified : AppIcons.warning,
          ),
        if (a.version > 1)
          AppButton(
            key: const Key('agreement-versions'),
            label: 'Versions',
            icon: AppIcons.history,
            size: AppButtonSize.small,
            variant: AppButtonVariant.ghost,
            onPressed: () => context.push(DocumentRoutes.versions(side, bookingId)),
          ),
        AppButton(
          key: const Key('document-history'),
          label: 'History',
          icon: AppIcons.history,
          size: AppButtonSize.small,
          variant: AppButtonVariant.ghost,
          onPressed: () => context.push(DocumentRoutes.history(side, bookingId)),
        ),
      ],
    );
  }
}
