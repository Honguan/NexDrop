package filetransfer

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"nexdrop/internal/auth"
)

var (
	ErrInvalid    = errors.New("invalid file operation")
	ErrNotFound   = errors.New("file not found")
	ErrForbidden  = errors.New("file operation forbidden")
	ErrConflict   = errors.New("file operation conflict")
	ErrHash       = errors.New("file hash mismatch")
	ErrIncomplete = errors.New("file chunks incomplete")
)

type FileRecord struct {
	ID          string `json:"id"`
	Size        int64  `json:"size"`
	SHA256      []byte `json:"sha256"`
	ChunkSize   int    `json:"chunkSize"`
	ChunkCount  int    `json:"chunkCount"`
	Status      string `json:"status"`
	StoragePath string `json:"-"`
}

type ChunkRecord struct {
	FileID      string    `json:"fileId"`
	Index       int       `json:"index"`
	Size        int       `json:"size"`
	SHA256      []byte    `json:"sha256"`
	StoragePath string    `json:"-"`
	CompletedAt time.Time `json:"completedAt"`
}

type Store interface {
	PrepareChunkUpload(context.Context, auth.Session, string, int) (FileRecord, *ChunkRecord, error)
	RecordChunk(context.Context, auth.Session, ChunkRecord) error
	OpenChunk(context.Context, auth.Session, string, int) (ChunkRecord, error)
	PrepareFileCompletion(context.Context, auth.Session, string) (FileRecord, []ChunkRecord, error)
	CompleteFile(context.Context, auth.Session, string, string, time.Time) error
}

type Service struct {
	store Store
	root  string
	now   func() time.Time
}

func NewService(store Store, root string) (*Service, error) {
	if root == "" {
		return nil, ErrInvalid
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve storage root: %w", err)
	}
	return &Service{store: store, root: absolute, now: time.Now}, nil
}

func (service *Service) UploadChunk(ctx context.Context, session auth.Session, fileID string, index int, expectedSHA256 []byte, reader io.Reader) (ChunkRecord, error) {
	if fileID == "" || index < 0 || len(expectedSHA256) != sha256.Size || reader == nil {
		return ChunkRecord{}, ErrInvalid
	}
	file, existing, err := service.store.PrepareChunkUpload(ctx, session, fileID, index)
	if err != nil {
		return ChunkRecord{}, err
	}
	if existing != nil {
		if string(existing.SHA256) == string(expectedSHA256) {
			return *existing, nil
		}
		return ChunkRecord{}, ErrConflict
	}
	expectedSize, err := expectedChunkSize(file, index)
	if err != nil {
		return ChunkRecord{}, err
	}
	directory := filepath.Join(service.root, "chunks", fileID)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return ChunkRecord{}, fmt.Errorf("create chunk directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".upload-*")
	if err != nil {
		return ChunkRecord{}, fmt.Errorf("create temporary chunk: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		_ = temporary.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	hash := sha256.New()
	written, err := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(reader, expectedSize+1))
	if err != nil {
		return ChunkRecord{}, fmt.Errorf("write chunk: %w", err)
	}
	if written != expectedSize {
		return ChunkRecord{}, ErrInvalid
	}
	digest := hash.Sum(nil)
	if string(digest) != string(expectedSHA256) {
		return ChunkRecord{}, ErrHash
	}
	if err := temporary.Sync(); err != nil {
		return ChunkRecord{}, fmt.Errorf("sync chunk: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return ChunkRecord{}, fmt.Errorf("close chunk: %w", err)
	}
	finalPath := filepath.Join(directory, strconv.Itoa(index)+"-"+hex.EncodeToString(digest)+".chunk")
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return ChunkRecord{}, fmt.Errorf("publish chunk: %w", err)
	}
	keepTemporary = true
	record := ChunkRecord{
		FileID: fileID, Index: index, Size: int(written), SHA256: digest,
		StoragePath: finalPath, CompletedAt: service.now().UTC(),
	}
	if err := service.store.RecordChunk(ctx, session, record); err != nil {
		_ = os.Remove(finalPath)
		return ChunkRecord{}, err
	}
	return record, nil
}

func (service *Service) OpenChunk(ctx context.Context, session auth.Session, fileID string, index int) (ChunkRecord, *os.File, error) {
	if fileID == "" || index < 0 {
		return ChunkRecord{}, nil, ErrInvalid
	}
	record, err := service.store.OpenChunk(ctx, session, fileID, index)
	if err != nil {
		return ChunkRecord{}, nil, err
	}
	file, err := os.Open(record.StoragePath)
	if errors.Is(err, os.ErrNotExist) {
		return ChunkRecord{}, nil, ErrNotFound
	}
	if err != nil {
		return ChunkRecord{}, nil, fmt.Errorf("open chunk: %w", err)
	}
	return record, file, nil
}

func (service *Service) Complete(ctx context.Context, session auth.Session, fileID string) (FileRecord, error) {
	if fileID == "" {
		return FileRecord{}, ErrInvalid
	}
	file, chunks, err := service.store.PrepareFileCompletion(ctx, session, fileID)
	if err != nil {
		return FileRecord{}, err
	}
	if len(chunks) != file.ChunkCount {
		return FileRecord{}, ErrIncomplete
	}
	directory := filepath.Join(service.root, "files")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return FileRecord{}, fmt.Errorf("create file directory: %w", err)
	}
	temporary, err := os.CreateTemp(directory, ".assemble-*")
	if err != nil {
		return FileRecord{}, fmt.Errorf("create assembled file: %w", err)
	}
	temporaryPath := temporary.Name()
	keepTemporary := false
	defer func() {
		_ = temporary.Close()
		if !keepTemporary {
			_ = os.Remove(temporaryPath)
		}
	}()
	wholeHash := sha256.New()
	for index, chunk := range chunks {
		if chunk.Index != index {
			return FileRecord{}, ErrIncomplete
		}
		chunkFile, err := os.Open(chunk.StoragePath)
		if err != nil {
			return FileRecord{}, ErrIncomplete
		}
		chunkHash := sha256.New()
		written, copyErr := io.Copy(io.MultiWriter(temporary, wholeHash, chunkHash), chunkFile)
		closeErr := chunkFile.Close()
		if copyErr != nil || closeErr != nil || written != int64(chunk.Size) || string(chunkHash.Sum(nil)) != string(chunk.SHA256) {
			return FileRecord{}, ErrHash
		}
	}
	if string(wholeHash.Sum(nil)) != string(file.SHA256) {
		return FileRecord{}, ErrHash
	}
	if err := temporary.Sync(); err != nil {
		return FileRecord{}, fmt.Errorf("sync assembled file: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return FileRecord{}, fmt.Errorf("close assembled file: %w", err)
	}
	finalPath := filepath.Join(directory, fileID)
	if err := os.Rename(temporaryPath, finalPath); err != nil {
		return FileRecord{}, fmt.Errorf("publish assembled file: %w", err)
	}
	keepTemporary = true
	if err := service.store.CompleteFile(ctx, session, fileID, finalPath, service.now().UTC()); err != nil {
		_ = os.Remove(finalPath)
		return FileRecord{}, err
	}
	file.Status = "AVAILABLE_ON_NODE"
	file.StoragePath = finalPath
	return file, nil
}

func expectedChunkSize(file FileRecord, index int) (int64, error) {
	if index >= file.ChunkCount || file.ChunkSize <= 0 || file.Size < 0 {
		return 0, ErrInvalid
	}
	if index < file.ChunkCount-1 {
		return int64(file.ChunkSize), nil
	}
	return file.Size - int64(index)*int64(file.ChunkSize), nil
}
