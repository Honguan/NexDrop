package api

import (
	"errors"
	"net/http"
	"strconv"

	"nexdrop/internal/admin"
	"nexdrop/internal/auth"
)

func (api *API) adminUsers(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	limit, offset, ok := adminPage(w, r)
	if !ok {
		return
	}
	result, err := api.admin.Users(r.Context(), session, limit, offset)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) createAdminUser(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	var request struct {
		Username string `json:"username"`
		Email    string `json:"email"`
		Password string `json:"password"`
		Admin    bool   `json:"admin"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.admin.CreateUser(r.Context(), session, request.Username, request.Email, request.Password, request.Admin)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) disableAdminUser(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	if err := api.admin.DisableUser(r.Context(), session, r.PathValue("id")); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) resetAdminPassword(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	var request struct {
		Password string `json:"password"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := api.admin.ResetPassword(r.Context(), session, r.PathValue("id"), request.Password); err != nil {
		writeAdminError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) adminSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	result, err := api.admin.Settings(r.Context(), session)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) updateAdminSettings(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	var request admin.NodeSettings
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.admin.UpdateSettings(r.Context(), session, request)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) setAdminQuota(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	var request struct {
		ByteLimit          int64 `json:"byteLimit"`
		DailyTransferLimit int64 `json:"dailyTransferLimit"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.admin.SetQuota(r.Context(), session, admin.Quota{OwnerType: r.PathValue("ownerType"), OwnerID: r.PathValue("ownerId"), ByteLimit: request.ByteLimit, DailyTransferLimit: request.DailyTransferLimit})
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) adminStorage(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	result, err := api.admin.Storage(r.Context(), session)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) adminFailures(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticateAdmin(w, r)
	if !ok {
		return
	}
	limit, offset, ok := adminPage(w, r)
	if !ok {
		return
	}
	result, err := api.admin.Failures(r.Context(), session, limit, offset)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) authenticateAdmin(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return auth.Session{}, false
	}
	if !session.Admin || !session.AdminVerified {
		writeError(w, http.StatusForbidden, "ADMIN_REAUTH_REQUIRED")
		return auth.Session{}, false
	}
	return session, true
}

func (api *API) adminAuditLogs(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	limit, offset, ok := adminPage(w, r)
	if !ok {
		return
	}
	result, err := api.admin.AuditLogs(r.Context(), session, limit, offset)
	if err != nil {
		writeAdminError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func adminPage(w http.ResponseWriter, r *http.Request) (int, int, bool) {
	limit, offset := 50, 0
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		limit, err = strconv.Atoi(value)
	}
	if err == nil {
		if value := r.URL.Query().Get("offset"); value != "" {
			offset, err = strconv.Atoi(value)
		}
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return 0, 0, false
	}
	return limit, offset, true
}

func writeAdminError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, admin.ErrInvalid):
		writeError(w, http.StatusBadRequest, "INVALID_ADMIN_REQUEST")
	case errors.Is(err, admin.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	case errors.Is(err, admin.ErrNotFound):
		writeError(w, http.StatusNotFound, "ADMIN_RESOURCE_NOT_FOUND")
	case errors.Is(err, admin.ErrConflict):
		writeError(w, http.StatusConflict, "ADMIN_RESOURCE_CONFLICT")
	default:
		writeError(w, http.StatusInternalServerError, "INTERNAL_ERROR")
	}
}
