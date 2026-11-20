import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';
import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import '../../verification/data/verification_api.dart' show linkOpenerProvider;
import 'admin_read_only_records.dart';

/// Read-only owner directory: one row per owner organisation, from
/// `GET /admin/equipment-owners` (admin-only on the server).
final adminOwnersProvider = FutureProvider.autoDispose.family<List<AdminRecord>, String>(
  (ref, query) => ref.watch(apiClientProvider).get(
    '/admin/equipment-owners',
    query: {'limit': 200, if (query.trim().isNotEmpty) 'q': query.trim()},
    decode: (r) {
      final rows = r is Map ? r['data'] : null;
      return [
        if (rows is List)
          for (final row in rows)
            if (row is Map) row.cast<String, dynamic>(),
      ];
    },
  ),
);

class AdminOwnersPage extends ConsumerStatefulWidget {
  const AdminOwnersPage({super.key});

  @override
  ConsumerState<AdminOwnersPage> createState() => _AdminOwnersPageState();
}

class _AdminOwnersPageState extends ConsumerState<AdminOwnersPage> {
  String _query = '';

  @override
  Widget build(BuildContext context) {
    final owners = ref.watch(adminOwnersProvider(_query));
    return AppPage(
      title: 'Equipment owners',
      subtitle: 'Read-only directory · one entry per owner',
      body: PageBody(
        children: [
          AppSearchField(
            hint: 'Search owner or company',
            onChanged: (v) => setState(() => _query = v),
          ),
          const Gap(AppSpacing.md),
          AsyncBody(
            value: owners,
            onRetry: () => ref.invalidate(adminOwnersProvider(_query)),
            builder: (rows) => AppDataTable<AdminRecord>(
              rows: rows,
              emptyMessage: 'No owners match this search.',
              columns: [
                TableColumnDef(label: 'Owner', cell: (o) => Text('${o['owner_name'] ?? '—'}')),
                TableColumnDef(label: 'Company', cell: (o) => Text('${o['company_name'] ?? '—'}')),
                TableColumnDef(label: 'Equipments', cell: (o) => Text('${o['equipment_count'] ?? 0}')),
                TableColumnDef(
                  label: 'Contact',
                  cell: (o) {
                    final phone = '${o['phone'] ?? ''}';
                    return TextButton.icon(
                      key: ValueKey('connect-${o['org_id']}'),
                      onPressed: phone.isEmpty ? null : () => ref.read(linkOpenerProvider)('tel:$phone'),
                      icon: const Icon(AppIcons.phone, size: 16),
                      label: const Text('Connect'),
                    );
                  },
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
