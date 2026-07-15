package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type User struct {
	ID       string `json:"id"`
	Username string `json:"username"`
	Email    string `json:"email"`
	Admin    bool   `json:"admin"`
}

type Credential struct {
	User
	PasswordHash string
}

type Session struct {
	User
	SessionID string
}

type Store interface {
	CredentialByIdentifier(context.Context, string) (Credential, error)
	CreateSession(context.Context, string, []byte, time.Time, []byte, time.Time) (string, error)
	SessionByAccessToken(context.Context, []byte, time.Time) (Session, error)
	RotateSession(context.Context, []byte, []byte, time.Time, []byte, time.Time, time.Time) error
	RevokeSessionByRefreshToken(context.Context, []byte, time.Time) error
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
	credential, err := service.store.CredentialByIdentifier(ctx, identifier)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(credential.PasswordHash), []byte(password)) != nil {
		return TokenPair{}, ErrInvalidCredentials
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
		return Session{}, ErrInvalidCredentials
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
