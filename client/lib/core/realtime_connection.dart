import 'dart:async';
import 'dart:convert';

import 'package:web_socket_channel/web_socket_channel.dart';

typedef RealtimeConnector = Future<WebSocketChannel> Function();
typedef RealtimeCallback = FutureOr<void> Function();
typedef RealtimeStatusCallback = void Function(bool online);

class RealtimeConnection {
  RealtimeConnection({
    required this.connect,
    required this.onConnected,
    required this.onHeartbeat,
    required this.onNotification,
    required this.onStatusChanged,
    this.heartbeatInterval = const Duration(seconds: 5),
    this.reconnectDelay = const Duration(seconds: 3),
  });

  final RealtimeConnector connect;
  final RealtimeCallback onConnected;
  final RealtimeCallback onHeartbeat;
  final RealtimeCallback onNotification;
  final RealtimeStatusCallback onStatusChanged;
  final Duration heartbeatInterval;
  final Duration reconnectDelay;

  WebSocketChannel? _socket;
  StreamSubscription<dynamic>? _subscription;
  Timer? _heartbeat;
  Timer? _reconnect;
  bool _online = false;
  bool _stopped = true;
  bool _closed = false;

  Future<void> start() async {
    if (_closed) return;
    _stopped = false;
    await _open();
  }

  Future<void> stop() async {
    _stopped = true;
    _reconnect?.cancel();
    _heartbeat?.cancel();
    await _closeSocket();
    _setOnline(false);
  }

  Future<void> close() async {
    if (_closed) return;
    _closed = true;
    await stop();
  }

  Future<void> _open() async {
    if (_closed || _stopped) return;
    await _closeSocket();
    if (_closed || _stopped) return;
    try {
      final socket = await connect();
      if (_closed || _stopped) {
        await socket.sink.close();
        return;
      }
      _socket = socket;
      _subscription = socket.stream.listen(
        _handleEvent,
        onDone: _scheduleReconnect,
        onError: (_) => _scheduleReconnect(),
        cancelOnError: true,
      );
    } catch (_) {
      _scheduleReconnect();
    }
  }

  void _handleEvent(dynamic event) {
    if (event is! String) return;
    Object? decoded;
    try {
      decoded = jsonDecode(event);
    } catch (_) {
      return;
    }
    if (decoded is! Map<String, dynamic>) return;

    switch (decoded['type']) {
      case 'connected':
        _setOnline(true);
        _heartbeat?.cancel();
        _heartbeat = Timer.periodic(
          heartbeatInterval,
          (_) => _send({'type': 'heartbeat'}),
        );
        unawaited(Future.sync(onConnected));
        break;
      case 'heartbeat_ack':
        unawaited(Future.sync(onHeartbeat));
        break;
      case 'notification':
        final notification = decoded['notification'];
        _send({
          'type': 'notification_ack',
          'notificationId': notification is Map<String, dynamic>
              ? notification['id']
              : null,
        });
        unawaited(Future.sync(onNotification));
        break;
    }
  }

  void _send(Map<String, dynamic> message) {
    if (_closed || _stopped) return;
    _socket?.sink.add(jsonEncode(message));
  }

  void _scheduleReconnect() {
    if (_closed || _stopped) return;
    _heartbeat?.cancel();
    _reconnect?.cancel();
    _setOnline(false);
    _reconnect = Timer(reconnectDelay, () => unawaited(_open()));
  }

  void _setOnline(bool value) {
    if (_online == value) return;
    _online = value;
    onStatusChanged(value);
  }

  Future<void> _closeSocket() async {
    final subscription = _subscription;
    final socket = _socket;
    _subscription = null;
    _socket = null;
    await subscription?.cancel();
    await socket?.sink.close();
  }
}
