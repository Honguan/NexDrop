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
)

type testStore struct {
	credential auth.Credential
	accessHash []byte
	refresh    []byte
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

func TestLoginAndReadAccount(t *testing.T) {
	passwordHash, err := bcrypt.GenerateFromPassword([]byte("password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	store := &testStore{credential: auth.Credential{
		User:         auth.User{ID: "user-1", Username: "owner", Email: "owner@example.com"},
		PasswordHash: string(passwordHash),
	}}
	handler := New(auth.NewService(store, time.Minute, time.Hour)).Routes()

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
