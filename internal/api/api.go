package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"nexdrop/internal/admin"
	"nexdrop/internal/analytics"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/domain"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/pairing"
	"nexdrop/internal/transfer"
	"nexdrop/internal/version"
)

type API struct {
	auth         *auth.Service
	devices      *device.Service
	pairing      *pairing.Service
	groups       *group.Service
	transfers    *transfer.Service
	files        *filetransfer.Service
	analytics    *analytics.Service
	admin        *admin.Service
	loginLimit   *fixedWindowLimiter
	pairingLimit *fixedWindowLimiter
	adminLimit   *fixedWindowLimiter
	cursorKey    []byte
}

func New(authService *auth.Service, deviceService *device.Service, pairingService *pairing.Service, groupService *group.Service, transferService *transfer.Service, fileService *filetransfer.Service, analyticsService *analytics.Service, adminService *admin.Service) *API {
	return NewWithCursorKey([]byte("nexdrop-development-cursor-key"), authService, deviceService, pairingService, groupService, transferService, fileService, analyticsService, adminService)
}

func NewWithCursorKey(cursorKey []byte, authService *auth.Service, deviceService *device.Service, pairingService *pairing.Service, groupService *group.Service, transferService *transfer.Service, fileService *filetransfer.Service, analyticsService *analytics.Service, adminService *admin.Service) *API {
	return &API{
		auth: authService, devices: deviceService, pairing: pairingService, groups: groupService, transfers: transferService, files: fileService, analytics: analyticsService, admin: adminService,
		loginLimit:   newFixedWindowLimiter(rateLimit("NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE", 10)),
		pairingLimit: newFixedWindowLimiter(rateLimit("NEXDROP_PAIRING_RATE_LIMIT_PER_MINUTE", 10)),
		adminLimit:   newFixedWindowLimiter(rateLimit("NEXDROP_ADMIN_RATE_LIMIT_PER_MINUTE", 30)),
		cursorKey:    append([]byte(nil), cursorKey...),
	}
}

type rateWindow struct {
	count int
	reset time.Time
}

type fixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	windows map[string]rateWindow
}

func newFixedWindowLimiter(limit int) *fixedWindowLimiter {
	return &fixedWindowLimiter{limit: limit, windows: make(map[string]rateWindow)}
}

func (limiter *fixedWindowLimiter) allow(key string, now time.Time) (bool, time.Duration) {
	limiter.mu.Lock()
	defer limiter.mu.Unlock()
	window := limiter.windows[key]
	if window.reset.IsZero() || !now.Before(window.reset) {
		window = rateWindow{reset: now.Add(time.Minute)}
	}
	window.count++
	limiter.windows[key] = window
	return window.count <= limiter.limit, time.Until(window.reset)
}

func rateLimit(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value < 1 {
		return fallback
	}
	return value
}

func rateLimitKey(r *http.Request, identity string) string {
	host := r.RemoteAddr
	if value, _, err := net.SplitHostPort(host); err == nil {
		host = value
	}
	return host + "|" + strings.ToLower(strings.TrimSpace(identity))
}

func enforceRateLimit(w http.ResponseWriter, r *http.Request, limiter *fixedWindowLimiter, identity string) bool {
	allowed, retryAfter := limiter.allow(rateLimitKey(r, identity), time.Now().UTC())
	if allowed {
		return true
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int(retryAfter.Seconds()))))
	writeError(w, http.StatusTooManyRequests, "RATE_LIMITED")
	return false
}

func (api *API) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", api.version)
	mux.HandleFunc("POST /api/auth/login", api.login)
	mux.HandleFunc("POST /api/auth/refresh", api.refresh)
	mux.HandleFunc("POST /api/auth/logout", api.logout)
	mux.HandleFunc("POST /api/auth/totp/setup", api.setupTOTP)
	mux.HandleFunc("POST /api/auth/totp/enable", api.enableTOTP)
	mux.HandleFunc("POST /api/auth/admin-verify", api.verifyAdmin)
	mux.HandleFunc("POST /api/auth/invitations/accept", api.acceptInvitation)
	mux.HandleFunc("GET /api/account", api.account)
	mux.HandleFunc("POST /api/devices", api.createDevice)
	mux.HandleFunc("GET /api/devices", api.listDevices)
	mux.HandleFunc("PATCH /api/devices/{id}", api.renameDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", api.deleteDevice)
	mux.HandleFunc("POST /api/devices/{id}/approve", api.approveDevice)
	mux.HandleFunc("POST /api/devices/{id}/revoke", api.revokeDevice)
	mux.HandleFunc("POST /api/devices/{id}/session-challenge", api.createDeviceSessionChallenge)
	mux.HandleFunc("POST /api/devices/{id}/attach-session", api.attachDeviceSession)
	mux.HandleFunc("PUT /api/devices/{id}/lan-identity", api.registerDeviceLANIdentity)
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
	mux.HandleFunc("POST /api/transfers", api.createTransfer)
	mux.HandleFunc("GET /api/transfers", api.listTransfers)
	mux.HandleFunc("GET /api/transfers/{id}", api.getTransfer)
	mux.HandleFunc("POST /api/transfers/{id}/cancel", api.cancelTransfer)
	mux.HandleFunc("DELETE /api/transfers/{id}", api.hideTransfer)
	mux.HandleFunc("POST /api/transfers/{id}/read", api.readTransfer)
	mux.HandleFunc("PUT /api/transfers/{id}/targets/{deviceId}", api.reportTransferProgress)
	mux.HandleFunc("POST /api/transfers/{id}/targets/{deviceId}/retry", api.retryTransferTarget)
	mux.HandleFunc("POST /api/files/{id}/chunks/{index}", api.uploadChunk)
	mux.HandleFunc("GET /api/files/{id}/chunks/{index}", api.downloadChunk)
	mux.HandleFunc("POST /api/files/{id}/complete", api.completeFile)
	mux.HandleFunc("POST /api/metrics/batch", api.uploadMetrics)
	mux.HandleFunc("GET /api/statistics/overview", api.statisticsOverview)
	mux.HandleFunc("GET /api/statistics/transfers", api.statisticsTransfers)
	mux.HandleFunc("GET /api/statistics/devices", api.statisticsDevices)
	mux.HandleFunc("GET /api/statistics/groups", api.statisticsGroups)
	mux.HandleFunc("GET /api/statistics/node", api.statisticsNode)
	mux.HandleFunc("GET /api/admin/users", api.adminUsers)
	mux.HandleFunc("POST /api/admin/users", api.createAdminUser)
	mux.HandleFunc("POST /api/admin/invitations", api.createAdminInvitation)
	mux.HandleFunc("DELETE /api/admin/users/{id}", api.disableAdminUser)
	mux.HandleFunc("POST /api/admin/users/{id}/reset-password", api.resetAdminPassword)
	mux.HandleFunc("GET /api/admin/devices", api.adminDevices)
	mux.HandleFunc("POST /api/admin/devices/{id}/revoke", api.revokeAdminDevice)
	mux.HandleFunc("GET /api/admin/groups", api.adminGroups)
	mux.HandleFunc("DELETE /api/admin/groups/{id}", api.deleteAdminGroup)
	mux.HandleFunc("GET /api/admin/settings", api.adminSettings)
	mux.HandleFunc("PUT /api/admin/settings", api.updateAdminSettings)
	mux.HandleFunc("PUT /api/admin/quotas/{ownerType}/{ownerId}", api.setAdminQuota)
	mux.HandleFunc("GET /api/admin/storage", api.adminStorage)
	mux.HandleFunc("GET /api/admin/failures", api.adminFailures)
	mux.HandleFunc("GET /api/admin/audit-logs", api.adminAuditLogs)
	mux.HandleFunc("DELETE /api/admin/group-transfers/{id}", api.deleteAdminGroupContent)
	return apiContract(mux)
}

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
		writer := &contractResponseWriter{ResponseWriter: w, requestID: requestID, versioned: strings.Contains(r.Header.Get("Accept"), versionMediaType)}
		started := time.Now()
		next.ServeHTTP(writer, r)
		status := writer.status
		if status == 0 {
			status = http.StatusOK
		}
		slog.Info("API request", "module", "api", "request_id", requestID, "method", r.Method, "path", r.URL.Path, "status", status, "error_code", writer.errorCode, "duration_ms", time.Since(started).Milliseconds())
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

func (api *API) version(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, version.Current())
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
		TOTP       string `json:"totp"`
	}
	if err := decodeJSON(r, &request); err != nil || request.Identifier == "" || request.Password == "" {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if !enforceRateLimit(w, r, api.loginLimit, request.Identifier) {
		return
	}
	pair, err := api.auth.LoginWithTOTP(r.Context(), request.Identifier, request.Password, request.TOTP)
	if err != nil {
		if errors.Is(err, auth.ErrTOTPRequired) {
			writeError(w, http.StatusUnauthorized, "TOTP_REQUIRED")
			return
		}
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
		return
	}
	writeJSON(w, http.StatusOK, pair)
}

func (api *API) setupTOTP(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
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
	setup, err := api.auth.SetupTOTP(r.Context(), session, request.Password)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_CREDENTIALS")
		return
	}
	writeJSON(w, http.StatusOK, setup)
}

func (api *API) enableTOTP(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		Password string `json:"password"`
		Secret   string `json:"secret"`
		Code     string `json:"code"`
	}
	if decodeJSON(r, &request) != nil || api.auth.EnableTOTP(r.Context(), session, request.Password, request.Secret, request.Code) != nil {
		writeError(w, http.StatusUnauthorized, "INVALID_TOTP_SETUP")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) verifyAdmin(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		Password string `json:"password"`
		TOTP     string `json:"totp"`
	}
	if decodeJSON(r, &request) != nil || api.auth.VerifyAdmin(r.Context(), session, request.Password, request.TOTP) != nil {
		writeError(w, http.StatusUnauthorized, "ADMIN_VERIFICATION_FAILED")
		return
	}
	w.WriteHeader(http.StatusNoContent)
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

func (api *API) registerDeviceLANIdentity(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request struct {
		Certificate string `json:"certificate"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	identity, err := api.devices.RegisterLANIdentity(r.Context(), session, r.PathValue("id"), request.Certificate)
	if err != nil {
		writeDeviceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, identity)
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
	if !enforceRateLimit(w, r, api.pairingLimit, session.SessionID) {
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
	if !enforceRateLimit(w, r, api.pairingLimit, request.ChallengeID) {
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

func (api *API) createTransfer(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	var request transfer.Request
	if key, ok := requireIdempotencyKey(w, r); !ok {
		return
	} else {
		request.IdempotencyKey = key
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := api.transfers.Create(r.Context(), session, request)
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (api *API) listTransfers(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if strings.Contains(r.Header.Get("Accept"), versionMediaType) {
		options, err := api.transferPageOptions(r)
		if err != nil {
			writeError(w, http.StatusBadRequest, "INVALID_PAGE")
			return
		}
		result, err := api.transfers.ListPage(r.Context(), session, options)
		if err != nil {
			writeTransferError(w, err)
			return
		}
		if result.NextCursor != "" {
			result.NextCursor = encodeCursor(api.cursorKey, result.NextPageKey.CreatedAt, result.NextPageKey.ID)
		}
		writeJSON(w, http.StatusOK, result)
		return
	}
	result, err := api.transfers.List(r.Context(), session)
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) transferPageOptions(r *http.Request) (transfer.PageOptions, error) {
	options := transfer.PageOptions{Limit: 50}
	cursor := strings.TrimSpace(r.URL.Query().Get("cursor"))
	var err error
	if value := r.URL.Query().Get("limit"); value != "" {
		options.Limit, err = strconv.Atoi(value)
	}
	if err == nil {
		if value := r.URL.Query().Get("from"); value != "" {
			options.From, err = time.Parse(time.RFC3339, value)
		}
	}
	if err == nil {
		if value := r.URL.Query().Get("to"); value != "" {
			options.To, err = time.Parse(time.RFC3339, value)
		}
	}
	options.Status = domain.TransferStatus(r.URL.Query().Get("status"))
	if options.Limit < 1 || options.Limit > 100 {
		err = errors.New("invalid limit")
	}
	if err == nil && cursor != "" {
		options.Cursor.CreatedAt, options.Cursor.ID, err = decodeCursor(api.cursorKey, cursor)
	}
	return options, err
}

func (api *API) getTransfer(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.transfers.Get(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) cancelTransfer(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	result, err := api.transfers.Cancel(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) hideTransfer(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if err := api.transfers.Hide(r.Context(), session, r.PathValue("id")); err != nil {
		writeTransferError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (api *API) readTransfer(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if _, ok := requireIdempotencyKey(w, r); !ok {
		return
	}
	result, err := api.transfers.Read(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) reportTransferProgress(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	var progress transfer.Progress
	if decodeJSON(r, &progress) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	progress.DeviceID = r.PathValue("deviceId")
	progress.IdempotencyKey = key
	result, err := api.transfers.ReportProgress(r.Context(), session, r.PathValue("id"), progress)
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) retryTransferTarget(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	if key == "" {
		writeError(w, http.StatusBadRequest, "IDEMPOTENCY_KEY_REQUIRED")
		return
	}
	result, err := api.transfers.Retry(r.Context(), session, r.PathValue("id"), r.PathValue("deviceId"), key)
	if err != nil {
		writeTransferError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) uploadChunk(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if _, ok := requireIdempotencyKey(w, r); !ok {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK")
		return
	}
	expectedHash, err := hex.DecodeString(r.Header.Get("X-Chunk-SHA256"))
	if err != nil || len(expectedHash) != 32 {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK_HASH")
		return
	}
	record, err := api.files.UploadChunk(r.Context(), session, r.PathValue("id"), index, expectedHash, r.Body)
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, record)
}

func (api *API) downloadChunk(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	index, err := strconv.Atoi(r.PathValue("index"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_CHUNK")
		return
	}
	record, file, err := api.files.OpenChunk(r.Context(), session, r.PathValue("id"), index)
	if err != nil {
		writeFileError(w, err)
		return
	}
	defer file.Close()
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(record.Size))
	w.Header().Set("ETag", `"`+hex.EncodeToString(record.SHA256)+`"`)
	w.WriteHeader(http.StatusOK)
	_, _ = io.Copy(w, file)
}

func (api *API) completeFile(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	if _, ok := requireIdempotencyKey(w, r); !ok {
		return
	}
	file, err := api.files.Complete(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeFileError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, file)
}

func (api *API) uploadMetrics(w http.ResponseWriter, r *http.Request) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	key, ok := requireIdempotencyKey(w, r)
	if !ok {
		return
	}
	var request struct {
		Events []analytics.Metric `json:"events"`
	}
	if err := decodeJSON(r, &request); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	encoded, err := json.Marshal(request.Events)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	fingerprint := sha256.Sum256(encoded)
	result, err := api.analytics.UploadIdempotent(r.Context(), session, key, fingerprint[:], request.Events)
	if err != nil {
		writeAnalyticsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (api *API) statisticsOverview(w http.ResponseWriter, r *http.Request) {
	api.withStatistics(w, r, func(ctx context.Context, session auth.Session, value analytics.TimeRange) (any, error) {
		return api.analytics.Overview(ctx, session, value)
	})
}

func (api *API) statisticsTransfers(w http.ResponseWriter, r *http.Request) {
	api.withStatistics(w, r, func(ctx context.Context, session auth.Session, value analytics.TimeRange) (any, error) {
		return api.analytics.Transfers(ctx, session, value)
	})
}

func (api *API) statisticsDevices(w http.ResponseWriter, r *http.Request) {
	api.withStatistics(w, r, func(ctx context.Context, session auth.Session, value analytics.TimeRange) (any, error) {
		return api.analytics.Devices(ctx, session, value)
	})
}

func (api *API) statisticsGroups(w http.ResponseWriter, r *http.Request) {
	api.withStatistics(w, r, func(ctx context.Context, session auth.Session, value analytics.TimeRange) (any, error) {
		return api.analytics.Groups(ctx, session, value)
	})
}

func (api *API) statisticsNode(w http.ResponseWriter, r *http.Request) {
	api.withStatistics(w, r, func(ctx context.Context, session auth.Session, value analytics.TimeRange) (any, error) {
		return api.analytics.Node(ctx, session, value)
	})
}

func (api *API) withStatistics(w http.ResponseWriter, r *http.Request, load func(context.Context, auth.Session, analytics.TimeRange) (any, error)) {
	session, ok := api.authenticate(w, r)
	if !ok {
		return
	}
	timeRange, err := parseTimeRange(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_TIME_RANGE")
		return
	}
	result, err := load(r.Context(), session, timeRange)
	if err != nil {
		writeAnalyticsError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func parseTimeRange(r *http.Request) (analytics.TimeRange, error) {
	to := time.Now().UTC()
	from := to.Add(-7 * 24 * time.Hour)
	var err error
	if value := r.URL.Query().Get("from"); value != "" {
		from, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return analytics.TimeRange{}, err
		}
	}
	if value := r.URL.Query().Get("to"); value != "" {
		to, err = time.Parse(time.RFC3339, value)
		if err != nil {
			return analytics.TimeRange{}, err
		}
	}
	return analytics.TimeRange{From: from, To: to}, nil
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
		if errors.Is(err, auth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "INVALID_TOKEN")
		} else {
			writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
		}
		return auth.Session{}, false
	}
	return session, true
}

func decodeJSON(r *http.Request, target any) error {
	decoder := json.NewDecoder(io.LimitReader(r.Body, 1<<20))
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
	if writer, ok := w.(*contractResponseWriter); ok {
		writer.errorCode = code
	}
	if writer, ok := w.(*contractResponseWriter); ok && writer.versioned {
		writeJSON(w, status, map[string]any{"error": map[string]any{
			"code": code, "message": errorMessage(code), "request_id": writer.requestID, "details": map[string]any{},
		}})
		return
	}
	writeJSON(w, status, map[string]string{"error": code})
}

func errorMessage(code string) string {
	if code == "INTERNAL_ERROR" {
		return "The server could not complete the request."
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
