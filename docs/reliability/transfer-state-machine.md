# Transfer recovery state machine

Recovery operates per transfer target. A failure for one receiving device never blocks completed or active work for another device.

```text
QUEUED -> ACTIVE -> VERIFYING -> DELIVERED -> READ
   |        |           |
   |        +-> PAUSED -+
   |        +-> WAITING_FOR_RECEIVER
   |        +-> RECONCILE
   |        +-> RETRY_BACKOFF
   |        +-> DEAD_LETTER
   +-> CANCELLED / EXPIRED
```

## Invariants

1. Verified chunks are immutable and reusable.
2. A completed target is never restarted by a recovery scan.
3. Every retry creates or preserves an execution attempt; history is not overwritten.
4. Leases prevent two coordinators from changing the same target concurrently.
5. Recovery is safe to repeat after a process or database restart.
6. Terminal states require an explicit operator action before retry.

## Error classes

- immediate retry: stale lock
- backoff retry: database, network, timeout, or Node restart
- waiting: receiving device offline
- reconciliation: missing chunk, stale assembly, or lost acknowledgement
- terminal: invalid manifest, hash mismatch, permission denial, revoked device, or exhausted quota

The persisted queue stores classification, retry count, next attempt, lease expiration, stable error code, recovery reason, and dead-letter time.
