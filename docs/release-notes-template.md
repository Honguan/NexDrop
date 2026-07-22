# NexDrop {{VERSION}}

[繁體中文](release-notes-v{{VERSION}}.zh-TW.md)

{{SUMMARY}}

## Upgrade

```bash
./deploy/nexdrop update {{VERSION}}
```

The update preserves `.env`, PostgreSQL data, file data, and existing secrets, and creates a backup before switching images.

If the release uses a temporary Android certificate, a device with an older APK can require uninstalling it before installation. Production deployments should configure persistent Android signing secrets. When no Windows certificate is provided, the EXE and ZIP remain usable but Windows can display a SmartScreen warning.
