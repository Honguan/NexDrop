# Deployment, upgrades, and recovery

[繁體中文](deployment.zh-TW.md)

## First deployment

Run `./deploy/nexdrop install`. If Docker permissions are unavailable, the script restarts itself through `sudo`; operators do not need to prefix the command manually. The installer displays all random defaults, allows every administrator, PostgreSQL password, and cursor secret to be edited, and completes only after all containers are healthy. Use `./deploy/nexdrop credentials` to view the bootstrap login again. If the administrator password has already been reset, use the new password because NexDrop cannot recover it. Infrastructure secrets appear only with an explicit `--show-secrets` option.

Run `./deploy/nexdrop configure-secrets` to rotate secrets on an existing installation. The installer asks separately about the administrator, PostgreSQL password, and cursor secret. Each value can be replaced, regenerated with `r`, or left unchanged. PostgreSQL role credentials are updated before `.env`, preventing a configuration-only change from making the Node unbootable.

## Upgrade

1. Read the CHANGELOG and release notes.
2. Run `./deploy/nexdrop update` to discover the latest stable GitHub version, or `./deploy/nexdrop update <VERSION>` to select one explicitly. The command creates a backup, pins `.env` to the exact image version, and waits for health checks. Automatic discovery requires `curl`.
3. Run `./deploy/nexdrop doctor` and confirm that the PostgreSQL password and `NEXDROP_CURSOR_SECRET` are unchanged.

The Node applies numbered migrations at startup. If an update fails, the script stops the Node, restores the previous image setting, and preserves the pre-update backup. A release containing an irreversible schema migration cannot be rolled back by changing only the image; restore the pre-update backup before starting the older version. Never delete PostgreSQL or file-data volumes during recovery.

## Backup and restore

Backups include the database, Node keys, and files. Encrypt them and move them off the host. Stop the Node before running `./deploy/nexdrop restore <backup>`, then run `doctor` and verify a sample transfer download.
