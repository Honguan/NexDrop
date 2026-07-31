package relay

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math"
	"sort"
	"strings"
	"time"
)

var (
	ErrNoRelay      = errors.New("no eligible relay")
	ErrInvalidGrant = errors.New("invalid relay grant")
	ErrExpiredGrant = errors.New("expired relay grant")
)

type Relay struct {
	ID             string
	Endpoint       string
	Region         string
	Healthy        bool
	Draining       bool
	CapacityBytes  int64
	UsedBytes      int64
	Latency        time.Duration
	FailureRate    float64
	LastSuccessful time.Time
}

func Select(relays []Relay, requiredBytes int64, preferredRegion string) (Relay, error) {
	eligible := make([]Relay, 0, len(relays))
	for _, candidate := range relays {
		if candidate.ID == "" || candidate.Endpoint == "" || !candidate.Healthy || candidate.Draining || requiredBytes < 0 || candidate.CapacityBytes-candidate.UsedBytes < requiredBytes {
			continue
		}
		eligible = append(eligible, candidate)
	}
	if len(eligible) == 0 {
		return Relay{}, ErrNoRelay
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		iScore := relayScore(eligible[i], preferredRegion)
		jScore := relayScore(eligible[j], preferredRegion)
		if iScore != jScore {
			return iScore > jScore
		}
		return eligible[i].ID < eligible[j].ID
	})
	return eligible[0], nil
}

func relayScore(candidate Relay, preferredRegion string) float64 {
	availableRatio := float64(candidate.CapacityBytes-candidate.UsedBytes) / float64(max(candidate.CapacityBytes, 1))
	score := 100*availableRatio - 60*clamp(candidate.FailureRate, 0, 1)
	if preferredRegion != "" && candidate.Region == preferredRegion {
		score += 20
	}
	if candidate.Latency > 0 {
		score -= math.Min(30, float64(candidate.Latency.Milliseconds())/10)
	}
	return score
}

type Operation string

const (
	OperationUpload   Operation = "UPLOAD"
	OperationDownload Operation = "DOWNLOAD"
	OperationDelete   Operation = "DELETE"
)

type GrantClaims struct {
	Version    int       `json:"v"`
	RelayID    string    `json:"relayId"`
	TransferID string    `json:"transferId"`
	FileID     string    `json:"fileId"`
	Operation  Operation `json:"operation"`
	MaxBytes   int64     `json:"maxBytes"`
	ExpiresAt  int64     `json:"expiresAt"`
	Nonce      string    `json:"nonce"`
}

type GrantSigner struct {
	secret []byte
	now    func() time.Time
}

func NewGrantSigner(secret []byte) (*GrantSigner, error) {
	if len(secret) < 32 {
		return nil, ErrInvalidGrant
	}
	return &GrantSigner{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

func (signer *GrantSigner) Issue(relayID, transferID, fileID string, operation Operation, maxBytes int64, ttl time.Duration) (string, error) {
	if strings.TrimSpace(relayID) == "" || strings.TrimSpace(transferID) == "" || strings.TrimSpace(fileID) == "" || !validOperation(operation) || maxBytes < 0 || ttl <= 0 || ttl > time.Hour {
		return "", ErrInvalidGrant
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	claims := GrantClaims{Version: 1, RelayID: relayID, TransferID: transferID, FileID: fileID, Operation: operation, MaxBytes: maxBytes, ExpiresAt: signer.now().UTC().Add(ttl).Unix(), Nonce: base64.RawURLEncoding.EncodeToString(nonceBytes)}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return "v1." + encoded + "." + signer.signature(encoded), nil
}

func (signer *GrantSigner) Verify(token, relayID, transferID, fileID string, operation Operation, requestedBytes int64) (GrantClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v1" {
		return GrantClaims{}, ErrInvalidGrant
	}
	expected := signer.signature(parts[1])
	if len(expected) != len(parts[2]) || subtle.ConstantTimeCompare([]byte(expected), []byte(parts[2])) != 1 {
		return GrantClaims{}, ErrInvalidGrant
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return GrantClaims{}, ErrInvalidGrant
	}
	var claims GrantClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Version != 1 || claims.Nonce == "" || !validOperation(claims.Operation) {
		return GrantClaims{}, ErrInvalidGrant
	}
	if signer.now().UTC().After(time.Unix(claims.ExpiresAt, 0)) {
		return GrantClaims{}, ErrExpiredGrant
	}
	if claims.RelayID != relayID || claims.TransferID != transferID || claims.FileID != fileID || claims.Operation != operation || requestedBytes < 0 || requestedBytes > claims.MaxBytes {
		return GrantClaims{}, ErrInvalidGrant
	}
	return claims, nil
}

func (signer *GrantSigner) signature(payload string) string {
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte("nexdrop/relay-grant/v1\n"))
	_, _ = mac.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validOperation(operation Operation) bool {
	return operation == OperationUpload || operation == OperationDownload || operation == OperationDelete
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

func max(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
