package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
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
		WHERE lower(username) = lower($1) OR lower(email) = lower($1)
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
		SELECT s.id::text, u.id::text, u.username, u.email, u.is_admin
		FROM user_sessions s
		JOIN users u ON u.id = s.user_id
		WHERE s.access_token_hash = $1
		  AND s.access_expires_at > $2
		  AND s.revoked_at IS NULL
	`, tokenHash, now).Scan(&session.SessionID, &session.ID, &session.Username, &session.Email, &session.Admin)
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
		SELECT d.id::text, d.user_id::text, d.display_name, d.device_type, gd.added_at
		FROM group_devices gd JOIN devices d ON d.id = gd.device_id
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
		if err := deviceRows.Scan(&item.ID, &item.OwnerUserID, &item.DisplayName, &item.Type, &item.AddedAt); err != nil {
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
		          added_at
	`, groupID, session.ID, deviceID, addedAt).Scan(
		&result.ID, &result.OwnerUserID, &result.DisplayName, &result.Type, &result.AddedAt,
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
