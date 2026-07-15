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
    _lanSubscription = lan.changes.listen((_) => notifyListeners());
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
  StreamSubscription<PlatformSharePayload>? _shareSubscription;
  PlatformSharePayload? _pendingShare;
  UserAccount? account;
  Device? currentDevice;
  List<Device> devices = const [];
  List<GroupSummary> groups = const [];
  List<TransferSummary> transfers = const [];
  bool loading = true;
  bool busy = false;
  bool nodeOnline = false;
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
      if (await api.restore()) {
        account = await api.account();
        await _synchronize();
      }
    } catch (reason) {
      error = _message(reason);
    } finally {
      loading = false;
      notifyListeners();
    }
  }

  PlatformSharePayload? takePendingShare() {
    final value = _pendingShare;
    _pendingShare = null;
    return value;
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
    transfers = values[2] as List<TransferSummary>;
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
  }) async {
    await _run(() async {
      await platformShare.startTransferService();
      try {
        final resolved = groupId == null
            ? recipients
            : await transfersService.groupDevices(groupId);
        if (files.isEmpty) {
          await transfersService.sendText(
            content: content,
            devices: resolved,
            groupId: groupId,
            lanAvailable: lan.onlineDeviceIds,
          );
        } else {
          await transfersService.sendFiles(
            sourcePaths: files,
            devices: resolved,
            groupId: groupId,
            lanAvailable: lan.onlineDeviceIds,
          );
        }
        await reload();
      } finally {
        await platformShare.stopTransferService();
      }
    });
  }

  Future<void> approve(Device device) => _run(() async {
    await api.sendJson('/api/devices/${device.id}/approve', 'POST');
    await reload();
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
    _heartbeat?.cancel();
    _reconnect?.cancel();
    _reconnect = Timer(const Duration(seconds: 3), () => unawaited(_connect()));
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
