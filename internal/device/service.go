package device

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"nexdrop/internal/auth"
)

var (
	ErrInvalid   = errors.New("invalid device")
	ErrNotFound  = errors.New("device not found")
	ErrForbidden = errors.New("device operation forbidden")
)

type Type string

const (
	TypeWindows   Type = "WINDOWS"
	TypeAndroid   Type = "ANDROID"
	TypeWebChrome Type = "WEB_CHROME"
	TypeWebEdge   Type = "WEB_EDGE"
)

type TrustStatus string

const (
	TrustPending TrustStatus = "PENDING"
	TrustTrusted TrustStatus = "TRUSTED"
	TrustRevoked TrustStatus = "REVOKED"
)

type Device struct {
	ID          string      `json:"id"`
	DisplayName string      `json:"displayName"`
	Type        Type        `json:"type"`
	PublicKey   []byte      `json:"publicKey,omitempty"`
	Algorithm   string      `json:"keyAlgorithm,omitempty"`
	TrustStatus TrustStatus `json:"trustStatus"`
	RevokedAt   *time.Time  `json:"revokedAt,omitempty"`
	CreatedAt   time.Time   `json:"createdAt"`
}

type SessionChallenge struct {
	ID                 string    `json:"id"`
	SessionID          string    `json:"sessionId"`
	EphemeralPublicKey []byte    `json:"ephemeralPublicKey"`
	Nonce              []byte    `json:"nonce"`
	ExpiresAt          time.Time `json:"expiresAt"`
}

type Store interface {
	CreateDevice(context.Context, auth.Session, string, Type, []byte, string) (Device, error)
	ListDevices(context.Context, string) ([]Device, error)
	RenameDevice(context.Context, string, string, string) (Device, error)
	DeleteDevice(context.Context, string, string) error
	ApproveDevice(context.Context, auth.Session, string) (Device, error)
	RevokeDevice(context.Context, auth.Session, string, time.Time) (Device, error)
	DevicePublicKeyForSession(context.Context, auth.Session, string) ([]byte, error)
	CreateDeviceSessionChallenge(context.Context, auth.Session, string, []byte, time.Time, time.Time) (string, error)
	RedeemDeviceSessionChallenge(context.Context, auth.Session, string, string, []byte, time.Time, int) error
}

const (
	sessionChallengeTTL      = 5 * time.Minute
	maximumChallengeAttempts = 5
)

func (service *Service) CreateSessionChallenge(ctx context.Context, session auth.Session, deviceID string) (SessionChallenge, error) {
	if deviceID == "" {
		return SessionChallenge{}, ErrInvalid
	}
	publicKey, err := service.store.DevicePublicKeyForSession(ctx, session, deviceID)
	if err != nil {
		return SessionChallenge{}, err
	}
	recipient, err := ecdh.X25519().NewPublicKey(publicKey)
	if err != nil {
		return SessionChallenge{}, ErrInvalid
	}
	ephemeral, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		return SessionChallenge{}, fmt.Errorf("generate session challenge key: %w", err)
	}
	shared, err := ephemeral.ECDH(recipient)
	if err != nil {
		return SessionChallenge{}, ErrInvalid
	}
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return SessionChallenge{}, fmt.Errorf("generate session challenge nonce: %w", err)
	}
	proof := sessionProof(shared, session.SessionID, nonce)
	proofHash := sha256.Sum256(proof)
	now := service.now().UTC()
	id, err := service.store.CreateDeviceSessionChallenge(ctx, session, deviceID, proofHash[:], now.Add(sessionChallengeTTL), now)
	if err != nil {
		return SessionChallenge{}, err
	}
	return SessionChallenge{ID: id, SessionID: session.SessionID, EphemeralPublicKey: ephemeral.PublicKey().Bytes(), Nonce: nonce, ExpiresAt: now.Add(sessionChallengeTTL)}, nil
}

func (service *Service) AttachSession(ctx context.Context, session auth.Session, deviceID, challengeID string, proof []byte) error {
	if deviceID == "" || challengeID == "" || len(proof) != sha256.Size {
		return ErrInvalid
	}
	proofHash := sha256.Sum256(proof)
	return service.store.RedeemDeviceSessionChallenge(ctx, session, deviceID, challengeID, proofHash[:], service.now().UTC(), maximumChallengeAttempts)
}

func sessionProof(shared []byte, sessionID string, nonce []byte) []byte {
	mac := hmac.New(sha256.New, shared)
	_, _ = mac.Write([]byte("nexdrop/session-attach/v1"))
	_, _ = mac.Write([]byte(sessionID))
	_, _ = mac.Write(nonce)
	return mac.Sum(nil)
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) Create(ctx context.Context, session auth.Session, name string, deviceType Type, publicKey []byte, algorithm string) (Device, error) {
	name = strings.TrimSpace(name)
	algorithm = strings.TrimSpace(algorithm)
	if name == "" || len(name) > 100 || !validType(deviceType) || len(publicKey) != 32 || algorithm != "X25519" {
		return Device{}, ErrInvalid
	}
	return service.store.CreateDevice(ctx, session, name, deviceType, publicKey, algorithm)
}

func (service *Service) List(ctx context.Context, session auth.Session) ([]Device, error) {
	return service.store.ListDevices(ctx, session.ID)
}

func (service *Service) Rename(ctx context.Context, session auth.Session, id, name string) (Device, error) {
	name = strings.TrimSpace(name)
	if id == "" || name == "" || len(name) > 100 {
		return Device{}, ErrInvalid
	}
	return service.store.RenameDevice(ctx, session.ID, id, name)
}

func (service *Service) Delete(ctx context.Context, session auth.Session, id string) error {
	if id == "" {
		return ErrInvalid
	}
	return service.store.DeleteDevice(ctx, session.ID, id)
}

func (service *Service) Approve(ctx context.Context, session auth.Session, id string) (Device, error) {
	if id == "" {
		return Device{}, ErrInvalid
	}
	return service.store.ApproveDevice(ctx, session, id)
}

func (service *Service) Revoke(ctx context.Context, session auth.Session, id string) (Device, error) {
	if id == "" {
		return Device{}, ErrInvalid
	}
	return service.store.RevokeDevice(ctx, session, id, service.now().UTC())
}

func validType(value Type) bool {
	switch value {
	case TypeWindows, TypeAndroid, TypeWebChrome, TypeWebEdge:
		return true
	default:
		return false
	}
}
