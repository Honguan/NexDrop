# Security design

[繁體中文](security.zh-TW.md)

- Passwords are stored with bcrypt. Access tokens are short-lived; refresh tokens are revocable and rotate on every refresh.
- Administrative actions require an administrator identity, recent password verification, and TOTP.
- Login, pairing, and administration endpoints use fixed-window limits. Exceeded limits return HTTP 429 and `Retry-After`.
- Content uses AES-256-GCM, with the content key wrapped independently for every receiving device through X25519 and HKDF.
- The Node revalidates ownership, quotas, filenames, and file paths. It never trusts client-supplied role fields.
- JSON logs contain only UTC time, level, module, request or transfer ID, status, and error code. They exclude passwords, tokens, private keys, and content.
- Release artifacts must pass CodeQL, dependency scans, checksum verification, attestations, and applicable signature checks. Security CI uses a Go 1.26-compatible vulnerability analyzer and an explicit permissive-license allowlist.

See the root [SECURITY policy](../SECURITY.md) for vulnerability reporting. Node private keys, device private keys, keystores, PFX files, database backups, and `.env` must never enter workflow artifacts or caches.
