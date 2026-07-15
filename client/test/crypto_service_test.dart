import 'dart:convert';

import 'package:flutter_test/flutter_test.dart';
import 'package:nexdrop_client/core/crypto_service.dart';

class MemorySecretStore implements SecretStore {
  final values = <String, String>{};

  @override
  Future<String?> read(String key) async => values[key];

  @override
  Future<void> write(String key, String value) async => values[key] = value;
}

void main() {
  test('每個收件設備可解開同一份內容金鑰', () async {
    final first = CryptoService(store: MemorySecretStore());
    final second = CryptoService(store: MemorySecretStore());
    final firstKey = await first.ensureDeviceKey('first');
    final secondKey = await second.ensureDeviceKey('second');
    final encrypted = await first.encryptText('跨設備加密內容', [
      (id: 'first-device', publicKey: base64Encode(firstKey.publicKey)),
      (id: 'second-device', publicKey: base64Encode(secondKey.publicKey)),
    ]);
    expect(
      await first.decryptText(
        'first',
        encrypted.content,
        encrypted.wrappedContentKeys['first-device']!,
      ),
      '跨設備加密內容',
    );
    expect(
      await second.decryptText(
        'second',
        encrypted.content,
        encrypted.wrappedContentKeys['second-device']!,
      ),
      '跨設備加密內容',
    );
  });
}
