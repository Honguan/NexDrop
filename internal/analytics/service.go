package analytics

import (
	"context"
	"errors"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/domain"
)

var (
	ErrInvalid   = errors.New("invalid analytics request")
	ErrForbidden = errors.New("analytics operation forbidden")
	ErrConflict  = errors.New("analytics idempotency conflict")
)

const MaximumBatchSize = 500

type Metric struct {
	EventID               string               `json:"eventId"`
	TransferID            string               `json:"transferId"`
	SenderDeviceID        string               `json:"senderDeviceId"`
	ReceiverDeviceID      string               `json:"receiverDeviceId,omitempty"`
	GroupID               string               `json:"groupId,omitempty"`
	ContentType           string               `json:"contentType"`
	Route                 domain.SelectedRoute `json:"route"`
	FileSize              int64                `json:"fileSize"`
	StartedAt             time.Time            `json:"startedAt"`
	CompletedAt           *time.Time           `json:"completedAt,omitempty"`
	AverageBytesPerSecond int64                `json:"averageBytesPerSecond"`
	RetryCount            int                  `json:"retryCount"`
	Succeeded             bool                 `json:"succeeded"`
	ErrorCode             string               `json:"errorCode,omitempty"`
}

type BatchResult struct {
	Accepted   int `json:"accepted"`
	Duplicates int `json:"duplicates"`
}

type TimeRange struct {
	From time.Time
	To   time.Time
}

type Overview struct {
	TransferCount int64            `json:"transferCount"`
	TotalBytes    int64            `json:"totalBytes"`
	Succeeded     int64            `json:"succeeded"`
	Failed        int64            `json:"failed"`
	RouteCounts   map[string]int64 `json:"routeCounts"`
	RouteBytes    map[string]int64 `json:"routeBytes"`
}

type DailyTransfer struct {
	Date       string `json:"date"`
	Count      int64  `json:"count"`
	TotalBytes int64  `json:"totalBytes"`
	LANBytes   int64  `json:"lanBytes"`
	NodeBytes  int64  `json:"nodeBytes"`
	Failed     int64  `json:"failed"`
}

type DeviceStatistic struct {
	DeviceID      string     `json:"deviceId"`
	DisplayName   string     `json:"displayName"`
	DeviceType    string     `json:"deviceType"`
	TrustStatus   string     `json:"trustStatus"`
	Online        bool       `json:"online"`
	LastSeenAt    *time.Time `json:"lastSeenAt,omitempty"`
	SentCount     int64      `json:"sentCount"`
	ReceivedCount int64      `json:"receivedCount"`
	SentBytes     int64      `json:"sentBytes"`
	ReceivedBytes int64      `json:"receivedBytes"`
	AverageSpeed  float64    `json:"averageBytesPerSecond"`
}

type GroupStatistic struct {
	GroupID       string `json:"groupId"`
	Name          string `json:"name"`
	MessageCount  int64  `json:"messageCount"`
	FileCount     int64  `json:"fileCount"`
	TransferBytes int64  `json:"transferBytes"`
	ActiveDevices int64  `json:"activeDevices"`
	ActiveUsers   int64  `json:"activeUsers"`
}

type NodeMetric struct {
	RecordedAt           time.Time `json:"recordedAt"`
	CPUPercent           float32   `json:"cpuPercent"`
	MemoryBytes          int64     `json:"memoryBytes"`
	DiskBytes            int64     `json:"diskBytes"`
	CacheBytes           int64     `json:"cacheBytes"`
	NetworkUploadBytes   int64     `json:"networkUploadBytes"`
	NetworkDownloadBytes int64     `json:"networkDownloadBytes"`
	OnlineDevices        int       `json:"onlineDevices"`
	ActiveTransfers      int       `json:"activeTransfers"`
}

type Store interface {
	InsertMetrics(context.Context, auth.Session, []Metric) (BatchResult, error)
	AnalyticsOverview(context.Context, auth.Session, TimeRange) (Overview, error)
	DailyTransfers(context.Context, auth.Session, TimeRange) ([]DailyTransfer, error)
	DeviceStatistics(context.Context, auth.Session, TimeRange) ([]DeviceStatistic, error)
	GroupStatistics(context.Context, auth.Session, TimeRange) ([]GroupStatistic, error)
	NodeStatistics(context.Context, auth.Session, TimeRange) ([]NodeMetric, error)
}

type Service struct{ store Store }

type IdempotentStore interface {
	InsertMetricsIdempotent(context.Context, auth.Session, string, []byte, []Metric) (BatchResult, error)
}

func NewService(store Store) *Service { return &Service{store: store} }

func (service *Service) Upload(ctx context.Context, session auth.Session, metrics []Metric) (BatchResult, error) {
	return service.UploadIdempotent(ctx, session, "", nil, metrics)
}

func (service *Service) UploadIdempotent(ctx context.Context, session auth.Session, key string, fingerprint []byte, metrics []Metric) (BatchResult, error) {
	if session.DeviceID == nil || len(metrics) == 0 || len(metrics) > MaximumBatchSize {
		return BatchResult{}, ErrInvalid
	}
	for _, metric := range metrics {
		if !validMetric(metric, *session.DeviceID) {
			return BatchResult{}, ErrInvalid
		}
	}
	if key != "" {
		if store, ok := service.store.(IdempotentStore); ok {
			return store.InsertMetricsIdempotent(ctx, session, key, fingerprint, metrics)
		}
	}
	return service.store.InsertMetrics(ctx, session, metrics)
}

func (service *Service) Overview(ctx context.Context, session auth.Session, timeRange TimeRange) (Overview, error) {
	if !validRange(timeRange) {
		return Overview{}, ErrInvalid
	}
	return service.store.AnalyticsOverview(ctx, session, utcRange(timeRange))
}

func (service *Service) Transfers(ctx context.Context, session auth.Session, timeRange TimeRange) ([]DailyTransfer, error) {
	if !validRange(timeRange) {
		return nil, ErrInvalid
	}
	return service.store.DailyTransfers(ctx, session, utcRange(timeRange))
}

func (service *Service) Devices(ctx context.Context, session auth.Session, timeRange TimeRange) ([]DeviceStatistic, error) {
	if !validRange(timeRange) {
		return nil, ErrInvalid
	}
	return service.store.DeviceStatistics(ctx, session, utcRange(timeRange))
}

func (service *Service) Groups(ctx context.Context, session auth.Session, timeRange TimeRange) ([]GroupStatistic, error) {
	if !validRange(timeRange) {
		return nil, ErrInvalid
	}
	return service.store.GroupStatistics(ctx, session, utcRange(timeRange))
}

func (service *Service) Node(ctx context.Context, session auth.Session, timeRange TimeRange) ([]NodeMetric, error) {
	if !session.Admin {
		return nil, ErrForbidden
	}
	if !validRange(timeRange) {
		return nil, ErrInvalid
	}
	return service.store.NodeStatistics(ctx, session, utcRange(timeRange))
}

func validMetric(metric Metric, senderDeviceID string) bool {
	if !isUUID(metric.EventID) || !isUUID(metric.TransferID) || metric.SenderDeviceID != senderDeviceID {
		return false
	}
	if metric.ReceiverDeviceID != "" && !isUUID(metric.ReceiverDeviceID) {
		return false
	}
	if metric.GroupID != "" && !isUUID(metric.GroupID) {
		return false
	}
	if metric.ContentType == "" || metric.FileSize < 0 || metric.RetryCount < 0 || metric.AverageBytesPerSecond < 0 || metric.StartedAt.IsZero() {
		return false
	}
	if metric.CompletedAt != nil && metric.CompletedAt.Before(metric.StartedAt) {
		return false
	}
	switch metric.Route {
	case domain.SelectedRouteLAN, domain.SelectedRouteNode, domain.SelectedRouteWaitingLAN:
		return true
	default:
		return false
	}
}

func validRange(value TimeRange) bool {
	return !value.From.IsZero() && !value.To.IsZero() && value.From.Before(value.To) && value.To.Sub(value.From) <= 366*24*time.Hour
}

func utcRange(value TimeRange) TimeRange {
	return TimeRange{From: value.From.UTC(), To: value.To.UTC()}
}

func isUUID(value string) bool {
	if len(value) != 36 {
		return false
	}
	for index, character := range value {
		if index == 8 || index == 13 || index == 18 || index == 23 {
			if character != '-' {
				return false
			}
			continue
		}
		if !((character >= '0' && character <= '9') || (character >= 'a' && character <= 'f') || (character >= 'A' && character <= 'F')) {
			return false
		}
	}
	return true
}
