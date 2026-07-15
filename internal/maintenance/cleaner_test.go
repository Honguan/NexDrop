package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type fakeStore struct {
	files  []ExpiredFile
	marked []string
}

func (store *fakeStore) ExpiredFiles(context.Context, time.Time, int) ([]ExpiredFile, error) {
	return store.files, nil
}
func (store *fakeStore) MarkFileExpired(_ context.Context, id string, _ time.Time) error {
	store.marked = append(store.marked, id)
	return nil
}

func TestCleanerDeletesBodiesAndMarksMetadata(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "files", "file-1")
	chunkPath := filepath.Join(root, "chunks", "file-1", "0.chunk")
	if err := os.MkdirAll(filepath.Dir(filePath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(chunkPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filePath, []byte("file"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(chunkPath, []byte("chunk"), 0o600); err != nil {
		t.Fatal(err)
	}
	store := &fakeStore{files: []ExpiredFile{{ID: "file-1", StoragePath: filePath, ChunkPaths: []string{chunkPath}}}}
	cleaner, err := NewCleaner(store, root)
	if err != nil {
		t.Fatal(err)
	}
	count, err := cleaner.RunOnce(context.Background(), 100)
	if err != nil || count != 1 || len(store.marked) != 1 {
		t.Fatalf("RunOnce() = %d, %v, marked=%v", count, err, store.marked)
	}
	if _, err := os.Stat(filePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("file still exists: %v", err)
	}
	if _, err := os.Stat(chunkPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("chunk still exists: %v", err)
	}
}

func TestCleanerRejectsPathOutsideRoot(t *testing.T) {
	root := t.TempDir()
	outside := filepath.Join(filepath.Dir(root), "outside-file")
	store := &fakeStore{files: []ExpiredFile{{ID: "file-1", StoragePath: outside}}}
	cleaner, _ := NewCleaner(store, root)
	_, err := cleaner.RunOnce(context.Background(), 1)
	if !errors.Is(err, ErrUnsafePath) || len(store.marked) != 0 {
		t.Fatalf("RunOnce() error = %v, marked=%v", err, store.marked)
	}
}
