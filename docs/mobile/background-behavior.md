# Platform background behavior

## Android

Use WorkManager for deferrable synchronization and resumable downloads. Use a foreground service only for a user-visible continuous transfer. Persist queue state before scheduling work so process termination and recreation do not lose progress. Respect Doze, battery saver, metered networks, charging requirements, low storage, and thermal pressure.

## Windows

The desktop client may start with the user and remain in the system tray. Persist the queue and resume at verified chunk boundaries after restart, sleep, network change, or application update. Notifications must be deduplicated by transfer and state version.

## Web

Web Push is an optional wake-up hint, not proof of delivery. Synchronize metadata after the page opens and require an acknowledgement before claiming delivery. Browser storage quotas and background suspension may defer file bodies.

## Browser extension

Manifest V3 service workers are temporary. Store work before returning from an event, use alarms only for bounded retries, and avoid long-running in-memory transfer state.

## User controls

Expose automatic download, Wi-Fi only, charging only, maximum mobile-data size, previews, foreground-only mode, and per-transfer override. Default large files to metadata-only on metered networks.
