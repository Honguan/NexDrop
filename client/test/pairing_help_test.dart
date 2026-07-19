import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('設備只使用節點連結與節點密鑰加入', () async {
    final mainSource = await File('lib/main.dart').readAsString();
    final apiSource = await File('lib/core/api_client.dart').readAsString();

    expect(mainSource, contains('節點密鑰'));
    expect(mainSource, contains('貼上匯入'));
    expect(mainSource, contains('節點聊天室'));
    expect(apiSource, contains('/api/node/enroll'));
    expect(apiSource, contains('exportNodeConfiguration'));
    for (final removed in const ['nexdrop://pair', '配對碼已由本機自動產生', '核准新設備', '輸入配對碼']) {
      expect(mainSource, isNot(contains(removed)));
    }
  });
}
