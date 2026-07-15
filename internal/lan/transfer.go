package lan

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"time"
)

const defaultMaxChunkSize int64 = 9 * 1024 * 1024

type ChunkStore interface {
	CompletedChunks(context.Context, string, string) ([]int, error)
	PutChunk(context.Context, string, string, int, []byte, [sha256.Size]byte) error
	CompleteFile(context.Context, string, string, int, [sha256.Size]byte) error
}

type TransferServer struct {
	identity     Identity
	trust        TrustDirectory
	challenge    func() string
	store        ChunkStore
	maxChunkSize int64
	server       *http.Server
}

func NewTransferServer(identity Identity, trust TrustDirectory, challenge func() string, store ChunkStore) (*TransferServer, error) {
	if identity.Certificate.PrivateKey == nil || trust == nil || challenge == nil || store == nil {
		return nil, errors.New("incomplete LAN transfer server configuration")
	}
	value := &TransferServer{identity: identity, trust: trust, challenge: challenge, store: store, maxChunkSize: defaultMaxChunkSize}
	value.server = &http.Server{Handler: value.routes(), TLSConfig: serverTLSConfig(identity, trust), ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 30 * time.Second}
	return value, nil
}

func (server *TransferServer) Serve(listener net.Listener) error {
	if listener == nil {
		return errors.New("LAN listener is required")
	}
	return server.server.ServeTLS(listener, "", "")
}

func (server *TransferServer) Close() error {
	return server.server.Close()
}

func (server *TransferServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/transfers/{transfer}/files/{file}", server.status)
	mux.HandleFunc("PUT /v1/transfers/{transfer}/files/{file}/chunks/{index}", server.putChunk)
	mux.HandleFunc("POST /v1/transfers/{transfer}/files/{file}/complete", server.complete)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-NexDrop-Protocol") != ProtocolVersion {
			writeLANError(w, http.StatusUpgradeRequired, "PROTOCOL_VERSION_UNSUPPORTED")
			return
		}
		if r.Header.Get("X-NexDrop-Challenge") == "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-NexDrop-Challenge")), []byte(server.challenge())) != 1 {
			writeLANError(w, http.StatusUnauthorized, "LAN_HANDSHAKE_FAILED")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (server *TransferServer) status(w http.ResponseWriter, r *http.Request) {
	transferID, fileID, ok := pathIDs(r)
	if !ok {
		writeLANError(w, http.StatusBadRequest, "INVALID_TRANSFER")
		return
	}
	completed, err := server.store.CompletedChunks(r.Context(), transferID, fileID)
	if err != nil {
		writeLANError(w, http.StatusInternalServerError, "LAN_STORAGE_FAILED")
		return
	}
	writeLANJSON(w, http.StatusOK, map[string]any{"completedChunks": completed, "protocolVersion": ProtocolVersion})
}

func (server *TransferServer) putChunk(w http.ResponseWriter, r *http.Request) {
	transferID, fileID, ok := pathIDs(r)
	index, indexErr := strconv.Atoi(r.PathValue("index"))
	expected, hashErr := decodeHash(r.Header.Get("X-Chunk-SHA256"))
	if !ok || indexErr != nil || index < 0 || hashErr != nil {
		writeLANError(w, http.StatusBadRequest, "INVALID_CHUNK")
		return
	}
	data, err := io.ReadAll(io.LimitReader(r.Body, server.maxChunkSize+1))
	if err != nil || int64(len(data)) > server.maxChunkSize {
		writeLANError(w, http.StatusRequestEntityTooLarge, "CHUNK_TOO_LARGE")
		return
	}
	actual := sha256.Sum256(data)
	if subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
		writeLANError(w, http.StatusUnprocessableEntity, "HASH_MISMATCH")
		return
	}
	if err := server.store.PutChunk(r.Context(), transferID, fileID, index, data, expected); err != nil {
		writeLANError(w, http.StatusInternalServerError, "LAN_STORAGE_FAILED")
		return
	}
	writeLANJSON(w, http.StatusOK, map[string]any{"index": index, "sha256": hex.EncodeToString(expected[:])})
}

func (server *TransferServer) complete(w http.ResponseWriter, r *http.Request) {
	transferID, fileID, ok := pathIDs(r)
	var request struct {
		ChunkCount int    `json:"chunkCount"`
		SHA256     string `json:"sha256"`
	}
	if !ok || json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&request) != nil || request.ChunkCount < 0 {
		writeLANError(w, http.StatusBadRequest, "INVALID_FILE")
		return
	}
	expected, hashErr := decodeHash(request.SHA256)
	if hashErr != nil {
		writeLANError(w, http.StatusBadRequest, "INVALID_FILE")
		return
	}
	if err := server.store.CompleteFile(r.Context(), transferID, fileID, request.ChunkCount, expected); err != nil {
		writeLANError(w, http.StatusConflict, "FILE_INCOMPLETE")
		return
	}
	writeLANJSON(w, http.StatusOK, map[string]string{"status": "DELIVERED"})
}

type TransferClient struct {
	identity Identity
	trust    TrustDirectory
}

func NewTransferClient(identity Identity, trust TrustDirectory) (*TransferClient, error) {
	if identity.Certificate.PrivateKey == nil || trust == nil {
		return nil, errors.New("incomplete LAN transfer client configuration")
	}
	return &TransferClient{identity: identity, trust: trust}, nil
}

func (client *TransferClient) CompletedChunks(ctx context.Context, target Advertisement, transferID, fileID string) ([]int, error) {
	if !pathToken(transferID) || !pathToken(fileID) {
		return nil, errors.New("invalid LAN transfer identifier")
	}
	var response struct {
		Completed []int `json:"completedChunks"`
	}
	if err := client.request(ctx, target, http.MethodGet, filePath(transferID, fileID), nil, nil, &response); err != nil {
		return nil, err
	}
	return response.Completed, nil
}

func (client *TransferClient) PutChunk(ctx context.Context, target Advertisement, transferID, fileID string, index int, data []byte) error {
	if !pathToken(transferID) || !pathToken(fileID) || index < 0 {
		return errors.New("invalid LAN chunk identifier")
	}
	digest := sha256.Sum256(data)
	headers := http.Header{"X-Chunk-SHA256": []string{hex.EncodeToString(digest[:])}, "Content-Type": []string{"application/octet-stream"}}
	return client.request(ctx, target, http.MethodPut, filePath(transferID, fileID)+"/chunks/"+strconv.Itoa(index), bytes.NewReader(data), headers, nil)
}

func (client *TransferClient) Complete(ctx context.Context, target Advertisement, transferID, fileID string, chunkCount int, digest [sha256.Size]byte) error {
	if !pathToken(transferID) || !pathToken(fileID) || chunkCount < 0 {
		return errors.New("invalid LAN file identifier")
	}
	body, _ := json.Marshal(map[string]any{"chunkCount": chunkCount, "sha256": hex.EncodeToString(digest[:])})
	return client.request(ctx, target, http.MethodPost, filePath(transferID, fileID)+"/complete", bytes.NewReader(body), http.Header{"Content-Type": []string{"application/json"}}, nil)
}

func (client *TransferClient) request(ctx context.Context, target Advertisement, method, path string, body io.Reader, headers http.Header, output any) error {
	if target.Validate() != nil || net.ParseIP(target.Address) == nil {
		return errors.New("invalid LAN target")
	}
	transport := &http.Transport{TLSClientConfig: clientTLSConfig(client.identity, client.trust, target.ShortDeviceID)}
	defer transport.CloseIdleConnections()
	httpClient := &http.Client{Transport: transport, Timeout: 30 * time.Second}
	request, err := http.NewRequestWithContext(ctx, method, "https://"+net.JoinHostPort(target.Address, strconv.Itoa(target.Port))+path, body)
	if err != nil {
		return err
	}
	request.Header = headers.Clone()
	if request.Header == nil {
		request.Header = make(http.Header)
	}
	request.Header.Set("X-NexDrop-Protocol", ProtocolVersion)
	request.Header.Set("X-NexDrop-Challenge", target.Challenge)
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("LAN request failed: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var result struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result)
		return fmt.Errorf("LAN request failed: %s", result.Error)
	}
	if output != nil {
		return json.NewDecoder(io.LimitReader(response.Body, 1024*1024)).Decode(output)
	}
	return nil
}

func pathIDs(r *http.Request) (string, string, bool) {
	transferID, fileID := r.PathValue("transfer"), r.PathValue("file")
	return transferID, fileID, pathToken(transferID) && pathToken(fileID)
}

func pathToken(value string) bool {
	return len(value) >= 6 && len(value) <= 64 && identifier(value)
}

func filePath(transferID, fileID string) string {
	return "/v1/transfers/" + transferID + "/files/" + fileID
}

func decodeHash(value string) ([sha256.Size]byte, error) {
	var result [sha256.Size]byte
	decoded, err := hex.DecodeString(value)
	if err != nil || len(decoded) != sha256.Size {
		return result, errors.New("invalid SHA-256")
	}
	copy(result[:], decoded)
	return result, nil
}

func writeLANJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeLANError(w http.ResponseWriter, status int, code string) {
	writeLANJSON(w, status, map[string]string{"error": code})
}
