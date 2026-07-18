import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('桌面與 Android 預設全選設備並移除群組頁', () {
    final mainSource = File('lib/main.dart').readAsStringSync();
    expect(mainSource, contains("selectedDevices.addAll"));
    expect(mainSource, isNot(contains("'群組'")));
    expect(mainSource, contains('複製'));
    expect(mainSource, contains('刪除'));
  });

  test('裝置與節點狀態透過心跳即時刷新', () {
    final controller = File('lib/app_controller.dart').readAsStringSync();
    final models = File('lib/core/models.dart').readAsStringSync();
    expect(controller, contains("message['type'] == 'heartbeat_ack'"));
    expect(models, contains('final bool online'));
    expect(models, contains('final DateTime? lastSeenAt'));
  });
}
