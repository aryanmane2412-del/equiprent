import 'dart:async';

import 'package:dio/dio.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../network/dio_provider.dart';
import '../theme/app_tokens.dart';

/// True while the API cannot be reached. Probes `/health` every 20 s and
/// only reacts to connection-level failures (never to 4xx/5xx answers).
/// Set to false (tests) to skip the periodic probe.
final apiProbeEnabledProvider = Provider<bool>((ref) => true);

final apiReachableProvider = StreamProvider<bool>((ref) async* {
  if (!ref.watch(apiProbeEnabledProvider)) return;
  final dio = ref.watch(dioProvider);
  while (true) {
    var ok = true;
    try {
      await dio.get<dynamic>('/health', options: Options(receiveTimeout: const Duration(seconds: 6)));
    } on DioException catch (e) {
      ok = e.response != null;
    } catch (_) {
      ok = false;
    }
    yield ok;
    await Future<void>.delayed(const Duration(seconds: 20));
  }
});

/// Wraps the app: shows a live-region banner above [child] when offline.
class OfflineBanner extends ConsumerWidget {
  const OfflineBanner({super.key, required this.child});
  final Widget child;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final offline = ref.watch(apiReachableProvider).value == false;
    return Column(children: [
      if (offline)
        Semantics(
          liveRegion: true,
          child: Material(
            color: AppColors.navy900,
            child: SafeArea(
              bottom: false,
              child: Padding(
                padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
                child: Row(children: const [
                  Icon(Icons.cloud_off, size: 18, color: AppColors.white),
                  SizedBox(width: 8),
                  Expanded(
                    child: Text(
                      'You are offline or EQUIPRENT cannot be reached. Showing what was last loaded; changes will not save.',
                      style: TextStyle(color: AppColors.white, fontSize: 13),
                    ),
                  ),
                ]),
              ),
            ),
          ),
        ),
      Expanded(child: child),
    ]);
  }
}
