import 'package:equiprent_app/features/auth/auth_common.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  testWidgets(
    'demo account labels contain one equipment owner without a city',
    (tester) async {
      await tester.pumpWidget(
        MaterialApp(
          home: Scaffold(body: DemoAccountsCard(onPick: (_, _) {})),
        ),
      );
      expect(find.text('Customer'), findsOneWidget);
      expect(find.text('Equipment owner'), findsOneWidget);
      expect(find.text('Admin'), findsOneWidget);
      expect(find.textContaining('Pune'), findsNothing);
      expect(find.textContaining('Mumbai'), findsNothing);
      expect(
        find.byKey(const ValueKey('demo-owner.pune@equiprent.dev')),
        findsOneWidget,
      );
    },
  );
}
