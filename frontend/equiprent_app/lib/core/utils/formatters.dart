import 'package:intl/intl.dart';

/// Formatting helpers for Indian users: ₹ with lakh/crore grouping, IST-style
/// dates (dd MMM). Amounts in mock data are whole rupees; the API will send
/// paise (see docs/API_DESIGN.md) and convert at the repository boundary.
abstract final class Fmt {
  static final _inr = NumberFormat.currency(locale: 'en_IN', symbol: '₹', decimalDigits: 0);
  static final _num = NumberFormat.decimalPattern('en_IN');

  /// ₹1,23,456
  static String inr(num amount) => _inr.format(amount);

  /// ₹1.2L / ₹3.4Cr / ₹85K, for dashboards.
  static String inrCompact(num amount) {
    final a = amount.abs();
    final sign = amount < 0 ? '-' : '';
    if (a >= 10000000) return '$sign₹${_trim(a / 10000000)}Cr';
    if (a >= 100000) return '$sign₹${_trim(a / 100000)}L';
    if (a >= 1000) return '$sign₹${_trim(a / 1000)}K';
    return inr(amount);
  }

  static String number(num n) => _num.format(n);

  static String percent(double ratio, {int decimals = 0}) => '${(ratio * 100).toStringAsFixed(decimals)}%';

  static String _trim(double v) {
    final s = v.toStringAsFixed(v >= 100 ? 0 : 1);
    return s.endsWith('.0') ? s.substring(0, s.length - 2) : s;
  }

  static String date(DateTime d) => DateFormat('d MMM yyyy').format(d);
  static String shortDate(DateTime d) => DateFormat('d MMM').format(d);
  static String dayDate(DateTime d) => DateFormat('EEE, d MMM').format(d);
  static String time(DateTime d) => DateFormat('h:mm a').format(d);
  static String dateTime(DateTime d) => DateFormat('d MMM, h:mm a').format(d);
  static String month(DateTime d) => DateFormat('MMMM yyyy').format(d);

  static String dateRange(DateTime a, DateTime b) =>
      a.month == b.month && a.year == b.year ? '${a.day}–${DateFormat('d MMM').format(b)}' : '${shortDate(a)} – ${shortDate(b)}';

  /// "2h ago", "3d ago" relative to [now].
  static String ago(DateTime t, DateTime now) {
    final d = now.difference(t);
    if (d.inMinutes < 1) return 'just now';
    if (d.inMinutes < 60) return '${d.inMinutes}m ago';
    if (d.inHours < 24) return '${d.inHours}h ago';
    if (d.inDays < 7) return '${d.inDays}d ago';
    return shortDate(t);
  }

  static String hours(double h) => '${h % 1 == 0 ? h.toInt() : h.toStringAsFixed(1)} hrs';

  static String initials(String name) {
    final parts = name.trim().split(RegExp(r'\s+')).where((p) => p.isNotEmpty).toList();
    if (parts.isEmpty) return '?';
    if (parts.length == 1) return parts.first.substring(0, 1).toUpperCase();
    return (parts.first[0] + parts.last[0]).toUpperCase();
  }
}
