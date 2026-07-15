package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
)

type API struct {
	auth    *auth.Service
	devices *device.Service
	pairing *pairing.Service
	groups  *group.Service
}

func New(authService *auth.Service, deviceService *device.Service, pairingService *pairing.Service, groupService *group.Service) *API {
	return &API{auth: authService, devices: deviceService, pairing: pairingService, groups: groupService}
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
	mux.HandleFunc("POST /api/devices/{id}/pairing-code", api.createPairingCode)
	mux.HandleFunc("POST /api/devices/{id}/pair", api.redeemPairingCode)
	mux.HandleFunc("POST /api/groups", api.createGroup)
	mux.HandleFunc("GET /api/groups", api.listGroups)
	mux.HandleFunc("GET /api/groups/{id}", api.getGroup)
	mux.HandleFunc("PATCH /api/groups/{id}", api.renameGroup)
	mux.HandleFunc("DELETE /api/groups/{id}", api.deleteGroup)
	mux.HandleFunc("POST /api/groups/{id}/members", api.addGroupMember)
	mux.HandleFunc("DELETE /api/groups/{id}/members/{userId}", api.removeGroupMember)
	mux.HandleFunc("POST /api/groups/{id}/devices", api.addGroupDevice)
	mux.HandleFunc("DELETE /api/groups/{id}/devices/{deviceId}", api.removeGroupDevice)
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

func (api *API) createPairingCode(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	challenge, err := api.pairing.Create(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writePairingError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, challenge)
}

func (api *API) redeemPairingCode(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		ChallengeID string `json:"challengeId"`
		Code        string `json:"code"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.pairing.Redeem(r.Context(), session, r.PathValue("id"), request.ChallengeID, request.Code)
	if err != nil {
		writePairingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) createGroup(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.groups.Create(r.Context(), session, request.Name)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) listGroups(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.groups.List(r.Context(), session)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) getGroup(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.groups.Get(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) renameGroup(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		Name string `json:"name"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.groups.Rename(r.Context(), session, r.PathValue("id"), request.Name)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) deleteGroup(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if err := api.groups.Delete(r.Context(), session, r.PathValue("id")); err != nil {
		writeGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) addGroupMember(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		UserID string     `json:"userId"`
		Role   group.Role `json:"role"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.groups.AddMember(r.Context(), session, r.PathValue("id"), request.UserID, request.Role)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) removeGroupMember(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if err := api.groups.RemoveMember(r.Context(), session, r.PathValue("id"), r.PathValue("userId")); err != nil {
		writeGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) addGroupDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		DeviceID string `json:"deviceId"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.groups.AddDevice(r.Context(), session, r.PathValue("id"), request.DeviceID)
	if err != nil {
		writeGroupError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) removeGroupDevice(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if err := api.groups.RemoveDevice(r.Context(), session, r.PathValue("id"), r.PathValue("deviceId")); err != nil {
		writeGroupError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func writePairingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, pairing.ErrInvalidCode):
		writeError(w, http.StatusBadRequest, "PAIRING_CODE_INVALID")
	case errors.Is(err, pairing.ErrExpired):
		writeError(w, http.StatusGone, "PAIRING_CODE_EXPIRED")
	case errors.Is(err, pairing.ErrUsed):
		writeError(w, http.StatusConflict, "PAIRING_CODE_USED")
	case errors.Is(err, pairing.ErrLocked):
		writeError(w, http.StatusTooManyRequests, "PAIRING_CODE_LOCKED")
	default:
		writeDeviceError(w, err)
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
