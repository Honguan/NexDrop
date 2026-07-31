package v3

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"nexdrop/internal/adaptive"
	"nexdrop/internal/auth"
	"nexdrop/internal/delivery"
	"nexdrop/internal/enrollment"
	"nexdrop/internal/folder"
	"nexdrop/internal/messagelifecycle"
	"nexdrop/internal/recovery"
	"nexdrop/internal/relay"
	"nexdrop/internal/routing"
)

var (
	ErrInvalid   = errors.New("invalid v3 request")
	ErrForbidden = errors.New("v3 operation forbidden")
	ErrNotFound  = errors.New("v3 resource not found")
	ErrConflict  = errors.New("v3 state conflict")
)

type RouteHistory struct {
	RouteID          string
	HandshakeLatency time.Duration
	ThroughputBPS    float64
	FailureRate      float64
	LastSuccess      time.Time
}

type RouteObservation struct {
	DeviceID         string        `json:"deviceId"`
	RouteID          string        `json:"routeId"`
	Kind             routing.Kind  `json:"kind"`
	Endpoint         string        `json:"endpoint"`
	Success          bool          `json:"success"`
	HandshakeLatency time.Duration `json:"-"`
	LatencyMillis    int64         `json:"latencyMillis"`
	ThroughputBPS    float64       `json:"throughputBytesPerSecond"`
	ErrorCode        string        `json:"errorCode,omitempty"`
	ObservedAt       time.Time     `json:"observedAt,omitempty"`
}

type RoutePlanRequest struct {
	DeviceID  string              `json:"deviceId"`
	Candidates []routing.Candidate `json:"candidates"`
}

type RouteAttempt struct {
	ID         string       `json:"id"`
	Endpoint   string       `json:"endpoint"`
	Kind       routing.Kind `json:"kind"`
	StartAfter int64        `json:"startAfterMillis"`
	Score      float64      `json:"score"`
}

type RouteSwitch struct {
	TransferID     string         `json:"transferId"`
	DeviceID       string         `json:"deviceId"`
	Route          routing.Kind   `json:"route"`
	Reason         string         `json:"reason"`
	VerifiedChunks map[int]string `json:"verifiedChunks,omitempty"`
}

type DeliveryRecord struct {
	TransferID             string
	TargetDeviceID          string
	Priority                delivery.Priority
	Size                    int64
	Policy                  delivery.Policy
	State                   delivery.State
	ReasonCode              string
	MetadataSynchronized    bool
	BodyDownloaded          bool
	UpdatedAt               time.Time
}

type RelayRecord struct {
	Relay          relay.Relay `json:"relay"`
	IdentityKey    []byte      `json:"-"`
	CredentialHash []byte      `json:"-"`
	Retention      time.Duration
}

type RelayRegistration struct {
	ID            string `json:"id,omitempty"`
	Endpoint      string `json:"endpoint"`
	Region        string `json:"region,omitempty"`
	IdentityKey   []byte `json:"identityPublicKey"`
	CapacityBytes int64  `json:"capacityBytes"`
	RetentionSecs int    `json:"retentionSeconds,omitempty"`
}

type RelayHeartbeat struct {
	RelayID       string  `json:"relayId"`
	Credential    string  `json:"credential"`
	UsedBytes     int64   `json:"usedBytes"`
	LatencyMillis int64   `json:"latencyMillis"`
	FailureRate   float64 `json:"failureRate"`
	Healthy       bool    `json:"healthy"`
}

type RelayAssignment struct {
	RelayID   string    `json:"relayId"`
	Endpoint  string    `json:"endpoint"`
	Grant     string    `json:"grant"`
	ExpiresAt time.Time `json:"expiresAt"`
}

type FolderSelection struct {
	EntryIndex int                   `json:"entryIndex"`
	Accepted   bool                  `json:"accepted"`
	Conflict   folder.ConflictAction `json:"conflictAction"`
}

type MessageView struct {
	ID                 string     `json:"id"`
	TransferID         string     `json:"transferId,omitempty"`
	GroupID            string     `json:"groupId,omitempty"`
	SenderUserID       string     `json:"senderUserId"`
	SenderDeviceID     string     `json:"senderDeviceId"`
	ContentType        string     `json:"contentType"`
	EncryptedContent   []byte     `json:"encryptedContent,omitempty"`
	CreatedAt          time.Time  `json:"createdAt"`
	ExpiresAt          *time.Time `json:"expiresAt,omitempty"`
	AttachmentBytes    int64      `json:"attachmentBytes"`
	EveryTargetFetched bool       `json:"everyTargetFetched"`
	Pinned             bool       `json:"pinned"`
	TombstonedAt       *time.Time `json:"tombstonedAt,omitempty"`
}

type MessagePage struct {
	Items      []MessageView `json:"items"`
	NextCursor string        `json:"nextCursor,omitempty"`
}

type RetentionCandidate struct {
	Message MessageView
	Policy  messagelifecycle.RetentionPolicy
}

type CleanupReport struct {
	Scanned           int `json:"scanned"`
	BodiesDeleted     int `json:"bodiesDeleted"`
	TombstonesCreated int `json:"tombstonesCreated"`
}

type Store interface {
	enrollment.Store
	recovery.Store

	AttachSessionByCredential(context.Context, auth.Session, string, []byte, time.Time) error
	RevokeDeviceCredential(context.Context, auth.Session, string, time.Time) error
	DeviceAccessible(context.Context, auth.Session, string) (bool, error)

	RouteHistory(context.Context, auth.Session, string) ([]RouteHistory, error)
	RecordRouteObservation(context.Context, auth.Session, RouteObservation) error
	SwitchTransferRoute(context.Context, auth.Session, RouteSwitch, time.Time) error
	SetAdaptiveProfile(context.Context, auth.Session, string, adaptive.Profile, time.Time) error

	UpsertDeliveryPolicy(context.Context, auth.Session, DeliveryRecord) (DeliveryRecord, error)
	DeliveryPolicy(context.Context, auth.Session, string, string) (DeliveryRecord, error)
	ListDeliveryQueue(context.Context, auth.Session, string, int) ([]DeliveryRecord, error)
	UpdateDeliveryDecision(context.Context, auth.Session, string, string, delivery.Decision, time.Time) error

	RegisterRelay(context.Context, auth.Session, RelayRecord, time.Time) (relay.Relay, error)
	ListRelays(context.Context, auth.Session) ([]relay.Relay, error)
	RelayByID(context.Context, string) (relay.Relay, error)
	RelayHeartbeat(context.Context, RelayHeartbeat, []byte, time.Time) error
	SetRelayDraining(context.Context, auth.Session, string, bool, time.Time) error
	RemoveRelay(context.Context, auth.Session, string, time.Time) error
	AssignRelayChunks(context.Context, auth.Session, string, string, int, int, time.Time) error

	SaveFolderManifest(context.Context, auth.Session, string, folder.Manifest, []byte, time.Time) error
	FolderManifest(context.Context, auth.Session, string) (folder.Manifest, error)
	SaveFolderSelection(context.Context, auth.Session, string, string, []FolderSelection, time.Time) error
	FolderSelection(context.Context, auth.Session, string, string) ([]FolderSelection, error)

	MessagePage(context.Context, auth.Session, string, messagelifecycle.Cursor, int) ([]MessageView, messagelifecycle.Cursor, error)
	AdvanceMessageRead(context.Context, auth.Session, string, messagelifecycle.ReadCursor, time.Time) (messagelifecycle.ReadCursor, error)
	SaveMessageTombstone(context.Context, auth.Session, messagelifecycle.Tombstone, []byte, time.Time) error
	MarkMessageLocalRemoved(context.Context, auth.Session, string, time.Time) error
	AttachmentPaths(context.Context, auth.Session, string) ([]string, error)
	MarkAttachmentBodyDeleted(context.Context, auth.Session, string, time.Time) error
	SetRetentionPolicy(context.Context, auth.Session, string, messagelifecycle.RetentionPolicy, int, time.Time) error
	RetentionCandidates(context.Context, time.Time, int) ([]RetentionCandidate, error)
	RecordRetentionRun(context.Context, CleanupReport, time.Time, time.Time, string) error

	FailedRecovery(context.Context, int) ([]recovery.WorkItem, error)
	InspectRecovery(context.Context, string) ([]recovery.WorkItem, error)
	RetryRecovery(context.Context, string, time.Time) error
	ReconcileRecovery(context.Context, recovery.WorkItem, string, time.Time) (recovery.Outcome, error)
	RecordRecoveryRun(context.Context, recovery.Report, string, time.Time, time.Time, string) error
}

type Service struct {
	store       Store
	enrollment  *enrollment.Service
	recovery    *recovery.Coordinator
	relaySigner *relay.GrantSigner
	cursor      *messagelifecycle.CursorCodec
	tombstones  *messagelifecycle.TombstoneSigner
	storageRoot string
	now         func() time.Time
}

func New(store Store, nodeID string, rootSecret, cursorSecret []byte, storageRoot string) (*Service, error) {
	if store == nil || strings.TrimSpace(storageRoot) == "" {
		return nil, ErrInvalid
	}
	enrollmentService, err := enrollment.New(store, nodeID, rootSecret)
	if err != nil {
		return nil, err
	}
	relaySigner, err := relay.NewGrantSigner(rootSecret)
	if err != nil {
		return nil, err
	}
	cursor, err := messagelifecycle.NewCursorCodec(cursorSecret)
	if err != nil {
		return nil, err
	}
	tombstones, err := messagelifecycle.NewTombstoneSigner(rootSecret)
	if err != nil {
		return nil, err
	}
	service := &Service{
		store: store, enrollment: enrollmentService, relaySigner: relaySigner,
		cursor: cursor, tombstones: tombstones, storageRoot: filepath.Clean(storageRoot), now: time.Now,
	}
	service.recovery = recovery.New(store, service, recovery.Config{})
	return service, nil
}

func requireAdmin(session auth.Session) error {
	if !session.Admin || !session.AdminVerified {
		return ErrForbidden
	}
	return nil
}

func (service *Service) IssueEnrollment(ctx context.Context, session auth.Session, request enrollment.IssueRequest) (enrollment.Issued, error) {
	if session.ID == "" {
		return enrollment.Issued{}, ErrForbidden
	}
	if request.OwnerID == "" {
		request.OwnerID = session.ID
	}
	if request.OwnerID != session.ID || request.MaxUses > 1 {
		if err := requireAdmin(session); err != nil {
			return enrollment.Issued{}, err
		}
	}
	return service.enrollment.Issue(ctx, request)
}

func (service *Service) RedeemEnrollment(ctx context.Context, session auth.Session, request enrollment.RedeemRequest) (enrollment.Credential, error) {
	if session.ID == "" {
		return enrollment.Credential{}, ErrForbidden
	}
	request.OwnerID = session.ID
	return service.enrollment.Redeem(ctx, request)
}

func (service *Service) BootstrapEnrollment(ctx context.Context, session auth.Session, request enrollment.RedeemRequest) (enrollment.Credential, error) {
	issued, err := service.enrollment.Issue(ctx, enrollment.IssueRequest{
		TTL: 5 * time.Minute, MaxUses: 1, DeviceType: request.DeviceType,
		NameHint: request.DeviceName, OwnerID: session.ID,
		Permissions: enrollment.Permissions{SendFiles: true, ReceiveBroadcast: true},
	})
	if err != nil {
		return enrollment.Credential{}, err
	}
	request.Token = issued.Token
	request.OwnerID = session.ID
	return service.enrollment.Redeem(ctx, request)
}

func (service *Service) AttachSession(ctx context.Context, session auth.Session, deviceID, credential string) error {
	if session.ID == "" || deviceID == "" || credential == "" {
		return ErrInvalid
	}
	digest := sha256.Sum256([]byte(credential))
	return service.store.AttachSessionByCredential(ctx, session, deviceID, digest[:], service.now().UTC())
}

func (service *Service) RevokeEnrollment(ctx context.Context, session auth.Session, grantID string) error {
	if session.ID == "" || grantID == "" {
		return ErrInvalid
	}
	return service.enrollment.Revoke(ctx, grantID)
}

func (service *Service) RevokeDeviceCredential(ctx context.Context, session auth.Session, deviceID string) error {
	if session.ID == "" || deviceID == "" {
		return ErrInvalid
	}
	return service.store.RevokeDeviceCredential(ctx, session, deviceID, service.now().UTC())
}

func (service *Service) PlanRoutes(ctx context.Context, session auth.Session, request RoutePlanRequest) ([]RouteAttempt, error) {
	if session.DeviceID == nil || request.DeviceID == "" || len(request.Candidates) == 0 || len(request.Candidates) > 16 {
		return nil, ErrInvalid
	}
	accessible, err := service.store.DeviceAccessible(ctx, session, request.DeviceID)
	if err != nil || !accessible {
		if err != nil {
			return nil, err
		}
		return nil, ErrForbidden
	}
	history, err := service.store.RouteHistory(ctx, session, request.DeviceID)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]RouteHistory, len(history))
	for _, item := range history {
		byID[item.RouteID] = item
	}
	for index := range request.Candidates {
		candidate := &request.Candidates[index]
		candidate.Authenticated = candidate.Authenticated && accessible
		if previous, ok := byID[candidate.ID]; ok {
			candidate.HandshakeLatency = previous.HandshakeLatency
			candidate.ThroughputBPS = previous.ThroughputBPS
			candidate.FailureRate = previous.FailureRate
			candidate.LastSuccess = previous.LastSuccess
		}
	}
	planned := routing.Plan(request.Candidates, service.now().UTC(), routing.Config{})
	result := make([]RouteAttempt, 0, len(planned))
	for _, attempt := range planned {
		result = append(result, RouteAttempt{
			ID: attempt.Candidate.ID, Endpoint: attempt.Candidate.Endpoint, Kind: attempt.Candidate.Kind,
			StartAfter: attempt.StartAfter.Milliseconds(), Score: attempt.Score,
		})
	}
	return result, nil
}

func (service *Service) ObserveRoute(ctx context.Context, session auth.Session, observation RouteObservation) error {
	if session.DeviceID == nil || observation.DeviceID == "" || observation.RouteID == "" || observation.Endpoint == "" || observation.LatencyMillis < 0 || observation.ThroughputBPS < 0 || len(observation.ErrorCode) > 100 {
		return ErrInvalid
	}
	accessible, err := service.store.DeviceAccessible(ctx, session, observation.DeviceID)
	if err != nil || !accessible {
		if err != nil {
			return err
		}
		return ErrForbidden
	}
	observation.HandshakeLatency = time.Duration(observation.LatencyMillis) * time.Millisecond
	observation.ObservedAt = service.now().UTC()
	return service.store.RecordRouteObservation(ctx, session, observation)
}

func (service *Service) SwitchRoute(ctx context.Context, session auth.Session, request RouteSwitch) error {
	if session.DeviceID == nil || request.TransferID == "" || request.DeviceID == "" || request.Route == "" || len(request.Reason) > 100 {
		return ErrInvalid
	}
	for index, hash := range request.VerifiedChunks {
		if index < 0 || len(hash) != 64 {
			return ErrInvalid
		}
		if _, err := hex.DecodeString(hash); err != nil {
			return ErrInvalid
		}
	}
	return service.store.SwitchTransferRoute(ctx, session, request, service.now().UTC())
}

func (service *Service) RecommendProfile(ctx context.Context, session auth.Session, transferID string, fileSize int64, current adaptive.Profile, limits adaptive.Limits, observation adaptive.Observation) (adaptive.Profile, error) {
	if session.DeviceID == nil || fileSize < 0 {
		return adaptive.Profile{}, ErrInvalid
	}
	profile := adaptive.Recommend(fileSize, current, limits, observation)
	if transferID != "" {
		if err := service.store.SetAdaptiveProfile(ctx, session, transferID, profile, service.now().UTC()); err != nil {
			return adaptive.Profile{}, err
		}
	}
	return profile, nil
}

func (service *Service) PutDeliveryPolicy(ctx context.Context, session auth.Session, record DeliveryRecord) (DeliveryRecord, error) {
	if session.DeviceID == nil || record.TransferID == "" || record.TargetDeviceID == "" || record.Size < 0 {
		return DeliveryRecord{}, ErrInvalid
	}
	if record.Policy.MaxMobileDataSize < -1 {
		return DeliveryRecord{}, ErrInvalid
	}
	if record.State == "" {
		record.State = delivery.StateQueued
	}
	record.UpdatedAt = service.now().UTC()
	return service.store.UpsertDeliveryPolicy(ctx, session, record)
}

func (service *Service) EvaluateDelivery(ctx context.Context, session auth.Session, transferID, targetDeviceID string, environment delivery.Environment) (delivery.Decision, error) {
	record, err := service.store.DeliveryPolicy(ctx, session, transferID, targetDeviceID)
	if err != nil {
		return delivery.Decision{}, err
	}
	if environment.Now.IsZero() {
		environment.Now = service.now().UTC()
	}
	decision := delivery.Evaluate(delivery.Item{
		ID: record.TransferID, Priority: record.Priority, Size: record.Size,
		CreatedAt: record.UpdatedAt, Policy: record.Policy, MetadataSynchronized: record.MetadataSynchronized,
	}, environment)
	if err := service.store.UpdateDeliveryDecision(ctx, session, transferID, targetDeviceID, decision, service.now().UTC()); err != nil {
		return delivery.Decision{}, err
	}
	return decision, nil
}

func (service *Service) DeliveryQueue(ctx context.Context, session auth.Session, targetDeviceID string, limit int) ([]DeliveryRecord, error) {
	if session.DeviceID == nil || targetDeviceID == "" {
		return nil, ErrInvalid
	}
	if limit < 1 || limit > 500 {
		limit = 100
	}
	items, err := service.store.ListDeliveryQueue(ctx, session, targetDeviceID, limit)
	if err != nil {
		return nil, err
	}
	ranked := make([]delivery.Item, 0, len(items))
	byID := make(map[string]DeliveryRecord, len(items))
	for _, item := range items {
		key := item.TransferID + ":" + item.TargetDeviceID
		byID[key] = item
		ranked = append(ranked, delivery.Item{ID: key, Priority: item.Priority, Size: item.Size, CreatedAt: item.UpdatedAt, Policy: item.Policy, MetadataSynchronized: item.MetadataSynchronized})
	}
	ordered := delivery.Order(ranked)
	result := make([]DeliveryRecord, 0, len(ordered))
	for _, item := range ordered {
		result = append(result, byID[item.ID])
	}
	return result, nil
}

func (service *Service) RegisterRelay(ctx context.Context, session auth.Session, request RelayRegistration) (relay.Relay, string, error) {
	if err := requireAdmin(session); err != nil {
		return relay.Relay{}, "", err
	}
	if request.Endpoint == "" || len(request.IdentityKey) < 32 || request.CapacityBytes < 0 {
		return relay.Relay{}, "", ErrInvalid
	}
	credentialBytes := make([]byte, 32)
	seed := sha256.Sum256([]byte(request.Endpoint + time.Now().UTC().String() + session.SessionID))
	copy(credentialBytes, seed[:])
	credential := hex.EncodeToString(credentialBytes)
	digest := sha256.Sum256([]byte(credential))
	retention := time.Duration(request.RetentionSecs) * time.Second
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}
	registered, err := service.store.RegisterRelay(ctx, session, RelayRecord{
		Relay: relay.Relay{ID: request.ID, Endpoint: request.Endpoint, Region: request.Region, CapacityBytes: request.CapacityBytes},
		IdentityKey: request.IdentityKey, CredentialHash: digest[:], Retention: retention,
	}, service.now().UTC())
	return registered, credential, err
}

func (service *Service) ListRelays(ctx context.Context, session auth.Session) ([]relay.Relay, error) {
	if err := requireAdmin(session); err != nil {
		return nil, err
	}
	return service.store.ListRelays(ctx, session)
}

func (service *Service) HeartbeatRelay(ctx context.Context, heartbeat RelayHeartbeat) error {
	if heartbeat.RelayID == "" || heartbeat.Credential == "" || heartbeat.UsedBytes < 0 || heartbeat.LatencyMillis < 0 || heartbeat.FailureRate < 0 || heartbeat.FailureRate > 1 {
		return ErrInvalid
	}
	digest := sha256.Sum256([]byte(heartbeat.Credential))
	return service.store.RelayHeartbeat(ctx, heartbeat, digest[:], service.now().UTC())
}

func (service *Service) DrainRelay(ctx context.Context, session auth.Session, relayID string, draining bool) error {
	if err := requireAdmin(session); err != nil {
		return err
	}
	return service.store.SetRelayDraining(ctx, session, relayID, draining, service.now().UTC())
}

func (service *Service) RemoveRelay(ctx context.Context, session auth.Session, relayID string) error {
	if err := requireAdmin(session); err != nil {
		return err
	}
	return service.store.RemoveRelay(ctx, session, relayID, service.now().UTC())
}

func (service *Service) AssignRelay(ctx context.Context, session auth.Session, transferID, fileID string, operation relay.Operation, requiredBytes int64, preferredRegion string, firstChunk, lastChunk int) (RelayAssignment, error) {
	if session.DeviceID == nil || transferID == "" || fileID == "" || requiredBytes < 0 || firstChunk < 0 || lastChunk < firstChunk {
		return RelayAssignment{}, ErrInvalid
	}
	relays, err := service.store.ListRelays(ctx, session)
	if err != nil {
		return RelayAssignment{}, err
	}
	selected, err := relay.Select(relays, requiredBytes, preferredRegion)
	if err != nil {
		return RelayAssignment{}, err
	}
	ttl := 15 * time.Minute
	grant, err := service.relaySigner.Issue(selected.ID, transferID, fileID, operation, requiredBytes, ttl)
	if err != nil {
		return RelayAssignment{}, err
	}
	if err := service.store.AssignRelayChunks(ctx, session, selected.ID, fileID, firstChunk, lastChunk, service.now().UTC().Add(ttl)); err != nil {
		return RelayAssignment{}, err
	}
	return RelayAssignment{RelayID: selected.ID, Endpoint: selected.Endpoint, Grant: grant, ExpiresAt: service.now().UTC().Add(ttl)}, nil
}

func (service *Service) SaveFolderManifest(ctx context.Context, session auth.Session, transferID string, manifest folder.Manifest) (folder.Manifest, error) {
	if session.DeviceID == nil || transferID == "" {
		return folder.Manifest{}, ErrInvalid
	}
	validated, err := folder.Validate(manifest, folder.Limits{})
	if err != nil {
		return folder.Manifest{}, err
	}
	encoded, err := json.Marshal(validated)
	if err != nil {
		return folder.Manifest{}, err
	}
	digest := sha256.Sum256(encoded)
	if err := service.store.SaveFolderManifest(ctx, session, transferID, validated, digest[:], service.now().UTC()); err != nil {
		return folder.Manifest{}, err
	}
	return validated, nil
}

func (service *Service) FolderManifest(ctx context.Context, session auth.Session, transferID string) (folder.Manifest, error) {
	if transferID == "" {
		return folder.Manifest{}, ErrInvalid
	}
	return service.store.FolderManifest(ctx, session, transferID)
}

func (service *Service) SaveFolderSelection(ctx context.Context, session auth.Session, transferID, targetDeviceID string, selections []FolderSelection) error {
	if session.DeviceID == nil || transferID == "" || targetDeviceID == "" || len(selections) > 100000 {
		return ErrInvalid
	}
	for _, selection := range selections {
		if selection.EntryIndex < 0 {
			return ErrInvalid
		}
		switch selection.Conflict {
		case folder.ConflictAsk, folder.ConflictOverwrite, folder.ConflictRename, folder.ConflictSkip:
		default:
			return ErrInvalid
		}
	}
	return service.store.SaveFolderSelection(ctx, session, transferID, targetDeviceID, selections, service.now().UTC())
}

func (service *Service) FolderSelection(ctx context.Context, session auth.Session, transferID, targetDeviceID string) ([]FolderSelection, error) {
	if transferID == "" || targetDeviceID == "" {
		return nil, ErrInvalid
	}
	return service.store.FolderSelection(ctx, session, transferID, targetDeviceID)
}

func (service *Service) Messages(ctx context.Context, session auth.Session, conversationKey, cursorValue string, limit int) (MessagePage, error) {
	if session.ID == "" || conversationKey == "" || limit < 1 || limit > 100 {
		return MessagePage{}, ErrInvalid
	}
	var cursor messagelifecycle.Cursor
	var err error
	if cursorValue != "" {
		cursor, err = service.cursor.Decode(cursorValue)
		if err != nil {
			return MessagePage{}, err
		}
	}
	items, next, err := service.store.MessagePage(ctx, session, conversationKey, cursor, limit)
	if err != nil {
		return MessagePage{}, err
	}
	result := MessagePage{Items: items}
	if next.ID != "" {
		result.NextCursor, err = service.cursor.Encode(next)
	}
	return result, err
}

func (service *Service) MarkRead(ctx context.Context, session auth.Session, conversationKey, messageID string, createdAt time.Time) (messagelifecycle.ReadCursor, error) {
	if session.DeviceID == nil || conversationKey == "" || messageID == "" || createdAt.IsZero() {
		return messagelifecycle.ReadCursor{}, ErrInvalid
	}
	return service.store.AdvanceMessageRead(ctx, session, conversationKey, messagelifecycle.ReadCursor{CreatedAt: createdAt.UTC(), MessageID: messageID}, service.now().UTC())
}

func (service *Service) DeleteMessage(ctx context.Context, session auth.Session, messageID string, scope messagelifecycle.DeleteScope) (string, error) {
	if session.DeviceID == nil || messageID == "" {
		return "", ErrInvalid
	}
	switch scope {
	case messagelifecycle.DeleteLocal:
		return "", service.store.MarkMessageLocalRemoved(ctx, session, messageID, service.now().UTC())
	case messagelifecycle.DeleteAttachmentBody:
		paths, err := service.store.AttachmentPaths(ctx, session, messageID)
		if err != nil {
			return "", err
		}
		for _, path := range paths {
			if err := service.removeStoredPath(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return "", err
			}
		}
		return "", service.store.MarkAttachmentBodyDeleted(ctx, session, messageID, service.now().UTC())
	case messagelifecycle.DeleteEverywhere:
		token, err := service.tombstones.Issue(messageID, *session.DeviceID, scope, 90*24*time.Hour)
		if err != nil {
			return "", err
		}
		tombstone, err := service.tombstones.Verify(token)
		if err != nil {
			return "", err
		}
		digest := sha256.Sum256([]byte(token))
		if err := service.store.SaveMessageTombstone(ctx, session, tombstone, digest[:], service.now().UTC()); err != nil {
			return "", err
		}
		return token, nil
	default:
		return "", ErrInvalid
	}
}

func (service *Service) SetRetention(ctx context.Context, session auth.Session, conversationKey string, policy messagelifecycle.RetentionPolicy, tombstoneDays int) error {
	if err := requireAdmin(session); err != nil {
		return err
	}
	if conversationKey == "" || tombstoneDays < 1 || tombstoneDays > 3650 {
		return ErrInvalid
	}
	switch policy.Mode {
	case messagelifecycle.RetentionPermanent, messagelifecycle.RetentionAfterAllDownloaded, messagelifecycle.RetentionBodyAfterExpiry:
	case messagelifecycle.RetentionFixedDays:
		if policy.Days < 1 || policy.Days > 3650 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return service.store.SetRetentionPolicy(ctx, session, conversationKey, policy, tombstoneDays, service.now().UTC())
}

func (service *Service) RunRetention(ctx context.Context, limit int) (CleanupReport, error) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	started := service.now().UTC()
	candidates, err := service.store.RetentionCandidates(ctx, started, limit)
	if err != nil {
		return CleanupReport{}, err
	}
	report := CleanupReport{Scanned: len(candidates)}
	var runErr error
	for _, candidate := range candidates {
		message := messagelifecycle.Message{
			ID: candidate.Message.ID, CreatedAt: candidate.Message.CreatedAt,
			AttachmentBytes: candidate.Message.AttachmentBytes, EveryTargetFetched: candidate.Message.EveryTargetFetched,
			ExpiresAt: dereferenceTime(candidate.Message.ExpiresAt), Pinned: candidate.Message.Pinned,
			TombstonedAt: candidate.Message.TombstonedAt,
		}
		switch messagelifecycle.EvaluateRetention(message, candidate.Policy, started) {
		case messagelifecycle.RetentionDeleteBody:
			paths, pathErr := service.store.AttachmentPaths(ctx, auth.Session{}, message.ID)
			if pathErr != nil {
				runErr = pathErr
				break
			}
			for _, path := range paths {
				if removeErr := service.removeStoredPath(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
					runErr = removeErr
					break
				}
			}
			if runErr == nil {
				runErr = service.store.MarkAttachmentBodyDeleted(ctx, auth.Session{}, message.ID, started)
				report.BodiesDeleted++
			}
		case messagelifecycle.RetentionTombstone:
			token, signErr := service.tombstones.Issue(message.ID, "retention-worker", messagelifecycle.DeleteEverywhere, 90*24*time.Hour)
			if signErr != nil {
				runErr = signErr
				break
			}
			tombstone, verifyErr := service.tombstones.Verify(token)
			if verifyErr != nil {
				runErr = verifyErr
				break
			}
			digest := sha256.Sum256([]byte(token))
			runErr = service.store.SaveMessageTombstone(ctx, auth.Session{}, tombstone, digest[:], started)
			if runErr == nil {
				report.TombstonesCreated++
			}
		}
		if runErr != nil {
			break
		}
	}
	code := ""
	if runErr != nil {
		code = "RETENTION_FAILED"
	}
	_ = service.store.RecordRetentionRun(ctx, report, started, service.now().UTC(), code)
	return report, runErr
}

func (service *Service) RunRecovery(ctx context.Context, trigger string, limit int) (recovery.Report, error) {
	started := service.now().UTC()
	report, err := service.recovery.RunOnce(ctx, limit)
	code := ""
	if err != nil {
		code = "RECOVERY_FAILED"
	}
	_ = service.store.RecordRecoveryRun(ctx, report, trigger, started, service.now().UTC(), code)
	return report, err
}

func (service *Service) FailedRecovery(ctx context.Context, session auth.Session, limit int) ([]recovery.WorkItem, error) {
	if err := requireAdmin(session); err != nil {
		return nil, err
	}
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	return service.store.FailedRecovery(ctx, limit)
}

func (service *Service) InspectRecovery(ctx context.Context, session auth.Session, transferID string) ([]recovery.WorkItem, error) {
	if err := requireAdmin(session); err != nil {
		return nil, err
	}
	if transferID == "" {
		return nil, ErrInvalid
	}
	return service.store.InspectRecovery(ctx, transferID)
}

func (service *Service) RetryRecovery(ctx context.Context, session auth.Session, transferID string) error {
	if err := requireAdmin(session); err != nil {
		return err
	}
	if transferID == "" {
		return ErrInvalid
	}
	return service.store.RetryRecovery(ctx, transferID, service.now().UTC())
}

func (service *Service) Resume(ctx context.Context, item recovery.WorkItem) (recovery.Outcome, error) {
	return service.store.ReconcileRecovery(ctx, item, service.storageRoot, service.now().UTC())
}

func (service *Service) removeStoredPath(value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	path := filepath.Clean(value)
	relative, err := filepath.Rel(service.storageRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ErrForbidden
	}
	return os.Remove(path)
}

func dereferenceTime(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return *value
}

func SortSelections(values []FolderSelection) {
	sort.SliceStable(values, func(i, j int) bool { return values[i].EntryIndex < values[j].EntryIndex })
}
