package version

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"strings"
)

const (
	APIVersion           = "1"
	CurrentProtocol      = "1.2"
	PreviousProtocol     = "1.1"
	LegacyProtocol       = "1.0"
	MinimumClientVersion = "1.0"

	CapabilitySchemaVersion   = 1
	CapabilityUnavailableCode = "CAPABILITY_UNAVAILABLE"

	CapabilityNegotiation = "capability_negotiation"
	StructuredErrors      = "structured_errors"
	CursorPagination      = "cursor_pagination"
	IdempotencyReplay     = "idempotency_replay"
	ResumableChunks       = "resumable_chunks"
	RealtimeVersions      = "realtime_versions"
)

var (
	ProductVersion = "2.1.0"
	BuildCommit    = "development"
)

type Information struct {
	ProductVersion          string   `json:"productVersion"`
	BuildCommit             string   `json:"buildCommit"`
	APIVersion              string   `json:"apiVersion"`
	ProtocolVersion         string   `json:"protocolVersion"`
	PreviousProtocol        string   `json:"previousProtocolVersion"`
	LegacyProtocol          string   `json:"legacyProtocolVersion"`
	MinimumClientVersion    string   `json:"minimumClientVersion"`
	NodeIdentity            string   `json:"nodeIdentity"`
	CapabilitySchemaVersion int      `json:"capabilitySchemaVersion"`
	VersionFingerprint      string   `json:"versionFingerprint"`
	Capabilities            []string `json:"capabilities"`
	Limits                  Limits   `json:"limits"`
}

func Current() Information {
	capabilities := SupportedCapabilities()
	limits := CurrentLimits()
	information := Information{
		ProductVersion:          ProductVersion,
		BuildCommit:             BuildCommit,
		APIVersion:              APIVersion,
		ProtocolVersion:         CurrentProtocol,
		PreviousProtocol:        PreviousProtocol,
		LegacyProtocol:          LegacyProtocol,
		MinimumClientVersion:    MinimumClientVersion,
		NodeIdentity:            nodeIdentity(),
		CapabilitySchemaVersion: CapabilitySchemaVersion,
		Capabilities:            capabilities,
		Limits:                  limits,
	}
	fingerprint := sha256.Sum256([]byte(fmt.Sprintf(
		"%s|%s|%s|%d|%s|%d|%d|%d",
		information.ProductVersion,
		information.BuildCommit,
		information.ProtocolVersion,
		information.CapabilitySchemaVersion,
		strings.Join(capabilities, ","),
		limits.MaxChunkSize,
		limits.MaxParallelChunks,
		limits.MaxRecipients,
	)))
	information.VersionFingerprint = hex.EncodeToString(fingerprint[:])
	return information
}

func SupportedProtocol(value string) bool {
	return value == CurrentProtocol || value == PreviousProtocol || value == LegacyProtocol || value == "1"
}

func SupportedClient(value string) bool {
	separator := strings.LastIndex(value, "-v")
	if separator < 1 || separator+2 >= len(value) {
		return false
	}
	major, minor, ok := parse(strings.TrimSpace(value[separator+2:]))
	return ok && major == 1 && minor >= 0 && minor <= 2
}

func parse(value string) (int, int, bool) {
	parts := strings.Split(value, ".")
	if len(parts) < 1 || len(parts) > 2 {
		return 0, 0, false
	}
	major, err := strconv.Atoi(parts[0])
	if err != nil || major < 0 {
		return 0, 0, false
	}
	minor := 0
	if len(parts) == 2 {
		minor, err = strconv.Atoi(parts[1])
		if err != nil || minor < 0 {
			return 0, 0, false
		}
	}
	return major, minor, true
}

type Limits struct {
	MaxChunkSize      int64 `json:"maxChunkSize"`
	MaxParallelChunks int   `json:"maxParallelChunks"`
	MaxRecipients     int   `json:"maxRecipients"`
}

func CurrentLimits() Limits {
	return Limits{
		MaxChunkSize:      8 * 1024 * 1024,
		MaxParallelChunks: 3,
		MaxRecipients:     100,
	}
}

type CapabilityDefinition struct {
	ID               string
	SchemaVersion    int
	Parties          []string
	Fallback         string
	UnavailableError string
}

var capabilityRegistry = []CapabilityDefinition{
	{ID: CapabilityNegotiation, SchemaVersion: CapabilitySchemaVersion, Parties: []string{"node", "client"}, Fallback: "Use protocol and minimum-client version checks.", UnavailableError: CapabilityUnavailableCode},
	{ID: StructuredErrors, SchemaVersion: CapabilitySchemaVersion, Parties: []string{"node", "client"}, Fallback: "Parse the legacy string error envelope.", UnavailableError: CapabilityUnavailableCode},
	{ID: CursorPagination, SchemaVersion: CapabilitySchemaVersion, Parties: []string{"node", "client"}, Fallback: "Use the legacy unpaginated or offset-compatible history response.", UnavailableError: CapabilityUnavailableCode},
	{ID: IdempotencyReplay, SchemaVersion: CapabilitySchemaVersion, Parties: []string{"node", "client"}, Fallback: "Do not automatically retry a non-idempotent request.", UnavailableError: CapabilityUnavailableCode},
	{ID: ResumableChunks, SchemaVersion: CapabilitySchemaVersion, Parties: []string{"sender", "receiver"}, Fallback: "Restart the file transfer from the first chunk.", UnavailableError: CapabilityUnavailableCode},
	{ID: RealtimeVersions, SchemaVersion: CapabilitySchemaVersion, Parties: []string{"node", "client"}, Fallback: "Use the initial HTTP version document and periodic refresh.", UnavailableError: CapabilityUnavailableCode},
}

func Registry() []CapabilityDefinition {
	result := make([]CapabilityDefinition, len(capabilityRegistry))
	for index, definition := range capabilityRegistry {
		result[index] = definition
		result[index].Parties = append([]string(nil), definition.Parties...)
	}
	return result
}

func SupportedCapabilities() []string {
	result := make([]string, len(capabilityRegistry))
	for index, definition := range capabilityRegistry {
		result[index] = definition.ID
	}
	return result
}

func NegotiateCapabilities(advertised []string) []string {
	supported := make(map[string]struct{}, len(capabilityRegistry))
	for _, definition := range capabilityRegistry {
		supported[definition.ID] = struct{}{}
	}
	result := make([]string, 0, len(advertised))
	seen := make(map[string]struct{}, len(advertised))
	for _, capability := range advertised {
		capability = strings.TrimSpace(capability)
		if _, ok := supported[capability]; !ok {
			continue
		}
		if _, duplicate := seen[capability]; duplicate {
			continue
		}
		seen[capability] = struct{}{}
		result = append(result, capability)
	}
	return result
}

func MutualCapabilities(parties ...[]string) []string {
	if len(parties) == 0 {
		return []string{}
	}
	allowed := make(map[string]struct{}, len(capabilityRegistry))
	for _, capability := range NegotiateCapabilities(parties[0]) {
		allowed[capability] = struct{}{}
	}
	for _, advertised := range parties[1:] {
		current := make(map[string]struct{}, len(advertised))
		for _, capability := range NegotiateCapabilities(advertised) {
			current[capability] = struct{}{}
		}
		for capability := range allowed {
			if _, ok := current[capability]; !ok {
				delete(allowed, capability)
			}
		}
	}
	result := make([]string, 0, len(allowed))
	for _, definition := range capabilityRegistry {
		if _, ok := allowed[definition.ID]; ok {
			result = append(result, definition.ID)
		}
	}
	return result
}

type CapabilityUnavailableError struct {
	Code       string
	Capability string
}

func (err CapabilityUnavailableError) Error() string {
	return err.Code + ": " + err.Capability
}

func RequireCapabilities(advertised []string, required ...string) error {
	available := make(map[string]struct{}, len(advertised))
	for _, capability := range NegotiateCapabilities(advertised) {
		available[capability] = struct{}{}
	}
	for _, capability := range required {
		if _, ok := available[capability]; !ok {
			return CapabilityUnavailableError{Code: CapabilityUnavailableCode, Capability: capability}
		}
	}
	return nil
}

func nodeIdentity() string {
	identity := strings.TrimSpace(os.Getenv("NEXDROP_NODE_ID"))
	if len(identity) >= 8 && len(identity) <= 128 {
		valid := true
		for _, character := range identity {
			if (character < 'a' || character > 'z') &&
				(character < 'A' || character > 'Z') &&
				(character < '0' || character > '9') &&
				!strings.ContainsRune("._~-", character) {
				valid = false
				break
			}
		}
		if valid {
			return identity
		}
	}
	return "node-development"
}
