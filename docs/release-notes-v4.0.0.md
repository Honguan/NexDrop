# NexDrop 4.0.0

[Traditional Chinese](release-notes-v4.0.0.zh-TW.md)

Adaptive routing, recovery, secure enrollment, relay pools, folders, and message lifecycle

## Upgrade

```bash
./deploy/nexdrop update 4.0.0
```

The update preserves `.env`, PostgreSQL data, file data, and existing secrets, and creates a backup before switching images.

If the release uses a temporary Android certificate, a device with an older APK can require uninstalling it before installation. Production deployments should configure persistent Android signing secrets. When no Windows certificate is provided, the EXE and ZIP remain usable but Windows can display a SmartScreen warning.

