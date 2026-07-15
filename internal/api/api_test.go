package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
)

type testStore struct {
	credential auth.Credential
	accessHash []byte
	refresh    []byte
	devices    []device.Device
	groups     []group.Group
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
	return auth.Session{User: store.credential.User, SessionID: "session-1"}, nil
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

func TestLoginAndReadAccount(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{
		User:         auth.User{ID: "user-1", Username: "owner", Email: "owner@example.com"},
		PasswordHash: string(passwordHash),
	}}
	handler := New(auth.NewService(store, time.Minute, time.Hour), nil, nil, nil).Routes()

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
	handler := New(authService, device.NewService(store), pairing.NewService(store), nil).Routes()
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
	handler := New(authService, nil, nil, group.NewService(store)).Routes()
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
