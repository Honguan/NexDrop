package postgres

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"os"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

func TestDeviceSessionAttachmentIntegration(t *testing.T) {
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
	var userID, deviceID, sessionID string
	err = store.pool.QueryRow(ctx, `INSERT INTO users (username,email,password_hash) VALUES ($1,$2,'unused') RETURNING id::text`, "attach-"+suffix, "attach-"+suffix+"@example.com").Scan(&userID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE id=$1`, userID) }()
	key, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	err = store.pool.QueryRow(ctx, `INSERT INTO devices (user_id,display_name,device_type,trust_status) VALUES ($1,'Web','WEB_CHROME','TRUSTED') RETURNING id::text`, userID).Scan(&deviceID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `INSERT INTO device_keys (device_id,public_key,key_algorithm) VALUES ($1,$2,'X25519')`, deviceID, key.PublicKey().Bytes()); err != nil {
		t.Fatal(err)
	}
	accessHash := []byte("attach-access-" + suffix)
	err = store.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (user_id,access_token_hash,access_expires_at,refresh_token_hash,expires_at)
		VALUES ($1,$2,now()+interval '1 hour',$3,now()+interval '1 day') RETURNING id::text
	`, userID, accessHash, []byte("attach-refresh-"+suffix)).Scan(&sessionID)
	if err != nil {
		t.Fatal(err)
	}
	session := auth.Session{User: auth.User{ID: userID}, SessionID: sessionID}
	service := device.NewService(store)
	challenge, err := service.CreateSessionChallenge(ctx, session, deviceID)
	if err != nil {
		t.Fatal(err)
	}
	ephemeral, err := ecdh.X25519().NewPublicKey(challenge.EphemeralPublicKey)
	if err != nil {
		t.Fatal(err)
	}
	shared, err := key.ECDH(ephemeral)
	if err != nil {
		t.Fatal(err)
	}
	mac := hmac.New(sha256.New, shared)
	_, _ = mac.Write([]byte("nexdrop/session-attach/v1"))
	_, _ = mac.Write([]byte(sessionID))
	_, _ = mac.Write(challenge.Nonce)
	if err := service.AttachSession(ctx, session, deviceID, challenge.ID, mac.Sum(nil)); err != nil {
		t.Fatal(err)
	}
	attached, err := store.SessionByAccessToken(ctx, accessHash, time.Now().UTC())
	if err != nil || attached.DeviceID == nil || *attached.DeviceID != deviceID {
		t.Fatalf("attached session = %+v, %v", attached, err)
	}
}
