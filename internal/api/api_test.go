package api

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
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
	credential         auth.Credential
	accessHash         []byte
	sessionError       error
	refresh            []byte
	devices            []device.Device
	groups             []group.Group
	sessionDeviceID    *string
	adminVerified      bool
	fileRecord         filetransfer.FileRecord
	chunks             map[int]filetransfer.ChunkRecord
	page               transfer.Page
	pageOptions        transfer.PageOptions
	auditPage          admin.AuditLogPage
	auditPageOptions   admin.PageOptions
	failurePage        admin.FailurePage
	failurePageOptions admin.PageOptions
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
	if store.sessionError != nil {
		return auth.Session{}, store.sessionError
	}
	if !bytes.Equal(access, store.accessHash) {
		return auth.Session{}, auth.ErrInvalidCredentials
	}
	return auth.Session{User: store.credential.User, SessionID: "session-1", DeviceID: store.sessionDeviceID, AdminVerified: store.adminVerified}, nil
}

func TestAuthenticationStoreFailureReturnsServiceUnavailable(t *testing.T) {
	store := &testStore{sessionError: errors.New("database unavailable")}
	handler := New(auth.NewService(store, time.Minute, time.Hour), nil, nil, nil, nil, nil, nil, nil).Routes()
	request := httptest.NewRequest(http.MethodGet, "/api/account", nil)
	request.Header.Set("Accept", "application/vnd.nexdrop.v1+json")
	request.Header.Set("Authorization", "Bearer access-token")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("authentication store failure status = %d, body = %s", response.Code, response.Body.String())
	}
	var body struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "SERVICE_UNAVAILABLE" {
		t.Fatalf("authentication store failure code = %q", body.Error.Code)
	}
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

func (store *testStore) ListTransferPage(_ context.Context, _ auth.Session, options transfer.PageOptions) (transfer.Page, error) {
	store.pageOptions = options
	if store.page.Items == nil {
		store.page.Items = []transfer.Transfer{}
	}
	return store.page, nil
}

func (*testStore) GetTransfer(context.Context, auth.Session, string) (transfer.Transfer, error) {
	return transfer.Transfer{ID: "transfer-1"}, nil
}

func (*testStore) CancelTransfer(context.Context, auth.Session, string, time.Time) (transfer.Transfer, error) {
	return transfer.Transfer{ID: "transfer-1", Status: domain.TransferCancelled}, nil
}

func (*testStore) HideTransfer(context.Context, auth.Session, string, time.Time) error {
	return nil
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

func (*testStore) CreateAdminInvitation(context.Context, auth.Session, string, string, bool, []byte, time.Time) (admin.Invitation, error) {
	return admin.Invitation{}, nil
}

func (*testStore) AcceptAdminInvitation(context.Context, []byte, string, time.Time) (admin.User, error) {
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

func (*testStore) ListAdminDevices(context.Context, int, int) ([]admin.Device, error) {
	return []admin.Device{}, nil
}

func (*testStore) RevokeAdminDevice(context.Context, auth.Session, string, time.Time) error {
	return nil
}

func (*testStore) ListAdminGroups(context.Context, int, int) ([]admin.Group, error) {
	return []admin.Group{}, nil
}

func (*testStore) DeleteAdminGroup(context.Context, auth.Session, string, time.Time) error {
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

func (store *testStore) ListAdminAuditLogPage(_ context.Context, options admin.PageOptions) (admin.AuditLogPage, error) {
	store.auditPageOptions = options
	return store.auditPage, nil
}

func (store *testStore) ListAdminFailurePage(_ context.Context, options admin.PageOptions) (admin.FailurePage, error) {
	store.failurePageOptions = options
	return store.failurePage, nil
}

func (*testStore) DeleteAdminGroupContent(context.Context, auth.Session, string, time.Time) ([]string, error) {
	return nil, nil
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

func TestAPIVersionHeadersAndNegotiatedError(t *testing.T) {
	handler := New(nil, nil, nil, nil, nil, nil, nil, nil).Routes()
	request := httptest.NewRequest(http.MethodGet, "/api/account", nil)
	request.Header.Set("Accept", "application/vnd.nexdrop.v1+json")
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Header().Get("X-NexDrop-API-Version") != "1" {
		t.Fatalf("API version header = %q", response.Header().Get("X-NexDrop-API-Version"))
	}
	requestID := response.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("X-Request-ID is empty")
	}
	var body struct {
		Error struct {
			Code      string         `json:"code"`
			Message   string         `json:"message"`
			RequestID string         `json:"request_id"`
			Details   map[string]any `json:"details"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != "INVALID_TOKEN" || body.Error.Message == "" || body.Error.RequestID != requestID || body.Error.Details == nil {
		t.Fatalf("error response = %+v", body.Error)
	}
}

func TestTransferRequestLogIncludesCorrelationFields(t *testing.T) {
	var output bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previous)

	handler := New(auth.NewService(&testStore{}, time.Minute, time.Hour), nil, nil, nil, nil, nil, nil, nil).Routes()
	const transferID = "11111111-1111-4111-8111-111111111111"
	request := httptest.NewRequest(http.MethodGet, "/api/transfers/"+transferID, nil)
	request.Header.Set("Authorization", "Bearer invalid")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var record map[string]any
	if err := json.Unmarshal(output.Bytes(), &record); err != nil {
		t.Fatal(err)
	}
	if record["request_id"] == "" || record["transfer_id"] != transferID || record["error_code"] != "INVALID_TOKEN" {
		t.Fatalf("correlation fields = %#v", record)
	}
}

func TestLegacyErrorRemainsCompatible(t *testing.T) {
	handler := New(nil, nil, nil, nil, nil, nil, nil, nil).Routes()
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/account", nil))

	var body map[string]string
	if err := json.NewDecoder(response.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "INVALID_TOKEN" {
		t.Fatalf("legacy error = %#v", body)
	}
}

func TestLoginRateLimitReturnsRetryAfter(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)}}
	handler := New(auth.NewService(store, time.Minute, time.Hour), nil, nil, nil, nil, nil, nil, nil).Routes()
	var response *httptest.ResponseRecorder
	for range 11 {
		request := httptest.NewRequest(http.MethodPost, "/api/auth/login", bytes.NewBufferString(`{"identifier":"owner","password":"wrong"}`))
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
	}
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("status = %d, Retry-After = %q, body = %s", response.Code, response.Header().Get("Retry-After"), response.Body.String())
	}
}

func TestAdminRateLimitUsesSessionIdentity(t *testing.T) {
	api := New(auth.NewService(&testStore{}, time.Minute, time.Hour), nil, nil, nil, nil, nil, nil, nil)
	api.adminLimit = newFixedWindowLimiter(1)
	handler := api.Routes()

	request := func(token string) *httptest.ResponseRecorder {
		value := httptest.NewRequest(http.MethodGet, "/api/admin/users", nil)
		value.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, value)
		return response
	}

	if response := request("session-a"); response.Code == http.StatusTooManyRequests {
		t.Fatalf("first session-a request status = %d", response.Code)
	}
	if response := request("session-b"); response.Code == http.StatusTooManyRequests {
		t.Fatalf("first session-b request status = %d", response.Code)
	}
	response := request("session-a")
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("second session-a status = %d, Retry-After = %q", response.Code, response.Header().Get("Retry-After"))
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

func TestVersionedTransferCreationRequiresIdempotencyKey(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	senderDeviceID := "sender-device"
	store := &testStore{credential: auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)}, sessionDeviceID: &senderDeviceID}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, transfer.NewService(store), nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/transfers", bytes.NewBufferString(`{}`))
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	request.Header.Set("Accept", versionMediaType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	if response.Code != http.StatusBadRequest || !strings.Contains(response.Body.String(), "IDEMPOTENCY_KEY_REQUIRED") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestVersionedTransferListUsesPageEnvelope(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device-1"
	store := &testStore{credential: auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)}, sessionDeviceID: &deviceID}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, transfer.NewService(store), nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/transfers?limit=25", nil)
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	request.Header.Set("Accept", versionMediaType)
	response := httptest.NewRecorder()

	handler.ServeHTTP(response, request)

	var page transfer.Page
	if response.Code != http.StatusOK || json.NewDecoder(response.Body).Decode(&page) != nil || page.Items == nil {
		t.Fatalf("status = %d, page = %+v, body = %s", response.Code, page, response.Body.String())
	}
}

func TestLegacyTransferListRemainsArray(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device-1"
	store := &testStore{credential: auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)}, sessionDeviceID: &deviceID}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, transfer.NewService(store), nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/api/transfers", nil)
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK || !strings.HasPrefix(strings.TrimSpace(response.Body.String()), "[") {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
}

func TestVersionedTransferCursorIsSignedAndTamperProof(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	deviceID := "device-1"
	createdAt := time.Date(2026, 7, 16, 8, 30, 0, 123, time.UTC)
	store := &testStore{
		credential:      auth.Credential{User: auth.User{ID: "user-1", Username: "owner"}, PasswordHash: string(passwordHash)},
		sessionDeviceID: &deviceID,
		page: transfer.Page{
			Items:      []transfer.Transfer{{ID: "11111111-1111-1111-1111-111111111111", CreatedAt: createdAt}},
			NextCursor: "11111111-1111-1111-1111-111111111111",
			NextPageKey: transfer.PageKey{
				ID: "11111111-1111-1111-1111-111111111111", CreatedAt: createdAt,
			},
		},
	}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := New(authService, nil, nil, nil, transfer.NewService(store), nil, nil, nil).Routes()
	pair, err := authService.Login(context.Background(), "owner", "password")
	if err != nil {
		t.Fatal(err)
	}
	first := httptest.NewRequest(http.MethodGet, "/api/transfers?limit=1", nil)
	first.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	first.Header.Set("Accept", versionMediaType)
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	var page transfer.Page
	if firstResponse.Code != http.StatusOK || json.NewDecoder(firstResponse.Body).Decode(&page) != nil || page.NextCursor == "" || page.NextCursor == store.page.NextCursor {
		t.Fatalf("status = %d, page = %+v", firstResponse.Code, page)
	}

	valid := httptest.NewRequest(http.MethodGet, "/api/transfers?limit=1&cursor="+url.QueryEscape(page.NextCursor), nil)
	valid.Header = first.Header.Clone()
	validResponse := httptest.NewRecorder()
	handler.ServeHTTP(validResponse, valid)
	if validResponse.Code != http.StatusOK || store.pageOptions.Cursor.ID != store.page.NextCursor || !store.pageOptions.Cursor.CreatedAt.Equal(createdAt) {
		t.Fatalf("valid cursor status = %d, options = %+v", validResponse.Code, store.pageOptions)
	}

	replacement := "A"
	if strings.HasSuffix(page.NextCursor, replacement) {
		replacement = "B"
	}
	tamperedCursor := page.NextCursor[:len(page.NextCursor)-1] + replacement
	tampered := httptest.NewRequest(http.MethodGet, "/api/transfers?cursor="+url.QueryEscape(tamperedCursor), nil)
	tampered.Header = first.Header.Clone()
	tamperedResponse := httptest.NewRecorder()
	handler.ServeHTTP(tamperedResponse, tampered)
	if tamperedResponse.Code != http.StatusBadRequest || !strings.Contains(tamperedResponse.Body.String(), "INVALID_PAGE") {
		t.Fatalf("tampered cursor status = %d, body = %s", tamperedResponse.Code, tamperedResponse.Body.String())
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

func TestAdminAPIRequiresReauthentication(t *testing.T) {
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
	unverifiedAudit := httptest.NewRequest(http.MethodGet, "/api/admin/audit-logs", nil)
	unverifiedAudit.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	unverifiedAuditResponse := httptest.NewRecorder()
	handler.ServeHTTP(unverifiedAuditResponse, unverifiedAudit)
	if unverifiedAuditResponse.Code != http.StatusForbidden {
		t.Fatalf("unverified audit status = %d, want %d", unverifiedAuditResponse.Code, http.StatusForbidden)
	}
	store.adminVerified = true

	request := httptest.NewRequest(http.MethodGet, "/api/admin/settings", nil)
	request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("admin settings status = %d, body = %s", response.Code, response.Body.String())
	}
	for _, endpoint := range []struct {
		method string
		path   string
		status int
	}{
		{http.MethodGet, "/api/admin/devices", http.StatusOK},
		{http.MethodPost, "/api/admin/devices/11111111-1111-1111-1111-111111111111/revoke", http.StatusNoContent},
		{http.MethodGet, "/api/admin/groups", http.StatusOK},
		{http.MethodDelete, "/api/admin/groups/22222222-2222-2222-2222-222222222222", http.StatusNoContent},
	} {
		request := httptest.NewRequest(endpoint.method, endpoint.path, nil)
		request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != endpoint.status {
			t.Fatalf("%s %s status = %d, body = %s", endpoint.method, endpoint.path, response.Code, response.Body.String())
		}
	}
	invitePayload, err := json.Marshal(map[string]any{"username": "invitee", "email": "invitee@example.com", "admin": false})
	if err != nil {
		t.Fatal(err)
	}
	inviteRequest := httptest.NewRequest(http.MethodPost, "/api/admin/invitations", bytes.NewReader(invitePayload))
	inviteRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	inviteResponse := httptest.NewRecorder()
	handler.ServeHTTP(inviteResponse, inviteRequest)
	if inviteResponse.Code != http.StatusCreated {
		t.Fatalf("create invitation status = %d, body = %s", inviteResponse.Code, inviteResponse.Body.String())
	}
	var invitation admin.Invitation
	if err := json.Unmarshal(inviteResponse.Body.Bytes(), &invitation); err != nil || invitation.Token == "" {
		t.Fatalf("invitation response = %+v, %v", invitation, err)
	}
	acceptPayload, err := json.Marshal(map[string]any{"token": invitation.Token, "password": "invited-password"})
	if err != nil {
		t.Fatal(err)
	}
	acceptResponse := httptest.NewRecorder()
	handler.ServeHTTP(acceptResponse, httptest.NewRequest(http.MethodPost, "/api/auth/invitations/accept", bytes.NewReader(acceptPayload)))
	if acceptResponse.Code != http.StatusCreated {
		t.Fatalf("accept invitation status = %d, body = %s", acceptResponse.Code, acceptResponse.Body.String())
	}
}

func TestVersionedAdminHistoryUsesSignedCursor(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Date(2026, 7, 16, 9, 0, 0, 0, time.UTC)
	auditID := "11111111-1111-4111-8111-111111111111"
	failureID := "22222222-2222-4222-8222-222222222222"
	store := &testStore{
		credential:    auth.Credential{User: auth.User{ID: "33333333-3333-4333-8333-333333333333", Username: "admin", Admin: true}, PasswordHash: string(passwordHash)},
		adminVerified: true,
		auditPage:     admin.AuditLogPage{Items: []admin.AuditLog{{ID: auditID, CreatedAt: createdAt}}, NextPageKey: admin.PageKey{ID: auditID, CreatedAt: createdAt}},
		failurePage:   admin.FailurePage{Items: []admin.Failure{{TransferID: failureID, CreatedAt: createdAt}}, NextPageKey: admin.PageKey{ID: failureID, CreatedAt: createdAt}},
	}
	authService := auth.NewService(store, time.Minute, time.Hour)
	handler := NewWithCursorKey([]byte("admin-history-test-cursor-secret-32-bytes"), authService, nil, nil, nil, nil, nil, nil, admin.NewService(store)).Routes()
	pair, err := authService.Login(context.Background(), "admin", "password")
	if err != nil {
		t.Fatal(err)
	}

	for _, test := range []struct {
		path    string
		options *admin.PageOptions
	}{
		{"/api/admin/audit-logs?limit=1&from=2026-07-01T00:00:00Z&to=2026-07-31T00:00:00Z", &store.auditPageOptions},
		{"/api/admin/failures?limit=1&status=FAILED", &store.failurePageOptions},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		request.Header.Set("Accept", versionMediaType)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusOK {
			t.Fatalf("%s status = %d, body = %s", test.path, response.Code, response.Body.String())
		}
		var page struct {
			NextCursor string `json:"nextCursor"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &page); err != nil || page.NextCursor == "" {
			t.Fatalf("%s page = %+v, %v", test.path, page, err)
		}
		if test.options.Limit != 1 {
			t.Fatalf("%s options = %+v", test.path, *test.options)
		}
		tampered := httptest.NewRequest(http.MethodGet, strings.Split(test.path, "?")[0]+"?cursor="+url.QueryEscape(page.NextCursor+"x"), nil)
		tampered.Header.Set("Authorization", "Bearer "+pair.AccessToken)
		tampered.Header.Set("Accept", versionMediaType)
		tamperedResponse := httptest.NewRecorder()
		handler.ServeHTTP(tamperedResponse, tampered)
		if tamperedResponse.Code != http.StatusBadRequest {
			t.Fatalf("tampered %s status = %d, body = %s", test.path, tamperedResponse.Code, tamperedResponse.Body.String())
		}
	}

	legacy := httptest.NewRequest(http.MethodGet, "/api/admin/audit-logs?limit=1&offset=0", nil)
	legacy.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	legacyResponse := httptest.NewRecorder()
	handler.ServeHTTP(legacyResponse, legacy)
	var legacyItems []admin.AuditLog
	if legacyResponse.Code != http.StatusOK || json.Unmarshal(legacyResponse.Body.Bytes(), &legacyItems) != nil {
		t.Fatalf("legacy status = %d, body = %s", legacyResponse.Code, legacyResponse.Body.String())
	}
}
