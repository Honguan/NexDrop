package pairing

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"net/url"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

const (
	CodeLifetime        = 10 * time.Minute
	MaximumCodeAttempts = 5
)

var (
	ErrInvalidCode = errors.New("invalid pairing code")
	ErrExpired     = errors.New("pairing code expired")
	ErrUsed        = errors.New("pairing code already used")
	ErrLocked      = errors.New("pairing code locked")
)

type Store interface {
	CreatePairingCode(context.Context, auth.Session, string, []byte, time.Time, time.Time) (string, error)
	RedeemPairingCode(context.Context, auth.Session, string, string, []byte, time.Time, int) (device.Device, error)
}

type Challenge struct {
	ID        string    `json:"id"`
	DeviceID  string    `json:"deviceId"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
	QRPayload string    `json:"qrPayload"`
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) Create(ctx context.Context, session auth.Session, deviceID string) (Challenge, error) {
	if deviceID == "" {
		return Challenge{}, device.ErrInvalid
	}
	code, err := newCode()
	if err != nil {
		return Challenge{}, err
	}
	now := service.now().UTC()
	expiresAt := now.Add(CodeLifetime)
	id, err := service.store.CreatePairingCode(ctx, session, deviceID, hashCode(code), expiresAt, now)
	if err != nil {
		return Challenge{}, err
	}
	query := url.Values{"id": {id}, "device": {deviceID}, "code": {code}}
	return Challenge{
		ID:        id,
		DeviceID:  deviceID,
		Code:      code,
		ExpiresAt: expiresAt,
		QRPayload: "nexdrop://pair?" + query.Encode(),
	}, nil
}

func (service *Service) Redeem(ctx context.Context, session auth.Session, deviceID, challengeID, code string) (device.Device, error) {
	if deviceID == "" || challengeID == "" || len(code) != 6 {
		return device.Device{}, ErrInvalidCode
	}
	for _, character := range code {
		if character < '0' || character > '9' {
			return device.Device{}, ErrInvalidCode
		}
	}
	return service.store.RedeemPairingCode(
		ctx, session, deviceID, challengeID, hashCode(code), service.now().UTC(), MaximumCodeAttempts,
	)
}

func newCode() (string, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate pairing code: %w", err)
	}
	return fmt.Sprintf("%06d", value.Int64()), nil
}

func hashCode(code string) []byte {
	digest := sha256.Sum256([]byte(code))
	return digest[:]
}
