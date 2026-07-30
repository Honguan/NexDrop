# Transfer event codes

[繁體中文](transfer-events.zh-TW.md)

`GET /api/transfers/{id}/timeline` returns events ordered by UTC occurrence time and a monotonic sequence. Authorization matches the transfer resource. Events contain stable codes and request, transfer, file, target, execution, route, status, error, and measured-duration correlation fields; they never contain message content, filenames, encryption material, or user files.

First-party clients report the client-observed phases below with `POST /api/transfers/{id}/timeline` and a UUID `Idempotency-Key`. Replaying the same key and body returns the original event; reusing the key for different content returns `IDEMPOTENCY_CONFLICT`. Server-owned lifecycle events cannot be submitted by clients.

| Code | Meaning | Operator action |
| --- | --- | --- |
| `TASK_CREATED` | The transfer transaction was created | Confirm targets and the next route event |
| `TARGET_RESOLVED` | A requested target was authorized and resolved | Confirm the target and selected route |
| `ROUTE_CHECKING` | Route evaluation started | Check target presence and Node readiness |
| `TARGET_WAITING` | A target, LAN, or Node dependency is unavailable | Check presence, firewall, DNS, and Node health |
| `TARGET_QUEUED` | Work is queued | Check queue growth and worker health |
| `NODE_UPLOAD_STARTED` | Sender started Node upload | Check storage capacity and write latency |
| `NODE_FILE_AVAILABLE` | Node upload completed | Check receiver presence and download authorization |
| `NODE_DOWNLOAD_STARTED` | Receiver started Node download | Check receiver network and storage |
| `LAN_TRANSFER_STARTED` | Direct LAN transfer started | Check TLS identity and local connectivity |
| `TRANSFER_PAUSED` | The current execution paused | Preserve the execution and resume with the same transfer |
| `FILE_HASH_VERIFYING` | Complete-file hash verification started | Wait for verification; investigate checksum failures |
| `FILE_HASH_VERIFIED` | Complete-file hash verification succeeded | Continue delivery; no repair is required |
| `TARGET_DELIVERED` | Target acknowledged delivery | No action unless acknowledgement latency missed its SLO |
| `TARGET_READ` | Target marked content read | No action |
| `TARGET_FAILED` | Current execution failed | Follow its `errorCode`, then retry with a new execution |
| `TARGET_CANCELLED` | Sender cancelled unfinished targets | No automatic restart |
| `TARGET_EXPIRED` | Retention expired | Create a new transfer if content is still needed |
| `SOURCE_FILE_MISSING` | Sender source is missing | Restore the source, then explicitly retry |
| `SOURCE_FILE_CHANGED` | Sender source changed | Verify the new source, then create or explicitly retry |
| `ROUTE_MIGRATED` | Execution changed route | Inspect the preceding route or handshake failure |
| `RETRY_STARTED` | A new idempotent execution attempt started | Confirm only one execution exists for the retry key |
| `ROUTE_CANDIDATES_DISCOVERED` | Client completed route discovery | Compare discovered availability with the selected route |
| `DIRECT_CONNECTION_ATTEMPTED` | Client attempted a direct connection | Check LAN reachability when no TLS event follows |
| `TLS_AUTHENTICATION_COMPLETED` | Mutual TLS authentication succeeded | Continue the direct transfer |
| `ROUTE_FALLBACK_SELECTED` | Client selected a fallback route | Inspect the preceding route or connection error |
| `ENCRYPTION_PREPARED` | Client prepared encrypted content and wrapped keys | Continue without logging key material |
| `CHUNK_UPLOAD_STARTED` | Client started a Node chunk upload | Check Node storage and request correlation |
| `CHUNK_DOWNLOAD_STARTED` | Client started a Node chunk download | Check receiver network and authorization |
| `CHUNK_RETRY_STARTED` | Client retried a chunk operation | Inspect the stable error code and retry rate |
| `WEBSOCKET_INTERRUPTED` | Realtime connection was interrupted | Check network and reconnect metrics |
| `RECEIVER_BACKGROUND_RESTRICTED` | Receiver reported background execution limits | Bring the receiver foreground or adjust OS settings |
| `NETWORK_INTERFACE_CHANGED` | Client network interface changed | Rediscover routes before resuming |

See [troubleshooting](../troubleshooting.md) for stable error-code runbooks.
