# Troubleshooting

[繁體中文](troubleshooting.zh-TW.md)

1. Run `docker compose config`, `docker compose ps`, and `./deploy/nexdrop doctor`.
2. Use `./deploy/nexdrop logs nexdrop` and search by request ID or transfer ID. Do not publish complete logs containing environment details.
3. A failed `/healthz` means the process is unavailable. A failed `/readyz` usually indicates PostgreSQL connectivity or migration trouble.
4. If an upload is rejected, inspect per-file, user, group, daily, and Node disk quotas.
5. If direct LAN transfer fails, inspect the subnet, client presence, firewall, mDNS or UDP, and access-point isolation. Node fallback should remain available.
6. For `RATE_LIMITED`, wait for `Retry-After` instead of repeatedly resending.
7. After a SHA-256 mismatch, remove the unfinished chunks and create a new task from the source file. A changed source file cannot reuse the old task.
8. `invalid admin request` means the bootstrap administrator username, email, or password is missing or invalid. Run `./deploy/nexdrop configure`, then recreate the Node container.
9. `password authentication failed for user "nexdrop"` means `.env` does not match the existing PostgreSQL volume. A new installation with no stored data can run `docker compose down --volumes` and reinstall. If data exists, never delete the volume; back it up and have a database administrator update the `nexdrop` role password.
10. If PostgreSQL URL parsing fails, confirm that the current Compose configuration keeps `NEXDROP_DATABASE_URL` free of passwords and passes the raw value through `NEXDROP_DATABASE_PASSWORD`. Do not repeatedly edit `.env` after authentication failures; synchronize the role password in the existing database first.
11. If Android reports an invalid application package, run `apksigner verify --verbose` against the APK. NexDrop release APKs require v1/v2 signatures and `armeabi-v7a`. If the same application ID is already installed with another certificate, back up local data and remove the older build, or rebuild with its original persistent signing key.

After an upgrade failure, preserve all data volumes and backups. Never run `docker compose down --volumes` as a recovery shortcut.
