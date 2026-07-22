package api

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"nexdrop/internal/analytics"
	"nexdrop/internal/auth"
	"nexdrop/internal/device"
	"nexdrop/internal/domain"
	"nexdrop/internal/filetransfer"
	"nexdrop/internal/group"
	"nexdrop/internal/transfer"
	"nexdrop/internal/version"
)

type API struct {
	auth       *auth.Service
	devices    *device.Service
	groups     *group.Service
	transfers  *transfer.Service
	files      *filetransfer.Service
	analytics  *analytics.Service
	loginLimit *fixedWindowLimiter
	cursorKey  []byte
	nodeKey    string
}

func New(authService *auth.Service, deviceService *device.Service, groupService *group.Service, transferService *transfer.Service, fileService *filetransfer.Service, analyticsService *analytics.Service) *API {
	return NewWithCursorKey([]byte("nexdrop-development-cursor-key"), authService, deviceService, groupService, transferService, fileService, analyticsService)
}

func NewWithCursorKey(cursorKey []byte, authService *auth.Service, deviceService *device.Service, groupService *group.Service, transferService *transfer.Service, fileService *filetransfer.Service, analyticsService *analytics.Service) *API {
	return &API{
		auth: authService, devices: deviceService, groups: groupService, transfers: transferService, files: fileService, analytics: analyticsService,
		loginLimit: newFixedWindowLimiter(rateLimit("NEXDROP_LOGIN_RATE_LIMIT_PER_MINUTE", 10)),
		cursorKey:  append([]byte(nil), cursorKey...),
		nodeKey:    strings.TrimSpace(os.Getenv("NEXDROP_NODE_KEY")),
	}
}

func (api *API) nodeKeyRequired(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if api.nodeKey == "" {
			next.ServeHTTP(w, r)
			return
		}
		provided := strings.TrimSpace(r.Header.Get("X-NexDrop-Node-Key"))
		if len(provided) != len(api.nodeKey) || subtle.ConstantTimeCompare([]byte(provided), []byte(api.nodeKey)) != 1 {
			writeError(w, http.StatusUnauthorized, "NODE_KEY_REQUIRED")
			return
		}
		next.ServeHTTP(w, r)
	})
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
	mux.HandleFunc("GET /api/account", api.account)
	mux.Handle("POST /api/devices", api.nodeKeyRequired(http.HandlerFunc(api.createDevice)))
	mux.HandleFunc("GET /api/devices", api.listDevices)
	mux.HandleFunc("PATCH /api/devices/{id}", api.renameDevice)
	mux.HandleFunc("DELETE /api/devices/{id}", api.deleteDevice)
	mux.HandleFunc("POST /api/devices/{id}/revoke", api.revokeDevice)
	mux.HandleFunc("POST /api/devices/{id}/session-challenge", api.createDeviceSessionChallenge)
	mux.HandleFunc("POST /api/devices/{id}/attach-session", api.attachDeviceSession)
	mux.HandleFunc("PUT /api/devices/{id}/lan-identity", api.registerDeviceLANIdentity)
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
	return apiContract(mux)
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
