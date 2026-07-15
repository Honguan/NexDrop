import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:crypto/crypto.dart' as hashes;
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';

import 'api_client.dart';
import 'crypto_service.dart';
import 'lan_identity.dart';
import 'lan_service.dart';
import 'local_database.dart';
import 'models.dart';

class DeviceSession {
  const DeviceSession({required this.account, required this.device});

  final UserAccount account;
  final Device device;
}

class TransferService {
  TransferService({
    required this.api,
    required this.crypto,
    required this.database,
    required this.lan,
    FlutterSecureStorage? storage,
    LanIdentityStore? lanIdentityStore,
  }) : _storage = storage ?? const FlutterSecureStorage(),
       _lanIdentityStore = lanIdentityStore ?? LanIdentityStore();

  static const _deviceKeyPrefix = 'nexdrop.device_id.';
  final ApiClient api;
  final CryptoService crypto;
  final LocalDatabase database;
  final LanService lan;
  final FlutterSecureStorage _storage;
  final LanIdentityStore _lanIdentityStore;

  Future<DeviceSession> synchronizeDevice(UserAccount account) async {
    final allDevices = await api.devices();
    final storedID = await _storage.read(key: '$_deviceKeyPrefix${account.id}');
    Device? current = allDevices
        .where(
          (device) => device.id == storedID && device.trustStatus != 'REVOKED',
        )
        .firstOrNull;
    if (current == null) {
      final keys = await crypto.ensureDeviceKey(account.id);
      final response =
          await api.sendJson('/api/devices', 'POST', {
                'displayName':
                    '${Platform.localHostname} · ${Platform.isWindows ? 'Windows' : 'Android'}',
                'type': Platform.isWindows ? 'WINDOWS' : 'ANDROID',
                'publicKey': base64Encode(keys.publicKey),
                'keyAlgorithm': 'X25519',
              })
              as Map<String, dynamic>;
      current = Device.fromJson(response);
      await _storage.write(
        key: '$_deviceKeyPrefix${account.id}',
        value: current.id,
      );
    } else {
      final challenge =
          await api.sendJson(
                '/api/devices/${current.id}/session-challenge',
                'POST',
              )
              as Map<String, dynamic>;
      final proof = await crypto.proveSession(
        account.id,
        challenge['ephemeralPublicKey'] as String,
        challenge['nonce'] as String,
        challenge['sessionId'] as String,
      );
      await api.sendJson('/api/devices/${current.id}/attach-session', 'POST', {
        'challengeId': challenge['id'],
        'proof': proof,
      });
    }
    final identity = await _lanIdentityStore.ensure(current.id);
    if (current.lanCertificateFingerprint != identity.fingerprint) {
      await api.sendJson('/api/devices/${current.id}/lan-identity', 'PUT', {
        'certificate': identity.certificate,
      });
      current = (await api.devices()).firstWhere(
        (device) => device.id == current!.id,
      );
    }
    return DeviceSession(account: account, device: current);
  }

  Future<TransferSummary> sendText({
    required String content,
    required List<Device> devices,
    String? groupId,
    Set<String> lanAvailable = const {},
  }) async {
    final recipients = devices
        .where((device) => device.trusted && device.publicKey != null)
        .map((device) => (id: device.id, publicKey: device.publicKey!))
        .toList();
    final encrypted = await crypto.encryptText(content.trim(), recipients);
    final request = <String, dynamic>{
      'targetType': groupId != null
          ? 'GROUP_ALL_DEVICES'
          : recipients.length == 1
          ? 'SINGLE_DEVICE'
          : 'MULTIPLE_DEVICES',
      'targetDeviceIds': groupId == null
          ? recipients.map((recipient) => recipient.id).toList()
          : <String>[],
      'lanAvailableDeviceIds': recipients
          .where((recipient) => lanAvailable.contains(recipient.id))
          .map((recipient) => recipient.id)
          .toList(),
      'contentType': content.trim().startsWith('http') ? 'URL' : 'TEXT',
      'routeMode': 'AUTOMATIC',
      'content': encrypted.content,
      'wrappedContentKeys': encrypted.wrappedContentKeys,
    };
    if (groupId != null) request['groupId'] = groupId;
    final response =
        await api.sendJson('/api/transfers', 'POST', request)
            as Map<String, dynamic>;
    final transfer = TransferSummary.fromJson(response);
    for (final target in transfer.targets.where(
      (target) => target.route == 'LAN',
    )) {
      final endpoint = lan.endpointFor(target.deviceId);
      final wrappedKey = encrypted.wrappedContentKeys[target.deviceId];
      if (endpoint == null || wrappedKey == null) {
        throw const HttpException('區網目標已離線');
      }
      await lan.sendMessage(
        endpoint,
        transfer.id,
        contentType: request['contentType'] as String,
        content: encrypted.content,
        wrappedContentKey: wrappedKey,
      );
      await _reportProgress(
        transfer.id,
        target.deviceId,
        'DELIVERED',
        utf8.encode(encrypted.content).length,
      );
    }
    await _cache(transfer);
    return transfer;
  }

  Future<TransferSummary> sendFiles({
    required List<String> sourcePaths,
    required List<Device> devices,
    String? groupId,
    Set<String> lanAvailable = const {},
  }) async {
    final recipients = devices
        .where((device) => device.trusted && device.publicKey != null)
        .map((device) => (id: device.id, publicKey: device.publicKey!))
        .toList();
    final support = await getApplicationSupportDirectory();
    final encrypted = await crypto.encryptFiles(
      sourcePaths,
      '${support.path}${Platform.pathSeparator}temp',
      recipients,
    );
    try {
      final request = <String, dynamic>{
        'targetType': groupId != null
            ? 'GROUP_ALL_DEVICES'
            : recipients.length == 1
            ? 'SINGLE_DEVICE'
            : 'MULTIPLE_DEVICES',
        'targetDeviceIds': groupId == null
            ? recipients.map((recipient) => recipient.id).toList()
            : <String>[],
        'lanAvailableDeviceIds': recipients
            .where((recipient) => lanAvailable.contains(recipient.id))
            .map((recipient) => recipient.id)
            .toList(),
        'contentType': 'FILE',
        'routeMode': 'AUTOMATIC',
        'allowLargeFileViaNode': false,
        'content': encrypted.content,
        'wrappedContentKeys': encrypted.wrappedContentKeys,
        'files': encrypted.files.map((file) => file.record).toList(),
      };
      if (groupId != null) request['groupId'] = groupId;
      final response =
          await api.sendJson('/api/transfers', 'POST', request)
              as Map<String, dynamic>;
      final transfer = TransferSummary.fromJson(response);
      final lanBytes = <String, int>{};
      for (final target in transfer.fileTargets.where(
        (target) => target.route == 'LAN',
      )) {
        final endpoint = lan.endpointFor(target.deviceId);
        if (endpoint == null) {
          throw const HttpException('區網目標已離線');
        }
        final encryptedFile = encrypted.files[target.fileIndex];
        final remoteFile = transfer.files[target.fileIndex];
        await _reportProgress(
          transfer.id,
          target.deviceId,
          'TRANSFERRING_LAN',
          lanBytes[target.deviceId] ?? 0,
        );
        final sent = await _uploadLanFile(
          endpoint,
          transfer.id,
          remoteFile.id,
          encryptedFile,
        );
        lanBytes[target.deviceId] = (lanBytes[target.deviceId] ?? 0) + sent;
      }
      for (final (fileIndex, encryptedFile) in encrypted.files.indexed) {
        if (!transfer.fileTargets.any(
          (target) => target.fileIndex == fileIndex && target.route == 'NODE',
        )) {
          continue;
        }
        final remoteFile = transfer.files[fileIndex];
        final input = await File(encryptedFile.tempPath).open();
        try {
          for (final (chunkIndex, chunk) in encryptedFile.chunks.indexed) {
            final bytes = await input.read(chunk.size);
            await api.uploadChunk(
              remoteFile.id,
              chunkIndex,
              bytes,
              chunk.sha256Hex,
            );
          }
        } finally {
          await input.close();
        }
        await api.sendJson('/api/files/${remoteFile.id}/complete', 'POST');
      }
      for (final target in transfer.targets.where(
        (target) => target.route == 'NODE',
      )) {
        await api.sendJson(
          '/api/transfers/${transfer.id}/targets/${target.deviceId}',
          'PUT',
          {
            'status': 'AVAILABLE_ON_NODE',
            'route': 'NODE',
            'bytesTransferred': encrypted.files.fold<int>(
              0,
              (total, file) => total + file.size,
            ),
          },
        );
      }
      for (final entry in lanBytes.entries) {
        final allLan = transfer.fileTargets
            .where((target) => target.deviceId == entry.key)
            .every((target) => target.route == 'LAN');
        await _reportProgress(
          transfer.id,
          entry.key,
          allLan ? 'DELIVERED' : 'AVAILABLE_ON_NODE',
          entry.value,
        );
      }
      await _cache(transfer);
      return transfer;
    } finally {
      for (final file in encrypted.files) {
        final temporary = File(file.tempPath);
        if (await temporary.exists()) await temporary.delete();
      }
    }
  }

  Future<List<Device>> groupDevices(String groupId) async {
    final details =
        await api.getJson('/api/groups/$groupId') as Map<String, dynamic>;
    return (details['devices'] as List<dynamic>)
        .map((value) => Device.fromJson(value as Map<String, dynamic>))
        .toList();
  }

  Future<String> readText(
    TransferSummary transfer,
    UserAccount account,
    Device currentDevice,
  ) async {
    final content = transfer.encryptedContent;
    final wrapped = transfer.wrappedContentKeys[currentDevice.id];
    if (content == null || wrapped == null) {
      throw const FormatException('此設備無法解密內容');
    }
    final plaintext = await crypto.decryptText(account.id, content, wrapped);
    await api.sendJson('/api/transfers/${transfer.id}/read', 'POST');
    return plaintext;
  }

  Future<List<String>> receiveFiles(
    TransferSummary transfer,
    UserAccount account,
    Device currentDevice,
  ) async {
    final content = transfer.encryptedContent;
    final wrapped = transfer.wrappedContentKeys[currentDevice.id];
    if (content == null || wrapped == null) {
      throw const FormatException('此設備無法解密檔案');
    }
    final metadataText = await crypto.decryptText(account.id, content, wrapped);
    final metadata = (jsonDecode(metadataText) as List<dynamic>)
        .cast<Map<String, dynamic>>();
    if (metadata.length != transfer.files.length) {
      throw const FormatException('檔案中繼資料不一致');
    }
    final decryptor = await crypto.fileChunkDecryptor(account.id, wrapped);
    final downloads = await getDownloadsDirectory();
    final support = await getApplicationSupportDirectory();
    final destination = Directory(
      path.join((downloads ?? support).path, 'NexDrop'),
    );
    await destination.create(recursive: true);
    final saved = <String>[];
    var usedNode = false;
    for (final (fileIndex, remoteFile) in transfer.files.indexed) {
      final originalName = path.basename(
        metadata[fileIndex]['name'] as String? ?? 'NexDrop-file-$fileIndex',
      );
      final outputPath = await _availablePath(destination.path, originalName);
      final output = await File(outputPath).open(mode: FileMode.write);
      final digestSink = _DigestSink();
      final digestInput = hashes.sha256.startChunkedConversion(digestSink);
      final localEncrypted = File(
        path.join(
          support.path,
          'lan-incoming',
          '${transfer.id}-${remoteFile.id}.nxd',
        ),
      );
      final isLocal = await localEncrypted.exists();
      usedNode = usedNode || !isLocal;
      if (!isLocal) {
        await _reportProgress(
          transfer.id,
          currentDevice.id,
          'DOWNLOADING_FROM_NODE',
          0,
        );
      }
      final localInput = isLocal ? await localEncrypted.open() : null;
      var transferred = 0;
      try {
        for (var index = 0; index < remoteFile.chunkCount; index++) {
          final encrypted = localInput == null
              ? await api.downloadChunk(remoteFile.id, index)
              : await localInput.read(remoteFile.chunkSize);
          if (encrypted.isEmpty) {
            throw const FileSystemException('加密檔案分段遺失');
          }
          digestInput.add(encrypted);
          await output.writeFrom(await decryptor.decrypt(encrypted));
          transferred += encrypted.length;
          if (!isLocal) {
            await _reportProgress(
              transfer.id,
              currentDevice.id,
              'DOWNLOADING_FROM_NODE',
              transferred,
            );
          }
        }
      } finally {
        digestInput.close();
        await localInput?.close();
        await output.close();
      }
      if (base64Encode(digestSink.value.bytes) != remoteFile.sha256) {
        await File(outputPath).delete();
        throw const FormatException('HASH_MISMATCH');
      }
      if (await localEncrypted.exists()) await localEncrypted.delete();
      await database.recordDownload(remoteFile.id, outputPath);
      saved.add(outputPath);
    }
    if (usedNode) {
      await _reportProgress(
        transfer.id,
        currentDevice.id,
        'DELIVERED',
        transfer.files.fold<int>(0, (total, file) => total + file.size),
      );
    }
    await api.sendJson('/api/transfers/${transfer.id}/read', 'POST');
    return saved;
  }

  Future<void> cancel(String transferId) async {
    await api.sendJson('/api/transfers/$transferId/cancel', 'POST');
  }

  Future<void> pause(String transferId, String deviceId, int bytes) =>
      _reportProgress(transferId, deviceId, 'PAUSED', bytes);

  Future<String> _availablePath(String directory, String requestedName) async {
    final safe = requestedName.replaceAll(
      RegExp(r'[<>:"/\\|?*\x00-\x1f]'),
      '_',
    );
    var candidate = path.join(directory, safe.isEmpty ? 'NexDrop-file' : safe);
    var suffix = 1;
    while (await File(candidate).exists()) {
      final extension = path.extension(safe);
      final stem = path.basenameWithoutExtension(safe);
      candidate = path.join(directory, '$stem ($suffix)$extension');
      suffix++;
    }
    return candidate;
  }

  Future<int> _uploadLanFile(
    LanEndpoint endpoint,
    String transferId,
    String fileId,
    EncryptedFileUpload file,
  ) async {
    final completed = (await lan.completedChunks(
      endpoint,
      transferId,
      fileId,
    )).toSet();
    final input = await File(file.tempPath).open();
    var sent = 0;
    try {
      for (final (index, chunk) in file.chunks.indexed) {
        final bytes = await input.read(chunk.size);
        if (!completed.contains(index)) {
          await lan.putChunk(endpoint, transferId, fileId, index, bytes);
        }
        sent += bytes.length;
      }
    } finally {
      await input.close();
    }
    await lan.complete(
      endpoint,
      transferId,
      fileId,
      file.chunks.length,
      file.sha256,
    );
    return sent;
  }

  Future<void> _reportProgress(
    String transferId,
    String deviceId,
    String status,
    int bytes,
  ) async {
    await api.sendJson('/api/transfers/$transferId/targets/$deviceId', 'PUT', {
      'status': status,
      'bytesTransferred': bytes,
    });
  }

  Future<void> _cache(TransferSummary transfer) => database.cacheTransfer(
    id: transfer.id,
    contentType: transfer.contentType,
    route: transfer.targets.map((target) => target.route).toSet().length == 1
        ? transfer.targets.first.route
        : 'MIXED',
    status: transfer.status,
    totalBytes: transfer.files.fold<int>(0, (total, file) => total + file.size),
    createdAt: transfer.createdAt,
  );
}

extension _FirstOrNull<T> on Iterable<T> {
  T? get firstOrNull => isEmpty ? null : first;
}

class _DigestSink implements Sink<hashes.Digest> {
  hashes.Digest? _value;

  hashes.Digest get value => _value ?? hashes.sha256.convert(const []);

  @override
  void add(hashes.Digest data) => _value = data;

  @override
  void close() {}
}
