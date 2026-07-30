# NexDrop 2.1.0

[Traditional Chinese](release-notes-v2.1.0.zh-TW.md)

NexDrop 2.1.0 introduces protocol 1.2 capability negotiation. Nodes and first-party clients now select a common protocol generation, enable optional behavior only after capability negotiation, and fall back safely with older 1.1 and 1.0 generations.

The version endpoint publishes a stable Node identity, capability registry, numeric limits, and version fingerprint. Release validation keeps the registry, client declarations, documentation, and compatibility matrix synchronized.

## Upgrade

```bash
./deploy/nexdrop update 2.1.0
```

The update preserves `.env`, PostgreSQL data, file data, existing secrets, and the generated `NEXDROP_NODE_ID`. A backup is created before switching images.
