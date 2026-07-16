package admin

import (
	"context"
	"errors"
	"testing"

	"nexdrop/internal/auth"
)

func TestAdminOperationsRequireAdministrator(t *testing.T) {
	service := NewService(nil)
	session := auth.Session{}
	if _, err := service.Users(context.Background(), session, 50, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Users() error = %v, want ErrForbidden", err)
	}
	if _, err := service.InviteUser(context.Background(), session, "invitee", "invitee@example.com", false); !errors.Is(err, ErrForbidden) {
		t.Fatalf("InviteUser() error = %v, want ErrForbidden", err)
	}
	if _, err := service.Devices(context.Background(), session, 50, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Devices() error = %v, want ErrForbidden", err)
	}
	if _, err := service.Groups(context.Background(), session, 50, 0); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Groups() error = %v, want ErrForbidden", err)
	}
	if err := service.RevokeDevice(context.Background(), session, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("RevokeDevice() error = %v, want ErrForbidden", err)
	}
	if err := service.DeleteGroup(context.Background(), session, "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("DeleteGroup() error = %v, want ErrForbidden", err)
	}
	if _, err := service.Settings(context.Background(), session); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Settings() error = %v, want ErrForbidden", err)
	}
	if _, err := service.Storage(context.Background(), session); !errors.Is(err, ErrForbidden) {
		t.Fatalf("Storage() error = %v, want ErrForbidden", err)
	}
}

func TestAdminValidation(t *testing.T) {
	if validIdentity("ab", "admin@example.com", "a-long-password") {
		t.Fatal("validIdentity() accepted short username")
	}
	if !validIdentity("admin", "admin@example.com", "a-long-password") {
		t.Fatal("validIdentity() rejected valid identity")
	}
	settings := NodeSettings{
		SingleFileLimitBytes: 1, DefaultUserQuotaBytes: 1,
		DefaultGroupQuotaBytes: 1, NodeCacheLimitBytes: 1,
		DefaultUserDailyBytes: 1, DefaultGroupDailyBytes: 1,
		DiskWarningPercent: 80, DiskStopPercent: 95,
	}
	if !validSettings(settings) {
		t.Fatal("validSettings() rejected valid settings")
	}
	settings.DiskStopPercent = 80
	if validSettings(settings) {
		t.Fatal("validSettings() accepted overlapping thresholds")
	}
}

func TestCLIResetPasswordValidation(t *testing.T) {
	service := NewService(nil)
	if err := service.ResetPasswordByIdentifier(context.Background(), "", "a-valid-password"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("ResetPasswordByIdentifier() error = %v, want ErrInvalid", err)
	}
}

func TestAcceptInvitationValidation(t *testing.T) {
	service := NewService(nil)
	if _, err := service.AcceptInvitation(context.Background(), "short", "a-valid-password"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("AcceptInvitation() error = %v, want ErrInvalid", err)
	}
}
