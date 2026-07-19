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
        expect(captured.body, contains('"totp":"123456"'));
      });
    }

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
              .having(
                apiExceptionMessage,
                'message',
                '操作過於頻繁，請在 42 秒後再試',
              ),
        ),
      );
    });
  });
}
