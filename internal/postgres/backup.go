package postgres

import (
	"context"
	"time"
)

func (store *Store) RevokedDeviceIDs(ctx context.Context) ([]string, error) {
	rows, err := store.pool.Query(ctx, `SELECT id::text FROM devices WHERE revoked_at IS NOT NULL`)
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
	return result, rows.Err()
}

func (store *Store) ProtectRestoredSecurity(ctx context.Context, revokedDeviceIDs []string, now time.Time) error {
	tx, err := store.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `UPDATE user_sessions SET revoked_at=$1 WHERE revoked_at IS NULL`, now); err != nil {
		return err
	}
	if len(revokedDeviceIDs) > 0 {
		if _, err := tx.Exec(ctx, `UPDATE devices SET revoked_at=COALESCE(revoked_at,$2) WHERE id=ANY($1::uuid[])`, revokedDeviceIDs, now); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
