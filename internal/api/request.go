package api

import (
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

const maximumJSONBodySize = 1 << 20

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, maximumJSONBodySize))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func requireIdempotencyKey(w http.ResponseWriter, r *http.Request) (string, bool) {
	if !strings.Contains(r.Header.Get("Accept"), versionMediaType) {
		return "", true
	}
	key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if !validUUID(key) {
		writeError(w, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED")
		return "", false
	}
	return key, true
}

func validUUID(value string) bool {
	compact := strings.ReplaceAll(value, "-", "")
	decoded, err := hex.DecodeString(compact)
	return err == nil && len(decoded) == 16 && len(value) == 36
}
