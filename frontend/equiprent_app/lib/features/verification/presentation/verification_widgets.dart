import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_exception.dart';
import '../../../core/widgets/widgets.dart';
import '../data/verification_api.dart';
import '../data/verification_models.dart';

/// Human-readable message for a failed call, including field problems.
String describeError(Object e) {
  if (e is ApiException) {
    final fields = [
      for (final f in e.fieldIssues)
        if (f.message != null) f.message!,
    ];
    return fields.isEmpty ? e.message : '${e.message}: ${fields.join('; ')}';
  }
  return 'Something went wrong. Please try again.';
}

/// Runs [action], showing a success toast or the error. Returns success.
Future<bool> runVerificationAction(BuildContext context, Future<void> Function() action, {String? success}) async {
  try {
    await action();
    if (context.mounted && success != null) AppToast.success(context, success);
    return true;
  } catch (e) {
    if (context.mounted) AppToast.error(context, describeError(e));
    return false;
  }
}

AppTone toneFor(ApplicationStatus s) => switch (s) {
  ApplicationStatus.approved => AppTone.success,
  ApplicationStatus.rejected => AppTone.danger,
  ApplicationStatus.submitted || ApplicationStatus.underReview => AppTone.info,
  ApplicationStatus.draft => AppTone.warning,
};

class ApplicationStatusBadge extends StatelessWidget {
  const ApplicationStatusBadge(this.status, {super.key});
  final ApplicationStatus status;
  @override
  Widget build(BuildContext context) => StatusBadge(label: status.label, tone: toneFor(status), dot: true);
}

/// The headline state of an owner/operator application.
class ApplicationBanner extends StatelessWidget {
  const ApplicationBanner({super.key, required this.application, required this.subject});
  final VApplication application;
  final String subject; // "business", "operator profile"

  @override
  Widget build(BuildContext context) {
    final (title, message) = switch (application.status) {
      ApplicationStatus.draft => ('Complete your $subject verification', 'Fill in every step below, then submit it for review.'),
      ApplicationStatus.submitted => ('Submitted for review', 'Our team usually reviews within one working day. Details are locked while we review.'),
      ApplicationStatus.underReview => ('Under review', 'A reviewer is checking your documents. Details are locked while we review.'),
      ApplicationStatus.approved => ('Verified', 'Your $subject is verified.'),
      ApplicationStatus.rejected => ('Changes needed', application.rejectionReason ?? 'Update the highlighted items and submit again.'),
    };
    return InlineBanner(
      title: title,
      message: message,
      tone: toneFor(application.status),
      icon: application.status == ApplicationStatus.approved ? AppIcons.verified : AppIcons.shield,
    );
  }
}

class ChecklistCard extends StatelessWidget {
  const ChecklistCard({super.key, required this.checks, this.title = 'Checklist'});
  final List<VCheck> checks;
  final String title;

  @override
  Widget build(BuildContext context) {
    final done = checks.where((c) => c.done).length;
    return SectionCard(
      title: title,
      icon: AppIcons.fileCheck,
      trailing: StatusBadge(label: '$done of ${checks.length}', tone: done == checks.length ? AppTone.success : AppTone.neutral),
      child: Column(
        children: [
          for (final c in checks)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 4),
              child: Row(
                children: [
                  Icon(c.done ? AppIcons.checkCircle : AppIcons.hourglass, size: 18, color: c.done ? AppColors.green700 : AppColors.slate500),
                  const Gap(AppSpacing.sm),
                  Expanded(child: Text(c.label, key: Key('check-${c.key}'))),
                ],
              ),
            ),
        ],
      ),
    );
  }
}

AppTone docTone(String status) => switch (status) {
  'verified' => AppTone.success,
  'rejected' => AppTone.danger,
  _ => AppTone.info,
};

String docStatusLabel(String status) => switch (status) {
  'verified' => 'Verified',
  'rejected' => 'Rejected',
  _ => 'Awaiting review',
};

/// Documents with a "view" action that fetches a fresh signed link (the
/// API checks access every time; nothing here is a permanent URL).
class DocumentList extends ConsumerWidget {
  const DocumentList({super.key, required this.documents, this.onDelete, this.trailing});
  final List<VDocument> documents;
  final ValueChanged<VDocument>? onDelete;
  final Widget Function(VDocument)? trailing;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    if (documents.isEmpty) {
      return const EmptyState(icon: AppIcons.upload, title: 'No documents yet', message: 'PDF, JPEG, PNG or WebP · up to 10 MB', compact: true);
    }
    final theme = Theme.of(context);
    // Details on top, status and actions in a wrapping row beneath: fits
    // 360 px phones even with reviewer actions.
    return Column(
      children: [
        for (final (i, d) in documents.indexed) ...[
          if (i > 0) const Divider(height: 1),
          Padding(
            padding: const EdgeInsets.symmetric(vertical: AppSpacing.sm),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Row(
                  children: [
                    Icon(d.contentType == 'application/pdf' ? AppIcons.receipt : AppIcons.image, size: 20, color: AppColors.slate600),
                    const Gap(AppSpacing.sm),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(
                            d.label + (d.numberLast4 == null ? '' : ' · ••${d.numberLast4}'),
                            style: theme.textTheme.titleSmall,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                          ),
                          if (d.expiresOn != null || d.reviewNote != null)
                            Text(
                              [if (d.expiresOn != null) 'Valid until ${d.expiresOn}', ?d.reviewNote].join(' · '),
                              style: theme.textTheme.bodySmall?.copyWith(color: d.status == 'rejected' ? AppColors.red700 : AppColors.slate500),
                              maxLines: 3,
                              overflow: TextOverflow.ellipsis,
                            ),
                        ],
                      ),
                    ),
                  ],
                ),
                Wrap(
                  spacing: 2,
                  crossAxisAlignment: WrapCrossAlignment.center,
                  children: [
                    StatusBadge(label: docStatusLabel(d.status), tone: docTone(d.status)),
                    IconButton(tooltip: 'View document', icon: const Icon(AppIcons.eye, size: 18), onPressed: () => openDocument(context, ref, d)),
                    if (onDelete != null && d.removable)
                      IconButton(tooltip: 'Remove', icon: const Icon(AppIcons.delete, size: 18), onPressed: () => onDelete!(d)),
                    ?trailing?.call(d),
                  ],
                ),
              ],
            ),
          ),
        ],
      ],
    );
  }
}

/// Opens a document through a short-lived signed link.
Future<void> openDocument(BuildContext context, WidgetRef ref, VDocument d) async {
  await runVerificationAction(context, () async {
    final link = d.download != null && d.download!.expiresAt.isAfter(DateTime.now().add(const Duration(seconds: 20)))
        ? d.download!
        : await ref.read(verificationApiProvider).documentLink(d.id);
    final ok = await ref.read(linkOpenerProvider)(link.url);
    if (!ok) throw const ApiException(kind: ApiErrorKind.unknown, code: 'OPEN', message: 'Could not open the document');
  });
}

/// Result of the add-document sheet.
class NewDocument {
  const NewDocument({required this.docType, required this.file, this.number, this.expiresOn});
  final String docType;
  final PickedFile file;
  final String? number;
  final String? expiresOn;
}

/// Asks for a document type, optional number/expiry and a file.
Future<NewDocument?> showAddDocumentSheet(BuildContext context, {required List<String> docTypes, String title = 'Add a document'}) {
  return showAppSheet<NewDocument>(
    context,
    title: title,
    subtitle: 'Stored privately. Only you and our verification team can open it.',
    builder: (ctx) => _AddDocumentForm(docTypes: docTypes),
  );
}

class _AddDocumentForm extends ConsumerStatefulWidget {
  const _AddDocumentForm({required this.docTypes});
  final List<String> docTypes;
  @override
  ConsumerState<_AddDocumentForm> createState() => _AddDocumentFormState();
}

class _AddDocumentFormState extends ConsumerState<_AddDocumentForm> {
  late String _type = widget.docTypes.first;
  final _number = TextEditingController();
  final _expiry = TextEditingController();
  PickedFile? _file;
  String? _error;

  @override
  void dispose() {
    _number.dispose();
    _expiry.dispose();
    super.dispose();
  }

  Future<void> _pick() async {
    final f = await ref.read(documentPickerProvider).pick();
    if (f == null) return;
    setState(() {
      _file = f;
      _error = f.bytes.length > 10 * 1024 * 1024 ? 'This file is larger than 10 MB' : null;
    });
  }

  @override
  Widget build(BuildContext context) {
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        AppDropdown<String>(
          label: 'Document type',
          value: _type,
          items: widget.docTypes,
          itemLabel: DocTypes.label,
          onChanged: (v) => setState(() => _type = v ?? _type),
        ),
        const Gap(AppSpacing.md),
        AppTextField(label: 'Document number (optional)', controller: _number, hint: 'Stored encrypted; only the last 4 are shown'),
        const Gap(AppSpacing.md),
        AppDateField(label: 'Valid until (optional)', controller: _expiry),
        const Gap(AppSpacing.md),
        OutlinedButton.icon(
          key: const Key('pick-file'),
          onPressed: _pick,
          icon: const Icon(AppIcons.attach, size: 18),
          label: Text(_file?.name ?? 'Choose file (PDF or image)', overflow: TextOverflow.ellipsis),
        ),
        if (_error != null) ...[const Gap(AppSpacing.sm), Text(_error!, style: const TextStyle(color: AppColors.red700))],
        const Gap(AppSpacing.lg),
        AppButton(
          key: const Key('add-document'),
          label: 'Add document',
          icon: AppIcons.upload,
          onPressed: _file == null || _error != null
              ? null
              : () {
                  final expiry = _expiry.text.trim();
                  if (expiry.isNotEmpty && !RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(expiry)) {
                    setState(() => _error = 'Use the YYYY-MM-DD format for the expiry date');
                    return;
                  }
                  Navigator.of(context).pop(
                    NewDocument(
                      docType: _type,
                      file: _file!,
                      number: _number.text.trim().isEmpty ? null : _number.text.trim(),
                      expiresOn: expiry.isEmpty ? null : expiry,
                    ),
                  );
                },
        ),
      ],
    );
  }
}

/// Async-value scaffold for verification pages.
class AsyncBody<T> extends StatelessWidget {
  const AsyncBody({super.key, required this.value, required this.onRetry, required this.builder});
  final AsyncValue<T> value;
  final VoidCallback onRetry;
  final Widget Function(T) builder;

  @override
  Widget build(BuildContext context) => value.when(
    data: builder,
    loading: () => const LoadingView(),
    error: (e, _) => ErrorState(message: describeError(e), onRetry: onRetry),
  );
}
