# NexDrop 2.1.0

[Traditional Chinese](release-notes-v2.1.0.zh-TW.md)

NexDrop 2.1.0 adds explicit capability negotiation and safer mixed-version operation across the Node, Windows, Android, Web, Chrome, and Edge clients.

## Highlights

- The version endpoint now publishes a stable Node identity, capability schema, limits, and version fingerprint.
- HTTP, WebSocket, and LAN sessions enable optional behavior only after mutual capability negotiation.
- Current clients safely downgrade to protocol 1.1 or 1.0 when connecting to an older Node.
- LAN discovery keeps a compatible 1.1 baseline while authenticated transfers negotiate protocol 1.2 features.
- Resumable chunks require support from both transfer endpoints; otherwise the transfer restarts safely.
- Compatibility errors identify the missing capability and whether the Node or target device requires an update.
- CI now detects stale compatibility documentation when the production protocol contract changes.

## Upgrade

```bash
./deploy/nexdrop update 2.1.0
```

The update preserves `.env`, PostgreSQL data, file data, existing secrets, and `NEXDROP_NODE_ID`, and creates a backup before switching images.

If the release uses a temporary Android certificate, a device with an older APK can require uninstalling it before installation. Production deployments should configure persistent Android signing secrets. When no Windows certificate is provided, the EXE and ZIP remain usable but Windows can display a SmartScreen warning.
