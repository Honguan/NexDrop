package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"nexdrop/internal/admin"
	"nexdrop/internal/auth"
)

func TestAdminManagementIntegration(t *testing.T) {
	databaseURL := os.Getenv("NEXDROP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEXDROP_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	suffix := fmt.Sprint(time.Now().UnixNano())
	var actorID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin)
		VALUES ($1, $2, 'unused', true) RETURNING id::text
	`, "admin-"+suffix, "admin-"+suffix+"@example.com").Scan(&actorID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(ctx, `DELETE FROM audit_logs WHERE actor_user_id=$1`, actorID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM storage_quotas WHERE owner_id IN (SELECT id FROM users WHERE username IN ($1,$2))`, "admin-"+suffix, "user-"+suffix)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE username IN ($1,$2)`, "admin-"+suffix, "user-"+suffix)
	}()
	actor := auth.Session{User: auth.User{ID: actorID, Admin: true}}
	service := admin.NewService(store)

	created, err := service.CreateUser(ctx, actor, "user-"+suffix, "user-"+suffix+"@example.com", "integration-password", false)
	if err != nil {
		t.Fatal(err)
	}
	users, err := service.Users(ctx, actor, 50, 0)
	if err != nil || len(users) < 2 {
		t.Fatalf("Users() = %+v, %v", users, err)
	}
	settings, err := service.Settings(ctx, actor)
	if err != nil || settings.SingleFileLimitBytes <= 0 {
		t.Fatalf("Settings() = %+v, %v", settings, err)
	}
	quota, err := service.SetQuota(ctx, actor, admin.Quota{OwnerType: "USER", OwnerID: created.ID, ByteLimit: 2048, DailyTransferLimit: 1024})
	if err != nil || quota.ByteLimit != 2048 {
		t.Fatalf("SetQuota() = %+v, %v", quota, err)
	}
	if _, err := service.Storage(ctx, actor); err != nil {
		t.Fatalf("Storage() error = %v", err)
	}
	if failures, err := service.Failures(ctx, actor, 50, 0); err != nil || failures == nil {
		t.Fatalf("Failures() = %+v, %v", failures, err)
	}
	if err := service.ResetPasswordByIdentifier(ctx, created.Username, "cli-reset-password"); err != nil {
		t.Fatalf("ResetPasswordByIdentifier() error = %v", err)
	}

	accessHash := []byte("admin-integration-access-" + suffix)
	_, err = store.pool.Exec(ctx, `
		INSERT INTO user_sessions (user_id, access_token_hash, access_expires_at, refresh_token_hash, expires_at)
		VALUES ($1, $2, now()+interval '1 hour', $3, now()+interval '1 day')
	`, created.ID, accessHash, []byte("admin-integration-refresh-"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	if err := service.ResetPassword(ctx, actor, created.ID, "replacement-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := store.SessionByAccessToken(ctx, accessHash, time.Now().UTC()); err == nil {
		t.Fatal("password reset did not revoke sessions")
	}
	if err := service.DisableUser(ctx, actor, created.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CredentialByIdentifier(ctx, created.Username); err == nil {
		t.Fatal("disabled user can still authenticate")
	}
	logs, err := service.AuditLogs(ctx, actor, 50, 0)
	if err != nil || len(logs) < 4 {
		t.Fatalf("AuditLogs() = %+v, %v", logs, err)
	}
}
