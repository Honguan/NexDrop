package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"nexdrop/internal/analytics"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/domain"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/lan"
	"nexdrop/internal/maintenance"
	"nexdrop/internal/pairing"
	"nexdrop/internal/presence"
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
	if created.TrustStatus != device.TrustTrusted {
		t.Fatalf("CreateDevice() trust status = %s, want TRUSTED", created.TrustStatus)
	}
	listed, err := store.ListDevices(ctx, userID)
	if err != nil || len(listed) != 1 {
		t.Fatalf("ListDevices() = %+v, %v", listed, err)
	}
	var pairingSessionID string
	err = store.pool.QueryRow(ctx, `
		INSERT INTO user_sessions (
			user_id, access_token_hash, access_expires_at, refresh_token_hash, expires_at
		) VALUES ($1, $2, now() + interval '1 hour', $3, now() + interval '1 day')
		RETURNING id::text
	`, userID, []byte("pairing-access-hash"), []byte("pairing-refresh-hash")).Scan(&pairingSessionID)
	if err != nil {
		t.Fatal(err)
	}
	pairingSession := auth.Session{User: session.User, SessionID: pairingSessionID}
	pendingDevice, err := store.CreateDevice(ctx, pairingSession, "Pending phone", device.TypeAndroid, bytes.Repeat([]byte{2}, 32), "X25519")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE devices SET trust_status = 'PENDING' WHERE id = $1`, pendingDevice.ID); err != nil {
		t.Fatal(err)
	}
	pairingSession.DeviceID = &pendingDevice.ID
	session.DeviceID = &created.ID

	codeHash := sha256.Sum256([]byte("123456"))
	challengeID, err := store.CreatePairingCode(ctx, pairingSession, pendingDevice.ID, codeHash[:], time.Now().Add(10*time.Minute), time.Now())
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

	challengeID, err = store.CreatePairingCode(ctx, pairingSession, pendingDevice.ID, codeHash[:], time.Now().Add(10*time.Minute), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	approved, err := store.RedeemPairingCode(ctx, session, created.ID, challengeID, codeHash[:], time.Now(), pairing.MaximumCodeAttempts)
	if err != nil || approved.TrustStatus != device.TrustTrusted || approved.ID != pendingDevice.ID {
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
	member, err = store.AddGroupMember(ctx, session, createdGroup.ID, memberUserID, group.RoleMember)
	if err != nil || member.Role != group.RoleMember {
		t.Fatalf("UpdateGroupMember() = %+v, %v", member, err)
	}
	member, err = store.AddGroupMember(ctx, session, createdGroup.ID, memberUserID, group.RoleAdmin)
	if err != nil || member.Role != group.RoleAdmin {
		t.Fatalf("RestoreGroupMember() = %+v, %v", member, err)
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
	session.DeviceID = &created.ID
	lanIdentity, err := lan.GenerateIdentity(strings.ReplaceAll(created.ID, "-", "")[:12], time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RegisterLANIdentity(ctx, session, created.ID, lanIdentity.ShortDeviceID, lanIdentity.Fingerprint, lanIdentity.CertificatePEM, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	listed, err = store.ListDevices(ctx, userID)
	if err != nil || listed[0].LANFingerprint != lanIdentity.Fingerprint || listed[0].LANCertificate != lanIdentity.CertificatePEM {
		t.Fatalf("LAN identity list = %+v, %v", listed, err)
	}
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
	if err := store.ConnectDevice(ctx, targetDevice.ID, time.Now().UTC(), presence.ProtocolVersion, "integration"); err != nil {
		t.Fatal(err)
	}
	if err := store.HeartbeatDevice(ctx, targetDevice.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	transferService := transfer.NewService(store)
	const clientBatchID = "4b9de43e-5903-4d29-ab44-cebe100daf4e"
	request := transfer.Request{
		IdempotencyKey: "5b9de43e-5903-4d29-ab44-cebe100daf4e",
		ClientBatchID:  clientBatchID,
		TargetType:     transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID},
		LANAvailableDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentText,
		Content: []byte("encrypted text"), WrappedContentKeys: map[string][]byte{targetDevice.ID: {1, 2, 3}, created.ID: {9, 8, 7}},
	}
	textTransfer, err := transferService.Create(ctx, session, request)
	if err != nil {
		t.Fatal(err)
	}
	replayedTransfer, err := transferService.Create(ctx, session, request)
	if err != nil || replayedTransfer.ID != textTransfer.ID {
		t.Fatalf("idempotent transfer = %+v, %v", replayedTransfer, err)
	}
	conflictingRequest := request
	conflictingRequest.Content = []byte("different encrypted text")
	if _, err := transferService.Create(ctx, session, conflictingRequest); !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("idempotency conflict error = %v", err)
	}
	if textTransfer.BatchID != clientBatchID {
		t.Fatalf("batch ID = %q, want %q", textTransfer.BatchID, clientBatchID)
	}
	if len(textTransfer.Targets) != 1 || textTransfer.Targets[0].SelectedRoute != domain.SelectedRouteLAN {
		t.Fatalf("text transfer targets = %+v", textTransfer.Targets)
	}
	if !bytes.Equal(textTransfer.WrappedContentKeys[created.ID], []byte{9, 8, 7}) {
		t.Fatalf("sender wrapped content keys = %+v", textTransfer.WrappedContentKeys)
	}
	targetTransfers, err := store.ListTransfers(ctx, targetSession)
	if err != nil || len(targetTransfers) != 1 {
		t.Fatalf("target ListTransfers() = %+v, %v", targetTransfers, err)
	}
	if !bytes.Equal(targetTransfers[0].WrappedContentKeys[targetDevice.ID], []byte{1, 2, 3}) {
		t.Fatalf("target wrapped content keys = %+v", targetTransfers[0].WrappedContentKeys)
	}
	verifyingProgress := transfer.Progress{
		IdempotencyKey: "6b9de43e-5903-4d29-ab44-cebe100daf4e",
		DeviceID:       targetDevice.ID, Status: domain.TransferVerifying, Route: domain.SelectedRouteLAN, BytesTransferred: 14,
	}
	progressErrors := make(chan error, 2)
	for range 2 {
		go func() {
			result, err := transferService.ReportProgress(ctx, targetSession, textTransfer.ID, verifyingProgress)
			if err == nil && result.Targets[0].Status != domain.TransferVerifying {
				err = errors.New("concurrent progress returned wrong status")
			}
			progressErrors <- err
		}()
	}
	for range 2 {
		if err := <-progressErrors; err != nil {
			t.Fatalf("concurrent progress error = %v", err)
		}
	}
	reported, err := transferService.ReportProgress(ctx, targetSession, textTransfer.ID, transfer.Progress{
		IdempotencyKey: "7b9de43e-5903-4d29-ab44-cebe100daf4e",
		DeviceID:       targetDevice.ID, Status: domain.TransferDelivered, Route: domain.SelectedRouteLAN, BytesTransferred: 14,
	})
	if err != nil || reported.Targets[0].Status != domain.TransferDelivered || reported.Targets[0].BytesTransferred != 14 {
		t.Fatalf("ReportProgress() = %+v, %v", reported, err)
	}
	terminalReplay, err := transferService.ReportProgress(ctx, targetSession, textTransfer.ID, transfer.Progress{
		IdempotencyKey: "8b9de43e-5903-4d29-ab44-cebe100daf4e",
		DeviceID:       targetDevice.ID, Status: domain.TransferDelivered, Route: domain.SelectedRouteLAN, BytesTransferred: 14,
	})
	if err != nil || terminalReplay.Targets[0].Status != domain.TransferDelivered {
		t.Fatalf("terminal progress replay = %+v, %v", terminalReplay, err)
	}
	_, err = transferService.ReportProgress(ctx, targetSession, textTransfer.ID, transfer.Progress{
		IdempotencyKey: "8b9de43e-5903-4d29-ab44-cebe100daf4e",
		DeviceID:       targetDevice.ID, Status: domain.TransferDelivered, Route: domain.SelectedRouteLAN, BytesTransferred: 15,
	})
	if !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("terminal progress conflict = %v, want ErrIdempotencyConflict", err)
	}
	replayedProgress, err := transferService.ReportProgress(ctx, targetSession, textTransfer.ID, transfer.Progress{
		IdempotencyKey: "7b9de43e-5903-4d29-ab44-cebe100daf4e",
		DeviceID:       targetDevice.ID, Status: domain.TransferDelivered, Route: domain.SelectedRouteLAN, BytesTransferred: 14,
	})
	if err != nil || replayedProgress.Targets[0].Status != domain.TransferDelivered {
		t.Fatalf("replayed progress = %+v, %v", replayedProgress, err)
	}
	_, err = transferService.ReportProgress(ctx, targetSession, textTransfer.ID, transfer.Progress{
		IdempotencyKey: "7b9de43e-5903-4d29-ab44-cebe100daf4e",
		DeviceID:       targetDevice.ID, Status: domain.TransferDelivered, Route: domain.SelectedRouteLAN, BytesTransferred: 15,
	})
	if !errors.Is(err, transfer.ErrIdempotencyConflict) {
		t.Fatalf("conflicting progress error = %v, want ErrIdempotencyConflict", err)
	}
	readTransfer, err := transferService.Read(ctx, targetSession, textTransfer.ID)
	if err != nil || readTransfer.Targets[0].Status != domain.TransferRead {
		t.Fatalf("Read() = %+v, %v", readTransfer, err)
	}
	if replayedRead, err := transferService.Read(ctx, targetSession, textTransfer.ID); err != nil || replayedRead.Targets[0].Status != domain.TransferRead {
		t.Fatalf("replayed Read() = %+v, %v", replayedRead, err)
	}

	smallContent := []byte("x")
	smallHash := sha256.Sum256(smallContent)
	fileTransfer, err := transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentFile,
		WrappedContentKeys: map[string][]byte{targetDevice.ID: {4, 5, 6}},
		Files: []transfer.File{
			{Name: "small.bin", MIMEType: "application/octet-stream", Size: 1, SHA256: smallHash[:], ChunkSize: int(8 * 1024 * 1024), ChunkCount: 1},
			{Name: "large.bin", MIMEType: "application/octet-stream", Size: domain.DefaultLargeFileThreshold + 1, SHA256: bytes.Repeat([]byte{2}, 32), ChunkSize: int(8 * 1024 * 1024), ChunkCount: 13},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(fileTransfer.FileTargets) != 2 || fileTransfer.Targets[0].SelectedRoute != domain.SelectedRouteMixed {
		t.Fatalf("file transfer = %+v", fileTransfer)
	}
	firstPage, err := transferService.ListPage(ctx, session, transfer.PageOptions{Limit: 1})
	if err != nil || len(firstPage.Items) != 1 || firstPage.NextCursor == "" || firstPage.NextPageKey.CreatedAt.IsZero() {
		t.Fatalf("first transfer page = %+v, %v", firstPage, err)
	}
	secondPage, err := transferService.ListPage(ctx, session, transfer.PageOptions{
		Limit: 1, Cursor: firstPage.NextPageKey,
	})
	if err != nil || len(secondPage.Items) != 1 || secondPage.Items[0].ID == firstPage.Items[0].ID {
		t.Fatalf("second transfer page = %+v, %v", secondPage, err)
	}
	if _, err := store.ListTransferPage(ctx, session, transfer.PageOptions{Limit: 101}); !errors.Is(err, transfer.ErrInvalid) {
		t.Fatalf("oversized transfer page error = %v, want ErrInvalid", err)
	}
	targetFileTransfer, err := store.GetTransfer(ctx, targetSession, fileTransfer.ID)
	if err != nil || !bytes.Equal(targetFileTransfer.WrappedContentKeys[targetDevice.ID], []byte{4, 5, 6}) {
		t.Fatalf("target file transfer key = %+v, %v", targetFileTransfer.WrappedContentKeys, err)
	}
	recoveryCases := []struct {
		status      domain.TransferStatus
		progressKey string
		retryKey    string
	}{
		{domain.TransferSourceFileMissing, "9b9de43e-5903-4d29-ab44-cebe100daf4e", "ab9de43e-5903-4d29-ab44-cebe100daf4e"},
		{domain.TransferSourceFileChanged, "bb9de43e-5903-4d29-ab44-cebe100daf4e", "cb9de43e-5903-4d29-ab44-cebe100daf4e"},
	}
	for _, testCase := range recoveryCases {
		if _, err := store.pool.Exec(ctx, `
			UPDATE transfer_targets SET status = $3
			WHERE transfer_id = $1 AND target_device_id = $2
		`, fileTransfer.ID, targetDevice.ID, testCase.status); err != nil {
			t.Fatal(err)
		}
		_, err = transferService.ReportProgress(ctx, targetSession, fileTransfer.ID, transfer.Progress{
			IdempotencyKey: testCase.progressKey,
			DeviceID:       targetDevice.ID,
			Status:         domain.TransferCheckingRoute,
		})
		if !errors.Is(err, transfer.ErrConflict) {
			t.Fatalf("%s progress recovery error = %v, want ErrConflict", testCase.status, err)
		}
		retried, err := transferService.Retry(ctx, session, fileTransfer.ID, targetDevice.ID, testCase.retryKey)
		if err != nil || retried.Targets[0].Status != domain.TransferCheckingRoute {
			t.Fatalf("%s Retry() = %+v, %v", testCase.status, retried, err)
		}
	}
	var smallFileID string
	for _, file := range fileTransfer.Files {
		if file.Name == "small.bin" {
			smallFileID = file.ID
		}
	}
	if smallFileID == "" {
		t.Fatal("small file ID was not returned")
	}
	storageRoot := t.TempDir()
	fileService, err := filetransfer.NewService(store, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fileService.UploadChunk(ctx, targetSession, smallFileID, 0, smallHash[:], bytes.NewReader(smallContent)); !errors.Is(err, filetransfer.ErrForbidden) {
		t.Fatalf("target upload error = %v, want ErrForbidden", err)
	}
	if _, err := fileService.UploadChunk(ctx, session, smallFileID, 0, smallHash[:], bytes.NewReader(smallContent)); err != nil {
		t.Fatal(err)
	}
	completedFile, err := fileService.Complete(ctx, session, smallFileID)
	if err != nil || completedFile.Status != "AVAILABLE_ON_NODE" {
		t.Fatalf("Complete() = %+v, %v", completedFile, err)
	}
	_, downloadedChunk, err := fileService.OpenChunk(ctx, targetSession, smallFileID, 0)
	if err != nil {
		t.Fatal(err)
	}
	downloaded, err := io.ReadAll(downloadedChunk)
	_ = downloadedChunk.Close()
	if err != nil || !bytes.Equal(downloaded, smallContent) {
		t.Fatalf("downloaded chunk = %q, %v", downloaded, err)
	}
	notifications, err := store.PendingNotifications(ctx, targetDevice.ID)
	if err != nil || len(notifications) < 2 {
		t.Fatalf("PendingNotifications() = %+v, %v", notifications, err)
	}
	if err := store.AcknowledgeNotification(ctx, targetDevice.ID, notifications[0].ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	remainingNotifications, err := store.PendingNotifications(ctx, targetDevice.ID)
	if err != nil || len(remainingNotifications) != len(notifications)-1 {
		t.Fatalf("remaining notifications = %+v, %v", remainingNotifications, err)
	}
	if err := store.DisconnectDevice(ctx, targetDevice.ID, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	analyticsService := analytics.NewService(store)
	metricStart := time.Now().UTC().Add(-time.Minute)
	metrics := []analytics.Metric{
		{
			EventID: "aaaaaaaa-aaaa-aaaa-aaaa-aaaaaaaaaaaa", TransferID: textTransfer.ID,
			SenderDeviceID: created.ID, ReceiverDeviceID: targetDevice.ID, ContentType: "TEXT",
			Route: domain.SelectedRouteLAN, FileSize: 10, StartedAt: metricStart, Succeeded: true,
		},
		{
			EventID: "bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb", TransferID: fileTransfer.ID,
			SenderDeviceID: created.ID, ReceiverDeviceID: targetDevice.ID, GroupID: createdGroup.ID,
			ContentType: "FILE", Route: domain.SelectedRouteNode, FileSize: 1, StartedAt: metricStart, Succeeded: true,
		},
	}
	batchKey := "cccccccc-cccc-cccc-cccc-cccccccccccc"
	batch, err := analyticsService.UploadIdempotent(ctx, session, batchKey, []byte("metrics-v1"), metrics)
	if err != nil || batch.Accepted != 2 {
		t.Fatalf("Upload metrics = %+v, %v", batch, err)
	}
	replayedBatch, err := analyticsService.UploadIdempotent(ctx, session, batchKey, []byte("metrics-v1"), metrics)
	if err != nil || replayedBatch != batch {
		t.Fatalf("replayed metrics = %+v, %v", replayedBatch, err)
	}
	if _, err := analyticsService.UploadIdempotent(ctx, session, batchKey, []byte("metrics-v2"), metrics); !errors.Is(err, analytics.ErrConflict) {
		t.Fatalf("conflicting metrics error = %v, want ErrConflict", err)
	}
	duplicateBatch, err := analyticsService.Upload(ctx, session, metrics)
	if err != nil || duplicateBatch.Duplicates != 2 {
		t.Fatalf("duplicate metrics = %+v, %v", duplicateBatch, err)
	}
	rangeValue := analytics.TimeRange{From: metricStart.Add(-time.Hour), To: time.Now().UTC().Add(time.Hour)}
	overview, err := analyticsService.Overview(ctx, session, rangeValue)
	if err != nil || overview.TransferCount != 2 || overview.TotalBytes != 11 || overview.RouteCounts["LAN"] != 1 || overview.RouteCounts["NODE"] != 1 {
		t.Fatalf("Overview() = %+v, %v", overview, err)
	}
	daily, err := analyticsService.Transfers(ctx, session, rangeValue)
	if err != nil || len(daily) != 1 || daily[0].TotalBytes != 11 {
		t.Fatalf("Transfers() = %+v, %v", daily, err)
	}
	deviceStatistics, err := analyticsService.Devices(ctx, session, rangeValue)
	if err != nil || len(deviceStatistics) < 2 {
		t.Fatalf("Devices() = %+v, %v", deviceStatistics, err)
	}
	groupStatistics, err := analyticsService.Groups(ctx, session, rangeValue)
	if err != nil || len(groupStatistics) != 1 || groupStatistics[0].FileCount != 1 {
		t.Fatalf("Groups() = %+v, %v", groupStatistics, err)
	}
	if err := store.DeleteGroup(ctx, session, createdGroup.ID); err != nil {
		t.Fatalf("DeleteGroup() error = %v", err)
	}
	cancelled, err := transferService.Cancel(ctx, session, fileTransfer.ID)
	if err != nil || cancelled.Status != domain.TransferCancelled {
		t.Fatalf("Cancel() = %+v, %v", cancelled, err)
	}
	_, err = store.pool.Exec(ctx, `
		INSERT INTO storage_quotas (owner_type, owner_id, byte_limit, daily_transfer_limit)
		VALUES ('USER', $1, 0, 0)
		ON CONFLICT (owner_type, owner_id) DO UPDATE SET byte_limit = 0, daily_transfer_limit = 0
	`, userID)
	if err != nil {
		t.Fatal(err)
	}
	quotaFile := transfer.File{Name: "quota.bin", MIMEType: "application/octet-stream", Size: 1, SHA256: smallHash[:], ChunkSize: 1, ChunkCount: 1}
	_, err = transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentFile,
		WrappedContentKeys: map[string][]byte{targetDevice.ID: {7}}, Files: []transfer.File{quotaFile},
	})
	if !errors.Is(err, transfer.ErrQuotaExceeded) {
		t.Fatalf("node quota error = %v, want ErrQuotaExceeded", err)
	}
	if _, err := store.pool.Exec(ctx, `
		UPDATE storage_quotas SET byte_limit = 1000, daily_transfer_limit = 1000
		WHERE owner_type = 'USER' AND owner_id = $1
	`, userID); err != nil {
		t.Fatal(err)
	}
	var originalSingleFileLimit, originalNodeCacheLimit int64
	if err := store.pool.QueryRow(ctx, `
		SELECT single_file_limit_bytes, node_cache_limit_bytes FROM node_settings WHERE singleton
	`).Scan(&originalSingleFileLimit, &originalNodeCacheLimit); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_, _ = store.pool.Exec(ctx, `
			UPDATE node_settings SET single_file_limit_bytes = $1, node_cache_limit_bytes = $2 WHERE singleton
		`, originalSingleFileLimit, originalNodeCacheLimit)
	}()
	if _, err := store.pool.Exec(ctx, `UPDATE node_settings SET single_file_limit_bytes = 0`); err != nil {
		t.Fatal(err)
	}
	_, err = transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentFile,
		WrappedContentKeys: map[string][]byte{targetDevice.ID: {7}}, Files: []transfer.File{quotaFile},
	})
	if !errors.Is(err, transfer.ErrFileTooLarge) {
		t.Fatalf("single file limit error = %v, want ErrFileTooLarge", err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE node_settings SET single_file_limit_bytes = 2147483648, node_cache_limit_bytes = 1`); err != nil {
		t.Fatal(err)
	}
	_, err = transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID}, ContentType: transfer.ContentFile,
		WrappedContentKeys: map[string][]byte{targetDevice.ID: {7}}, Files: []transfer.File{quotaFile},
	})
	if !errors.Is(err, transfer.ErrStorageFull) {
		t.Fatalf("node storage limit error = %v, want ErrStorageFull", err)
	}
	lanOnlyTransfer, err := transferService.Create(ctx, session, transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{targetDevice.ID}, LANAvailableDeviceIDs: []string{targetDevice.ID},
		ContentType: transfer.ContentFile, WrappedContentKeys: map[string][]byte{targetDevice.ID: {8}}, Files: []transfer.File{quotaFile},
	})
	if err != nil || lanOnlyTransfer.Targets[0].SelectedRoute != domain.SelectedRouteLAN {
		t.Fatalf("LAN-only quota transfer = %+v, %v", lanOnlyTransfer, err)
	}
	if _, err := store.pool.Exec(ctx, `UPDATE files SET expires_at = now() - interval '1 minute' WHERE id = $1`, smallFileID); err != nil {
		t.Fatal(err)
	}
	cleaner, err := maintenance.NewCleaner(store, storageRoot)
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := cleaner.RunOnce(ctx, 100)
	if err != nil || cleaned != 1 {
		t.Fatalf("cleanup = %d, %v", cleaned, err)
	}
	var expiredStatus string
	var remainingChunks int
	if err := store.pool.QueryRow(ctx, `SELECT status FROM files WHERE id = $1`, smallFileID).Scan(&expiredStatus); err != nil {
		t.Fatal(err)
	}
	if err := store.pool.QueryRow(ctx, `SELECT count(*) FROM file_chunks WHERE file_id = $1`, smallFileID).Scan(&remainingChunks); err != nil {
		t.Fatal(err)
	}
	if expiredStatus != "EXPIRED" || remainingChunks != 0 {
		t.Fatalf("expired file status = %s, chunks = %d", expiredStatus, remainingChunks)
	}

	renamed, err := store.RenameDevice(ctx, userID, created.ID, "Workstation")
	if err != nil || renamed.DisplayName != "Workstation" {
		t.Fatalf("RenameDevice() = %+v, %v", renamed, err)
	}
	if err := store.ConnectDevice(ctx, created.ID, time.Now().UTC(), presence.ProtocolVersion, "integration"); err != nil {
		t.Fatal(err)
	}
	revoked, err := store.RevokeDevice(ctx, session, created.ID, time.Now().UTC())
	if err != nil || revoked.TrustStatus != device.TrustRevoked {
		t.Fatalf("RevokeDevice() = %+v, %v", revoked, err)
	}
	if _, err := store.SessionByAccessToken(ctx, accessHash, time.Now().UTC()); err == nil {
		t.Fatal("device revocation did not revoke its session")
	}
	if _, err := store.PendingNotifications(ctx, created.ID); !errors.Is(err, device.ErrForbidden) {
		t.Fatalf("revoked device notifications error = %v, want ErrForbidden", err)
	}
	if err := store.HeartbeatDevice(ctx, created.ID, time.Now().UTC()); !errors.Is(err, device.ErrNotFound) {
		t.Fatalf("revoked device heartbeat error = %v, want ErrNotFound", err)
	}
}
