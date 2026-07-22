class NodeJoinConfiguration {
  const NodeJoinConfiguration({
    required this.nodeUrl,
    required this.nodeSecret,
  });

  final String nodeUrl;
  final String nodeSecret;

  static NodeJoinConfiguration? fromArguments(Iterable<String> arguments) {
    for (final argument in arguments) {
      final configuration = tryParse(argument);
      if (configuration != null) return configuration;
    }
    return null;
  }

  static NodeJoinConfiguration? tryParse(String value) {
    final uri = Uri.tryParse(value.trim());
    if (uri == null || uri.scheme.toLowerCase() != 'nexdrop') return null;
    if (uri.host.toLowerCase() != 'join') return null;
    final node =
        (uri.queryParameters['node'] ?? uri.queryParameters['url'] ?? '')
            .trim();
    final secret =
        (uri.queryParameters['key'] ?? uri.queryParameters['secret'] ?? '')
            .trim();
    if (node.isEmpty || secret.isEmpty) return null;
    return NodeJoinConfiguration(nodeUrl: node, nodeSecret: secret);
  }

  String toUri() => Uri(
    scheme: 'nexdrop',
    host: 'join',
    queryParameters: {'node': nodeUrl, 'key': nodeSecret},
  ).toString();
}
