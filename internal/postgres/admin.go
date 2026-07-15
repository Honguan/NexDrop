package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"nexdrop/internal/admin"
	"nexdrop/internal/auth"
)

func (store *Store) BootstrapAdmin(ctx context.Context, username, email, passwordHash string) error {
	_, err := store.pool.Exec(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin)
		SELECT $1, $2, $3, true
		WHERE NOT EXISTS (SELECT 1 FROM users)
	`, username, email, passwordHash)
	return err
}

func (store *Store) ListAdminUsers(ctx context.Context, limit, offset int) ([]admin.User, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, username, email, is_admin, disabled_at, created_at
		FROM users ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]admin.User, 0)
	for rows.Next() {
		var item admin.User
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &item.Admin, &item.DisabledAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) CreateAdminUser(ctx context.Context, actor auth.Session, username, email, passwordHash string, isAdmin bool) (admin.User, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return admin.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result admin.User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, username, email, is_admin, disabled_at, created_at
	`, username, email, passwordHash, isAdmin).Scan(&result.ID, &result.Username, &result.Email, &result.Admin, &result.DisabledAt, &result.CreatedAt)
	if err != nil {
		return admin.User{}, mapAdminError(err)
	}
	if err := insertAudit(ctx, tx, actor, "USER_CREATED", "USER", result.ID); err != nil {
		return admin.User{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.User{}, err
	}
	return result, nil
}

func (store *Store) DisableAdminUser(ctx context.Context, actor auth.Session, userID string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE users SET disabled_at = $2 WHERE id = $1 AND disabled_at IS NULL`, userID, now)
	if err != nil {
		return mapAdminError(err)
	}
	if command.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`, userID, now); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor, "USER_DISABLED", "USER", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) ResetAdminPassword(ctx context.Context, actor auth.Session, userID, passwordHash string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1 AND disabled_at IS NULL`, userID, passwordHash)
	if err != nil {
		return mapAdminError(err)
	}
	if command.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = $2 WHERE user_id = $1 AND revoked_at IS NULL`, userID, now); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor, "USER_PASSWORD_RESET", "USER", userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) ResetAdminPasswordByIdentifier(ctx context.Context, identifier, passwordHash string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userID string
	err = tx.QueryRow(ctx, `
		UPDATE users SET password_hash=$2
		WHERE (lower(username)=lower($1) OR lower(email)=lower($1)) AND disabled_at IS NULL
		RETURNING id::text
	`, identifier, passwordHash).Scan(&userID)
	if err != nil {
		return mapAdminError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=$2 WHERE user_id=$1 AND revoked_at IS NULL`, userID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (action,target_type,target_id) VALUES ('USER_PASSWORD_RESET_CLI','USER',$1)`, userID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) AdminNodeSettings(ctx context.Context) (admin.NodeSettings, error) {
	var settings admin.NodeSettings
	err := store.pool.QueryRow(ctx, `
		SELECT single_file_limit_bytes, default_user_quota_bytes, default_group_quota_bytes,
		       node_cache_limit_bytes, default_user_daily_bytes, default_group_daily_bytes,
		       disk_warning_percent, disk_stop_percent
		FROM node_settings WHERE singleton = true
	`).Scan(&settings.SingleFileLimitBytes, &settings.DefaultUserQuotaBytes, &settings.DefaultGroupQuotaBytes, &settings.NodeCacheLimitBytes, &settings.DefaultUserDailyBytes, &settings.DefaultGroupDailyBytes, &settings.DiskWarningPercent, &settings.DiskStopPercent)
	return settings, err
}

func (store *Store) UpdateAdminNodeSettings(ctx context.Context, actor auth.Session, settings admin.NodeSettings) (admin.NodeSettings, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return admin.NodeSettings{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
		UPDATE node_settings SET single_file_limit_bytes=$1, default_user_quota_bytes=$2,
		default_group_quota_bytes=$3, node_cache_limit_bytes=$4, default_user_daily_bytes=$5,
		default_group_daily_bytes=$6, disk_warning_percent=$7, disk_stop_percent=$8
		WHERE singleton=true
		RETURNING single_file_limit_bytes, default_user_quota_bytes, default_group_quota_bytes,
		node_cache_limit_bytes, default_user_daily_bytes, default_group_daily_bytes,
		disk_warning_percent, disk_stop_percent
	`, settings.SingleFileLimitBytes, settings.DefaultUserQuotaBytes, settings.DefaultGroupQuotaBytes, settings.NodeCacheLimitBytes, settings.DefaultUserDailyBytes, settings.DefaultGroupDailyBytes, settings.DiskWarningPercent, settings.DiskStopPercent).Scan(&settings.SingleFileLimitBytes, &settings.DefaultUserQuotaBytes, &settings.DefaultGroupQuotaBytes, &settings.NodeCacheLimitBytes, &settings.DefaultUserDailyBytes, &settings.DefaultGroupDailyBytes, &settings.DiskWarningPercent, &settings.DiskStopPercent)
	if err != nil {
		return admin.NodeSettings{}, mapAdminError(err)
	}
	if err := insertAudit(ctx, tx, actor, "NODE_SETTINGS_UPDATED", "NODE_SETTINGS", ""); err != nil {
		return admin.NodeSettings{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.NodeSettings{}, err
	}
	return settings, nil
}

func (store *Store) SetAdminQuota(ctx context.Context, actor auth.Session, quota admin.Quota) (admin.Quota, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return admin.Quota{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	err = tx.QueryRow(ctx, `
		INSERT INTO storage_quotas (owner_type, owner_id, byte_limit, daily_transfer_limit)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (owner_type, owner_id) DO UPDATE SET byte_limit=EXCLUDED.byte_limit,
		  daily_transfer_limit=EXCLUDED.daily_transfer_limit, updated_at=now()
		RETURNING owner_type, owner_id::text, byte_limit, bytes_used,
		  COALESCE(daily_transfer_limit, 0), daily_transfer_used, updated_at
	`, quota.OwnerType, quota.OwnerID, quota.ByteLimit, quota.DailyTransferLimit).Scan(&quota.OwnerType, &quota.OwnerID, &quota.ByteLimit, &quota.BytesUsed, &quota.DailyTransferLimit, &quota.DailyTransferUsed, &quota.UpdatedAt)
	if err != nil {
		return admin.Quota{}, mapAdminError(err)
	}
	if err := insertAudit(ctx, tx, actor, "QUOTA_UPDATED", quota.OwnerType, quota.OwnerID); err != nil {
		return admin.Quota{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Quota{}, err
	}
	return quota, nil
}

func (store *Store) AdminStorageOverview(ctx context.Context, now time.Time) (admin.StorageOverview, error) {
	var result admin.StorageOverview
	err := store.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(size) FILTER (WHERE status='COMPLETED' AND expires_at > $1), 0),
		COALESCE(SUM(size) FILTER (WHERE status='UPLOADING' AND expires_at > $1), 0),
		COALESCE(SUM(size) FILTER (WHERE expires_at <= $1), 0)
		FROM files
	`, now).Scan(&result.FileCount, &result.StoredBytes, &result.UploadingBytes, &result.ExpiredBytes)
	if err != nil {
		return admin.StorageOverview{}, err
	}
	err = store.pool.QueryRow(ctx, `SELECT COALESCE(SUM(bytes_used),0), COALESCE(SUM(byte_limit),0) FROM storage_quotas`).Scan(&result.QuotaBytesUsed, &result.QuotaByteLimit)
	return result, err
}

func (store *Store) ListAdminFailures(ctx context.Context, limit, offset int) ([]admin.Failure, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT tt.transfer_id::text, tt.target_device_id::text, COALESCE(tt.error_code, 'UNKNOWN'), t.created_at
		FROM transfer_targets tt JOIN transfer_tasks t ON t.id=tt.transfer_id
		WHERE tt.error_code IS NOT NULL OR tt.status='FAILED'
		ORDER BY t.created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]admin.Failure, 0)
	for rows.Next() {
		var item admin.Failure
		if err := rows.Scan(&item.TransferID, &item.TargetDeviceID, &item.ErrorCode, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) ListAdminAuditLogs(ctx context.Context, limit, offset int) ([]admin.AuditLog, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, actor_user_id::text, actor_device_id::text, action, target_type,
		       target_id::text, metadata, created_at
		FROM audit_logs ORDER BY created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]admin.AuditLog, 0)
	for rows.Next() {
		var item admin.AuditLog
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.ActorDeviceID, &item.Action, &item.TargetType, &item.TargetID, &item.Metadata, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func insertAudit(ctx context.Context, tx pgx.Tx, actor auth.Session, action, targetType, targetID string) error {
	var target any
	if targetID != "" {
		target = targetID
	}
	_, err := tx.Exec(ctx, `INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, target_id) VALUES ($1,$2,$3,$4,$5)`, actor.ID, actor.DeviceID, action, targetType, target)
	return err
}

func mapAdminError(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return admin.ErrNotFound
	}
	var databaseError *pgconn.PgError
	if errors.As(err, &databaseError) && (databaseError.Code == "23505" || databaseError.Code == "23503") {
		return admin.ErrConflict
	}
	return err
}
