package postgres

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"
)

func TestRestoreSecurityIntegration(t *testing.T) {
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
	var userID, deviceID string
	err = store.pool.QueryRow(ctx, `INSERT INTO users (username,email,password_hash) VALUES ($1,$2,'unused') RETURNING id::text`, "restore-"+suffix, "restore-"+suffix+"@example.com").Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID) }()
	err = store.pool.QueryRow(ctx, `INSERT INTO devices (user_id,display_name,device_type,trust_status) VALUES ($1,'device','LINUX','TRUSTED') RETURNING id::text`, userID).Scan(&deviceID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.pool.Exec(ctx, `INSERT INTO user_sessions (user_id,device_id,access_token_hash,access_expires_at,refresh_token_hash,expires_at) VALUES ($1,$2,$3,now()+interval '1 hour',$4,now()+interval '1 day')`, userID, deviceID, []byte("restore-access-"+suffix), []byte("restore-refresh-"+suffix))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := store.ProtectRestoredSecurity(ctx, []string{deviceID}, now); err != nil {
		t.Fatal(err)
	}
	var deviceRevoked, sessionRevoked *time.Time
	err = store.pool.QueryRow(ctx, `SELECT d.revoked_at,s.revoked_at FROM devices d JOIN user_sessions s ON s.device_id=d.id WHERE d.id=$1`, deviceID).Scan(&deviceRevoked, &sessionRevoked)
	if err != nil {
		t.Fatal(err)
	}
	if deviceRevoked == nil || sessionRevoked == nil {
		t.Fatalf("device revoked = %v, session revoked = %v", deviceRevoked, sessionRevoked)
	}
	ids, err := store.RevokedDeviceIDs(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, id := range ids {
		if id == deviceID {
			found = true
		}
	}
	if !found {
		t.Fatalf("revoked IDs = %v", ids)
	}
}
