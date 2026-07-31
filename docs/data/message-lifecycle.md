# Message lifecycle

## History loading

History uses a signed cursor containing `(created_at, message_id)` and stable descending ordering. New messages do not move previously returned items between pages. Clients request bounded pages and never load an entire large conversation into memory.

## Search

Full-text indexes remain on clients because message content is end-to-end encrypted. Local indexes may include decrypted text, filename, sender device, target device, content type, date, delivery state, and read state. Encrypt indexes at rest when platform facilities permit.

## Read state

Each device maintains a monotonic read cursor. Updates are idempotent: older or equal cursors cannot move state backward. Presence and read state remain independent.

## Delete scopes

- local only: remove the local cached item; other devices and Node state are unchanged
- attachment body only: delete downloaded or Node-hosted body while retaining minimal message metadata
- all devices: publish an authorized signed versioned tombstone

Offline devices apply valid tombstones when they reconnect. Tombstones remain available for the configured retention window. A device returning after that window must perform a bounded consistency refresh.

## References

Pinned messages and replies store identifiers rather than duplicate message content. A tombstoned referenced message renders a stable unavailable placeholder.
