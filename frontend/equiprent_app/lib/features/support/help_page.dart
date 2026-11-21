import 'package:flutter/material.dart';

import '../../core/widgets/widgets.dart' hide Review, ChatMessage, AppNotification;

const _faqs = <(String, String)>[
  ('How do I book a machine?', 'Search or browse equipment, pick dates and your work site, and send the request. The owner accepts it and you pay the advance to confirm.'),
  ('How do I cancel a booking?', 'Open the booking and choose Request cancellation. The fee, if any, is shown before you confirm.'),
  ('Why can I not confirm handover?', 'Both you and the owner or driver must acknowledge the handover inspection first.'),
  ('How do I change the language?', 'Open Settings and pick English, Hindi or Marathi under Language. It is remembered on this device.'),
  ('How do I reach support?', 'Use the Customer care button in the top bar to start a conversation with the EQUIPRENT team.'),
];

class HelpPage extends StatelessWidget {
  const HelpPage({super.key});

  @override
  Widget build(BuildContext context) => AppPage(
        title: 'Help & support',
        topLevel: false,
        body: PageBody(children: [
          for (final f in _faqs)
            AppCard(
              child: ExpansionTile(
                tilePadding: EdgeInsets.zero,
                title: Text(f.$1, style: Theme.of(context).textTheme.titleSmall),
                children: [Align(alignment: Alignment.centerLeft, child: Padding(padding: const EdgeInsets.only(bottom: 12), child: Text(f.$2)))],
              ),
            ),
        ]),
      );
}
