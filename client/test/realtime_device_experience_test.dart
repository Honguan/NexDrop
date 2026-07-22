import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('桌面與 Android 預設全選設備並移除群組頁', () {
    final mainSource = File('lib/main.dart').readAsStringSync();
    final sendViewSource = File('lib/ui/send_view.dart').readAsStringSync();
    expect(mainSource, contains("import 'ui/send_view.dart'"));
    expect(sendViewSource, contains("selectedDevices.addAll"));
    expect(mainSource, isNot(contains("'群組'")));
    expect(mainSource, contains('複製'));
    expect(mainSource, contains('刪除'));
    expect(
      sendViewSource,
      contains('ValueListenableBuilder<TextEditingValue>'),
    );
    expect(sendViewSource, contains('LogicalKeyboardKey.enter'));
    expect(sendViewSource, contains('_sendFromShortcut'));
  });

  test('共用頁面由呼叫端明確提供重新整理動作', () {
    final mainSource = File('lib/main.dart').readAsStringSync();
    final sendViewSource = File('lib/ui/send_view.dart').readAsStringSync();
    final pageSource = File('lib/ui/nexdrop_page.dart').readAsStringSync();
    expect(mainSource, contains("import 'ui/nexdrop_page.dart'"));
    expect(sendViewSource, contains('onRefresh: widget.controller.reload'));
    expect(pageSource, contains('required this.onRefresh'));
    expect(pageSource, contains('onRefresh: onRefresh'));
    expect(pageSource, isNot(contains('findAncestorWidgetOfExactType')));
  });

  test('Windows 最小化使用已封裝的系統匣圖示', () {
    final mainSource = File('lib/main.dart').readAsStringSync();
    final desktopLifecycle = File(
      'lib/ui/desktop_lifecycle.dart',
    ).readAsStringSync();
    final pubspec = File('pubspec.yaml').readAsStringSync();
    expect(mainSource, contains("import 'ui/desktop_lifecycle.dart'"));
    expect(desktopLifecycle, contains('onWindowMinimize'));
    expect(desktopLifecycle, contains('getApplicationSupportDirectory'));
    expect(desktopLifecycle, contains('await _desktopTrayIconPath()'));
    expect(pubspec, contains('windows/runner/resources/app_icon.ico'));
  });

  test('裝置與節點狀態透過心跳即時刷新', () {
    final controller = File('lib/app_controller.dart').readAsStringSync();
    final models = File('lib/core/models.dart').readAsStringSync();
    final realtime = File(
      'lib/core/realtime_connection.dart',
    ).readAsStringSync();
    expect(controller, contains('RealtimeConnection('));
    expect(controller, contains('onHeartbeat: reload'));
    expect(realtime, contains("case 'heartbeat_ack'"));
    expect(realtime, contains("'type': 'notification_ack'"));
    expect(realtime, contains('_scheduleReconnect'));
    expect(models, contains('final bool online'));
    expect(models, contains('final DateTime? lastSeenAt'));
  });
}
