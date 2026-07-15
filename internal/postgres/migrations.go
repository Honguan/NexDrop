package postgres

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (store *Store) ApplyMigrations(ctx context.Context, directory string) error {
	entries, err := os.ReadDir(directory)
	if err != nil {
		return fmt.Errorf("read migrations: %w", err)
	}

	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)

	if _, err := store.pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)
	`); err != nil {
		return fmt.Errorf("create migration history: %w", err)
	}

	var hasUsers bool
	if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.users') IS NOT NULL`).Scan(&hasUsers); err != nil {
		return fmt.Errorf("inspect existing schema: %w", err)
	}
	if hasUsers {
		if _, err := store.pool.Exec(ctx, `
			INSERT INTO schema_migrations (filename)
			VALUES ('001_initial.sql')
			ON CONFLICT (filename) DO NOTHING
		`); err != nil {
			return fmt.Errorf("record initial migration: %w", err)
		}
	}

	for _, name := range names {
		var applied bool
		if err := store.pool.QueryRow(ctx, `
			SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = $1)
		`, name).Scan(&applied); err != nil {
			return fmt.Errorf("inspect migration %s: %w", name, err)
		}
		if applied {
			continue
		}

		script, err := os.ReadFile(filepath.Join(directory, name))
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := store.pool.Begin(ctx)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, string(script)); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.Exec(ctx, `INSERT INTO schema_migrations (filename) VALUES ($1)`, name); err != nil {
			_ = tx.Rollback(ctx)
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(ctx); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}
