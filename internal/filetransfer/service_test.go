package filetransfer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexdrop/internal/auth"
)

type serviceStore struct {
	file   FileRecord
	chunks map[int]ChunkRecord
}

func (store *serviceStore) PrepareChunkUpload(_ context.Context, _ auth.Session, _ string, index int) (FileRecord, *ChunkRecord, error) {
	if chunk, ok := store.chunks[index]; ok {
		return store.file, &chunk, nil
	}
	return store.file, nil, nil
}
func (store *serviceStore) RecordChunk(_ context.Context, _ auth.Session, chunk ChunkRecord) error {
	store.chunks[chunk.Index] = chunk
	return nil
}
func (store *serviceStore) OpenChunk(_ context.Context, _ auth.Session, _ string, index int) (ChunkRecord, error) {
	return store.chunks[index], nil
}
func (store *serviceStore) PrepareFileCompletion(context.Context, auth.Session, string) (FileRecord, []ChunkRecord, error) {
	chunks := make([]ChunkRecord, 0, len(store.chunks))
	for index := 0; index < store.file.ChunkCount; index++ {
		if chunk, ok := store.chunks[index]; ok {
			chunks = append(chunks, chunk)
		}
	}
	return store.file, chunks, nil
}
func (store *serviceStore) CompleteFile(_ context.Context, _ auth.Session, _ string, path string, _ time.Time) error {
	store.file.StoragePath = path
	store.file.Status = "AVAILABLE_ON_NODE"
	return nil
}

func TestUploadDownloadAndComplete(t *testing.T) {
	content := []byte("abcdefghij")
	wholeHash := sha256.Sum256(content)
	store := &serviceStore{file: FileRecord{ID: "file-1", Size: int64(len(content)), SHA256: wholeHash[:], ChunkSize: 4, ChunkCount: 3}, chunks: make(map[int]ChunkRecord)}
	service, err := NewService(store, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for index, data := range [][]byte{[]byte("abcd"), []byte("efgh"), []byte("ij")} {
		digest := sha256.Sum256(data)
		if _, err := service.UploadChunk(context.Background(), auth.Session{}, "file-1", index, digest[:], bytes.NewReader(data)); err != nil {
			t.Fatal(err)
		}
	}
	record, reader, err := service.OpenChunk(context.Background(), auth.Session{}, "file-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	got, err := io.ReadAll(reader)
	_ = reader.Close()
	if err != nil || record.Size != 4 || !bytes.Equal(got, []byte("efgh")) {
		t.Fatalf("download = %q, %+v, %v", got, record, err)
	}
	completed, err := service.Complete(context.Background(), auth.Session{}, "file-1")
	if err != nil {
		t.Fatal(err)
	}
	assembled, err := os.ReadFile(completed.StoragePath)
	if err != nil || !bytes.Equal(assembled, content) {
		t.Fatalf("assembled = %q, %v", assembled, err)
	}
	replayed, err := service.Complete(context.Background(), auth.Session{}, "file-1")
	if err != nil || replayed.StoragePath != completed.StoragePath {
		t.Fatalf("replayed completion = %+v, %v", replayed, err)
	}
}

func TestUploadRejectsWrongHashAndSize(t *testing.T) {
	store := &serviceStore{file: FileRecord{ID: "file-1", Size: 4, SHA256: make([]byte, 32), ChunkSize: 4, ChunkCount: 1}, chunks: make(map[int]ChunkRecord)}
	root := t.TempDir()
	service, _ := NewService(store, root)
	wrongHash := sha256.Sum256([]byte("wrong"))
	if _, err := service.UploadChunk(context.Background(), auth.Session{}, "file-1", 0, wrongHash[:], bytes.NewReader([]byte("data"))); !errors.Is(err, ErrHash) {
		t.Fatalf("hash error = %v", err)
	}
	dataHash := sha256.Sum256([]byte("abc"))
	if _, err := service.UploadChunk(context.Background(), auth.Session{}, "file-1", 0, dataHash[:], bytes.NewReader([]byte("abc"))); !errors.Is(err, ErrInvalid) {
		t.Fatalf("size error = %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(root, "chunks", "file-1"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("temporary chunks remain: %v, %v", entries, err)
	}
}
