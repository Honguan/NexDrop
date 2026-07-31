package enrollment

import (
	"context"
	"crypto/subtle"
	"sync"
	"testing"
	"time"
)

type memoryStore struct {
	mu     sync.Mutex
	grants map[string]Grant
	nextID int
}

func newMemoryStore() *memoryStore { return &memoryStore{grants: make(map[string]Grant)} }
func (store *memoryStore) SaveGrant(_ context.Context, grant Grant) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.grants[grant.ID] = grant
	return nil
}
func (store *memoryStore) ConsumeGrant(_ context.Context, id string, hash []byte, now time.Time) (Grant, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	grant, ok := store.grants[id]
	if !ok || subtle.ConstantTimeCompare(grant.TokenHash, hash) != 1 {
		return Grant{}, ErrInvalidToken
	}
	if grant.RevokedAt != nil {
		return Grant{}, ErrRevoked
	}
	if now.After(grant.ExpiresAt) {
		return Grant{}, ErrExpiredToken
	}
	if grant.Uses >= grant.MaxUses {
		return Grant{}, ErrExhausted
	}
	grant.Uses++
	store.grants[id] = grant
	return grant, nil
}
func (store *memoryStore) CreateDeviceCredential(_ context.Context, _ Grant, _ CredentialRecord) (string, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.nextID++
	return "device-" + string(rune('0'+store.nextID)), nil
}
func (store *memoryStore) RevokeGrant(_ context.Context, id string, at time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	grant := store.grants[id]
	grant.RevokedAt = &at
	store.grants[id] = grant
	return nil
}

func TestOneTimeTokenCannotBeReplayed(t *testing.T) {
	store := newMemoryStore()
	service, err := New(store, "node-test", []byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	issued, err := service.Issue(context.Background(), IssueRequest{TTL: time.Minute, MaxUses: 1, DeviceType: "ANDROID", NameHint: "Phone", Permissions: Permissions{SendFiles: true}})
	if err != nil {
		t.Fatal(err)
	}
	credential, err := service.Redeem(context.Background(), RedeemRequest{Token: issued.Token, DeviceType: "ANDROID", DeviceName: "Phone"})
	if err != nil || credential.DeviceID == "" || credential.Secret == "" {
		t.Fatalf("unexpected credential: %#v %v", credential, err)
	}
	if _, err := service.Redeem(context.Background(), RedeemRequest{Token: issued.Token, DeviceType: "ANDROID", DeviceName: "Phone"}); err != ErrExhausted {
		t.Fatalf("expected exhausted token, got %v", err)
	}
}

func TestTamperedTokenIsRejected(t *testing.T) {
	service, _ := New(newMemoryStore(), "node-test", []byte("0123456789abcdef0123456789abcdef"))
	issued, _ := service.Issue(context.Background(), IssueRequest{TTL: time.Minute, MaxUses: 1})
	tampered := issued.Token[:len(issued.Token)-1] + "x"
	if _, err := service.Redeem(context.Background(), RedeemRequest{Token: tampered, DeviceName: "Device"}); err != ErrInvalidToken {
		t.Fatalf("expected invalid token, got %v", err)
	}
}

func TestExpiredTokenIsRejectedWithClockSkew(t *testing.T) {
	store := newMemoryStore()
	service, _ := New(store, "node-test", []byte("0123456789abcdef0123456789abcdef"))
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return base }
	issued, _ := service.Issue(context.Background(), IssueRequest{TTL: time.Minute, MaxUses: 1})
	service.now = func() time.Time { return base.Add(2 * time.Minute) }
	if _, err := service.Redeem(context.Background(), RedeemRequest{Token: issued.Token, DeviceName: "Device"}); err != ErrExpiredToken {
		t.Fatalf("expected expired token, got %v", err)
	}
}
