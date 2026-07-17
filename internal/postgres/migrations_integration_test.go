package postgres

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

func TestMigrationsUpgradeAndRollbackIntegration(t *testing.T) {
	databaseURL := os.Getenv("NEXDROP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEXDROP_TEST_DATABASE_URL is not set")
	}

	ctx := context.Background()
	admin, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	databaseName := fmt.Sprintf("nexdrop_migrations_%d", time.Now().UnixNano())
	identifier := pgx.Identifier{databaseName}.Sanitize()
	if _, err := admin.pool.Exec(ctx, "CREATE DATABASE "+identifier); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := admin.pool.Exec(ctx, "DROP DATABASE "+identifier+" WITH (FORCE)"); err != nil {
			t.Errorf("drop migration database: %v", err)
		}
	}()

	testURL, err := url.Parse(databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	testURL.Path = "/" + databaseName
	store, err := Open(ctx, testURL.String())
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	migrationsDirectory := filepath.Join("..", "..", "migrations")
	initialDirectory := t.TempDir()
	initial, err := os.ReadFile(filepath.Join(migrationsDirectory, "001_initial.sql"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(initialDirectory, "001_initial.sql"), initial, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyMigrations(ctx, initialDirectory); err != nil {
		t.Fatalf("apply initial migration: %v", err)
	}

	const username = "migration-sentinel"
	if _, err := store.pool.Exec(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, 'unused')
	`, username, username+"@example.invalid"); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyMigrations(ctx, migrationsDirectory); err != nil {
		t.Fatalf("upgrade migrations: %v", err)
	}
	if err := store.ApplyMigrations(ctx, migrationsDirectory); err != nil {
		t.Fatalf("repeat migrations: %v", err)
	}

	var userCount, migrationCount int
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM users WHERE username = $1`, username).Scan(&userCount); err != nil {
		t.Fatal(err)
	}
	if userCount != 1 {
		t.Fatalf("preserved users = %d, want 1", userCount)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(migrationsDirectory)
	if err != nil {
		t.Fatal(err)
	}
	wantMigrations := 0
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			wantMigrations++
		}
	}
	if migrationCount != wantMigrations {
		t.Fatalf("migration history = %d, want %d", migrationCount, wantMigrations)
	}

	brokenDirectory := t.TempDir()
	if err := os.WriteFile(filepath.Join(brokenDirectory, "010_broken.sql"), []byte(`
		CREATE TABLE migration_must_rollback (id integer PRIMARY KEY);
		SELECT nexdrop_missing_migration_function();
	`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyMigrations(ctx, brokenDirectory); err == nil {
		t.Fatal("broken migration unexpectedly succeeded")
	}
	var tableExists, historyExists bool
	if err := store.pool.QueryRow(ctx, `SELECT to_regclass('public.migration_must_rollback') IS NOT NULL`).Scan(&tableExists); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE filename = '010_broken.sql')`).Scan(&historyExists); err != nil {
		t.Fatal(err)
	}
	if tableExists || historyExists {
		t.Fatalf("failed migration persisted table=%t history=%t", tableExists, historyExists)
	}
}
