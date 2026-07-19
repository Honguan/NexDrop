import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter/services.dart';
import 'package:flutter_local_notifications/flutter_local_notifications.dart';
import 'package:path/path.dart' as path;

class PlatformSharePayload {
  const PlatformSharePayload({required this.text, required this.files});

  factory PlatformSharePayload.fromMap(Map<dynamic, dynamic> value) =>
      PlatformSharePayload(
        text: value['text'] as String? ?? '',
        files: (value['files'] as List<dynamic>? ?? const []).cast<String>(),
      );

  final String text;
  final List<String> files;
}

class PlatformShareService {
  static const _methods = MethodChannel('com.nexdrop/client');
  static const _events = EventChannel('com.nexdrop/client/shares');
  final _shares = StreamController<PlatformSharePayload>.broadcast();
  final _notifications = FlutterLocalNotificationsPlugin();
  bool _notificationsReady = false;
  StreamSubscription<dynamic>? _subscription;
  Timer? _windowsTimer;
  bool _readingWindowsQueue = false;

  Stream<PlatformSharePayload> get shares => _shares.stream;

  Future<void> startTransferService() => _optionalCall('startTransferService');

  Future<void> stopTransferService() => _optionalCall('stopTransferService');

  Future<void> updateDesktopStatus(Map<String, dynamic> status) async {
    if (!Platform.isWindows) return;
    final local = Platform.environment['LOCALAPPDATA'];
    if (local == null) return;
    final directory = Directory(path.join(local, 'NexDrop'));
    await directory.create(recursive: true);
    final destination = File(path.join(directory.path, 'status.json'));
    final temporary = File('${destination.path}.tmp');
    await temporary.writeAsString(jsonEncode(status), flush: true);
    if (await destination.exists()) await destination.delete();
    await temporary.rename(destination.path);
  }

  Future<void> _optionalCall(String method) async {
    try {
      await _methods.invokeMethod<void>(method);
    } on MissingPluginException {
      // Only Android provides the foreground transfer service.
    } on PlatformException {
      // The transfer itself can continue if Android rejects the service start.
    }
  }

  Future<void> initialize() async {
    await _initializeNotifications();
    if (Platform.isWindows) {
      await _startWindowsBridge();
      return;
    }
    try {
      final initial = await _methods.invokeMapMethod<dynamic, dynamic>(
        'getInitialShare',
      );
      if (initial != null) _shares.add(PlatformSharePayload.fromMap(initial));
      _subscription = _events.receiveBroadcastStream().listen((value) {
        if (value is Map<dynamic, dynamic>) {
          _shares.add(PlatformSharePayload.fromMap(value));
        }
      }, onError: (_) {});
    } on MissingPluginException {
      // Desktop platforms use command-line integration instead.
    } on PlatformException {
      // Sharing remains optional when the host platform rejects an item.
    }
  }

  Future<void> _initializeNotifications() async {
    try {
      await _notifications.initialize(
        settings: const InitializationSettings(
          android: AndroidInitializationSettings('@mipmap/ic_launcher'),
          windows: WindowsInitializationSettings(
            appName: 'NexDrop',
            appUserModelId: 'NexDrop.Desktop',
            guid: '4f3fb6d9-827b-42a8-a45a-90ce17f5fc43',
          ),
        ),
      );
      _notificationsReady = true;
    } catch (_) {
      _notificationsReady = false;
    }
  }

  Future<void> showNotification(String title, String body) async {
    if (!_notificationsReady) return;
    try {
      await _notifications.show(
        id: DateTime.now().millisecondsSinceEpoch.remainder(1 << 31),
        title: title,
        body: body,
        notificationDetails: const NotificationDetails(
          android: AndroidNotificationDetails(
            'nexdrop_messages',
            'NexDrop 訊息',
            channelDescription: '設備加入與收到新內容通知',
            importance: Importance.high,
            priority: Priority.high,
          ),
          windows: WindowsNotificationDetails(),
        ),
      );
    } catch (_) {
      // The in-app banner remains available when the OS rejects notifications.
    }
  }

  Future<void> _startWindowsBridge() async {
    final service = File(
      path.join(
        path.dirname(Platform.resolvedExecutable),
        'nexdrop-desktop-service.exe',
      ),
    );
    if (await service.exists()) {
      await Process.start(
        service.path,
        const [],
        mode: ProcessStartMode.detached,
      );
    }
    _windowsTimer = Timer.periodic(
      const Duration(seconds: 1),
      (_) => unawaited(_readWindowsQueue()),
    );
    await _readWindowsQueue();
  }

  Future<void> _readWindowsQueue() async {
    if (_readingWindowsQueue) return;
    _readingWindowsQueue = true;
    try {
      final local = Platform.environment['LOCALAPPDATA'];
      if (local == null) return;
      final directory = Directory(path.join(local, 'NexDrop', 'bridge-queue'));
      if (!await directory.exists()) return;
      await for (final entry in directory.list()) {
        if (entry is! File || !entry.path.toLowerCase().endsWith('.json')) {
          continue;
        }
        try {
          final value =
              jsonDecode(await entry.readAsString()) as Map<String, dynamic>;
          final kind = value['kind'] as String? ?? '';
          final url = value['url'] as String? ?? '';
          final text = value['text'] as String? ?? '';
          final title = value['title'] as String? ?? '';
          final content = kind == 'SELECTION'
              ? text
              : [title, url].where((part) => part.isNotEmpty).join('\n');
          if (content.isNotEmpty) {
            _shares.add(PlatformSharePayload(text: content, files: const []));
          }
          await entry.delete();
        } catch (_) {
          final failed = File('${entry.path}.invalid');
          if (await failed.exists()) {
            await entry.delete();
          } else {
            await entry.rename(failed.path);
          }
        }
      }
    } finally {
      _readingWindowsQueue = false;
    }
  }

  Future<void> dispose() async {
    _windowsTimer?.cancel();
    await _subscription?.cancel();
    await _shares.close();
  }
}
