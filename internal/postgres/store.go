package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
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
