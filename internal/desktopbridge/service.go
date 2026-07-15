package desktopbridge

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"nexdrop/internal/nativebridge"
)

const (
	maximumBodySize = nativebridge.MaximumMessageSize
	pairingTTL      = 5 * time.Minute
)

type Queue interface {
	Enqueue(context.Context, nativebridge.SharePayload) (string, error)
}

type StatusProvider interface {
	Status(context.Context) (json.RawMessage, error)
}

type Pairing struct {
	ID        string    `json:"id"`
	Code      string    `json:"code"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type pairingRecord struct {
	codeHash  [sha256.Size]byte
	expiresAt time.Time
	attempts  int
}

type Service struct {
	allowedOrigin string
	queue         Queue
	status        StatusProvider
	now           func() time.Time
	mu            sync.Mutex
	pairings      map[string]pairingRecord
	tokens        map[[sha256.Size]byte]struct{}
}

func New(allowedOrigin string, queue Queue, status StatusProvider) (*Service, error) {
	origin, err := normalizeOrigin(allowedOrigin)
	if err != nil || queue == nil || status == nil {
		return nil, errors.New("invalid desktop bridge configuration")
	}
	return &Service{allowedOrigin: origin, queue: queue, status: status, now: time.Now, pairings: make(map[string]pairingRecord), tokens: make(map[[sha256.Size]byte]struct{})}, nil
}

func (service *Service) BeginPairing() (Pairing, error) {
	id, err := randomToken(24)
	if err != nil {
		return Pairing{}, err
	}
	codeBytes := make([]byte, 6)
	if _, err := rand.Read(codeBytes); err != nil {
		return Pairing{}, err
	}
	code := fmt.Sprintf("%06d", uint64(codeBytes[0])<<40|uint64(codeBytes[1])<<32|uint64(codeBytes[2])<<24|uint64(codeBytes[3])<<16|uint64(codeBytes[4])<<8|uint64(codeBytes[5]))
	code = code[len(code)-6:]
	expiresAt := service.now().UTC().Add(pairingTTL)
	service.mu.Lock()
	service.pairings[id] = pairingRecord{codeHash: sha256.Sum256([]byte(code)), expiresAt: expiresAt}
	service.mu.Unlock()
	return Pairing{ID: id, Code: code, ExpiresAt: expiresAt}, nil
}

func (service *Service) IssueNativeToken() (string, error) {
	return service.issueToken()
}

func (service *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/pair", service.pair)
	mux.HandleFunc("POST /v1/status", service.authenticated(service.handleStatus))
	mux.HandleFunc("POST /v1/share", service.authenticated(service.share))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" {
			if origin != service.allowedOrigin {
				writeError(w, http.StatusForbidden, "ORIGIN_REJECTED")
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if origin == "" && r.Header.Get("X-NexDrop-Client") != "native-messaging" {
			writeError(w, http.StatusForbidden, "CLIENT_REJECTED")
			return
		}
		mux.ServeHTTP(w, r)
	})
}

func (service *Service) pair(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Origin") == "" {
		writeError(w, http.StatusForbidden, "ORIGIN_REQUIRED")
		return
	}
	var request struct {
		PairingID string `json:"pairingId"`
		Code      string `json:"code"`
	}
	if decode(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAIRING")
		return
	}
	service.mu.Lock()
	record, ok := service.pairings[request.PairingID]
	if !ok || !record.expiresAt.After(service.now().UTC()) || record.attempts >= 5 {
		delete(service.pairings, request.PairingID)
		service.mu.Unlock()
		writeError(w, http.StatusGone, "PAIRING_EXPIRED")
		return
	}
	provided := sha256.Sum256([]byte(request.Code))
	if subtle.ConstantTimeCompare(provided[:], record.codeHash[:]) != 1 {
		record.attempts++
		service.pairings[request.PairingID] = record
		service.mu.Unlock()
		writeError(w, http.StatusUnauthorized, "PAIRING_CODE_INVALID")
		return
	}
	delete(service.pairings, request.PairingID)
	service.mu.Unlock()
	token, err := service.issueToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "PAIRING_FAILED")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"token": token})
}

func (service *Service) authenticated(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if len(token) < 32 || !service.hasToken(token) {
			writeError(w, http.StatusUnauthorized, "BRIDGE_TOKEN_INVALID")
			return
		}
		next(w, r)
	}
}

func (service *Service) handleStatus(w http.ResponseWriter, r *http.Request) {
	var request nativebridge.Request
	if decode(r, &request) != nil || request.Type != "status" || nativebridge.ValidateRequest(request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	status, err := service.status.Status(r.Context())
	if err != nil || !json.Valid(status) {
		writeError(w, http.StatusServiceUnavailable, "STATUS_UNAVAILABLE")
		return
	}
	writeJSON(w, http.StatusOK, nativebridge.Response{ID: request.ID, OK: true, Status: status})
}

func (service *Service) share(w http.ResponseWriter, r *http.Request) {
	var request nativebridge.Request
	if decode(r, &request) != nil || request.Type != "share" || nativebridge.ValidateRequest(request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	var payload nativebridge.SharePayload
	if json.Unmarshal(request.Payload, &payload) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	queueID, err := service.queue.Enqueue(r.Context(), payload)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "QUEUE_UNAVAILABLE")
		return
	}
	status, _ := json.Marshal(map[string]string{"queueId": queueID})
	writeJSON(w, http.StatusOK, nativebridge.Response{ID: request.ID, OK: true, Status: status})
}

func (service *Service) issueToken() (string, error) {
	token, err := randomToken(32)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(token))
	service.mu.Lock()
	service.tokens[digest] = struct{}{}
	service.mu.Unlock()
	return token, nil
}

func (service *Service) hasToken(token string) bool {
	digest := sha256.Sum256([]byte(token))
	service.mu.Lock()
	defer service.mu.Unlock()
	_, ok := service.tokens[digest]
	return ok
}

func normalizeOrigin(value string) (string, error) {
	parsed, err := url.Parse(value)
	if err != nil || parsed.User != nil || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || (parsed.Scheme != "https" && !(parsed.Scheme == "http" && (parsed.Hostname() == "127.0.0.1" || parsed.Hostname() == "localhost"))) {
		return "", errors.New("invalid origin")
	}
	return parsed.Scheme + "://" + parsed.Host, nil
}

func randomToken(size int) (string, error) {
	value := make([]byte, size)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func decode(r *http.Request, value any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maximumBodySize+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(value); err != nil {
		return err
	}
	if decoder.Decode(&struct{}{}) != io.EOF {
		return errors.New("multiple JSON values")
	}
	return nil
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}
