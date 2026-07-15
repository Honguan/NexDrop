import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'models.dart';

class ApiException implements Exception {
  const ApiException(this.code, this.statusCode);

  final String code;
  final int statusCode;

  @override
  String toString() => code;
}

class ApiClient {
  ApiClient({http.Client? client, FlutterSecureStorage? secureStorage})
    : _client = client ?? http.Client(),
      _storage = secureStorage ?? const FlutterSecureStorage();

  static const protocolVersion = '1.1';
  static const clientVersion = 'nexdrop-v1.1';
  static const _nodeKey = 'nexdrop.node_url';
  static const _accessKey = 'nexdrop.access_token';
  static const _refreshKey = 'nexdrop.refresh_token';

  final http.Client _client;
  final FlutterSecureStorage _storage;
  Uri? _node;
  String? _accessToken;
  String? _refreshToken;
  Future<bool>? _refreshing;

  Uri? get node => _node;
  bool get authenticated => _accessToken != null;

  Future<bool> restore() async {
    final node = await _storage.read(key: _nodeKey);
    _accessToken = await _storage.read(key: _accessKey);
    _refreshToken = await _storage.read(key: _refreshKey);
    if (node == null) return false;
    _node = validateNodeUrl(node);
    return _accessToken != null && _refreshToken != null;
  }

  Future<UserAccount> login(
    String nodeUrl,
    String identifier,
    String password,
  ) async {
    _node = validateNodeUrl(nodeUrl);
    final response = await _client.post(
      _uri('/api/auth/login'),
      headers: const {'Content-Type': 'application/json'},
      body: jsonEncode({'identifier': identifier.trim(), 'password': password}),
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
        headers: const {'Content-Type': 'application/json'},
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

  Future<List<GroupSummary>> groups() async =>
      ((await getJson('/api/groups')) as List<dynamic>)
          .map((value) => GroupSummary.fromJson(value as Map<String, dynamic>))
          .toList();

  Future<List<TransferSummary>> transfers() async =>
      ((await getJson('/api/transfers')) as List<dynamic>)
          .map(
            (value) => TransferSummary.fromJson(value as Map<String, dynamic>),
          )
          .toList();

  Future<dynamic> getJson(String path) => _request(path, method: 'GET');

  Future<dynamic> sendJson(String path, String method, [Object? body]) =>
      _request(path, method: method, body: body);

  Future<void> uploadChunk(
    String fileId,
    int index,
    List<int> bytes,
    String sha256Hex,
  ) async {
    final response = await _authorized(
      () => _client.post(
        _uri('/api/files/$fileId/chunks/$index'),
        headers: _headers({
          'X-Chunk-SHA256': sha256Hex,
          'Content-Type': 'application/octet-stream',
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
    final response = await _authorized(() {
      final request = http.Request(method, _uri(path));
      request.headers.addAll(
        _headers(
          body == null ? null : const {'Content-Type': 'application/json'},
        ),
      );
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
          headers: const {'Content-Type': 'application/json'},
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
      _storage.write(key: _nodeKey, value: _node.toString()),
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
      return ApiException(
        (jsonDecode(response.body) as Map<String, dynamic>)['error']
                as String? ??
            'REQUEST_FAILED',
        response.statusCode,
      );
    } catch (_) {
      return ApiException('REQUEST_FAILED', response.statusCode);
    }
  }

  static Uri validateNodeUrl(String value) {
    final uri = Uri.parse(value.trim());
    final localDevelopment =
        uri.scheme == 'http' &&
        (uri.host == '127.0.0.1' || uri.host == 'localhost');
    if ((!localDevelopment && uri.scheme != 'https') ||
        !uri.hasAuthority ||
        uri.userInfo.isNotEmpty ||
        uri.pathSegments.isNotEmpty ||
        uri.hasQuery ||
        uri.hasFragment) {
      throw const FormatException('請輸入有效的 HTTPS 節點網址');
    }
    return uri;
  }

  void close() => _client.close();
}
