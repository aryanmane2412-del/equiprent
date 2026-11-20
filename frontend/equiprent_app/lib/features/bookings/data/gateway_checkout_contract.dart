typedef GatewayPaymentSuccess =
    void Function({required String paymentId, required String signature});
typedef GatewayPaymentFailure = void Function(String message);

abstract interface class GatewayCheckout {
  bool get supported;
  void open(
    Map<String, dynamic> options, {
    required GatewayPaymentSuccess onSuccess,
    required GatewayPaymentFailure onFailure,
  });
  void dispose();
}
