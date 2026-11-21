import 'package:dio/dio.dart';
import 'package:equiprent_app/app/router.dart';
import 'package:equiprent_app/core/auth/auth_controller.dart';
import 'package:equiprent_app/features/auth/auth_common.dart';
import 'package:equiprent_app/core/config/app_config.dart';
import 'package:equiprent_app/core/mock/mock_data.dart';
import 'package:equiprent_app/core/mock/mock_store.dart';
import 'package:equiprent_app/core/network/api_exception.dart';
import 'package:equiprent_app/core/network/dio_provider.dart';
import 'package:equiprent_app/core/responsive/responsive.dart';
import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/core/utils/formatters.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'test_helpers.dart';

void main() {
  group('Fmt (Indian formats)', () {
    test('rupees use lakh grouping', () {
      expect(Fmt.inr(123456), '₹1,23,456');
      expect(Fmt.inr(8500), '₹8,500');
    });

    test('compact amounts use K / L / Cr', () {
      expect(Fmt.inrCompact(85000), '₹85K');
      expect(Fmt.inrCompact(347000), '₹3.5L');
      expect(Fmt.inrCompact(37000000), '₹3.7Cr');
    });

    test('initials and relative time', () {
      expect(Fmt.initials('Ravi Deshmukh'), 'RD');
      expect(Fmt.initials('Neha'), 'N');
      expect(
        Fmt.ago(MockData.now.subtract(const Duration(hours: 3)), MockData.now),
        '3h ago',
      );
    });
  });

  group('mock store', () {
    test('compare holds at most three machines', () {
      final c = ProviderContainer();
      addTearDown(c.dispose);
      final n = c.read(compareProvider.notifier);
      expect(
        [n.toggle('a'), n.toggle('b'), n.toggle('c'), n.toggle('d')],
        [true, true, true, false],
      );
      n.toggle('a');
      expect(c.read(compareProvider), ['b', 'c']);
    });

    test('settlements release all pending at once', () {
      final c = ProviderContainer();
      addTearDown(c.dispose);
      c.read(settlementsProvider.notifier).releaseAllPending();
      expect(
        c.read(settlementsProvider).where((s) => s.status.name == 'pending'),
        isEmpty,
      );
    });
  });

  group('WindowSize', () {
    test('uses Material 3 breakpoints', () {
      expect(WindowSize.fromWidth(360), WindowSize.compact);
      expect(WindowSize.fromWidth(600), WindowSize.medium);
      expect(WindowSize.fromWidth(840), WindowSize.expanded);
      expect(WindowSize.fromWidth(1200), WindowSize.large);
    });

    test('Responsive falls back to the next smaller value', () {
      const r = Responsive(compact: 1, expanded: 3);
      expect(r.resolve(WindowSize.medium), 1);
      expect(r.resolve(WindowSize.large), 3);
    });
  });

  group('routing', () {
    test('maps locations to roles by prefix, not substring', () {
      expect(UserRole.fromLocation('/owner/listings/new'), UserRole.owner);
      expect(UserRole.fromLocation('/fleet/live'), UserRole.fleetManager);
      expect(UserRole.fromLocation('/ownership'), isNull);
    });

    test('blocks other roles’ areas and allows shared routes', () {
      final all = signedInAs(UserRole.values);
      expect(appRedirect(all, null, '/'), isNull);
      expect(appRedirect(all, null, '/admin'), AppRoutes.roleSelection);
      expect(
        appRedirect(all, UserRole.customer, '/admin/users'),
        AppRoutes.roleSelection,
      );
      expect(appRedirect(all, UserRole.admin, '/admin/users'), isNull);
    });

    test(
      'a role area needs the role granted by the server, not just selected',
      () {
        final customerOnly = signedInAs([UserRole.customer]);
        expect(
          appRedirect(customerOnly, UserRole.customer, '/customer'),
          isNull,
        );
        // Even if the client state claims admin, the account does not have it.
        expect(
          appRedirect(customerOnly, UserRole.admin, '/admin/users'),
          AppRoutes.roleSelection,
        );
        expect(
          appRedirect(customerOnly, UserRole.owner, '/owner'),
          AppRoutes.roleSelection,
        );
      },
    );

    test('signed-out and restoring sessions only reach the auth screens', () {
      const out = AuthState.signedOut();
      for (final path in ['/', '/customer', '/admin/users', '/account']) {
        expect(appRedirect(out, null, path), AuthRoutes.signIn, reason: path);
      }
      for (final path in AuthRoutes.public) {
        expect(appRedirect(out, null, path), isNull, reason: path);
      }
      expect(
        appRedirect(const AuthState.restoring(), null, '/owner'),
        AuthRoutes.loading,
      );
      expect(
        appRedirect(signedInAs([UserRole.owner]), null, AuthRoutes.signIn),
        AppRoutes.roleSelection,
      );
    });

    test('each role has its own navigation under its prefix', () {
      expect(roleModules.map((m) => m.role).toSet(), UserRole.values.toSet());
      final expected = {
        UserRole.customer: 6,
        UserRole.owner: 8,
        UserRole.operator: 8, // Includes verification and settings.
        UserRole.fleetManager: 7,
        UserRole.admin: 13, // Read-only monitoring, reports and settings.
      };
      for (final m in roleModules) {
        expect(m.destinations.length, expected[m.role], reason: m.role.name);
        for (final d in m.destinations) {
          expect(UserRole.fromLocation(d.path), m.role, reason: d.path);
        }
      }
    });
  });

  group('network layer', () {
    test('dio uses the versioned base url and client headers', () {
      final container = ProviderContainer(
        overrides: [
          appConfigProvider.overrideWithValue(
            const AppConfig(
              environment: AppEnvironment.development,
              apiBaseUrl: 'http://x',
              appVersion: '9.9.9',
            ),
          ),
        ],
      );
      addTearDown(container.dispose);
      final dio = container.read(dioProvider);
      expect(dio.options.baseUrl, 'http://x/api/v1');
      expect(dio.options.headers['X-App-Version'], '9.9.9');
    });

    test('parses problem+json errors', () {
      RequestOptions req() => RequestOptions(path: '/bookings');
      final e = ApiException.fromDio(
        DioException.badResponse(
          statusCode: 409,
          requestOptions: req(),
          response: Response(
            requestOptions: req(),
            statusCode: 409,
            data: {
              'code': 'BOOKING_OVERLAP',
              'detail': 'Already booked.',
              'request_id': 'r1',
            },
          ),
        ),
      );
      expect(e.kind, ApiErrorKind.conflict);
      expect(e.message, 'Already booked.');
      expect(e.requestId, 'r1');
    });
  });
}
