package device

import (
	"context"
	"crypto/ecdh"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
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
	ID             string      `json:"id"`
	DisplayName    string      `json:"displayName"`
	Type           Type        `json:"type"`
	PublicKey      []byte      `json:"publicKey,omitempty"`
	Algorithm      string      `json:"keyAlgorithm,omitempty"`
	LANShortID     string      `json:"lanShortId,omitempty"`
	LANFingerprint string      `json:"lanCertificateFingerprint,omitempty"`
	LANCertificate string      `json:"lanCertificate,omitempty"`
	TrustStatus    TrustStatus `json:"trustStatus"`
	Online         bool        `json:"online"`
	LastSeenAt     *time.Time  `json:"lastSeenAt,omitempty"`
	RevokedAt      *time.Time  `json:"revokedAt,omitempty"`
	CreatedAt      time.Time   `json:"createdAt"`
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
	RevokeDevice(context.Context, auth.Session, string, time.Time) (Device, error)
	DevicePublicKeyForSession(context.Context, auth.Session, string) ([]byte, error)
	CreateDeviceSessionChallenge(context.Context, auth.Session, string, []byte, time.Time, time.Time) (string, error)
	RedeemDeviceSessionChallenge(context.Context, auth.Session, string, string, []byte, time.Time, int) error
	RegisterLANIdentity(context.Context, auth.Session, string, string, string, string, time.Time) error
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

func (service *Service) Revoke(ctx context.Context, session auth.Session, id string) (Device, error) {
	if id == "" {
		return Device{}, ErrInvalid
	}
	return service.store.RevokeDevice(ctx, session, id, service.now().UTC())
}

type LANIdentity struct {
	ShortDeviceID string `json:"shortDeviceId"`
	Fingerprint   string `json:"certificateFingerprint"`
	Certificate   string `json:"certificate"`
}

func (service *Service) RegisterLANIdentity(ctx context.Context, session auth.Session, id, certificatePEM string) (LANIdentity, error) {
	compactID := strings.ReplaceAll(id, "-", "")
	if session.DeviceID == nil || *session.DeviceID != id {
		return LANIdentity{}, ErrForbidden
	}
	if len(compactID) != 32 || len(certificatePEM) > 16384 {
		return LANIdentity{}, ErrInvalid
	}
	for _, character := range compactID {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return LANIdentity{}, ErrInvalid
		}
	}
	shortID := compactID[:12]
	block, remainder := pem.Decode([]byte(certificatePEM))
	if block == nil || block.Type != "CERTIFICATE" || len(strings.TrimSpace(string(remainder))) != 0 {
		return LANIdentity{}, ErrInvalid
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	now := service.now().UTC()
	if err != nil || certificate.Subject.CommonName != "nexdrop:"+shortID || now.Before(certificate.NotBefore) || now.After(certificate.NotAfter) || certificate.CheckSignature(certificate.SignatureAlgorithm, certificate.RawTBSCertificate, certificate.Signature) != nil {
		return LANIdentity{}, ErrInvalid
	}
	digest := sha256.Sum256(block.Bytes)
	fingerprint := hex.EncodeToString(digest[:])
	normalized := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: block.Bytes}))
	if err := service.store.RegisterLANIdentity(ctx, session, id, shortID, fingerprint, normalized, now); err != nil {
		return LANIdentity{}, err
	}
	return LANIdentity{ShortDeviceID: shortID, Fingerprint: fingerprint, Certificate: normalized}, nil
}

func validType(value Type) bool {
	switch value {
	case TypeWindows, TypeAndroid, TypeWebChrome, TypeWebEdge:
		return true
	default:
		return false
	}
}
