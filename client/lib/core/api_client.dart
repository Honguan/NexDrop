import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:http/http.dart' as http;
import 'package:uuid/uuid.dart';
import 'package:web_socket_channel/io.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'models.dart';

const nexDropCapabilities = <String>[
  'capability_negotiation',
  'structured_errors',
  'cursor_pagination',
  'idempotency_replay',
  'resumable_chunks',
  'realtime_versions',
  'adaptive_route_racing',
  'adaptive_transfer_profile',
  'transfer_recovery',
  'scoped_device_enrollment',
  'offline_delivery_policies',
  'relay_pool',
  'folder_manifest',
  'message_lifecycle',
];
const nexDropProtocols = <String>{'1.0', '1.1', '1.2'};

String compatibleProtocol(NodeCapabilityDocument? document) {
  final advertised = document?.protocolVersion;
  return advertised != null && nexDropProtocols.contains(advertised)
      ? advertised
      : '1.0';
}

class ApiException implements Exception {
  const ApiException(
    this.code,
    this.statusCode, {
    this.retryAfterSeconds,
    this.details = const <String, dynamic>{},
  });

  final String code;
  final int statusCode;
  final int? retryAfterSeconds;
  final Map<String, dynamic> details;

  @override
  String toString() => code;
}

String apiExceptionMessage(ApiException error) {
  if (error.code == 'RATE_LIMITED') {
    return error.retryAfterSeconds == null
        ? '操作過於頻繁，請稍後再試'
        : '操作過於頻繁，請在 ${error.retryAfterSeconds} 秒後再試';
  }
  if (error.code == 'CAPABILITY_UNAVAILABLE') {
    final party = error.details['party'] == 'node' ? '節點' : '目標設備';
    final capability = error.details['capability'] as String?;
    return capability == null
        ? '目前的$party版本不支援此功能，請更新後再試'
        : '目前的$party缺少 $capability 相容能力，請更新後再試';
  }
  return {
        'INVALID_REQUEST': '請確認所有必填欄位與格式',
        'INVALID_CREDENTIALS': '帳號或密碼不正確',
        'NODE_KEY_REQUIRED': '節點密鑰不正確或尚未設定',
        'ENROLLMENT_TOKEN_EXPIRED': '裝置註冊權杖已過期',
        'ENROLLMENT_TOKEN_EXHAUSTED': '裝置註冊權杖已使用完畢',
        'ENROLLMENT_TOKEN_REVOKED': '裝置註冊權杖已撤銷',
        'PERMISSION_DENIED': '你沒有執行此操作的權限',
        'INVALID_TOKEN': '登入已失效，請重新登入',
        'FILE_TOO_LARGE': '檔案超過節點限制，請等待區網傳送',
        'QUOTA_EXCEEDED': '已超過可用配額',
      }[error.code] ??
      '操作失敗：${error.code}';
}

class NodeLimits {
  const NodeLimits({
    required this.maxChunkSize,
    required this.maxParallelChunks,
    required this.maxRecipients,
  });

  factory NodeLimits.fromJson(Object? value) {
    final json = value is Map<String, dynamic>
        ? value
        : const <String, dynamic>{};
    return NodeLimits(
      maxChunkSize: json['maxChunkSize'] is int
          ? json['maxChunkSize'] as int
          : 8 * 1024 * 1024,
      maxParallelChunks: json['maxParallelChunks'] is int
          ? json['maxParallelChunks'] as int
          : 3,
      maxRecipients: json['maxRecipients'] is int
          ? json['maxRecipients'] as int
          : 100,
    );
  }

  final int maxChunkSize;
  final int maxParallelChunks;
  final int maxRecipients;
}

class NodeCapabilityDocument {
  const NodeCapabilityDocument({
    required this.nodeIdentity,
    required this.versionFingerprint,
    required this.protocolVersion,
    required this.capabilities,
    required this.limits,
  });

  factory NodeCapabilityDocument.fromJson(
    Map<String, dynamic> json, {
    required String fallbackNodeIdentity,
  }) {
    final capabilities = json['capabilities'];
    return NodeCapabilityDocument(
      nodeIdentity: json['nodeIdentity'] as String? ?? fallbackNodeIdentity,
      versionFingerprint:
          json['versionFingerprint'] as String? ??
          [
            json['productVersion'],
            json['buildCommit'],
            json['protocolVersion'],
          ].join('|'),
      protocolVersion: json['protocolVersion'] as String? ?? '1.0',
      capabilities: capabilities is List<dynamic>
          ? capabilities.whereType<String>().toSet()
          : const <String>{},
      limits: NodeLimits.fromJson(json['limits']),
    );
  }

  final String nodeIdentity;
  final String versionFingerprint;
  final String protocolVersion;
  final Set<String> capabilities;
  final NodeLimits limits;

  bool supports(String capability) =>
      nexDropCapabilities.contains(capability) &&
      capabilities.contains(capability);
}

class ApiClient {
  ApiClient({http.Client? client, FlutterSecureStorage? secureStorage})
    : _client = client ?? http.Client(),
      _storage = secureStorage ?? const FlutterSecureStorage();

  static const protocolVersion = '1.2';
  static const clientVersion = 'nexdrop-v1.2';
  static const supportedCapabilities = nexDropCapabilities;
  static const _nodeUrlKey = 'nexdrop.node_url';
  static const _nodeSecretKey = 'nexdrop.node_secret';
  static const _deviceIDKey = 'nexdrop.enrollment.device_id';
  static const _deviceCredentialKey = 'nexdrop.enrollment.credential';
  static const _accessKey = 'nexdrop.access_token';
  static const _refreshKey = 'nexdrop.refresh_token';
  static const _capabilitiesKey = 'nexdrop.node_capabilities.v1';
  static const _accept = 'application/vnd.nexdrop.v1+json';
  static const _uuid = Uuid();

  final http.Client _client;
  final FlutterSecureStorage _storage;
  final Map<String, Map<String, dynamic>> _transferCache = {};
  final Set<String> _fallbackSignals = {};
  Uri? _node;
  String? _nodeSecret;
  String? _deviceID;
  String? _deviceCredential;
  String? _accessToken;
  String? _refreshToken;
  NodeCapabilityDocument? _capabilityDocument;
  bool _capabilityDocumentVerified = false;
  Future<bool>? _refreshing;
  String? _pendingFallbackTransferID;

  Uri? get node => _node;
  bool get authenticated => _accessToken != null;
  String? get enrolledDeviceID => _deviceID;
  bool get hasDeviceCredential =>
      _deviceID?.isNotEmpty == true && _deviceCredential?.isNotEmpty == true;
  bool get hasNodeBootstrapSecret => _nodeSecret?.trim().isNotEmpty == true;
  NodeCapabilityDocument? get capabilityDocument =>
      _capabilityDocumentVerified ? _capabilityDocument : null;
  String? get nodeJoinUri {
    final node = _node;
    final secret = _nodeSecret?.trim() ?? '';
    if (node == null || secret.isEmpty || hasDeviceCredential) return null;
    return Uri(
      scheme: 'nexdrop',
      host: 'join',
      queryParameters: {'node': node.toString(), 'key': secret},
    ).toString();
  }

  Future<bool> restore() async {
    final node = await _storage.read(key: _nodeUrlKey);
    _nodeSecret = await _storage.read(key: _nodeSecretKey);
    _deviceID = await _storage.read(key: _deviceIDKey);
    _deviceCredential = await _storage.read(key: _deviceCredentialKey);
    _accessToken = await _storage.read(key: _accessKey);
    _refreshToken = await _storage.read(key: _refreshKey);
    if (node == null) return false;
    _node = validateNodeUrl(node);
    await _restoreCapabilityCache(_node!);
    try {
      await refreshCapabilities();
    } catch (_) {
      // Legacy or temporarily unavailable Nodes keep the cached-free fallback.
    }
    final restored = _accessToken != null && _refreshToken != null;
    if (restored && hasDeviceCredential) {
      try {
        await attachStoredDevice();
      } catch (_) {
        // A revoked credential is surfaced by the normal device sync flow.
      }
    }
    return restored;
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
    try {
      await refreshCapabilities();
    } catch (_) {
      // Capability negotiation is additive; login remains compatible with 1.x.
    }
    final response = await _client.post(
      _uri('/api/auth/login'),
      headers: {
        'Content-Type': 'application/json',
        'Accept': _accept,
        'X-NexDrop-Capabilities': supportedCapabilities.join(','),
      },
      body: jsonEncode({
        'identifier': identifier.trim(),
        'password': password,
        'totp': totp.trim(),
      }),
    );
    if (response.statusCode != HttpStatus.ok) throw _error(response);
    await _saveTokens(jsonDecode(response.body) as Map<String, dynamic>);
    if (hasDeviceCredential) {
      await attachStoredDevice();
    }
    return account();
  }

  Future<void> logout() async {
    final refreshToken = _refreshToken;
    await _clearTokens();
    if (refreshToken != null && _node != null) {
      await _client.post(
        _uri('/api/auth/logout'),
        headers: {
          'Content-Type': 'application/json',
          'Accept': _accept,
          'X-NexDrop-Capabilities': supportedCapabilities.join(','),
        },
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

  Future<String> bootstrapDevice({
    required String deviceType,
    required String deviceName,
    required List<int> publicKey,
  }) async {
    final nodeSecret = _nodeSecret?.trim() ?? '';
    if (nodeSecret.isEmpty) {
      throw const ApiException('NODE_KEY_REQUIRED', 401);
    }
    final response = await _authorized(
      () => _client.post(
        _uri('/api/v3/enrollment/bootstrap'),
        headers: {
          ..._headers({
            'Content-Type': 'application/json',
            'Idempotency-Key': _uuid.v4(),
          }),
          'X-NexDrop-Node-Key': nodeSecret,
        },
        body: jsonEncode({
          'deviceType': deviceType,
          'deviceName': deviceName,
          'publicKey': base64Encode(publicKey),
          'keyAlgorithm': 'X25519',
        }),
      ),
    );
    if (response.statusCode != HttpStatus.created) throw _error(response);
    final json = jsonDecode(response.body) as Map<String, dynamic>;
    _deviceID = json['deviceId'] as String;
    _deviceCredential = json['deviceCredential'] as String;
    _nodeSecret = null;
    await Future.wait([
      _storage.write(key: _deviceIDKey, value: _deviceID),
      _storage.write(key: _deviceCredentialKey, value: _deviceCredential),
      _storage.delete(key: _nodeSecretKey),
    ]);
    await attachStoredDevice();
    return _deviceID!;
  }

  Future<String> createLegacyDevice({
    required String deviceType,
    required String deviceName,
    required List<int> publicKey,
  }) async {
    final nodeSecret = _nodeSecret?.trim() ?? '';
    if (nodeSecret.isEmpty) {
      throw const ApiException('NODE_KEY_REQUIRED', 401);
    }
    final response = await _authorized(
      () => _client.post(
        _uri('/api/devices'),
        headers: {
          ..._headers({
            'Content-Type': 'application/json',
            'Idempotency-Key': _uuid.v4(),
          }),
          'X-NexDrop-Node-Key': nodeSecret,
        },
        body: jsonEncode({
          'displayName': deviceName,
          'type': deviceType,
          'publicKey': base64Encode(publicKey),
          'keyAlgorithm': 'X25519',
        }),
      ),
    );
    if (response.statusCode != HttpStatus.created) throw _error(response);
    return (jsonDecode(response.body) as Map<String, dynamic>)['id'] as String;
  }

  Future<void> attachStoredDevice() async {
    final deviceID = _deviceID;
    final credential = _deviceCredential;
    if (deviceID == null || credential == null) return;
    await _request(
      '/api/v3/enrollment/attach-session',
      method: 'POST',
      body: {'deviceId': deviceID, 'deviceCredential': credential},
    );
  }

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

  Future<NodeCapabilityDocument> refreshCapabilities() async {
    _capabilityDocumentVerified = false;
    final node = _node;
    if (node == null) {
      throw const ApiException('NODE_NOT_CONFIGURED', 0);
    }
    final response = await _client.get(
      node.replace(path: '/api/version', query: null),
      headers: {
        'Accept': _accept,
        'X-NexDrop-Capabilities': supportedCapabilities.join(','),
      },
    );
    if (response.statusCode != HttpStatus.ok) throw _error(response);
    final document = NodeCapabilityDocument.fromJson(
      jsonDecode(response.body) as Map<String, dynamic>,
      fallbackNodeIdentity: node.origin,
    );
    _capabilityDocument = document;
    _capabilityDocumentVerified = true;
    await _storage.write(
      key: _capabilitiesKey,
      value: jsonEncode({
        'nodeUrl': node.toString(),
        'nodeIdentity': document.nodeIdentity,
        'versionFingerprint': document.versionFingerprint,
        'protocolVersion': document.protocolVersion,
        'capabilities': document.capabilities.toList(),
        'limits': {
          'maxChunkSize': document.limits.maxChunkSize,
          'maxParallelChunks': document.limits.maxParallelChunks,
          'maxRecipients': document.limits.maxRecipients,
        },
      }),
    );
    return document;
  }

  Future<void> _restoreCapabilityCache(Uri node) async {
    final encoded = await _storage.read(key: _capabilitiesKey);
    if (encoded == null) return;
    try {
      final cached = jsonDecode(encoded) as Map<String, dynamic>;
      if (cached['nodeUrl'] != node.toString()) return;
      _capabilityDocument = NodeCapabilityDocument.fromJson(
        cached,
        fallbackNodeIdentity: node.origin,
      );
      _capabilityDocumentVerified = false;
    } catch (_) {
      await _storage.delete(key: _capabilitiesKey);
    }
  }

  Future<dynamic> getJson(String path) => _request(path, method: 'GET');

  Future<dynamic> sendJson(String path, String method, [Object? body]) async {
    if (path == '/api/devices' &&
        method == 'POST' &&
        body is Map<String, dynamic> &&
        capabilityDocument?.supports('scoped_device_enrollment') == true) {
      final publicKey = base64Decode(body['publicKey'] as String);
      final deviceID = await bootstrapDevice(
        deviceType: body['type'] as String,
        deviceName: body['displayName'] as String,
        publicKey: publicKey,
      );
      final rawDevices = await getJson('/api/devices') as List<dynamic>;
      return rawDevices.cast<Map<String, dynamic>>().firstWhere(
        (device) => device['id'] == deviceID,
      );
    }

    final timelineTransferID = _timelineTransferID(path);
    if (method == 'POST' &&
        timelineTransferID != null &&
        body is Map<String, dynamic> &&
        body['code'] == 'ROUTE_FALLBACK_SELECTED' &&
        capabilityDocument?.supports('adaptive_route_racing') == true) {
      _fallbackSignals.add(timelineTransferID);
    }

    final deletedTransferID = _deletedTransferID(path);
    if (method == 'DELETE' &&
        deletedTransferID != null &&
        _fallbackSignals.remove(deletedTransferID) &&
        _transferCache.containsKey(deletedTransferID) &&
        capabilityDocument?.supports('adaptive_route_racing') == true) {
      _pendingFallbackTransferID = deletedTransferID;
      return null;
    }

    if (path == '/api/transfers' &&
        method == 'POST' &&
        body is Map<String, dynamic>) {
      final pendingID = _pendingFallbackTransferID;
      final lanIDs = body['lanAvailableDeviceIds'];
      if (pendingID != null &&
          lanIDs is List<dynamic> &&
          lanIDs.isEmpty &&
          capabilityDocument?.supports('adaptive_route_racing') == true) {
        _pendingFallbackTransferID = null;
        return _resumeTransferOnNode(pendingID);
      }
      final result = await _request(path, method: method, body: body);
      _cacheTransfer(result);
      return result;
    }

    return _request(path, method: method, body: body);
  }

  String? _timelineTransferID(String path) {
    final match = RegExp(r'^/api/transfers/([^/]+)/timeline$').firstMatch(path);
    return match?.group(1);
  }

  String? _deletedTransferID(String path) {
    final match = RegExp(r'^/api/transfers/([^/]+)$').firstMatch(path);
    return match?.group(1);
  }

  void _cacheTransfer(Object? value) {
    if (value is! Map<String, dynamic>) return;
    final id = value['id'];
    if (id is String && id.isNotEmpty) {
      _transferCache[id] = value;
    }
  }

  Future<Map<String, dynamic>> _resumeTransferOnNode(String transferID) async {
    final cached = _transferCache[transferID];
    if (cached == null) {
      throw const ApiException('TRANSFER_FALLBACK_STATE_MISSING', 409);
    }
    final targets = cached['targets'];
    if (targets is! List<dynamic>) {
      throw const ApiException('TRANSFER_FALLBACK_STATE_MISSING', 409);
    }
    final switched = <String>{};
    for (final value in targets) {
      if (value is! Map<String, dynamic>) continue;
      final deviceID = value['deviceId'];
      if (deviceID is! String || !switched.add(deviceID)) continue;
      await _request(
        '/api/v3/transfers/$transferID/route',
        method: 'POST',
        body: {
          'deviceId': deviceID,
          'route': 'NODE',
          'reason': 'DIRECT_PATH_FAILED',
          'verifiedChunks': const <String, String>{},
        },
      );
    }
    final refreshed =
        await _request('/api/transfers/$transferID', method: 'GET')
            as Map<String, dynamic>;
    _transferCache[transferID] = refreshed;
    return refreshed;
  }

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
    final document = capabilityDocument;
    final negotiatedProtocol = compatibleProtocol(document);
    final query = <String, String>{
      'protocolVersion': negotiatedProtocol,
      'clientVersion': 'nexdrop-v$negotiatedProtocol',
    };
    if (document?.supports('capability_negotiation') == true) {
      query['capabilities'] = supportedCapabilities.join(',');
    }
    final uri = _node!.replace(
      scheme: _node!.scheme == 'https' ? 'wss' : 'ws',
      path: '/ws',
      queryParameters: query,
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
          headers: {
            'Content-Type': 'application/json',
            'Accept': _accept,
            'X-NexDrop-Capabilities': supportedCapabilities.join(','),
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
    'X-NexDrop-Capabilities': supportedCapabilities.join(','),
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
      if (_nodeSecret?.isNotEmpty == true)
        _storage.write(key: _nodeSecretKey, value: _nodeSecret)
      else
        _storage.delete(key: _nodeSecretKey),
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
      final details = error is Map<String, dynamic>
          ? error['details'] as Map<String, dynamic>? ??
                const <String, dynamic>{}
          : const <String, dynamic>{};
      return ApiException(
        error is String
            ? error
            : (error as Map<String, dynamic>?)?['code'] as String? ??
                  'REQUEST_FAILED',
        response.statusCode,
        retryAfterSeconds: int.tryParse(response.headers['retry-after'] ?? ''),
        details: details,
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
