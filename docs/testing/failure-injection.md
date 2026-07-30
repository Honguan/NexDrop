# Failure injection

[繁體中文](failure-injection.zh-TW.md)

Failure injection is allowed only in a disposable environment. The script refuses to run unless `NEXDROP_INTEGRATION_DISPOSABLE=true`.

With an isolated Node and PostgreSQL container already running:

```sh
export NEXDROP_INTEGRATION_DISPOSABLE=true
export NEXDROP_POSTGRES_CONTAINER="$(docker compose ps -q postgres)"
./scripts/failure-injection.sh database-outage
```

Run `./scripts/failure-injection.sh deterministic` first to execute the storage reset, WebSocket interruption/reconnect, state transition, and replay tests. Set `NEXDROP_TEST_DATABASE_URL` to include the concurrent PostgreSQL retry-replay test; the script reports when that test is skipped. The database scenario stops PostgreSQL, verifies `/readyz` returns 503, restarts PostgreSQL, and waits for readiness recovery. The integration workflow runs both modes in an isolated service container. It separately interrupts an active transfer across a Node process restart and verifies that completed chunks, idempotency keys, and stored results are not duplicated.

Automated deterministic coverage includes connection resets, controlled latency, storage rejection and slowdown, WebSocket reconnects, capability fallback, client constraint signals, retry replay, database unavailability, and Node restart recovery. Real packet shaping, filesystem exhaustion, Android Doze controls, and physical interface changes are optional device-lab extensions; never run destructive controls against a production Node or persistent volume.

| Mode | Repeatable injected condition and assertion |
| --- | --- |
| `network-faults` | Upload/download reset, delayed delivery/reconnect, and LAN-unavailable IPv4/IPv6 route fallback |
| `storage-faults` | Slow durable write, rejected write cleanup, checksum rejection, quota and storage-full refusal |
| `client-constraints` | Receiver background restriction and network-interface change events remain content-free and ordered |
| `mixed-clients` | Supported/unsupported protocol and legacy/new structured-error fallback |
| `worker-recovery` | Cleanup re-entry, WebSocket reconnect, concurrent retry replay, and one durable execution |
| `database-outage` | Readiness fails closed during PostgreSQL outage and recovers after restart |
| Integration workflow restart barrier | An active transfer crosses a Node restart without duplicate chunks or completion |

The network modes use deterministic in-process reset and route-availability controls rather than privileged packet shaping, so they run identically on a developer machine and an isolated CI runner. Android Doze/background and interface-change inputs are represented by their stable client-observed events; device-lab runs may additionally exercise the real operating-system controls.

Every added scenario must define its fault point, expected state transition, cleanup, retry key, and an assertion that one logical request produces at most one durable result.
