package folder

import (
	"encoding/hex"
	"errors"
	"path"
	"sort"
	"strings"
	"time"
	"unicode"
)

var (
	ErrInvalidManifest = errors.New("invalid folder manifest")
	ErrPathTraversal   = errors.New("folder path traversal")
	ErrPathCollision   = errors.New("folder path collision")
	ErrLimitExceeded   = errors.New("folder manifest limit exceeded")
)

type EntryType string

const (
	EntryDirectory EntryType = "directory"
	EntryFile      EntryType = "file"
)

type Entry struct {
	Path       string    `json:"path"`
	Type       EntryType `json:"type"`
	Size       int64     `json:"size,omitempty"`
	ModifiedAt time.Time `json:"modifiedAt,omitempty"`
	SHA256     string    `json:"sha256,omitempty"`
	MIMEType   string    `json:"mimeType,omitempty"`
	FileID     string    `json:"fileId,omitempty"`
}

type Manifest struct {
	Version  int     `json:"version"`
	RootName string  `json:"rootName"`
	Entries  []Entry `json:"entries"`
}

type Limits struct {
	MaxEntries       int
	MaxPathBytes     int
	MaxManifestBytes int64
	MaxTotalBytes    int64
	MaxDepth         int
}

func (limits Limits) normalized() Limits {
	if limits.MaxEntries <= 0 {
		limits.MaxEntries = 100000
	}
	if limits.MaxPathBytes <= 0 {
		limits.MaxPathBytes = 1024
	}
	if limits.MaxManifestBytes <= 0 {
		limits.MaxManifestBytes = 32 * 1024 * 1024
	}
	if limits.MaxTotalBytes <= 0 {
		limits.MaxTotalBytes = 16 * 1024 * 1024 * 1024 * 1024
	}
	if limits.MaxDepth <= 0 {
		limits.MaxDepth = 64
	}
	return limits
}

func Validate(manifest Manifest, limits Limits) (Manifest, error) {
	limits = limits.normalized()
	root := strings.TrimSpace(manifest.RootName)
	if manifest.Version != 1 || root == "" || strings.ContainsAny(root, "/\\\x00") || len(root) > 255 || len(manifest.Entries) > limits.MaxEntries {
		return Manifest{}, ErrInvalidManifest
	}
	validated := Manifest{Version: 1, RootName: root, Entries: make([]Entry, 0, len(manifest.Entries))}
	seen := make(map[string]string, len(manifest.Entries))
	var total int64
	var estimatedManifestBytes int64
	for _, entry := range manifest.Entries {
		normalized, depth, err := NormalizePath(entry.Path)
		if err != nil {
			return Manifest{}, err
		}
		if len(normalized) > limits.MaxPathBytes || depth > limits.MaxDepth {
			return Manifest{}, ErrLimitExceeded
		}
		folded := strings.ToLowerSpecial(unicode.TurkishCase, normalized)
		if previous, exists := seen[folded]; exists && previous != normalized {
			return Manifest{}, ErrPathCollision
		}
		if _, exists := seen[folded]; exists {
			return Manifest{}, ErrPathCollision
		}
		seen[folded] = normalized
		entry.Path = normalized
		switch entry.Type {
		case EntryDirectory:
			if entry.Size != 0 || entry.SHA256 != "" || entry.FileID != "" {
				return Manifest{}, ErrInvalidManifest
			}
		case EntryFile:
			if entry.Size < 0 || entry.FileID == "" || !validSHA256(entry.SHA256) {
				return Manifest{}, ErrInvalidManifest
			}
			if total > limits.MaxTotalBytes-entry.Size {
				return Manifest{}, ErrLimitExceeded
			}
			total += entry.Size
		default:
			return Manifest{}, ErrInvalidManifest
		}
		estimatedManifestBytes += int64(len(entry.Path) + len(entry.SHA256) + len(entry.MIMEType) + len(entry.FileID) + 96)
		if estimatedManifestBytes > limits.MaxManifestBytes {
			return Manifest{}, ErrLimitExceeded
		}
		validated.Entries = append(validated.Entries, entry)
	}
	return validated, nil
}

func NormalizePath(value string) (string, int, error) {
	if value == "" || strings.ContainsRune(value, 0) {
		return "", 0, ErrInvalidManifest
	}
	value = strings.ReplaceAll(value, "\\", "/")
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "//") || driveQualified(value) {
		return "", 0, ErrPathTraversal
	}
	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || strings.Contains(cleaned, "/../") {
		return "", 0, ErrPathTraversal
	}
	segments := strings.Split(cleaned, "/")
	for _, segment := range segments {
		if segment == "" || segment == "." || segment == ".." || strings.HasSuffix(segment, " ") || strings.HasSuffix(segment, ".") || windowsReserved(segment) {
			return "", 0, ErrInvalidManifest
		}
	}
	return strings.Join(segments, "/"), len(segments), nil
}

func Select(manifest Manifest, acceptedPaths []string) Manifest {
	accepted := make(map[string]struct{}, len(acceptedPaths))
	parents := make(map[string]struct{})
	for _, value := range acceptedPaths {
		normalized, _, err := NormalizePath(value)
		if err != nil {
			continue
		}
		accepted[normalized] = struct{}{}
		for parent := path.Dir(normalized); parent != "."; parent = path.Dir(parent) {
			parents[parent] = struct{}{}
		}
	}
	selected := Manifest{Version: manifest.Version, RootName: manifest.RootName}
	for _, entry := range manifest.Entries {
		_, fileAccepted := accepted[entry.Path]
		_, parentRequired := parents[entry.Path]
		if fileAccepted || (entry.Type == EntryDirectory && parentRequired) {
			selected.Entries = append(selected.Entries, entry)
		}
	}
	sort.SliceStable(selected.Entries, func(i, j int) bool { return selected.Entries[i].Path < selected.Entries[j].Path })
	return selected
}

type ConflictAction string

const (
	ConflictOverwrite ConflictAction = "OVERWRITE"
	ConflictRename    ConflictAction = "RENAME"
	ConflictSkip      ConflictAction = "SKIP"
	ConflictAsk       ConflictAction = "ASK"
)

func ResolveConflict(existingSize int64, existingSHA256 string, incoming Entry, action ConflictAction) ConflictAction {
	if incoming.Type == EntryFile && existingSize == incoming.Size && strings.EqualFold(existingSHA256, incoming.SHA256) {
		return ConflictSkip
	}
	switch action {
	case ConflictOverwrite, ConflictRename, ConflictSkip, ConflictAsk:
		return action
	default:
		return ConflictAsk
	}
}

func validSHA256(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && strings.ToLower(value) == value
}

func driveQualified(value string) bool {
	return len(value) >= 2 && ((value[0] >= 'a' && value[0] <= 'z') || (value[0] >= 'A' && value[0] <= 'Z')) && value[1] == ':'
}

func windowsReserved(segment string) bool {
	trimmed := strings.TrimRight(segment, " .")
	base := strings.ToUpper(strings.SplitN(trimmed, ".", 2)[0])
	if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" {
		return true
	}
	if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
		return true
	}
	return false
}
