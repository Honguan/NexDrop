package group

import (
	"context"
	"errors"
	"strings"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

var (
	ErrInvalid   = errors.New("invalid group request")
	ErrNotFound  = errors.New("group not found")
	ErrForbidden = errors.New("group operation forbidden")
	ErrConflict  = errors.New("group membership conflict")
)

type Role string

const (
	RoleOwner  Role = "OWNER"
	RoleAdmin  Role = "ADMIN"
	RoleMember Role = "MEMBER"
)

type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	OwnerID   string    `json:"ownerUserId"`
	Role      Role      `json:"role"`
	CreatedAt time.Time `json:"createdAt"`
}

type Member struct {
	UserID   string    `json:"userId"`
	Username string    `json:"username"`
	Role     Role      `json:"role"`
	JoinedAt time.Time `json:"joinedAt"`
}

type GroupDevice struct {
	ID          string      `json:"id"`
	OwnerUserID string      `json:"ownerUserId"`
	DisplayName string      `json:"displayName"`
	Type        device.Type `json:"type"`
	PublicKey   []byte      `json:"publicKey"`
	Algorithm   string      `json:"keyAlgorithm"`
	AddedAt     time.Time   `json:"addedAt"`
}

type Details struct {
	Group
	Members []Member      `json:"members"`
	Devices []GroupDevice `json:"devices"`
}

type Store interface {
	CreateGroup(context.Context, auth.Session, string) (Details, error)
	ListGroups(context.Context, auth.Session) ([]Group, error)
	GetGroup(context.Context, auth.Session, string) (Details, error)
	RenameGroup(context.Context, auth.Session, string, string) (Group, error)
	DeleteGroup(context.Context, auth.Session, string) error
	AddGroupMember(context.Context, auth.Session, string, string, Role) (Member, error)
	RemoveGroupMember(context.Context, auth.Session, string, string) error
	AddGroupDevice(context.Context, auth.Session, string, string, time.Time) (GroupDevice, error)
	RemoveGroupDevice(context.Context, auth.Session, string, string) error
}

type Service struct {
	store Store
	now   func() time.Time
}

func NewService(store Store) *Service {
	return &Service{store: store, now: time.Now}
}

func (service *Service) Create(ctx context.Context, session auth.Session, name string) (Details, error) {
	name = strings.TrimSpace(name)
	if !validName(name) {
		return Details{}, ErrInvalid
	}
	return service.store.CreateGroup(ctx, session, name)
}

func (service *Service) List(ctx context.Context, session auth.Session) ([]Group, error) {
	return service.store.ListGroups(ctx, session)
}

func (service *Service) Get(ctx context.Context, session auth.Session, id string) (Details, error) {
	if id == "" {
		return Details{}, ErrInvalid
	}
	return service.store.GetGroup(ctx, session, id)
}

func (service *Service) Rename(ctx context.Context, session auth.Session, id, name string) (Group, error) {
	name = strings.TrimSpace(name)
	if id == "" || !validName(name) {
		return Group{}, ErrInvalid
	}
	return service.store.RenameGroup(ctx, session, id, name)
}

func (service *Service) Delete(ctx context.Context, session auth.Session, id string) error {
	if id == "" {
		return ErrInvalid
	}
	return service.store.DeleteGroup(ctx, session, id)
}

func (service *Service) AddMember(ctx context.Context, session auth.Session, groupID, userID string, role Role) (Member, error) {
	if groupID == "" || userID == "" || (role != RoleMember && role != RoleAdmin) {
		return Member{}, ErrInvalid
	}
	return service.store.AddGroupMember(ctx, session, groupID, userID, role)
}

func (service *Service) RemoveMember(ctx context.Context, session auth.Session, groupID, userID string) error {
	if groupID == "" || userID == "" {
		return ErrInvalid
	}
	return service.store.RemoveGroupMember(ctx, session, groupID, userID)
}

func (service *Service) AddDevice(ctx context.Context, session auth.Session, groupID, deviceID string) (GroupDevice, error) {
	if groupID == "" || deviceID == "" {
		return GroupDevice{}, ErrInvalid
	}
	return service.store.AddGroupDevice(ctx, session, groupID, deviceID, service.now().UTC())
}

func (service *Service) RemoveDevice(ctx context.Context, session auth.Session, groupID, deviceID string) error {
	if groupID == "" || deviceID == "" {
		return ErrInvalid
	}
	return service.store.RemoveGroupDevice(ctx, session, groupID, deviceID)
}

func validName(name string) bool {
	return name != "" && len(name) <= 100
}
