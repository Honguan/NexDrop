# HTTP interface v1

[繁體中文](api.zh-TW.md)

The base path is `/api`. Times use UTC RFC 3339 and resource IDs use UUIDs. Every response includes:

```text
X-Request-ID: <UUID>
X-NexDrop-API-Version: 1
```

First-party clients send `Accept: application/vnd.nexdrop.v1+json`. Versioned errors use:

```json
{"error":{"code":"INVALID_TOKEN","message":"...","request_id":"...","details":{}}}
```

Legacy 1.x clients that do not negotiate the media type continue to receive `{"error":"INVALID_TOKEN"}`.

When the Node returns `RATE_LIMITED`, versioned clients wait for the `Retry-After` response header instead of immediately resending the request.

Creating a transfer, uploading a chunk, completing a file, synchronizing metrics, reporting progress, and marking content read require `Idempotency-Key: <UUID>`. Reusing a key with the same request replays the original result. Reusing it with different content returns `IDEMPOTENCY_CONFLICT`.

Cursor-based lists use:

```json
{"items":[],"nextCursor":"<opaque signed cursor>"}
```

The HMAC-protected cursor binds a UTC creation time to a stable UUID ordering key. Clients must neither parse nor modify it. An invalid signature returns `INVALID_PAGE`. The administration failure list applies `status` to target state; the audit list applies it to the audit action.

Primary resources include auth, account, devices, groups, transfers, files, metrics, statistics, and admin. `GET /api/version` returns product, interface, protocol, minimum-client, and build-commit versions.

## Device state and trust

When an authenticated account calls `POST /api/devices` against its current Linux Node, the Node creates that device as `TRUSTED` and links the current session to it. `PENDING`, pairing codes, and approval endpoints remain for existing pending records and 1.x compatibility. First-party clients do not request another pairing code on the same Node.

Alongside existing fields, `GET /api/devices` returns `online` and an optional `lastSeenAt`. `online` means the device still has an open connection and sent a heartbeat within the previous 45 seconds. Clients use this value for live presence and do not infer presence from `trustStatus`.

`GET /api/statistics/devices` returns `deviceType`, `trustStatus`, `online`, optional `lastSeenAt`, sent and received counts, byte totals, and average rates for each device. `GET /api/statistics/node` still accepts `from` and `to`; first-party views query only the latest two minutes and display the newest Node sample. Node resources are sampled every five seconds and high-frequency samples are retained for 31 days.

Group endpoints remain available in 1.x for older clients, but current first-party Web, Windows, Android, and extension clients no longer expose group-transfer entry points.
