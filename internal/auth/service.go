package auth

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrTOTPRequired       = errors.New("totp required")
	ErrForbidden          = errors.New("operation forbidden")
)

type User struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	Admin       bool   `json:"admin"`
	TOTPEnabled bool   `json:"totpEnabled"`
}

type Credential struct {
	User
	PasswordHash string
	TOTPSecret   string
}

type Session struct {
	User
	SessionID     string
	DeviceID      *string
	AdminVerified bool
}

type Store interface {
	CredentialByIdentifier(context.Context, string) (Credential, error)
	CreateSession(context.Context, string, []byte, time.Time, []byte, time.Time) (string, error)
	SessionByAccessToken(context.Context, []byte, time.Time) (Session, error)
	RotateSession(context.Context, []byte, []byte, time.Time, []byte, time.Time, time.Time) error
	RevokeSessionByRefreshToken(context.Context, []byte, time.Time) error
	SetTOTPSecret(context.Context, string, string) error
	MarkAdminVerified(context.Context, string, time.Time) error
}

type TokenPair struct {
	AccessToken      string    `json:"accessToken"`
	AccessExpiresAt  time.Time `json:"accessExpiresAt"`
	RefreshToken     string    `json:"refreshToken"`
	RefreshExpiresAt time.Time `json:"refreshExpiresAt"`
}

type Service struct {
	store      Store
	now        func() time.Time
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func NewService(store Store, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{store: store, now: time.Now, accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func (service *Service) Login(ctx context.Context, identifier, password string) (TokenPair, error) {
	return service.LoginWithTOTP(ctx, identifier, password, "")
}

func (service *Service) LoginWithTOTP(ctx context.Context, identifier, password, code string) (TokenPair, error) {
	credential, err := service.store.CredentialByIdentifier(ctx, identifier)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) != nil {
		return TokenPair{}, ErrInvalidCredentials
	}
	if credential.TOTPSecret != "" && !validTOTP(credential.TOTPSecret, code, service.now().UTC()) {
		return TokenPair{}, ErrTOTPRequired
	}

	pair, err := service.issueTokenPair()
	if err != nil {
		return TokenPair{}, err
	}
	_, err = service.store.CreateSession(
		ctx,
		credential.ID,
		hashToken(pair.AccessToken),
		pair.AccessExpiresAt,
		hashToken(pair.RefreshToken),
		pair.RefreshExpiresAt,
	)
	if err != nil {
		return TokenPair{}, fmt.Errorf("create session: %w", err)
	}
	return pair, nil
}

type TOTPSetup struct {
	Secret string `json:"secret"`
	URI    string `json:"uri"`
}

func (service *Service) SetupTOTP(ctx context.Context, session Session, password string) (TOTPSetup, error) {
	if !session.Admin || !service.validPassword(ctx, session.Username, password) {
		return TOTPSetup{}, ErrInvalidCredentials
	}
	value := make([]byte, 20)
	if _, err := rand.Read(value); err != nil {
		return TOTPSetup{}, err
	}
	secret := base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(value)
	label := url.PathEscape("NexDrop:" + session.Username)
	query := url.Values{"secret": {secret}, "issuer": {"NexDrop"}, "algorithm": {"SHA1"}, "digits": {"6"}, "period": {"30"}}
	return TOTPSetup{Secret: secret, URI: "otpauth://totp/" + label + "?" + query.Encode()}, nil
}

func (service *Service) EnableTOTP(ctx context.Context, session Session, password, secret, code string) error {
	if !session.Admin || !service.validPassword(ctx, session.Username, password) || !validTOTP(secret, code, service.now().UTC()) {
		return ErrInvalidCredentials
	}
	return service.store.SetTOTPSecret(ctx, session.ID, normalizeSecret(secret))
}

func (service *Service) VerifyAdmin(ctx context.Context, session Session, password, code string) error {
	if !session.Admin {
		return ErrForbidden
	}
	credential, err := service.store.CredentialByIdentifier(ctx, session.Username)
	if err != nil || credential.TOTPSecret == "" || bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) != nil || !validTOTP(credential.TOTPSecret, code, service.now().UTC()) {
		return ErrInvalidCredentials
	}
	return service.store.MarkAdminVerified(ctx, session.SessionID, service.now().UTC())
}

func (service *Service) validPassword(ctx context.Context, identifier, password string) bool {
	credential, err := service.store.CredentialByIdentifier(ctx, identifier)
	return err == nil && bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) == nil
}

func validTOTP(secret, code string, now time.Time) bool {
	if len(code) != 6 {
		return false
	}
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(normalizeSecret(secret))
	if err != nil || len(key) < 16 {
		return false
	}
	for offset := int64(-1); offset <= 1; offset++ {
		counter := uint64(now.Unix()/30 + offset)
		message := make([]byte, 8)
		binary.BigEndian.PutUint64(message, counter)
		digest := hmac.New(sha1.New, key)
		_, _ = digest.Write(message)
		sum := digest.Sum(nil)
		index := sum[len(sum)-1] & 0x0f
		value := (uint32(sum[index])&0x7f)<<24 | uint32(sum[index+1])<<16 | uint32(sum[index+2])<<8 | uint32(sum[index+3])
		if fmt.Sprintf("%06d", value%1000000) == code {
			return true
		}
	}
	return false
}

func normalizeSecret(value string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(value), " ", ""))
}

func (service *Service) Refresh(ctx context.Context, refreshToken string) (TokenPair, error) {
	if refreshToken == "" {
		return TokenPair{}, ErrInvalidCredentials
	}
	pair, err := service.issueTokenPair()
	if err != nil {
		return TokenPair{}, err
	}
	err = service.store.RotateSession(
		ctx,
		hashToken(refreshToken),
		hashToken(pair.AccessToken),
		pair.AccessExpiresAt,
		hashToken(pair.RefreshToken),
		pair.RefreshExpiresAt,
		service.now().UTC(),
	)
	if err != nil {
		return TokenPair{}, ErrInvalidCredentials
	}
	return pair, nil
}

func (service *Service) Authenticate(ctx context.Context, accessToken string) (Session, error) {
	if accessToken == "" {
		return Session{}, ErrInvalidCredentials
	}
	session, err := service.store.SessionByAccessToken(ctx, hashToken(accessToken), service.now().UTC())
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			return Session{}, ErrInvalidCredentials
		}
		return Session{}, fmt.Errorf("authenticate session: %w", err)
	}
	return session, nil
}

func (service *Service) Logout(ctx context.Context, refreshToken string) error {
	if refreshToken == "" {
		return ErrInvalidCredentials
	}
	if err := service.store.RevokeSessionByRefreshToken(ctx, hashToken(refreshToken), service.now().UTC()); err != nil {
		return ErrInvalidCredentials
	}
	return nil
}

func (service *Service) issueTokenPair() (TokenPair, error) {
	accessToken, err := newToken()
	if err != nil {
		return TokenPair{}, err
	}
	refreshToken, err := newToken()
	if err != nil {
		return TokenPair{}, err
	}
	now := service.now().UTC()
	return TokenPair{
		AccessToken:      accessToken,
		AccessExpiresAt:  now.Add(service.accessTTL),
		RefreshToken:     refreshToken,
		RefreshExpiresAt: now.Add(service.refreshTTL),
	}, nil
}

func newToken() (string, error) {
	value := make([]byte, 32)
	if _, err := rand.Read(value); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func hashToken(token string) []byte {
	digest := sha256.Sum256([]byte(token))
	return digest[:]
}
