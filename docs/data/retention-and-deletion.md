# Retention and deletion

Message metadata and attachment bodies have independent policies.

## Message policies

- permanent
- fixed number of days
- global deletion through an authorized tombstone
- local removal, which is never propagated

Pinned messages are exempt from automatic message deletion until unpinned. Reply and pin references do not preserve deleted plaintext.

## Attachment policies

- permanent
- delete Node body after every target has verified download
- delete body at content expiration
- fixed number of days

Deleting a body also removes obsolete wrapped content keys and cached decrypted copies controlled by NexDrop. It cannot remove files exported outside NexDrop.

## Cleanup execution

Cleanup jobs use bounded batches, leases, and persistent execution history. They can restart safely and must not block text delivery or active file transfers. Before deleting a body, verify that no active upload, download, recovery, or relay assignment still references it.

## Tombstones

A global tombstone contains version, message identifier, scope, actor, issued time, expiration, and signature. Authorization is checked before creation. Tombstones outlive the deleted body long enough for offline devices to converge, then expire according to policy.

## Capacity planning

Operators should track encrypted body bytes, manifest bytes, active relay bytes, tombstone count, local-index size, cleanup backlog, and oldest pending deletion. Retention changes apply prospectively unless an explicit migration is run.
