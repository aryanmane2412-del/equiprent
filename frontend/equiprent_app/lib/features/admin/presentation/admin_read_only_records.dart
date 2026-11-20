import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../../core/network/api_client.dart';

typedef AdminRecord = Map<String, dynamic>;

List<AdminRecord> _records(Object? response) {
  if (response is! Map) return const [];
  final rows = response['data'];
  if (rows is! List) return const [];
  return [
    for (final row in rows)
      if (row is Map) row.cast<String, dynamic>(),
  ];
}

final adminUsersRecordsProvider = FutureProvider.autoDispose<List<AdminRecord>>(
  (ref) => ref
      .watch(apiClientProvider)
      .get(
        '/admin/read-only-users',
        query: const {'limit': 100},
        decode: _records,
      ),
);

final adminEquipmentRecordsProvider =
    FutureProvider.autoDispose<List<AdminRecord>>(
      (ref) => ref
          .watch(apiClientProvider)
          .get(
            '/admin/read-only-equipment',
            query: const {'limit': 100},
            decode: _records,
          ),
    );

final adminBookingsRecordsProvider = FutureProvider.autoDispose
    .family<List<AdminRecord>, (String, String)>(
      (ref, range) => ref
          .watch(apiClientProvider)
          .get(
            '/admin/read-only-bookings',
            query: {'from': range.$1, 'to': range.$2, 'limit': 100},
            decode: _records,
          ),
    );
