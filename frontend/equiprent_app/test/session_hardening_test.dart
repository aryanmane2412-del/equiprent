import 'package:equiprent_app/app/session_reset.dart';
import 'package:equiprent_app/core/auth/auth_controller.dart';
import 'package:equiprent_app/core/auth/auth_api.dart';
import 'package:equiprent_app/core/auth/token_store.dart';
import 'package:equiprent_app/core/session/pending_link.dart';
import 'package:equiprent_app/core/utils/validators.dart';
import 'package:equiprent_app/features/discovery/data/discovery_api.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_auth_api.dart';

class _BrokenStore extends MemoryTokenStore {
  @override
  Future<String?> readRefreshToken() async => throw StateError('keystore unavailable');
}

void main() {
  test('one phone validator accepts 10-digit, +91 and E.164 numbers and rejects the rest', () {
    for (final ok in ['9876543210', '+919876543210', '91 98765 43210', '+14155552671']) {
      expect(Validators.phone(ok), isNull, reason: ok);
    }
    for (final bad in ['', '12345', '5876543210', 'abcdefghij', '+0123456789']) {
      expect(Validators.phone(bad), isNotNull, reason: bad);
    }
    expect(Validators.normalisePhone('98765 43210'), '+919876543210');
    expect(Validators.optionalPhone(''), isNull);
  });

  test('dates must come from the calendar and can be required to be in the future', () {
    expect(Validators.isoDate('2031-01-31', required: true), isNull);
    expect(Validators.isoDate('', required: true), isNotNull);
    expect(Validators.isoDate('31/01/2031'), isNotNull);
    expect(Validators.isoDate('2020-01-01', future: true), isNotNull);
  });

  test('a failing keystore or response decoding ends in the signed-out state, not a hang', () async {
    final c = ProviderContainer(overrides: [
      authApiProvider.overrideWithValue(FakeAuthApi()),
      tokenStoreProvider.overrideWithValue(_BrokenStore()),
    ]);
    addTearDown(c.dispose);
    c.read(authControllerProvider);
    await Future<void>.delayed(const Duration(milliseconds: 50));
    expect(c.read(authControllerProvider).status, AuthStatus.signedOut);
  });

  test('resetUserState drops the comparison tray and any parked deep link', () {
    final c = ProviderContainer();
    addTearDown(c.dispose);
    c.read(compareSelectionProvider.notifier).toggle('machine-1');
    c.read(pendingLinkProvider.notifier).set(const PendingLink('booking.requested', {'booking_id': 'b1'}));
    expect(c.read(compareSelectionProvider), ['machine-1']);
    resetUserState(c.read(Provider((ref) => ref)));
    expect(c.read(compareSelectionProvider), isEmpty);
    expect(c.read(pendingLinkProvider), isNull);
  });
}
