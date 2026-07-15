package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"nexdrop/internal/admin"
	"nexdrop/internal/analytics"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/domain"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
	"nexdrop/internal/transfer"
)

type testStore struct {
	credential      auth.Credential
	accessHash      []byte
	refresh         []byte
	devices         []device.Device
	groups          []group.Group
	sessionDeviceID *string
	adminVerified   bool
	fileRecord      filetransfer.FileRecord
	chunks          map[int]filetransfer.ChunkRecord
}

func (store *testStore) CredentialByIdentifier(context.Context, string) (auth.Credential, error) {
	return store.credential, nil
}

func (store *testStore) CreateSession(_ context.Context, _ string, access []byte, _ time.Time, refresh []byte, _ time.Time) (string, error) {
	store.accessHash = append([]byte(nil), access...)
	store.refresh = append([]byte(nil), refresh...)
	return "session-1", nil
}

func (store *testStore) SessionByAccessToken(_ context.Context, access []byte, _ time.Time) (auth.Session, error) {
	if !bytes.Equal(access, store.accessHash) {
		return auth.Session{}, errors.New("not found")
	}
	return auth.Session{User: store.credential.User, SessionID: "session-1", DeviceID: store.sessionDeviceID, AdminVerified: store.adminVerified}, nil
}

func (store *testStore) RotateSession(_ context.Context, oldRefresh, access []byte, _ time.Time, refresh []byte, _ time.Time, _ time.Time) error {
	if !bytes.Equal(oldRefresh, store.refresh) {
		return errors.New("not found")
	}
	store.accessHash = append([]byte(nil), access...)
	store.refresh = append([]byte(nil), refresh...)
	return nil
}

func (store *testStore) RevokeSessionByRefreshToken(_ context.Context, refresh []byte, _ time.Time) error {
	if !bytes.Equal(refresh, store.refresh) {
		return errors.New("not found")
	}
	return nil
}

func (store *testStore) SetTOTPSecret(_ context.Context, _ string, secret string) error {
	store.credential.TOTPSecret = secret
	store.credential.TOTPEnabled = true
	return nil
}

func (store *testStore) MarkAdminVerified(_ context.Context, _ string, _ time.Time) error {
	store.adminVerified = true
	return nil
}

func (store *testStore) CreateDevice(_ context.Context, _ auth.Session, name string, kind device.Type, publicKey []byte, algorithm string) (device.Device, error) {
	item := device.Device{ID: "device-1", DisplayName: name, Type: kind, PublicKey: publicKey, Algorithm: algorithm, TrustStatus: device.TrustPending}
	store.devices = append(store.devices, item)
	return item, nil
}

func (store *testStore) ListDevices(context.Context, string) ([]device.Device, error) {
	return store.devices, nil
}

func (store *testStore) RenameDevice(_ context.Context, _, id, name string) (device.Device, error) {
	return device.Device{ID: id, DisplayName: name}, nil
}

func (store *testStore) DeleteDevice(context.Context, string, string) error { return nil }

func (store *testStore) ApproveDevice(_ context.Context, _ auth.Session, id string) (device.Device, error) {
	return device.Device{ID: id, TrustStatus: device.TrustTrusted}, nil
}

func (store *testStore) RevokeDevice(_ context.Context, _ auth.Session, id string, now time.Time) (device.Device, error) {
	return device.Device{ID: id, TrustStatus: device.TrustRevoked, RevokedAt: &now}, nil
}

func (*testStore) DevicePublicKeyForSession(context.Context, auth.Session, string) ([]byte, error) {
	return make([]byte, 32), nil
}

func (*testStore) CreateDeviceSessionChallenge(context.Context, auth.Session, string, []byte, time.Time, time.Time) (string, error) {
	return "challenge-1", nil
}

func (*testStore) RedeemDeviceSessionChallenge(context.Context, auth.Session, string, string, []byte, time.Time, int) error {
	return nil
}

func (*testStore) RegisterLANIdentity(context.Context, auth.Session, string, string, string, string, time.Time) error {
	return nil
}

func (*testStore) CreatePairingCode(context.Context, auth.Session, string, []byte, time.Time, time.Time) (string, error) {
	return "challenge-1", nil
}

func (*testStore) RedeemPairingCode(_ context.Context, _ auth.Session, deviceID, _ string, _ []byte, _ time.Time, _ int) (device.Device, error) {
	return device.Device{ID: deviceID, TrustStatus: device.TrustTrusted}, nil
}

func (store *testStore) CreateGroup(_ context.Context, session auth.Session, name string) (group.Details, error) {
	item := group.Group{ID: "group-1", Name: name, OwnerID: session.ID, Role: group.RoleOwner}
	store.groups = append(store.groups, item)
	return group.Details{Group: item}, nil
}

func (store *testStore) ListGroups(context.Context, auth.Session) ([]group.Group, error) {
	return store.groups, nil
}

func (*testStore) GetGroup(context.Context, auth.Session, string) (group.Details, error) {
	return group.Details{Group: group.Group{ID: "group-1"}}, nil
}

func (*testStore) RenameGroup(_ context.Context, _ auth.Session, id, name string) (group.Group, error) {
	return group.Group{ID: id, Name: name}, nil
}

func (*testStore) DeleteGroup(context.Context, auth.Session, string) error { return nil }

func (*testStore) AddGroupMember(_ context.Context, _ auth.Session, _, userID string, role group.Role) (group.Member, error) {
	return group.Member{UserID: userID, Role: role}, nil
}

func (*testStore) RemoveGroupMember(context.Context, auth.Session, string, string) error {
	return nil
}

func (*testStore) AddGroupDevice(_ context.Context, _ auth.Session, _, deviceID string, now time.Time) (group.GroupDevice, error) {
	return group.GroupDevice{ID: deviceID, AddedAt: now}, nil
}

func (*testStore) RemoveGroupDevice(context.Context, auth.Session, string, string) error {
	return nil
}

func (*testStore) ResolveTransferTargets(_ context.Context, _ auth.Session, _ transfer.TargetType, _ string, requested []string) ([]string, error) {
	return requested, nil
}

func (*testStore) CreateTransfer(_ context.Context, session auth.Session, prepared transfer.Prepared) (transfer.Transfer, error) {
	return transfer.Transfer{
		ID: "transfer-1", SenderUserID: session.ID, SenderDeviceID: *session.DeviceID,
		TargetType: prepared.TargetType, ContentType: prepared.ContentType, Targets: prepared.Targets,
		FileTargets: prepared.FileTargets, Status: prepared.Status,
	}, nil
}

func (*testStore) ListTransfers(context.Context, auth.Session) ([]transfer.Transfer, error) {
	return []transfer.Transfer{}, nil
}

func (*testStore) GetTransfer(context.Context, auth.Session, string) (transfer.Transfer, error) {
	return transfer.Transfer{ID: "transfer-1"}, nil
}

func (*testStore) CancelTransfer(context.Context, auth.Session, string, time.Time) (transfer.Transfer, error) {
	return transfer.Transfer{ID: "transfer-1", Status: domain.TransferCancelled}, nil
}

func (*testStore) ReadTransfer(context.Context, auth.Session, string, time.Time) (transfer.Transfer, error) {
	return transfer.Transfer{ID: "transfer-1"}, nil
}

func (*testStore) ReportTransferProgress(_ context.Context, _ auth.Session, id string, progress transfer.Progress, _ time.Time) (transfer.Transfer, error) {
	return transfer.Transfer{ID: id, Status: progress.Status}, nil
}

func (store *testStore) PrepareChunkUpload(_ context.Context, _ auth.Session, _ string, index int) (filetransfer.FileRecord, *filetransfer.ChunkRecord, error) {
	if chunk, ok := store.chunks[index]; ok {
		return store.fileRecord, &chunk, nil
	}
	return store.fileRecord, nil, nil
}

func (store *testStore) RecordChunk(_ context.Context, _ auth.Session, chunk filetransfer.ChunkRecord) error {
	store.chunks[chunk.Index] = chunk
	return nil
}

func (store *testStore) OpenChunk(_ context.Context, _ auth.Session, _ string, index int) (filetransfer.ChunkRecord, error) {
	return store.chunks[index], nil
}

func (store *testStore) PrepareFileCompletion(context.Context, auth.Session, string) (filetransfer.FileRecord, []filetransfer.ChunkRecord, error) {
	result := make([]filetransfer.ChunkRecord, 0, len(store.chunks))
	for index := 0; index < store.fileRecord.ChunkCount; index++ {
		result = append(result, store.chunks[index])
	}
	return store.fileRecord, result, nil
}

func (store *testStore) CompleteFile(_ context.Context, _ auth.Session, _ string, path string, _ time.Time) error {
	store.fileRecord.Status = "AVAILABLE_ON_NODE"
	store.fileRecord.StoragePath = path
	return nil
}

func (*testStore) InsertMetrics(_ context.Context, _ auth.Session, metrics []analytics.Metric) (analytics.BatchResult, error) {
	return analytics.BatchResult{Accepted: len(metrics)}, nil
}

func (*testStore) AnalyticsOverview(context.Context, auth.Session, analytics.TimeRange) (analytics.Overview, error) {
	return analytics.Overview{TransferCount: 1}, nil
}

func (*testStore) DailyTransfers(context.Context, auth.Session, analytics.TimeRange) ([]analytics.DailyTransfer, error) {
	return []analytics.DailyTransfer{}, nil
}

func (*testStore) DeviceStatistics(context.Context, auth.Session, analytics.TimeRange) ([]analytics.DeviceStatistic, error) {
	return []analytics.DeviceStatistic{}, nil
}

func (*testStore) GroupStatistics(context.Context, auth.Session, analytics.TimeRange) ([]analytics.GroupStatistic, error) {
	return []analytics.GroupStatistic{}, nil
}

func (*testStore) NodeStatistics(context.Context, auth.Session, analytics.TimeRange) ([]analytics.NodeMetric, error) {
	return []analytics.NodeMetric{}, nil
}

func (*testStore) BootstrapAdmin(context.Context, string, string, string) error { return nil }

func (*testStore) ListAdminUsers(context.Context, int, int) ([]admin.User, error) {
	return []admin.User{}, nil
}

func (*testStore) CreateAdminUser(context.Context, auth.Session, string, string, string, bool) (admin.User, error) {
	return admin.User{}, nil
}

func (*testStore) DisableAdminUser(context.Context, auth.Session, string, time.Time) error {
	return nil
}

func (*testStore) ResetAdminPassword(context.Context, auth.Session, string, string, time.Time) error {
	return nil
}

func (*testStore) ResetAdminPasswordByIdentifier(context.Context, string, string, time.Time) error {
	return nil
}

func (*testStore) AdminNodeSettings(context.Context) (admin.NodeSettings, error) {
	return admin.NodeSettings{SingleFileLimitBytes: 1024, DefaultUserQuotaBytes: 2048, DefaultGroupQuotaBytes: 4096, NodeCacheLimitBytes: 8192, DefaultUserDailyBytes: 16384, DefaultGroupDailyBytes: 32768, DiskWarningPercent: 80, DiskStopPercent: 95}, nil
}

func (*testStore) UpdateAdminNodeSettings(_ context.Context, _ auth.Session, settings admin.NodeSettings) (admin.NodeSettings, error) {
	return settings, nil
}

func (*testStore) SetAdminQuota(_ context.Context, _ auth.Session, quota admin.Quota) (admin.Quota, error) {
	return quota, nil
}

func (*testStore) AdminStorageOverview(context.Context, time.Time) (admin.StorageOverview, error) {
	return admin.StorageOverview{}, nil
}

func (*testStore) ListAdminFailures(context.Context, int, int) ([]admin.Failure, error) {
	return []admin.Failure{}, nil
}

func (*testStore) ListAdminAuditLogs(context.Context, int, int) ([]admin.AuditLog, error) {
	return []admin.AuditLog{}, nil
}

func TestLoginAndReadAccount(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{
		User:         auth.User{ID: "user-1", Username: "owner", Email: "owner@example.com"},
		PasswordHash: string(passwordHash),
	}}
	handler := New(auth.NewService(store, time.Minute, time.Hour), nil, nil, nil, nil, nil, nil, nil).Routes()

	login := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"identifier":"owner","password":"password"}`))
	loginResponse := httptest.NewRecorder()
	handler.ServeHTTP(loginResponse, login)
	if loginResponse.Code != http.StatusOK {
		t.Fatalf("login status = %d, body = %s", loginResponse.Code, loginResponse.Body.String())
	}
	var pair auth.TokenPair
	if err := json.NewDecoder(loginResponse.Body).Decode(&pair); err != nil {
		t.Fatal(err)
	}
	refresh := httptest.NewRequest(http.MethodPost, "/api/auth/refresh", bytes.NewBufferString(`{"refreshToken":"`+pair.RefreshToken+`"}`))
	refreshResponse := httptest.NewRecorder()
	handler.ServeHTTP(refreshResponse, refresh)
	if refreshResponse.Code != http.StatusOK {
		t.Fatalf("refresh status = %d, body = %s", refreshResponse.Code, refreshResponse.Body.String())
	}
	var refreshed auth.TokenPair
	if err := json.NewDecoder(refreshResponse.Body).Decode(&refreshed); err != nil {
		t.Fatal(err)
	}

	account := httptest.NewRequest(http.MethodGet, "/api/account", nil)
	account.Header.Set("Authorization", "Bearer "+refreshed.AccessToken)
	accountResponse := httptest.NewRecorder()
	handler.ServeHTTP(accountResponse, account)
	if accountResponse.Code != http.StatusOK {
		t.Fatalf("account status = %d, body = %s", accountResponse.Code, accountResponse.Body.String())
	}
}

func TestCreateAndListDevice(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{
		User:         auth.User{ID: "user-1", Username: "owner", Email: "owner@example.com"},
		PasswordHash: string(passwordHash),
	}}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, device.NewService(store), pairing.NewService(store), nil, nil, nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}

	payload, err := json.Marshal(map[string]any{
		"displayName":  "Laptop",
		"type":         device.TypeWindows,
		"publicKey":    make([]byte, 32),
		"keyAlgorithm": "X25519",
	})
	if err != nil {
		t.Fatal(err)
	}
	create := httptest.NewRequest(http.MethodPost, "/api/devices", bytes.NewReader(payload))
	create.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}

	list := httptest.NewRequest(http.MethodGet, "/api/devices", nil)
	list.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
	var devices []device.Device
	if err := json.NewDecoder(listResponse.Body).Decode(&devices); err != nil {
		t.Fatal(err)
	}
	if len(devices) != 1 || devices[0].DisplayName != "Laptop" {
		t.Fatalf("devices = %+v", devices)
	}
}

func TestCreateAndListGroup(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{
		User:         auth.User{ID: "user-1", Username: "owner", Email: "owner@example.com"},
		PasswordHash: string(passwordHash),
	}}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, group.NewService(store), nil, nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}

	create := httptest.NewRequest(http.MethodPost, "/api/groups", bytes.NewBufferString(`{"name":"Team"}`))
	create.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	createResponse := httptest.NewRecorder()
	handler.ServeHTTP(createResponse, create)
	if createResponse.Code != http.StatusCreated {
		t.Fatalf("create group status = %d, body = %s", createResponse.Code, createResponse.Body.String())
	}
	var created group.Details
	if err := json.NewDecoder(createResponse.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.Name != "Team" || created.Role != group.RoleOwner {
		t.Fatalf("created group = %+v", created)
	}

	list := httptest.NewRequest(http.MethodGet, "/api/groups", nil)
	list.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, list)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("list groups status = %d, body = %s", listResponse.Code, listResponse.Body.String())
	}
}

func TestCreateTransfer(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	senderDeviceID := "sender-device"
	store := &testStore{
		credential: auth.Credential{
			User: auth.User{ID: "user-1", Username: "owner", Email: "owner@example.com"}, PasswordHash: string(passwordHash),
		},
		sessionDeviceID: &senderDeviceID,
	}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, transfer.NewService(store), nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(transfer.Request{
		TargetType: transfer.TargetSingle, TargetDeviceIDs: []string{"target-device"},
		ContentType: transfer.ContentText, Content: []byte("encrypted"),
		WrappedContentKeys: map[string][]byte{"target-device": {1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/transfers", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("create transfer status = %d, body = %s", response.Code, response.Body.String())
	}
	var created transfer.Transfer
	if err := json.NewDecoder(response.Body).Decode(&created); err != nil {
		t.Fatal(err)
	}
	if created.ID != "transfer-1" || created.Targets[0].SelectedRoute != domain.SelectedRouteNode {
		t.Fatalf("created transfer = %+v", created)
	}
}

func TestUploadAndDownloadChunk(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	content := []byte("chunk")
	digest := sha256.Sum256(content)
	deviceID := "device-1"
	store := &testStore{
		credential:      auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)},
		sessionDeviceID: &deviceID,
		fileRecord:      filetransfer.FileRecord{ID: "file-1", Size: int64(len(content)), SHA256: digest[:], ChunkSize: len(content), ChunkCount: 1},
		chunks:          make(map[int]filetransfer.ChunkRecord),
	}
	authService := auth.NewService(store, time.Minute, time.Hour)
	fileService, err := filetransfer.NewService(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	handler := New(authService, nil, nil, nil, nil, fileService, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest(http.MethodPost, "/api/files/file-1/chunks/0", bytes.NewReader(content))
	upload.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	upload.Header.Set("X-Chunk-SHA256", hex.EncodeToString(digest[:]))
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("upload status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	download := httptest.NewRequest(http.MethodGet, "/api/files/file-1/chunks/0", nil)
	download.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	downloadResponse := httptest.NewRecorder()
	handler.ServeHTTP(downloadResponse, download)
	if downloadResponse.Code != http.StatusOK || !bytes.Equal(downloadResponse.Body.Bytes(), content) {
		t.Fatalf("download status = %d, body = %q", downloadResponse.Code, downloadResponse.Body.Bytes())
	}
}

func TestUploadMetricsAndReadOverview(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "11111111-1111-1111-1111-111111111111"
	store := &testStore{
		credential:      auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)},
		sessionDeviceID: &deviceID,
	}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, nil, nil, analytics.NewService(store), nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(map[string]any{"events": []analytics.Metric{{
		EventID: "22222222-2222-2222-2222-222222222222", TransferID: "33333333-3333-3333-3333-333333333333",
		SenderDeviceID: deviceID, ContentType: "TEXT", Route: domain.SelectedRouteLAN, StartedAt: time.Now().UTC(), Succeeded: true,
	}}})
	if err != nil {
		t.Fatal(err)
	}
	upload := httptest.NewRequest(http.MethodPost, "/api/metrics/batch", bytes.NewReader(payload))
	upload.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	uploadResponse := httptest.NewRecorder()
	handler.ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusOK {
		t.Fatalf("metrics status = %d, body = %s", uploadResponse.Code, uploadResponse.Body.String())
	}
	overview := httptest.NewRequest(http.MethodGet, "/api/statistics/overview", nil)
	overview.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	overviewResponse := httptest.NewRecorder()
	handler.ServeHTTP(overviewResponse, overview)
	if overviewResponse.Code != http.StatusOK {
		t.Fatalf("overview status = %d, body = %s", overviewResponse.Code, overviewResponse.Body.String())
	}
}

func TestReadAdminSettings(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{
		User:         auth.User{ID: "admin-1", Username: "admin", Admin: true},
		PasswordHash: string(passwordHash),
	}}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, nil, nil, nil, admin.NewService(store)).Routes()
	pair, err := authService.Login(context.Background(), "admin", "password")
	if err != nil {
		t.Fatal(err)
	}
	unverified := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	unverified.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	unverifiedResponse := httptest.NewRecorder()
	handler.ServeHTTP(unverifiedResponse, unverified)
	if unverifiedResponse.Code != http.StatusForbidden {
		t.Fatalf("unverified admin status = %d, want %d", unverifiedResponse.Code, http.StatusForbidden)
	}
	store.adminVerified = true

	request := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin settings status = %d, body = %s", response.Code, response.Body.String())
	}
}
