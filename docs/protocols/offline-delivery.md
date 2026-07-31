# Offline delivery protocol

NexDrop separates presence, trust, metadata synchronization, body download, delivery, and read state.

## Priority order

1. urgent metadata and text
2. links, images, and small files
3. deferred large files and folders

Metadata is synchronized before file bodies. A reconnecting device processes unsynchronized metadata first, then higher priority and smaller bodies.

## States

`QUEUED`, `METADATA_AVAILABLE`, `WAITING_FOR_DEVICE`, `WAITING_FOR_NETWORK_POLICY`, `WAITING_FOR_POWER`, `DOWNLOADING`, `PAUSED_BY_SYSTEM`, `DELIVERED`, `READ`, `EXPIRED`, and `CANCELLED`.

Every waiting or paused state carries a stable reason code such as `WIFI_REQUIRED`, `CHARGING_REQUIRED`, `MOBILE_DATA_LIMIT`, `FOREGROUND_REQUIRED`, `INSUFFICIENT_STORAGE`, or `SYSTEM_RESOURCE_LIMIT`.

## Acknowledgements

- accepted by Node does not mean delivered
- metadata synchronized does not mean body downloaded
- body downloaded requires verified completion
- read is independent per device

Queue mutations and notifications are idempotent. Reconnects and retries must not produce duplicate notifications.

## Privacy

Push payloads contain opaque identifiers by default. Message text and filenames appear only when the user explicitly enables previews.
