import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path_provider/path_provider.dart';

import 'api_client.dart';
import 'crypto_service.dart';
import 'lan_identity.dart';
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
    FlutterSecureStorage? storage,
    LanIdentityStore? lanIdentityStore,
  }) : _storage = storage ?? const FlutterSecureStorage(),
       _lanIdentityStore = lanIdentityStore ?? LanIdentityStore();

  static const _deviceKeyPrefix = 'nexdrop.device_id.';
  final ApiClient api;
  final CryptoService crypto;
  final LocalDatabase database;
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
