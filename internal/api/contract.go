package api

import (
	"crypto/rand"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"nexdrop/internal/version"
)

const versionMediaType = "application/vnd.nexdrop.v1+json"

type contractResponseWriter struct {
	http.ResponseWriter
	requestID string
	versioned bool
	status    int
	errorCode string
}

func (writer *contractResponseWriter) WriteHeader(status int) {
	writer.status = status
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *contractResponseWriter) Write(data []byte) (int, error) {
	if writer.status == 0 {
		writer.WriteHeader(http.StatusOK)
	}
	return writer.ResponseWriter.Write(data)
}

func apiContract(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := newRequestID()
		w.Header().Set("X-Request-ID", requestID)
		w.Header().Set("X-NexDrop-API-Version", version.APIVersion)
		writer := &contractResponseWriter{
			ResponseWriter: w,
			requestID:      requestID,
			versioned:      strings.Contains(r.Header.Get("Accept"), versionMediaType),
		}
		started := time.Now()
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		attributes := []any{
			"module", "api",
			"request_id", requestID,
			"method", r.Method,
			"path", r.URL.Path,
			"status", status,
			"error_code", writer.errorCode,
			"duration_ms", time.Since(started).Milliseconds(),
		}
		if strings.HasPrefix(r.URL.Path, "/api/transfers/") {
			if transferID := r.PathValue("id"); transferID != "" {
				attributes = append(attributes, "transfer_id", transferID)
			}
		}
		slog.Info("API request", attributes...)
	})
}

func newRequestID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return fmt.Sprintf("request-%d", time.Now().UnixNano())
	}
	value[6] = (value[6] & 0x0f) | 0x40
	value[8] = (value[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", value[0:4], value[4:6], value[6:8], value[8:10], value[10:16])
}
