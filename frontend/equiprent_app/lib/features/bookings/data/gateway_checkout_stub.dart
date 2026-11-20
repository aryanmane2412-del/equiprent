import 'gateway_checkout_contract.dart';

GatewayCheckout createGatewayCheckout() => _UnsupportedCheckout();

class _UnsupportedCheckout implements GatewayCheckout {
  @override
  bool get supported => false;

  @override
  void open(
    Map<String, dynamic> options, {
    required GatewayPaymentSuccess onSuccess,
    required GatewayPaymentFailure onFailure,
  }) => onFailure('Online checkout is unavailable on this platform.');

  @override
  void dispose() {}
}
