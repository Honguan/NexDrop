# NexDrop 2.0.4

[繁體中文](release-notes-v2.0.4.zh-TW.md)

Improves bilingual documentation, API and realtime boundaries, extension productivity, Flutter send and connection reliability, deployment diagnostics, and Go 1.26 security automation.

## Highlights

- The browser extension preserves popup drafts, shows a character count, supports `Ctrl/Command + Enter`, and restores the send action cleanly after runtime errors.
- Web and Flutter realtime connections now own heartbeat, notification acknowledgement, malformed-message isolation, reconnection, and shutdown behavior behind focused interfaces.
- Go API request parsing, response rendering, versioned errors, and rate limiting have clearer module boundaries; limiter retry timing now uses the injected clock consistently.
- Current project, component, architecture, operations, security, and release documentation is English-first with a complete Traditional Chinese counterpart.
- Installation diagnostics can display saved bootstrap credentials without exposing them in routine status output, and the update flow preserves secrets and PostgreSQL state.
- Security CI now uses a Go 1.26-compatible vulnerability analyzer and an explicit permissive-license allowlist.

## Upgrade

```bash
./deploy/nexdrop update 2.0.4
```

The update preserves `.env`, PostgreSQL data, file data, and existing secrets, and creates a backup before switching images.

If the release uses a temporary Android certificate, a device with an older APK can require uninstalling it before installation. Production deployments should configure persistent Android signing secrets. When no Windows certificate is provided, the EXE and ZIP remain usable but Windows can display a SmartScreen warning.
