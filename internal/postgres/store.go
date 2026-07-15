package postgres

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"nexdrop/internal/auth"
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
