package enrollment

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

var (
	ErrInvalidToken = errors.New("invalid enrollment token")
	ErrExpiredToken = errors.New("expired enrollment token")
	ErrExhausted    = errors.New("enrollment token exhausted")
	ErrRevoked      = errors.New("enrollment token revoked")
)

type Permissions struct {
	SendFiles        bool `json:"sendFiles"`
	ReceiveBroadcast bool `json:"receiveBroadcast"`
}

type Grant struct {
	ID         string
	NodeID     string
	TokenHash  []byte
	ExpiresAt  time.Time
	MaxUses    int
	Uses       int
	DeviceType string
	NameHint   string
	OwnerID    string
	Permissions Permissions
	RevokedAt  *time.Time
}

type CredentialRecord struct {
	DeviceType string
	Name       string
	OwnerID    string
	SecretHash []byte
	Permissions Permissions
	CreatedAt  time.Time
}

type Store interface {
	SaveGrant(context.Context, Grant) error
	ConsumeGrant(context.Context, string, []byte, time.Time) (Grant, error)
	CreateDeviceCredential(context.Context, Grant, CredentialRecord) (string, error)
	RevokeGrant(context.Context, string, time.Time) error
}

type Service struct {
	store      Store
	nodeID     string
	rootSecret []byte
	now        func() time.Time
	clockSkew  time.Duration
}

func New(store Store, nodeID string, rootSecret []byte) (*Service, error) {
	nodeID = strings.TrimSpace(nodeID)
	if store == nil || len(nodeID) < 8 || len(rootSecret) < 32 {
		return nil, errors.New("invalid enrollment configuration")
	}
	return &Service{store: store, nodeID: nodeID, rootSecret: append([]byte(nil), rootSecret...), now: time.Now, clockSkew: 30 * time.Second}, nil
}

type IssueRequest struct {
	TTL         time.Duration
	MaxUses     int
	DeviceType string
	NameHint   string
	OwnerID    string
	Permissions Permissions
}

type Issued struct {
	GrantID   string    `json:"grantId"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type claims struct {
	Version int    `json:"v"`
	GrantID string `json:"gid"`
	NodeID  string `json:"nid"`
	Expires int64  `json:"exp"`
	Nonce   string `json:"nonce"`
}

func (service *Service) Issue(ctx context.Context, request IssueRequest) (Issued, error) {
	if request.TTL <= 0 || request.TTL > 24*time.Hour || request.MaxUses < 1 || request.MaxUses > 100 || len(request.DeviceType) > 50 || len(request.NameHint) > 100 || len(request.OwnerID) > 100 {
		return Issued{}, ErrInvalidToken
	}
	grantID, err := randomID(16)
	if err != nil {
		return Issued{}, err
	}
	nonce, err := randomID(16)
	if err != nil {
		return Issued{}, err
	}
	expiresAt := service.now().UTC().Add(request.TTL)
	payload, err := json.Marshal(claims{Version: 1, GrantID: grantID, NodeID: service.nodeID, Expires: expiresAt.Unix(), Nonce: nonce})
	if err != nil {
		return Issued{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	token := "v1." + encoded + "." + service.signature(encoded)
	digest := sha256.Sum256([]byte(token))
	grant := Grant{
		ID: grantID, NodeID: service.nodeID, TokenHash: digest[:], ExpiresAt: expiresAt, MaxUses: request.MaxUses,
		DeviceType: strings.TrimSpace(request.DeviceType), NameHint: strings.TrimSpace(request.NameHint), OwnerID: strings.TrimSpace(request.OwnerID), Permissions: request.Permissions,
	}
	if err := service.store.SaveGrant(ctx, grant); err != nil {
		return Issued{}, err
	}
	return Issued{GrantID: grantID, Token: token, ExpiresAt: expiresAt}, nil
}

type RedeemRequest struct {
	Token      string
	DeviceType string
	DeviceName string
	OwnerID    string
}

type Credential struct {
	DeviceID   string      `json:"deviceId"`
	Secret     string      `json:"deviceCredential"`
	Permissions Permissions `json:"permissions"`
}

func (service *Service) Redeem(ctx context.Context, request RedeemRequest) (Credential, error) {
	parsed, err := service.parse(request.Token)
	if err != nil {
		return Credential{}, err
	}
	now := service.now().UTC()
	if now.After(time.Unix(parsed.Expires, 0).Add(service.clockSkew)) {
		return Credential{}, ErrExpiredToken
	}
	digest := sha256.Sum256([]byte(request.Token))
	grant, err := service.store.ConsumeGrant(ctx, parsed.GrantID, digest[:], now)
	if err != nil {
		return Credential{}, err
	}
	if grant.NodeID != service.nodeID || (!grant.ExpiresAt.IsZero() && now.After(grant.ExpiresAt.Add(service.clockSkew))) {
		return Credential{}, ErrExpiredToken
	}
	deviceType := strings.TrimSpace(request.DeviceType)
	if grant.DeviceType != "" && deviceType != grant.DeviceType {
		return Credential{}, ErrInvalidToken
	}
	name := strings.TrimSpace(request.DeviceName)
	if name == "" {
		name = grant.NameHint
	}
	if name == "" || len(name) > 100 || (grant.OwnerID != "" && request.OwnerID != grant.OwnerID) {
		return Credential{}, ErrInvalidToken
	}
	secret, err := randomID(32)
	if err != nil {
		return Credential{}, err
	}
	secretHash := sha256.Sum256([]byte(secret))
	deviceID, err := service.store.CreateDeviceCredential(ctx, grant, CredentialRecord{
		DeviceType: deviceType, Name: name, OwnerID: request.OwnerID, SecretHash: secretHash[:], Permissions: grant.Permissions, CreatedAt: now,
	})
	if err != nil {
		return Credential{}, err
	}
	return Credential{DeviceID: deviceID, Secret: secret, Permissions: grant.Permissions}, nil
}

func (service *Service) Revoke(ctx context.Context, grantID string) error {
	if strings.TrimSpace(grantID) == "" {
		return ErrInvalidToken
	}
	return service.store.RevokeGrant(ctx, grantID, service.now().UTC())
}

func (service *Service) parse(token string) (claims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" || parts[1] == "" || parts[2] == "" {
		return claims{}, ErrInvalidToken
	}
	expected := service.signature(parts[1])
	if len(expected) != len(parts[2]) || subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return claims{}, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return claims{}, ErrInvalidToken
	}
	var parsed claims
	if json.Unmarshal(payload, &parsed) != nil || parsed.Version != 1 || parsed.GrantID == "" || parsed.NodeID != service.nodeID || parsed.Nonce == "" {
		return claims{}, ErrInvalidToken
	}
	return parsed, nil
}

func (service *Service) signature(payload string) string {
	mac := hmac.New(sha256.New, service.rootSecret)
	_, _ = mac.Write([]byte("nexdrop/enrollment/v1\n"))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func randomID(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate enrollment secret: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}
