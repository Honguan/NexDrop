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
	settings := NodeSettings{1, 1, 1, 1, 1, 1, 80, 95}
	if !validSettings(settings) {
		t.Fatal("validSettings() rejected valid settings")
	}
	settings.DiskStopPercent = 80
	if validSettings(settings) {
		t.Fatal("validSettings() accepted overlapping thresholds")
	}
}
