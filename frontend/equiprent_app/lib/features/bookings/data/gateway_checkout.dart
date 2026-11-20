import 'gateway_checkout_stub.dart'
    if (dart.library.io) 'gateway_checkout_native.dart'
    if (dart.library.js_interop) 'gateway_checkout_web.dart'
    as implementation;
import 'gateway_checkout_contract.dart';

export 'gateway_checkout_contract.dart';

GatewayCheckout createGatewayCheckout() =>
    implementation.createGatewayCheckout();
