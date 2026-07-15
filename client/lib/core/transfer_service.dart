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
  static const _waitingRecipePrefix = 'nexdrop.waiting.recipe.';
  final ApiClient api;
  final CryptoService crypto;
  final LocalDatabase database;
  final LanService lan;
  final FlutterSecureStorage _storage;
  final LanIdentityStore _lanIdentityStore;
  bool _retryingWaitingLan = false;

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
    bool groupAll = true,
    Set<String> lanAvailable = const {},
  }) async {
    final recipients = devices
        .where((device) => device.trusted && device.publicKey != null)
        .map((device) => (id: device.id, publicKey: device.publicKey!))
        .toList();
    final encrypted = await crypto.encryptText(content.trim(), recipients);
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
    bool groupAll = true,
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
          await _tryReportWaiting(task, 'TRANSFERRING_LAN');
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
