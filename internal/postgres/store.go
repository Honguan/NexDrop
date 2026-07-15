package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"nexdrop/internal/analytics"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/domain"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/maintenance"
	"nexdrop/internal/pairing"
	"nexdrop/internal/presence"
	"nexdrop/internal/transfer"
)

type Store struct {
	pool *pgxpool.Pool
}

func Open(ctx context.Context, databaseURL string) (*Store, error) {
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &Store{pool: pool}, nil
}

func (store *Store) Close() {
	store.pool.Close()
}

func (store *Store) Ping(ctx context.Context) error {
	return store.pool.Ping(ctx)
}

func (store *Store) CredentialByIdentifier(ctx context.Context, identifier string) (auth.Credential, error) {
	var credential auth.Credential
	err := store.pool.QueryRow(ctx, `
		SELECT id::text, username, email, is_admin, password_hash
		FROM users
		WHERE (lower(username) = lower($1) OR lower(email) = lower($1))
		  AND disabled_at IS NULL
	`, identifier).Scan(
		&credential.ID,
		&credential.Username,
		&credential.Email,
		&credential.Admin,
		&credential.PasswordHash,
	)
	return credential, err
}

func (store *Store) CreateSession(
	ctx context.Context,
	userID string,
	accessTokenHash []byte,
	accessExpiresAt time.Time,
	refreshTokenHash []byte,
	refreshExpiresAt time.Time,
) (string, error) {
	var sessionID string
	err := store.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (
			user_id, access_token_hash, access_expires_at, refresh_token_hash, expires_at
		) VALUES ($1, $2, $3, $4, $5)
		RETURNING id::text
	`, userID, accessTokenHash, accessExpiresAt, refreshTokenHash, refreshExpiresAt).Scan(&sessionID)
	return sessionID, err
}

func (store *Store) SessionByAccessToken(ctx context.Context, tokenHash []byte, now time.Time) (auth.Session, error) {
	var session auth.Session
	err := store.pool.QueryRow(ctx, `
		SELECT s.id::text, u.id::text, u.username, u.email, u.is_admin, s.device_id::text
		FROM user_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.access_token_hash = $1
		  AND s.access_expires_at > $2
		  AND s.revoked_at IS NULL
		  AND u.disabled_at IS NULL
	`, tokenHash, now).Scan(&session.SessionID, &session.ID, &session.Username, &session.Email, &session.Admin, &session.DeviceID)
	return session, err
}

func (store *Store) RotateSession(
	ctx context.Context,
	oldRefreshTokenHash []byte,
	accessTokenHash []byte,
	accessExpiresAt time.Time,
	refreshTokenHash []byte,
	refreshExpiresAt time.Time,
	now time.Time,
) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE user_sessions
		SET access_token_hash = $2,
		    access_expires_at = $3,
		    refresh_token_hash = $4,
		    expires_at = $5
		WHERE refresh_token_hash = $1
		  AND expires_at > $6
		  AND revoked_at IS NULL
	`, oldRefreshTokenHash, accessTokenHash, accessExpiresAt, refreshTokenHash, refreshExpiresAt, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (store *Store) RevokeSessionByRefreshToken(ctx context.Context, tokenHash []byte, now time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE user_sessions
		SET revoked_at = $2
		WHERE refresh_token_hash = $1 AND revoked_at IS NULL
	`, tokenHash, now)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return pgx.ErrNoRows
	}
	return nil
}

func (store *Store) CreateDevice(ctx context.Context, session auth.Session, name string, deviceType device.Type, publicKey []byte, algorithm string) (device.Device, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return device.Device{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var result device.Device
	err = tx.QueryRow(ctx, `
		INSERT INTO devices (user_id, display_name, device_type)
		VALUES ($1, $2, $3)
		RETURNING id::text, display_name, device_type, trust_status, revoked_at, created_at
	`, session.ID, name, deviceType).Scan(
		&result.ID, &result.DisplayName, &result.Type, &result.TrustStatus, &result.RevokedAt, &result.CreatedAt,
	)
	if err != nil {
		return device.Device{}, err
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO device_keys (device_id, public_key, key_algorithm)
		VALUES ($1, $2, $3)
	`, result.ID, publicKey, algorithm)
	if err != nil {
		return device.Device{}, err
	}
	command, err := tx.Exec(ctx, `
		UPDATE user_sessions SET device_id = $1
		WHERE id = $2 AND user_id = $3 AND device_id IS NULL AND revoked_at IS NULL
	`, result.ID, session.SessionID, session.ID)
	if err != nil {
		return device.Device{}, err
	}
	if command.RowsAffected() == 0 {
		return device.Device{}, device.ErrForbidden
	}
	if err := tx.Commit(ctx); err != nil {
		return device.Device{}, err
	}
	result.PublicKey = append([]byte(nil), publicKey...)
	result.Algorithm = algorithm
	return result, nil
}

func (store *Store) ListDevices(ctx context.Context, userID string) ([]device.Device, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.id::text, d.display_name, d.device_type, k.public_key, k.key_algorithm,
		       d.trust_status, d.revoked_at, d.created_at
		FROM devices d
		JOIN device_keys k ON k.device_id = d.id
		WHERE d.user_id = $1
		ORDER BY d.created_at
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	devices := make([]device.Device, 0)
	for rows.Next() {
		var item device.Device
		if err := rows.Scan(&item.ID, &item.DisplayName, &item.Type, &item.PublicKey, &item.Algorithm, &item.TrustStatus, &item.RevokedAt, &item.CreatedAt); err != nil {
			return nil, err
		}
		devices = append(devices, item)
	}
	return devices, rows.Err()
}

func (store *Store) RenameDevice(ctx context.Context, userID, deviceID, name string) (device.Device, error) {
	return scanDevice(store.pool.QueryRow(ctx, `
		UPDATE devices d SET display_name = $3
		FROM device_keys k
		WHERE d.id = $1 AND d.user_id = $2 AND k.device_id = d.id
		RETURNING d.id::text, d.display_name, d.device_type, k.public_key, k.key_algorithm,
		          d.trust_status, d.revoked_at, d.created_at
	`, deviceID, userID, name))
}

func (store *Store) DeleteDevice(ctx context.Context, userID, deviceID string) error {
	command, err := store.pool.Exec(ctx, `DELETE FROM devices WHERE id = $1 AND user_id = $2`, deviceID, userID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return device.ErrNotFound
	}
	return nil
}

func (store *Store) ApproveDevice(ctx context.Context, session auth.Session, deviceID string) (device.Device, error) {
	result, err := scanDevice(store.pool.QueryRow(ctx, `
		UPDATE devices d SET trust_status = 'TRUSTED'
		FROM device_keys k
		WHERE d.id = $1 AND k.device_id = d.id AND d.revoked_at IS NULL
		  AND ($2 OR EXISTS (
		      SELECT 1 FROM user_sessions s
		      JOIN devices actor ON actor.id = s.device_id
		      WHERE s.id = $3 AND s.revoked_at IS NULL
		        AND actor.user_id = d.user_id
		        AND actor.trust_status = 'TRUSTED' AND actor.revoked_at IS NULL
		  ))
		RETURNING d.id::text, d.display_name, d.device_type, k.public_key, k.key_algorithm,
		          d.trust_status, d.revoked_at, d.created_at
	`, deviceID, session.Admin, session.SessionID))
	if errors.Is(err, pgx.ErrNoRows) {
		return device.Device{}, device.ErrForbidden
	}
	return result, err
}

func (store *Store) RevokeDevice(ctx context.Context, session auth.Session, deviceID string, now time.Time) (device.Device, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return device.Device{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result, err := scanDevice(tx.QueryRow(ctx, `
		UPDATE devices d SET trust_status = 'REVOKED', revoked_at = $3
		FROM device_keys k
		WHERE d.id = $1 AND k.device_id = d.id AND ($2 OR d.user_id = $4)
		RETURNING d.id::text, d.display_name, d.device_type, k.public_key, k.key_algorithm,
		          d.trust_status, d.revoked_at, d.created_at
	`, deviceID, session.Admin, now, session.ID))
	if errors.Is(err, pgx.ErrNoRows) {
		return device.Device{}, device.ErrForbidden
	}
	if err != nil {
		return device.Device{}, err
	}
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at = $2 WHERE device_id = $1 AND revoked_at IS NULL`, deviceID, now); err != nil {
		return device.Device{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return device.Device{}, err
	}
	return result, nil
}

func (store *Store) CreatePairingCode(ctx context.Context, session auth.Session, deviceID string, codeHash []byte, expiresAt, now time.Time) (string, error) {
	var id string
	err := store.pool.QueryRow(ctx, `
		INSERT INTO device_pairing_codes (
			target_device_id, created_by_session_id, code_hash, expires_at, created_at
		)
		SELECT target.id, $2, $3, $4, $5
		FROM devices target
		WHERE target.id = $1 AND target.trust_status = 'PENDING' AND target.revoked_at IS NULL
		  AND ($6 OR EXISTS (
		      SELECT 1 FROM user_sessions s
		      JOIN devices actor ON actor.id = s.device_id
		      WHERE s.id = $2 AND s.revoked_at IS NULL
		        AND actor.user_id = target.user_id
		        AND actor.trust_status = 'TRUSTED' AND actor.revoked_at IS NULL
		  ))
		RETURNING id::text
	`, deviceID, session.SessionID, codeHash, expiresAt, now, session.Admin).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", device.ErrForbidden
	}
	return id, err
}

func (store *Store) RedeemPairingCode(
	ctx context.Context,
	session auth.Session,
	deviceID string,
	challengeID string,
	codeHash []byte,
	now time.Time,
	maximumAttempts int,
) (device.Device, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return device.Device{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	var sessionOwnsDevice bool
	err = tx.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM user_sessions
			WHERE id = $1 AND device_id = $2 AND revoked_at IS NULL
		)
	`, session.SessionID, deviceID).Scan(&sessionOwnsDevice)
	if err != nil {
		return device.Device{}, err
	}
	if !sessionOwnsDevice {
		return device.Device{}, device.ErrForbidden
	}

	var targetDeviceID string
	var storedHash []byte
	var attemptCount int
	var expiresAt time.Time
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT target_device_id::text, code_hash, attempt_count, expires_at, used_at
		FROM device_pairing_codes
		WHERE id = $1 AND target_device_id = $2
		FOR UPDATE
	`, challengeID, deviceID).Scan(&targetDeviceID, &storedHash, &attemptCount, &expiresAt, &usedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return device.Device{}, pairing.ErrInvalidCode
	}
	if err != nil {
		return device.Device{}, err
	}
	if usedAt != nil {
		return device.Device{}, pairing.ErrUsed
	}
	if !expiresAt.After(now) {
		return device.Device{}, pairing.ErrExpired
	}
	if attemptCount >= maximumAttempts {
		return device.Device{}, pairing.ErrLocked
	}
	if subtle.ConstantTimeCompare(storedHash, codeHash) != 1 {
		attemptCount++
		if _, err := tx.Exec(ctx, `UPDATE device_pairing_codes SET attempt_count = $2 WHERE id = $1`, challengeID, attemptCount); err != nil {
			return device.Device{}, err
		}
		if err := tx.Commit(ctx); err != nil {
			return device.Device{}, err
		}
		if attemptCount >= maximumAttempts {
			return device.Device{}, pairing.ErrLocked
		}
		return device.Device{}, pairing.ErrInvalidCode
	}

	if _, err := tx.Exec(ctx, `UPDATE device_pairing_codes SET used_at = $2 WHERE id = $1`, challengeID, now); err != nil {
		return device.Device{}, err
	}
	result, err := scanDevice(tx.QueryRow(ctx, `
		UPDATE devices d SET trust_status = 'TRUSTED'
		FROM device_keys k
		WHERE d.id = $1 AND k.device_id = d.id AND d.trust_status = 'PENDING' AND d.revoked_at IS NULL
		RETURNING d.id::text, d.display_name, d.device_type, k.public_key, k.key_algorithm,
		          d.trust_status, d.revoked_at, d.created_at
	`, targetDeviceID))
	if err != nil {
		return device.Device{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return device.Device{}, err
	}
	return result, nil
}

func (store *Store) CreateGroup(ctx context.Context, session auth.Session, name string) (group.Details, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return group.Details{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var result group.Details
	err = tx.QueryRow(ctx, `
		INSERT INTO groups (owner_user_id, name)
		VALUES ($1, $2)
		RETURNING id::text, name, owner_user_id::text, created_at
	`, session.ID, name).Scan(&result.ID, &result.Name, &result.OwnerID, &result.CreatedAt)
	if err != nil {
		return group.Details{}, err
	}
	result.Role = group.RoleOwner
	var owner group.Member
	err = tx.QueryRow(ctx, `
		INSERT INTO group_members (group_id, user_id, role)
		VALUES ($1, $2, 'OWNER')
		RETURNING user_id::text, $3, role, joined_at
	`, result.ID, session.ID, session.Username).Scan(&owner.UserID, &owner.Username, &owner.Role, &owner.JoinedAt)
	if err != nil {
		return group.Details{}, err
	}
	result.Members = []group.Member{owner}
	result.Devices = []group.GroupDevice{}
	if err := tx.Commit(ctx); err != nil {
		return group.Details{}, err
	}
	return result, nil
}

func (store *Store) ListGroups(ctx context.Context, session auth.Session) ([]group.Group, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT g.id::text, g.name, g.owner_user_id::text, membership.role, g.created_at
		FROM groups g
		JOIN group_members membership ON membership.group_id = g.id
		WHERE membership.user_id = $1
		ORDER BY g.created_at DESC
	`, session.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]group.Group, 0)
	for rows.Next() {
		var item group.Group
		if err := rows.Scan(&item.ID, &item.Name, &item.OwnerID, &item.Role, &item.CreatedAt); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) GetGroup(ctx context.Context, session auth.Session, groupID string) (group.Details, error) {
	var result group.Details
	err := store.pool.QueryRow(ctx, `
		SELECT g.id::text, g.name, g.owner_user_id::text, membership.role, g.created_at
		FROM groups g
		JOIN group_members membership ON membership.group_id = g.id
		WHERE g.id = $1 AND membership.user_id = $2
	`, groupID, session.ID).Scan(&result.ID, &result.Name, &result.OwnerID, &result.Role, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return group.Details{}, group.ErrNotFound
	}
	if err != nil {
		return group.Details{}, err
	}
	memberRows, err := store.pool.Query(ctx, `
		SELECT m.user_id::text, u.username, m.role, m.joined_at
		FROM group_members m JOIN users u ON u.id = m.user_id
		WHERE m.group_id = $1
		ORDER BY m.joined_at
	`, groupID)
	if err != nil {
		return group.Details{}, err
	}
	defer memberRows.Close()
	result.Members = make([]group.Member, 0)
	for memberRows.Next() {
		var member group.Member
		if err := memberRows.Scan(&member.UserID, &member.Username, &member.Role, &member.JoinedAt); err != nil {
			return group.Details{}, err
		}
		result.Members = append(result.Members, member)
	}
	if err := memberRows.Err(); err != nil {
		return group.Details{}, err
	}
	deviceRows, err := store.pool.Query(ctx, `
		SELECT d.id::text, d.user_id::text, d.display_name, d.device_type,
		       k.public_key, k.key_algorithm, gd.added_at
		FROM group_devices gd
		JOIN devices d ON d.id = gd.device_id
		JOIN device_keys k ON k.device_id = d.id
		WHERE gd.group_id = $1
		ORDER BY gd.added_at
	`, groupID)
	if err != nil {
		return group.Details{}, err
	}
	defer deviceRows.Close()
	result.Devices = make([]group.GroupDevice, 0)
	for deviceRows.Next() {
		var item group.GroupDevice
		if err := deviceRows.Scan(&item.ID, &item.OwnerUserID, &item.DisplayName, &item.Type, &item.PublicKey, &item.Algorithm, &item.AddedAt); err != nil {
			return group.Details{}, err
		}
		result.Devices = append(result.Devices, item)
	}
	return result, deviceRows.Err()
}

func (store *Store) RenameGroup(ctx context.Context, session auth.Session, groupID, name string) (group.Group, error) {
	var result group.Group
	err := store.pool.QueryRow(ctx, `
		UPDATE groups SET name = $3
		WHERE id = $1 AND owner_user_id = $2
		RETURNING id::text, name, owner_user_id::text, 'OWNER', created_at
	`, groupID, session.ID, name).Scan(&result.ID, &result.Name, &result.OwnerID, &result.Role, &result.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return group.Group{}, group.ErrForbidden
	}
	return result, err
}

func (store *Store) DeleteGroup(ctx context.Context, session auth.Session, groupID string) error {
	command, err := store.pool.Exec(ctx, `DELETE FROM groups WHERE id = $1 AND owner_user_id = $2`, groupID, session.ID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return group.ErrForbidden
	}
	return nil
}

func (store *Store) AddGroupMember(ctx context.Context, session auth.Session, groupID, userID string, role group.Role) (group.Member, error) {
	var result group.Member
	err := store.pool.QueryRow(ctx, `
		INSERT INTO group_members (group_id, user_id, role)
		SELECT $1, target.id, $4
		FROM users target
		JOIN group_members actor ON actor.group_id = $1 AND actor.user_id = $2
		WHERE target.id = $3
		  AND (actor.role = 'OWNER' OR (actor.role = 'ADMIN' AND $4 = 'MEMBER'))
		RETURNING user_id::text, (SELECT username FROM users WHERE id = user_id), role, joined_at
	`, groupID, session.ID, userID, role).Scan(&result.UserID, &result.Username, &result.Role, &result.JoinedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return group.Member{}, group.ErrForbidden
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return group.Member{}, group.ErrConflict
	}
	return result, err
}

func (store *Store) RemoveGroupMember(ctx context.Context, session auth.Session, groupID, userID string) error {
	command, err := store.pool.Exec(ctx, `
		DELETE FROM group_members target
		USING group_members actor
		WHERE target.group_id = $1 AND target.user_id = $2
		  AND actor.group_id = target.group_id AND actor.user_id = $3
		  AND target.role <> 'OWNER'
		  AND (actor.role = 'OWNER' OR (actor.role = 'ADMIN' AND target.role = 'MEMBER'))
	`, groupID, userID, session.ID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return group.ErrForbidden
	}
	return nil
}

func (store *Store) AddGroupDevice(ctx context.Context, session auth.Session, groupID, deviceID string, addedAt time.Time) (group.GroupDevice, error) {
	var result group.GroupDevice
	err := store.pool.QueryRow(ctx, `
		INSERT INTO group_devices (group_id, device_id, added_at)
		SELECT $1, target.id, $4
		FROM devices target
		JOIN group_members device_owner ON device_owner.group_id = $1 AND device_owner.user_id = target.user_id
		JOIN group_members actor ON actor.group_id = $1 AND actor.user_id = $2
		WHERE target.id = $3 AND target.trust_status = 'TRUSTED' AND target.revoked_at IS NULL
		  AND (actor.role IN ('OWNER', 'ADMIN') OR target.user_id = $2)
		RETURNING device_id::text,
		          (SELECT user_id::text FROM devices WHERE id = device_id),
		          (SELECT display_name FROM devices WHERE id = device_id),
		          (SELECT device_type FROM devices WHERE id = device_id),
		          (SELECT public_key FROM device_keys k WHERE k.device_id = group_devices.device_id),
		          (SELECT key_algorithm FROM device_keys k WHERE k.device_id = group_devices.device_id),
		          added_at
	`, groupID, session.ID, deviceID, addedAt).Scan(
		&result.ID, &result.OwnerUserID, &result.DisplayName, &result.Type, &result.PublicKey, &result.Algorithm, &result.AddedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return group.GroupDevice{}, group.ErrForbidden
	}
	var pgError *pgconn.PgError
	if errors.As(err, &pgError) && pgError.Code == "23505" {
		return group.GroupDevice{}, group.ErrConflict
	}
	return result, err
}

func (store *Store) RemoveGroupDevice(ctx context.Context, session auth.Session, groupID, deviceID string) error {
	command, err := store.pool.Exec(ctx, `
		DELETE FROM group_devices target
		USING devices d, group_members actor
		WHERE target.group_id = $1 AND target.device_id = $2
		  AND d.id = target.device_id
		  AND actor.group_id = target.group_id AND actor.user_id = $3
		  AND (actor.role IN ('OWNER', 'ADMIN') OR d.user_id = $3)
	`, groupID, deviceID, session.ID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return group.ErrForbidden
	}
	return nil
}

func (store *Store) ResolveTransferTargets(ctx context.Context, session auth.Session, targetType transfer.TargetType, groupID string, requested []string) ([]string, error) {
	var rows pgx.Rows
	var err error
	switch targetType {
	case transfer.TargetSingle, transfer.TargetMultiple:
		rows, err = store.pool.Query(ctx, `
			SELECT id::text FROM devices
			WHERE user_id = $1 AND id::text = ANY($2)
			  AND trust_status = 'TRUSTED' AND revoked_at IS NULL
		`, session.ID, requested)
	case transfer.TargetAllDevices:
		rows, err = store.pool.Query(ctx, `
			SELECT id::text FROM devices
			WHERE user_id = $1 AND id <> $2
			  AND trust_status = 'TRUSTED' AND revoked_at IS NULL
		`, session.ID, session.DeviceID)
	case transfer.TargetGroupAll:
		rows, err = store.pool.Query(ctx, `
			SELECT d.id::text
			FROM group_devices gd
			JOIN devices d ON d.id = gd.device_id
			JOIN group_members actor ON actor.group_id = gd.group_id AND actor.user_id = $2
			WHERE gd.group_id = $1 AND d.trust_status = 'TRUSTED' AND d.revoked_at IS NULL
		`, groupID, session.ID)
	case transfer.TargetGroupSelected:
		rows, err = store.pool.Query(ctx, `
			SELECT d.id::text
			FROM group_devices gd
			JOIN devices d ON d.id = gd.device_id
			JOIN group_members actor ON actor.group_id = gd.group_id AND actor.user_id = $2
			WHERE gd.group_id = $1 AND d.id::text = ANY($3)
			  AND d.trust_status = 'TRUSTED' AND d.revoked_at IS NULL
		`, groupID, session.ID, requested)
	default:
		return nil, transfer.ErrInvalid
	}
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		result = append(result, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if (targetType == transfer.TargetSingle || targetType == transfer.TargetMultiple || targetType == transfer.TargetGroupSelected) && len(result) != len(requested) {
		return nil, transfer.ErrForbidden
	}
	return result, nil
}

func (store *Store) CreateTransfer(ctx context.Context, session auth.Session, prepared transfer.Prepared) (transfer.Transfer, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return transfer.Transfer{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := enforceTransferQuotas(ctx, tx, session, prepared); err != nil {
		return transfer.Transfer{}, err
	}
	var result transfer.Transfer
	var groupID any
	if prepared.GroupID != "" {
		groupID = prepared.GroupID
	}
	var totalSize int64
	for _, file := range prepared.Files {
		totalSize += file.Size
	}
	err = tx.QueryRow(ctx, `
		INSERT INTO transfer_tasks (
			sender_user_id, sender_device_id, group_id, target_type, content_type,
			total_file_count, total_size, status, created_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		RETURNING id::text
	`, session.ID, session.DeviceID, groupID, prepared.TargetType, prepared.ContentType,
		len(prepared.Files), totalSize, prepared.Status, prepared.CreatedAt, prepared.ExpiresAt).Scan(&result.ID)
	if err != nil {
		return transfer.Transfer{}, err
	}
	for _, deviceID := range prepared.ResolvedDeviceIDs {
		wrappedKey := prepared.WrappedContentKeys[deviceID]
		if len(wrappedKey) == 0 {
			continue
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_content_keys (transfer_id, target_device_id, wrapped_content_key)
			VALUES ($1, $2, $3)
		`, result.ID, deviceID, wrappedKey)
		if err != nil {
			return transfer.Transfer{}, err
		}
	}
	if len(prepared.Content) > 0 {
		var messageID string
		err = tx.QueryRow(ctx, `
			INSERT INTO messages (
				transfer_id, sender_user_id, sender_device_id, group_id, content_type,
				encrypted_content, created_at, expires_at
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
			RETURNING id::text
		`, result.ID, session.ID, session.DeviceID, groupID, prepared.ContentType,
			prepared.Content, prepared.CreatedAt, prepared.ExpiresAt).Scan(&messageID)
		if err != nil {
			return transfer.Transfer{}, err
		}
		for _, deviceID := range prepared.ResolvedDeviceIDs {
			_, err = tx.Exec(ctx, `
				INSERT INTO message_targets (message_id, target_device_id, wrapped_content_key)
				VALUES ($1, $2, $3)
			`, messageID, deviceID, prepared.WrappedContentKeys[deviceID])
			if err != nil {
				return transfer.Transfer{}, err
			}
		}
	}
	fileIDs := make([]string, len(prepared.Files))
	for index, file := range prepared.Files {
		err = tx.QueryRow(ctx, `
			WITH generated AS (SELECT gen_random_uuid() AS id)
			INSERT INTO files (
				id, transfer_id, original_name, mime_type, size, sha256,
				chunk_size, chunk_count, storage_path, expires_at
			)
			SELECT id, $1, $2, $3, $4, $5, $6, $7, 'pending/' || id::text, $8
			FROM generated
			RETURNING id::text
		`, result.ID, file.Name, file.MIMEType, file.Size, file.SHA256,
			file.ChunkSize, file.ChunkCount, prepared.ExpiresAt).Scan(&fileIDs[index])
		if err != nil {
			return transfer.Transfer{}, err
		}
	}
	for _, target := range prepared.Targets {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_targets (
				transfer_id, target_device_id, selected_route, status, bytes_transferred
			) VALUES ($1, $2, $3, $4, 0)
		`, result.ID, target.DeviceID, target.SelectedRoute, target.Status)
		if err != nil {
			return transfer.Transfer{}, err
		}
		_, err = tx.Exec(ctx, `
			INSERT INTO notifications (device_id, notification_type, payload, created_at)
			VALUES ($1, 'TRANSFER', jsonb_build_object('transferId', $2::text), $3)
		`, target.DeviceID, result.ID, prepared.CreatedAt)
		if err != nil {
			return transfer.Transfer{}, err
		}
	}
	for _, target := range prepared.FileTargets {
		_, err = tx.Exec(ctx, `
			INSERT INTO transfer_file_targets (file_id, target_device_id, selected_route, status)
			VALUES ($1, $2, $3, $4)
		`, fileIDs[target.FileIndex], target.DeviceID, target.SelectedRoute, target.Status)
		if err != nil {
			return transfer.Transfer{}, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return transfer.Transfer{}, err
	}
	return store.GetTransfer(ctx, session, result.ID)
}

func enforceTransferQuotas(ctx context.Context, tx pgx.Tx, session auth.Session, prepared transfer.Prepared) error {
	nodeFiles := make(map[int]bool)
	for _, target := range prepared.FileTargets {
		if target.SelectedRoute == domain.SelectedRouteNode {
			nodeFiles[target.FileIndex] = true
		}
	}
	if len(nodeFiles) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(730042)`); err != nil {
		return err
	}
	var singleFileLimit, defaultUserQuota, defaultGroupQuota, nodeLimit int64
	var defaultUserDaily, defaultGroupDaily int64
	var warningPercent, stopPercent int
	err := tx.QueryRow(ctx, `
		SELECT single_file_limit_bytes, default_user_quota_bytes, default_group_quota_bytes,
		       node_cache_limit_bytes, default_user_daily_bytes, default_group_daily_bytes,
		       disk_warning_percent, disk_stop_percent
		FROM node_settings WHERE singleton
	`).Scan(&singleFileLimit, &defaultUserQuota, &defaultGroupQuota, &nodeLimit,
		&defaultUserDaily, &defaultGroupDaily, &warningPercent, &stopPercent)
	if err != nil {
		return err
	}
	var requestedBytes int64
	for index := range nodeFiles {
		fileSize := prepared.Files[index].Size
		if fileSize > singleFileLimit {
			return transfer.ErrFileTooLarge
		}
		if requestedBytes > int64(^uint64(0)>>1)-fileSize {
			return transfer.ErrQuotaExceeded
		}
		requestedBytes += fileSize
	}
	var userQuota, userDailyLimit int64
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE((SELECT byte_limit FROM storage_quotas WHERE owner_type = 'USER' AND owner_id = $1), $2),
			COALESCE((SELECT daily_transfer_limit FROM storage_quotas WHERE owner_type = 'USER' AND owner_id = $1), $3)
	`, session.ID, defaultUserQuota, defaultUserDaily).Scan(&userQuota, &userDailyLimit)
	if err != nil {
		return err
	}
	var userUsage, nodeUsage, userDailyUsage int64
	err = tx.QueryRow(ctx, `
		SELECT
			COALESCE(SUM(f.size) FILTER (WHERE t.sender_user_id = $1), 0),
			COALESCE(SUM(f.size), 0),
			COALESCE(SUM(f.size) FILTER (
				WHERE t.sender_user_id = $1 AND t.created_at >= $2
			), 0)
		FROM files f JOIN transfer_tasks t ON t.id = f.transfer_id
		WHERE f.status <> 'EXPIRED' AND f.expires_at > $3
		  AND EXISTS (
		      SELECT 1 FROM transfer_file_targets ft
		      WHERE ft.file_id = f.id AND ft.selected_route = 'NODE'
		  )
	`, session.ID, startOfUTCDay(prepared.CreatedAt), prepared.CreatedAt).Scan(&userUsage, &nodeUsage, &userDailyUsage)
	if err != nil {
		return err
	}
	if exceeds(userUsage, requestedBytes, userQuota) || exceeds(userDailyUsage, requestedBytes, userDailyLimit) {
		return transfer.ErrQuotaExceeded
	}
	nodeStopLimit := nodeLimit * int64(stopPercent) / 100
	if exceeds(nodeUsage, requestedBytes, nodeStopLimit) {
		return transfer.ErrStorageFull
	}
	if prepared.GroupID != "" {
		var groupQuota, groupDailyLimit, groupUsage, groupDailyUsage int64
		err = tx.QueryRow(ctx, `
			SELECT
				COALESCE((SELECT byte_limit FROM storage_quotas WHERE owner_type = 'GROUP' AND owner_id = $1), $2),
				COALESCE((SELECT daily_transfer_limit FROM storage_quotas WHERE owner_type = 'GROUP' AND owner_id = $1), $3)
		`, prepared.GroupID, defaultGroupQuota, defaultGroupDaily).Scan(&groupQuota, &groupDailyLimit)
		if err != nil {
			return err
		}
		err = tx.QueryRow(ctx, `
			SELECT
				COALESCE(SUM(f.size), 0),
				COALESCE(SUM(f.size) FILTER (WHERE t.created_at >= $2), 0)
			FROM files f JOIN transfer_tasks t ON t.id = f.transfer_id
			WHERE t.group_id = $1 AND f.status <> 'EXPIRED' AND f.expires_at > $3
			  AND EXISTS (
			      SELECT 1 FROM transfer_file_targets ft
			      WHERE ft.file_id = f.id AND ft.selected_route = 'NODE'
			  )
		`, prepared.GroupID, startOfUTCDay(prepared.CreatedAt), prepared.CreatedAt).Scan(&groupUsage, &groupDailyUsage)
		if err != nil {
			return err
		}
		if exceeds(groupUsage, requestedBytes, groupQuota) || exceeds(groupDailyUsage, requestedBytes, groupDailyLimit) {
			return transfer.ErrQuotaExceeded
		}
	}
	projectedUsage := nodeUsage + requestedBytes
	if projectedUsage >= nodeLimit*int64(warningPercent)/100 {
		_, err = tx.Exec(ctx, `
			INSERT INTO audit_logs (actor_user_id, actor_device_id, action, target_type, metadata)
			VALUES ($1, $2, 'STORAGE_WARNING', 'NODE', jsonb_build_object(
				'bytesUsed', $3::bigint, 'byteLimit', $4::bigint, 'warningPercent', $5::integer
			))
		`, session.ID, session.DeviceID, projectedUsage, nodeLimit, warningPercent)
		if err != nil {
			return err
		}
	}
	return nil
}

func exceeds(current, requested, limit int64) bool {
	return limit < 0 || requested > limit || current > limit-requested
}

func startOfUTCDay(value time.Time) time.Time {
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func (store *Store) ListTransfers(ctx context.Context, session auth.Session) ([]transfer.Transfer, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT DISTINCT t.id::text, t.created_at
		FROM transfer_tasks t
		LEFT JOIN transfer_targets target ON target.transfer_id = t.id
		WHERE t.sender_user_id = $1 OR ($2::uuid IS NOT NULL AND target.target_device_id = $2)
		ORDER BY t.created_at DESC
	`, session.ID, session.DeviceID)
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0)
	for rows.Next() {
		var id string
		var createdAt time.Time
		if err := rows.Scan(&id, &createdAt); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := make([]transfer.Transfer, 0, len(ids))
	for _, id := range ids {
		item, err := store.GetTransfer(ctx, session, id)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, nil
}

func (store *Store) GetTransfer(ctx context.Context, session auth.Session, transferID string) (transfer.Transfer, error) {
	var result transfer.Transfer
	var groupID *string
	err := store.pool.QueryRow(ctx, `
		SELECT t.id::text, t.sender_user_id::text, t.sender_device_id::text, t.target_type,
		       t.group_id::text, t.content_type, m.encrypted_content, t.status, t.created_at, t.expires_at
		FROM transfer_tasks t
		LEFT JOIN messages m ON m.transfer_id = t.id
		WHERE t.id = $1 AND (
			t.sender_user_id = $2 OR ($3::uuid IS NOT NULL AND EXISTS (
				SELECT 1 FROM transfer_targets access_target
				WHERE access_target.transfer_id = t.id AND access_target.target_device_id = $3
			))
		)
	`, transferID, session.ID, session.DeviceID).Scan(
		&result.ID, &result.SenderUserID, &result.SenderDeviceID, &result.TargetType,
		&groupID, &result.ContentType, &result.Content, &result.Status, &result.CreatedAt, &result.ExpiresAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return transfer.Transfer{}, transfer.ErrNotFound
	}
	if err != nil {
		return transfer.Transfer{}, err
	}
	if groupID != nil {
		result.GroupID = *groupID
	}
	keyRows, err := store.pool.Query(ctx, `
		SELECT target_device_id::text, wrapped_content_key
		FROM transfer_content_keys
		WHERE transfer_id = $1 AND ($2 OR target_device_id = $3)
	`, transferID, result.SenderUserID == session.ID, session.DeviceID)
	if err != nil {
		return transfer.Transfer{}, err
	}
	result.WrappedContentKeys = make(map[string][]byte)
	for keyRows.Next() {
		var deviceID string
		var wrappedKey []byte
		if err := keyRows.Scan(&deviceID, &wrappedKey); err != nil {
			keyRows.Close()
			return transfer.Transfer{}, err
		}
		result.WrappedContentKeys[deviceID] = wrappedKey
	}
	err = keyRows.Err()
	keyRows.Close()
	if err != nil {
		return transfer.Transfer{}, err
	}
	fileRows, err := store.pool.Query(ctx, `
		SELECT id::text, original_name, mime_type, size, sha256, chunk_size, chunk_count, expires_at
		FROM files WHERE transfer_id = $1 ORDER BY id
	`, transferID)
	if err != nil {
		return transfer.Transfer{}, err
	}
	fileIndex := make(map[string]int)
	result.Files = make([]transfer.File, 0)
	for fileRows.Next() {
		var file transfer.File
		if err := fileRows.Scan(&file.ID, &file.Name, &file.MIMEType, &file.Size, &file.SHA256, &file.ChunkSize, &file.ChunkCount, &file.ExpiresAt); err != nil {
			fileRows.Close()
			return transfer.Transfer{}, err
		}
		fileIndex[file.ID] = len(result.Files)
		result.Files = append(result.Files, file)
	}
	err = fileRows.Err()
	fileRows.Close()
	if err != nil {
		return transfer.Transfer{}, err
	}
	targetRows, err := store.pool.Query(ctx, `
		SELECT target_device_id::text, selected_route, status, bytes_transferred
		FROM transfer_targets WHERE transfer_id = $1 ORDER BY target_device_id
	`, transferID)
	if err != nil {
		return transfer.Transfer{}, err
	}
	result.Targets = make([]transfer.Target, 0)
	for targetRows.Next() {
		var target transfer.Target
		if err := targetRows.Scan(&target.DeviceID, &target.SelectedRoute, &target.Status, &target.BytesTransferred); err != nil {
			targetRows.Close()
			return transfer.Transfer{}, err
		}
		result.Targets = append(result.Targets, target)
	}
	err = targetRows.Err()
	targetRows.Close()
	if err != nil {
		return transfer.Transfer{}, err
	}
	if len(result.Files) > 0 {
		rows, err := store.pool.Query(ctx, `
			SELECT ft.file_id::text, ft.target_device_id::text, ft.selected_route, ft.status
			FROM transfer_file_targets ft JOIN files f ON f.id = ft.file_id
			WHERE f.transfer_id = $1 ORDER BY ft.file_id, ft.target_device_id
		`, transferID)
		if err != nil {
			return transfer.Transfer{}, err
		}
		for rows.Next() {
			var fileID string
			var target transfer.FileTarget
			if err := rows.Scan(&fileID, &target.DeviceID, &target.SelectedRoute, &target.Status); err != nil {
				rows.Close()
				return transfer.Transfer{}, err
			}
			target.FileIndex = fileIndex[fileID]
			result.FileTargets = append(result.FileTargets, target)
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return transfer.Transfer{}, err
		}
	}
	return result, nil
}

func (store *Store) CancelTransfer(ctx context.Context, session auth.Session, transferID string, now time.Time) (transfer.Transfer, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return transfer.Transfer{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE transfer_tasks SET status = 'CANCELLED'
		WHERE id = $1 AND sender_user_id = $2 AND status NOT IN ('DELIVERED', 'READ', 'FAILED', 'CANCELLED', 'EXPIRED')
	`, transferID, session.ID)
	if err != nil {
		return transfer.Transfer{}, err
	}
	if command.RowsAffected() == 0 {
		return transfer.Transfer{}, transfer.ErrForbidden
	}
	_, err = tx.Exec(ctx, `
		UPDATE transfer_targets SET status = 'CANCELLED', completed_at = $2
		WHERE transfer_id = $1 AND status NOT IN ('DELIVERED', 'READ', 'FAILED', 'CANCELLED', 'EXPIRED')
	`, transferID, now)
	if err != nil {
		return transfer.Transfer{}, err
	}
	_, err = tx.Exec(ctx, `
		UPDATE transfer_file_targets SET status = 'CANCELLED'
		WHERE file_id IN (SELECT id FROM files WHERE transfer_id = $1)
		  AND status NOT IN ('DELIVERED', 'READ', 'FAILED', 'CANCELLED', 'EXPIRED')
	`, transferID)
	if err != nil {
		return transfer.Transfer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return transfer.Transfer{}, err
	}
	return store.GetTransfer(ctx, session, transferID)
}

func (store *Store) ReadTransfer(ctx context.Context, session auth.Session, transferID string, now time.Time) (transfer.Transfer, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return transfer.Transfer{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE transfer_targets SET status = 'READ', read_at = $3
		WHERE transfer_id = $1 AND target_device_id = $2 AND status = 'DELIVERED'
	`, transferID, session.DeviceID, now)
	if err != nil {
		return transfer.Transfer{}, err
	}
	if command.RowsAffected() == 0 {
		return transfer.Transfer{}, transfer.ErrForbidden
	}
	_, err = tx.Exec(ctx, `
		INSERT INTO delivery_status (transfer_id, device_id, delivered_at, read_at)
		VALUES ($1, $2, $3, $3)
		ON CONFLICT (transfer_id, device_id) DO UPDATE SET read_at = EXCLUDED.read_at
	`, transferID, session.DeviceID, now)
	if err != nil {
		return transfer.Transfer{}, err
	}
	if err := tx.Commit(ctx); err != nil {
		return transfer.Transfer{}, err
	}
	return store.GetTransfer(ctx, session, transferID)
}

func (store *Store) PrepareChunkUpload(ctx context.Context, session auth.Session, fileID string, index int) (filetransfer.FileRecord, *filetransfer.ChunkRecord, error) {
	var file filetransfer.FileRecord
	err := store.pool.QueryRow(ctx, `
		SELECT f.id::text, f.size, f.sha256, f.chunk_size, f.chunk_count, f.status, f.storage_path
		FROM files f
		JOIN transfer_tasks t ON t.id = f.transfer_id
		WHERE f.id = $1 AND t.sender_user_id = $2 AND t.sender_device_id = $3
		  AND f.status = 'UPLOADING'
		  AND EXISTS (
		      SELECT 1 FROM transfer_file_targets ft
		      WHERE ft.file_id = f.id AND ft.selected_route = 'NODE'
		  )
	`, fileID, session.ID, session.DeviceID).Scan(
		&file.ID, &file.Size, &file.SHA256, &file.ChunkSize, &file.ChunkCount, &file.Status, &file.StoragePath,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return filetransfer.FileRecord{}, nil, filetransfer.ErrForbidden
	}
	if err != nil {
		return filetransfer.FileRecord{}, nil, err
	}
	var chunk filetransfer.ChunkRecord
	err = store.pool.QueryRow(ctx, `
		SELECT file_id::text, chunk_index, size, sha256, storage_path, completed_at
		FROM file_chunks WHERE file_id = $1 AND chunk_index = $2 AND status = 'COMPLETE'
	`, fileID, index).Scan(&chunk.FileID, &chunk.Index, &chunk.Size, &chunk.SHA256, &chunk.StoragePath, &chunk.CompletedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return file, nil, nil
	}
	if err != nil {
		return filetransfer.FileRecord{}, nil, err
	}
	return file, &chunk, nil
}

func (store *Store) RecordChunk(ctx context.Context, session auth.Session, chunk filetransfer.ChunkRecord) error {
	command, err := store.pool.Exec(ctx, `
		INSERT INTO file_chunks (
			file_id, chunk_index, size, sha256, status, storage_path, completed_at
		)
		SELECT f.id, $2, $3, $4, 'COMPLETE', $5, $6
		FROM files f JOIN transfer_tasks t ON t.id = f.transfer_id
		WHERE f.id = $1 AND t.sender_user_id = $7 AND t.sender_device_id = $8
		  AND f.status = 'UPLOADING'
		ON CONFLICT (file_id, chunk_index) DO NOTHING
	`, chunk.FileID, chunk.Index, chunk.Size, chunk.SHA256, chunk.StoragePath, chunk.CompletedAt, session.ID, session.DeviceID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 1 {
		return nil
	}
	var existingHash []byte
	err = store.pool.QueryRow(ctx, `
		SELECT sha256 FROM file_chunks WHERE file_id = $1 AND chunk_index = $2 AND status = 'COMPLETE'
	`, chunk.FileID, chunk.Index).Scan(&existingHash)
	if err == nil && subtle.ConstantTimeCompare(existingHash, chunk.SHA256) == 1 {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) || err == nil {
		return filetransfer.ErrConflict
	}
	return err
}

func (store *Store) OpenChunk(ctx context.Context, session auth.Session, fileID string, index int) (filetransfer.ChunkRecord, error) {
	var chunk filetransfer.ChunkRecord
	err := store.pool.QueryRow(ctx, `
		SELECT c.file_id::text, c.chunk_index, c.size, c.sha256, c.storage_path, c.completed_at
		FROM file_chunks c
		JOIN transfer_file_targets ft ON ft.file_id = c.file_id
		WHERE c.file_id = $1 AND c.chunk_index = $2 AND c.status = 'COMPLETE'
		  AND ft.target_device_id = $3 AND ft.selected_route = 'NODE'
	`, fileID, index, session.DeviceID).Scan(
		&chunk.FileID, &chunk.Index, &chunk.Size, &chunk.SHA256, &chunk.StoragePath, &chunk.CompletedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return filetransfer.ChunkRecord{}, filetransfer.ErrNotFound
	}
	return chunk, err
}

func (store *Store) PrepareFileCompletion(ctx context.Context, session auth.Session, fileID string) (filetransfer.FileRecord, []filetransfer.ChunkRecord, error) {
	file, _, err := store.PrepareChunkUpload(ctx, session, fileID, 0)
	if err != nil {
		return filetransfer.FileRecord{}, nil, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT file_id::text, chunk_index, size, sha256, storage_path, completed_at
		FROM file_chunks WHERE file_id = $1 AND status = 'COMPLETE'
		ORDER BY chunk_index
	`, fileID)
	if err != nil {
		return filetransfer.FileRecord{}, nil, err
	}
	defer rows.Close()
	chunks := make([]filetransfer.ChunkRecord, 0, file.ChunkCount)
	for rows.Next() {
		var chunk filetransfer.ChunkRecord
		if err := rows.Scan(&chunk.FileID, &chunk.Index, &chunk.Size, &chunk.SHA256, &chunk.StoragePath, &chunk.CompletedAt); err != nil {
			return filetransfer.FileRecord{}, nil, err
		}
		chunks = append(chunks, chunk)
	}
	return file, chunks, rows.Err()
}

func (store *Store) CompleteFile(ctx context.Context, session auth.Session, fileID, storagePath string, completedAt time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	command, err := tx.Exec(ctx, `
		UPDATE files f SET status = 'AVAILABLE_ON_NODE', storage_path = $2, completed_at = $3
		FROM transfer_tasks t
		WHERE f.id = $1 AND t.id = f.transfer_id
		  AND t.sender_user_id = $4 AND t.sender_device_id = $5
		  AND f.status = 'UPLOADING'
	`, fileID, storagePath, completedAt, session.ID, session.DeviceID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return filetransfer.ErrConflict
	}
	_, err = tx.Exec(ctx, `
		UPDATE transfer_file_targets SET status = 'AVAILABLE_ON_NODE'
		WHERE file_id = $1 AND selected_route = 'NODE'
	`, fileID)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `
		UPDATE transfer_targets target SET status = 'AVAILABLE_ON_NODE'
		WHERE target.transfer_id = (SELECT transfer_id FROM files WHERE id = $1)
		  AND target.selected_route = 'NODE'
	`, fileID)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (store *Store) ConnectDevice(ctx context.Context, deviceID string, connectedAt time.Time, protocolVersion, clientVersion string) error {
	command, err := store.pool.Exec(ctx, `
		INSERT INTO device_connections (
			device_id, connected_at, last_seen_at, disconnected_at, protocol_version, client_version
		)
		SELECT id, $2, $2, NULL, $3, $4
		FROM devices
		WHERE id = $1 AND trust_status = 'TRUSTED' AND revoked_at IS NULL
		ON CONFLICT (device_id) DO UPDATE SET
			connected_at = EXCLUDED.connected_at,
			last_seen_at = EXCLUDED.last_seen_at,
			disconnected_at = NULL,
			protocol_version = EXCLUDED.protocol_version,
			client_version = EXCLUDED.client_version
	`, deviceID, connectedAt, protocolVersion, clientVersion)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return device.ErrForbidden
	}
	return nil
}

func (store *Store) HeartbeatDevice(ctx context.Context, deviceID string, seenAt time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE device_connections connection SET last_seen_at = $2
		FROM devices d
		WHERE connection.device_id = $1 AND d.id = connection.device_id
		  AND connection.disconnected_at IS NULL
		  AND d.trust_status = 'TRUSTED' AND d.revoked_at IS NULL
	`, deviceID, seenAt)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return device.ErrNotFound
	}
	return nil
}

func (store *Store) DisconnectDevice(ctx context.Context, deviceID string, disconnectedAt time.Time) error {
	_, err := store.pool.Exec(ctx, `
		UPDATE device_connections SET disconnected_at = $2
		WHERE device_id = $1 AND disconnected_at IS NULL
	`, deviceID, disconnectedAt)
	return err
}

func (store *Store) PendingNotifications(ctx context.Context, deviceID string) ([]presence.Notification, error) {
	var available bool
	err := store.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM devices
			WHERE id = $1 AND trust_status = 'TRUSTED' AND revoked_at IS NULL
		)
	`, deviceID).Scan(&available)
	if err != nil {
		return nil, err
	}
	if !available {
		return nil, device.ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, notification_type, payload
		FROM notifications
		WHERE device_id = $1 AND acknowledged_at IS NULL
		ORDER BY created_at
		LIMIT 100
	`, deviceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]presence.Notification, 0)
	for rows.Next() {
		var notification presence.Notification
		if err := rows.Scan(&notification.ID, &notification.Type, &notification.Payload); err != nil {
			return nil, err
		}
		result = append(result, notification)
	}
	return result, rows.Err()
}

func (store *Store) AcknowledgeNotification(ctx context.Context, deviceID, notificationID string, acknowledgedAt time.Time) error {
	command, err := store.pool.Exec(ctx, `
		UPDATE notifications SET acknowledged_at = $3
		WHERE id = $1 AND device_id = $2 AND acknowledged_at IS NULL
	`, notificationID, deviceID, acknowledgedAt)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return device.ErrNotFound
	}
	return nil
}

func (store *Store) InsertMetrics(ctx context.Context, session auth.Session, metrics []analytics.Metric) (analytics.BatchResult, error) {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return analytics.BatchResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	result := analytics.BatchResult{}
	for _, metric := range metrics {
		var receiver any
		if metric.ReceiverDeviceID != "" {
			receiver = metric.ReceiverDeviceID
		}
		var groupID any
		if metric.GroupID != "" {
			groupID = metric.GroupID
		}
		command, err := tx.Exec(ctx, `
			INSERT INTO transfer_metrics (
				event_id, transfer_id, sender_user_id, sender_device_id, receiver_device_id, group_id,
				content_type, route, file_size, started_at, completed_at,
				average_bytes_per_second, retry_count, succeeded, error_code
			)
			SELECT $1, $2, $15, sender.id, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14
			FROM devices sender
			WHERE sender.id = $3 AND sender.user_id = $15
			  AND sender.trust_status = 'TRUSTED' AND sender.revoked_at IS NULL
			  AND ($4::uuid IS NULL OR EXISTS (SELECT 1 FROM devices WHERE id = $4))
			  AND ($5::uuid IS NULL OR EXISTS (
			      SELECT 1 FROM group_members WHERE group_id = $5 AND user_id = $15
			  ))
			ON CONFLICT (event_id) DO NOTHING
		`, metric.EventID, metric.TransferID, metric.SenderDeviceID, receiver, groupID,
			metric.ContentType, metric.Route, metric.FileSize, metric.StartedAt, metric.CompletedAt,
			metric.AverageBytesPerSecond, metric.RetryCount, metric.Succeeded, metric.ErrorCode, session.ID)
		if err != nil {
			return analytics.BatchResult{}, err
		}
		if command.RowsAffected() == 1 {
			result.Accepted++
		} else {
			var exists bool
			if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM transfer_metrics WHERE event_id = $1)`, metric.EventID).Scan(&exists); err != nil {
				return analytics.BatchResult{}, err
			}
			if !exists {
				return analytics.BatchResult{}, analytics.ErrForbidden
			}
			result.Duplicates++
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return analytics.BatchResult{}, err
	}
	return result, nil
}

func (store *Store) AnalyticsOverview(ctx context.Context, session auth.Session, timeRange analytics.TimeRange) (analytics.Overview, error) {
	result := analytics.Overview{RouteCounts: make(map[string]int64), RouteBytes: make(map[string]int64)}
	err := store.pool.QueryRow(ctx, `
		SELECT COUNT(*), COALESCE(SUM(m.file_size), 0),
		       COUNT(*) FILTER (WHERE m.succeeded), COUNT(*) FILTER (WHERE NOT m.succeeded)
		FROM transfer_metrics m
		WHERE m.sender_user_id = $1 AND m.started_at >= $2 AND m.started_at < $3
	`, session.ID, timeRange.From, timeRange.To).Scan(&result.TransferCount, &result.TotalBytes, &result.Succeeded, &result.Failed)
	if err != nil {
		return analytics.Overview{}, err
	}
	rows, err := store.pool.Query(ctx, `
		SELECT m.route, COUNT(*), COALESCE(SUM(m.file_size), 0)
		FROM transfer_metrics m
		WHERE m.sender_user_id = $1 AND m.started_at >= $2 AND m.started_at < $3
		GROUP BY m.route
	`, session.ID, timeRange.From, timeRange.To)
	if err != nil {
		return analytics.Overview{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var route string
		var count, bytes int64
		if err := rows.Scan(&route, &count, &bytes); err != nil {
			return analytics.Overview{}, err
		}
		result.RouteCounts[route] = count
		result.RouteBytes[route] = bytes
	}
	return result, rows.Err()
}

func (store *Store) DailyTransfers(ctx context.Context, session auth.Session, timeRange analytics.TimeRange) ([]analytics.DailyTransfer, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT date_trunc('day', m.started_at AT TIME ZONE 'UTC')::date::text,
		       COUNT(*), COALESCE(SUM(m.file_size), 0),
		       COALESCE(SUM(m.file_size) FILTER (WHERE m.route = 'LAN'), 0),
		       COALESCE(SUM(m.file_size) FILTER (WHERE m.route = 'NODE'), 0),
		       COUNT(*) FILTER (WHERE NOT m.succeeded)
		FROM transfer_metrics m
		WHERE m.sender_user_id = $1 AND m.started_at >= $2 AND m.started_at < $3
		GROUP BY 1 ORDER BY 1
	`, session.ID, timeRange.From, timeRange.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]analytics.DailyTransfer, 0)
	for rows.Next() {
		var item analytics.DailyTransfer
		if err := rows.Scan(&item.Date, &item.Count, &item.TotalBytes, &item.LANBytes, &item.NodeBytes, &item.Failed); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) DeviceStatistics(ctx context.Context, session auth.Session, timeRange analytics.TimeRange) ([]analytics.DeviceStatistic, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT d.id::text, d.display_name,
		       COUNT(m.event_id) FILTER (WHERE m.sender_device_id = d.id),
		       COUNT(m.event_id) FILTER (WHERE m.receiver_device_id = d.id),
		       COALESCE(SUM(m.file_size) FILTER (WHERE m.sender_device_id = d.id), 0),
		       COALESCE(SUM(m.file_size) FILTER (WHERE m.receiver_device_id = d.id), 0),
		       COALESCE(AVG(m.average_bytes_per_second) FILTER (WHERE m.sender_device_id = d.id), 0)
		FROM devices d
		LEFT JOIN transfer_metrics m ON (m.sender_device_id = d.id OR m.receiver_device_id = d.id)
		  AND m.started_at >= $2 AND m.started_at < $3
		WHERE d.user_id = $1
		GROUP BY d.id, d.display_name ORDER BY d.display_name
	`, session.ID, timeRange.From, timeRange.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]analytics.DeviceStatistic, 0)
	for rows.Next() {
		var item analytics.DeviceStatistic
		if err := rows.Scan(&item.DeviceID, &item.DisplayName, &item.SentCount, &item.ReceivedCount, &item.SentBytes, &item.ReceivedBytes, &item.AverageSpeed); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) GroupStatistics(ctx context.Context, session auth.Session, timeRange analytics.TimeRange) ([]analytics.GroupStatistic, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT g.id::text, g.name,
		       COUNT(m.event_id) FILTER (WHERE m.content_type IN ('TEXT', 'URL', 'NOTIFICATION')),
		       COUNT(m.event_id) FILTER (WHERE m.content_type IN ('FILE', 'IMAGE')),
		       COALESCE(SUM(m.file_size), 0),
		       COUNT(DISTINCT m.sender_device_id),
		       COUNT(DISTINCT sender.user_id)
		FROM groups g
		JOIN group_members membership ON membership.group_id = g.id AND membership.user_id = $1
		LEFT JOIN transfer_metrics m ON m.group_id = g.id AND m.started_at >= $2 AND m.started_at < $3
		LEFT JOIN devices sender ON sender.id = m.sender_device_id
		GROUP BY g.id, g.name ORDER BY g.name
	`, session.ID, timeRange.From, timeRange.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]analytics.GroupStatistic, 0)
	for rows.Next() {
		var item analytics.GroupStatistic
		if err := rows.Scan(&item.GroupID, &item.Name, &item.MessageCount, &item.FileCount, &item.TransferBytes, &item.ActiveDevices, &item.ActiveUsers); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) NodeStatistics(ctx context.Context, session auth.Session, timeRange analytics.TimeRange) ([]analytics.NodeMetric, error) {
	if !session.Admin {
		return nil, analytics.ErrForbidden
	}
	rows, err := store.pool.Query(ctx, `
		SELECT recorded_at, cpu_percent, memory_bytes, disk_bytes, cache_bytes,
		       network_upload_bytes, network_download_bytes, online_devices, active_transfers
		FROM system_metrics WHERE recorded_at >= $1 AND recorded_at < $2 ORDER BY recorded_at
	`, timeRange.From, timeRange.To)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]analytics.NodeMetric, 0)
	for rows.Next() {
		var item analytics.NodeMetric
		if err := rows.Scan(&item.RecordedAt, &item.CPUPercent, &item.MemoryBytes, &item.DiskBytes, &item.CacheBytes, &item.NetworkUploadBytes, &item.NetworkDownloadBytes, &item.OnlineDevices, &item.ActiveTransfers); err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, rows.Err()
}

func (store *Store) ExpiredFiles(ctx context.Context, now time.Time, limit int) ([]maintenance.ExpiredFile, error) {
	rows, err := store.pool.Query(ctx, `
		SELECT id::text, storage_path
		FROM files
		WHERE expires_at <= $1 AND status <> 'EXPIRED'
		ORDER BY expires_at
		LIMIT $2
	`, now, limit)
	if err != nil {
		return nil, err
	}
	result := make([]maintenance.ExpiredFile, 0)
	for rows.Next() {
		var file maintenance.ExpiredFile
		if err := rows.Scan(&file.ID, &file.StoragePath); err != nil {
			rows.Close()
			return nil, err
		}
		result = append(result, file)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	for index := range result {
		chunkRows, err := store.pool.Query(ctx, `SELECT storage_path FROM file_chunks WHERE file_id = $1`, result[index].ID)
		if err != nil {
			return nil, err
		}
		for chunkRows.Next() {
			var path string
			if err := chunkRows.Scan(&path); err != nil {
				chunkRows.Close()
				return nil, err
			}
			result[index].ChunkPaths = append(result[index].ChunkPaths, path)
		}
		err = chunkRows.Err()
		chunkRows.Close()
		if err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (store *Store) MarkFileExpired(ctx context.Context, fileID string, expiredAt time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var transferID string
	err = tx.QueryRow(ctx, `
		UPDATE files SET status = 'EXPIRED', storage_path = '', completed_at = NULL
		WHERE id = $1 AND status <> 'EXPIRED'
		RETURNING transfer_id::text
	`, fileID).Scan(&transferID)
	if errors.Is(err, pgx.ErrNoRows) {
		return filetransfer.ErrNotFound
	}
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM file_chunks WHERE file_id = $1`, fileID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE transfer_file_targets SET status = 'EXPIRED' WHERE file_id = $1`, fileID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transfer_targets target SET status = 'EXPIRED', completed_at = $2
		WHERE target.transfer_id = $1
		  AND NOT EXISTS (
		      SELECT 1 FROM files f
		      JOIN transfer_file_targets ft ON ft.file_id = f.id AND ft.target_device_id = target.target_device_id
		      WHERE f.transfer_id = target.transfer_id AND f.status <> 'EXPIRED'
		  )
	`, transferID, expiredAt); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE transfer_tasks SET status = 'EXPIRED'
		WHERE id = $1 AND NOT EXISTS (
			SELECT 1 FROM transfer_targets WHERE transfer_id = $1 AND status <> 'EXPIRED'
		)
	`, transferID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

type rowScanner interface {
	Scan(...any) error
}

func scanDevice(row rowScanner) (device.Device, error) {
	var result device.Device
	err := row.Scan(
		&result.ID,
		&result.DisplayName,
		&result.Type,
		&result.PublicKey,
		&result.Algorithm,
		&result.TrustStatus,
		&result.RevokedAt,
		&result.CreatedAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return device.Device{}, device.ErrNotFound
	}
	return result, err
}
