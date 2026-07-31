# Relay pool protocol

## Registration and health

An administrator registers a relay endpoint, region, capacity, and pinned identity public key. Relays report bounded health and capacity data. The primary Node marks stale relays unhealthy and excludes unhealthy, draining, or full relays from new assignments.

## Grant format

A signed relay grant binds:

- relay identifier
- transfer and file identifiers
- permitted operation: upload, download, or delete
- maximum byte count
- expiration and nonce

A grant is rejected when used on another relay, transfer, file, operation, or size. Grants never carry plaintext keys or permanent device credentials.

## Assignment and fallback

Selection is deterministic for equal measurements. Capacity ratio, recent failure rate, latency, and region preference influence ranking. On failure, the client obtains a new authorized assignment and reuses chunks whose hashes match the transfer manifest.

## Cleanup

Assignments expire independently from stored ciphertext. Cleanup first confirms that no active transfer or valid grant references a chunk, then removes orphaned data in bounded batches. Relay restart must not invalidate verified chunks already on durable storage.

## Metrics

Expose relay availability, usable capacity, active transfers, throughput, authorization failures, assignment failures, drain progress, orphan count, and cleanup results without endpoint secrets or content metadata.
