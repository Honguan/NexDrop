# Capability negotiation

[繁體中文](capability-negotiation.zh-TW.md)

NexDrop uses protocol versions to reject incompatible wire formats and capabilities to enable optional behavior. Product versions do not decide feature support.

## Node document

`GET /api/version` returns the existing product and protocol fields plus:

```json
{
  "nodeIdentity": "node-<opaque fingerprint>",
  "capabilitySchemaVersion": 1,
  "versionFingerprint": "<sha256>",
  "capabilities": ["capability_negotiation", "structured_errors"],
  "limits": {
    "maxChunkSize": 8388608,
    "maxParallelChunks": 6,
    "maxRecipients": 100
  }
}
```

Clients scope cached data to `nodeIdentity`. They refresh the document when a session starts and replace the cache when `versionFingerprint` changes. A legacy response without these fields means that no optional capability is available.

Unknown additive fields and unknown capability identifiers must be ignored. Numeric limits are independent from boolean capabilities and clients must stay at or below every limit advertised by all required parties.

## Session and LAN negotiation

Current clients advertise a comma-separated capability list in the WebSocket `capabilities` query parameter. The `connected` message returns `negotiatedCapabilities`, which is the intersection with the Node registry.

LAN clients advertise the same list in `X-NexDrop-Capabilities`. The receiver returns the negotiated list and its limits in the resumable-transfer status response. A feature is enabled only when every party listed below advertises it.

| Capability | Required parties | Safe fallback |
| --- | --- | --- |
| `capability_negotiation` | Node, client | Use protocol and minimum-client version checks. |
| `structured_errors` | Node, client | Parse the legacy string error envelope. |
| `cursor_pagination` | Node, client | Use the legacy list response. |
| `idempotency_replay` | Node, client | Do not automatically retry a non-idempotent request. |
| `resumable_chunks` | Sender and receiver (the Node is the receiver for Node-routed uploads) | Restart the file from the first chunk. |
| `realtime_versions` | Node, client | Refresh the HTTP version document periodically. |
| `adaptive_route_racing` | Sender and receiver | Use sequential LAN-first routing and Node fallback. |
| `adaptive_transfer_profile` | Sender and receiver | Use the fixed negotiated chunk size and one transfer stream. |
| `transfer_recovery` | Node | Use manual per-target retry and startup cleanup. |
| `scoped_device_enrollment` | Node, client | Use legacy Node-key device creation during the migration window. |
| `offline_delivery_policies` | Node, client | Queue content without platform-aware background constraints. |
| `relay_pool` | Node, client | Use the primary Node as the only relay path. |
| `folder_manifest` | Sender and receiver | Return unsupported-feature or use an explicitly requested archive. |
| `message_lifecycle` | Node, client | Use existing history, local hide, and fixed retention behavior. |

When a requested behavior has no safe fallback, the initiating side returns or displays `CAPABILITY_UNAVAILABLE` and identifies the required capability in structured details.

## Lifecycle rules

- Identifiers are lowercase semantic names and are never reused for another behavior.
- Additions are backward compatible; removals require a documented deprecation period.
- Security-critical incompatibilities continue to use `minimumClientVersion`.
- Protocol-affecting changes must update the [compatibility matrix](../compatibility-matrix.md), this registry table, contract fixtures, and mixed-version tests.
