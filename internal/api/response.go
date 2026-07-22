package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"nexdrop/internal/analytics"
	"nexdrop/internal/device"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/transfer"
)

func writeJSON(w http.ResponseWriter, status int, value any) {
	contentType := "application/json; charset=utf-8"
	if writer, ok := w.(*contractResponseWriter); ok && writer.versioned {
		contentType = versionMediaType + "; charset=utf-8"
	}
	w.Header().Set("Content-Type", contentType)
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writer, versioned := w.(*contractResponseWriter)
	if versioned {
		writer.errorCode = code
	}
	if versioned && writer.versioned {
		writeJSON(w, status, map[string]any{
			"error": map[string]any{
				"code":       code,
				"message":    errorMessage(code),
				"request_id": writer.requestID,
				"details":    map[string]any{},
			},
		})
		return
	}
	writeJSON(w, status, map[string]string{"error": code})
}

func errorMessage(code string) string {
	switch code {
	case "INTERNAL_ERROR":
		return "The server could not complete the request."
	case "RATE_LIMITED":
		return "Too many requests. Wait for the Retry-After interval before retrying."
	}
	return "The request could not be completed (" + code + ")."
}

func writeDeviceError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, device.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_DEVICE")
	case errors.Is(err, device.ErrNotFound):
		writeError(w, http.StatusNotFound, "DEVICE_NOT_FOUND")
	case errors.Is(err, device.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}

func writeGroupError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, group.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_GROUP_REQUEST")
	case errors.Is(err, group.ErrNotFound):
		writeError(w, http.StatusNotFound, "GROUP_NOT_FOUND")
	case errors.Is(err, group.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	case errors.Is(err, group.ErrConflict):
		writeError(w, http.StatusConflict, "GROUP_CONFLICT")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}

func writeTransferError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, transfer.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_TRANSFER")
	case errors.Is(err, transfer.ErrNotFound):
		writeError(w, http.StatusNotFound, "TRANSFER_NOT_FOUND")
	case errors.Is(err, transfer.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	case errors.Is(err, transfer.ErrFileTooLarge):
		writeError(w, http.StatusRequestEntityTooLarge, "FILE_TOO_LARGE")
	case errors.Is(err, transfer.ErrQuotaExceeded):
		writeError(w, http.StatusInsufficientStorage, "QUOTA_EXCEEDED")
	case errors.Is(err, transfer.ErrStorageFull):
		writeError(w, http.StatusInsufficientStorage, "STORAGE_FULL")
	case errors.Is(err, transfer.ErrConflict):
		writeError(w, http.StatusConflict, "TRANSFER_STATE_CONFLICT")
	case errors.Is(err, transfer.ErrIdempotencyConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}

func writeFileError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, filetransfer.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_FILE_OPERATION")
	case errors.Is(err, filetransfer.ErrNotFound):
		writeError(w, http.StatusNotFound, "FILE_NOT_FOUND")
	case errors.Is(err, filetransfer.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	case errors.Is(err, filetransfer.ErrConflict):
		writeError(w, http.StatusConflict, "FILE_CONFLICT")
	case errors.Is(err, filetransfer.ErrHash):
		writeError(w, http.StatusUnprocessableEntity, "HASH_MISMATCH")
	case errors.Is(err, filetransfer.ErrIncomplete):
		writeError(w, http.StatusConflict, "FILE_INCOMPLETE")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}

func writeAnalyticsError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, analytics.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_ANALYTICS_REQUEST")
	case errors.Is(err, analytics.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	case errors.Is(err, analytics.ErrConflict):
		writeError(w, http.StatusConflict, "IDEMPOTENCY_CONFLICT")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}
