import 'package:flutter_test/flutter_test.dart';
import 'package:nexdrop_client/core/node_join.dart';

void main() {
  test('parses a NexDrop join URI', () {
    final value = NodeJoinConfiguration.tryParse(
      'nexdrop://join?node=http%3A%2F%2F192.168.1.20%3A8080&key=node-secret',
    );

    expect(value?.nodeUrl, 'http://192.168.1.20:8080');
    expect(value?.nodeSecret, 'node-secret');
  });

  test('rejects incomplete or unrelated URIs', () {
    expect(NodeJoinConfiguration.tryParse('https://example.com'), isNull);
    expect(
      NodeJoinConfiguration.tryParse('nexdrop://join?node=https://n.example'),
      isNull,
    );
  });

  test('round trips encoded node details', () {
    const source = NodeJoinConfiguration(
      nodeUrl: 'https://drop.example.com',
      nodeSecret: 'a secret/+value',
    );

    final parsed = NodeJoinConfiguration.tryParse(source.toUri());
    expect(parsed?.nodeUrl, source.nodeUrl);
    expect(parsed?.nodeSecret, source.nodeSecret);
  });
}
