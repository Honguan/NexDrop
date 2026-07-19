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

func (store *Store) BootstrapAdminTOTP(ctx context.Context, identifier, secret string) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE users SET totp_secret=$2
		WHERE (lower(username)=lower($1) OR lower(email)=lower($1))
		  AND is_admin=true AND disabled_at IS NULL
		  AND (totp_secret IS NULL OR totp_secret='')
	`, identifier, secret)
	if err != nil {
		return err
	}
	if command.RowsAffected() > 0 {
		return nil
	}
	var existing string
	err = store.pool.QueryRow(ctx, `
		SELECT COALESCE(totp_secret, '') FROM users
		WHERE (lower(username)=lower($1) OR lower(email)=lower($1))
		  AND is_admin=true AND disabled_at IS NULL
	`, identifier).Scan(&existing)
	if err != nil {
		return mapAdminError(err)
	}
	return nil
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

func (store *Store) CreateAdminInvitation(ctx context.Context, actor auth.Session, username, email string, isAdmin bool, tokenHash []byte, expiresAt time.Time) (admin.Invitation, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return admin.Invitation{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var userExists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE lower(username)=lower($1) OR lower(email)=lower($2))`, username, email).Scan(&userExists); err != nil {
		return admin.Invitation{}, err
	}
	if userExists {
		return admin.Invitation{}, admin.ErrConflict
	}
	if _, err := tx.Exec(ctx, `DELETE FROM user_invitations WHERE accepted_at IS NULL AND expires_at <= now() AND (lower(username)=lower($1) OR lower(email)=lower($2))`, username, email); err != nil {
		return admin.Invitation{}, err
	}
	var result admin.Invitation
	err = tx.QueryRow(ctx, `
		INSERT INTO user_invitations (invited_by, username, email, is_admin, token_hash, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id::text, username, email, is_admin, expires_at, created_at
	`, actor.ID, username, email, isAdmin, tokenHash, expiresAt).Scan(&result.ID, &result.Username, &result.Email, &result.Admin, &result.ExpiresAt, &result.CreatedAt)
	if err != nil {
		return admin.Invitation{}, mapAdminError(err)
	}
	if err := insertAudit(ctx, tx, actor, "USER_INVITED", "USER_INVITATION", result.ID); err != nil {
		return admin.Invitation{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return admin.Invitation{}, err
	}
	return result, nil
}

func (store *Store) AcceptAdminInvitation(ctx context.Context, tokenHash []byte, passwordHash string, now time.Time) (admin.User, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return admin.User{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var invitationID, username, email string
	var isAdmin bool
	err = tx.QueryRow(ctx, `
		SELECT id::text, username, email, is_admin FROM user_invitations
		WHERE token_hash=$1 AND accepted_at IS NULL AND expires_at > $2
		FOR UPDATE
	`, tokenHash, now).Scan(&invitationID, &username, &email, &isAdmin)
	if err != nil {
		return admin.User{}, mapAdminError(err)
	}
	var result admin.User
	err = tx.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin)
		VALUES ($1, $2, $3, $4)
		RETURNING id::text, username, email, is_admin, disabled_at, created_at
	`, username, email, passwordHash, isAdmin).Scan(&result.ID, &result.Username, &result.Email, &result.Admin, &result.DisabledAt, &result.CreatedAt)
	if err != nil {
		return admin.User{}, mapAdminError(err)
	}
	if _, err := tx.Exec(ctx, `UPDATE user_invitations SET accepted_at=$2 WHERE id=$1`, invitationID, now); err != nil {
		return admin.User{}, err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO audit_logs (action, target_type, target_id, metadata) VALUES ('USER_INVITATION_ACCEPTED', 'USER', $1, jsonb_build_object('invitationId', $2::text))`, result.ID, invitationID); err != nil {
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

func (store *Store) ListAdminDevices(ctx context.Context, limit, offset int) ([]admin.Device, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.id::text, d.user_id::text, u.username, d.display_name, d.device_type,
		       d.trust_status,
		       COALESCE(c.disconnected_at IS NULL AND c.last_seen_at >= now() - interval '45 seconds', false),
		       c.last_seen_at, d.created_at
		FROM devices d JOIN users u ON u.id = d.user_id
		LEFT JOIN LATERAL (
			SELECT last_seen_at, disconnected_at FROM device_connections
			WHERE device_id=d.id ORDER BY connected_at DESC LIMIT 1
		) c ON true
		WHERE d.deleted_at IS NULL
		ORDER BY d.created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]admin.Device, 0)
	for rows.Next() {
		var item admin.Device
		if err := rows.Scan(&item.ID, &item.OwnerUserID, &item.OwnerUsername, &item.DisplayName, &item.Type, &item.TrustStatus, &item.Online, &item.LastSeenAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) RevokeAdminDevice(ctx context.Context, actor auth.Session, deviceID string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `UPDATE devices SET trust_status='REVOKED', revoked_at=$2 WHERE id=$1 AND trust_status<>'REVOKED'`, deviceID, now)
	if err != nil {
		return mapAdminError(err)
	}
	if command.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=$2 WHERE device_id=$1 AND revoked_at IS NULL`, deviceID, now); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor, "DEVICE_REVOKED", "DEVICE", deviceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) DeleteAdminDevice(ctx context.Context, actor auth.Session, deviceID string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE devices d
		SET deleted_at=$2, trust_status='REVOKED', revoked_at=COALESCE(revoked_at, $2)
		WHERE d.id=$1 AND d.deleted_at IS NULL AND NOT EXISTS (
			SELECT 1 FROM device_connections c
			WHERE c.device_id=d.id AND c.disconnected_at IS NULL
			  AND c.last_seen_at >= $2 - interval '45 seconds'
		)
	`, deviceID, now)
	if err != nil {
		return mapAdminError(err)
	}
	if command.RowsAffected() == 0 {
		return admin.ErrConflict
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=$2 WHERE device_id=$1 AND revoked_at IS NULL`, deviceID, now); err != nil {
		return err
	}
	if err := insertAudit(ctx, tx, actor, "DEVICE_DELETED", "DEVICE", deviceID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) ListAdminGroups(ctx context.Context, limit, offset int) ([]admin.Group, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT g.id::text, g.name, g.owner_user_id::text, u.username,
		       (SELECT COUNT(*) FROM group_members gm WHERE gm.group_id=g.id),
		       (SELECT COUNT(*) FROM group_devices gd WHERE gd.group_id=g.id), g.created_at
		FROM groups g JOIN users u ON u.id = g.owner_user_id
		ORDER BY g.created_at DESC LIMIT $1 OFFSET $2
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]admin.Group, 0)
	for rows.Next() {
		var item admin.Group
		if err := rows.Scan(&item.ID, &item.Name, &item.OwnerUserID, &item.OwnerUsername, &item.MemberCount, &item.DeviceCount, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) DeleteAdminGroup(ctx context.Context, actor auth.Session, groupID string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `
		UPDATE transfer_tasks SET group_id=NULL, group_deleted_at=COALESCE(group_deleted_at, $2)
		WHERE group_id=$1
	`, groupID, now); err != nil {
		return mapAdminError(err)
	}
	command, err := tx.Exec(ctx, `DELETE FROM groups WHERE id=$1`, groupID)
	if err != nil {
		return mapAdminError(err)
	}
	if command.RowsAffected() == 0 {
		return admin.ErrNotFound
	}
	if err := insertAudit(ctx, tx, actor, "GROUP_DELETED", "GROUP", groupID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) AdminNodeSettings(ctx context.Context) (admin.NodeSettings, error) {
	var settings admin.NodeSettings
	err := store.pool.QueryRow(ctx, `
		SELECT single_file_limit_bytes, default_user_quota_bytes, default_group_quota_bytes,
		       node_cache_limit_bytes, default_user_daily_bytes, default_group_daily_bytes,
		       disk_warning_percent, disk_stop_percent, public_registration_enabled
		FROM node_settings WHERE singleton = true
	`).Scan(&settings.SingleFileLimitBytes, &settings.DefaultUserQuotaBytes, &settings.DefaultGroupQuotaBytes, &settings.NodeCacheLimitBytes, &settings.DefaultUserDailyBytes, &settings.DefaultGroupDailyBytes, &settings.DiskWarningPercent, &settings.DiskStopPercent, &settings.PublicRegistrationEnabled)
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
		default_group_daily_bytes=$6, disk_warning_percent=$7, disk_stop_percent=$8,
		public_registration_enabled=$9
		WHERE singleton=true
		RETURNING single_file_limit_bytes, default_user_quota_bytes, default_group_quota_bytes,
		node_cache_limit_bytes, default_user_daily_bytes, default_group_daily_bytes,
		disk_warning_percent, disk_stop_percent, public_registration_enabled
	`, settings.SingleFileLimitBytes, settings.DefaultUserQuotaBytes, settings.DefaultGroupQuotaBytes, settings.NodeCacheLimitBytes, settings.DefaultUserDailyBytes, settings.DefaultGroupDailyBytes, settings.DiskWarningPercent, settings.DiskStopPercent, settings.PublicRegistrationEnabled).Scan(&settings.SingleFileLimitBytes, &settings.DefaultUserQuotaBytes, &settings.DefaultGroupQuotaBytes, &settings.NodeCacheLimitBytes, &settings.DefaultUserDailyBytes, &settings.DefaultGroupDailyBytes, &settings.DiskWarningPercent, &settings.DiskStopPercent, &settings.PublicRegistrationEnabled)
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
		ORDER BY t.created_at DESC, tt.id DESC LIMIT $1 OFFSET $2
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
		FROM audit_logs ORDER BY created_at DESC, id DESC LIMIT $1 OFFSET $2
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

func (store *Store) ListAdminFailurePage(ctx context.Context, options admin.PageOptions) (admin.FailurePage, error) {
	from, to, cursorCreatedAt, cursorID := adminPageParameters(options)
	rows, err := store.pool.Query(ctx, `
		SELECT tt.transfer_id::text, tt.target_device_id::text, COALESCE(tt.error_code, 'UNKNOWN'),
		       t.created_at, tt.id::text
		FROM transfer_targets tt JOIN transfer_tasks t ON t.id=tt.transfer_id
		WHERE (tt.error_code IS NOT NULL OR tt.status='FAILED')
		  AND ($1::timestamptz IS NULL OR t.created_at >= $1)
		  AND ($2::timestamptz IS NULL OR t.created_at <= $2)
		  AND ($3::text = '' OR tt.status = $3)
		  AND ($4::timestamptz IS NULL OR (t.created_at, tt.id) < ($4::timestamptz, $5::uuid))
		ORDER BY t.created_at DESC, tt.id DESC
		LIMIT $6
	`, from, to, options.Status, cursorCreatedAt, cursorID, options.Limit+1)
	if err != nil {
		return admin.FailurePage{}, err
	}
	defer rows.Close()
	items := make([]admin.Failure, 0, options.Limit+1)
	keys := make([]admin.PageKey, 0, options.Limit+1)
	for rows.Next() {
		var item admin.Failure
		var key admin.PageKey
		if err := rows.Scan(&item.TransferID, &item.TargetDeviceID, &item.ErrorCode, &item.CreatedAt, &key.ID); err != nil {
			return admin.FailurePage{}, err
		}
		key.CreatedAt = item.CreatedAt.UTC()
		items = append(items, item)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return admin.FailurePage{}, err
	}
	page := admin.FailurePage{Items: items}
	if len(items) > options.Limit {
		page.Items = items[:options.Limit]
		page.NextCursor = keys[options.Limit-1].ID
		page.NextPageKey = keys[options.Limit-1]
	}
	return page, nil
}

func (store *Store) ListAdminAuditLogPage(ctx context.Context, options admin.PageOptions) (admin.AuditLogPage, error) {
	from, to, cursorCreatedAt, cursorID := adminPageParameters(options)
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, actor_user_id::text, actor_device_id::text, action, target_type,
		       target_id::text, metadata, created_at
		FROM audit_logs
		WHERE ($1::timestamptz IS NULL OR created_at >= $1)
		  AND ($2::timestamptz IS NULL OR created_at <= $2)
		  AND ($3::text = '' OR action = $3)
		  AND ($4::timestamptz IS NULL OR (created_at, id) < ($4::timestamptz, $5::uuid))
		ORDER BY created_at DESC, id DESC
		LIMIT $6
	`, from, to, options.Status, cursorCreatedAt, cursorID, options.Limit+1)
	if err != nil {
		return admin.AuditLogPage{}, err
	}
	defer rows.Close()
	items := make([]admin.AuditLog, 0, options.Limit+1)
	for rows.Next() {
		var item admin.AuditLog
		if err := rows.Scan(&item.ID, &item.ActorUserID, &item.ActorDeviceID, &item.Action, &item.TargetType, &item.TargetID, &item.Metadata, &item.CreatedAt); err != nil {
			return admin.AuditLogPage{}, err
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return admin.AuditLogPage{}, err
	}
	page := admin.AuditLogPage{Items: items}
	if len(items) > options.Limit {
		page.Items = items[:options.Limit]
		last := items[options.Limit-1]
		page.NextCursor = last.ID
		page.NextPageKey = admin.PageKey{ID: last.ID, CreatedAt: last.CreatedAt.UTC()}
	}
	return page, nil
}

func adminPageParameters(options admin.PageOptions) (from, to, cursorCreatedAt, cursorID any) {
	if !options.From.IsZero() {
		from = options.From.UTC()
	}
	if !options.To.IsZero() {
		to = options.To.UTC()
	}
	if !options.Cursor.CreatedAt.IsZero() {
		cursorCreatedAt = options.Cursor.CreatedAt.UTC()
		cursorID = options.Cursor.ID
	}
	return from, to, cursorCreatedAt, cursorID
}

func (store *Store) DeleteAdminGroupContent(ctx context.Context, actor auth.Session, transferID string, now time.Time) ([]string, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	rows, err := tx.Query(ctx, `
		SELECT storage_path FROM files WHERE transfer_id = $1 AND storage_path <> ''
		UNION ALL
		SELECT chunk.storage_path FROM file_chunks chunk
		JOIN files file ON file.id = chunk.file_id
		WHERE file.transfer_id = $1 AND chunk.storage_path <> ''
	`, transferID)
	if err != nil {
		return nil, err
	}
	paths := make([]string, 0)
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			rows.Close()
			return nil, err
		}
		paths = append(paths, path)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	command, err := tx.Exec(ctx, `
		UPDATE transfer_tasks
		SET group_deleted_at = $2, status = 'CANCELLED'
		WHERE id = $1 AND group_id IS NOT NULL AND group_deleted_at IS NULL
	`, transferID, now)
	if err != nil {
		return nil, mapAdminError(err)
	}
	if command.RowsAffected() == 0 {
		return nil, admin.ErrNotFound
	}
	if _, err := tx.Exec(ctx, `UPDATE messages SET encrypted_content = NULL WHERE transfer_id = $1`, transferID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `UPDATE files SET status = 'EXPIRED', expires_at = $2 WHERE transfer_id = $1`, transferID, now); err != nil {
		return nil, err
	}
	if err := insertAudit(ctx, tx, actor, "GROUP_CONTENT_DELETED", "TRANSFER", transferID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return paths, nil
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
