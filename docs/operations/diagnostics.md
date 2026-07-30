# Diagnostic bundles

[繁體中文](diagnostics.zh-TW.md)

Create a privacy-safe support bundle from the repository directory:

```sh
./deploy/nexdrop diagnostics --output diagnostics.zip
```

The command uses administrator access when Docker requires it, writes the ZIP with owner-only permissions, and performs read-only inspection. It does not repair configuration, create database records, or change transfer state.

The archive contains a schema-versioned manifest, product and commit versions, runtime information, health checks, and a redacted effective configuration. Database URLs, bearer tokens, passwords, tokens, secrets, credentials, and keys are replaced with `[REDACTED]`. Message content, plaintext filenames, decrypted indexes, user files, private keys, Node keys, and TOTP secrets are not collected.

Before sharing a bundle:

1. Keep the archive encrypted in transit.
2. Confirm that its manifest identifies the expected version and commit.
3. Attach only the bundle, not `.env`, database dumps, storage directories, or complete container logs.
4. Delete local support copies after the incident retention period.

The redaction boundary is specified in [diagnostic privacy](diagnostics-privacy.md). Stable transfer phases are described in [transfer events](transfer-events.md).
