package postgres

import (
	"context"
	"crypto/subtle"
	"time"

	"github.com/jackc/pgx/v5"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

func (store *Store) DevicePublicKeyForSession(ctx context.Context, session auth.Session, deviceID string) ([]byte, error) {
	var publicKey []byte
	err := store.pool.QueryRow(ctx, `
		SELECT k.public_key
		FROM devices d JOIN device_keys k ON k.device_id=d.id
		JOIN user_sessions s ON s.id=$2 AND s.user_id=d.user_id AND s.revoked_at IS NULL
		WHERE d.id=$1 AND d.user_id=$3 AND d.trust_status='TRUSTED' AND d.revoked_at IS NULL
		  AND (s.device_id IS NULL OR s.device_id=d.id)
	`, deviceID, session.SessionID, session.ID).Scan(&publicKey)
	if err == pgx.ErrNoRows {
		return nil, device.ErrForbidden
	}
	return publicKey, err
}

func (store *Store) CreateDeviceSessionChallenge(ctx context.Context, session auth.Session, deviceID string, proofHash []byte, expiresAt, now time.Time) (string, error) {
	var id string
	err := store.pool.QueryRow(ctx, `
		INSERT INTO device_session_challenges (device_id,session_id,proof_hash,expires_at,created_at)
		SELECT d.id,s.id,$3,$4,$5
		FROM devices d JOIN user_sessions s ON s.id=$2 AND s.user_id=d.user_id AND s.revoked_at IS NULL
		WHERE d.id=$1 AND d.user_id=$6 AND d.trust_status='TRUSTED' AND d.revoked_at IS NULL
		  AND (s.device_id IS NULL OR s.device_id=d.id)
		RETURNING id::text
	`, deviceID, session.SessionID, proofHash, expiresAt, now, session.ID).Scan(&id)
	if err == pgx.ErrNoRows {
		return "", device.ErrForbidden
	}
	return id, err
}

func (store *Store) RedeemDeviceSessionChallenge(ctx context.Context, session auth.Session, deviceID, challengeID string, proofHash []byte, now time.Time, maximumAttempts int) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	var expected []byte
	var attempts int
	var expiresAt time.Time
	var usedAt *time.Time
	err = tx.QueryRow(ctx, `
		SELECT proof_hash,attempt_count,expires_at,used_at
		FROM device_session_challenges
		WHERE id=$1 AND device_id=$2 AND session_id=$3
		FOR UPDATE
	`, challengeID, deviceID, session.SessionID).Scan(&expected, &attempts, &expiresAt, &usedAt)
	if err == pgx.ErrNoRows {
		return device.ErrForbidden
	}
	if err != nil {
		return err
	}
	if usedAt != nil || !now.Before(expiresAt) || attempts >= maximumAttempts {
		return device.ErrForbidden
	}
	if subtle.ConstantTimeCompare(expected, proofHash) != 1 {
		_, err := tx.Exec(ctx, `UPDATE device_session_challenges SET attempt_count=attempt_count+1 WHERE id=$1`, challengeID)
		if err != nil {
			return err
		}
		if err := tx.Commit(ctx); err != nil {
			return err
		}
		return device.ErrForbidden
	}
	command, err := tx.Exec(ctx, `
		UPDATE user_sessions s SET device_id=$1
		FROM devices d
		WHERE s.id=$2 AND s.user_id=$3 AND s.revoked_at IS NULL
		  AND d.id=$1 AND d.user_id=s.user_id AND d.trust_status='TRUSTED' AND d.revoked_at IS NULL
		  AND (s.device_id IS NULL OR s.device_id=d.id)
	`, deviceID, session.SessionID, session.ID)
	if err != nil {
		return err
	}
	if command.RowsAffected() == 0 {
		return device.ErrForbidden
	}
	if _, err := tx.Exec(ctx, `UPDATE device_session_challenges SET used_at=$2 WHERE id=$1`, challengeID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
