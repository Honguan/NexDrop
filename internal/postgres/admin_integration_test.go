package postgres

import (
	"context"
	"errors"
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
		_, _ = store.pool.Exec(ctx, `DELETE FROM audit_logs WHERE actor_user_id=$1 OR target_id IN (SELECT id FROM users WHERE username IN ($2,$3,$4)) OR target_id IN (SELECT id FROM user_invitations WHERE invited_by=$1)`, actorID, "admin-"+suffix, "user-"+suffix, "invited-"+suffix)
		_, _ = store.pool.Exec(ctx, `DELETE FROM storage_quotas WHERE owner_id IN (SELECT id FROM users WHERE username IN ($1,$2,$3))`, "admin-"+suffix, "user-"+suffix, "invited-"+suffix)
		_, _ = store.pool.Exec(ctx, `DELETE FROM transfer_tasks WHERE sender_user_id IN (SELECT id FROM users WHERE username IN ($1,$2,$3))`, "admin-"+suffix, "user-"+suffix, "invited-"+suffix)
		_, _ = store.pool.Exec(ctx, `DELETE FROM user_invitations WHERE invited_by=$1`, actorID)
		_, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE username IN ($1,$2,$3)`, "admin-"+suffix, "user-"+suffix, "invited-"+suffix)
	}()
	actor := auth.Session{User: auth.User{ID: actorID, Admin: true}}
	service := admin.NewService(store)

	created, err := service.CreateUser(ctx, actor, "user-"+suffix, "user-"+suffix+"@example.com", "integration-password", false)
	if err != nil {
		t.Fatal(err)
	}
	invitation, err := service.InviteUser(ctx, actor, "invited-"+suffix, "invited-"+suffix+"@example.com", false)
	if err != nil || invitation.Token == "" {
		t.Fatalf("InviteUser() = %+v, %v", invitation, err)
	}
	accepted, err := service.AcceptInvitation(ctx, invitation.Token, "invited-user-password")
	if err != nil || accepted.Username != "invited-"+suffix {
		t.Fatalf("AcceptInvitation() = %+v, %v", accepted, err)
	}
	if _, err := service.AcceptInvitation(ctx, invitation.Token, "invited-user-password"); !errors.Is(err, admin.ErrNotFound) {
		t.Fatalf("second AcceptInvitation() error = %v, want ErrNotFound", err)
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

	var deviceID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO devices (user_id, display_name, device_type, trust_status)
		VALUES ($1, 'Admin managed device', 'WINDOWS', 'TRUSTED') RETURNING id::text
	`, created.ID).Scan(&deviceID)
	if err != nil {
		t.Fatal(err)
	}
	devices, err := service.Devices(ctx, actor, 50, 0)
	if err != nil || !containsAdminDevice(devices, deviceID) {
		t.Fatalf("Devices() = %+v, %v", devices, err)
	}
	if err := service.RevokeDevice(ctx, actor, deviceID); err != nil {
		t.Fatalf("RevokeDevice() error = %v", err)
	}

	var groupID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO groups (owner_user_id, name) VALUES ($1, $2) RETURNING id::text
	`, created.ID, "group-"+suffix).Scan(&groupID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO group_members (group_id, user_id, role) VALUES ($1, $2, 'OWNER')`, groupID, created.ID); err != nil {
		t.Fatal(err)
	}
	var groupTransferID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO transfer_tasks (sender_user_id, sender_device_id, group_id, target_type, content_type, status)
		VALUES ($1, $2, $3, 'GROUP', 'TEXT', 'DELIVERED') RETURNING id::text
	`, created.ID, deviceID, groupID).Scan(&groupTransferID)
	if err != nil {
		t.Fatal(err)
	}
	groups, err := service.Groups(ctx, actor, 50, 0)
	if err != nil || !containsAdminGroup(groups, groupID) {
		t.Fatalf("Groups() = %+v, %v", groups, err)
	}
	if err := service.DeleteGroup(ctx, actor, groupID); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}
	var detachedGroupID *string
	var groupDeletedAt *time.Time
	if err := store.pool.QueryRow(ctx, `SELECT group_id::text, group_deleted_at FROM transfer_tasks WHERE id=$1`, groupTransferID).Scan(&detachedGroupID, &groupDeletedAt); err != nil {
		t.Fatal(err)
	}
	if detachedGroupID != nil || groupDeletedAt == nil {
		t.Fatalf("deleted group transfer retained group link: group=%v deletedAt=%v", detachedGroupID, groupDeletedAt)
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

func containsAdminDevice(devices []admin.Device, id string) bool {
	for _, device := range devices {
		if device.ID == id {
			return true
		}
	}
	return false
}

func containsAdminGroup(groups []admin.Group, id string) bool {
	for _, group := range groups {
		if group.ID == id && group.MemberCount == 1 {
			return true
		}
	}
	return false
}
