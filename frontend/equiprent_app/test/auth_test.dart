import 'dart:async';
import 'dart:convert';
import 'dart:typed_data';

import 'package:dio/dio.dart';
import 'package:equiprent_app/app/router.dart';
import 'package:equiprent_app/core/auth/auth_controller.dart';
import 'package:equiprent_app/core/auth/auth_api.dart';
import 'package:equiprent_app/core/auth/auth_interceptor.dart';
import 'package:equiprent_app/core/auth/auth_models.dart';
import 'package:equiprent_app/core/auth/token_store.dart';
import 'package:equiprent_app/core/session/active_role_provider.dart';
import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/auth/auth_common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_auth_api.dart';
import 'test_helpers.dart';

Finder field(String key) => find.descendant(
  of: find.byKey(ValueKey(key)),
  matching: find.byType(EditableText),
);

Future<void> type(WidgetTester tester, String key, String text) async {
  await tester.ensureVisible(field(key));
  await tester.enterText(field(key), text);
  await tester.pump();
}

String location(ProviderContainer c) =>
    c.read(routerProvider).routeInformationProvider.value.uri.path;

void main() {
  const signedOut = AuthState.signedOut();

  testWidgets('signed-out users only reach the auth screens', (tester) async {
    final c = await pumpApp(tester, auth: signedOut);
    expect(find.text('Sign in to EQUIPRENT'), findsOneWidget);
    for (final path in ['/customer', '/admin/users', '/owner', '/account']) {
      c.read(routerProvider).go(path);
      await settle(tester);
      expect(location(c), AuthRoutes.signIn, reason: path);
    }
  });

  testWidgets('password sign-in: validation, wrong password, then success', (
    tester,
  ) async {
    final api = FakeAuthApi()
      ..accounts['owner@example.com'] = (
        'Tractor-Rental-2026',
        ['EQUIPMENT_OWNER'],
      );
    final store = MemoryTokenStore();
    final c = await pumpApp(tester, auth: signedOut, api: api, tokens: store);

    await tapOn(tester, find.byKey(const ValueKey('sign-in')));
    expect(find.text('Enter your email or mobile number'), findsOneWidget);
    expect(api.calls, isEmpty, reason: 'client-side validation runs first');

    await type(tester, 'identifier', 'Owner@Example.com');
    await type(tester, 'password', 'wrong-password');
    await tapOn(tester, find.byKey(const ValueKey('sign-in')));
    expect(
      find.text('The email/mobile number or password is incorrect.'),
      findsOneWidget,
    );
    expect(
      api.calls.last,
      'login:owner@example.com',
      reason: 'emails are normalised',
    );

    await type(tester, 'password', 'Tractor-Rental-2026');
    await tapOn(tester, find.byKey(const ValueKey('sign-in')));
    // A single role opens its area directly.
    expect(location(c), '/owner');
    expect(c.read(activeRoleProvider), UserRole.owner);
    expect(
      store.token,
      api.lastRefreshToken,
      reason: 'refresh token persisted',
    );
  });

  test(
    'sign-out clears the local session even when server revocation fails',
    () async {
      final api = FakeAuthApi()..failLogout = true;
      final store = MemoryTokenStore('refresh-token-current-padding-pad');
      final c = ProviderContainer(
        overrides: [
          authApiProvider.overrideWithValue(api),
          tokenStoreProvider.overrideWithValue(store),
          authControllerProvider.overrideWith(
            () => AuthController(
              initial: AuthState.signedIn(
                AuthUser(
                  id: 'user-1',
                  phone: '+919800000000',
                  fullName: 'Test User',
                ),
                AuthTokens(
                  accessToken: 'access-token',
                  refreshToken: 'refresh-token-current-padding-pad',
                  accessExpiresAt: DateTime(2030),
                ),
              ),
            ),
          ),
        ],
      );
      addTearDown(c.dispose);

      await c.read(authControllerProvider.notifier).logout();

      expect(c.read(authControllerProvider).status, AuthStatus.signedOut);
      expect(store.token, isNull);
      expect(api.calls, contains('logout'));
    },
  );

  testWidgets('mobile OTP sign-in with development simulation', (tester) async {
    final api = FakeAuthApi();
    final c = await pumpApp(tester, auth: signedOut, api: api);
    await tapOn(tester, find.text('Mobile OTP'));
    await type(tester, 'otp-phone', '98765 43210');
    await tapOn(tester, find.byKey(const ValueKey('send-code')));
    expect(api.calls.last, 'otp:+919876543210');
    expect(find.byKey(const ValueKey('dev-secret')), findsOneWidget);
    await tapOn(tester, find.text('Use it'));
    await tapOn(tester, find.byKey(const ValueKey('verify-code')));
    expect(c.read(authControllerProvider).isSignedIn, isTrue);
    expect(location(c), '/customer');
  });

  testWidgets(
    'without an SMS provider the OTP option is disabled and explained',
    (tester) async {
      final api = FakeAuthApi(caps: const AuthCapabilities());
      await pumpApp(tester, auth: signedOut, api: api);
      expect(
        find.textContaining('switched off until an SMS provider is configured'),
        findsOneWidget,
      );
      await tester.tap(find.text('Mobile OTP'));
      await settle(tester);
      expect(find.byKey(const ValueKey('send-code')), findsNothing);
      expect(find.byKey(const ValueKey('dev-secret')), findsNothing);
    },
  );

  testWidgets(
    'registration offers customer, owner and driver, but no privileged roles',
    (tester) async {
      final api = FakeAuthApi()..otpRoles = const ['EQUIPMENT_OWNER'];
      final c = await pumpApp(tester, auth: signedOut, api: api, size: desktop);
      await tapOn(tester, find.byKey(const ValueKey('go-register')));
      expect(
        find.byKey(const ValueKey('account-type-customer')),
        findsOneWidget,
      );
      expect(
        find.byKey(const ValueKey('account-type-equipment_owner')),
        findsOneWidget,
      );
      expect(find.byKey(const ValueKey('account-type-driver')), findsOneWidget);
      for (final forbidden in ['Platform Admin', 'Fleet Manager']) {
        expect(find.text(forbidden), findsNothing, reason: forbidden);
      }

      await tapOn(tester, find.byKey(const ValueKey('account-type-driver')));
      await type(tester, 'reg-name', 'Imran Khan');
      await type(tester, 'reg-email', 'imran@example.com');
      await type(tester, 'reg-phone', '9876543210');
      await type(tester, 'reg-password', 'Driver-Account-2026');
      await tapOn(tester, find.byKey(const ValueKey('reg-terms')));
      await tapOn(tester, find.byKey(const ValueKey('reg-inspection-policy')));
      await tapOn(tester, find.byKey(const ValueKey('register')));
      expect(api.calls.last, 'register:driver:+919876543210:');
      expect(location(c), AuthRoutes.verify);

      api.otpRoles = const ['OPERATOR'];
      await tapOn(tester, find.text('Use it').first);
      await tapOn(tester, find.byKey(const ValueKey('verify-phone')));
      expect(location(c), '/operator');
    },
  );

  testWidgets('owner registration validates business name', (tester) async {
    final api = FakeAuthApi()..otpRoles = const ['EQUIPMENT_OWNER'];
    final c = await pumpApp(tester, auth: signedOut, api: api, size: desktop);
    await tapOn(tester, find.byKey(const ValueKey('go-register')));

    await tapOn(
      tester,
      find.byKey(const ValueKey('account-type-equipment_owner')),
    );
    await type(tester, 'reg-name', 'Ravi Patil');
    await type(tester, 'reg-email', 'ravi@example.com');
    await type(tester, 'reg-phone', '9876543210');
    await type(tester, 'reg-password', 'Tractor-Rental-2026');
    await tapOn(tester, find.byKey(const ValueKey('reg-terms')));
    await tapOn(tester, find.byKey(const ValueKey('reg-inspection-policy')));
    await tapOn(tester, find.byKey(const ValueKey('register')));
    expect(
      find.text('Enter your business name'),
      findsOneWidget,
      reason: 'owners need a business name',
    );
    expect(api.calls.where((c) => c.startsWith('register')), isEmpty);

    await type(tester, 'reg-business', 'Patil Earthmovers');
    await tapOn(tester, find.byKey(const ValueKey('register')));
    expect(
      api.calls.last,
      'register:equipment_owner:+919876543210:Patil Earthmovers',
    );
    expect(location(c), AuthRoutes.verify);

    await tapOn(tester, find.text('Use it').first);
    await tapOn(tester, find.byKey(const ValueKey('verify-phone')));
    expect(location(c), '/owner');
  });

  testWidgets(
    'demo mode explains a duplicate registration instead of waiting for a code',
    (tester) async {
      final api = FakeAuthApi()..registeredPhones.add('+919876543210');
      final c = await pumpApp(tester, auth: signedOut, api: api, size: desktop);
      await tapOn(tester, find.byKey(const ValueKey('go-register')));
      await tapOn(tester, find.byKey(const ValueKey('account-type-customer')));
      await type(tester, 'reg-name', 'Ravi Patil');
      await type(tester, 'reg-email', 'ravi@example.com');
      await type(tester, 'reg-phone', '9876543210');
      await type(tester, 'reg-password', 'Tractor-Rental-2026');
      await tapOn(tester, find.byKey(const ValueKey('reg-terms')));
      await tapOn(tester, find.byKey(const ValueKey('reg-inspection-policy')));
      await tapOn(tester, find.byKey(const ValueKey('register')));
      expect(find.byKey(const ValueKey('dev-notice')), findsOneWidget);
      expect(
        find.textContaining('mobile number is already registered'),
        findsOneWidget,
      );
      expect(
        location(c),
        AuthRoutes.register,
        reason: 'no empty verify screen',
      );
      await tapOn(
        tester,
        find.descendant(
          of: find.byKey(const ValueKey('dev-notice')),
          matching: find.text('Sign in'),
        ),
      );
      expect(location(c), AuthRoutes.signIn);
    },
  );

  testWidgets('demo mode lists seeded accounts that fill the sign-in form', (
    tester,
  ) async {
    final api = FakeAuthApi();
    await pumpApp(tester, auth: signedOut, api: api);
    await tapOn(
      tester,
      find.byKey(const ValueKey('demo-owner.pune@equiprent.dev')),
    );
    expect(find.text('owner.pune@equiprent.dev'), findsOneWidget);
    await tapOn(tester, find.byKey(const ValueKey('sign-in')));
    expect(
      api.calls.any((c) => c.contains('owner.pune@equiprent.dev')),
      isTrue,
    );
  });

  testWidgets('demo roles show one city-free equipment owner account', (
    tester,
  ) async {
    final api = FakeAuthApi();
    await pumpApp(tester, auth: signedOut, api: api);
    expect(find.text('Customer'), findsOneWidget);
    expect(find.text('Equipment owner'), findsOneWidget);
    expect(find.text('Admin'), findsOneWidget);
    expect(find.textContaining('Pune'), findsNothing);
    expect(find.textContaining('Mumbai'), findsNothing);
    await tapOn(
      tester,
      find.byKey(const ValueKey('demo-owner.pune@equiprent.dev')),
    );
    await tapOn(tester, find.byKey(const ValueKey('sign-in')));
    expect(
      api.calls.any((c) => c.contains('owner.pune@equiprent.dev')),
      isTrue,
    );
  });

  testWidgets('without demo mode the demo accounts are hidden', (tester) async {
    await pumpApp(
      tester,
      auth: signedOut,
      api: FakeAuthApi(caps: const AuthCapabilities(emailVerification: true)),
    );
    expect(find.byKey(const ValueKey('demo-accounts')), findsNothing);
  });

  testWidgets('weak passwords are refused before reaching the server', (
    tester,
  ) async {
    final api = FakeAuthApi();
    await pumpApp(tester, auth: signedOut, api: api, size: desktop);
    await tapOn(tester, find.byKey(const ValueKey('go-register')));
    await tapOn(tester, find.byKey(const ValueKey('reg-terms')));
    await tapOn(tester, find.byKey(const ValueKey('reg-inspection-policy')));
    await type(tester, 'reg-password', 'onlyletters');
    await tapOn(tester, find.byKey(const ValueKey('register')));
    expect(find.text('Mix letters with numbers or symbols'), findsOneWidget);
  });

  testWidgets('forgot password by email resets with the emailed code', (
    tester,
  ) async {
    final api = FakeAuthApi();
    await pumpApp(tester, auth: signedOut, api: api, size: desktop);
    await tapOn(tester, find.byKey(const ValueKey('go-forgot')));
    await type(tester, 'forgot-identifier', 'ravi@example.com');
    await tapOn(tester, find.byKey(const ValueKey('forgot-send')));
    expect(
      find.text('If an account matches, we have sent instructions.'),
      findsOneWidget,
    );
    await tapOn(tester, find.text('Use it'));
    await type(tester, 'reset-password', 'Brand-New-Secret-7');
    await tapOn(tester, find.byKey(const ValueKey('reset-submit')));
    expect(api.calls.last, 'reset:reset-token-abcdefghijklmnop');
    expect(find.byKey(const ValueKey('reset-done')), findsOneWidget);
  });

  testWidgets('only roles granted by the server are offered and reachable', (
    tester,
  ) async {
    final c = await pumpApp(
      tester,
      auth: signedInAs([UserRole.customer, UserRole.owner]),
    );
    expect(find.byKey(const ValueKey('role-customer')), findsOneWidget);
    expect(find.byKey(const ValueKey('role-owner')), findsOneWidget);
    expect(find.byKey(const ValueKey('role-admin')), findsNothing);

    // Forcing the client-side active role does not open an ungranted area.
    c.read(activeRoleProvider.notifier).select(UserRole.admin);
    c.read(routerProvider).go('/admin/users');
    await settle(tester);
    expect(location(c), AppRoutes.roleSelection);
  });

  testWidgets(
    'a stored session is restored on launch; a revoked one is cleared',
    (tester) async {
      final api = FakeAuthApi();
      api.lastRefreshToken = 'refresh-token-stored-padding-pad';
      final good = MemoryTokenStore('refresh-token-stored-padding-pad');
      final c = await pumpApp(tester, auth: null, api: api, tokens: good);
      // pumpApp passes initial=signedInAs(all) when auth is null; restore explicitly.
      c.read(authControllerProvider.notifier).state =
          const AuthState.restoring();
      await c.read(authControllerProvider.notifier).restore();
      await settle(tester);
      expect(c.read(authControllerProvider).isSignedIn, isTrue);
      expect(
        good.token,
        isNot('refresh-token-stored-padding-pad'),
        reason: 'rotated',
      );

      final revoked = MemoryTokenStore('refresh-token-revoked');
      final c2 = await pumpApp(
        tester,
        auth: signedOut,
        api: FakeAuthApi(),
        tokens: revoked,
      );
      await c2.read(authControllerProvider.notifier).restore();
      await settle(tester);
      expect(c2.read(authControllerProvider).isSignedIn, isFalse);
      expect(revoked.token, isNull);
    },
  );

  testWidgets('sign out revokes the session and clears stored tokens', (
    tester,
  ) async {
    final api = FakeAuthApi();
    final store = MemoryTokenStore('anything');
    final c = await pumpApp(
      tester,
      auth: signedInAs([UserRole.customer, UserRole.owner]),
      api: api,
      tokens: store,
    );
    await tapOn(tester, find.byKey(const ValueKey('role-sign-out')));
    expect(api.calls, contains('logout'));
    expect(store.token, isNull);
    expect(location(c), AuthRoutes.signIn);
  });

  testWidgets('account & security lists devices and signs out the others', (
    tester,
  ) async {
    final api = FakeAuthApi();
    final c = await pumpApp(
      tester,
      auth: signedInAs([UserRole.customer, UserRole.owner]),
      api: api,
      size: desktop,
    );
    c.read(routerProvider).go(AuthRoutes.account);
    await settle(tester);
    expect(find.text('android · this device'), findsOneWidget);
    expect(find.text('web'), findsOneWidget);
    await tapOn(tester, find.byKey(const ValueKey('revoke-others')));
    expect(api.calls, contains('revoke-others'));
    expect(find.text('Signed out of all other devices.'), findsOneWidget);
  });

  for (final (name, size) in const [
    ('phone', phone),
    ('tablet', tablet),
    ('desktop', desktop),
  ]) {
    testWidgets('auth screens render without layout errors on $name', (
      tester,
    ) async {
      final c = await pumpApp(tester, auth: signedOut, size: size);
      for (final path in [
        AuthRoutes.signIn,
        AuthRoutes.register,
        AuthRoutes.forgotPassword,
        AuthRoutes.verifyEmail,
        AuthRoutes.verify,
      ]) {
        c.read(routerProvider).go(path);
        await settle(tester);
        expect(tester.takeException(), isNull, reason: '$path on $name');
        expect(location(c), path);
      }
    });

    testWidgets('account & security renders without layout errors on $name', (
      tester,
    ) async {
      final c = await pumpApp(
        tester,
        auth: signedInAs([UserRole.customer, UserRole.owner]),
        size: size,
      );
      c.read(routerProvider).go(AuthRoutes.account);
      await settle(tester);
      expect(tester.takeException(), isNull, reason: 'account on $name');
      expect(find.text('Account & security'), findsWidgets);
    });
  }

  group('AuthInterceptor', () {
    test(
      'refreshes once on 401 and retries; concurrent 401s share the refresh',
      () async {
        var token = 'old';
        var refreshes = 0;
        final dio = Dio(BaseOptions(baseUrl: 'http://api'));
        dio.httpClientAdapter = _Adapter((options) {
          final auth = options.headers['Authorization'];
          return auth == 'Bearer new'
              ? (200, {'ok': true})
              : (401, {'code': 'UNAUTHENTICATED'});
        });
        Future<String?>? inFlight;
        dio.interceptors.add(
          AuthInterceptor(
            dio: dio,
            accessToken: () => token,
            refresh: () => inFlight ??= Future(() {
              refreshes++;
              token = 'new';
              return 'new';
            }),
          ),
        );
        final results = await Future.wait([
          dio.get<Object?>('/a'),
          dio.get<Object?>('/b'),
          dio.get<Object?>('/c'),
        ]);
        expect(results.map((r) => r.statusCode), everyElement(200));
        expect(refreshes, 1);
      },
    );

    test('gives up when the refresh fails (no loop)', () async {
      final dio = Dio(BaseOptions(baseUrl: 'http://api'));
      var calls = 0;
      dio.httpClientAdapter = _Adapter((_) {
        calls++;
        return (401, {'code': 'UNAUTHENTICATED'});
      });
      dio.interceptors.add(
        AuthInterceptor(
          dio: dio,
          accessToken: () => 'old',
          refresh: () async => null,
        ),
      );
      await expectLater(dio.get<Object?>('/x'), throwsA(isA<DioException>()));
      expect(calls, 1);
    });

    test(
      'requests without a session are sent without a token and not retried',
      () async {
        final dio = Dio(BaseOptions(baseUrl: 'http://api'));
        String? seen;
        dio.httpClientAdapter = _Adapter((o) {
          seen = o.headers['Authorization'] as String?;
          return (401, {'code': 'UNAUTHENTICATED'});
        });
        var refreshed = false;
        dio.interceptors.add(
          AuthInterceptor(
            dio: dio,
            accessToken: () => null,
            refresh: () async {
              refreshed = true;
              return 'x';
            },
          ),
        );
        await expectLater(dio.get<Object?>('/x'), throwsA(isA<DioException>()));
        expect(seen, isNull);
        expect(refreshed, isFalse);
      },
    );
  });

  group('validators', () {
    test('phone numbers normalise to E.164', () {
      expect(AuthValidators.normalisePhone('98765 43210'), '+919876543210');
      expect(AuthValidators.normalisePhone('+919876543210'), '+919876543210');
      expect(AuthValidators.normalisePhone('919876543210'), '+919876543210');
      expect(AuthValidators.normalisePhone('12345'), isNull);
      expect(
        AuthValidators.normaliseIdentifier(' Ravi@Example.COM '),
        'ravi@example.com',
      );
    });

    test('app roles map to role areas; unknown roles are ignored', () {
      expect(
        AuthUser.rolesFromJson(['CUSTOMER', 'ADMIN', 'SUPERUSER', 'CUSTOMER']),
        [UserRole.customer, UserRole.admin],
      );
      expect(
        userRoleFromApp('admin_super'),
        isNull,
        reason: 'only server app-role names are accepted',
      );
    });
  });
}

class _Adapter implements HttpClientAdapter {
  _Adapter(this.respond);

  final (int, Map<String, Object?>) Function(RequestOptions options) respond;

  @override
  Future<ResponseBody> fetch(
    RequestOptions options,
    Stream<Uint8List>? requestStream,
    Future<void>? cancelFuture,
  ) async {
    final (status, body) = respond(options);
    return ResponseBody.fromString(
      jsonEncode(body),
      status,
      headers: {
        Headers.contentTypeHeader: ['application/json'],
      },
    );
  }

  @override
  void close({bool force = false}) {}
}
