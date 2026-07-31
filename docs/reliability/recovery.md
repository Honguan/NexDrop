# Transfer recovery

The recovery coordinator runs at Node startup, on a bounded periodic schedule, and on explicit operator request. Each pass is re-entrant and processes only due, non-terminal targets for which it can obtain a lease.

## Policy

- base backoff: 5 seconds
- maximum backoff: 30 minutes
- maximum automatic attempts: 8
- default lease: 2 minutes
- deterministic jitter is derived from transfer and target identifiers

## Reconciliation

For ambiguous work, compare PostgreSQL state, recorded chunk hashes, storage objects, assembled-file hashes, and receiver acknowledgements. Missing or mismatched chunks are marked for retransmission. Valid completed chunks are retained.

## Dead-letter handling

A target enters dead letter when retries are exhausted or the error is non-retryable. Operators must be able to inspect the transfer, stable error code, last attempt, and recovery guidance before retrying or cancelling it.

Recommended commands:

```text
./deploy/nexdrop transfers failed
./deploy/nexdrop transfers inspect <transfer-id>
./deploy/nexdrop transfers retry <transfer-id>
./deploy/nexdrop transfers reconcile
```

## Operational checks

Test Node restart during upload and download, database interruption, receiver offline, missing chunk, failed assembly, duplicate recovery execution, and storage-full behavior. Text delivery must remain available when file storage is unavailable.
