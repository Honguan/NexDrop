package postgres

import (
	"context"

	"nexdrop/internal/analytics"
)

func (store *Store) RecordSystemMetric(ctx context.Context, metric analytics.NodeMetric) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
		SELECT COUNT(*) FILTER (WHERE disconnected_at IS NULL AND last_seen_at >= $1::timestamptz - interval '45 seconds'),
		       (SELECT COUNT(*) FROM transfer_tasks WHERE status IN ('UPLOADING_TO_NODE','DOWNLOADING_FROM_NODE','TRANSFERRING_LAN','VERIFYING'))
		FROM device_connections
	`, metric.RecordedAt).Scan(&metric.OnlineDevices, &metric.ActiveTransfers)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO system_metrics (recorded_at, cpu_percent, memory_bytes, disk_bytes, cache_bytes,
		  network_upload_bytes, network_download_bytes, online_devices, active_transfers)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		ON CONFLICT (recorded_at) DO UPDATE SET cpu_percent=EXCLUDED.cpu_percent,
		  memory_bytes=EXCLUDED.memory_bytes, disk_bytes=EXCLUDED.disk_bytes,
		  cache_bytes=EXCLUDED.cache_bytes, network_upload_bytes=EXCLUDED.network_upload_bytes,
		  network_download_bytes=EXCLUDED.network_download_bytes, online_devices=EXCLUDED.online_devices,
		  active_transfers=EXCLUDED.active_transfers
	`, metric.RecordedAt, metric.CPUPercent, metric.MemoryBytes, metric.DiskBytes, metric.CacheBytes, metric.NetworkUploadBytes, metric.NetworkDownloadBytes, metric.OnlineDevices, metric.ActiveTransfers)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM system_metrics WHERE recorded_at < $1::timestamptz - interval '31 days'`, metric.RecordedAt); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
