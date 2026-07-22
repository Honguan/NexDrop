import 'dart:async';
import 'dart:convert';
import 'dart:io';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexdrop_client/core/realtime_connection.dart';
import 'package:web_socket_channel/io.dart';

void main() {
  test('即時連線忽略損壞訊息、確認通知並在斷線後重連', () async {
    final server = await HttpServer.bind(InternetAddress.loopbackIPv4, 0);
    addTearDown(() => server.close(force: true));

    final firstPeer = Completer<WebSocket>();
    final secondPeer = Completer<WebSocket>();
    var acceptedConnections = 0;
    server.transform(WebSocketTransformer()).listen((peer) {
      acceptedConnections += 1;
      if (acceptedConnections == 1) {
        firstPeer.complete(peer);
      } else if (!secondPeer.isCompleted) {
        secondPeer.complete(peer);
      }
    });

    var connectedEvents = 0;
    var heartbeatEvents = 0;
    var notificationEvents = 0;
    final connectedHandled = Completer<void>();
    final heartbeatHandled = Completer<void>();
    final notificationHandled = Completer<void>();
    final acknowledgementReceived = Completer<void>();
    final statuses = <bool>[];
    final connection = RealtimeConnection(
      connect: () async {
        final channel = IOWebSocketChannel.connect(
          Uri.parse('ws://127.0.0.1:${server.port}'),
        );
        await channel.ready;
        return channel;
      },
      onConnected: () {
        connectedEvents += 1;
        connectedHandled.complete();
      },
      onHeartbeat: () {
        heartbeatEvents += 1;
        heartbeatHandled.complete();
      },
      onNotification: () {
        notificationEvents += 1;
        notificationHandled.complete();
      },
      onStatusChanged: statuses.add,
      heartbeatInterval: const Duration(hours: 1),
      reconnectDelay: Duration.zero,
    );
    addTearDown(connection.close);

    await connection.start();
    final peer = await firstPeer.future;
    final replies = <Map<String, dynamic>>[];
    peer.listen((message) {
      final decoded = jsonDecode(message as String);
      if (decoded is Map<String, dynamic>) {
        replies.add(decoded);
        if (decoded['type'] == 'notification_ack') {
          acknowledgementReceived.complete();
        }
      }
    });

    peer
      ..add('{invalid-json')
      ..add(jsonEncode({'type': 'connected'}))
      ..add(jsonEncode({'type': 'heartbeat_ack'}))
      ..add(
        jsonEncode({
          'type': 'notification',
          'notification': {'id': 'notification-1'},
        }),
      );
    await Future.wait([
      connectedHandled.future,
      heartbeatHandled.future,
      notificationHandled.future,
      acknowledgementReceived.future,
    ]).timeout(const Duration(seconds: 2));

    expect(connectedEvents, 1);
    expect(heartbeatEvents, 1);
    expect(notificationEvents, 1);
    expect(statuses, [true]);
    expect(
      replies,
      contains(
        allOf(
          containsPair('type', 'notification_ack'),
          containsPair('notificationId', 'notification-1'),
        ),
      ),
    );

    await peer.close();
    await secondPeer.future.timeout(const Duration(seconds: 2));
    expect(acceptedConnections, 2);
    expect(statuses, [true, false]);
  });
}
