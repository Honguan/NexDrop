import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('待核准設備會顯示兩種配對資料來源', () async {
    final source = await File('lib/main.dart').readAsString();
    final dialog = RegExp(
      r'Future<void> _showPairDialog[\s\S]*?class _Page',
    ).firstMatch(source)?.group(0);

    expect(dialog, isNotNull);
    for (final text in const [
      '另一台已信任設備',
      '配對碼',
      '管理員登入 NexDrop Web',
      '核准',
      '已在網頁核准，重新整理',
      '不會開啟相機',
    ]) {
      expect(dialog, contains(text));
    }
  });
}
