import 'package:flutter/material.dart';

import '../theme/app_icons.dart';
import '../theme/app_tokens.dart';

/// Column definition for [AppDataTable].
class TableColumnDef<T> {
  const TableColumnDef({
    required this.label,
    required this.cell,
    this.numeric = false,
    this.sortKey,
    this.showOnCompact = true,
  });

  final String label;
  final Widget Function(T row) cell;
  final bool numeric;

  /// Enables sorting on this column.
  final Comparable<Object?> Function(T row)? sortKey;

  /// Whether this column appears in the stacked card layout on phones.
  final bool showOnCompact;
}

/// Data table that is a real sortable table on wide screens and a list of
/// stacked cards on phones (tables do not fit 360 px).
class AppDataTable<T> extends StatefulWidget {
  const AppDataTable({
    super.key,
    required this.columns,
    required this.rows,
    this.onRowTap,
    this.rowActions,
    this.compactTitle,
    this.compactBreakpoint = 720,
    this.emptyMessage = 'Nothing to show yet.',
  });

  final List<TableColumnDef<T>> columns;
  final List<T> rows;
  final ValueChanged<T>? onRowTap;
  final Widget Function(T row)? rowActions;

  /// Title cell for compact cards (defaults to the first column).
  final Widget Function(T row)? compactTitle;
  final double compactBreakpoint;
  final String emptyMessage;

  @override
  State<AppDataTable<T>> createState() => _AppDataTableState<T>();
}

class _AppDataTableState<T> extends State<AppDataTable<T>> {
  int? _sortColumn;
  bool _ascending = true;

  List<T> get _sorted {
    final i = _sortColumn;
    if (i == null || widget.columns[i].sortKey == null) return widget.rows;
    final key = widget.columns[i].sortKey!;
    final list = [...widget.rows]..sort((a, b) => key(a).compareTo(key(b)));
    return _ascending ? list : list.reversed.toList();
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);
    if (widget.rows.isEmpty) {
      return Container(
        padding: const EdgeInsets.all(AppSpacing.xl),
        decoration: BoxDecoration(color: AppColors.white, borderRadius: AppRadius.card, border: Border.all(color: AppColors.border)),
        child: Center(child: Text(widget.emptyMessage, style: theme.textTheme.bodyMedium?.copyWith(color: AppColors.slate500))),
      );
    }

    return LayoutBuilder(
      builder: (context, constraints) {
        final rows = _sorted;
        if (constraints.maxWidth < widget.compactBreakpoint) return _compact(context, rows);

        return Container(
          decoration: BoxDecoration(color: AppColors.white, borderRadius: AppRadius.card, border: Border.all(color: AppColors.border)),
          clipBehavior: Clip.antiAlias,
          child: SingleChildScrollView(
            scrollDirection: Axis.horizontal,
            child: ConstrainedBox(
              constraints: BoxConstraints(minWidth: constraints.maxWidth),
              child: DataTable(
                sortColumnIndex: _sortColumn,
                sortAscending: _ascending,
                showCheckboxColumn: false,
                columns: [
                  for (var i = 0; i < widget.columns.length; i++)
                    DataColumn(
                      label: Text(widget.columns[i].label.toUpperCase()),
                      numeric: widget.columns[i].numeric,
                      onSort: widget.columns[i].sortKey == null
                          ? null
                          : (index, asc) => setState(() {
                                _sortColumn = index;
                                _ascending = asc;
                              }),
                    ),
                  if (widget.rowActions != null) const DataColumn(label: Text('')),
                ],
                rows: [
                  for (final row in rows)
                    DataRow(
                      onSelectChanged: widget.onRowTap == null ? null : (_) => widget.onRowTap!(row),
                      cells: [
                        for (final c in widget.columns) DataCell(c.cell(row)),
                        if (widget.rowActions != null) DataCell(widget.rowActions!(row)),
                      ],
                    ),
                ],
              ),
            ),
          ),
        );
      },
    );
  }

  Widget _compact(BuildContext context, List<T> rows) {
    final theme = Theme.of(context);
    final cols = widget.columns.where((c) => c.showOnCompact).toList();
    final titleCol = cols.first;
    return Column(
      children: [
        for (final row in rows)
          Padding(
            padding: const EdgeInsets.only(bottom: AppSpacing.sm),
            child: Material(
              color: AppColors.white,
              shape: const RoundedRectangleBorder(borderRadius: AppRadius.card, side: BorderSide(color: AppColors.border)),
              child: InkWell(
                borderRadius: AppRadius.card,
                onTap: widget.onRowTap == null ? null : () => widget.onRowTap!(row),
                child: Padding(
                  padding: const EdgeInsets.all(AppSpacing.md),
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Expanded(
                            child: DefaultTextStyle.merge(
                              style: theme.textTheme.titleSmall,
                              child: (widget.compactTitle ?? titleCol.cell)(row),
                            ),
                          ),
                          if (widget.rowActions != null) widget.rowActions!(row)
                          else if (widget.onRowTap != null) const Icon(AppIcons.chevronRight, size: 18, color: AppColors.slate400),
                        ],
                      ),
                      const SizedBox(height: AppSpacing.sm),
                      // The first column is the card title (or replaced by compactTitle).
                      for (final c in cols.skip(1))
                        Padding(
                          padding: const EdgeInsets.symmetric(vertical: 3),
                          child: Row(
                            children: [
                              SizedBox(
                                width: 110,
                                child: Text(c.label, style: theme.textTheme.bodySmall?.copyWith(color: AppColors.slate500)),
                              ),
                              Expanded(child: Align(alignment: Alignment.centerLeft, child: c.cell(row))),
                            ],
                          ),
                        ),
                    ],
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }
}
