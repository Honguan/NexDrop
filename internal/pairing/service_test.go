package pairing

import (
	"context"
	"strings"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

type fakeStore struct {
	createdHash []byte
	expiresAt   time.Time
}

func (store *fakeStore) CreatePairingCode(_ context.Context, _ auth.Session, _ string, codeHash []byte, expiresAt, _ time.Time) (string, error) {
	store.createdHash = append([]byte(nil), codeHash...)
	store.expiresAt = expiresAt
	return "challenge-1", nil
}

func (*fakeStore) RedeemPairingCode(_ context.Context, _ auth.Session, deviceID, _ string, _ []byte, _ time.Time, _ int) (device.Device, error) {
	return device.Device{ID: deviceID, TrustStatus: device.TrustTrusted}, nil
}

func TestCreatePairingChallenge(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.UTC)
	service.now = func() time.Time { return now }

	challenge, err := service.Create(context.Background(), auth.Session{}, "device-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(challenge.Code) != 6 || strings.Trim(challenge.Code, "0123456789") != "" {
		t.Fatalf("code = %q, want six digits", challenge.Code)
	}
	if !challenge.ExpiresAt.Equal(now.Add(10*time.Minute)) || !store.expiresAt.Equal(challenge.ExpiresAt) {
		t.Fatalf("expiresAt = %v", challenge.ExpiresAt)
	}
	if len(store.createdHash) != 32 || strings.Contains(challenge.QRPayload, "device-1") == false {
		t.Fatal("challenge hash or QR payload is invalid")
	}
}

func TestRedeemValidatesCodeFormat(t *testing.T) {
	service := NewService(&fakeStore{})
	for _, code := range []string{"12345", "1234567", "12A456"} {
		if _, err := service.Redeem(context.Background(), auth.Session{}, "device-1", "challenge-1", code); err != ErrInvalidCode {
			t.Fatalf("Redeem(%q) error = %v, want ErrInvalidCode", code, err)
		}
	}
}
