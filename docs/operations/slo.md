# Service-level objectives

[繁體中文](slo.zh-TW.md)

These initial objectives apply to a healthy, supported NexDrop Node and exclude planned maintenance. Measurements use bounded labels from the [observability contract](transfer-events.md); raw resource identifiers are never metric labels.

| Objective | Target | Measurement |
| --- | --- | --- |
| API availability | 99.9% per rolling 30 days | Successful non-administration API responses, excluding valid 4xx requests |
| Ordinary API latency | p95 below 500 ms | The published 100-device, 50-online-device, 10-transfer load scenario |
| Online text delivery | 99% within 30 seconds | `TASK_CREATED` to `TARGET_DELIVERED` while the target remains online |
| Resumability | 99% without duplicate completion | Interrupted eligible transfers that resume or safely restart |
| State consistency | 99.99% | Transfers whose task, target, execution, and delivery states agree |

`GET /metrics` exposes the Prometheus text format. The operational metric registry uses only `route`, `status`, `error_code`, `worker`, `operation`, and `result`; values outside the documented allowlists are rejected or normalized to `OTHER`/`UNKNOWN`. Current metrics cover API outcomes and latency, route selection, direct-handshake latency and failure reason, WebSocket connection events and heartbeat gaps, chunk retries, checksum and storage rejection outcomes, delivery latency, route throughput, database and storage latency, worker runs and recovery outcomes, transfer-state events, and current queued, stalled, delivered-but-unacknowledged, and dead-letter counts.

Investigate availability or consistency immediately when the objective is missed. For latency and resumability, inspect the transfer timeline, generate a [diagnostic bundle](diagnostics.md), and follow the stable-code actions in [troubleshooting](../troubleshooting.md).
