package group

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexdrop/internal/auth"
)

type fakeStore struct{}

func (*fakeStore) CreateGroup(_ context.Context, session auth.Session, name string) (Details, error) {
	return Details{Group: Group{ID: "group-1", Name: name, OwnerID: session.ID, Role: RoleOwner}}, nil
}
func (*fakeStore) ListGroups(context.Context, auth.Session) ([]Group, error) {
	return []Group{{ID: "group-1"}}, nil
}
func (*fakeStore) GetGroup(context.Context, auth.Session, string) (Details, error) {
	return Details{Group: Group{ID: "group-1"}}, nil
}
func (*fakeStore) RenameGroup(_ context.Context, _ auth.Session, id, name string) (Group, error) {
	return Group{ID: id, Name: name}, nil
}
func (*fakeStore) DeleteGroup(context.Context, auth.Session, string) error { return nil }
func (*fakeStore) AddGroupMember(_ context.Context, _ auth.Session, _, userID string, role Role) (Member, error) {
	return Member{UserID: userID, Role: role}, nil
}
func (*fakeStore) RemoveGroupMember(context.Context, auth.Session, string, string) error {
	return nil
}
func (*fakeStore) AddGroupDevice(_ context.Context, _ auth.Session, _, deviceID string, now time.Time) (GroupDevice, error) {
	return GroupDevice{ID: deviceID, AddedAt: now}, nil
}
func (*fakeStore) RemoveGroupDevice(context.Context, auth.Session, string, string) error {
	return nil
}

func TestCreateTrimsAndValidatesName(t *testing.T) {
	service := NewService(&fakeStore{})
	created, err := service.Create(context.Background(), auth.Session{User: auth.User{ID: "user-1"}}, " Team ")
	if err != nil || created.Name != "Team" {
		t.Fatalf("Create() = %+v, %v", created, err)
	}
	for _, name := range []string{"", "   ", string(make([]byte, 101))} {
		if _, err := service.Create(context.Background(), auth.Session{}, name); !errors.Is(err, ErrInvalid) {
			t.Fatalf("Create(%q) error = %v, want ErrInvalid", name, err)
		}
	}
}

func TestMemberRoleValidation(t *testing.T) {
	service := NewService(&fakeStore{})
	if _, err := service.AddMember(context.Background(), auth.Session{}, "group-1", "user-2", RoleOwner); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AddMember() error = %v, want ErrInvalid", err)
	}
	member, err := service.AddMember(context.Background(), auth.Session{}, "group-1", "user-2", RoleAdmin)
	if err != nil || member.Role != RoleAdmin {
		t.Fatalf("AddMember() = %+v, %v", member, err)
	}
}
