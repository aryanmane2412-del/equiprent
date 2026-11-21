import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../core/mock/mock_store.dart';
import '../core/session/pending_link.dart';
import '../features/comms/data/comms_api.dart';
import '../features/discovery/data/discovery_api.dart';
import '../features/listings/data/listings_api.dart';

/// Forgets everything that belongs to the previous user: comparison selection, saved items,
/// search history, notification and chat badges, cached listings and any parked deep link.
void resetUserState(Ref ref) {
  ref.invalidate(compareSelectionProvider);
  ref.invalidate(savedIdsProvider);
  ref.invalidate(savedListProvider);
  ref.invalidate(searchHistoryProvider);
  ref.invalidate(comparisonProvider);
  ref.invalidate(notificationListProvider);
  ref.invalidate(unreadNotificationsProvider);
  ref.invalidate(unreadMessagesProvider);
  ref.invalidate(conversationsProvider);
  ref.invalidate(myListingsProvider);
  ref.invalidate(favoritesProvider);
  ref.invalidate(compareProvider);
  ref.invalidate(pendingLinkProvider);
}
