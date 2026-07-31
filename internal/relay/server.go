package relay

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrRelayQuotaExceeded = errors.New("relay storage quota exceeded")
	ErrChunkHashMismatch  = errors.New("relay chunk hash mismatch")
)

type ServerConfig struct {
	RelayID       string
	StorageRoot   string
	CapacityBytes int64
	Retention     time.Duration
	Verifier      *GrantVerifier
}

type Server struct {
	id        string
	root      string
	capacity  int64
	retention time.Duration
	verifier  *GrantVerifier
	now       func() time.Time
	mu        sync.Mutex
	used      int64
}

type Health struct {
	Status        string    `json:"status"`
	RelayID       string    `json:"relayId"`
	CapacityBytes int64     `json:"capacityBytes"`
	UsedBytes     int64     `json:"usedBytes"`
	Available     int64     `json:"availableBytes"`
	CheckedAt     time.Time `json:"checkedAt"`
}

func NewServer(config ServerConfig) (*Server, error) {
	config.RelayID = strings.TrimSpace(config.RelayID)
	config.StorageRoot = filepath.Clean(strings.TrimSpace(config.StorageRoot))
	if !safeIdentifier(config.RelayID) || config.StorageRoot == "." || config.CapacityBytes <= 0 || config.Verifier == nil {
		return nil, ErrInvalidGrant
	}
	if config.Retention <= 0 {
		config.Retention = 7 * 24 * time.Hour
	}
	if err := os.MkdirAll(config.StorageRoot, 0o700); err != nil {
		return nil, err
	}
	used, err := storedBytes(config.StorageRoot)
	if err != nil {
		return nil, err
	}
	if used > config.CapacityBytes {
		return nil, ErrRelayQuotaExceeded
	}
	return &Server{
		id: config.RelayID, root: config.StorageRoot, capacity: config.CapacityBytes,
		retention: config.Retention, verifier: config.Verifier, now: time.Now, used: used,
	}, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", server.health)
	mux.HandleFunc("PUT /relay/v1/transfers/{transferId}/files/{fileId}/chunks/{chunk}", server.putChunk)
	mux.HandleFunc("GET /relay/v1/transfers/{transferId}/files/{fileId}/chunks/{chunk}", server.getChunk)
	mux.HandleFunc("DELETE /relay/v1/transfers/{transferId}/files/{fileId}", server.deleteFile)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		mux.ServeHTTP(w, r)
	})
}

func (server *Server) StartCleanup(ctxDone <-chan struct{}, interval time.Duration) {
	if interval <= 0 {
		interval = time.Hour
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctxDone:
			return
		case <-ticker.C:
			_, _ = server.CleanupExpired()
		}
	}
}

func (server *Server) CleanupExpired() (int, error) {
	server.mu.Lock()
	defer server.mu.Unlock()
	cutoff := server.now().UTC().Add(-server.retention)
	removed := 0
	var reclaimed int64
	err := filepath.WalkDir(server.root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || strings.HasSuffix(path, ".tmp") {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(cutoff) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			removed++
			reclaimed += info.Size()
		}
		return nil
	})
	server.used -= reclaimed
	if server.used < 0 {
		server.used = 0
	}
	removeEmptyDirectories(server.root)
	return removed, err
}

func (server *Server) health(w http.ResponseWriter, _ *http.Request) {
	server.mu.Lock()
	used := server.used
	server.mu.Unlock()
	writeRelayJSON(w, http.StatusOK, Health{
		Status: "ok", RelayID: server.id, CapacityBytes: server.capacity,
		UsedBytes: used, Available: max(server.capacity-used, 0), CheckedAt: server.now().UTC(),
	})
}

func (server *Server) putChunk(w http.ResponseWriter, r *http.Request) {
	transferID, fileID, chunkIndex, ok := requestCoordinates(w, r)
	if !ok {
		return
	}
	if r.ContentLength < 0 {
		writeRelayError(w, http.StatusLengthRequired, "CONTENT_LENGTH_REQUIRED")
		return
	}
	claims, err := server.authorize(r, transferID, fileID, OperationUpload, r.ContentLength)
	if err != nil {
		writeRelayGrantError(w, err)
		return
	}
	if claims.MaxBytes == 0 && r.ContentLength != 0 {
		writeRelayError(w, http.StatusRequestEntityTooLarge, "GRANT_SIZE_EXCEEDED")
		return
	}
	expectedHash := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Chunk-SHA256")))
	if len(expectedHash) != sha256.Size*2 {
		writeRelayError(w, http.StatusBadRequest, "CHUNK_HASH_REQUIRED")
		return
	}
	if _, err := hex.DecodeString(expectedHash); err != nil {
		writeRelayError(w, http.StatusBadRequest, "CHUNK_HASH_INVALID")
		return
	}
	path := server.chunkPath(transferID, fileID, chunkIndex)
	if existingHash, existingSize, err := hashFile(path); err == nil {
		if existingHash == expectedHash && existingSize == r.ContentLength {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		writeRelayError(w, http.StatusConflict, "CHUNK_ALREADY_EXISTS")
		return
	}

	server.mu.Lock()
	if server.used+r.ContentLength > server.capacity {
		server.mu.Unlock()
		writeRelayError(w, http.StatusInsufficientStorage, "RELAY_QUOTA_EXCEEDED")
		return
	}
	server.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".chunk-*.tmp")
	if err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	temporaryPath := temporary.Name()
	committed := false
	defer func() {
		_ = temporary.Close()
		if !committed {
			_ = os.Remove(temporaryPath)
		}
	}()

	hash := sha256.New()
	written, copyErr := io.Copy(io.MultiWriter(temporary, hash), io.LimitReader(r.Body, claims.MaxBytes+1))
	if copyErr != nil {
		writeRelayError(w, http.StatusBadRequest, "CHUNK_READ_FAILED")
		return
	}
	if written != r.ContentLength || written > claims.MaxBytes {
		writeRelayError(w, http.StatusRequestEntityTooLarge, "GRANT_SIZE_EXCEEDED")
		return
	}
	actualHash := hex.EncodeToString(hash.Sum(nil))
	if actualHash != expectedHash {
		writeRelayError(w, http.StatusUnprocessableEntity, "CHUNK_HASH_MISMATCH")
		return
	}
	if err := temporary.Sync(); err != nil || temporary.Close() != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	committed = true
	server.mu.Lock()
	server.used += written
	server.mu.Unlock()
	w.Header().Set("ETag", `"sha256:`+actualHash+`"`)
	w.WriteHeader(http.StatusCreated)
}

func (server *Server) getChunk(w http.ResponseWriter, r *http.Request) {
	transferID, fileID, chunkIndex, ok := requestCoordinates(w, r)
	if !ok {
		return
	}
	path := server.chunkPath(transferID, fileID, chunkIndex)
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		writeRelayError(w, http.StatusNotFound, "CHUNK_NOT_FOUND")
		return
	}
	if err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	if _, err := server.authorize(r, transferID, fileID, OperationDownload, info.Size()); err != nil {
		writeRelayGrantError(w, err)
		return
	}
	hash, _, err := hashFile(path)
	if err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.FormatInt(info.Size(), 10))
	w.Header().Set("X-Chunk-SHA256", hash)
	w.Header().Set("ETag", `"sha256:`+hash+`"`)
	http.ServeContent(w, r, filepath.Base(path), info.ModTime(), file)
}

func (server *Server) deleteFile(w http.ResponseWriter, r *http.Request) {
	transferID := strings.TrimSpace(r.PathValue("transferId"))
	fileID := strings.TrimSpace(r.PathValue("fileId"))
	if !safeIdentifier(transferID) || !safeIdentifier(fileID) {
		writeRelayError(w, http.StatusBadRequest, "INVALID_PATH")
		return
	}
	if _, err := server.authorize(r, transferID, fileID, OperationDelete, 0); err != nil {
		writeRelayGrantError(w, err)
		return
	}
	path := filepath.Join(server.root, transferID, fileID)
	server.mu.Lock()
	bytes, _ := storedBytes(path)
	err := os.RemoveAll(path)
	if err == nil {
		server.used -= bytes
		if server.used < 0 {
			server.used = 0
		}
	}
	server.mu.Unlock()
	if err != nil {
		writeRelayError(w, http.StatusInternalServerError, "STORAGE_UNAVAILABLE")
		return
	}
	removeEmptyDirectories(filepath.Join(server.root, transferID))
	w.WriteHeader(http.StatusNoContent)
}

func (server *Server) authorize(r *http.Request, transferID, fileID string, operation Operation, requestedBytes int64) (GrantClaims, error) {
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if !strings.HasPrefix(authorization, "Bearer ") {
		return GrantClaims{}, ErrInvalidGrant
	}
	return server.verifier.Verify(strings.TrimSpace(strings.TrimPrefix(authorization, "Bearer ")), server.id, transferID, fileID, operation, requestedBytes)
}

func (server *Server) chunkPath(transferID, fileID string, chunkIndex int) string {
	return filepath.Join(server.root, transferID, fileID, strconv.Itoa(chunkIndex)+".chunk")
}

func requestCoordinates(w http.ResponseWriter, r *http.Request) (string, string, int, bool) {
	transferID := strings.TrimSpace(r.PathValue("transferId"))
	fileID := strings.TrimSpace(r.PathValue("fileId"))
	chunkIndex, err := strconv.Atoi(r.PathValue("chunk"))
	if !safeIdentifier(transferID) || !safeIdentifier(fileID) || err != nil || chunkIndex < 0 || chunkIndex > 10_000_000 {
		writeRelayError(w, http.StatusBadRequest, "INVALID_PATH")
		return "", "", 0, false
	}
	return transferID, fileID, chunkIndex, true
}

func safeIdentifier(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if (char >= 'a' && char <= 'z') || (char >= 'A' && char <= 'Z') || (char >= '0' && char <= '9') || char == '-' || char == '_' {
			continue
		}
		return false
	}
	return true
}

func storedBytes(root string) (int64, error) {
	var total int64
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			if errors.Is(walkErr, os.ErrNotExist) {
				return nil
			}
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		total += info.Size()
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	return total, err
}

func hashFile(path string) (string, int64, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer file.Close()
	hash := sha256.New()
	bytes, err := io.Copy(hash, file)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(hash.Sum(nil)), bytes, nil
}

func removeEmptyDirectories(root string) {
	_ = filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil || !entry.IsDir() || path == root {
			return nil
		}
		entries, readErr := os.ReadDir(path)
		if readErr == nil && len(entries) == 0 {
			_ = os.Remove(path)
		}
		return nil
	})
}

func writeRelayGrantError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrExpiredGrant):
		writeRelayError(w, http.StatusGone, "GRANT_EXPIRED")
	case errors.Is(err, ErrGrantReplay):
		writeRelayError(w, http.StatusConflict, "GRANT_REPLAYED")
	default:
		writeRelayError(w, http.StatusUnauthorized, "GRANT_INVALID")
	}
}

func writeRelayError(w http.ResponseWriter, status int, code string) {
	writeRelayJSON(w, status, map[string]any{"error": map[string]string{"code": code}})
}

func writeRelayJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func (server *Server) String() string {
	return fmt.Sprintf("relay %s (%d/%d bytes)", server.id, server.used, server.capacity)
}
