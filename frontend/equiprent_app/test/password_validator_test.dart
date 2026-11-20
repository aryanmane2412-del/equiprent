import 'package:equiprent_app/features/auth/auth_common.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  test(
    'registration catches a password containing the user name before submit',
    () {
      expect(
        AuthValidators.passwordPersonalInfo(
          'aryan@2006',
          fullName: 'Aryan',
          email: 'aryanmane2412@example.com',
          phone: '8007868587',
        ),
        'Must not contain your name, email or phone number',
      );
    },
  );

  test(
    'personal information check accepts a password unrelated to the account',
    () {
      expect(
        AuthValidators.passwordPersonalInfo(
          'River-Crane!2049',
          fullName: 'Aryan Mane',
          email: 'aryanmane2412@example.com',
          phone: '8007868587',
        ),
        isNull,
      );
    },
  );
}
