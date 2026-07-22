import 'package:flutter/material.dart';

class NexDropPage extends StatelessWidget {
  const NexDropPage({
    super.key,
    required this.title,
    required this.subtitle,
    required this.onRefresh,
    required this.child,
  });

  final String title;
  final String subtitle;
  final Future<void> Function() onRefresh;
  final Widget child;

  @override
  Widget build(BuildContext context) => RefreshIndicator(
    onRefresh: onRefresh,
    child: ListView(
      padding: const EdgeInsets.all(28),
      children: [
        Text(
          title,
          style: Theme.of(
            context,
          ).textTheme.headlineMedium?.copyWith(fontWeight: FontWeight.w800),
        ),
        const SizedBox(height: 4),
        Text(
          subtitle,
          style: Theme.of(
            context,
          ).textTheme.bodyLarge?.copyWith(color: Colors.blueGrey),
        ),
        const SizedBox(height: 24),
        child,
      ],
    ),
  );
}
