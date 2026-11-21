import 'dart:async';

import 'package:equiprent_app/features/verification/presentation/verification_widgets.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

Widget _host(Future<bool> Function(BuildContext) run, void Function(bool) done) => MaterialApp(
      home: Scaffold(
        body: Builder(
          builder: (context) => Center(
            child: ElevatedButton(
              onPressed: () async => done(await run(context)),
              child: const Text('go'),
            ),
          ),
        ),
      ),
    );

void main() {
  testWidgets('a slow bank check blocks the screen, then closes when it finishes', (tester) async {
    final slow = Completer<void>();
    bool? result;
    await tester.pumpWidget(_host(
      (c) => runVerificationActionWithProgress(c, 'Verifying your account', () => slow.future),
      (v) => result = v,
    ));
    await tester.tap(find.text('go'));
    await tester.pump();
    expect(find.byKey(const Key('verification-progress')), findsOneWidget);
    expect(find.text('Verifying your account'), findsOneWidget);

    // It cannot be dismissed by tapping outside or going back, so it cannot be started twice.
    await tester.tapAt(const Offset(2, 2));
    await tester.pump();
    expect(find.byKey(const Key('verification-progress')), findsOneWidget);

    slow.complete();
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('verification-progress')), findsNothing);
    expect(result, isTrue);
  });

  testWidgets('a failed check also closes the progress dialog and reports failure', (tester) async {
    bool? result;
    await tester.pumpWidget(_host(
      (c) => runVerificationActionWithProgress(c, 'Verifying', () async => throw Exception('bank down')),
      (v) => result = v,
    ));
    await tester.tap(find.text('go'));
    await tester.pumpAndSettle();
    expect(find.byKey(const Key('verification-progress')), findsNothing);
    expect(result, isFalse);
  });
}
