import 'package:flutter/foundation.dart';

DateTime? _dt(Object? v) => v == null ? null : DateTime.parse(v as String).toLocal();
String? _str(Object? v) => v == null || '$v'.isEmpty ? null : '$v';

/// A configured tax (tax_rates) with its compliance review.
@immutable
class TaxRule {
  const TaxRule({
    required this.id,
    required this.code,
    required this.name,
    required this.rateBps,
    required this.appliesTo,
    required this.sacCode,
    required this.validFrom,
    required this.isActive,
    this.validUntil,
    this.legalBasis,
    this.reviewedBy,
    this.reviewedAt,
  });
  factory TaxRule.fromJson(Map<String, dynamic> j) => TaxRule(
    id: '${j['id']}',
    code: '${j['code']}',
    name: '${j['name']}',
    rateBps: (j['rate_bps'] as num?)?.toInt() ?? 0,
    appliesTo: [for (final a in (j['applies_to'] as List? ?? const [])) '$a'],
    sacCode: _str(j['sac_code']) ?? '997313',
    validFrom: '${j['valid_from']}',
    validUntil: _str(j['valid_until']),
    isActive: j['is_active'] != false,
    legalBasis: _str(j['legal_basis']),
    reviewedBy: _str(j['reviewed_by_name']),
    reviewedAt: _dt(j['reviewed_at']),
  );
  final String id;
  final String code;
  final String name;
  final int rateBps;
  final List<String> appliesTo;
  final String sacCode;
  final String validFrom;
  final String? validUntil;
  final bool isActive;
  final String? legalBasis;
  final String? reviewedBy;
  final DateTime? reviewedAt;

  bool get reviewed => reviewedAt != null;
  String get rateLabel => rateBps % 100 == 0 ? '${rateBps ~/ 100}%' : '${(rateBps / 100).toStringAsFixed(2)}%';
}

/// A rental-agreement terms template (draft, published or retired).
@immutable
class LegalTemplate {
  const LegalTemplate({
    required this.id,
    required this.version,
    required this.title,
    required this.status,
    required this.missing,
    required this.sections,
    this.reviewedBy,
    this.reviewerCapacity,
    this.reviewReference,
    this.reviewedAt,
    this.publishedAt,
  });
  factory LegalTemplate.fromJson(Map<String, dynamic> j) {
    final body = (j['body'] as Map? ?? const {}).cast<String, dynamic>();
    List<String> list(String k) => [for (final c in (body[k] as List? ?? const [])) '$c'];
    return LegalTemplate(
      id: '${j['id']}',
      version: '${j['version']}',
      title: '${j['title']}',
      status: '${j['status']}',
      missing: [for (final m in (j['missing'] as List? ?? const [])) '$m'],
      sections: {
        'Preamble': [if (_str(body['preamble']) != null) '${body['preamble']}'],
        'Equipment condition': list('condition_clauses'),
        'Use of the equipment': list('use_clauses'),
        'Damage responsibility': list('damage_clauses'),
        'Security deposit': list('deposit_clauses'),
        'Terms and conditions': list('general_terms'),
        'Acceptance statement': [if (_str(body['acceptance_statement']) != null) '${body['acceptance_statement']}'],
        'Consent checkbox text': [if (_str(body['consent_text']) != null) '${body['consent_text']}'],
      },
      reviewedBy: _str(j['reviewed_by_name']),
      reviewerCapacity: _str(j['reviewer_capacity']),
      reviewReference: _str(j['review_reference']),
      reviewedAt: _dt(j['reviewed_at']),
      publishedAt: _dt(j['published_at']),
    );
  }
  final String id;
  final String version;
  final String title;
  final String status; // draft | published | retired
  final List<String> missing;
  final Map<String, List<String>> sections;
  final String? reviewedBy;
  final String? reviewerCapacity;
  final String? reviewReference;
  final DateTime? reviewedAt;
  final DateTime? publishedAt;

  bool get reviewed => reviewedAt != null;
  bool get isDraft => status == 'draft';
}
