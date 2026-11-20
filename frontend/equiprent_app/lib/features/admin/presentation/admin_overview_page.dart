import 'package:flutter/material.dart';

import 'admin_read_only_dashboard.dart';

/// Overview deliberately shares the live, read-only reporting snapshot.
class AdminOverviewPage extends StatelessWidget {
  const AdminOverviewPage({super.key});

  @override
  Widget build(BuildContext context) => const AdminReadOnlyDashboardPage();
}
