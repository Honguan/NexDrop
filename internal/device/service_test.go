package device

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"time"

	"nexdrop/internal/auth"
)

type fakeStore struct {
	created bool
	proof   []byte
	lanID   string
}

func (store *fakeStore) CreateDevice(_ context.Context, _ auth.Session, name string, deviceType Type, publicKey []byte, algorithm string) (Device, error) {
	store.created = true
	return Device{ID: "device-1", DisplayName: name, Type: deviceType, PublicKey: publicKey, Algorithm: algorithm, TrustStatus: TrustPending}, nil
}

func (*fakeStore) ListDevices(context.Context, string) ([]Device, error) {
	return []Device{{ID: "device-1"}}, nil
}

func (*fakeStore) RenameDevice(_ context.Context, _, id, name string) (Device, error) {
	return Device{ID: id, DisplayName: name}, nil
}

func (*fakeStore) DeleteDevice(context.Context, string, string) error { return nil }

func (*fakeStore) ApproveDevice(_ context.Context, _ auth.Session, id string) (Device, error) {
	return Device{ID: id, TrustStatus: TrustTrusted}, nil
}

func (*fakeStore) RevokeDevice(_ context.Context, _ auth.Session, id string, now time.Time) (Device, error) {
	return Device{ID: id, TrustStatus: TrustRevoked, RevokedAt: &now}, nil
}

func (*fakeStore) DevicePublicKeyForSession(context.Context, auth.Session, string) ([]byte, error) {
	pair, _ := ecdh.X25519().GenerateKey(rand.Reader)
	return pair.PublicKey().Bytes(), nil
}

func (store *fakeStore) CreateDeviceSessionChallenge(_ context.Context, _ auth.Session, _ string, proof []byte, _ time.Time, _ time.Time) (string, error) {
	store.proof = append([]byte(nil), proof...)
	return "challenge-1", nil
}

func (*fakeStore) RedeemDeviceSessionChallenge(context.Context, auth.Session, string, string, []byte, time.Time, int) error {
	return nil
}

func (store *fakeStore) RegisterLANIdentity(_ context.Context, _ auth.Session, _, shortID, _ string, _ time.Time) error {
	store.lanID = shortID
	return nil
}

func TestCreateValidatesDeviceIdentity(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	key := make([]byte, 32)
	device, err := service.Create(context.Background(), auth.Session{}, " Laptop ", TypeWindows, key, "X25519")
	if err != nil {
		t.Fatal(err)
	}
	if !store.created || device.DisplayName != "Laptop" || device.TrustStatus != TrustPending {
		t.Fatalf("unexpected device: %+v", device)
	}

	invalid := []struct {
		name      string
		kind      Type
		key       []byte
		algorithm string
	}{
		{"", TypeWindows, key, "X25519"},
		{"Laptop", "IOS", key, "X25519"},
		{"Laptop", TypeWindows, make([]byte, 31), "X25519"},
		{"Laptop", TypeWindows, key, "RSA"},
	}
	for _, test := range invalid {
		_, err := service.Create(context.Background(), auth.Session{}, test.name, test.kind, test.key, test.algorithm)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("Create(%q, %q) error = %v, want ErrInvalid", test.name, test.kind, err)
		}
	}
}

func TestDeviceLifecycle(t *testing.T) {
	service := NewService(&fakeStore{})
	session := auth.Session{User: auth.User{ID: "user-1"}, SessionID: "session-1"}
	devices, err := service.List(context.Background(), session)
	if err != nil || len(devices) != 1 {
		t.Fatalf("List() = %+v, %v", devices, err)
	}
	renamed, err := service.Rename(context.Background(), session, "device-1", "Phone")
	if err != nil || renamed.DisplayName != "Phone" {
		t.Fatalf("Rename() = %+v, %v", renamed, err)
	}
	approved, err := service.Approve(context.Background(), session, "device-1")
	if err != nil || approved.TrustStatus != TrustTrusted {
		t.Fatalf("Approve() = %+v, %v", approved, err)
	}
	revoked, err := service.Revoke(context.Background(), session, "device-1")
	if err != nil || revoked.TrustStatus != TrustRevoked || revoked.RevokedAt == nil {
		t.Fatalf("Revoke() = %+v, %v", revoked, err)
	}
}

func TestRegisterLANIdentityRequiresOwningDeviceSession(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	deviceID := "12345678-abcd-4000-8000-123456789abc"
	session := auth.Session{DeviceID: &deviceID}
	shortID, err := service.RegisterLANIdentity(context.Background(), session, deviceID, strings.Repeat("a", 64))
	if err != nil {
		t.Fatal(err)
	}
	if shortID != "12345678abcd" || store.lanID != shortID {
		t.Fatalf("short ID = %q, stored = %q", shortID, store.lanID)
	}
	otherID := "aaaaaaaa-bbbb-4000-8000-123456789abc"
	if _, err := service.RegisterLANIdentity(context.Background(), session, otherID, strings.Repeat("a", 64)); !errors.Is(err, ErrForbidden) {
		t.Fatalf("other device error = %v, want ErrForbidden", err)
	}
}

func TestCreateSessionChallenge(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	session := auth.Session{User: auth.User{ID: "user-1"}, SessionID: "session-1"}
	challenge, err := service.CreateSessionChallenge(context.Background(), session, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if challenge.ID != "challenge-1" || challenge.SessionID != session.SessionID || len(challenge.EphemeralPublicKey) != 32 || len(challenge.Nonce) != 32 || len(store.proof) != 32 {
		t.Fatalf("challenge = %+v, proof length = %d", challenge, len(store.proof))
	}
}
