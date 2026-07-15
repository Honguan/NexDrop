import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:crypto/crypto.dart' as hashes;
import 'package:path/path.dart' as path;
import 'package:path_provider/path_provider.dart';
import 'package:uuid/uuid.dart';

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
  static const _waitingRecipePrefix = 'nexdrop.waiting.recipe.';
  final ApiClient api;
  final CryptoService crypto;
  final LocalDatabase database;
  final LanService lan;
  final FlutterSecureStorage _storage;
  final LanIdentityStore _lanIdentityStore;
  bool _retryingWaitingLan = false;
  bool _flushingDrafts = false;
  Device? _currentDevice;

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
    _currentDevice = current;
    return DeviceSession(account: account, device: current);
  }

  Future<TransferSummary> sendText({
    required String content,
    required List<Device> devices,
    String? groupId,
    bool groupAll = true,
    Set<String> lanAvailable = const {},
    bool nodeAvailable = true,
    String routeMode = 'AUTOMATIC',
  }) async {
    final recipients = devices
        .where((device) => device.trusted && device.publicKey != null)
        .map((device) => (id: device.id, publicKey: device.publicKey!))
        .toList();
    final encryptionRecipients = [...recipients];
    final sender = _currentDevice;
    if (sender != null &&
        sender.publicKey != null &&
        !encryptionRecipients.any((recipient) => recipient.id == sender.id)) {
      encryptionRecipients.add((id: sender.id, publicKey: sender.publicKey!));
    }
    final encrypted = await crypto.encryptText(
      content.trim(),
      encryptionRecipients,
    );
    if (!nodeAvailable &&
        routeMode != 'NODE_ONLY' &&
        recipients.every(
          (recipient) => lan.endpointFor(recipient.id) != null,
        )) {
      return _sendTextLanOnly(content, devices, encrypted, groupId: groupId);
    }
    final request = <String, dynamic>{
      'targetType': groupId != null
          ? groupAll
                ? 'GROUP_ALL_DEVICES'
                : 'GROUP_SELECTED_DEVICES'
          : recipients.length == 1
          ? 'SINGLE_DEVICE'
          : 'MULTIPLE_DEVICES',
      'targetDeviceIds': groupId == null || !groupAll
          ? recipients.map((recipient) => recipient.id).toList()
          : <String>[],
      'lanAvailableDeviceIds': recipients
          .where((recipient) => lanAvailable.contains(recipient.id))
          .map((recipient) => recipient.id)
          .toList(),
      'contentType': content.trim().startsWith('http') ? 'URL' : 'TEXT',
      'routeMode': routeMode,
      'content': encrypted.content,
      'wrappedContentKeys': encrypted.wrappedContentKeys,
    };
    if (groupId != null) request['groupId'] = groupId;
    if (!nodeAvailable) {
      request['lanAvailableDeviceIds'] = <String>[];
      return _saveTextDraft(request, devices);
    }
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
    bool groupAll = true,
    Set<String> lanAvailable = const {},
    bool nodeAvailable = true,
    String routeMode = 'AUTOMATIC',
    bool allowLargeFileViaNode = false,
  }) async {
    final recipients = devices
        .where((device) => device.trusted && device.publicKey != null)
        .map((device) => (id: device.id, publicKey: device.publicKey!))
        .toList();
    final encryptionRecipients = [...recipients];
    final sender = _currentDevice;
    if (sender != null &&
        sender.publicKey != null &&
        !encryptionRecipients.any((recipient) => recipient.id == sender.id)) {
      encryptionRecipients.add((id: sender.id, publicKey: sender.publicKey!));
    }
    final support = await getApplicationSupportDirectory();
    final encrypted = await crypto.encryptFiles(
      sourcePaths,
      '${support.path}${Platform.pathSeparator}temp',
      encryptionRecipients,
    );
    try {
      if (!nodeAvailable &&
          routeMode != 'NODE_ONLY' &&
          recipients.every(
            (recipient) => lan.endpointFor(recipient.id) != null,
          )) {
        return await _sendFilesLanOnly(devices, encrypted, groupId: groupId);
      }
      final request = <String, dynamic>{
        'targetType': groupId != null
            ? groupAll
                  ? 'GROUP_ALL_DEVICES'
                  : 'GROUP_SELECTED_DEVICES'
            : recipients.length == 1
            ? 'SINGLE_DEVICE'
            : 'MULTIPLE_DEVICES',
        'targetDeviceIds': groupId == null || !groupAll
            ? recipients.map((recipient) => recipient.id).toList()
            : <String>[],
        'lanAvailableDeviceIds': recipients
            .where((recipient) => lanAvailable.contains(recipient.id))
            .map((recipient) => recipient.id)
            .toList(),
        'contentType': 'FILE',
        'routeMode': routeMode,
        'allowLargeFileViaNode': allowLargeFileViaNode,
        'content': encrypted.content,
        'wrappedContentKeys': encrypted.wrappedContentKeys,
        'files': encrypted.files.map((file) => file.record).toList(),
      };
      if (groupId != null) request['groupId'] = groupId;
      if (!nodeAvailable) {
        request['lanAvailableDeviceIds'] = <String>[];
        return await _saveFileDraft(request, devices, sourcePaths, encrypted);
      }
      final response =
          await api.sendJson('/api/transfers', 'POST', request)
              as Map<String, dynamic>;
      final transfer = TransferSummary.fromJson(response);
      final waitingTargets = transfer.fileTargets
          .where((target) => target.route == 'WAITING_LAN')
          .toList();
      if (waitingTargets.isNotEmpty) {
        await _storage.write(
          key: '$_waitingRecipePrefix${transfer.id}',
          value: jsonEncode({
            'contentKey': base64Encode(encrypted.contentKey),
            'files': {
              for (final (index, file) in encrypted.files.indexed)
                '$index': {'nonces': file.nonces, 'sha256': file.sha256},
            },
          }),
        );
        for (final target in waitingTargets) {
          final source = encrypted.files[target.fileIndex];
          await database.saveWaitingLanTask(
            id: '${transfer.id}:${target.fileIndex}:${target.deviceId}',
            transferId: transfer.id,
            fileId: transfer.files[target.fileIndex].id,
            fileIndex: target.fileIndex,
            targetDeviceId: target.deviceId,
            targetRoute: transfer.targets
                .firstWhere((item) => item.deviceId == target.deviceId)
                .route,
            sourcePath: sourcePaths[target.fileIndex],
            sourceSize: source.originalSize,
            sourceModifiedAt: source.originalModifiedAt,
            sourceSha256: source.originalSha256,
          );
        }
      }
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
      final nodeDeviceIds = transfer.fileTargets
          .where((target) => target.route == 'NODE')
          .map((target) => target.deviceId)
          .toSet();
      for (final target in transfer.targets.where(
        (target) => nodeDeviceIds.contains(target.deviceId),
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

  Future<TransferSummary> _sendTextLanOnly(
    String plaintext,
    List<Device> devices,
    EncryptedEnvelope encrypted, {
    String? groupId,
  }) async {
    final startedAt = DateTime.now().toUtc();
    final transferId = const Uuid().v4();
    final contentType = plaintext.trim().startsWith('http') ? 'URL' : 'TEXT';
    final targets = <TransferTarget>[];
    for (final device in devices) {
      final endpoint = lan.endpointFor(device.id);
      final wrapped = encrypted.wrappedContentKeys[device.id];
      if (endpoint == null || wrapped == null) {
        throw const HttpException('區網目標已離線');
      }
      await lan.sendMessage(
        endpoint,
        transferId,
        contentType: contentType,
        content: encrypted.content,
        wrappedContentKey: wrapped,
      );
      targets.add(
        TransferTarget(
          deviceId: device.id,
          route: 'LAN',
          status: 'DELIVERED',
          bytesTransferred: utf8.encode(encrypted.content).length,
        ),
      );
    }
    final transfer = TransferSummary(
      id: transferId,
      senderDeviceId: _currentDevice?.id,
      contentType: contentType,
      status: 'DELIVERED',
      createdAt: DateTime.now(),
      targets: targets,
      files: const [],
      encryptedContent: encrypted.content,
      wrappedContentKeys: encrypted.wrappedContentKeys,
    );
    await _cache(transfer);
    await database.cacheLocalTransfer(transfer);
    await _queueLanMetrics(transfer, startedAt, groupId: groupId);
    return transfer;
  }

  Future<TransferSummary> _saveTextDraft(
    Map<String, dynamic> request,
    List<Device> devices,
  ) async {
    final id = const Uuid().v4();
    await database.saveDraft(id, {'kind': 'TEXT', 'request': request});
    final transfer = TransferSummary(
      id: id,
      senderDeviceId: _currentDevice?.id,
      contentType: request['contentType'] as String,
      status: 'CREATED',
      createdAt: DateTime.now(),
      encryptedContent: request['content'] as String,
      wrappedContentKeys:
          (request['wrappedContentKeys'] as Map<String, String>),
      targets: [
        for (final device in devices)
          TransferTarget(
            deviceId: device.id,
            route: 'LOCAL_DRAFT',
            status: 'CREATED',
            bytesTransferred: 0,
          ),
      ],
      files: const [],
    );
    await _cache(transfer);
    await database.cacheLocalTransfer(transfer);
    return transfer;
  }

  Future<TransferSummary> _sendFilesLanOnly(
    List<Device> devices,
    EncryptedFileTransfer encrypted, {
    String? groupId,
  }) async {
    final startedAt = DateTime.now().toUtc();
    final transferId = const Uuid().v4();
    final files = encrypted.files
        .map(
          (file) => TransferFile(
            id: const Uuid().v4(),
            name: file.record['name'] as String,
            size: file.size,
            chunkCount: file.chunks.length,
            chunkSize: file.record['chunkSize'] as int,
            sha256: file.sha256,
          ),
        )
        .toList();
    final manifest = [
      for (final file in files)
        {
          'id': file.id,
          'name': file.name,
          'size': file.size,
          'chunkCount': file.chunkCount,
          'chunkSize': file.chunkSize,
          'sha256': file.sha256,
        },
    ];
    final targets = <TransferTarget>[];
    final fileTargets = <TransferFileTarget>[];
    for (final device in devices) {
      final endpoint = lan.endpointFor(device.id);
      final wrapped = encrypted.wrappedContentKeys[device.id];
      if (endpoint == null || wrapped == null) {
        throw const HttpException('區網目標已離線');
      }
      var transferred = 0;
      for (final (index, upload) in encrypted.files.indexed) {
        transferred += await _uploadLanFile(
          endpoint,
          transferId,
          files[index].id,
          upload,
        );
        fileTargets.add(
          TransferFileTarget(
            fileIndex: index,
            deviceId: device.id,
            route: 'LAN',
            status: 'DELIVERED',
          ),
        );
      }
      await lan.sendMessage(
        endpoint,
        transferId,
        contentType: 'FILE',
        content: encrypted.content,
        wrappedContentKey: wrapped,
        files: manifest,
      );
      targets.add(
        TransferTarget(
          deviceId: device.id,
          route: 'LAN',
          status: 'DELIVERED',
          bytesTransferred: transferred,
        ),
      );
    }
    final transfer = TransferSummary(
      id: transferId,
      senderDeviceId: _currentDevice?.id,
      contentType: 'FILE',
      status: 'DELIVERED',
      createdAt: DateTime.now(),
      targets: targets,
      files: files,
      fileTargets: fileTargets,
      encryptedContent: encrypted.content,
      wrappedContentKeys: encrypted.wrappedContentKeys,
    );
    await _cache(transfer);
    await database.cacheLocalTransfer(transfer);
    await _queueLanMetrics(transfer, startedAt, groupId: groupId);
    return transfer;
  }

  Future<void> _queueLanMetrics(
    TransferSummary transfer,
    DateTime startedAt, {
    String? groupId,
  }) async {
    final sender = _currentDevice;
    if (sender == null) return;
    final completedAt = DateTime.now().toUtc();
    final elapsedMilliseconds = completedAt
        .difference(startedAt)
        .inMilliseconds
        .clamp(1, 1 << 31);
    final fileSize = transfer.files.fold<int>(
      0,
      (total, file) => total + file.size,
    );
    for (final target in transfer.targets) {
      final eventId = const Uuid().v4();
      final bytes = fileSize > 0 ? fileSize : target.bytesTransferred;
      await database.savePendingMetric(eventId, {
        'eventId': eventId,
        'transferId': transfer.id,
        'senderDeviceId': sender.id,
        'receiverDeviceId': target.deviceId,
        'groupId': ?groupId,
        'contentType': transfer.contentType,
        'route': 'LAN',
        'fileSize': bytes,
        'startedAt': startedAt.toIso8601String(),
        'completedAt': completedAt.toIso8601String(),
        'averageBytesPerSecond': (bytes * 1000) ~/ elapsedMilliseconds,
        'retryCount': 0,
        'succeeded': true,
      });
    }
  }

  Future<void> flushMetrics() async {
    while (true) {
      final rows = await database.pendingMetrics();
      if (rows.isEmpty) return;
      await api.sendJson('/api/metrics/batch', 'POST', {
        'events': rows.map((row) => row['payload']).toList(),
      });
      await database.deletePendingMetrics(
        rows.map((row) => row['eventId'] as String),
      );
      if (rows.length < 500) return;
    }
  }

  Future<TransferSummary> _saveFileDraft(
    Map<String, dynamic> request,
    List<Device> devices,
    List<String> sourcePaths,
    EncryptedFileTransfer encrypted,
  ) async {
    final id = const Uuid().v4();
    final recipe = <String, dynamic>{
      'contentKey': base64Encode(encrypted.contentKey),
      'files': {
        for (final (index, file) in encrypted.files.indexed)
          '$index': {
            'nonces': file.nonces,
            'sha256': file.sha256,
            'sourceSize': file.originalSize,
            'sourceModifiedAt': file.originalModifiedAt
                .toUtc()
                .toIso8601String(),
            'sourceSha256': file.originalSha256,
          },
      },
    };
    await database.saveDraft(id, {
      'kind': 'FILE',
      'request': request,
      'sources': sourcePaths,
      'recipe': recipe,
    });
    final files = encrypted.files
        .map(
          (file) => TransferFile(
            id: const Uuid().v4(),
            name: file.record['name'] as String,
            size: file.size,
            chunkCount: file.chunks.length,
            chunkSize: file.record['chunkSize'] as int,
            sha256: file.sha256,
          ),
        )
        .toList();
    final routeMode = request['routeMode'] as String;
    final allowLarge = request['allowLargeFileViaNode'] as bool? ?? false;
    final waitForLan =
        routeMode != 'NODE_ONLY' &&
        encrypted.files.every(
          (file) =>
              routeMode == 'LAN_ONLY' ||
              routeMode == 'WAIT_LAN' ||
              (!allowLarge && file.originalSize > 100 * 1024 * 1024),
        );
    if (waitForLan) {
      await _storage.write(
        key: '$_waitingRecipePrefix$id',
        value: jsonEncode(recipe),
      );
      for (final device in devices) {
        for (final (index, source) in encrypted.files.indexed) {
          await database.saveWaitingLanTask(
            id: '$id:$index:${device.id}',
            transferId: id,
            fileId: files[index].id,
            fileIndex: index,
            targetDeviceId: device.id,
            targetRoute: 'LOCAL_WAITING_LAN',
            sourcePath: sourcePaths[index],
            sourceSize: source.originalSize,
            sourceModifiedAt: source.originalModifiedAt,
            sourceSha256: source.originalSha256,
          );
        }
      }
    }
    final transfer = TransferSummary(
      id: id,
      senderDeviceId: _currentDevice?.id,
      contentType: 'FILE',
      status: waitForLan ? 'WAITING_FOR_LAN' : 'CREATED',
      createdAt: DateTime.now(),
      encryptedContent: encrypted.content,
      wrappedContentKeys:
          (request['wrappedContentKeys'] as Map<String, String>),
      targets: [
        for (final device in devices)
          TransferTarget(
            deviceId: device.id,
            route: waitForLan ? 'WAITING_LAN' : 'LOCAL_DRAFT',
            status: waitForLan ? 'WAITING_FOR_LAN' : 'CREATED',
            bytesTransferred: 0,
          ),
      ],
      files: files,
      fileTargets: waitForLan
          ? [
              for (final device in devices)
                for (final index in files.indexed.map((entry) => entry.$1))
                  TransferFileTarget(
                    fileIndex: index,
                    deviceId: device.id,
                    route: 'WAITING_LAN',
                    status: 'WAITING_FOR_LAN',
                  ),
            ]
          : const [],
    );
    await _cache(transfer);
    await database.cacheLocalTransfer(transfer);
    if (!waitForLan) return transfer;
    await retryWaitingLan();
    return (await database.localTransfers()).firstWhere(
      (item) => item.id == transfer.id,
    );
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
    try {
      await api.sendJson('/api/transfers/${transfer.id}/read', 'POST');
    } catch (_) {
      if (!transfer.targets.any((target) => target.route == 'LAN')) rethrow;
    }
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
    try {
      await api.sendJson('/api/transfers/${transfer.id}/read', 'POST');
    } catch (_) {
      if (!transfer.targets.any((target) => target.route == 'LAN')) rethrow;
    }
    return saved;
  }

  Future<void> cancel(String transferId) async {
    await api.sendJson('/api/transfers/$transferId/cancel', 'POST');
  }

  Future<void> pause(String transferId, String deviceId, int bytes) =>
      _reportProgress(transferId, deviceId, 'PAUSED', bytes);

  Future<void> retryWaitingLan() async {
    if (_retryingWaitingLan) return;
    _retryingWaitingLan = true;
    try {
      final tasks = await database.waitingLanTasks();
      for (final task in tasks) {
        if (task.status == 'DELIVERED_PENDING_SYNC') {
          final hasOutstanding = (await database.waitingLanTasks()).any(
            (other) =>
                other.id != task.id &&
                other.transferId == task.transferId &&
                other.targetDeviceId == task.targetDeviceId &&
                other.status != 'DELIVERED_PENDING_SYNC',
          );
          if (await _tryReportWaiting(
            task,
            hasOutstanding ? 'TRANSFERRING_LAN' : 'DELIVERED',
            route: task.targetRoute == 'WAITING_LAN' ? 'LAN' : null,
          )) {
            await database.deleteWaitingLanTask(task.id);
            await _cleanupWaitingRecipe(task.transferId);
          }
          continue;
        }
        if (task.status != 'WAITING_FOR_LAN') continue;
        final endpoint = lan.endpointFor(task.targetDeviceId);
        if (endpoint == null) continue;
        final source = File(task.sourcePath);
        if (!await source.exists()) {
          await database.updateWaitingLanStatus(task.id, 'SOURCE_FILE_MISSING');
          await _tryReportWaiting(task, 'SOURCE_FILE_MISSING');
          continue;
        }
        final stat = await source.stat();
        final currentDigest = await hashes.sha256.bind(source.openRead()).first;
        if (stat.size != task.sourceSize ||
            stat.modified.toUtc().millisecondsSinceEpoch !=
                task.sourceModifiedAt.toUtc().millisecondsSinceEpoch ||
            currentDigest.toString() != task.sourceSha256) {
          await database.updateWaitingLanStatus(task.id, 'SOURCE_FILE_CHANGED');
          await _tryReportWaiting(task, 'SOURCE_FILE_CHANGED');
          continue;
        }
        final encodedRecipe = await _storage.read(
          key: '$_waitingRecipePrefix${task.transferId}',
        );
        if (encodedRecipe == null) {
          await database.updateWaitingLanStatus(task.id, 'FAILED');
          continue;
        }
        final recipe = jsonDecode(encodedRecipe) as Map<String, dynamic>;
        final files = recipe['files'] as Map<String, dynamic>;
        final fileRecipe = files['${task.fileIndex}'] as Map<String, dynamic>;
        final support = await getApplicationSupportDirectory();
        EncryptedFileUpload? recreated;
        try {
          recreated = await crypto.recreateEncryptedFile(
            sourcePath: task.sourcePath,
            tempDirectory: path.join(support.path, 'temp'),
            contentKey: base64Decode(recipe['contentKey'] as String),
            nonces: (fileRecipe['nonces'] as List<dynamic>).cast<String>(),
          );
          if (recreated.sha256 != fileRecipe['sha256']) {
            throw const FormatException('等待 LAN 密文驗證失敗');
          }
          if (task.targetRoute != 'LOCAL_WAITING_LAN') {
            await _tryReportWaiting(task, 'TRANSFERRING_LAN');
          }
          await _uploadLanFile(
            endpoint,
            task.transferId,
            task.fileId,
            recreated,
          );
          final remaining = (await database.waitingLanTasks()).where(
            (other) =>
                other.id != task.id &&
                other.transferId == task.transferId &&
                other.targetDeviceId == task.targetDeviceId,
          );
          if (task.targetRoute == 'LOCAL_WAITING_LAN') {
            await database.deleteWaitingLanTask(task.id);
            if (remaining.isEmpty) {
              await _announceLocalWaitingTransfer(task, endpoint);
            }
            await _cleanupWaitingRecipe(task.transferId);
            continue;
          }
          final synchronized = await _tryReportWaiting(
            task,
            remaining.isEmpty ? 'DELIVERED' : 'TRANSFERRING_LAN',
            route: task.targetRoute == 'WAITING_LAN' ? 'LAN' : null,
          );
          if (synchronized) {
            await database.deleteWaitingLanTask(task.id);
            await _cleanupWaitingRecipe(task.transferId);
          } else {
            await database.updateWaitingLanStatus(
              task.id,
              'DELIVERED_PENDING_SYNC',
            );
          }
        } finally {
          if (recreated != null) {
            final temporary = File(recreated.tempPath);
            if (await temporary.exists()) await temporary.delete();
          }
        }
      }
    } finally {
      _retryingWaitingLan = false;
    }
  }

  Future<void> _announceLocalWaitingTransfer(
    WaitingLanTask task,
    LanEndpoint endpoint,
  ) async {
    final transfer = (await database.localTransfers()).firstWhere(
      (item) => item.id == task.transferId,
    );
    final content = transfer.encryptedContent;
    final wrapped = transfer.wrappedContentKeys[task.targetDeviceId];
    if (content == null || wrapped == null) {
      throw const FormatException('等待 LAN 傳輸中繼資料遺失');
    }
    await lan.sendMessage(
      endpoint,
      transfer.id,
      contentType: 'FILE',
      content: content,
      wrappedContentKey: wrapped,
      files: [
        for (final file in transfer.files)
          {
            'id': file.id,
            'name': file.name,
            'size': file.size,
            'chunkCount': file.chunkCount,
            'chunkSize': file.chunkSize,
            'sha256': file.sha256,
          },
      ],
    );
    final targets = [
      for (final target in transfer.targets)
        TransferTarget(
          deviceId: target.deviceId,
          route: target.deviceId == task.targetDeviceId ? 'LAN' : target.route,
          status: target.deviceId == task.targetDeviceId
              ? 'DELIVERED'
              : target.status,
          bytesTransferred: target.deviceId == task.targetDeviceId
              ? transfer.files.fold<int>(0, (total, file) => total + file.size)
              : target.bytesTransferred,
        ),
    ];
    final delivered = targets.every((target) => target.status == 'DELIVERED');
    final updated = TransferSummary(
      id: transfer.id,
      senderDeviceId: transfer.senderDeviceId,
      contentType: transfer.contentType,
      status: delivered ? 'DELIVERED' : 'WAITING_FOR_LAN',
      createdAt: transfer.createdAt,
      targets: targets,
      files: transfer.files,
      fileTargets: [
        for (final target in transfer.fileTargets)
          TransferFileTarget(
            fileIndex: target.fileIndex,
            deviceId: target.deviceId,
            route: target.deviceId == task.targetDeviceId
                ? 'LAN'
                : target.route,
            status: target.deviceId == task.targetDeviceId
                ? 'DELIVERED'
                : target.status,
          ),
      ],
      encryptedContent: content,
      wrappedContentKeys: transfer.wrappedContentKeys,
    );
    await _cache(updated);
    await database.cacheLocalTransfer(updated);
    if (delivered) {
      await database.deleteDraft(transfer.id);
      await _queueLanMetrics(updated, transfer.createdAt.toUtc());
    }
  }

  Future<void> flushDrafts() async {
    if (_flushingDrafts) return;
    _flushingDrafts = true;
    try {
      for (final draft in await database.drafts()) {
        if (draft.payload['kind'] == 'TEXT') {
          await api.sendJson(
            '/api/transfers',
            'POST',
            draft.payload['request'] as Map<String, dynamic>,
          );
        } else if (draft.payload['kind'] == 'FILE') {
          await _flushFileDraft(draft);
        } else {
          continue;
        }
        await database.deleteDraft(draft.id);
        await database.deleteLocalTransfer(draft.id);
      }
    } finally {
      _flushingDrafts = false;
    }
  }

  Future<void> _flushFileDraft(LocalDraft draft) async {
    final request = Map<String, dynamic>.from(draft.payload['request'] as Map);
    final sources = (draft.payload['sources'] as List<dynamic>).cast<String>();
    final recipe = Map<String, dynamic>.from(draft.payload['recipe'] as Map);
    final recipes = Map<String, dynamic>.from(recipe['files'] as Map);
    for (final (index, sourcePath) in sources.indexed) {
      final source = File(sourcePath);
      if (!await source.exists()) {
        throw const FileSystemException('SOURCE_FILE_MISSING');
      }
      final stat = await source.stat();
      final fileRecipe = Map<String, dynamic>.from(recipes['$index'] as Map);
      final digest = await hashes.sha256.bind(source.openRead()).first;
      if (stat.size != fileRecipe['sourceSize'] ||
          digest.toString() != fileRecipe['sourceSha256']) {
        throw const FormatException('SOURCE_FILE_CHANGED');
      }
    }
    final response =
        await api.sendJson('/api/transfers', 'POST', request)
            as Map<String, dynamic>;
    final transfer = TransferSummary.fromJson(response);
    final support = await getApplicationSupportDirectory();
    final recreatedFiles = <EncryptedFileUpload>[];
    try {
      for (final (index, sourcePath) in sources.indexed) {
        final fileRecipe = Map<String, dynamic>.from(recipes['$index'] as Map);
        final recreated = await crypto.recreateEncryptedFile(
          sourcePath: sourcePath,
          tempDirectory: path.join(support.path, 'temp'),
          contentKey: base64Decode(recipe['contentKey'] as String),
          nonces: (fileRecipe['nonces'] as List<dynamic>).cast<String>(),
        );
        if (recreated.sha256 != fileRecipe['sha256']) {
          throw const FormatException('HASH_MISMATCH');
        }
        recreatedFiles.add(recreated);
        if (transfer.fileTargets.any(
          (target) => target.fileIndex == index && target.route == 'NODE',
        )) {
          final input = await File(recreated.tempPath).open();
          try {
            for (final (chunkIndex, chunk) in recreated.chunks.indexed) {
              await api.uploadChunk(
                transfer.files[index].id,
                chunkIndex,
                await input.read(chunk.size),
                chunk.sha256Hex,
              );
            }
          } finally {
            await input.close();
          }
          await api.sendJson(
            '/api/files/${transfer.files[index].id}/complete',
            'POST',
          );
        }
      }
      await database.deleteWaitingLanTasksForTransfer(draft.id);
      await _storage.delete(key: '$_waitingRecipePrefix${draft.id}');
      final waitingTargets = transfer.fileTargets.where(
        (target) => target.route == 'WAITING_LAN',
      );
      if (waitingTargets.isNotEmpty) {
        await _storage.write(
          key: '$_waitingRecipePrefix${transfer.id}',
          value: jsonEncode(recipe),
        );
        for (final target in waitingTargets) {
          final fileRecipe = Map<String, dynamic>.from(
            recipes['${target.fileIndex}'] as Map,
          );
          await database.saveWaitingLanTask(
            id: '${transfer.id}:${target.fileIndex}:${target.deviceId}',
            transferId: transfer.id,
            fileId: transfer.files[target.fileIndex].id,
            fileIndex: target.fileIndex,
            targetDeviceId: target.deviceId,
            targetRoute: transfer.targets
                .firstWhere((item) => item.deviceId == target.deviceId)
                .route,
            sourcePath: sources[target.fileIndex],
            sourceSize: fileRecipe['sourceSize'] as int,
            sourceModifiedAt: DateTime.parse(
              fileRecipe['sourceModifiedAt'] as String,
            ),
            sourceSha256: fileRecipe['sourceSha256'] as String,
          );
        }
      }
      final nodeDeviceIds = transfer.fileTargets
          .where((target) => target.route == 'NODE')
          .map((target) => target.deviceId)
          .toSet();
      for (final target in transfer.targets.where(
        (target) => nodeDeviceIds.contains(target.deviceId),
      )) {
        await _reportProgress(
          transfer.id,
          target.deviceId,
          'AVAILABLE_ON_NODE',
          recreatedFiles.fold<int>(0, (total, file) => total + file.size),
        );
      }
    } finally {
      for (final recreated in recreatedFiles) {
        final temporary = File(recreated.tempPath);
        if (await temporary.exists()) await temporary.delete();
      }
    }
  }

  Future<void> setWaitingPaused(WaitingLanTask task, bool paused) async {
    final status = paused ? 'PAUSED' : 'WAITING_FOR_LAN';
    await database.updateWaitingLanStatus(task.id, status);
    if (paused) await _tryReportWaiting(task, 'PAUSED');
  }

  Future<void> removeWaitingTask(WaitingLanTask task) async {
    await database.deleteWaitingLanTask(task.id);
    await _cleanupWaitingRecipe(task.transferId);
    final hasRemainingTarget = (await database.waitingLanTasks()).any(
      (other) =>
          other.transferId == task.transferId &&
          other.targetDeviceId == task.targetDeviceId,
    );
    if (hasRemainingTarget) return;
    try {
      await api.sendJson(
        '/api/transfers/${task.transferId}/targets/${task.targetDeviceId}',
        'PUT',
        {
          'status': 'FAILED',
          'bytesTransferred': 0,
          'errorCode': 'USER_CANCELLED',
        },
      );
    } catch (_) {
      // Local removal remains effective while the node is offline.
    }
  }

  Future<void> replaceWaitingSource(
    WaitingLanTask task,
    String sourcePath,
  ) async {
    final source = File(sourcePath);
    final stat = await source.stat();
    if (stat.type != FileSystemEntityType.file) {
      throw const FileSystemException('來源檔案不存在');
    }
    final digest = await hashes.sha256.bind(source.openRead()).first;
    if (stat.size != task.sourceSize ||
        digest.toString() != task.sourceSha256) {
      await database.updateWaitingLanStatus(task.id, 'SOURCE_FILE_CHANGED');
      throw const FormatException('SOURCE_FILE_CHANGED');
    }
    await database.replaceWaitingLanSource(
      id: task.id,
      sourcePath: sourcePath,
      sourceSize: stat.size,
      sourceModifiedAt: stat.modified,
      sourceSha256: digest.toString(),
    );
  }

  Future<bool> _tryReportWaiting(
    WaitingLanTask task,
    String status, {
    String? route,
  }) async {
    try {
      final progress = <String, dynamic>{
        'status': status,
        'bytesTransferred': 0,
      };
      if (route != null) progress['route'] = route;
      await api.sendJson(
        '/api/transfers/${task.transferId}/targets/${task.targetDeviceId}',
        'PUT',
        progress,
      );
      return true;
    } catch (_) {
      // LAN-only delivery is synchronized when the node becomes available.
      return false;
    }
  }

  Future<void> _cleanupWaitingRecipe(String transferId) async {
    if (!(await database.waitingLanTasks()).any(
      (task) => task.transferId == transferId,
    )) {
      await _storage.delete(key: '$_waitingRecipePrefix$transferId');
    }
  }

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
