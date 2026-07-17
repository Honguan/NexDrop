package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type memoryStore struct {
	credential Credential
	sessions   map[string]Session
	refreshes  map[string]string
}

func newMemoryStore(t *testing.T) *memoryStore {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte("correct-password"), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	return &memoryStore{
		credential: Credential{User: User{ID: "user-1", Username: "owner", Email: "owner@example.com"}, PasswordHash: string(hash)},
		sessions:   make(map[string]Session),
		refreshes:  make(map[string]string),
	}
}

func (store *memoryStore) CredentialByIdentifier(_ context.Context, identifier string) (Credential, error) {
	if identifier != store.credential.Username && identifier != store.credential.Email {
		return Credential{}, errors.New("not found")
	}
	return store.credential, nil
}

func (store *memoryStore) CreateSession(_ context.Context, userID string, accessHash []byte, _ time.Time, refreshHash []byte, _ time.Time) (string, error) {
	sessionID := "session-1"
	store.sessions[string(accessHash)] = Session{User: store.credential.User, SessionID: sessionID}
	store.refreshes[string(refreshHash)] = sessionID
	return sessionID, nil
}

func (store *memoryStore) SessionByAccessToken(_ context.Context, accessHash []byte, _ time.Time) (Session, error) {
	session, ok := store.sessions[string(accessHash)]
	if !ok {
		return Session{}, ErrInvalidCredentials
	}
	return session, nil
}

func (store *memoryStore) RotateSession(_ context.Context, oldRefreshHash, accessHash []byte, _ time.Time, refreshHash []byte, _ time.Time, _ time.Time) error {
	sessionID, ok := store.refreshes[string(oldRefreshHash)]
	if !ok {
		return errors.New("not found")
	}
	delete(store.refreshes, string(oldRefreshHash))
	store.refreshes[string(refreshHash)] = sessionID
	store.sessions[string(accessHash)] = Session{User: store.credential.User, SessionID: sessionID}
	return nil
}

func (store *memoryStore) RevokeSessionByRefreshToken(_ context.Context, refreshHash []byte, _ time.Time) error {
	if _, ok := store.refreshes[string(refreshHash)]; !ok {
		return errors.New("not found")
	}
	delete(store.refreshes, string(refreshHash))
	return nil
}

func (store *memoryStore) SetTOTPSecret(_ context.Context, _ string, secret string) error {
	store.credential.TOTPSecret = secret
	store.credential.TOTPEnabled = true
	return nil
}

func (store *memoryStore) MarkAdminVerified(_ context.Context, sessionID string, _ time.Time) error {
	for key, session := range store.sessions {
		if session.SessionID == sessionID {
			session.AdminVerified = true
			store.sessions[key] = session
		}
	}
	return nil
}

func TestLoginAuthenticateAndLogout(t *testing.T) {
	store := newMemoryStore(t)
	service := NewService(store, 15*time.Minute, 24*time.Hour)
	pair, err := service.Login(context.Background(), "owner", "correct-password")
	if err != nil {
		t.Fatal(err)
	}
	if pair.AccessToken == "" || pair.RefreshToken == "" || pair.AccessToken == pair.RefreshToken {
		t.Fatal("service did not issue distinct access and refresh tokens")
	}

	session, err := service.Authenticate(context.Background(), pair.AccessToken)
	if err != nil {
		t.Fatal(err)
	}
	if session.ID != "user-1" {
		t.Fatalf("authenticated user = %q, want user-1", session.ID)
	}
	if _, err := service.Authenticate(context.Background(), "unknown-access-token"); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("unknown access token error = %v, want ErrInvalidCredentials", err)
	}
	refreshed, err := service.Refresh(context.Background(), pair.RefreshToken)
	if err != nil {
		t.Fatal(err)
	}
	if refreshed.AccessToken == pair.AccessToken || refreshed.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh did not rotate both tokens")
	}
	if _, err := service.Refresh(context.Background(), pair.RefreshToken); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("reused refresh token error = %v, want ErrInvalidCredentials", err)
	}
	if err := service.Logout(context.Background(), refreshed.RefreshToken); err != nil {
		t.Fatal(err)
	}
}

func TestLoginDoesNotRevealCredentialFailure(t *testing.T) {
	service := NewService(newMemoryStore(t), time.Minute, time.Hour)
	for _, test := range []struct{ identifier, password string }{
		{"missing", "correct-password"},
		{"owner", "wrong-password"},
	} {
		_, err := service.Login(context.Background(), test.identifier, test.password)
		if !errors.Is(err, ErrInvalidCredentials) {
			t.Fatalf("Login(%q) error = %v, want ErrInvalidCredentials", test.identifier, err)
		}
	}
}

func TestLoginRequiresConfiguredTOTP(t *testing.T) {
	store := newMemoryStore(t)
	store.credential.TOTPSecret = "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	service := NewService(store, time.Minute, time.Hour)
	service.now = func() time.Time { return time.Unix(59, 0) }
	if _, err := service.Login(context.Background(), "owner", "correct-password"); !errors.Is(err, ErrTOTPRequired) {
		t.Fatalf("login without TOTP error = %v, want ErrTOTPRequired", err)
	}
	if _, err := service.LoginWithTOTP(context.Background(), "owner", "correct-password", "287082"); err != nil {
		t.Fatalf("login with TOTP: %v", err)
	}
}
