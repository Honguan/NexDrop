package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
)

type API struct {
	auth    *auth.Service
	devices *device.Service
}

func New(authService *auth.Service, deviceService *device.Service) *API {
	return &API{auth: authService, devices: deviceService}
}

func (api *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /api/auth/login", api.login)
	mux.HandleFunc("POST /api/auth/refresh", api.refresh)
	mux.HandleFunc("POST /api/auth/logout", api.logout)
	mux.HandleFunc("GET /api/account", api.account)
	mux.HandleFunc("POST /api/devices", api.createDevice)
	mux.HandleFunc("GET /api/devices", api.listDevices)
	mux.HandleFunc("PATCH /api/devices/{id}", api.renameDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", api.deleteDevice)
	mux.HandleFunc("POST /api/devices/{id}/approve", api.approveDevice)
	mux.HandleFunc("POST /api/devices/{id}/revoke", api.revokeDevice)
	return mux
}

func (api *API) refresh(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	pair, err := api.auth.Refresh(r.Context(), request.RefreshToken)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (api *API) login(w http.ResponseWriter, r *http.Request) {
	var request struct {
		Identifier string `json:"identifier"`
		Password   string `json:"password"`
	}
	if err := decodeJSON(r, &request); err != nil || request.Identifier == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	pair, err := api.auth.Login(r.Context(), request.Identifier, request.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (api *API) logout(w http.ResponseWriter, r *http.Request) {
	var request struct {
		RefreshToken string `json:"refreshToken"`
	}
	if err := decodeJSON(r, &request); err != nil || api.auth.Logout(r.Context(), request.RefreshToken) != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) account(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, session.User)
}

func (api *API) createDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		DisplayName  string      `json:"displayName"`
		Type         device.Type `json:"type"`
		PublicKey    []byte      `json:"publicKey"`
		KeyAlgorithm string      `json:"keyAlgorithm"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.devices.Create(r.Context(), session, request.DisplayName, request.Type, request.PublicKey, request.KeyAlgorithm)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) listDevices(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.devices.List(r.Context(), session)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) renameDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		DisplayName string `json:"displayName"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.devices.Rename(r.Context(), session, r.PathValue("id"), request.DisplayName)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) deleteDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if err := api.devices.Delete(r.Context(), session, r.PathValue("id")); err != nil {
		writeDeviceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) approveDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.devices.Approve(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) revokeDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.devices.Revoke(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) authenticate(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	header := r.Header.Get("Authorization")
	if !strings.HasPrefix(header, "Bearer ") {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return auth.Session{}, false
	}
	token := strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	session, err := api.auth.Authenticate(r.Context(), token)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		return auth.Session{}, false
	}
	return session, true
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	return decoder.Decode(target)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
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
