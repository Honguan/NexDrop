package transfer

import (
	"context"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/domain"
)

type EventCode string

const (
	EventTaskCreated       EventCode = "TASK_CREATED"
	EventTargetResolved    EventCode = "TARGET_RESOLVED"
	EventRouteChecking     EventCode = "ROUTE_CHECKING"
	EventTargetWaiting     EventCode = "TARGET_WAITING"
	EventTargetQueued      EventCode = "TARGET_QUEUED"
	EventNodeUploading     EventCode = "NODE_UPLOAD_STARTED"
	EventNodeAvailable     EventCode = "NODE_FILE_AVAILABLE"
	EventNodeDownloading   EventCode = "NODE_DOWNLOAD_STARTED"
	EventLANTransferring   EventCode = "LAN_TRANSFER_STARTED"
	EventTransferPaused    EventCode = "TRANSFER_PAUSED"
	EventHashVerifying     EventCode = "FILE_HASH_VERIFYING"
	EventHashVerified      EventCode = "FILE_HASH_VERIFIED"
	EventTargetDelivered   EventCode = "TARGET_DELIVERED"
	EventTargetRead        EventCode = "TARGET_READ"
	EventTargetFailed      EventCode = "TARGET_FAILED"
	EventTargetCancelled   EventCode = "TARGET_CANCELLED"
	EventTargetExpired     EventCode = "TARGET_EXPIRED"
	EventSourceFileMissing EventCode = "SOURCE_FILE_MISSING"
	EventSourceFileChanged EventCode = "SOURCE_FILE_CHANGED"
	EventRouteMigrated     EventCode = "ROUTE_MIGRATED"
	EventRetryStarted      EventCode = "RETRY_STARTED"
	EventRoutesDiscovered  EventCode = "ROUTE_CANDIDATES_DISCOVERED"
	EventDirectAttempted   EventCode = "DIRECT_CONNECTION_ATTEMPTED"
	EventTLSAuthenticated  EventCode = "TLS_AUTHENTICATION_COMPLETED"
	EventFallbackSelected  EventCode = "ROUTE_FALLBACK_SELECTED"
	EventEncryptionReady   EventCode = "ENCRYPTION_PREPARED"
	EventChunkUpload       EventCode = "CHUNK_UPLOAD_STARTED"
	EventChunkDownload     EventCode = "CHUNK_DOWNLOAD_STARTED"
	EventChunkRetry        EventCode = "CHUNK_RETRY_STARTED"
	EventWSInterrupted     EventCode = "WEBSOCKET_INTERRUPTED"
	EventReceiverLimited   EventCode = "RECEIVER_BACKGROUND_RESTRICTED"
	EventNetworkChanged    EventCode = "NETWORK_INTERFACE_CHANGED"
)

var validEventCodes = map[EventCode]struct{}{
	EventTaskCreated: {}, EventTargetResolved: {}, EventRouteChecking: {}, EventTargetWaiting: {}, EventTargetQueued: {},
	EventNodeUploading: {}, EventNodeAvailable: {}, EventNodeDownloading: {}, EventLANTransferring: {},
	EventTransferPaused: {}, EventHashVerifying: {}, EventHashVerified: {}, EventTargetDelivered: {}, EventTargetRead: {},
	EventTargetFailed: {}, EventTargetCancelled: {}, EventTargetExpired: {}, EventSourceFileMissing: {},
	EventSourceFileChanged: {}, EventRouteMigrated: {}, EventRetryStarted: {},
	EventRoutesDiscovered: {}, EventDirectAttempted: {}, EventTLSAuthenticated: {}, EventFallbackSelected: {},
	EventEncryptionReady: {}, EventChunkUpload: {}, EventChunkDownload: {}, EventChunkRetry: {},
	EventWSInterrupted: {}, EventReceiverLimited: {}, EventNetworkChanged: {},
}

var clientReportableEventCodes = map[EventCode]struct{}{
	EventRoutesDiscovered: {}, EventDirectAttempted: {}, EventTLSAuthenticated: {}, EventFallbackSelected: {},
	EventEncryptionReady: {}, EventChunkUpload: {}, EventChunkDownload: {}, EventChunkRetry: {},
	EventWSInterrupted: {}, EventReceiverLimited: {}, EventNetworkChanged: {},
}

func (code EventCode) Valid() bool {
	_, ok := validEventCodes[code]
	return ok
}

func (code EventCode) ClientReportable() bool {
	_, ok := clientReportableEventCodes[code]
	return ok
}

func EventCodeForStatus(status domain.TransferStatus) (EventCode, bool) {
	code, ok := map[domain.TransferStatus]EventCode{
		domain.TransferCreated:           EventTaskCreated,
		domain.TransferCheckingRoute:     EventRouteChecking,
		domain.TransferWaitingForTarget:  EventTargetWaiting,
		domain.TransferWaitingForNode:    EventTargetWaiting,
		domain.TransferWaitingForLAN:     EventTargetWaiting,
		domain.TransferQueued:            EventTargetQueued,
		domain.TransferUploadingToNode:   EventNodeUploading,
		domain.TransferAvailableOnNode:   EventNodeAvailable,
		domain.TransferDownloading:       EventNodeDownloading,
		domain.TransferTransferringLAN:   EventLANTransferring,
		domain.TransferPaused:            EventTransferPaused,
		domain.TransferVerifying:         EventHashVerifying,
		domain.TransferDelivered:         EventTargetDelivered,
		domain.TransferRead:              EventTargetRead,
		domain.TransferFailed:            EventTargetFailed,
		domain.TransferCancelled:         EventTargetCancelled,
		domain.TransferExpired:           EventTargetExpired,
		domain.TransferSourceFileMissing: EventSourceFileMissing,
		domain.TransferSourceFileChanged: EventSourceFileChanged,
	}[status]
	return code, ok
}

type TimelineEvent struct {
	Sequence       int64                 `json:"sequence"`
	TransferID     string                `json:"transferId,omitempty"`
	Code           EventCode             `json:"code"`
	RequestID      string                `json:"requestId,omitempty"`
	FileID         string                `json:"fileId,omitempty"`
	TargetDeviceID string                `json:"targetDeviceId,omitempty"`
	ExecutionID    string                `json:"executionId,omitempty"`
	Route          domain.SelectedRoute  `json:"route,omitempty"`
	Status         domain.TransferStatus `json:"status,omitempty"`
	ErrorCode      string                `json:"errorCode,omitempty"`
	DurationMillis int64                 `json:"durationMillis,omitempty"`
	OccurredAt     time.Time             `json:"occurredAt"`
}

type TimelineStore interface {
	TransferTimeline(context.Context, auth.Session, string) ([]TimelineEvent, error)
}

type TimelineEventReport struct {
	IdempotencyKey string               `json:"-"`
	Code           EventCode            `json:"code"`
	FileID         string               `json:"fileId,omitempty"`
	TargetDeviceID string               `json:"targetDeviceId"`
	ExecutionID    string               `json:"executionId,omitempty"`
	Route          domain.SelectedRoute `json:"route,omitempty"`
	ErrorCode      string               `json:"errorCode,omitempty"`
}

type TimelineEventReporter interface {
	ReportTransferTimelineEvent(context.Context, auth.Session, string, TimelineEventReport, time.Time) (TimelineEvent, error)
}
