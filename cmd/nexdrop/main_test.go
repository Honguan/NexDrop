package main

import (
	"archive/zip"
	"context"
	"path/filepath"
	"testing"
)

func TestDiagnosticsWorksWithoutDatabaseConfiguration(t *testing.T) {
	t.Setenv("NEXDROP_DATABASE_URL", "")
	t.Setenv("NEXDROP_DATABASE_PASSWORD", "must-not-appear")
	t.Setenv("NEXDROP_STORAGE_PATH", t.TempDir())
	output := filepath.Join(t.TempDir(), "diagnostics.zip")

	handled, err := runMaintenanceCommand(context.Background(), []string{"diagnostics", "--output", output})

	if err != nil || !handled {
		t.Fatalf("runMaintenanceCommand() = %t, %v", handled, err)
	}
	archive, err := zip.OpenReader(output)
	if err != nil {
		t.Fatal(err)
	}
	defer archive.Close()
	if len(archive.File) < 3 {
		t.Fatalf("diagnostics entries = %d", len(archive.File))
	}
}
