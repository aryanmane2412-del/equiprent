import 'package:flutter/widgets.dart';

/// True while the app is not visible (paused/hidden/detached). Pollers skip
/// their work then, to save battery and data.
bool get appInBackground {
  final s = WidgetsBinding.instance.lifecycleState;
  return s == AppLifecycleState.paused ||
      s == AppLifecycleState.hidden ||
      s == AppLifecycleState.detached;
}
