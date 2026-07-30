package operations

import (
	"context"
	"errors"
	"os"
	"testing"
)

type fakeDatabase struct{ err error }

func (database fakeDatabase) Ping(context.Context) error { return database.err }

func TestDoctorReportsDatabaseAndStorage(t *testing.T) {
	checks := Doctor(context.Background(), fakeDatabase{}, t.TempDir())
	if len(checks) != 4 || !checks[0].OK || !checks[1].OK {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestHealthyRejectsFailedCheck(t *testing.T) {
	checks := []Check{{Name: "database", OK: true}, {Name: "storage", Detail: errors.New("failed").Error()}}
	if Healthy(checks) {
		t.Fatalf("Healthy(%+v) = true", checks)
	}
}

func TestInspectDoesNotCreateStoragePath(t *testing.T) {
	path := t.TempDir() + "/missing"
	checks := Inspect(context.Background(), fakeDatabase{}, path)
	if len(checks) != 4 || checks[1].OK {
		t.Fatalf("checks = %+v", checks)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Inspect created or changed storage path: %v", err)
	}
}

func TestInspectUnavailablePreservesDatabaseFailure(t *testing.T) {
	checks := InspectUnavailable(t.TempDir(), errors.New("database offline"))
	if len(checks) != 4 || checks[0].OK || checks[0].Detail != "database offline" {
		t.Fatalf("checks = %+v", checks)
	}
}
