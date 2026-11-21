import 'package:equiprent_app/core/widgets/offline_banner.dart';
import 'package:equiprent_app/app/app.dart';
import 'package:equiprent_app/app/router.dart';
import 'package:equiprent_app/core/auth/auth_api.dart';
import 'package:equiprent_app/core/auth/auth_controller.dart';
import 'package:equiprent_app/core/auth/auth_models.dart';
import 'package:equiprent_app/core/auth/token_store.dart';
import 'package:equiprent_app/core/localization/locale_controller.dart';
import 'package:equiprent_app/core/network/retry_policy.dart';
import 'package:equiprent_app/core/session/active_role_provider.dart';
import 'package:equiprent_app/core/session/user_role.dart';
import 'package:equiprent_app/features/tracking/data/live_track.dart';
import 'package:equiprent_app/features/tracking/data/location_source.dart';
import 'package:equiprent_app/features/tracking/data/tracking_api.dart';
import 'package:equiprent_app/features/bookings/data/booking_api.dart';
import 'package:equiprent_app/features/comms/data/comms_api.dart';
import 'package:equiprent_app/features/comms/push/push_service.dart';
import 'package:equiprent_app/features/discovery/data/discovery_api.dart';
import 'package:equiprent_app/features/fleet_manager/data/fleet_api.dart';
import 'package:equiprent_app/features/listings/data/listings_api.dart';
import 'package:equiprent_app/features/operator/data/operator_api.dart';
import 'package:equiprent_app/features/owner/data/owner_api.dart';
import 'package:equiprent_app/features/admin/presentation/admin_read_only_dashboard.dart';
import 'package:equiprent_app/features/admin/presentation/admin_read_only_records.dart';
import 'package:equiprent_app/features/admin/presentation/admin_finance_pages.dart';
import 'package:equiprent_app/features/verification/data/verification_api.dart';
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:flutter_test/flutter_test.dart';

import 'fake_auth_api.dart';
import 'fake_booking_api.dart';
import 'fake_comms_api.dart';
import 'fake_discovery_api.dart';
import 'fake_listings_api.dart';
import 'fake_operations_api.dart';
import 'fake_tracking_api.dart';
import 'fake_verification_api.dart';

const phone = Size(360, 780);
const tablet = Size(800, 1100);
const desktop = Size(1440, 900);

/// A signed-in state holding [roles] (as the server would grant them).
AuthState signedInAs(List<UserRole> roles) => AuthState.signedIn(
  AuthUser(
    id: 'u-test',
    phone: '+919800000000',
    fullName: 'Test User',
    phoneVerified: true,
    hasPassword: true,
    roles: roles,
    orgs: {
      for (final r in roles)
        if (r != UserRole.admin) r: 'org-${r.name}',
    },
  ),
  AuthTokens(
    accessToken: 'access',
    refreshToken: 'refresh',
    accessExpiresAt: DateTime(2100),
  ),
);

/// Pumps the whole app with reduced motion (so looping animations such as
/// the moving map vehicle and skeleton shimmer stay still) and returns the
/// provider container for driving navigation and inspecting state.
///
/// By default the user is signed in with all five roles so every area can be
/// exercised; pass [auth] (e.g. `const AuthState.signedOut()`) and [api] to
/// test the authentication flows.
Future<ProviderContainer> pumpApp(
  WidgetTester tester, {
  Size size = phone,
  AuthState? auth,
  FakeAuthApi? api,
  TokenStore? tokens,
  FakeVerificationApi? verification,
  FakeListingsApi? listings,
  FakeDiscoveryApi? discovery,
  DeviceLocator? locator,
  FakePicker? picker,
  List<String>? opened,
  FakeCommsApi? comms,
  FakeBookingApi? bookings,
  FakeOperatorApi? operator,
  FakeFleetApi? fleet,
  PushService? push,
  FakeTrackingApi? tracking,
  FakeLiveChannel? live,
  LocationSource? location,
  LocaleStore? localeStore,
}) async {
  tester.view.physicalSize = size;
  tester.view.devicePixelRatio = 1;
  tester.platformDispatcher.accessibilityFeaturesTestValue =
      const FakeAccessibilityFeatures(disableAnimations: true);
  addTearDown(tester.view.reset);
  addTearDown(tester.platformDispatcher.clearAccessibilityFeaturesTestValue);

  final commsApi = comms ?? bookings?.comms ?? FakeCommsApi();
  final container = ProviderContainer(
    retry: apiRetry,
    overrides: [
      authApiProvider.overrideWithValue(api ?? FakeAuthApi()),
      tokenStoreProvider.overrideWithValue(tokens ?? MemoryTokenStore()),
      authControllerProvider.overrideWith(
        () => AuthController(initial: auth ?? signedInAs(UserRole.values)),
      ),
      if (localeStore != null)
        localeStoreProvider.overrideWithValue(localeStore),
      verificationApiProvider.overrideWithValue(
        verification ?? FakeVerificationApi(),
      ),
      listingsApiProvider.overrideWithValue(listings ?? FakeListingsApi()),
      discoveryApiProvider.overrideWithValue(
        discovery ?? FakeDiscoveryApi(listings: listings),
      ),
      deviceLocatorProvider.overrideWithValue(
        locator ?? () async => (lat: 18.5308, lng: 73.8475),
      ),
      documentPickerProvider.overrideWithValue(picker ?? FakePicker()),
      inspectionCameraProvider.overrideWithValue(FakeInspectionCamera()),
      commsApiProvider.overrideWithValue(commsApi),
      bookingApiProvider.overrideWithValue(
        bookings ?? FakeBookingApi(commsApi),
      ),
      operatorApiProvider.overrideWithValue(operator ?? FakeOperatorApi()),
      ownerDashboardProvider.overrideWith(
        (ref) async => const OwnerDashboard(),
      ),
      ownerEarningsProvider.overrideWith((ref) async => const OwnerEarnings()),
      adminAnalyticsProvider.overrideWith(
        (ref, range) async => const AdminAnalyticsSnapshot(
          summary: AdminMonitorSnapshot({}),
          categories: [],
          trends: [],
        ),
      ),
      adminUsersRecordsProvider.overrideWith((ref) async => <AdminRecord>[]),
      adminEquipmentRecordsProvider.overrideWith(
        (ref) async => <AdminRecord>[],
      ),
      adminBookingsRecordsProvider.overrideWith(
        (ref, range) async => <AdminRecord>[],
      ),
      adminPaymentHistoryProvider.overrideWith(
        (ref, range) async => <AdminPaymentRow>[],
      ),
      fleetApiProvider.overrideWithValue(fleet ?? FakeFleetApi()),
      pushServiceProvider.overrideWithValue(push ?? const NoPushService()),
      // No polling timers in tests (they would outlive the widget tree).
      badgePollProvider.overrideWithValue(null),
      apiProbeEnabledProvider.overrideWithValue(false),
      chatPollProvider.overrideWithValue(null),
      trackPollProvider.overrideWithValue(null),
      trackingApiProvider.overrideWithValue(
        tracking ??
            FakeTrackingApi(
              statusOf: (id) => commsApi.statusOf[id] ?? 'dispatched',
            ),
      ),
      liveChannelProvider.overrideWithValue(live ?? FakeLiveChannel()),
      locationSourceProvider.overrideWithValue(
        location ?? FakeLocationSource(),
      ),
      linkOpenerProvider.overrideWithValue((url) async {
        opened?.add(url);
        return true;
      }),
    ],
  );
  addTearDown(container.dispose);
  await tester.pumpWidget(
    UncontrolledProviderScope(
      container: container,
      child: const EquipRentApp(),
    ),
  );
  await tester.pumpAndSettle();
  return container;
}

/// Activates [role] (when given) and navigates to [path], letting simulated
/// loading timers finish.
Future<void> goTo(
  WidgetTester tester,
  ProviderContainer c,
  String path, {
  UserRole? role,
}) async {
  if (role != null) c.read(activeRoleProvider.notifier).select(role);
  c.read(routerProvider).go(path);
  await settle(tester);
}

Future<void> settle(WidgetTester tester) async {
  await tester.pumpAndSettle();
  await tester.pump(const Duration(milliseconds: 700));
  await tester.pumpAndSettle();
}

/// Scrolls [finder] into view and taps it.
Future<void> tapOn(WidgetTester tester, Finder finder) async {
  await tester.ensureVisible(finder.first);
  await tester.pumpAndSettle();
  await tester.tap(finder.first);
  await settle(tester);
}
