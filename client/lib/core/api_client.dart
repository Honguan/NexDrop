import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'models.dart';

class ApiException implements Exception {
  const ApiException(this.code, this.statusCode, {this.retryAfterSeconds});

  final String code;
  final int statusCode;
  final int? retryAfterSeconds;

  @override
  String toString() => code;
}

String apiExceptionMessage(ApiException error) {
  if (error.code == 'RATE_LIMITED') {
    return error.retryAfterSeconds == null
        ? '操作過於頻繁，請稍後再試'
        : '操作過於頻繁，請在 ${error.retryAfterSeconds} 秒後再試';
  }
  return {
        'INVALID_REQUEST': '請確認所有必填欄位與格式',
        'INVALID_CREDENTIALS': '帳號或密碼不正確',
        'NODE_KEY_REQUIRED': '節點密鑰不正確或尚未設定',
        'PERMISSION_DENIED': '你沒有執行此操作的權限',
        'INVALID_TOKEN': '登入已失效，請重新登入',
        'FILE_TOO_LARGE': '檔案超過節點限制，請等待區網傳送',
        'QUOTA_EXCEEDED': '已超過可用配額',
      }[error.code] ??
      '操作失敗：${error.code}';
}

class ApiClient {
  ApiClient({http.Client? client, FlutterSecureStorage? secureStorage})
    : _client = client ?? http.Client(),
      _storage = secureStorage ?? const FlutterSecureStorage();

  static const protocolVersion = '1.1';
  static const clientVersion = 'nexdrop-v1.1';
  static const _nodeUrlKey = 'nexdrop.node_url';
  static const _nodeSecretKey = 'nexdrop.node_secret';
  static const _accessKey = 'nexdrop.access_token';
  static const _refreshKey = 'nexdrop.refresh_token';
  static const _accept = 'application/vnd.nexdrop.v1+json';
  static const _uuid = Uuid();

  final http.Client _client;
  final FlutterSecureStorage _storage;
  Uri? _node;
  String? _nodeSecret;
  String? _accessToken;
  String? _refreshToken;
  Future<bool>? _refreshing;

  Uri? get node => _node;
  bool get authenticated => _accessToken != null;
  String? get nodeJoinUri {
    final node = _node;
    final secret = _nodeSecret?.trim() ?? '';
    if (node == null || secret.isEmpty) return null;
    return Uri(
      scheme: 'nexdrop',
      host: 'join',
      queryParameters: {'node': node.toString(), 'key': secret},
    ).toString();
  }

  Future<bool> restore() async {
    final node = await _storage.read(key: _nodeUrlKey);
    _nodeSecret = await _storage.read(key: _nodeSecretKey);
    _accessToken = await _storage.read(key: _accessKey);
    _refreshToken = await _storage.read(key: _refreshKey);
    if (node == null) return false;
    _node = validateNodeUrl(node);
    return _accessToken != null &&
        _refreshToken != null &&
        _nodeSecret?.trim().isNotEmpty == true;
  }

  Future<UserAccount> login(
    String nodeUrl,
    String nodeSecret,
    String identifier,
    String password,
    String totp,
  ) async {
    _node = validateNodeUrl(nodeUrl);
    _nodeSecret = nodeSecret.trim();
    if (_nodeSecret!.isEmpty) {
      throw const ApiException('NODE_KEY_REQUIRED', 401);
    }
    final response = await _client.post(
      _uri('/api/auth/login'),
      headers: {
        'Content-Type': 'application/json',
        'Accept': _accept,
        'X-NexDrop-Node-Key': _nodeSecret!,
      },
      body: jsonEncode({
        'identifier': identifier.trim(),
        'password': password,
        'totp': totp.trim(),
      }),
    );
    if (response.statusCode != HttpStatus.ok) throw _error(response);
    await _saveTokens(jsonDecode(response.body) as Map<String, dynamic>);
    return account();
  }

  Future<void> logout() async {
    final refreshToken = _refreshToken;
    await _clearTokens();
    if (refreshToken != null && _node != null) {
      await _client.post(
        _uri('/api/auth/logout'),
        headers: const {'Content-Type': 'application/json', 'Accept': _accept},
        body: jsonEncode({'refreshToken': refreshToken}),
      );
    }
  }

  Future<UserAccount> account() async => UserAccount.fromJson(
    await getJson('/api/account') as Map<String, dynamic>,
  );

  Future<List<Device>> devices() async =>
      ((await getJson('/api/devices')) as List<dynamic>)
          .map((value) => Device.fromJson(value as Map<String, dynamic>))
          .toList();

  Future<List<DeviceStatistic>> deviceStatistics() async =>
      ((await getJson('/api/statistics/devices')) as List<dynamic>)
          .map(
            (value) => DeviceStatistic.fromJson(value as Map<String, dynamic>),
          )
          .toList();

  Future<List<GroupSummary>> groups() async =>
      ((await getJson('/api/groups')) as List<dynamic>)
          .map((value) => GroupSummary.fromJson(value as Map<String, dynamic>))
          .toList();

  Future<List<TransferSummary>> transfers() async {
    final response =
        await getJson('/api/transfers?limit=100') as Map<String, dynamic>;
    return (response['items'] as List<dynamic>)
        .map((value) => TransferSummary.fromJson(value as Map<String, dynamic>))
        .toList();
  }

  Future<dynamic> getJson(String path) => _request(path, method: 'GET');

  Future<dynamic> sendJson(String path, String method, [Object? body]) =>
      _request(path, method: method, body: body);

  Future<void> uploadChunk(
    String fileId,
    int index,
    List<int> bytes,
    String sha256Hex,
  ) async {
    final idempotencyKey = _uuid.v4();
    final response = await _authorized(
      () => _client.post(
        _uri('/api/files/$fileId/chunks/$index'),
        headers: _headers({
          'X-Chunk-SHA256': sha256Hex,
          'Content-Type': 'application/octet-stream',
          'Idempotency-Key': idempotencyKey,
        }),
        body: bytes,
      ),
    );
    if (response.statusCode != HttpStatus.ok) throw _error(response);
  }

  Future<List<int>> downloadChunk(String fileId, int index) async {
    final response = await _authorized(
      () => _client.get(
        _uri('/api/files/$fileId/chunks/$index'),
        headers: _headers(),
      ),
    );
    if (response.statusCode != HttpStatus.ok) throw _error(response);
    return response.bodyBytes;
  }

  Future<WebSocketChannel> connectWebSocket() async {
    if (_node == null || _accessToken == null) {
      throw const ApiException('AUTHENTICATION_REQUIRED', 401);
    }
    final uri = _node!.replace(
      scheme: _node!.scheme == 'https' ? 'wss' : 'ws',
      path: '/ws',
      queryParameters: {
        'protocolVersion': protocolVersion,
        'clientVersion': clientVersion,
      },
    );
    return IOWebSocketChannel.connect(
      uri,
      protocols: const ['nexdrop.v1'],
      headers: {'Authorization': 'Bearer $_accessToken'},
      pingInterval: const Duration(seconds: 15),
    );
  }

  Future<dynamic> _request(
    String path, {
    required String method,
    Object? body,
  }) async {
    final idempotencyKey = method == 'GET' ? null : _uuid.v4();
    final response = await _authorized(() {
      final request = http.Request(method, _uri(path));
      request.headers.addAll(
        _headers(
          body == null ? null : const {'Content-Type': 'application/json'},
        ),
      );
      if (idempotencyKey != null) {
        request.headers['Idempotency-Key'] = idempotencyKey;
      }
      if (body != null) request.body = jsonEncode(body);
      return _client.send(request).then(http.Response.fromStream);
    });
    if (response.statusCode < 200 || response.statusCode >= 300) {
      throw _error(response);
    }
    if (response.statusCode == HttpStatus.noContent || response.body.isEmpty) {
      return null;
    }
    return jsonDecode(response.body);
  }

  Future<http.Response> _authorized(
    Future<http.Response> Function() send, {
    bool retry = true,
  }) async {
    var response = await _sendWithRetry(send);
    if (response.statusCode == HttpStatus.unauthorized &&
        retry &&
        await _refresh()) {
      response = await _sendWithRetry(send);
    }
    return response;
  }

  Future<http.Response> _sendWithRetry(
    Future<http.Response> Function() send,
  ) async {
    for (var attempt = 0; ; attempt++) {
      try {
        final response = await send();
        if (response.statusCode < 500 || attempt >= 3) return response;
      } on SocketException {
        if (attempt >= 3) rethrow;
      } on http.ClientException {
        if (attempt >= 3) rethrow;
      }
      await Future<void>.delayed(Duration(seconds: 1 << attempt));
    }
  }

  Future<bool> _refresh() {
    if (_refreshing != null) return _refreshing!;
    _refreshing = (() async {
      try {
        if (_refreshToken == null) return false;
        final response = await _client.post(
          _uri('/api/auth/refresh'),
          headers: const {
            'Content-Type': 'application/json',
            'Accept': _accept,
          },
          body: jsonEncode({'refreshToken': _refreshToken}),
        );
        if (response.statusCode != HttpStatus.ok) throw _error(response);
        await _saveTokens(jsonDecode(response.body) as Map<String, dynamic>);
        return true;
      } catch (_) {
        await _clearTokens();
        return false;
      } finally {
        _refreshing = null;
      }
    })();
    return _refreshing!;
  }

  Map<String, String> _headers([Map<String, String>? extra]) => {
    'Authorization': 'Bearer $_accessToken',
    'Accept': _accept,
    if (_nodeSecret?.trim().isNotEmpty == true)
      'X-NexDrop-Node-Key': _nodeSecret!.trim(),
    ...?extra,
  };

  Uri _uri(String path) {
    if (_node == null) throw const ApiException('NODE_NOT_CONFIGURED', 0);
    return _node!.replace(path: path, query: null);
  }

  Future<void> _saveTokens(Map<String, dynamic> json) async {
    _accessToken = json['accessToken'] as String;
    _refreshToken = json['refreshToken'] as String;
    await Future.wait([
      _storage.write(key: _nodeUrlKey, value: _node.toString()),
      _storage.write(key: _nodeSecretKey, value: _nodeSecret),
      _storage.write(key: _accessKey, value: _accessToken),
      _storage.write(key: _refreshKey, value: _refreshToken),
    ]);
  }

  Future<void> _clearTokens() async {
    _accessToken = null;
    _refreshToken = null;
    await Future.wait([
      _storage.delete(key: _accessKey),
      _storage.delete(key: _refreshKey),
    ]);
  }

  ApiException _error(http.Response response) {
    try {
      final json = jsonDecode(response.body) as Map<String, dynamic>;
      final error = json['error'];
      return ApiException(
        error is String
            ? error
            : (error as Map<String, dynamic>?)?['code'] as String? ??
                  'REQUEST_FAILED',
        response.statusCode,
        retryAfterSeconds: int.tryParse(response.headers['retry-after'] ?? ''),
      );
    } catch (_) {
      return ApiException(
        'REQUEST_FAILED',
        response.statusCode,
        retryAfterSeconds: int.tryParse(response.headers['retry-after'] ?? ''),
      );
    }
  }

  static Uri validateNodeUrl(String value) {
    final uri = Uri.parse(value.trim());
    final address = InternetAddress.tryParse(uri.host);
    final localDevelopment =
        uri.scheme == 'http' &&
        (uri.host == '127.0.0.1' || uri.host == 'localhost' || address != null);
    if ((!localDevelopment && uri.scheme != 'https') ||
        !uri.hasAuthority ||
        uri.userInfo.isNotEmpty ||
        uri.pathSegments.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment) {
      throw const FormatException('請輸入有效的 HTTPS 網址或 HTTP IP 節點網址');
    }
    return uri;
  }

  void close() => _client.close();
}
