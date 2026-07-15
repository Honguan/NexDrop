package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/domain"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
	"nexdrop/internal/transfer"
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
	session := auth.Session{User: auth.User{ID: userID, Username: identifier, Admin: true}, SessionID: sessionID}

	created, err := store.CreateDevice(ctx, session, "Laptop", device.TypeWindows, make([]byte, 32), "X25519")
	if err != nil {
		t.Fatal(err)
	}
	listed, err := store.ListDevices(ctx, userID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListDevices() = %+v, %v", listed, err)
	}
	codeHash := sha256.Sum256([]byte("123456"))
	challengeID, err := store.CreatePairingCode(ctx, session, created.ID, codeHash[:], time.Now().Add(10*time.Minute), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	wrongHash := sha256.Sum256([]byte("654321"))
	for attempt := 1; attempt <= pairing.MaximumCodeAttempts; attempt++ {
		_, err := store.RedeemPairingCode(ctx, session, created.ID, challengeID, wrongHash[:], time.Now(), pairing.MaximumCodeAttempts)
		want := pairing.ErrInvalidCode
		if attempt == pairing.MaximumCodeAttempts {
			want = pairing.ErrLocked
		}
		if !errors.Is(err, want) {
			t.Fatalf("pairing attempt %d error = %v, want %v", attempt, err, want)
		}
	}
	if _, err := store.RedeemPairingCode(ctx, session, created.ID, challengeID, codeHash[:], time.Now(), pairing.MaximumCodeAttempts); !errors.Is(err, pairing.ErrLocked) {
		t.Fatalf("locked pairing code error = %v, want ErrLocked", err)
	}

	challengeID, err = store.CreatePairingCode(ctx, session, created.ID, codeHash[:], time.Now().Add(10*time.Minute), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.RedeemPairingCode(ctx, session, created.ID, challengeID, codeHash[:], time.Now(), pairing.MaximumCodeAttempts)
	if err != nil || approved.TrustStatus != device.TrustTrusted {
		t.Fatalf("RedeemPairingCode() = %+v, %v", approved, err)
	}
	if _, err := store.RedeemPairingCode(ctx, session, created.ID, challengeID, codeHash[:], time.Now(), pairing.MaximumCodeAttempts); !errors.Is(err, pairing.ErrUsed) {
		t.Fatalf("reused pairing code error = %v, want ErrUsed", err)
	}

	memberIdentifier := identifier + "-member"
	var memberUserID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO users (username, email, password_hash)
		VALUES ($1, $2, 'unused') RETURNING id::text
	`, memberIdentifier, memberIdentifier+"@example.com").Scan(&memberUserID)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = store.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, memberUserID) }()
	var memberSessionID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (
			user_id, access_token_hash, access_expires_at, refresh_token_hash, expires_at
		) VALUES ($1, $2, now() + interval '1 hour', $3, now() + interval '1 day')
		RETURNING id::text
	`, memberUserID, []byte("member-access-hash"), []byte("member-refresh-hash")).Scan(&memberSessionID)
	if err != nil {
		t.Fatal(err)
	}
	memberSession := auth.Session{User: auth.User{ID: memberUserID, Username: memberIdentifier}, SessionID: memberSessionID}

	createdGroup, err := store.CreateGroup(ctx, session, "Team")
	if err != nil {
		t.Fatal(err)
	}
	member, err := store.AddGroupMember(ctx, session, createdGroup.ID, memberUserID, group.RoleAdmin)
	if err != nil || member.Role != group.RoleAdmin {
		t.Fatalf("AddGroupMember() = %+v, %v", member, err)
	}
	if _, err := store.RenameGroup(ctx, memberSession, createdGroup.ID, "Forbidden"); !errors.Is(err, group.ErrForbidden) {
		t.Fatalf("admin rename error = %v, want ErrForbidden", err)
	}
	groupDevice, err := store.AddGroupDevice(ctx, session, createdGroup.ID, created.ID, time.Now().UTC())
	if err != nil || groupDevice.ID != created.ID {
		t.Fatalf("AddGroupDevice() = %+v, %v", groupDevice, err)
	}
	details, err := store.GetGroup(ctx, memberSession, createdGroup.ID)
	if err != nil || len(details.Members) != 2 || len(details.Devices) != 1 {
		t.Fatalf("GetGroup() = %+v, %v", details, err)
	}
	if err := store.RemoveGroupDevice(ctx, memberSession, createdGroup.ID, created.ID); err != nil {
		t.Fatalf("admin RemoveGroupDevice() error = %v", err)
	}
	if err := store.RemoveGroupMember(ctx, session, createdGroup.ID, memberUserID); err != nil {
		t.Fatalf("owner RemoveGroupMember() error = %v", err)
	}
	if err := store.DeleteGroup(ctx, session, createdGroup.ID); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}

	session.DeviceID = &created.ID
	var targetSessionID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (
			user_id, access_token_hash, access_expires_at, refresh_token_hash, expires_at
		) VALUES ($1, $2, now() + interval '1 hour', $3, now() + interval '1 day')
		RETURNING id::text
	`, userID, []byte("target-access-hash"), []byte("target-refresh-hash")).Scan(&targetSessionID)
	if err != nil {
		t.Fatal(err)
	}
	targetSession := auth.Session{User: session.User, SessionID: targetSessionID}
	targetDevice, err := store.CreateDevice(ctx, targetSession, "Phone", device.TypeAndroid, bytes.Repeat([]byte{1}, 32), "X25519")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ApproveDevice(ctx, session, targetDevice.ID); err != nil {
		t.Fatal(err)
	}
	targetSession.DeviceID = &targetDevice.ID
	transferService := transfer.NewService(store)
	textTransfer, err := transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID},
		LANAvailableDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentText,
		Content: []byte("encrypted text"), WrappedContentKeys: map[string][]byte{targetDevice.ID: {1, 2, 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(textTransfer.Targets) != 1 || textTransfer.Targets[0].SelectedRoute != domain.SelectedRouteLAN {
		t.Fatalf("text transfer targets = %+v", textTransfer.Targets)
	}
	targetTransfers, err := store.ListTransfers(ctx, targetSession)
	if err != nil || len(targetTransfers) != 1 {
		t.Fatalf("target ListTransfers() = %+v, %v", targetTransfers, err)
	}
	if !bytes.Equal(targetTransfers[0].WrappedContentKeys[targetDevice.ID], []byte{1, 2, 3}) {
		t.Fatalf("target wrapped content keys = %+v", targetTransfers[0].WrappedContentKeys)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE transfer_targets SET status = 'DELIVERED' WHERE transfer_id = $1`, textTransfer.ID); err != nil {
		t.Fatal(err)
	}
	readTransfer, err := transferService.Read(ctx, targetSession, textTransfer.ID)
	if err != nil || readTransfer.Targets[0].Status != domain.TransferRead {
		t.Fatalf("Read() = %+v, %v", readTransfer, err)
	}

	fileTransfer, err := transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentFile,
		WrappedContentKeys: map[string][]byte{targetDevice.ID: {4, 5, 6}},
		Files: []transfer.File{
			{Name: "small.bin", MIMEType: "application/octet-stream", Size: 1, SHA256: make([]byte, 32), ChunkSize: int(8 * 1024 * 1024), ChunkCount: 1},
			{Name: "large.bin", MIMEType: "application/octet-stream", Size: domain.DefaultLargeFileThreshold + 1, SHA256: bytes.Repeat([]byte{2}, 32), ChunkSize: int(8 * 1024 * 1024), ChunkCount: 13},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fileTransfer.FileTargets) != 2 || fileTransfer.Targets[0].SelectedRoute != domain.SelectedRouteMixed {
		t.Fatalf("file transfer = %+v", fileTransfer)
	}
	targetFileTransfer, err := store.GetTransfer(ctx, targetSession, fileTransfer.ID)
	if err != nil || !bytes.Equal(targetFileTransfer.WrappedContentKeys[targetDevice.ID], []byte{4, 5, 6}) {
		t.Fatalf("target file transfer key = %+v, %v", targetFileTransfer.WrappedContentKeys, err)
	}
	cancelled, err := transferService.Cancel(ctx, session, fileTransfer.ID)
	if err != nil || cancelled.Status != domain.TransferCancelled {
		t.Fatalf("Cancel() = %+v, %v", cancelled, err)
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
