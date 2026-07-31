import 'dart:convert';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
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
        expect(captured.headers.containsKey('X-NexDrop-Node-Key'), isFalse);
        expect(captured.body, contains('"totp":"123456"'));
      });
    }

    test('allows authentication without a Node key', () async {
      var loginRequested = false;
      final api = ApiClient(
        client: MockClient((request) async {
          if (request.url.path == '/api/version') {
            return http.Response('{}', 200);
          }
          loginRequested = true;
          expect(request.headers.containsKey('X-NexDrop-Node-Key'), isFalse);
          return http.Response('{"error":"INVALID_CREDENTIALS"}', 401);
        }),
      );

      await expectLater(
        api.login('https://node.example', '   ', 'user', 'password', ''),
        throwsA(
          isA<ApiException>()
              .having((error) => error.code, 'code', 'INVALID_CREDENTIALS')
              .having((error) => error.statusCode, 'statusCode', 401),
        ),
      );
      expect(loginRequested, isTrue);
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
          'adaptive_route_racing',
          'scoped_device_enrollment',
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
      expect(document.supports('adaptive_route_racing'), isTrue);
      expect(document.supports('scoped_device_enrollment'), isTrue);
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

    test('reuses one transfer when a LAN route falls back to Node', () async {
      FlutterSecureStorage.setMockInitialValues({
        'nexdrop.node_url': 'https://node.example',
        'nexdrop.access_token': 'access-token',
        'nexdrop.refresh_token': 'refresh-token',
      });
      var createCount = 0;
      var deleteCount = 0;
      var routeSwitchCount = 0;
      final transfer = <String, dynamic>{
        'id': 'transfer-1',
        'senderDeviceId': 'sender-1',
        'contentType': 'FILE',
        'status': 'TRANSFERRING_LAN',
        'createdAt': '2026-07-31T12:00:00Z',
        'updatedAt': '2026-07-31T12:00:00Z',
        'targets': [
          {
            'deviceId': 'target-1',
            'route': 'LAN',
            'status': 'TRANSFERRING_LAN',
            'bytesTransferred': 0,
          },
        ],
        'files': <dynamic>[],
        'fileTargets': <dynamic>[],
      };
      final client = MockClient((request) async {
        if (request.url.path == '/api/version') {
          return http.Response(
            jsonEncode({
              'nodeIdentity': 'node-1',
              'protocolVersion': '1.2',
              'capabilities': [
                'capability_negotiation',
                'adaptive_route_racing',
              ],
            }),
            200,
          );
        }
        if (request.url.path == '/api/transfers' && request.method == 'POST') {
          createCount++;
          return http.Response(jsonEncode(transfer), 201);
        }
        if (request.url.path == '/api/transfers/transfer-1/timeline') {
          return http.Response('', 204);
        }
        if (request.url.path == '/api/transfers/transfer-1' &&
            request.method == 'DELETE') {
          deleteCount++;
          return http.Response('', 204);
        }
        if (request.url.path == '/api/v3/transfers/transfer-1/route') {
          routeSwitchCount++;
          final route = jsonDecode(request.body) as Map<String, dynamic>;
          expect(route['deviceId'], 'target-1');
          expect(route['route'], 'NODE');
          return http.Response('', 204);
        }
        if (request.url.path == '/api/transfers/transfer-1' &&
            request.method == 'GET') {
          final refreshed = Map<String, dynamic>.from(transfer);
          refreshed['targets'] = [
            {
              'deviceId': 'target-1',
              'route': 'NODE',
              'status': 'QUEUED',
              'bytesTransferred': 0,
            },
          ];
          return http.Response(jsonEncode(refreshed), 200);
        }
        return http.Response('{"error":"NOT_FOUND"}', 404);
      });
      final api = ApiClient(client: client);
      expect(await api.restore(), isTrue);

      final created = await api.sendJson('/api/transfers', 'POST', {
        'targetDeviceIds': ['target-1'],
        'lanAvailableDeviceIds': ['target-1'],
      }) as Map<String, dynamic>;
      expect(created['id'], 'transfer-1');
      await api.sendJson('/api/transfers/transfer-1/timeline', 'POST', {
        'code': 'ROUTE_FALLBACK_SELECTED',
        'targetDeviceId': 'target-1',
        'route': 'NODE',
      });
      await api.sendJson('/api/transfers/transfer-1', 'DELETE');
      final resumed = await api.sendJson('/api/transfers', 'POST', {
        'targetDeviceIds': ['target-1'],
        'lanAvailableDeviceIds': <String>[],
      }) as Map<String, dynamic>;

      expect(resumed['id'], 'transfer-1');
      expect(createCount, 1);
      expect(deleteCount, 0);
      expect(routeSwitchCount, 1);
    });
  });
}
