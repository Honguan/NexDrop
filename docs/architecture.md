# System architecture

[繁體中文](architecture.zh-TW.md)

NexDrop is a modular monolith. `cmd/nexdrop` assembles the auth, device, group, transfer, filetransfer, presence, analytics, admin, maintenance, and monitoring modules from `internal/`. Modules access PostgreSQL through explicit Store interfaces.

```text
Windows / Android / Web / Extension
             │ HTTPS + WebSocket
             ▼
      NexDrop Node + Caddy ── PostgreSQL
             │
             └── Encrypted file-chunk storage

Device A ◄──────── Direct TLS LAN transfer ────────► Device B
```

Route selection prefers LAN and falls back to the Node when peers cannot connect directly. Clients encrypt message and file content, so the Node stores only ciphertext and per-device wrapped keys. Background workers must be re-entrant, and database transactions are the single source of truth for task and transfer state.
