package operations

import (
	"context"
	"errors"
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
