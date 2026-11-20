import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import '../comms/data/comms_api.dart';
import '../comms/data/comms_models.dart';
import '../comms/presentation/comms_widgets.dart';
import '../comms/presentation/ui.dart';
import '../verification/data/verification_api.dart' show linkOpenerProvider;

/// Route helpers for the booking document screens (customer and owner).
abstract final class DocumentRoutes {
  static String _base(BookingSide side) => side == BookingSide.owner ? '/owner' : '/customer';
  static String versions(BookingSide side, String bookingId) => '${_base(side)}/bookings/$bookingId/agreement/versions';
  static String version(BookingSide side, String bookingId, int v) => '${_base(side)}/bookings/$bookingId/agreement/versions/$v';
  static String history(BookingSide side, String bookingId) => '${_base(side)}/bookings/$bookingId/documents';
  static String invoice(BookingSide side, String invoiceId) => '${_base(side)}/invoices/$invoiceId';
}

/// Opens a short-lived PDF link from [fetch], reporting failures.
Future<void> openDocument(BuildContext context, WidgetRef ref, Future<String> Function() fetch) async {
  try {
    final url = await fetch();
    final opened = await ref.read(linkOpenerProvider)(url);
    if (!opened && context.mounted) AppToast.error(context, 'Could not open the PDF.');
  } catch (e) {
    if (context.mounted) AppToast.error(context, describeError(e));
  }
}

String shortHash(String h) => h.length > 12 ? '${h.substring(0, 12)}…' : h;

// ---- versions ----------------------------------------------------------------------------------

/// Every issued version of the booking's agreement, newest first.
class AgreementVersionsPage extends ConsumerWidget {
  const AgreementVersionsPage({super.key, required this.side, required this.bookingId});
  final BookingSide side;
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final key = (side, bookingId);
    final versions = ref.watch(agreementVersionsProvider(key));
    return AppPage(
      title: 'Agreement versions',
      topLevel: false,
      body: AsyncBody(
        value: versions,
        onRetry: () => ref.invalidate(agreementVersionsProvider(key)),
        builder: (list) => list.isEmpty
            ? const EmptyState(icon: AppIcons.fileCheck, title: 'No agreement yet', message: 'The agreement is issued once the owner accepts the booking.')
            : PageBody(
                children: [
                  const InlineBanner(
                    tone: AppTone.info,
                    icon: AppIcons.info,
                    title: 'Every version is kept',
                    message: 'A new version is issued when the rental dates or price change. Earlier versions and their acceptance records stay unchanged.',
                  ),
                  const Gap(AppSpacing.md),
                  for (final v in list) ...[
                    _VersionCard(v: v, onTap: () => context.push(DocumentRoutes.version(side, bookingId, v.version))),
                    const Gap(AppSpacing.sm),
                  ],
                ],
              ),
      ),
    );
  }
}

class _VersionCard extends StatelessWidget {
  const _VersionCard({required this.v, required this.onTap});
  final AgreementVersionInfo v;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final (label, tone) = switch (v.status) {
      'executed' => ('Accepted by both', AppTone.success),
      'superseded' => ('Superseded', AppTone.neutral),
      _ => ('Current', AppTone.info),
    };
    return AppCard(
      key: Key('version-${v.version}'),
      onTap: onTap,
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(child: Text('Version ${v.version}', style: theme.textTheme.titleSmall)),
              StatusBadge(label: label, tone: tone),
            ],
          ),
          const Gap(4),
          Text(
            'Issued ${Fmt.dateTime(v.createdAt)} · terms ${v.legalTermsVersion?.isNotEmpty == true ? v.legalTermsVersion : '—'}',
            style: theme.textTheme.bodySmall,
          ),
          Text('Fingerprint ${shortHash(v.termsSha256)}', style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
          if (v.customerAcceptedAt != null) Text('Customer accepted ${Fmt.dateTime(v.customerAcceptedAt!)}', style: theme.textTheme.bodySmall),
          if (v.ownerAcceptedAt != null) Text('Owner accepted ${Fmt.dateTime(v.ownerAcceptedAt!)}', style: theme.textTheme.bodySmall),
          if (v.supersedeReason != null) ...[
            const Gap(4),
            Text(
              v.supersedeReason!,
              style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate600, fontStyle: FontStyle.italic),
            ),
          ],
        ],
      ),
    );
  }
}

/// One version, read-only: its exact text, acceptance records and PDF.
class AgreementVersionPage extends ConsumerWidget {
  const AgreementVersionPage({super.key, required this.side, required this.bookingId, required this.version});
  final BookingSide side;
  final String bookingId;
  final int version;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final key = (side, bookingId, version);
    final ag = ref.watch(agreementVersionProvider(key));
    Future<void> download() => openDocument(context, ref, () async {
      final link = await ref.read(commsApiProvider).agreementVersionDocument(ref.read(sideOrgProvider(side)), bookingId, version);
      return link.url;
    });
    return AppPage(
      title: 'Agreement v$version',
      subtitle: ag.value?.reference,
      topLevel: false,
      actions: [AppIconButton(key: const Key('version-pdf'), icon: AppIcons.download, tooltip: 'Download PDF', onPressed: download)],
      body: AsyncBody(
        value: ag,
        onRetry: () => ref.invalidate(agreementVersionProvider(key)),
        builder: (a) => PageBody(
          children: [
            InlineBanner(
              tone: a.superseded ? AppTone.neutral : (a.executed ? AppTone.success : AppTone.info),
              icon: a.superseded ? AppIcons.history : AppIcons.fileCheck,
              title: a.statusLabel,
              message: a.supersedeReason ?? (a.current ? 'This is the current version.' : ''),
            ),
            const Gap(AppSpacing.md),
            if (a.signatures.isNotEmpty)
              SectionCard(
                title: 'Acceptance records',
                icon: AppIcons.userCheck,
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [for (final s in a.signatures) AcceptanceRecordTile(signature: s)],
                ),
              ),
            const Gap(AppSpacing.md),
            SectionCard(
              title: 'Exact text',
              subtitle: 'Fingerprint ${shortHash(a.termsSha256)} · ${a.templateVersion}',
              icon: AppIcons.shield,
              child: SelectableText(a.terms, key: const Key('version-terms'), style: const TextStyle(height: 1.45, fontSize: 13)),
            ),
            const Gap(AppSpacing.md),
            AppButton(label: 'Download PDF', icon: AppIcons.download, variant: AppButtonVariant.secondary, expand: true, onPressed: download),
          ],
        ),
      ),
    );
  }
}

/// Who accepted (or signed), when, how, and the consent they gave.
class AcceptanceRecordTile extends StatelessWidget {
  const AcceptanceRecordTile({super.key, required this.signature});
  final AgreementSignature signature;

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    final s = signature;
    final who = s.party == 'owner' ? 'Owner' : 'Customer';
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: AppSpacing.xs),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          IconTile(icon: s.isElectronicSignature ? AppIcons.edit : AppIcons.userCheck, tone: AppTone.success),
          const Gap(AppSpacing.ms),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Text('$who · ${s.signerName}${s.designation == null ? '' : ', ${s.designation}'}', style: theme.textTheme.titleSmall),
                Text(
                  s.isElectronicSignature
                      ? 'Electronic signature via ${s.provider} · ${Fmt.dateTime(s.signedAt)}'
                      : 'Acceptance record · ${Fmt.dateTime(s.signedAt)}',
                  style: theme.textTheme.bodySmall,
                ),
                if (s.consentText != null && s.consentText!.isNotEmpty)
                  Text(
                    '“${s.consentText}”',
                    style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500, fontStyle: FontStyle.italic),
                  ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

// ---- history -----------------------------------------------------------------------------------

/// The audit trail of the booking's agreements, acceptances and invoices.
class DocumentHistoryPage extends ConsumerWidget {
  const DocumentHistoryPage({super.key, required this.side, required this.bookingId});
  final BookingSide side;
  final String bookingId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final key = (side, bookingId);
    final history = ref.watch(documentHistoryProvider(key));
    return AppPage(
      title: 'Document history',
      topLevel: false,
      body: AsyncBody(
        value: history,
        onRetry: () => ref.invalidate(documentHistoryProvider(key)),
        builder: (list) => list.isEmpty
            ? const EmptyState(icon: AppIcons.history, title: 'No document activity yet', message: 'Issues, acceptances and downloads appear here.')
            : RefreshIndicator(
                onRefresh: () async => ref.invalidate(documentHistoryProvider(key)),
                child: PageBody(children: [for (final e in list) _HistoryRow(e: e)]),
              ),
      ),
    );
  }
}

class _HistoryRow extends StatelessWidget {
  const _HistoryRow({required this.e});
  final DocumentHistoryEntry e;

  @override
  Widget build(BuildContext context) {
    final (icon, tone) = switch (e.action) {
      'accepted' || 'acceptance_recorded' => (AppIcons.userCheck, AppTone.success),
      'downloaded' => (AppIcons.download, AppTone.neutral),
      'superseded' => (AppIcons.history, AppTone.warning),
      'issued' || 'created' => (e.document == 'invoice' ? AppIcons.receipt : AppIcons.fileCheck, AppTone.info),
      _ => (AppIcons.info, AppTone.neutral),
    };
    final who = [if (e.actorName != null) e.actorName!, Fmt.dateTime(e.at)].join(' · ');
    return ListRow(
      dense: true,
      leading: IconTile(icon: icon, tone: tone),
      title: e.description,
      subtitle: who,
    );
  }
}
