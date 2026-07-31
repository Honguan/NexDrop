# HTTP interface v3 integrations

The v3 integration endpoints extend the stable v1 HTTP contract. They use the same bearer access token, media type, structured errors, capability negotiation, and `Idempotency-Key` rules as the existing API.

## Credential boundary

Routine API, WebSocket, and transfer requests never require or transmit the Node key.

The Node key is accepted only by the enrollment bootstrap and grant-issuance endpoints. A successful bootstrap exchanges it for a unique device credential. First-party clients delete the Node key after this exchange. The device credential is used only to attach an authenticated user session to a device; normal requests continue using the short-lived access token.

## Enrollment

- `POST /api/v3/enrollment/bootstrap`
- `POST /api/v3/enrollment/grants`
- `POST /api/v3/enrollment/redeem`
- `POST /api/v3/enrollment/attach-session`
- `POST /api/v3/enrollment/grants/{grant-id}/revoke`
- `POST /api/v3/devices/{device-id}/credential/revoke`

Bootstrap and grant issuance require `X-NexDrop-Node-Key`. Redemption is rate-limited and atomic: token validation, use-count consumption, device creation, X25519 public-key registration, and credential creation commit in one PostgreSQL transaction.

## Adaptive routing and transfer profile

- `POST /api/v3/routes/plan`
- `POST /api/v3/routes/observations`
- `POST /api/v3/transfers/{transfer-id}/route`
- `POST /api/v3/transfers/{transfer-id}/profile`

Route planning accepts authenticated direct, VPN, IPv4, IPv6, and Node candidates. The response includes deterministic scores and start delays. Direct candidates receive a configurable head start; Node fallback starts without waiting for the full direct timeout. Route switching updates the same transfer and records verified chunk hashes, so a fallback does not create a replacement transfer.

The adaptive profile endpoint returns bounded chunk size and parallelism based on RTT, throughput, retries, checksum failures, receiver memory, backpressure, storage, battery, thermal, and background state.

## Offline delivery

- `PUT /api/v3/transfers/{transfer-id}/targets/{device-id}/delivery-policy`
- `GET /api/v3/transfers/{transfer-id}/targets/{device-id}/delivery-policy`
- `POST /api/v3/transfers/{transfer-id}/targets/{device-id}/delivery-evaluate`
- `GET /api/v3/devices/{device-id}/delivery-queue`

Policies cover expiration, Wi-Fi-only, charging-only, mobile-data limits, automatic download, and foreground-only behavior. Reconnecting devices receive metadata and high-priority small items before deferred large bodies.

## Relay pool

- `POST /api/v3/relays`
- `GET /api/v3/relays`
- `POST /api/v3/relays/{relay-id}/heartbeat`
- `POST /api/v3/relays/{relay-id}/drain`
- `DELETE /api/v3/relays/{relay-id}`
- `POST /api/v3/relay-assignments`

Registration, listing, drain, and removal require a recently verified administrator session. Relay credentials are independent from Node, device, administrator, and database credentials. Assignment returns a short-lived grant bound to relay, transfer, file, operation, byte limit, and expiration.

## Folder manifests

- `PUT /api/v3/transfers/{transfer-id}/folder-manifest`
- `GET /api/v3/transfers/{transfer-id}/folder-manifest`
- `PUT /api/v3/transfers/{transfer-id}/folder-selection/{device-id}`
- `GET /api/v3/transfers/{transfer-id}/folder-selection/{device-id}`

The sender uploads a validated versioned manifest. Each receiver stores an independent accepted-entry and conflict-action selection. Paths are normalized and checked for traversal, absolute paths, drive prefixes, NUL bytes, reserved names, case collisions, depth, length, entry count, and manifest size before persistence.

## Message lifecycle

- `GET /api/v3/messages?conversation=inbox&limit=50&cursor=...`
- `PUT /api/v3/messages/read`
- `DELETE /api/v3/messages/{message-id}?scope=LOCAL|ATTACHMENT_BODY|EVERYWHERE`
- `PUT /api/v3/retention/{conversation}`
- `POST /api/v3/retention/run`

History uses signed stable cursors and never exposes plaintext content. Read cursors are independent per device. Local removal affects only the requesting device. Global deletion creates a signed, versioned tombstone for offline reconciliation. Attachment and message retention are evaluated independently by a bounded restart-safe cleanup worker.

## Recovery and dead-letter operations

- `GET /api/v3/recovery/failed`
- `GET /api/v3/recovery/transfers/{transfer-id}`
- `POST /api/v3/recovery/transfers/{transfer-id}/retry`
- `POST /api/v3/recovery/run`

These endpoints require a recently verified administrator session. Recovery uses PostgreSQL leases, bounded exponential backoff, per-target retry state, filesystem reconciliation, append-only timeline events, and stable terminal error codes.

Equivalent deployment commands are:

```text
./deploy/nexdrop transfers failed
./deploy/nexdrop transfers inspect <transfer-id>
./deploy/nexdrop transfers retry <transfer-id>
./deploy/nexdrop transfers reconcile
```
