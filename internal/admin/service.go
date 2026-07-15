package admin

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	"nexdrop/internal/auth"
)

var (
	ErrInvalid   = errors.New("invalid admin request")
	ErrForbidden = errors.New("admin operation forbidden")
	ErrNotFound  = errors.New("admin resource not found")
	ErrConflict  = errors.New("admin resource conflict")
)

type User struct {
	ID         string     `json:"id"`
	Username   string     `json:"username"`
	Email      string     `json:"email"`
	Admin      bool       `json:"admin"`
	DisabledAt *time.Time `json:"disabledAt,omitempty"`
	CreatedAt  time.Time  `json:"createdAt"`
}

type NodeSettings struct {
	SingleFileLimitBytes   int64 `json:"singleFileLimitBytes"`
	DefaultUserQuotaBytes  int64 `json:"defaultUserQuotaBytes"`
	DefaultGroupQuotaBytes int64 `json:"defaultGroupQuotaBytes"`
	NodeCacheLimitBytes    int64 `json:"nodeCacheLimitBytes"`
	DefaultUserDailyBytes  int64 `json:"defaultUserDailyBytes"`
	DefaultGroupDailyBytes int64 `json:"defaultGroupDailyBytes"`
	DiskWarningPercent     int   `json:"diskWarningPercent"`
	DiskStopPercent        int   `json:"diskStopPercent"`
}

type Quota struct {
	OwnerType          string    `json:"ownerType"`
	OwnerID            string    `json:"ownerId"`
	ByteLimit          int64     `json:"byteLimit"`
	BytesUsed          int64     `json:"bytesUsed"`
	DailyTransferLimit int64     `json:"dailyTransferLimit"`
	DailyTransferUsed  int64     `json:"dailyTransferUsed"`
	UpdatedAt          time.Time `json:"updatedAt"`
}

type StorageOverview struct {
	FileCount      int64 `json:"fileCount"`
	StoredBytes    int64 `json:"storedBytes"`
	UploadingBytes int64 `json:"uploadingBytes"`
	ExpiredBytes   int64 `json:"expiredBytes"`
	QuotaBytesUsed int64 `json:"quotaBytesUsed"`
	QuotaByteLimit int64 `json:"quotaByteLimit"`
}

type Failure struct {
	TransferID     string    `json:"transferId"`
	TargetDeviceID string    `json:"targetDeviceId"`
	ErrorCode      string    `json:"errorCode"`
	CreatedAt      time.Time `json:"createdAt"`
}

type AuditLog struct {
	ID            string         `json:"id"`
	ActorUserID   *string        `json:"actorUserId,omitempty"`
	ActorDeviceID *string        `json:"actorDeviceId,omitempty"`
	Action        string         `json:"action"`
	TargetType    string         `json:"targetType"`
	TargetID      *string        `json:"targetId,omitempty"`
	Metadata      map[string]any `json:"metadata"`
	CreatedAt     time.Time      `json:"createdAt"`
}

type Store interface {
	BootstrapAdmin(context.Context, string, string, string) error
	ListAdminUsers(context.Context, int, int) ([]User, error)
	CreateAdminUser(context.Context, auth.Session, string, string, string, bool) (User, error)
	DisableAdminUser(context.Context, auth.Session, string, time.Time) error
	ResetAdminPassword(context.Context, auth.Session, string, string, time.Time) error
	ResetAdminPasswordByIdentifier(context.Context, string, string, time.Time) error
	AdminNodeSettings(context.Context) (NodeSettings, error)
	UpdateAdminNodeSettings(context.Context, auth.Session, NodeSettings) (NodeSettings, error)
	SetAdminQuota(context.Context, auth.Session, Quota) (Quota, error)
	AdminStorageOverview(context.Context, time.Time) (StorageOverview, error)
	ListAdminFailures(context.Context, int, int) ([]Failure, error)
	ListAdminAuditLogs(context.Context, int, int) ([]AuditLog, error)
	DeleteAdminGroupContent(context.Context, auth.Session, string, time.Time) ([]string, error)
}

type Service struct{ store Store }

func NewService(store Store) *Service { return &Service{store: store} }

func (service *Service) Bootstrap(ctx context.Context, username, email, password string) error {
	if username == "" && email == "" && password == "" {
		return nil
	}
	if !validIdentity(username, email, password) {
		return ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return service.store.BootstrapAdmin(ctx, strings.TrimSpace(username), strings.TrimSpace(email), string(hash))
}

func (service *Service) Users(ctx context.Context, session auth.Session, limit, offset int) ([]User, error) {
	if !session.Admin {
		return nil, ErrForbidden
	}
	if !validPage(limit, offset) {
		return nil, ErrInvalid
	}
	return service.store.ListAdminUsers(ctx, limit, offset)
}

func (service *Service) CreateUser(ctx context.Context, session auth.Session, username, email, password string, isAdmin bool) (User, error) {
	if !session.Admin {
		return User{}, ErrForbidden
	}
	if !validIdentity(username, email, password) {
		return User{}, ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return User{}, err
	}
	return service.store.CreateAdminUser(ctx, session, strings.TrimSpace(username), strings.TrimSpace(email), string(hash), isAdmin)
}

func (service *Service) DisableUser(ctx context.Context, session auth.Session, userID string) error {
	if !session.Admin {
		return ErrForbidden
	}
	if !isUUID(userID) || userID == session.ID {
		return ErrInvalid
	}
	return service.store.DisableAdminUser(ctx, session, userID, time.Now().UTC())
}

func (service *Service) ResetPassword(ctx context.Context, session auth.Session, userID, password string) error {
	if !session.Admin {
		return ErrForbidden
	}
	if !isUUID(userID) || len(password) < 12 {
		return ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return service.store.ResetAdminPassword(ctx, session, userID, string(hash), time.Now().UTC())
}

func (service *Service) ResetPasswordByIdentifier(ctx context.Context, identifier, password string) error {
	identifier = strings.TrimSpace(identifier)
	if identifier == "" || len(password) < 12 {
		return ErrInvalid
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	return service.store.ResetAdminPasswordByIdentifier(ctx, identifier, string(hash), time.Now().UTC())
}

func (service *Service) Settings(ctx context.Context, session auth.Session) (NodeSettings, error) {
	if !session.Admin {
		return NodeSettings{}, ErrForbidden
	}
	return service.store.AdminNodeSettings(ctx)
}

func (service *Service) UpdateSettings(ctx context.Context, session auth.Session, settings NodeSettings) (NodeSettings, error) {
	if !session.Admin {
		return NodeSettings{}, ErrForbidden
	}
	if !validSettings(settings) {
		return NodeSettings{}, ErrInvalid
	}
	return service.store.UpdateAdminNodeSettings(ctx, session, settings)
}

func (service *Service) SetQuota(ctx context.Context, session auth.Session, quota Quota) (Quota, error) {
	if !session.Admin {
		return Quota{}, ErrForbidden
	}
	quota.OwnerType = strings.ToUpper(strings.TrimSpace(quota.OwnerType))
	if (quota.OwnerType != "USER" && quota.OwnerType != "GROUP") || !isUUID(quota.OwnerID) || quota.ByteLimit < 0 || quota.DailyTransferLimit < 0 {
		return Quota{}, ErrInvalid
	}
	return service.store.SetAdminQuota(ctx, session, quota)
}

func (service *Service) Storage(ctx context.Context, session auth.Session) (StorageOverview, error) {
	if !session.Admin {
		return StorageOverview{}, ErrForbidden
	}
	return service.store.AdminStorageOverview(ctx, time.Now().UTC())
}

func (service *Service) Failures(ctx context.Context, session auth.Session, limit, offset int) ([]Failure, error) {
	if !session.Admin {
		return nil, ErrForbidden
	}
	if !validPage(limit, offset) {
		return nil, ErrInvalid
	}
	return service.store.ListAdminFailures(ctx, limit, offset)
}

func (service *Service) AuditLogs(ctx context.Context, session auth.Session, limit, offset int) ([]AuditLog, error) {
	if !session.Admin {
		return nil, ErrForbidden
	}
	if !validPage(limit, offset) {
		return nil, ErrInvalid
	}
	return service.store.ListAdminAuditLogs(ctx, limit, offset)
}

func (service *Service) DeleteGroupContent(ctx context.Context, session auth.Session, transferID string) error {
	if !session.Admin {
		return ErrForbidden
	}
	if !isUUID(transferID) {
		return ErrInvalid
	}
	paths, err := service.store.DeleteAdminGroupContent(ctx, session, transferID, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, path := range paths {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	return nil
}

func validIdentity(username, email, password string) bool {
	username, email = strings.TrimSpace(username), strings.TrimSpace(email)
	return len(username) >= 3 && len(username) <= 64 && len(email) >= 3 && len(email) <= 254 && strings.Contains(email, "@") && len(password) >= 12
}

func validSettings(value NodeSettings) bool {
	return value.SingleFileLimitBytes > 0 && value.DefaultUserQuotaBytes > 0 && value.DefaultGroupQuotaBytes > 0 && value.NodeCacheLimitBytes > 0 && value.DefaultUserDailyBytes > 0 && value.DefaultGroupDailyBytes > 0 && value.DiskWarningPercent > 0 && value.DiskWarningPercent < value.DiskStopPercent && value.DiskStopPercent <= 100
}

func validPage(limit, offset int) bool { return limit > 0 && limit <= 200 && offset >= 0 }

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
