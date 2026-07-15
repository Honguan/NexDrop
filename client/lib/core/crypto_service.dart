import 'dart:convert';
import 'dart:io';

import 'package:cryptography/cryptography.dart';
import 'package:crypto/crypto.dart' as hashes;
import 'package:flutter_secure_storage/flutter_secure_storage.dart';
import 'package:path/path.dart' as path;

abstract interface class SecretStore {
  Future<String?> read(String key);
  Future<void> write(String key, String value);
}

class PlatformSecretStore implements SecretStore {
  PlatformSecretStore([FlutterSecureStorage? storage])
    : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  @override
  Future<String?> read(String key) => _storage.read(key: key);

  @override
  Future<void> write(String key, String value) =>
      _storage.write(key: key, value: value);
}

class DeviceKeyMaterial {
  const DeviceKeyMaterial({required this.privateKey, required this.publicKey});

  final List<int> privateKey;
  final List<int> publicKey;
}

class EncryptedEnvelope {
  const EncryptedEnvelope({
    required this.content,
    required this.wrappedContentKeys,
  });

  final String content;
  final Map<String, String> wrappedContentKeys;
}

class EncryptedFileChunk {
  const EncryptedFileChunk({required this.size, required this.sha256Hex});

  final int size;
  final String sha256Hex;
}

class EncryptedFileUpload {
  const EncryptedFileUpload({
    required this.originalName,
    required this.originalSize,
    required this.originalModifiedAt,
    required this.originalSha256,
    required this.tempPath,
    required this.size,
    required this.sha256,
    required this.chunks,
    required this.nonces,
  });

  final String originalName;
  final int originalSize;
  final DateTime originalModifiedAt;
  final String originalSha256;
  final String tempPath;
  final int size;
  final String sha256;
  final List<EncryptedFileChunk> chunks;
  final List<String> nonces;

  Map<String, dynamic> get record => {
    'name':
        'encrypted-${base64Url.encode(utf8.encode(originalName)).replaceAll('=', '')}.nxd',
    'mimeType': 'application/octet-stream',
    'size': size,
    'sha256': sha256,
    'chunkSize': 8 * 1024 * 1024 + 28,
    'chunkCount': chunks.length,
  };
}

class EncryptedFileTransfer {
  const EncryptedFileTransfer({
    required this.content,
    required this.wrappedContentKeys,
    required this.files,
    required this.contentKey,
  });

  final String content;
  final Map<String, String> wrappedContentKeys;
  final List<EncryptedFileUpload> files;
  final List<int> contentKey;
}

class FileChunkDecryptor {
  const FileChunkDecryptor(this._key);

  final SecretKey _key;

  Future<List<int>> decrypt(List<int> bytes) {
    if (bytes.length < 28) {
      throw const FormatException('無效的加密檔案分段');
    }
    return CryptoService._aes.decrypt(
      SecretBox(
        bytes.sublist(12, bytes.length - 16),
        nonce: bytes.sublist(0, 12),
        mac: Mac(bytes.sublist(bytes.length - 16)),
      ),
      secretKey: _key,
    );
  }
}

class CryptoService {
  CryptoService({SecretStore? store}) : _store = store ?? PlatformSecretStore();

  static const _privateKeyPrefix = 'nexdrop.device.private.';
  static const _publicKeyPrefix = 'nexdrop.device.public.';
  static final _x25519 = X25519();
  static final _aes = AesGcm.with256bits();
  static final _hkdf = Hkdf(hmac: Hmac.sha256(), outputLength: 32);
  final SecretStore _store;

  Future<DeviceKeyMaterial> ensureDeviceKey(String accountId) async {
    final privateValue = await _store.read('$_privateKeyPrefix$accountId');
    final publicValue = await _store.read('$_publicKeyPrefix$accountId');
    if (privateValue != null && publicValue != null) {
      return DeviceKeyMaterial(
        privateKey: base64Decode(privateValue),
        publicKey: base64Decode(publicValue),
      );
    }
    final keyPair = await _x25519.newKeyPair();
    final privateKey = await keyPair.extractPrivateKeyBytes();
    final publicKey = (await keyPair.extractPublicKey()).bytes;
    await Future.wait([
      _store.write('$_privateKeyPrefix$accountId', base64Encode(privateKey)),
      _store.write('$_publicKeyPrefix$accountId', base64Encode(publicKey)),
    ]);
    return DeviceKeyMaterial(privateKey: privateKey, publicKey: publicKey);
  }

  Future<EncryptedEnvelope> encryptText(
    String plaintext,
    List<({String id, String publicKey})> recipients,
  ) async {
    if (plaintext.isEmpty || recipients.isEmpty) {
      throw const FormatException('加密內容與收件設備不可為空');
    }
    final contentKey = await _aes.newSecretKey();
    final contentKeyBytes = await contentKey.extractBytes();
    final content = await _encryptEnvelope(utf8.encode(plaintext), contentKey);
    final wrapped = <String, String>{};
    for (final recipient in recipients) {
      wrapped[recipient.id] = base64Encode(
        utf8.encode(
          jsonEncode(
            await _wrapKey(contentKeyBytes, base64Decode(recipient.publicKey)),
          ),
        ),
      );
    }
    return EncryptedEnvelope(content: content, wrappedContentKeys: wrapped);
  }

  Future<EncryptedFileTransfer> encryptFiles(
    List<String> sourcePaths,
    String tempDirectory,
    List<({String id, String publicKey})> recipients,
  ) async {
    if (sourcePaths.isEmpty || recipients.isEmpty) {
      throw const FormatException('加密檔案與收件設備不可為空');
    }
    const plaintextChunkSize = 8 * 1024 * 1024;
    final contentKey = await _aes.newSecretKey();
    final contentKeyBytes = await contentKey.extractBytes();
    final uploads = <EncryptedFileUpload>[];
    final metadata = <Map<String, dynamic>>[];
    await Directory(tempDirectory).create(recursive: true);
    for (final (index, sourcePath) in sourcePaths.indexed) {
      final source = File(sourcePath);
      final stat = await source.stat();
      if (stat.type != FileSystemEntityType.file) {
        throw const FileSystemException('來源檔案不存在');
      }
      final tempPath = path.join(
        tempDirectory,
        'upload-${DateTime.now().microsecondsSinceEpoch}-$index.nxd',
      );
      final input = await source.open();
      final output = await File(tempPath).open(mode: FileMode.write);
      final wholeDigest = _DigestSink();
      final wholeInput = hashes.sha256.startChunkedConversion(wholeDigest);
      final sourceDigest = _DigestSink();
      final sourceInput = hashes.sha256.startChunkedConversion(sourceDigest);
      final chunks = <EncryptedFileChunk>[];
      final nonces = <String>[];
      var encryptedSize = 0;
      try {
        while (true) {
          final plaintext = await input.read(plaintextChunkSize);
          if (plaintext.isEmpty) break;
          sourceInput.add(plaintext);
          final nonce = _aes.newNonce();
          nonces.add(base64Encode(nonce));
          final encrypted = await _aes.encrypt(
            plaintext,
            secretKey: contentKey,
            nonce: nonce,
          );
          final bytes = <int>[
            ...nonce,
            ...encrypted.cipherText,
            ...encrypted.mac.bytes,
          ];
          await output.writeFrom(bytes);
          wholeInput.add(bytes);
          encryptedSize += bytes.length;
          chunks.add(
            EncryptedFileChunk(
              size: bytes.length,
              sha256Hex: hashes.sha256.convert(bytes).toString(),
            ),
          );
        }
      } finally {
        await input.close();
        await output.close();
        wholeInput.close();
        sourceInput.close();
      }
      metadata.add({
        'name': path.basename(sourcePath),
        'mimeType': 'application/octet-stream',
        'size': stat.size,
      });
      uploads.add(
        EncryptedFileUpload(
          originalName: path.basename(sourcePath),
          originalSize: stat.size,
          originalModifiedAt: stat.modified,
          originalSha256: sourceDigest.value.toString(),
          tempPath: tempPath,
          size: encryptedSize,
          sha256: base64Encode(wholeDigest.value.bytes),
          chunks: chunks,
          nonces: nonces,
        ),
      );
    }
    final wrapped = <String, String>{};
    for (final recipient in recipients) {
      wrapped[recipient.id] = base64Encode(
        utf8.encode(
          jsonEncode(
            await _wrapKey(contentKeyBytes, base64Decode(recipient.publicKey)),
          ),
        ),
      );
    }
    return EncryptedFileTransfer(
      content: await _encryptEnvelope(
        utf8.encode(jsonEncode(metadata)),
        contentKey,
      ),
      wrappedContentKeys: wrapped,
      files: uploads,
      contentKey: contentKeyBytes,
    );
  }

  Future<EncryptedFileUpload> recreateEncryptedFile({
    required String sourcePath,
    required String tempDirectory,
    required List<int> contentKey,
    required List<String> nonces,
  }) async {
    const plaintextChunkSize = 8 * 1024 * 1024;
    final source = File(sourcePath);
    final stat = await source.stat();
    if (stat.type != FileSystemEntityType.file) {
      throw const FileSystemException('來源檔案不存在');
    }
    await Directory(tempDirectory).create(recursive: true);
    final tempPath = path.join(
      tempDirectory,
      'waiting-${DateTime.now().microsecondsSinceEpoch}.nxd',
    );
    final input = await source.open();
    final output = await File(tempPath).open(mode: FileMode.write);
    final wholeDigest = _DigestSink();
    final wholeInput = hashes.sha256.startChunkedConversion(wholeDigest);
    final sourceDigest = _DigestSink();
    final sourceInput = hashes.sha256.startChunkedConversion(sourceDigest);
    final chunks = <EncryptedFileChunk>[];
    final key = SecretKey(contentKey);
    var encryptedSize = 0;
    var index = 0;
    Object? failure;
    StackTrace? failureStack;
    try {
      while (true) {
        final plaintext = await input.read(plaintextChunkSize);
        if (plaintext.isEmpty) break;
        if (index >= nonces.length) {
          throw const FormatException('等待 LAN 加密配方不完整');
        }
        sourceInput.add(plaintext);
        final nonce = base64Decode(nonces[index]);
        final encrypted = await _aes.encrypt(
          plaintext,
          secretKey: key,
          nonce: nonce,
        );
        final bytes = <int>[
          ...nonce,
          ...encrypted.cipherText,
          ...encrypted.mac.bytes,
        ];
        await output.writeFrom(bytes);
        wholeInput.add(bytes);
        encryptedSize += bytes.length;
        chunks.add(
          EncryptedFileChunk(
            size: bytes.length,
            sha256Hex: hashes.sha256.convert(bytes).toString(),
          ),
        );
        index++;
      }
      if (index != nonces.length) {
        throw const FormatException('來源檔案分段數已變更');
      }
    } catch (error, stack) {
      failure = error;
      failureStack = stack;
    } finally {
      await input.close();
      await output.close();
      wholeInput.close();
      sourceInput.close();
    }
    if (failure != null) {
      final temporary = File(tempPath);
      if (await temporary.exists()) await temporary.delete();
      Error.throwWithStackTrace(failure, failureStack!);
    }
    return EncryptedFileUpload(
      originalName: path.basename(sourcePath),
      originalSize: stat.size,
      originalModifiedAt: stat.modified,
      originalSha256: sourceDigest.value.toString(),
      tempPath: tempPath,
      size: encryptedSize,
      sha256: base64Encode(wholeDigest.value.bytes),
      chunks: chunks,
      nonces: nonces,
    );
  }

  Future<String> decryptText(
    String accountId,
    String content,
    String wrappedValue,
  ) async {
    final contentKey = await _unwrapKey(accountId, wrappedValue);
    return utf8.decode(await _decryptEnvelope(content, contentKey));
  }

  Future<FileChunkDecryptor> fileChunkDecryptor(
    String accountId,
    String wrappedValue,
  ) async => FileChunkDecryptor(await _unwrapKey(accountId, wrappedValue));

  Future<String> proveSession(
    String accountId,
    String ephemeralPublicKey,
    String nonce,
    String sessionId,
  ) async {
    final keyMaterial = await ensureDeviceKey(accountId);
    final pair = SimpleKeyPairData(
      keyMaterial.privateKey,
      publicKey: SimplePublicKey(
        keyMaterial.publicKey,
        type: KeyPairType.x25519,
      ),
      type: KeyPairType.x25519,
    );
    final shared = await _x25519.sharedSecretKey(
      keyPair: pair,
      remotePublicKey: SimplePublicKey(
        base64Decode(ephemeralPublicKey),
        type: KeyPairType.x25519,
      ),
    );
    final message = [
      ...utf8.encode('nexdrop/session-attach/v1$sessionId'),
      ...base64Decode(nonce),
    ];
    final mac = await Hmac.sha256().calculateMac(message, secretKey: shared);
    return base64Encode(mac.bytes);
  }

  Future<Map<String, dynamic>> _wrapKey(
    List<int> contentKey,
    List<int> recipientPublicKey,
  ) async {
    final ephemeral = await _x25519.newKeyPair();
    final shared = await _x25519.sharedSecretKey(
      keyPair: ephemeral,
      remotePublicKey: SimplePublicKey(
        recipientPublicKey,
        type: KeyPairType.x25519,
      ),
    );
    final wrappingKey = await _deriveWrappingKey(shared);
    final nonce = _aes.newNonce();
    final encrypted = await _aes.encrypt(
      contentKey,
      secretKey: wrappingKey,
      nonce: nonce,
    );
    return {
      'version': 1,
      'ephemeralPublicKey': base64Encode(
        (await ephemeral.extractPublicKey()).bytes,
      ),
      'nonce': base64Encode(nonce),
      'ciphertext': base64Encode([
        ...encrypted.cipherText,
        ...encrypted.mac.bytes,
      ]),
    };
  }

  Future<SecretKey> _unwrapKey(String accountId, String wrappedValue) async {
    final keyMaterial = await ensureDeviceKey(accountId);
    final wrapped =
        jsonDecode(utf8.decode(base64Decode(wrappedValue)))
            as Map<String, dynamic>;
    if (wrapped['version'] != 1) throw const FormatException('不支援的加密版本');
    final pair = SimpleKeyPairData(
      keyMaterial.privateKey,
      publicKey: SimplePublicKey(
        keyMaterial.publicKey,
        type: KeyPairType.x25519,
      ),
      type: KeyPairType.x25519,
    );
    final shared = await _x25519.sharedSecretKey(
      keyPair: pair,
      remotePublicKey: SimplePublicKey(
        base64Decode(wrapped['ephemeralPublicKey'] as String),
        type: KeyPairType.x25519,
      ),
    );
    final wrappingKey = await _deriveWrappingKey(shared);
    final encrypted = base64Decode(wrapped['ciphertext'] as String);
    if (encrypted.length < 16) throw const FormatException('無效的包裝金鑰');
    final plaintext = await _aes.decrypt(
      SecretBox(
        encrypted.sublist(0, encrypted.length - 16),
        nonce: base64Decode(wrapped['nonce'] as String),
        mac: Mac(encrypted.sublist(encrypted.length - 16)),
      ),
      secretKey: wrappingKey,
    );
    return SecretKey(plaintext);
  }

  Future<SecretKey> _deriveWrappingKey(SecretKey shared) => _hkdf.deriveKey(
    secretKey: shared,
    nonce: List<int>.filled(32, 0),
    info: utf8.encode('nexdrop/private-transfer/v1'),
  );

  Future<String> _encryptEnvelope(List<int> plaintext, SecretKey key) async {
    final nonce = _aes.newNonce();
    final encrypted = await _aes.encrypt(
      plaintext,
      secretKey: key,
      nonce: nonce,
    );
    return base64Encode(
      utf8.encode(
        jsonEncode({
          'version': 1,
          'nonce': base64Encode(nonce),
          'ciphertext': base64Encode([
            ...encrypted.cipherText,
            ...encrypted.mac.bytes,
          ]),
        }),
      ),
    );
  }

  Future<List<int>> _decryptEnvelope(String content, SecretKey key) async {
    final envelope =
        jsonDecode(utf8.decode(base64Decode(content))) as Map<String, dynamic>;
    if (envelope['version'] != 1) throw const FormatException('不支援的加密版本');
    final encrypted = base64Decode(envelope['ciphertext'] as String);
    if (encrypted.length < 16) throw const FormatException('無效的加密內容');
    return _aes.decrypt(
      SecretBox(
        encrypted.sublist(0, encrypted.length - 16),
        nonce: base64Decode(envelope['nonce'] as String),
        mac: Mac(encrypted.sublist(encrypted.length - 16)),
      ),
      secretKey: key,
    );
  }
}

class _DigestSink implements Sink<hashes.Digest> {
  hashes.Digest? _value;

  hashes.Digest get value => _value ?? hashes.sha256.convert(const []);

  @override
  void add(hashes.Digest data) => _value = data;

  @override
  void close() {}
}
