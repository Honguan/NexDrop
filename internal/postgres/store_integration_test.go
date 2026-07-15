package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

func TestDeviceLifecycleIntegration(t *testing.T) {
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

	identifier := fmt.Sprintf("integration-%d", time.Now().UnixNano())
	var userID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash, is_admin)
		VALUES ($1, $2, 'unused', true)
		RETURNING id::text
	`, identifier, identifier+"@example.com").Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, userID) }()

	var sessionID string
	accessHash := []byte("integration-access-hash")
	err = store.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (
			user_id, access_token_hash, access_expires_at, refresh_token_hash, expires_at
		) VALUES ($1, $2, now() + interval '1 hour', $3, now() + interval '1 day')
		RETURNING id::text
	`, userID, accessHash, []byte("integration-refresh-hash")).Scan(&sessionID)
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{User: auth.User{ID: userID, Admin: true}, SessionID: sessionID}

	created, err := store.CreateDevice(ctx, session, "Laptop", device.TypeWindows, make([]byte, 32), "X25519")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListDevices(ctx, userID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListDevices() = %+v, %v", listed, err)
	}
	approved, err := store.ApproveDevice(ctx, session, created.ID)
	if err != nil || approved.TrustStatus != device.TrustTrusted {
		t.Fatalf("ApproveDevice() = %+v, %v", approved, err)
	}
	renamed, err := store.RenameDevice(ctx, userID, created.ID, "Workstation")
	if err != nil || renamed.DisplayName != "Workstation" {
		t.Fatalf("RenameDevice() = %+v, %v", renamed, err)
	}
	revoked, err := store.RevokeDevice(ctx, session, created.ID, time.Now().UTC())
	if err != nil || revoked.TrustStatus != device.TrustRevoked {
		t.Fatalf("RevokeDevice() = %+v, %v", revoked, err)
	}
	if _, err := store.SessionByAccessToken(ctx, accessHash, time.Now().UTC()); err == nil {
		t.Fatal("device revocation did not revoke its session")
	}
}

