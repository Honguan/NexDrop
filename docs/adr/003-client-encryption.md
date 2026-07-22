# ADR-003: Client-side content encryption

[繁體中文](003-client-encryption.zh-TW.md)

Status: Accepted

Context: A self-hosted Node must not receive plaintext private or group content.

Decision: Encrypt content at the source with AES-256-GCM. Wrap the content key independently for every device through X25519 and HKDF.

Consequences: The Node stores only ciphertext. It cannot recover content after a device private key is lost.
