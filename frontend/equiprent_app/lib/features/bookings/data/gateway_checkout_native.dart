import 'dart:io' show Platform;

import 'package:razorpay_flutter/razorpay_flutter.dart';

import 'gateway_checkout_contract.dart';

GatewayCheckout createGatewayCheckout() => _NativeCheckout();

class _NativeCheckout implements GatewayCheckout {
  final Razorpay _razorpay = Razorpay();
  GatewayPaymentSuccess? _onSuccess;
  GatewayPaymentFailure? _onFailure;

  _NativeCheckout() {
    _razorpay.on(Razorpay.EVENT_PAYMENT_SUCCESS, _paymentSucceeded);
    _razorpay.on(Razorpay.EVENT_PAYMENT_ERROR, _paymentFailed);
  }

  @override
  bool get supported => Platform.isAndroid || Platform.isIOS;

  @override
  void open(
    Map<String, dynamic> options, {
    required GatewayPaymentSuccess onSuccess,
    required GatewayPaymentFailure onFailure,
  }) {
    _onSuccess = onSuccess;
    _onFailure = onFailure;
    _razorpay.open(options);
  }

  void _paymentSucceeded(PaymentSuccessResponse response) {
    final paymentId = response.paymentId;
    final signature = response.signature;
    if (paymentId == null || signature == null) {
      _onFailure?.call('The gateway returned an incomplete payment result.');
      return;
    }
    _onSuccess?.call(paymentId: paymentId, signature: signature);
  }

  void _paymentFailed(PaymentFailureResponse response) {
    _onFailure?.call(response.message ?? 'Payment was not completed.');
  }

  @override
  void dispose() {
    _razorpay.clear();
    _onSuccess = null;
    _onFailure = null;
  }
}
