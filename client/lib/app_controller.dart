import 'dart:async';
import 'dart:convert';

import 'package:flutter/foundation.dart';
import 'package:web_socket_channel/web_socket_channel.dart';

import 'core/api_client.dart';
import 'core/crypto_service.dart';
import 'core/local_database.dart';
import 'core/lan_service.dart';
import 'core/models.dart';
import 'core/platform_share.dart';
import 'core/transfer_service.dart';

class AppController extends ChangeNotifier {
  AppController()
    : api = ApiClient(),
      crypto = CryptoService(),
      database = LocalDatabase(),
      lan = LanService(),
      platformShare = PlatformShareService() {
    transfersService = TransferService(
      api: api,
      crypto: crypto,
      database: database,
      lan: lan,
    );
    _lanSubscription = lan.changes.listen((_) {
      unawaited(_retryWaitingLan());
      unawaited(_updateDesktopStatus());
      notifyListeners();
    });
    _incomingLanSubscription = lan.incomingTransfers.listen(
      (incoming) => unawaited(_acceptIncomingLan(incoming)),
    );
    _shareSubscription = platformShare.shares.listen((share) {
      _pendingShare = share;
      notifyListeners();
    });
  }

  final ApiClient api;
  final CryptoService crypto;
  final LocalDatabase database;
  final LanService lan;
  final PlatformShareService platformShare;
  late final TransferService transfersService;
  StreamSubscription<Set<String>>? _lanSubscription;
  StreamSubscription<LanIncomingTransfer>? _incomingLanSubscription;
  StreamSubscription<PlatformSharePayload>? _shareSubscription;
  PlatformSharePayload? _pendingShare;
  UserAccount? account;
  Device? currentDevice;
  List<Device> devices = const [];
  List<GroupSummary> groups = const [];
  List<TransferSummary> transfers = const [];
  List<WaitingLanTask> waitingLanTasks = const [];
  bool loading = true;
  bool busy = false;
  bool nodeOnline = false;
  bool allowLargeFileViaNode = false;
  String? receiveDirectory;
  Set<String> get lanOnlineDeviceIds => lan.onlineDeviceIds;
  String? error;
  WebSocketChannel? _socket;
  StreamSubscription<dynamic>? _socketSubscription;
  Timer? _heartbeat;
  Timer? _reconnect;

  Future<void> initialize() async {
    try {
      await platformShare.initialize();
      await database.open();
      allowLargeFileViaNode =
          await database.setting('allow_large_file_via_node') == 'true';
      receiveDirectory = await database.setting('receive_directory');
      waitingLanTasks = await database.waitingLanTasks();
      transfers = await database.localTransfers();
      if (await api.restore()) {
        account = await api.account();
        await _synchronize();
      }
    } catch (reason) {
      error = _message(reason);
    } finally {
      loading = false;
      await _updateDesktopStatus();
      notifyListeners();
    }
  }

  PlatformSharePayload? takePendingShare() {
    final value = _pendingShare;
    _pendingShare = null;
    return value;
  }

  Future<void> _updateDesktopStatus() => platformShare.updateDesktopStatus({
    'online': account != null,
    'nodeOnline': nodeOnline,
    'deviceId': currentDevice?.id,
    'lanDeviceIds': lan.onlineDeviceIds.toList(),
    'updatedAt': DateTime.now().toUtc().toIso8601String(),
  });

  void queueShare(PlatformSharePayload share) {
    _pendingShare = share;
    notifyListeners();
  }

  Future<void> login(String node, String identifier, String password) async {
    await _run(() async {
      account = await api.login(node, identifier, password);
      await _synchronize();
    });
  }

  Future<void> logout() async {
    await _disconnect();
    await lan.stop();
    await api.logout();
    account = null;
    currentDevice = null;
    devices = const [];
    groups = const [];
    transfers = const [];
    waitingLanTasks = const [];
    await _updateDesktopStatus();
    notifyListeners();
  }

  Future<void> reload() async {
    if (account == null) return;
    final values = await Future.wait([
      api.devices(),
      api.groups(),
      api.transfers(),
    ]);
    devices = values[0] as List<Device>;
    groups = values[1] as List<GroupSummary>;
    final remoteTransfers = values[2] as List<TransferSummary>;
    final remoteIds = remoteTransfers.map((transfer) => transfer.id).toSet();
    final localTransfers = await database.localTransfers();
    transfers = [
      ...remoteTransfers,
      ...localTransfers.where(
        (transfer) =>
            !remoteIds.contains(transfer.id) &&
            transfer.targets.any((target) => target.route == 'LAN'),
      ),
    ];
    waitingLanTasks = await database.waitingLanTasks();
    currentDevice =
        devices.where((device) => device.id == currentDevice?.id).firstOrNull ??
        currentDevice;
    await lan.updateTrustedDevices(devices);
    for (final transfer in transfers) {
      await database.cacheTransfer(
        id: transfer.id,
        contentType: transfer.contentType,
        route:
            transfer.targets.map((target) => target.route).toSet().length == 1
            ? transfer.targets.firstOrNull?.route ?? 'NONE'
            : 'MIXED',
        status: transfer.status,
        totalBytes: transfer.files.fold<int>(
          0,
          (total, file) => total + file.size,
        ),
        createdAt: transfer.createdAt,
      );
    }
    notifyListeners();
  }

  Future<void> send({
    required String content,
    required List<Device> recipients,
    String? groupId,
    List<String> files = const [],
    bool groupAll = true,
    String routeMode = 'AUTOMATIC',
    bool notification = false,
  }) async {
    await _run(() async {
      await platformShare.startTransferService();
      try {
        final resolved = groupId == null || !groupAll
            ? recipients
            : await transfersService.groupDevices(groupId);
        if (files.isEmpty) {
          await transfersService.sendText(
            content: content,
            devices: resolved,
            groupId: groupId,
            groupAll: groupAll,
            lanAvailable: lan.onlineDeviceIds,
            nodeAvailable: nodeOnline,
            routeMode: routeMode,
            notification: notification,
          );
        } else {
          await transfersService.sendFiles(
            sourcePaths: files,
            devices: resolved,
            groupId: groupId,
            groupAll: groupAll,
            lanAvailable: lan.onlineDeviceIds,
            nodeAvailable: nodeOnline,
            routeMode: routeMode,
            allowLargeFileViaNode: allowLargeFileViaNode,
          );
        }
        if (nodeOnline) {
          await reload();
        } else {
          final local = await database.localTransfers();
          final localIds = local.map((item) => item.id).toSet();
          transfers = [
            ...local,
            ...transfers.where((item) => !localIds.contains(item.id)),
          ];
          waitingLanTasks = await database.waitingLanTasks();
          notifyListeners();
        }
      } finally {
        await platformShare.stopTransferService();
      }
    });
  }

  Future<void> setAllowLargeFileViaNode(bool value) async {
    allowLargeFileViaNode = value;
    await database.saveSetting('allow_large_file_via_node', value.toString());
    notifyListeners();
  }

  Future<void> setReceiveDirectory(String value) async {
    receiveDirectory = value;
    await database.saveSetting('receive_directory', value);
    notifyListeners();
  }

  Future<void> approve(Device device) => _run(() async {
    await api.sendJson('/api/devices/${device.id}/approve', 'POST');
    await reload();
  });

  Future<String> readText(TransferSummary transfer) async {
    if (account == null || currentDevice == null) {
      throw StateError('尚未登入設備');
    }
    late String value;
    await _run(() async {
      value = await transfersService.readText(
        transfer,
        account!,
        currentDevice!,
      );
      await reload();
    });
    return value;
  }

  Future<List<String>> receiveFiles(TransferSummary transfer) async {
    if (account == null || currentDevice == null) {
      throw StateError('尚未登入設備');
    }
    late List<String> value;
    await _run(() async {
      value = await transfersService.receiveFiles(
        transfer,
        account!,
        currentDevice!,
      );
      await reload();
    });
    return value;
  }

  Future<void> cancelTransfer(TransferSummary transfer) => _run(() async {
    await transfersService.cancel(transfer.id);
    await reload();
  });

  Future<void> setTransferPaused(TransferSummary transfer, bool paused) =>
      _run(() async {
        await transfersService.setTransferPaused(transfer, paused);
        if (nodeOnline) await reload();
      });

  Future<void> hideTransfer(TransferSummary transfer) => _run(() async {
    if (nodeOnline) {
      try {
        await api.sendJson('/api/transfers/${transfer.id}', 'DELETE');
      } catch (_) {
        if (!transfer.targets.every((target) => target.route == 'LAN')) rethrow;
      }
    }
    await database.deleteLocalTransfer(transfer.id);
    transfers = transfers.where((item) => item.id != transfer.id).toList();
    notifyListeners();
  });

  Future<void> setWaitingPaused(WaitingLanTask task, bool paused) =>
      _run(() async {
        await transfersService.setWaitingPaused(task, paused);
        waitingLanTasks = await database.waitingLanTasks();
      });

  Future<void> removeWaitingTask(WaitingLanTask task) => _run(() async {
    await transfersService.removeWaitingTask(task);
    waitingLanTasks = await database.waitingLanTasks();
  });

  Future<void> replaceWaitingSource(WaitingLanTask task, String sourcePath) =>
      _run(() async {
        await transfersService.replaceWaitingSource(task, sourcePath);
        waitingLanTasks = await database.waitingLanTasks();
        await transfersService.retryWaitingLan();
      });

  Future<Map<String, dynamic>> createPairingCode(Device device) async {
    late Map<String, dynamic> result;
    await _run(
      () async => result =
          await api.sendJson('/api/devices/${device.id}/pairing-code', 'POST')
              as Map<String, dynamic>,
    );
    return result;
  }

  Future<void> redeemPairingCode(String challengeId, String code) =>
      _run(() async {
        if (currentDevice == null) return;
        await api.sendJson('/api/devices/${currentDevice!.id}/pair', 'POST', {
          'challengeId': challengeId.trim(),
          'code': code.trim(),
        });
        await reload();
      });

  Future<void> _synchronize() async {
    final session = await transfersService.synchronizeDevice(account!);
    currentDevice = session.device;
    await reload();
    await lan.start(current: currentDevice!, trustedDevices: devices);
    await _connect();
  }

  Future<void> _connect() async {
    await _disconnect();
    if (currentDevice?.trusted != true) return;
    try {
      _socket = await api.connectWebSocket();
      _socketSubscription = _socket!.stream.listen(
        (event) {
          final message = jsonDecode(event as String) as Map<String, dynamic>;
          if (message['type'] == 'connected') {
            nodeOnline = true;
            unawaited(_retryWaitingLan());
            unawaited(_flushDrafts());
            unawaited(_flushMetrics());
            unawaited(_updateDesktopStatus());
            _heartbeat = Timer.periodic(
              const Duration(seconds: 15),
              (_) => _socket?.sink.add(jsonEncode({'type': 'heartbeat'})),
            );
          } else if (message['type'] == 'notification') {
            final notification =
                message['notification'] as Map<String, dynamic>?;
            _socket?.sink.add(
              jsonEncode({
                'type': 'notification_ack',
                'notificationId': notification?['id'],
              }),
            );
            unawaited(reload());
          }
          notifyListeners();
        },
        onDone: _scheduleReconnect,
        onError: (_) => _scheduleReconnect(),
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect();
    }
  }

  void _scheduleReconnect() {
    nodeOnline = false;
    unawaited(_updateDesktopStatus());
    _heartbeat?.cancel();
    _reconnect?.cancel();
    _reconnect = Timer(const Duration(seconds: 3), () => unawaited(_connect()));
    notifyListeners();
  }

  Future<void> _retryWaitingLan() async {
    try {
      await transfersService.retryWaitingLan();
      waitingLanTasks = await database.waitingLanTasks();
      notifyListeners();
    } catch (reason) {
      error = _message(reason);
      notifyListeners();
    }
  }

  Future<void> _flushDrafts() async {
    try {
      await transfersService.flushDrafts();
      await reload();
    } catch (reason) {
      error = _message(reason);
      notifyListeners();
    }
  }

  Future<void> _flushMetrics() async {
    try {
      await transfersService.flushMetrics();
    } catch (reason) {
      error = _message(reason);
      notifyListeners();
    }
  }

  Future<void> _acceptIncomingLan(LanIncomingTransfer incoming) async {
    final current = currentDevice;
    if (current == null) return;
    final transfer = TransferSummary(
      id: incoming.id,
      senderDeviceId: incoming.senderDeviceId,
      contentType: incoming.contentType,
      status: 'DELIVERED',
      createdAt: incoming.receivedAt.toLocal(),
      encryptedContent: incoming.content,
      wrappedContentKeys: {current.id: incoming.wrappedContentKey},
      targets: [
        TransferTarget(
          deviceId: current.id,
          route: 'LAN',
          status: 'DELIVERED',
          bytesTransferred: incoming.files.fold<int>(
            0,
            (total, file) => total + (file['size'] as int? ?? 0),
          ),
        ),
      ],
      files: incoming.files.map(TransferFile.fromJson).toList(),
    );
    transfers = [
      transfer,
      ...transfers.where((item) => item.id != transfer.id),
    ];
    await database.cacheTransfer(
      id: transfer.id,
      contentType: transfer.contentType,
      route: 'LAN',
      status: transfer.status,
      totalBytes: transfer.files.fold<int>(
        0,
        (total, file) => total + file.size,
      ),
      createdAt: transfer.createdAt,
    );
    await database.cacheLocalTransfer(transfer);
    notifyListeners();
  }

  Future<void> _disconnect() async {
    _reconnect?.cancel();
    _heartbeat?.cancel();
    await _socketSubscription?.cancel();
    await _socket?.sink.close();
    _socket = null;
    nodeOnline = false;
  }

  Future<void> _run(Future<void> Function() action) async {
    busy = true;
    error = null;
    notifyListeners();
    try {
      await action();
    } catch (reason) {
      error = _message(reason);
      rethrow;
    } finally {
      busy = false;
      notifyListeners();
    }
  }

  String _message(Object reason) {
    if (reason is ApiException) {
      return {
            'INVALID_CREDENTIALS': '帳號或密碼不正確',
            'PERMISSION_DENIED': '你沒有執行此操作的權限',
            'INVALID_TOKEN': '登入已失效，請重新登入',
            'FILE_TOO_LARGE': '檔案超過節點限制，請等待區網傳送',
            'QUOTA_EXCEEDED': '已超過可用配額',
          }[reason.code] ??
          '操作失敗：${reason.code}';
    }
    return reason.toString();
  }

  @override
  void dispose() {
    unawaited(_disconnect());
    unawaited(_lanSubscription?.cancel());
    unawaited(_incomingLanSubscription?.cancel());
    unawaited(_shareSubscription?.cancel());
    unawaited(lan.dispose());
    unawaited(platformShare.dispose());
    unawaited(database.close());
    api.close();
    super.dispose();
  }
}

extension _FirstOrNull<T> on Iterable<T> {
  T? get firstOrNull => isEmpty ? null : first;
}
