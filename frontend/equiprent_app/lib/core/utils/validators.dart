/// Input validators shared by every form (sign-in, registration, bookings, fleet, verification).
abstract final class Validators {
  static final _indian = RegExp(r'^[6-9]\d{9}$');
  static final _e164 = RegExp(r'^\+[1-9]\d{7,14}$');

  /// Normalises a mobile number to E.164 (10-digit Indian numbers get +91); null when invalid.
  static String? normalisePhone(String raw) {
    final digits = raw.replaceAll(RegExp(r'[\s\-()]'), '');
    if (_indian.hasMatch(digits)) return '+91$digits';
    if (digits.startsWith('91') && digits.length == 12 && _indian.hasMatch(digits.substring(2))) return '+$digits';
    if (_e164.hasMatch(digits)) return digits;
    return null;
  }

  /// Form validator: a valid mobile number, with or without +91.
  static String? phone(String? v) => normalisePhone(v ?? '') == null ? 'Enter a 10-digit mobile number' : null;

  /// Like [phone] but an empty value is allowed.
  static String? optionalPhone(String? v) => (v ?? '').trim().isEmpty ? null : phone(v);

  /// Calendar date written as YYYY-MM-DD (what the pickers produce).
  static String? isoDate(String? v, {bool required = false, bool future = false}) {
    final s = (v ?? '').trim();
    if (s.isEmpty) return required ? 'Pick a date' : null;
    final d = RegExp(r'^\d{4}-\d{2}-\d{2}$').hasMatch(s) ? DateTime.tryParse(s) : null;
    if (d == null) return 'Pick a date from the calendar';
    if (future && d.isBefore(DateTime(DateTime.now().year, DateTime.now().month, DateTime.now().day))) return 'This date has passed';
    return null;
  }
}
