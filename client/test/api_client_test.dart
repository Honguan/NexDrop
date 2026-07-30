import 'package:flutter_test/flutter_test.dart';
import 'package:http/http.dart' as http;
import 'package:http/testing.dart';
import 'package:nexdrop_client/core/api_client.dart';

void main() {
  group('API contract compatibility', () {
    const cases = {
      'legacy error': '{"error":"INVALID_CREDENTIALS"}',
      'v1 error':
          '{"error":{"code":"INVALID_CREDENTIALS","message":"Invalid credentials","request_id":"request-1","details":{}}}',
    };

    for (final entry in cases.entries) {
      test('parses ${entry.key} and requests the v1 media type', () async {
        late http.Request captured;
        final client = MockClient((request) async {
          captured = request;
          return http.Response(entry.value, 401);
        });
        final api = ApiClient(client: client);

        await expectLater(
          api.login(
            'https://node.example',
            'node-secret',
            'user',
            'password',
            '123456',
          ),
          throwsA(
            isA<ApiException>()
                .having((error) => error.code, 'code', 'INVALID_CREDENTIALS')
                .having((error) => error.statusCode, 'statusCode', 401),
          ),
        );

        final accept = captured.headers.entries
            .singleWhere((header) => header.key.toLowerCase() == 'accept')
            .value;
        expect(accept, 'application/vnd.nexdrop.v1+json');
        expect(captured.headers['X-NexDrop-Node-Key'], 'node-secret');
        expect(captured.body, contains('"totp":"123456"'));
      });
    }

    test('rejects an empty node key before sending a request', () async {
      var requested = false;
      final api = ApiClient(
        client: MockClient((_) async {
          requested = true;
          return http.Response('{}', 200);
        }),
      );

      await expectLater(
        api.login('https://node.example', '   ', 'user', 'password', ''),
        throwsA(
          isA<ApiException>()
              .having((error) => error.code, 'code', 'NODE_KEY_REQUIRED')
              .having((error) => error.statusCode, 'statusCode', 401),
        ),
      );
      expect(requested, isFalse);
    });

    test('preserves Retry-After and formats a rate limit message', () async {
      final api = ApiClient(
        client: MockClient(
          (_) async => http.Response(
            '{"error":{"code":"RATE_LIMITED"}}',
            429,
            headers: {'retry-after': '42'},
          ),
        ),
      );

      await expectLater(
        api.login(
          'https://node.example',
          'node-secret',
          'user',
          'password',
          '123456',
        ),
        throwsA(
          isA<ApiException>()
              .having((error) => error.code, 'code', 'RATE_LIMITED')
              .having(
                (error) => error.retryAfterSeconds,
                'retryAfterSeconds',
                42,
              )
              .having(apiExceptionMessage, 'message', '操作過於頻繁，請在 42 秒後再試'),
        ),
      );
    });

    test('explains which compatibility party is unavailable', () {
      const error = ApiException(
        'CAPABILITY_UNAVAILABLE',
        409,
        details: {
          'capability': 'resumable_chunks',
          'party': 'node',
        },
      );

      expect(
        apiExceptionMessage(error),
        '目前的節點缺少 resumable_chunks 相容能力，請更新後再試',
      );
    });

    test('parses capability documents and ignores unknown additive fields', () {
      final document = NodeCapabilityDocument.fromJson({
        'nodeIdentity': 'node-1',
        'versionFingerprint': 'fingerprint-1',
        'protocolVersion': '1.2',
        'capabilities': [
          'capability_negotiation',
          'future_unknown',
          42,
        ],
        'limits': {
          'maxChunkSize': 33554432,
          'maxParallelChunks': 6,
          'maxRecipients': 100,
          'futureLimit': true,
        },
        'futureField': {'ignored': true},
      }, fallbackNodeIdentity: 'https://fallback.example');

      expect(document.nodeIdentity, 'node-1');
      expect(document.versionFingerprint, 'fingerprint-1');
      expect(document.supports('capability_negotiation'), isTrue);
      expect(document.supports('missing'), isFalse);
      expect(document.supports('future_unknown'), isFalse);
      expect(document.capabilities, contains('future_unknown'));
      expect(document.limits.maxChunkSize, 33554432);
      expect(document.limits.maxParallelChunks, 6);
      expect(document.limits.maxRecipients, 100);
    });

    test('legacy version response produces a safe empty capability set', () {
      final document = NodeCapabilityDocument.fromJson({
        'productVersion': '1.0.0',
        'buildCommit': 'legacy',
        'protocolVersion': '1.0',
      }, fallbackNodeIdentity: 'https://legacy.example');

      expect(document.nodeIdentity, 'https://legacy.example');
      expect(document.capabilities, isEmpty);
      expect(document.protocolVersion, '1.0');
      expect(document.limits.maxChunkSize, 8 * 1024 * 1024);
      expect(compatibleProtocol(document), '1.0');
    });

    test('uses the Node protocol generation for mixed-version handshakes', () {
      final previous = NodeCapabilityDocument.fromJson({
        'protocolVersion': '1.1',
      }, fallbackNodeIdentity: 'node-previous');
      final unsupported = NodeCapabilityDocument.fromJson({
        'protocolVersion': '2.0',
      }, fallbackNodeIdentity: 'node-future');

      expect(compatibleProtocol(previous), '1.1');
      expect(compatibleProtocol(unsupported), '1.0');
      expect(compatibleProtocol(null), '1.0');
    });
  });
}
