# NexDrop 2.2.0

[Traditional Chinese](release-notes-v2.2.0.zh-TW.md)

NexDrop 2.2.0 adds privacy-safe transfer observability and repeatable recovery diagnostics.

## Highlights

- Inspect every authorized transfer through a content-free, ordered timeline with stable event codes and file, target, route, execution, and error correlation.
- Create a read-only, automatically redacted support bundle with `./deploy/nexdrop diagnostics --output diagnostics.zip`.
- Correlate HTTP, WebSocket, storage, and cleanup activity using request and transfer identifiers without logging message or file content.
- Use bounded operational metric labels, documented initial SLOs, stable error-code runbooks, and isolated failure-injection scenarios.
- Load verification reports now include product version, build commit, environment, success rate, and latency percentiles.

## Upgrade

```bash
./deploy/nexdrop update 2.2.0
```

The update preserves `.env`, PostgreSQL data, file data, and existing secrets, and creates a backup before switching images.

If the release uses a temporary Android certificate, a device with an older APK can require uninstalling it before installation. Production deployments should configure persistent Android signing secrets. When no Windows certificate is provided, the EXE and ZIP remain usable but Windows can display a SmartScreen warning.
