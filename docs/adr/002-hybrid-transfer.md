# ADR-002: LAN-first hybrid routing

[繁體中文](002-hybrid-transfer.zh-TW.md)

Status: Accepted

Context: Devices on the same network should avoid Node relay, while offline and cross-network delivery must remain reliable.

Decision: Prefer direct TLS LAN transfer, then fall back to Node storage. Large files can wait for LAN availability.

Consequences: Clients maintain discovery, source-file, and retry state. Each target can use a different route.
