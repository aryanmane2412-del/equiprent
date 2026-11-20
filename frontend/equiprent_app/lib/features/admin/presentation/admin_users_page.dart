import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import 'admin_read_only_records.dart';

class AdminUsersPage extends ConsumerStatefulWidget {
  const AdminUsersPage({super.key});

  @override
  ConsumerState<AdminUsersPage> createState() => _AdminUsersPageState();
}

class _AdminUsersPageState extends ConsumerState<AdminUsersPage> {
  // Filter label -> stored role code (roles.code). Customers, drivers' other
  // roles and admins are never mapped here. 'Owner' is the organisation
  // owner/admin (org_admin); 'Equipment Owner' is the lister role (owner).
  static const _filters = <String, String?>{
    'All': null,
    'Owner': 'org_admin',
    'Operator': 'operator',
    'Equipment Owner': 'owner',
  };
  String _role = 'All';
  String _query = '';

  @override
  Widget build(BuildContext context) {
    final records = ref.watch(adminUsersRecordsProvider);
    return AppPage(
      title: 'Users',
      subtitle: 'Read-only user accounts and assigned roles',
      body: AsyncBody(
        value: records,
        onRetry: () => ref.invalidate(adminUsersRecordsProvider),
        builder: (all) {
          final query = _query.trim().toLowerCase();
          final rows = all.where((user) {
            final roles = (user['roles'] as List? ?? const [])
                .map((role) => '$role')
                .toList();
            final code = _filters[_role];
            final roleMatch = code == null
                ? _filters.values.any((c) => c != null && roles.contains(c))
                : roles.contains(code);
            final text =
                '${user['full_name'] ?? ''} ${user['email'] ?? ''} ${user['phone'] ?? ''}'
                    .toLowerCase();
            return roleMatch && (query.isEmpty || text.contains(query));
          }).toList();
          return RefreshIndicator(
            onRefresh: () async => ref.invalidate(adminUsersRecordsProvider),
            child: PageBody(
              children: [
                AppSearchField(
                  hint: 'Search name, email or phone',
                  onChanged: (value) => setState(() => _query = value),
                ),
                const Gap(AppSpacing.ms),
                AppChoiceChips<String>(
                  options: _filters.keys.toList(),
                  selected: _role,
                  onSelected: (value) => setState(() => _role = value),
                  labelOf: (label) => label,
                ),
                const Gap(AppSpacing.md),
                AppDataTable<AdminRecord>(
                  rows: rows,
                  emptyMessage: 'No users match these filters.',
                  columns: [
                    TableColumnDef(
                      label: 'Name',
                      cell: (user) => Text('${user['full_name'] ?? '—'}'),
                    ),
                    TableColumnDef(
                      label: 'Roles',
                      cell: (user) => Text(
                        (user['roles'] as List? ?? const [])
                            .map((role) => '$role'.replaceAll('_', ' '))
                            .join(', '),
                      ),
                    ),
                    TableColumnDef(
                      label: 'Email',
                      cell: (user) => Text('${user['email'] ?? '—'}'),
                      showOnCompact: false,
                    ),
                    TableColumnDef(
                      label: 'Phone',
                      cell: (user) => Text('${user['phone'] ?? '—'}'),
                    ),
                    TableColumnDef(
                      label: 'Status',
                      cell: (user) => Text('${user['status'] ?? '—'}'),
                    ),
                  ],
                ),
              ],
            ),
          );
        },
      ),
    );
  }
}
