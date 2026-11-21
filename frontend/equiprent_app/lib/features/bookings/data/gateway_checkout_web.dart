import 'dart:async';
import 'dart:js_interop';
import 'dart:js_interop_unsafe';

import 'package:web/web.dart' as web;

import 'gateway_checkout_contract.dart';

GatewayCheckout createGatewayCheckout() => _WebCheckout();

@JS('Razorpay')
extension type _RazorpayJs._(JSObject _) implements JSObject {
  external _RazorpayJs(JSObject options);
  external void open();
  external void close();
  external void on(String event, JSFunction callback);
}

const _scriptUrl = 'https://checkout.razorpay.com/v1/checkout.js';

/// Razorpay Standard Checkout in the browser. Only the public key id and the order id (created
/// by our backend) are used here; the key secret never leaves the server. A success callback is
/// only forwarded for the backend to verify: it is not proof of payment.
class _WebCheckout implements GatewayCheckout {
  static Future<void>? _loading;
  _RazorpayJs? _instance;
  Timer? _timeout;
  bool _active = false;

  @override
  bool get supported => true;

  static Future<void> _loadScript() {
    if (web.window.has('Razorpay')) return Future.value();
    return _loading ??= () {
      final done = Completer<void>();
      final script = web.HTMLScriptElement()
        ..src = _scriptUrl
        ..async = true;
      script.onload = ((web.Event _) => done.complete()).toJS;
      script.onerror = ((web.Event _) {
        _loading = null; // allow a retry
        done.completeError(StateError('script'));
      }).toJS;
      web.document.head!.append(script);
      return done.future;
    }();
  }

  @override
  void open(
    Map<String, dynamic> options, {
    required GatewayPaymentSuccess onSuccess,
    required GatewayPaymentFailure onFailure,
  }) {
    if (_active) return; // one checkout at a time: no duplicate submission
    _active = true;
    void finish() {
      _active = false;
      _timeout?.cancel();
      _instance = null;
    }

    void fail(String message) {
      if (!_active) return;
      finish();
      onFailure(message);
    }

    _loadScript().then((_) {
      if (!_active) return;
      final opts = options.jsify() as JSObject;
      opts['handler'] = ((JSObject response) {
        if (!_active) return;
        final paymentId = (response['razorpay_payment_id'] as JSString?)?.toDart;
        final signature = (response['razorpay_signature'] as JSString?)?.toDart;
        finish();
        if (paymentId == null || signature == null) {
          onFailure('The gateway returned an incomplete payment result.');
          return;
        }
        onSuccess(paymentId: paymentId, signature: signature);
      }).toJS;
      final modal = (opts['modal'] as JSObject?) ?? JSObject();
      modal['confirm_close'] = true.toJS;
      modal['ondismiss'] = (() => fail('Checkout was closed before the payment was completed.')).toJS;
      opts['modal'] = modal;
      if (!opts.has('timeout')) opts['timeout'] = 900.toJS;
      final rzp = _RazorpayJs(opts);
      _instance = rzp;
      rzp.on(
        'payment.failed',
        ((JSObject response) {
          final error = response['error'] as JSObject?;
          final description = (error?['description'] as JSString?)?.toDart;
          fail(description ?? 'The payment failed. You were not charged, or the amount will be refunded.');
        }).toJS,
      );
      _timeout = Timer(const Duration(minutes: 16), () {
        _instance?.close();
        fail('Checkout timed out. If money was taken it is confirmed automatically; otherwise try again.');
      });
      rzp.open();
    }).catchError((Object _) {
      fail('Could not load the payment window. Check your connection and try again.');
    });
  }

  @override
  void dispose() {
    _timeout?.cancel();
    _active = false;
    _instance = null;
  }
}
