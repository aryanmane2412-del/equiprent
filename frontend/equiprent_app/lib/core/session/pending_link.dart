import 'package:flutter_riverpod/flutter_riverpod.dart';

/// A notification the user tapped before the session was ready (restoring, signed out, or no
/// role chosen yet). It is replayed once the session and the role are known.
class PendingLink {
  const PendingLink(this.type, this.data);
  final String type;
  final Map<String, Object?> data;
}

class PendingLinkController extends Notifier<PendingLink?> {
  @override
  PendingLink? build() => null;

  void set(PendingLink link) => state = link;

  /// Returns the stored link and forgets it.
  PendingLink? take() {
    final link = state;
    state = null;
    return link;
  }
}

final pendingLinkProvider = NotifierProvider<PendingLinkController, PendingLink?>(PendingLinkController.new);
