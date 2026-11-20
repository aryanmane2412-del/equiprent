import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:go_router/go_router.dart';

import 'app/app.dart';
import 'core/network/retry_policy.dart';

void main() {
  WidgetsFlutterBinding.ensureInitialized();
  // Keep the browser URL in sync with imperative navigation on web.
  GoRouter.optionURLReflectsImperativeAPIs = true;
  runApp(const ProviderScope(retry: apiRetry, child: EquipRentApp()));
}
