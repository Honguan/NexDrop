package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nexdrop/internal/adaptive"
	"nexdrop/internal/auth"
	"nexdrop/internal/delivery"
	"nexdrop/internal/enrollment"
	"nexdrop/internal/folder"
	"nexdrop/internal/messagelifecycle"
	"nexdrop/internal/relay"
	"nexdrop/internal/routing"
	"nexdrop/internal/v3"
)

type v3API struct {
	api             *API
	service         *v3.Service
	enrollmentLimit *fixedWindowLimiter
}

func (api *API) V3Routes(service *v3.Service) http.Handler {
	handler := &v3API{
		api: api, service: service,
		enrollmentLimit: newFixedWindowLimiter(rateLimit("NEXDROP_ENROLLMENT_RATE_LIMIT_PER_MINUTE", 10)),
	}
	mux := http.NewServeMux()
	mux.Handle("POST /api/v3/enrollment/bootstrap", api.nodeKeyRequired(http.HandlerFunc(handler.bootstrapEnrollment)))
	mux.Handle("POST /api/v3/enrollment/grants", api.nodeKeyRequired(http.HandlerFunc(handler.issueEnrollment)))
	mux.HandleFunc("POST /api/v3/enrollment/redeem", handler.redeemEnrollment)
	mux.HandleFunc("POST /api/v3/enrollment/attach-session", handler.attachEnrollmentSession)
	mux.HandleFunc("POST /api/v3/enrollment/grants/{id}/revoke", handler.revokeEnrollment)
	mux.HandleFunc("POST /api/v3/devices/{id}/credential/revoke", handler.revokeDeviceCredential)

	mux.HandleFunc("POST /api/v3/routes/plan", handler.planRoutes)
	mux.HandleFunc("POST /api/v3/routes/observations", handler.observeRoute)
	mux.HandleFunc("POST /api/v3/transfers/{id}/route", handler.switchRoute)
	mux.HandleFunc("POST /api/v3/transfers/{id}/profile", handler.recommendProfile)

	mux.HandleFunc("PUT /api/v3/transfers/{id}/targets/{deviceId}/delivery-policy", handler.putDeliveryPolicy)
	mux.HandleFunc("GET /api/v3/transfers/{id}/targets/{deviceId}/delivery-policy", handler.getDeliveryPolicy)
	mux.HandleFunc("POST /api/v3/transfers/{id}/targets/{deviceId}/delivery-evaluate", handler.evaluateDelivery)
	mux.HandleFunc("GET /api/v3/devices/{id}/delivery-queue", handler.deliveryQueue)

	mux.HandleFunc("POST /api/v3/relays", handler.registerRelay)
	mux.HandleFunc("GET /api/v3/relays", handler.listRelays)
	mux.HandleFunc("POST /api/v3/relays/{id}/heartbeat", handler.relayHeartbeat)
	mux.HandleFunc("POST /api/v3/relays/{id}/drain", handler.drainRelay)
	mux.HandleFunc("DELETE /api/v3/relays/{id}", handler.removeRelay)
	mux.HandleFunc("POST /api/v3/relay-assignments", handler.assignRelay)

	mux.HandleFunc("PUT /api/v3/transfers/{id}/folder-manifest", handler.saveFolderManifest)
	mux.HandleFunc("GET /api/v3/transfers/{id}/folder-manifest", handler.getFolderManifest)
	mux.HandleFunc("PUT /api/v3/transfers/{id}/folder-selection/{deviceId}", handler.saveFolderSelection)
	mux.HandleFunc("GET /api/v3/transfers/{id}/folder-selection/{deviceId}", handler.getFolderSelection)

	mux.HandleFunc("GET /api/v3/messages", handler.messages)
	mux.HandleFunc("PUT /api/v3/messages/read", handler.markMessagesRead)
	mux.HandleFunc("DELETE /api/v3/messages/{id}", handler.deleteMessage)
	mux.HandleFunc("PUT /api/v3/retention/{conversation}", handler.setRetention)
	mux.HandleFunc("POST /api/v3/retention/run", handler.runRetention)

	mux.HandleFunc("GET /api/v3/recovery/failed", handler.failedRecovery)
	mux.HandleFunc("GET /api/v3/recovery/transfers/{id}", handler.inspectRecovery)
	mux.HandleFunc("POST /api/v3/recovery/transfers/{id}/retry", handler.retryRecovery)
	mux.HandleFunc("POST /api/v3/recovery/run", handler.runRecovery)
	return apiContract(mux)
}

func (handler *v3API) session(w http.ResponseWriter, r *http.Request) (auth.Session, bool) {
	return handler.api.authenticate(w, r)
}

func requireWriteKey(w http.ResponseWriter, r *http.Request) bool {
	_, ok := requireIdempotencyKey(w, r)
	return ok
}

type enrollmentDeviceRequest struct {
	Token        string `json:"token,omitempty"`
	DeviceType   string `json:"deviceType"`
	DeviceName   string `json:"deviceName"`
	PublicKey    []byte `json:"publicKey"`
	KeyAlgorithm string `json:"keyAlgorithm"`
}

func (request enrollmentDeviceRequest) redeem() enrollment.RedeemRequest {
	return enrollment.RedeemRequest{
		Token: request.Token, DeviceType: request.DeviceType, DeviceName: request.DeviceName,
		PublicKey: request.PublicKey, KeyAlgorithm: request.KeyAlgorithm,
	}
}

func (handler *v3API) bootstrapEnrollment(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if !enforceRateLimit(w, r, handler.enrollmentLimit, session.ID) {
		return
	}
	var request enrollmentDeviceRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.BootstrapEnrollment(r.Context(), session, request.redeem())
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (handler *v3API) issueEnrollment(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if !enforceRateLimit(w, r, handler.enrollmentLimit, session.ID) {
		return
	}
	var request struct {
		TTLSeconds  int64                  `json:"ttlSeconds"`
		MaxUses     int                    `json:"maxUses"`
		DeviceType  string                 `json:"deviceType,omitempty"`
		NameHint    string                 `json:"nameHint,omitempty"`
		OwnerID     string                 `json:"ownerId,omitempty"`
		Permissions enrollment.Permissions `json:"permissions"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.IssueEnrollment(r.Context(), session, enrollment.IssueRequest{
		TTL: time.Duration(request.TTLSeconds) * time.Second, MaxUses: request.MaxUses,
		DeviceType: request.DeviceType, NameHint: request.NameHint, OwnerID: request.OwnerID,
		Permissions: request.Permissions,
	})
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (handler *v3API) redeemEnrollment(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if !enforceRateLimit(w, r, handler.enrollmentLimit, session.ID) {
		return
	}
	var request enrollmentDeviceRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.RedeemEnrollment(r.Context(), session, request.redeem())
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (handler *v3API) attachEnrollmentSession(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		DeviceID   string `json:"deviceId"`
		Credential string `json:"deviceCredential"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := handler.service.AttachSession(r.Context(), session, request.DeviceID, request.Credential); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) revokeEnrollment(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if err := handler.service.RevokeEnrollment(r.Context(), session, r.PathValue("id")); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) revokeDeviceCredential(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if err := handler.service.RevokeDeviceCredential(r.Context(), session, r.PathValue("id")); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type routeCandidateRequest struct {
	ID            string       `json:"id"`
	Endpoint      string       `json:"endpoint"`
	Kind          routing.Kind `json:"kind"`
	Authenticated bool         `json:"authenticated"`
	Healthy       bool         `json:"healthy"`
	LatencyMillis int64        `json:"latencyMillis,omitempty"`
	ThroughputBPS float64      `json:"throughputBytesPerSecond,omitempty"`
	FailureRate   float64      `json:"failureRate,omitempty"`
	LastSuccess   time.Time    `json:"lastSuccess,omitempty"`
}

func (handler *v3API) planRoutes(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	var request struct {
		DeviceID   string                  `json:"deviceId"`
		Candidates []routeCandidateRequest `json:"candidates"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	converted := make([]routing.Candidate, 0, len(request.Candidates))
	for _, candidate := range request.Candidates {
		converted = append(converted, routing.Candidate{
			ID: candidate.ID, Endpoint: candidate.Endpoint, Kind: candidate.Kind,
			Authenticated: candidate.Authenticated, Healthy: candidate.Healthy,
			HandshakeLatency: time.Duration(candidate.LatencyMillis) * time.Millisecond,
			ThroughputBPS:    candidate.ThroughputBPS, FailureRate: candidate.FailureRate,
			LastSuccess: candidate.LastSuccess,
		})
	}
	result, err := handler.service.PlanRoutes(r.Context(), session, v3.RoutePlanRequest{DeviceID: request.DeviceID, Candidates: converted})
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"attempts": result})
}

func (handler *v3API) observeRoute(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request v3.RouteObservation
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := handler.service.ObserveRoute(r.Context(), session, request); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) switchRoute(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request v3.RouteSwitch
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	request.TransferID = r.PathValue("id")
	if err := handler.service.SwitchRoute(r.Context(), session, request); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) recommendProfile(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		FileSize    int64            `json:"fileSize"`
		Current     adaptive.Profile `json:"current"`
		Limits      adaptive.Limits  `json:"limits"`
		Observation struct {
			RTTMillis           int64   `json:"rttMillis"`
			ThroughputBPS       float64 `json:"throughputBytesPerSecond"`
			RetryRate           float64 `json:"retryRate"`
			ChecksumFailureRate float64 `json:"checksumFailureRate"`
			Backpressure        bool    `json:"backpressure"`
			StoragePressure     bool    `json:"storagePressure"`
			ThermalPressure     bool    `json:"thermalPressure"`
			LowBattery          bool    `json:"lowBattery"`
			Background          bool    `json:"background"`
		} `json:"observation"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.RecommendProfile(r.Context(), session, r.PathValue("id"), request.FileSize, request.Current, request.Limits, adaptive.Observation{
		RTT:                 time.Duration(request.Observation.RTTMillis) * time.Millisecond,
		ThroughputBPS:       request.Observation.ThroughputBPS,
		RetryRate:           request.Observation.RetryRate,
		ChecksumFailureRate: request.Observation.ChecksumFailureRate,
		Backpressure:        request.Observation.Backpressure,
		StoragePressure:     request.Observation.StoragePressure,
		ThermalPressure:     request.Observation.ThermalPressure,
		LowBattery:          request.Observation.LowBattery,
		Background:          request.Observation.Background,
	})
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

type deliveryPolicyRequest struct {
	Priority          delivery.Priority `json:"priority"`
	Size              int64             `json:"size"`
	ExpiresAt         time.Time         `json:"expiresAt,omitempty"`
	WiFiOnly          bool              `json:"wifiOnly"`
	ChargingOnly      bool              `json:"chargingOnly"`
	MaxMobileDataSize int64             `json:"maximumMobileDataBytes"`
	AutomaticDownload bool              `json:"automaticDownload"`
	ForegroundOnly    bool              `json:"foregroundOnly"`
}

func (handler *v3API) putDeliveryPolicy(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request deliveryPolicyRequest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.PutDeliveryPolicy(r.Context(), session, v3.DeliveryRecord{
		TransferID: r.PathValue("id"), TargetDeviceID: r.PathValue("deviceId"),
		Priority: request.Priority, Size: request.Size,
		Policy: delivery.Policy{ExpiresAt: request.ExpiresAt, WiFiOnly: request.WiFiOnly,
			ChargingOnly: request.ChargingOnly, MaxMobileDataSize: request.MaxMobileDataSize,
			AutomaticDownload: request.AutomaticDownload, ForegroundOnly: request.ForegroundOnly},
	})
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) getDeliveryPolicy(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	result, err := handler.service.DeliveryQueue(r.Context(), session, r.PathValue("deviceId"), 500)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	for _, item := range result {
		if item.TransferID == r.PathValue("id") {
			writeJSON(w, http.StatusOK, item)
			return
		}
	}
	writeError(w, http.StatusNotFound, "NOT_FOUND")
}

func (handler *v3API) evaluateDelivery(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		Online       bool  `json:"online"`
		WiFi         bool  `json:"wifi"`
		Metered      bool  `json:"metered"`
		Charging     bool  `json:"charging"`
		Foreground   bool  `json:"foreground"`
		StorageBytes int64 `json:"storageBytes"`
		LowBattery   bool  `json:"lowBattery"`
		ThermalHigh  bool  `json:"thermalHigh"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.EvaluateDelivery(r.Context(), session, r.PathValue("id"), r.PathValue("deviceId"), delivery.Environment{
		Now: time.Now().UTC(), Online: request.Online, WiFi: request.WiFi,
		Metered: request.Metered, Charging: request.Charging, Foreground: request.Foreground,
		StorageBytes: request.StorageBytes, LowBattery: request.LowBattery, ThermalHigh: request.ThermalHigh,
	})
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) deliveryQueue(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := handler.service.DeliveryQueue(r.Context(), session, r.PathValue("id"), limit)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (handler *v3API) registerRelay(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request v3.RelayRegistration
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	registered, credential, err := handler.service.RegisterRelay(r.Context(), session, request)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"relay": registered, "credential": credential})
}

func (handler *v3API) listRelays(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	result, err := handler.service.ListRelays(r.Context(), session)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) relayHeartbeat(w http.ResponseWriter, r *http.Request) {
	if !requireWriteKey(w, r) {
		return
	}
	var request v3.RelayHeartbeat
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	request.RelayID = r.PathValue("id")
	if err := handler.service.HeartbeatRelay(r.Context(), request); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) drainRelay(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		Draining bool `json:"draining"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := handler.service.DrainRelay(r.Context(), session, r.PathValue("id"), request.Draining); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) removeRelay(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if err := handler.service.RemoveRelay(r.Context(), session, r.PathValue("id")); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) assignRelay(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		TransferID      string          `json:"transferId"`
		FileID          string          `json:"fileId"`
		Operation       relay.Operation `json:"operation"`
		RequiredBytes   int64           `json:"requiredBytes"`
		PreferredRegion string          `json:"preferredRegion,omitempty"`
		FirstChunk      int             `json:"firstChunk"`
		LastChunk       int             `json:"lastChunk"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.AssignRelay(r.Context(), session, request.TransferID, request.FileID,
		request.Operation, request.RequiredBytes, request.PreferredRegion, request.FirstChunk, request.LastChunk)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (handler *v3API) saveFolderManifest(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request folder.Manifest
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.SaveFolderManifest(r.Context(), session, r.PathValue("id"), request)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) getFolderManifest(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	result, err := handler.service.FolderManifest(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) saveFolderSelection(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		Entries []v3.FolderSelection `json:"entries"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := handler.service.SaveFolderSelection(r.Context(), session, r.PathValue("id"), r.PathValue("deviceId"), request.Entries); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) getFolderSelection(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	result, err := handler.service.FolderSelection(r.Context(), session, r.PathValue("id"), r.PathValue("deviceId"))
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"entries": result})
}

func (handler *v3API) messages(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = 50
	}
	result, err := handler.service.Messages(r.Context(), session, r.URL.Query().Get("conversation"), r.URL.Query().Get("cursor"), limit)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) markMessagesRead(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		Conversation string    `json:"conversation"`
		MessageID    string    `json:"messageId"`
		CreatedAt    time.Time `json:"createdAt"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.MarkRead(r.Context(), session, request.Conversation, request.MessageID, request.CreatedAt)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) deleteMessage(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	scope := messagelifecycle.DeleteScope(r.URL.Query().Get("scope"))
	tombstone, err := handler.service.DeleteMessage(r.Context(), session, r.PathValue("id"), scope)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	if tombstone == "" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"tombstone": tombstone})
}

func (handler *v3API) setRetention(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	var request struct {
		Mode          messagelifecycle.RetentionMode `json:"mode"`
		Days          int                            `json:"days,omitempty"`
		TombstoneDays int                            `json:"tombstoneDays"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	conversation, err := decodeConversationPath(r.PathValue("conversation"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	if err := handler.service.SetRetention(r.Context(), session, conversation,
		messagelifecycle.RetentionPolicy{Mode: request.Mode, Days: request.Days}, request.TombstoneDays); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeConversationPath(value string) (string, error) {
	value = strings.TrimSpace(value)
	if value == "inbox" || strings.HasPrefix(value, "group:") {
		return value, nil
	}
	return "", errors.New("invalid conversation")
}

func (handler *v3API) runRetention(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if !session.Admin || !session.AdminVerified {
		writeError(w, http.StatusForbidden, "ADMIN_VERIFICATION_REQUIRED")
		return
	}
	var request struct {
		Limit int `json:"limit"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.RunRetention(r.Context(), request.Limit)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (handler *v3API) failedRecovery(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	result, err := handler.service.FailedRecovery(r.Context(), session, limit)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (handler *v3API) inspectRecovery(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok {
		return
	}
	result, err := handler.service.InspectRecovery(r.Context(), session, r.PathValue("id"))
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": result})
}

func (handler *v3API) retryRecovery(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if err := handler.service.RetryRecovery(r.Context(), session, r.PathValue("id")); err != nil {
		writeV3Error(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (handler *v3API) runRecovery(w http.ResponseWriter, r *http.Request) {
	session, ok := handler.session(w, r)
	if !ok || !requireWriteKey(w, r) {
		return
	}
	if !session.Admin || !session.AdminVerified {
		writeError(w, http.StatusForbidden, "ADMIN_VERIFICATION_REQUIRED")
		return
	}
	var request struct {
		Limit int `json:"limit"`
	}
	if decodeJSON(r, &request) != nil {
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
		return
	}
	result, err := handler.service.RunRecovery(r.Context(), "operator", request.Limit)
	if err != nil {
		writeV3Error(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func writeV3Error(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, v3.ErrInvalid), errors.Is(err, enrollment.ErrInvalidToken),
		errors.Is(err, folder.ErrInvalidManifest), errors.Is(err, folder.ErrPathTraversal),
		errors.Is(err, folder.ErrPathCollision), errors.Is(err, folder.ErrLimitExceeded),
		errors.Is(err, messagelifecycle.ErrInvalidCursor), errors.Is(err, messagelifecycle.ErrInvalidTombstone),
		errors.Is(err, relay.ErrInvalidGrant):
		writeError(w, http.StatusBadRequest, "INVALID_REQUEST")
	case errors.Is(err, enrollment.ErrExpiredToken):
		writeError(w, http.StatusGone, "ENROLLMENT_TOKEN_EXPIRED")
	case errors.Is(err, enrollment.ErrExhausted):
		writeError(w, http.StatusConflict, "ENROLLMENT_TOKEN_EXHAUSTED")
	case errors.Is(err, enrollment.ErrRevoked):
		writeError(w, http.StatusGone, "ENROLLMENT_TOKEN_REVOKED")
	case errors.Is(err, relay.ErrNoRelay):
		writeError(w, http.StatusServiceUnavailable, "NO_RELAY_AVAILABLE")
	case errors.Is(err, relay.ErrExpiredGrant), errors.Is(err, messagelifecycle.ErrExpiredTombstone):
		writeError(w, http.StatusGone, "TOKEN_EXPIRED")
	case errors.Is(err, v3.ErrForbidden), errors.Is(err, auth.ErrForbidden):
		writeError(w, http.StatusForbidden, "PERMISSION_DENIED")
	case errors.Is(err, v3.ErrNotFound):
		writeError(w, http.StatusNotFound, "NOT_FOUND")
	case errors.Is(err, v3.ErrConflict):
		writeError(w, http.StatusConflict, "STATE_CONFLICT")
	default:
		writeError(w, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE")
	}
}

func compactJSON(value any) string {
	encoded, _ := json.Marshal(value)
	return string(encoded)
}
