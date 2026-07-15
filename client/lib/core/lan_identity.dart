import 'dart:convert';
import 'dart:isolate';

import 'package:basic_utils/basic_utils.dart';
import 'package:crypto/crypto.dart';
import 'package:flutter_secure_storage/flutter_secure_storage.dart';

class LanIdentity {
  const LanIdentity({
    required this.shortDeviceId,
    required this.certificate,
    required this.privateKey,
    required this.fingerprint,
  });

  final String shortDeviceId;
  final String certificate;
  final String privateKey;
  final String fingerprint;
}

class LanIdentityStore {
  LanIdentityStore([FlutterSecureStorage? storage])
    : _storage = storage ?? const FlutterSecureStorage();

  final FlutterSecureStorage _storage;

  Future<LanIdentity> ensure(String deviceId) async {
    final shortId = deviceId.replaceAll('-', '').substring(0, 12);
    final certificateKey = 'nexdrop.lan.certificate.$deviceId';
    final privateKeyKey = 'nexdrop.lan.private.$deviceId';
    final existingCertificate = await _storage.read(key: certificateKey);
    final existingPrivateKey = await _storage.read(key: privateKeyKey);
    if (existingCertificate != null && existingPrivateKey != null) {
      return LanIdentity(
        shortDeviceId: shortId,
        certificate: existingCertificate,
        privateKey: existingPrivateKey,
        fingerprint: certificateFingerprint(existingCertificate),
      );
    }
    final generated = await Isolate.run(() => _generate(shortId));
    await Future.wait([
      _storage.write(key: certificateKey, value: generated.certificate),
      _storage.write(key: privateKeyKey, value: generated.privateKey),
    ]);
    return generated;
  }

  static LanIdentity _generate(String shortId) {
    final pair = CryptoUtils.generateRSAKeyPair(keySize: 2048);
    final privateKey = pair.privateKey as RSAPrivateKey;
    final publicKey = pair.publicKey as RSAPublicKey;
    final csr = X509Utils.generateRsaCsrPem(
      {'CN': 'nexdrop:$shortId', 'O': 'NexDrop'},
      privateKey,
      publicKey,
      san: ['nexdrop-$shortId.local'],
    );
    final certificate = X509Utils.generateSelfSignedCertificate(
      privateKey,
      csr,
      1825,
      sans: ['nexdrop-$shortId.local'],
      keyUsage: [KeyUsage.DIGITAL_SIGNATURE, KeyUsage.KEY_ENCIPHERMENT],
      extKeyUsage: [ExtendedKeyUsage.SERVER_AUTH, ExtendedKeyUsage.CLIENT_AUTH],
      serialNumber: DateTime.now().microsecondsSinceEpoch.toString(),
    );
    return LanIdentity(
      shortDeviceId: shortId,
      certificate: certificate,
      privateKey: CryptoUtils.encodeRSAPrivateKeyToPem(privateKey),
      fingerprint: certificateFingerprint(certificate),
    );
  }
}

String certificateFingerprint(String certificate) =>
    sha256.convert(_certificateDer(certificate)).toString();

List<int> _certificateDer(String certificate) {
  final content = certificate
      .replaceAll('-----BEGIN CERTIFICATE-----', '')
      .replaceAll('-----END CERTIFICATE-----', '')
      .replaceAll(RegExp(r'\s'), '');
  return base64Decode(content);
}
