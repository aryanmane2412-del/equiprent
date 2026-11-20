import 'dart:async';
import 'dart:js_interop';

import 'package:web/web.dart' as web;

Future<bool>? _loading;

/// Injects the Maps JavaScript API once. google_maps_flutter_web needs it
/// before the first GoogleMap is built.
Future<bool> load(String key) => _loading ??= _inject(key);

Future<bool> _inject(String key) {
  final done = Completer<bool>();
  final script = web.HTMLScriptElement()
    ..src = 'https://maps.googleapis.com/maps/api/js?key=${Uri.encodeQueryComponent(key)}&loading=async&region=IN'
    ..async = true;
  script.onload = ((web.Event _) => done.complete(true)).toJS;
  script.onerror = ((web.Event _) {
    _loading = null;
    done.complete(false);
  }).toJS;
  web.document.head!.append(script);
  return done.future.timeout(const Duration(seconds: 20), onTimeout: () => false);
}
