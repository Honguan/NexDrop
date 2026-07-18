import 'dart:io';

import 'package:flutter_test/flutter_test.dart';

void main() {
  test('待核准設備自行產生配對資料並由信任設備核准', () async {
    final source = await File('lib/main.dart').readAsString();

    for (final text in const [
      '配對碼已由本機自動產生',
      '請在 10 分鐘內',
      '核准新設備',
      '輸入配對碼',
      'nexdrop://pair',
    ]) {
      expect(source, contains(text));
    }
    expect(source, isNot(contains('管理員登入 NexDrop Web')));
    expect(source, isNot(contains('已在網頁核准，重新整理')));
  });
}
