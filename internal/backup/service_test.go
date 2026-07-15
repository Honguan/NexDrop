package backup

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeRunner struct {
	commands   []string
	restoreErr error
}

func (runner *fakeRunner) Run(_ context.Context, name string, arguments ...string) error {
	runner.commands = append(runner.commands, name)
	if name == "pg_dump" {
		for _, argument := range arguments {
			if strings.HasPrefix(argument, "--file=") {
				return os.WriteFile(strings.TrimPrefix(argument, "--file="), []byte("database"), 0o600)
			}
		}
	}
	if name == "pg_restore" {
		return runner.restoreErr
	}
	return nil
}

type fakeSecurityStore struct {
	revoked   []string
	protected []string
	closed    int
}

func (store *fakeSecurityStore) RevokedDeviceIDs(context.Context) ([]string, error) {
	return store.revoked, nil
}

func (store *fakeSecurityStore) ProtectRestoredSecurity(_ context.Context, ids []string, _ time.Time) error {
	store.protected = append([]string(nil), ids...)
	return nil
}

func (store *fakeSecurityStore) Close() { store.closed++ }

func TestCreateAndRestoreBackup(t *testing.T) {
	storage := t.TempDir()
	if err := os.WriteFile(filepath.Join(storage, "chunk"), []byte("content"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(storage, "backups"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(storage, "backups", "old.tar.gz"), []byte("old"), 0o600); err != nil {
		t.Fatal(err)
	}
	runner := &fakeRunner{}
	security := &fakeSecurityStore{revoked: []string{"11111111-1111-1111-1111-111111111111"}}
	service := NewService(func(context.Context, string) (SecurityStore, error) { return security, nil })
	service.runner = runner
	service.now = func() time.Time { return time.Date(2026, 7, 15, 0, 0, 0, 0, time.UTC) }
	archive := filepath.Join(t.TempDir(), "backup.tar.gz")
	if err := service.Create(context.Background(), "postgres://database", storage, archive, true); err != nil {
		t.Fatal(err)
	}
	extracted := t.TempDir()
	manifest, err := extractArchive(archive, extracted)
	if err != nil {
		t.Fatal(err)
	}
	if !manifest.IncludeFiles || manifest.FormatVersion != FormatVersion {
		t.Fatalf("manifest = %+v", manifest)
	}
	if content, err := os.ReadFile(filepath.Join(extracted, "files", "chunk")); err != nil || string(content) != "content" {
		t.Fatalf("content = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(extracted, "files", "backups", "old.tar.gz")); !os.IsNotExist(err) {
		t.Fatalf("backup directory was archived: %v", err)
	}

	restored := t.TempDir()
	if err := service.Restore(context.Background(), "postgres://database", restored, archive); err != nil {
		t.Fatal(err)
	}
	if content, err := os.ReadFile(filepath.Join(restored, "chunk")); err != nil || string(content) != "content" {
		t.Fatalf("restored content = %q, %v", content, err)
	}
	if len(security.protected) != 1 || security.protected[0] != security.revoked[0] {
		t.Fatalf("protected = %v", security.protected)
	}
	if len(runner.commands) != 2 || runner.commands[0] != "pg_dump" || runner.commands[1] != "pg_restore" {
		t.Fatalf("commands = %v", runner.commands)
	}
	security.protected = nil
	runner.restoreErr = errors.New("partial restore")
	if err := service.Restore(context.Background(), "postgres://database", restored, archive); err == nil {
		t.Fatal("Restore() error = nil")
	}
	if len(security.protected) != 1 {
		t.Fatalf("partial restore protected = %v", security.protected)
	}
}

func TestExtractArchiveRejectsTraversal(t *testing.T) {
	archive := filepath.Join(t.TempDir(), "invalid.tar.gz")
	file, err := os.Create(archive)
	if err != nil {
		t.Fatal(err)
	}
	gzipWriter := gzip.NewWriter(file)
	tarWriter := tar.NewWriter(gzipWriter)
	if err := writeBytes(tarWriter, "manifest.json", []byte(`{"formatVersion":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeBytes(tarWriter, "database.dump", []byte("database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeBytes(tarWriter, "../escape", []byte("escape"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := tarWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gzipWriter.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := extractArchive(archive, t.TempDir()); err != ErrInvalidArchive {
		t.Fatalf("extractArchive() error = %v", err)
	}
}
