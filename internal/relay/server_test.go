package relay

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

func relayFixture(t *testing.T, capacity int64) (*Server, *GrantSigner) {
	t.Helper()
	signer, err := NewGrantSigner([]byte("relay-test-signing-seed-32-bytes-minimum"))
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewGrantVerifier(signer.PublicKey())
	if err != nil {
		t.Fatal(err)
	}
	server, err := NewServer(ServerConfig{
		RelayID: "relay-test", StorageRoot: t.TempDir(), CapacityBytes: capacity,
		Retention: time.Hour, Verifier: verifier,
	})
	if err != nil {
		t.Fatal(err)
	}
	return server, signer
}

func issueGrant(t *testing.T, signer *GrantSigner, operation Operation, maximum int64) string {
	t.Helper()
	grant, err := signer.Issue("relay-test", "transfer-test", "file-test", operation, maximum, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return grant
}

func TestRelayStoresAndServesOnlyVerifiedCiphertext(t *testing.T) {
	server, signer := relayFixture(t, 1024)
	ciphertext := []byte("opaque encrypted chunk")
	digest := sha256.Sum256(ciphertext)
	path := "/relay/v1/transfers/transfer-test/files/file-test/chunks/0"

	upload := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(ciphertext))
	upload.Header.Set("Authorization", "Bearer "+issueGrant(t, signer, OperationUpload, int64(len(ciphertext))))
	upload.Header.Set("X-Chunk-SHA256", hex.EncodeToString(digest[:]))
	uploadResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(uploadResponse, upload)
	if uploadResponse.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d %s", uploadResponse.Code, uploadResponse.Body.String())
	}

	download := httptest.NewRequest(http.MethodGet, path, nil)
	download.Header.Set("Authorization", "Bearer "+issueGrant(t, signer, OperationDownload, int64(len(ciphertext))))
	downloadResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(downloadResponse, download)
	if downloadResponse.Code != http.StatusOK {
		t.Fatalf("download failed: %d %s", downloadResponse.Code, downloadResponse.Body.String())
	}
	body, _ := io.ReadAll(downloadResponse.Result().Body)
	if !bytes.Equal(body, ciphertext) {
		t.Fatalf("ciphertext changed: %q", body)
	}
	if downloadResponse.Header().Get("X-Chunk-SHA256") != hex.EncodeToString(digest[:]) {
		t.Fatal("missing verified chunk digest")
	}
}

func TestRelayRejectsWrongHashAndGrantBinding(t *testing.T) {
	server, signer := relayFixture(t, 1024)
	path := "/relay/v1/transfers/transfer-test/files/file-test/chunks/0"
	request := httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte("ciphertext")))
	request.Header.Set("Authorization", "Bearer "+issueGrant(t, signer, OperationUpload, 10))
	request.Header.Set("X-Chunk-SHA256", string(bytes.Repeat([]byte{'0'}, 64)))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected hash rejection, got %d", response.Code)
	}

	wrongRelayGrant, err := signer.Issue("other-relay", "transfer-test", "file-test", OperationUpload, 10, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodPut, path, bytes.NewReader([]byte("ciphertext")))
	request.Header.Set("Authorization", "Bearer "+wrongRelayGrant)
	digest := sha256.Sum256([]byte("ciphertext"))
	request.Header.Set("X-Chunk-SHA256", hex.EncodeToString(digest[:]))
	response = httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected relay binding rejection, got %d", response.Code)
	}
}

func TestRelayEnforcesQuotaAndScopedDelete(t *testing.T) {
	server, signer := relayFixture(t, 4)
	ciphertext := []byte("12345")
	digest := sha256.Sum256(ciphertext)
	path := "/relay/v1/transfers/transfer-test/files/file-test/chunks/0"
	request := httptest.NewRequest(http.MethodPut, path, bytes.NewReader(ciphertext))
	request.Header.Set("Authorization", "Bearer "+issueGrant(t, signer, OperationUpload, int64(len(ciphertext))))
	request.Header.Set("X-Chunk-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusInsufficientStorage {
		t.Fatalf("expected quota rejection, got %d", response.Code)
	}

	deleteRequest := httptest.NewRequest(http.MethodDelete, "/relay/v1/transfers/transfer-test/files/file-test", nil)
	deleteRequest.Header.Set("Authorization", "Bearer "+issueGrant(t, signer, OperationDownload, 0))
	deleteResponse := httptest.NewRecorder()
	server.Handler().ServeHTTP(deleteResponse, deleteRequest)
	if deleteResponse.Code != http.StatusUnauthorized {
		t.Fatalf("download grant must not delete: %d", deleteResponse.Code)
	}
}

func TestRelayCleanupRemovesExpiredChunks(t *testing.T) {
	server, signer := relayFixture(t, 1024)
	server.retention = time.Second
	base := time.Now().UTC().Truncate(time.Second)
	server.now = func() time.Time { return base }
	server.verifier.now = func() time.Time { return base }
	signer.now = func() time.Time { return base }
	ciphertext := []byte("ciphertext")
	digest := sha256.Sum256(ciphertext)
	requestPath := "/relay/v1/transfers/transfer-test/files/file-test/chunks/0"
	request := httptest.NewRequest(http.MethodPut, requestPath, bytes.NewReader(ciphertext))
	request.Header.Set("Authorization", "Bearer "+issueGrant(t, signer, OperationUpload, int64(len(ciphertext))))
	request.Header.Set("X-Chunk-SHA256", hex.EncodeToString(digest[:]))
	response := httptest.NewRecorder()
	server.Handler().ServeHTTP(response, request)
	if response.Code != http.StatusCreated {
		t.Fatalf("upload failed: %d", response.Code)
	}
	storedPath := server.chunkPath("transfer-test", "file-test", 0)
	if err := os.Chtimes(storedPath, base, base); err != nil {
		t.Fatal(err)
	}

	server.now = func() time.Time { return base.Add(2 * time.Second) }
	removed, err := server.CleanupExpired()
	if err != nil || removed != 1 {
		t.Fatalf("unexpected cleanup: removed=%d err=%v", removed, err)
	}
}
