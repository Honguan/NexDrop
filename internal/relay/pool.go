package relay

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

var (
	ErrNoRelay      = errors.New("no eligible relay")
	ErrInvalidGrant = errors.New("invalid relay grant")
	ErrExpiredGrant = errors.New("expired relay grant")
	ErrGrantReplay  = errors.New("relay grant replayed")
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
	availableRatio := float64(candidate.CapacityBytes-candidate.UsedBytes) / float64(maxInt64(candidate.CapacityBytes, 1))
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
	privateKey ed25519.PrivateKey
	publicKey  ed25519.PublicKey
	now        func() time.Time
}

type GrantVerifier struct {
	publicKey ed25519.PublicKey
	now       func() time.Time
}

// NewGrantSigner creates the control-plane signer from an independent seed.
// Production Nodes prefer NEXDROP_RELAY_SIGNING_SEED. If it is absent or still
// contains an example placeholder, a random seed is created once in the
// persistent storage volume. Relays receive only PublicKey().
func NewGrantSigner(fallbackSeed []byte) (*GrantSigner, error) {
	seed, err := relaySigningSeed(fallbackSeed)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(seed)
	privateKey := ed25519.NewKeyFromSeed(digest[:])
	publicKey := append(ed25519.PublicKey(nil), privateKey.Public().(ed25519.PublicKey)...)
	return &GrantSigner{privateKey: privateKey, publicKey: publicKey, now: time.Now}, nil
}

func relaySigningSeed(fallback []byte) ([]byte, error) {
	configured := strings.TrimSpace(os.Getenv("NEXDROP_RELAY_SIGNING_SEED"))
	if configured != "" && !relaySeedPlaceholder(configured) {
		if len(configured) < ed25519.SeedSize {
			return nil, fmt.Errorf("NEXDROP_RELAY_SIGNING_SEED must contain at least %d characters", ed25519.SeedSize)
		}
		return []byte(configured), nil
	}

	storageRoot := strings.TrimSpace(os.Getenv("NEXDROP_STORAGE_PATH"))
	if storageRoot != "" {
		path := filepath.Join(filepath.Clean(storageRoot), ".relay-signing-seed")
		if encoded, readErr := os.ReadFile(path); readErr == nil {
			seed, decodeErr := hex.DecodeString(strings.TrimSpace(string(encoded)))
			if decodeErr != nil || len(seed) != ed25519.SeedSize {
				return nil, errors.New("persisted relay signing seed is invalid")
			}
			return seed, nil
		} else if !errors.Is(readErr, os.ErrNotExist) {
			return nil, readErr
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return nil, err
		}
		seed := make([]byte, ed25519.SeedSize)
		if _, err := rand.Read(seed); err != nil {
			return nil, err
		}
		temporary, err := os.CreateTemp(filepath.Dir(path), ".relay-signing-seed-*")
		if err != nil {
			return nil, err
		}
		temporaryPath := temporary.Name()
		committed := false
		defer func() {
			_ = temporary.Close()
			if !committed {
				_ = os.Remove(temporaryPath)
			}
		}()
		if err := temporary.Chmod(0o600); err != nil {
			return nil, err
		}
		if _, err := temporary.WriteString(hex.EncodeToString(seed) + "\n"); err != nil {
			return nil, err
		}
		if err := temporary.Sync(); err != nil || temporary.Close() != nil {
			return nil, errors.New("persist relay signing seed")
		}
		if err := os.Rename(temporaryPath, path); err != nil {
			if encoded, readErr := os.ReadFile(path); readErr == nil {
				existing, decodeErr := hex.DecodeString(strings.TrimSpace(string(encoded)))
				if decodeErr == nil && len(existing) == ed25519.SeedSize {
					return existing, nil
				}
			}
			return nil, err
		}
		committed = true
		return seed, nil
	}

	if len(fallback) < ed25519.SeedSize {
		return nil, ErrInvalidGrant
	}
	return append([]byte(nil), fallback...), nil
}

func relaySeedPlaceholder(value string) bool {
	switch value {
	case "change-me", "replace-with-openssl-rand-hex-32", "replace-with-random-relay-signing-seed":
		return true
	default:
		return false
	}
}

func NewGrantVerifier(publicKey []byte) (*GrantVerifier, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, ErrInvalidGrant
	}
	return &GrantVerifier{publicKey: append(ed25519.PublicKey(nil), publicKey...), now: time.Now}, nil
}

func (signer *GrantSigner) PublicKey() []byte {
	return append([]byte(nil), signer.publicKey...)
}

func (signer *GrantSigner) PublicKeyBase64() string {
	return base64.RawURLEncoding.EncodeToString(signer.publicKey)
}

func (signer *GrantSigner) Issue(relayID, transferID, fileID string, operation Operation, maxBytes int64, ttl time.Duration) (string, error) {
	if strings.TrimSpace(relayID) == "" || strings.TrimSpace(transferID) == "" || strings.TrimSpace(fileID) == "" || !validOperation(operation) || maxBytes < 0 || ttl <= 0 || ttl > time.Hour {
		return "", ErrInvalidGrant
	}
	nonceBytes := make([]byte, 16)
	if _, err := rand.Read(nonceBytes); err != nil {
		return "", err
	}
	claims := GrantClaims{
		Version: 2, RelayID: relayID, TransferID: transferID, FileID: fileID,
		Operation: operation, MaxBytes: maxBytes,
		ExpiresAt: signer.now().UTC().Add(ttl).Unix(),
		Nonce:     base64.RawURLEncoding.EncodeToString(nonceBytes),
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	signature := ed25519.Sign(signer.privateKey, grantSigningMessage(encoded))
	return "v2." + encoded + "." + base64.RawURLEncoding.EncodeToString(signature), nil
}

func (signer *GrantSigner) Verify(token, relayID, transferID, fileID string, operation Operation, requestedBytes int64) (GrantClaims, error) {
	verifier := &GrantVerifier{publicKey: signer.publicKey, now: signer.now}
	return verifier.Verify(token, relayID, transferID, fileID, operation, requestedBytes)
}

func (verifier *GrantVerifier) Verify(token, relayID, transferID, fileID string, operation Operation, requestedBytes int64) (GrantClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 || parts[0] != "v2" || parts[1] == "" || parts[2] == "" {
		return GrantClaims{}, ErrInvalidGrant
	}
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(signature) != ed25519.SignatureSize || !ed25519.Verify(verifier.publicKey, grantSigningMessage(parts[1]), signature) {
		return GrantClaims{}, ErrInvalidGrant
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return GrantClaims{}, ErrInvalidGrant
	}
	var claims GrantClaims
	if json.Unmarshal(payload, &claims) != nil || claims.Version != 2 || claims.Nonce == "" || !validOperation(claims.Operation) {
		return GrantClaims{}, ErrInvalidGrant
	}
	if verifier.now().UTC().After(time.Unix(claims.ExpiresAt, 0)) {
		return GrantClaims{}, ErrExpiredGrant
	}
	if claims.RelayID != relayID || claims.TransferID != transferID || claims.FileID != fileID || claims.Operation != operation || requestedBytes < 0 || requestedBytes > claims.MaxBytes {
		return GrantClaims{}, ErrInvalidGrant
	}
	return claims, nil
}

func grantSigningMessage(payload string) []byte {
	return []byte("nexdrop/relay-grant/v2\n" + payload)
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

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}
