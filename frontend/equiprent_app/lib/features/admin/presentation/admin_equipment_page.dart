import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/widgets/widgets.dart';
import '../../verification/presentation/verification_widgets.dart';
import 'admin_read_only_records.dart';

class AdminEquipmentPage extends ConsumerWidget {
  const AdminEquipmentPage({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final records = ref.watch(adminEquipmentRecordsProvider);
    return AppPage(
      title: 'Equipment listings',
      subtitle: 'Read-only inventory and listing status',
      body: AsyncBody(
        value: records,
        onRetry: () => ref.invalidate(adminEquipmentRecordsProvider),
        builder: (rows) => RefreshIndicator(
          onRefresh: () async => ref.invalidate(adminEquipmentRecordsProvider),
          child: PageBody(
            children: [
              AppDataTable<AdminRecord>(
                rows: rows,
                emptyMessage: 'No equipment listings recorded.',
                columns: [
                  TableColumnDef(
                    label: 'Equipment',
                    cell: (row) => Text('${row['title'] ?? '—'}'),
                  ),
                  TableColumnDef(
                    label: 'Category',
                    cell: (row) => Text('${row['category_name'] ?? '—'}'),
                  ),
                  TableColumnDef(
                    label: 'Model',
                    cell: (row) => Text('${row['model_name'] ?? '—'}'),
                    showOnCompact: false,
                  ),
                  TableColumnDef(
                    label: 'Owner',
                    cell: (row) => Text('${row['owner_name'] ?? '—'}'),
                  ),
                  TableColumnDef(
                    label: 'Status',
                    cell: (row) => Text('${row['status'] ?? '—'}'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}
