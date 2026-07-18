import 'dart:async';
import 'dart:convert';
import 'dart:io';
import 'dart:math';

import 'package:bonsoir/bonsoir.dart';
import 'package:crypto/crypto.dart';
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import 'lan_identity.dart';
import 'models.dart';

const _serviceType = '_nexdrop._tcp';
const _protocolVersion = '1.1';
const _serviceVersion = '1.0.4';
const _fallbackPort = 53317;
const _discoveryMagic = 'NEXDROP_DISCOVERY_V1';
const _maxChunkSize = 9 * 1024 * 1024;
final _tokenPattern = RegExp(r'^[A-Za-z0-9_-]{6,64}$');

class LanEndpoint {
  const LanEndpoint({
    required this.deviceId,
    required this.shortDeviceId,
    required this.address,
    required this.port,
    required this.protocol,
    required this.challenge,
    required this.lastSeen,
  });

  final String deviceId;
  final String shortDeviceId;
  final String address;
  final int port;
  final String protocol;
  final String challenge;
  final DateTime lastSeen;
}

class LanIncomingTransfer {
  const LanIncomingTransfer({
    required this.id,
    required this.senderDeviceId,
    required this.contentType,
    required this.content,
    required this.wrappedContentKey,
    required this.files,
    required this.receivedAt,
  });

  factory LanIncomingTransfer.fromJson(Map<String, dynamic> value) =>
      LanIncomingTransfer(
        id: value['transferId'] as String,
        senderDeviceId: value['senderDeviceId'] as String,
        contentType: value['contentType'] as String,
        content: value['content'] as String,
        wrappedContentKey: value['wrappedContentKey'] as String,
        files: (value['files'] as List<dynamic>? ?? const [])
            .cast<Map<String, dynamic>>(),
        receivedAt: DateTime.parse(value['receivedAt'] as String),
      );

  final String id;
  final String senderDeviceId;
  final String contentType;
  final String content;
  final String wrappedContentKey;
  final List<Map<String, dynamic>> files;
  final DateTime receivedAt;
}

class LanService {
  LanService({LanIdentityStore? identities})
    : _identities = identities ?? LanIdentityStore();

  final LanIdentityStore _identities;
  final _online = <String, LanEndpoint>{};
  final _responseNonces = <String>{};
  final _random = Random.secure();
  final _changes = StreamController<Set<String>>.broadcast();
  final _incoming = StreamController<LanIncomingTransfer>.broadcast();
  BonsoirBroadcast? _broadcast;
  BonsoirDiscovery? _discovery;
  StreamSubscription<BonsoirDiscoveryEvent>? _discoveryEvents;
  StreamSubscription<RawSocketEvent>? _udpEvents;
  RawDatagramSocket? _udp;
  HttpServer? _server;
  Timer? _discoverTimer;
  Timer? _expiryTimer;
  LanIdentity? _identity;
  Device? _current;
  Map<String, Device> _trustedByShortId = const {};
  String _challenge = '';
  String? _storagePath;

  Stream<Set<String>> get changes => _changes.stream;
  Stream<LanIncomingTransfer> get incomingTransfers => _incoming.stream;
  Set<String> get onlineDeviceIds => Set.unmodifiable(_online.keys);

  LanEndpoint? endpointFor(String deviceId) {
    final value = _online[deviceId];
    if (value == null ||
        DateTime.now().difference(value.lastSeen) >
            const Duration(seconds: 20)) {
      return null;
    }
    return value;
  }

  Future<void> start({
    required Device current,
    required List<Device> trustedDevices,
  }) async {
    await stop();
    if (!current.lanCapable) return;
    _current = current;
    _identity = await _identities.ensure(current.id);
    _trustedByShortId = {
      for (final device in trustedDevices.where(
        (device) =>
            device.trusted && device.lanCapable && device.id != current.id,
      ))
        device.lanShortId!: device,
    };
    final support = await getApplicationSupportDirectory();
    _storagePath = path.join(support.path, 'lan-incoming');
    await Directory(_storagePath!).create(recursive: true);
    await _loadIncomingMessages();
    _challenge = _randomToken();
    await _startServer();
    await _startMdns();
    await _startUdp();
    _discoverTimer = Timer.periodic(
      const Duration(seconds: 5),
      (_) => unawaited(_discoverUdp()),
    );
    _expiryTimer = Timer.periodic(const Duration(seconds: 5), (_) => _expire());
    await _discoverUdp();
  }

  Future<void> updateTrustedDevices(List<Device> devices) async {
    final current = _current;
    if (current == null) return;
    final fingerprints = _trustedByShortId.values
        .map((device) => device.lanCertificateFingerprint)
        .toSet();
    final next = devices
        .where(
          (device) =>
              device.trusted && device.lanCapable && device.id != current.id,
        )
        .toList();
    if (next
            .map((device) => device.lanCertificateFingerprint)
            .toSet()
            .difference(fingerprints)
            .isNotEmpty ||
        fingerprints
            .difference(
              next.map((device) => device.lanCertificateFingerprint).toSet(),
            )
            .isNotEmpty) {
      await start(current: current, trustedDevices: devices);
    }
  }

  Future<void> stop() async {
    _discoverTimer?.cancel();
    _expiryTimer?.cancel();
    await _discoveryEvents?.cancel();
    await _udpEvents?.cancel();
    await _discovery?.stop();
    await _broadcast?.stop();
    _udp?.close();
    await _server?.close(force: true);
    _discovery = null;
    _broadcast = null;
    _udp = null;
    _server = null;
    _online.clear();
    _emit();
  }

  Future<void> _startServer() async {
    final identity = _identity!;
    final context = SecurityContext(withTrustedRoots: false)
      ..useCertificateChainBytes(utf8.encode(identity.certificate))
      ..usePrivateKeyBytes(utf8.encode(identity.privateKey));
    for (final device in _trustedByShortId.values) {
      context.setTrustedCertificatesBytes(utf8.encode(device.lanCertificate!));
    }
    _server = await HttpServer.bindSecure(
      InternetAddress.anyIPv4,
      0,
      context,
      requestClientCertificate: true,
      shared: true,
    );
    _server!.listen(_handleRequest);
  }

  Future<void> _startMdns() async {
    final service = BonsoirService(
      name: 'nexdrop-${_identity!.shortDeviceId}',
      type: _serviceType,
      port: _server!.port,
      attributes: {
        'id': _identity!.shortDeviceId,
        'sv': _serviceVersion,
        'pv': _protocolVersion,
        'port': '${_server!.port}',
        'challenge': _challenge,
      },
    );
    _broadcast = BonsoirBroadcast(service: service, printLogs: false);
    await _broadcast!.initialize();
    await _broadcast!.start();
    _discovery = BonsoirDiscovery(type: _serviceType, printLogs: false);
    await _discovery!.initialize();
    _discoveryEvents = _discovery!.eventStream?.listen((event) {
      if (event is BonsoirDiscoveryServiceFoundEvent) {
        unawaited(event.service.resolve(_discovery!.serviceResolver));
      } else if (event is BonsoirDiscoveryServiceResolvedEvent ||
          event is BonsoirDiscoveryServiceUpdatedEvent) {
        final service = event.service!;
        for (final address in service.hostAddresses) {
          _acceptAdvertisement(service.attributes, address, service.port);
        }
      } else if (event is BonsoirDiscoveryServiceLostEvent) {
        final shortId = event.service.attributes['id'];
        final device = _trustedByShortId[shortId];
        if (device != null) {
          _online.remove(device.id);
          _emit();
        }
      }
    });
    await _discovery!.start();
  }

  Future<void> _startUdp() async {
    _udp = await RawDatagramSocket.bind(
      InternetAddress.anyIPv4,
      _fallbackPort,
      reuseAddress: true,
    );
    _udp!.broadcastEnabled = true;
    _udpEvents = _udp!.listen((event) {
      if (event != RawSocketEvent.read) return;
      Datagram? datagram;
      while ((datagram = _udp!.receive()) != null) {
        _handleDatagram(datagram!);
      }
    });
  }

  Future<void> _discoverUdp() async {
    final socket = _udp;
    if (socket == null) return;
    final nonce = _randomToken();
    _responseNonces.add(nonce);
    if (_responseNonces.length > 8) {
      _responseNonces.remove(_responseNonces.first);
    }
    final packet = jsonEncode({
      'magic': _discoveryMagic,
      'type': 'query',
      'nonce': nonce,
    });
    socket.send(
      utf8.encode(packet),
      InternetAddress('255.255.255.255'),
      _fallbackPort,
    );
  }

  void _handleDatagram(Datagram datagram) {
    Map<String, dynamic> packet;
    try {
      packet = jsonDecode(utf8.decode(datagram.data)) as Map<String, dynamic>;
    } catch (_) {
      return;
    }
    if (packet['magic'] != _discoveryMagic || packet['nonce'] is! String) {
      return;
    }
    if (packet['type'] == 'query') {
      final advertisement = {
        'deviceId': _identity!.shortDeviceId,
        'serviceVersion': _serviceVersion,
        'protocolVersion': _protocolVersion,
        'port': _server!.port,
        'challenge': _challenge,
      };
      final response = jsonEncode({
        'magic': _discoveryMagic,
        'type': 'response',
        'nonce': packet['nonce'],
        'advertisement': advertisement,
      });
      _udp!.send(utf8.encode(response), datagram.address, datagram.port);
    } else if (packet['type'] == 'response' &&
        _responseNonces.contains(packet['nonce'])) {
      final value = packet['advertisement'];
      if (value is Map<String, dynamic>) {
        _acceptAdvertisement(
          {
            'id': '${value['deviceId'] ?? ''}',
            'sv': '${value['serviceVersion'] ?? ''}',
            'pv': '${value['protocolVersion'] ?? ''}',
            'port': '${value['port'] ?? ''}',
            'challenge': '${value['challenge'] ?? ''}',
          },
          datagram.address.address,
          value['port'] as int? ?? 0,
        );
      }
    }
  }

  void _acceptAdvertisement(
    Map<String, String> attributes,
    String address,
    int servicePort,
  ) {
    final shortId = attributes['id'];
    final device = _trustedByShortId[shortId];
    final port = int.tryParse(attributes['port'] ?? '');
    final protocol = attributes['pv'];
    final challenge = attributes['challenge'];
    if (device == null ||
        InternetAddress.tryParse(address) == null ||
        port == null ||
        port != servicePort ||
        port < 1 ||
        port > 65535 ||
        !const {'1.0', '1.1'}.contains(protocol) ||
        attributes['sv'] == null ||
        !_validChallenge(challenge)) {
      return;
    }
    _online[device.id] = LanEndpoint(
      deviceId: device.id,
      shortDeviceId: shortId!,
      address: address,
      port: port,
      protocol: protocol!,
      challenge: challenge!,
      lastSeen: DateTime.now(),
    );
    _emit();
  }

  bool _validChallenge(String? value) {
    if (value == null) return false;
    try {
      return base64Url.decode(base64Url.normalize(value)).length == 16;
    } catch (_) {
      return false;
    }
  }

  void _expire() {
    final cutoff = DateTime.now().subtract(const Duration(seconds: 20));
    final before = _online.length;
    _online.removeWhere((_, endpoint) => endpoint.lastSeen.isBefore(cutoff));
    if (_online.length != before) _emit();
  }

  void _emit() {
    if (!_changes.isClosed) _changes.add(onlineDeviceIds);
  }

  String _randomToken() => base64UrlEncode(
    List<int>.generate(16, (_) => _random.nextInt(256)),
  ).replaceAll('=', '');

  Future<void> _handleRequest(HttpRequest request) async {
    try {
      final peer = _trustedDevice(request.certificate);
      if (peer == null ||
          !const {
            '1.0',
            '1.1',
          }.contains(request.headers.value('X-NexDrop-Protocol')) ||
          request.headers.value('X-NexDrop-Challenge') != _challenge) {
        return _json(request.response, HttpStatus.unauthorized, {
          'error': 'LAN_HANDSHAKE_FAILED',
        });
      }
      final segments = request.uri.pathSegments;
      if (request.method == 'POST' &&
          segments.length == 4 &&
          segments[0] == 'v1' &&
          segments[1] == 'transfers' &&
          _tokenPattern.hasMatch(segments[2]) &&
          segments[3] == 'message') {
        return _receiveMessage(request, segments[2], peer.id);
      }
      if (segments.length < 5 ||
          segments[0] != 'v1' ||
          segments[1] != 'transfers' ||
          segments[3] != 'files' ||
          !_tokenPattern.hasMatch(segments[2]) ||
          !_tokenPattern.hasMatch(segments[4])) {
        return _json(request.response, HttpStatus.badRequest, {
          'error': 'INVALID_TRANSFER',
        });
      }
      final transferId = segments[2];
      final fileId = segments[4];
      if (request.method == 'GET' && segments.length == 5) {
        final completed = await _completedChunks(transferId, fileId);
        return _json(request.response, HttpStatus.ok, {
          'completedChunks': completed,
          'protocolVersion': _protocolVersion,
        });
      }
      if (request.method == 'PUT' &&
          segments.length == 7 &&
          segments[5] == 'chunks') {
        return _receiveChunk(
          request,
          transferId,
          fileId,
          int.tryParse(segments[6]),
        );
      }
      if (request.method == 'POST' &&
          segments.length == 6 &&
          segments[5] == 'complete') {
        return _completeFile(request, transferId, fileId);
      }
      return _json(request.response, HttpStatus.notFound, {
        'error': 'NOT_FOUND',
      });
    } catch (_) {
      try {
        await _json(request.response, HttpStatus.internalServerError, {
          'error': 'LAN_STORAGE_FAILED',
        });
      } catch (_) {
        await request.response.close();
      }
    }
  }

  Device? _trustedDevice(X509Certificate? certificate) {
    if (certificate == null) return null;
    final fingerprint = sha256.convert(certificate.der).toString();
    for (final device in _trustedByShortId.values) {
      if (device.lanCertificateFingerprint?.toLowerCase() == fingerprint) {
        return device;
      }
    }
    return null;
  }

  Future<List<int>> _completedChunks(String transferId, String fileId) async {
    final directory = Directory(path.join(_storagePath!, transferId, fileId));
    if (!await directory.exists()) return const [];
    final values = <int>[];
    await for (final entry in directory.list()) {
      final name = path.basename(entry.path);
      if (entry is File && name.endsWith('.chunk')) {
        final index = int.tryParse(name.substring(0, name.length - 6));
        if (index != null) values.add(index);
      }
    }
    values.sort();
    return values;
  }

  Future<void> _receiveMessage(
    HttpRequest request,
    String transferId,
    String senderDeviceId,
  ) async {
    final bytes = <int>[];
    await for (final part in request) {
      bytes.addAll(part);
      if (bytes.length > 1024 * 1024) {
        return _json(request.response, HttpStatus.requestEntityTooLarge, {
          'error': 'MESSAGE_TOO_LARGE',
        });
      }
    }
    Map<String, dynamic> body;
    try {
      body = jsonDecode(utf8.decode(bytes)) as Map<String, dynamic>;
    } catch (_) {
      return _json(request.response, HttpStatus.badRequest, {
        'error': 'INVALID_MESSAGE',
      });
    }
    if (!const {'TEXT', 'URL', 'FILE', 'NOTIFICATION'}.contains(body['contentType']) ||
        body['content'] is! String ||
        body['wrappedContentKey'] is! String) {
      return _json(request.response, HttpStatus.badRequest, {
        'error': 'INVALID_MESSAGE',
      });
    }
    final directory = Directory(path.join(_storagePath!, 'messages'));
    await directory.create(recursive: true);
    body['transferId'] = transferId;
    body['senderDeviceId'] = senderDeviceId;
    body['receivedAt'] = DateTime.now().toUtc().toIso8601String();
    final normalized = utf8.encode(jsonEncode(body));
    await File(
      path.join(directory.path, '$transferId.json'),
    ).writeAsBytes(normalized, flush: true);
    _incoming.add(LanIncomingTransfer.fromJson(body));
    await _json(request.response, HttpStatus.ok, {'status': 'DELIVERED'});
  }

  Future<void> _receiveChunk(
    HttpRequest request,
    String transferId,
    String fileId,
    int? index,
  ) async {
    final expected = request.headers.value('X-Chunk-SHA256')?.toLowerCase();
    if (index == null ||
        index < 0 ||
        expected == null ||
        !RegExp(r'^[a-f0-9]{64}$').hasMatch(expected)) {
      return _json(request.response, HttpStatus.badRequest, {
        'error': 'INVALID_CHUNK',
      });
    }
    final bytes = <int>[];
    await for (final part in request) {
      bytes.addAll(part);
      if (bytes.length > _maxChunkSize) {
        return _json(request.response, HttpStatus.requestEntityTooLarge, {
          'error': 'CHUNK_TOO_LARGE',
        });
      }
    }
    if (sha256.convert(bytes).toString() != expected) {
      return _json(request.response, HttpStatus.unprocessableEntity, {
        'error': 'HASH_MISMATCH',
      });
    }
    final directory = Directory(path.join(_storagePath!, transferId, fileId));
    await directory.create(recursive: true);
    await File(
      path.join(directory.path, '$index.chunk'),
    ).writeAsBytes(bytes, flush: true);
    await _json(request.response, HttpStatus.ok, {
      'index': index,
      'sha256': expected,
    });
  }

  Future<void> _completeFile(
    HttpRequest request,
    String transferId,
    String fileId,
  ) async {
    final body =
        jsonDecode(await utf8.decoder.bind(request).join())
            as Map<String, dynamic>;
    final count = body['chunkCount'] as int?;
    final expected = body['sha256'] as String?;
    if (count == null ||
        count < 0 ||
        expected == null ||
        !RegExp(r'^[a-fA-F0-9]{64}$').hasMatch(expected)) {
      return _json(request.response, HttpStatus.badRequest, {
        'error': 'INVALID_FILE',
      });
    }
    final directory = Directory(path.join(_storagePath!, transferId, fileId));
    final assembled = File(path.join(_storagePath!, '$transferId-$fileId.nxd'));
    final output = await assembled.open(mode: FileMode.write);
    try {
      for (var index = 0; index < count; index++) {
        final chunk = File(path.join(directory.path, '$index.chunk'));
        if (!await chunk.exists()) {
          await output.close();
          if (await assembled.exists()) await assembled.delete();
          return _json(request.response, HttpStatus.conflict, {
            'error': 'FILE_INCOMPLETE',
          });
        }
        await output.writeFrom(await chunk.readAsBytes());
      }
    } finally {
      if (await assembled.exists()) await output.close();
    }
    final actual = await sha256.bind(assembled.openRead()).first;
    if (actual.toString() != expected.toLowerCase()) {
      await assembled.delete();
      return _json(request.response, HttpStatus.conflict, {
        'error': 'FILE_INCOMPLETE',
      });
    }
    await directory.delete(recursive: true);
    await _json(request.response, HttpStatus.ok, {'status': 'DELIVERED'});
  }

  Future<void> _json(HttpResponse response, int status, Object body) async {
    response.statusCode = status;
    response.headers.contentType = ContentType.json;
    response.write(jsonEncode(body));
    await response.close();
  }

  Future<List<int>> completedChunks(
    LanEndpoint target,
    String transferId,
    String fileId,
  ) async {
    final result = await _request(
      target,
      'GET',
      '/v1/transfers/$transferId/files/$fileId',
    );
    return (result['completedChunks'] as List<dynamic>).cast<int>();
  }

  Future<void> sendMessage(
    LanEndpoint target,
    String transferId, {
    required String contentType,
    required String content,
    required String wrappedContentKey,
    List<Map<String, dynamic>> files = const [],
  }) async {
    await _request(
      target,
      'POST',
      '/v1/transfers/$transferId/message',
      body: {
        'contentType': contentType,
        'content': content,
        'wrappedContentKey': wrappedContentKey,
        'files': files,
      },
    );
  }

  Future<void> putChunk(
    LanEndpoint target,
    String transferId,
    String fileId,
    int index,
    List<int> bytes,
  ) async {
    await _request(
      target,
      'PUT',
      '/v1/transfers/$transferId/files/$fileId/chunks/$index',
      bytes: bytes,
      headers: {'X-Chunk-SHA256': sha256.convert(bytes).toString()},
    );
  }

  Future<void> complete(
    LanEndpoint target,
    String transferId,
    String fileId,
    int chunkCount,
    String digest,
  ) async {
    await _request(
      target,
      'POST',
      '/v1/transfers/$transferId/files/$fileId/complete',
      body: {'chunkCount': chunkCount, 'sha256': digest},
    );
  }

  Future<Map<String, dynamic>> _request(
    LanEndpoint target,
    String method,
    String requestPath, {
    Map<String, dynamic>? body,
    List<int>? bytes,
    Map<String, String> headers = const {},
  }) async {
    if (!_tokenPattern.hasMatch(target.shortDeviceId)) {
      throw const FormatException('無效的區網目標');
    }
    final peer = _trustedByShortId[target.shortDeviceId];
    if (peer == null) throw const HttpException('區網目標未受信任');
    final context = SecurityContext(withTrustedRoots: false)
      ..useCertificateChainBytes(utf8.encode(_identity!.certificate))
      ..usePrivateKeyBytes(utf8.encode(_identity!.privateKey))
      ..setTrustedCertificatesBytes(utf8.encode(peer.lanCertificate!));
    final client = HttpClient(context: context)
      ..connectionTimeout = const Duration(seconds: 5)
      ..badCertificateCallback = (certificate, _, _) =>
          sha256.convert(certificate.der).toString() ==
          peer.lanCertificateFingerprint?.toLowerCase();
    try {
      final uri = Uri.parse(
        'https://${target.address}:${target.port}$requestPath',
      );
      final request = await client.openUrl(method, uri);
      request.headers.set('X-NexDrop-Protocol', target.protocol);
      request.headers.set('X-NexDrop-Challenge', target.challenge);
      headers.forEach(request.headers.set);
      if (body != null) {
        request.headers.contentType = ContentType.json;
        request.write(jsonEncode(body));
      } else if (bytes != null) {
        request.headers.contentType = ContentType.binary;
        request.add(bytes);
      }
      final response = await request.close().timeout(
        const Duration(seconds: 30),
      );
      final text = await utf8.decoder.bind(response).join();
      final result = text.isEmpty
          ? <String, dynamic>{}
          : jsonDecode(text) as Map<String, dynamic>;
      if (response.statusCode < 200 || response.statusCode >= 300) {
        throw HttpException('區網傳輸失敗：${result['error'] ?? response.statusCode}');
      }
      return result;
    } finally {
      client.close(force: true);
    }
  }

  Future<void> dispose() async {
    await stop();
    await _changes.close();
    await _incoming.close();
  }

  Future<void> _loadIncomingMessages() async {
    final directory = Directory(path.join(_storagePath!, 'messages'));
    if (!await directory.exists()) return;
    await for (final entry in directory.list()) {
      if (entry is! File || !entry.path.endsWith('.json')) continue;
      try {
        final value =
            jsonDecode(await entry.readAsString()) as Map<String, dynamic>;
        _incoming.add(LanIncomingTransfer.fromJson(value));
      } catch (_) {
        // Invalid local records remain available for diagnosis.
      }
    }
  }
}
