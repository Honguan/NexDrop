package analytics

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/domain"
)

type fakeStore struct {
	inserted       []Metric
	idempotencyKey string
}

func (store *fakeStore) InsertMetrics(_ context.Context, _ auth.Session, metrics []Metric) (BatchResult, error) {
	store.inserted = metrics
	return BatchResult{Accepted: len(metrics)}, nil
}
func (store *fakeStore) InsertMetricsIdempotent(_ context.Context, _ auth.Session, key string, _ []byte, metrics []Metric) (BatchResult, error) {
	store.idempotencyKey = key
	store.inserted = metrics
	return BatchResult{Accepted: len(metrics)}, nil
}
func (*fakeStore) AnalyticsOverview(context.Context, auth.Session, TimeRange) (Overview, error) {
	return Overview{}, nil
}
func (*fakeStore) DailyTransfers(context.Context, auth.Session, TimeRange) ([]DailyTransfer, error) {
	return nil, nil
}
func (*fakeStore) DeviceStatistics(context.Context, auth.Session, TimeRange) ([]DeviceStatistic, error) {
	return nil, nil
}
func (*fakeStore) GroupStatistics(context.Context, auth.Session, TimeRange) ([]GroupStatistic, error) {
	return nil, nil
}
func (*fakeStore) NodeStatistics(context.Context, auth.Session, TimeRange) ([]NodeMetric, error) {
	return nil, nil
}

func TestUploadValidatesAndAcceptsMetric(t *testing.T) {
	deviceID := "11111111-1111-1111-1111-111111111111"
	store := &fakeStore{}
	service := NewService(store)
	now := time.Now().UTC()
	result, err := service.Upload(context.Background(), auth.Session{DeviceID: &deviceID}, []Metric{{
		EventID: "22222222-2222-2222-2222-222222222222", TransferID: "33333333-3333-3333-3333-333333333333",
		SenderDeviceID: deviceID, ContentType: "FILE", Route: domain.SelectedRouteLAN, FileSize: 10, StartedAt: now,
	}})
	if err != nil || result.Accepted != 1 || len(store.inserted) != 1 {
		t.Fatalf("Upload() = %+v, %v", result, err)
	}
}

func TestUploadRejectsSpoofedSender(t *testing.T) {
	deviceID := "11111111-1111-1111-1111-111111111111"
	service := NewService(&fakeStore{})
	_, err := service.Upload(context.Background(), auth.Session{DeviceID: &deviceID}, []Metric{{
		EventID: "22222222-2222-2222-2222-222222222222", TransferID: "33333333-3333-3333-3333-333333333333",
		SenderDeviceID: "44444444-4444-4444-4444-444444444444", ContentType: "FILE", Route: domain.SelectedRouteNode, StartedAt: time.Now(),
	}})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("Upload() error = %v, want ErrInvalid", err)
	}
}

func TestUploadIdempotentUsesDurableStore(t *testing.T) {
	deviceID := "11111111-1111-1111-1111-111111111111"
	store := &fakeStore{}
	service := NewService(store)
	metrics := []Metric{{
		EventID: "22222222-2222-2222-2222-222222222222", TransferID: "33333333-3333-3333-3333-333333333333",
		SenderDeviceID: deviceID, ContentType: "FILE", Route: domain.SelectedRouteLAN, StartedAt: time.Now().UTC(),
	}}
	result, err := service.UploadIdempotent(context.Background(), auth.Session{DeviceID: &deviceID}, "44444444-4444-4444-4444-444444444444", []byte("hash"), metrics)
	if err != nil || result.Accepted != 1 || store.idempotencyKey == "" {
		t.Fatalf("UploadIdempotent() = %+v, %v; key = %q", result, err, store.idempotencyKey)
	}
}

func TestNodeStatisticsRequiresAdmin(t *testing.T) {
	service := NewService(&fakeStore{})
	_, err := service.Node(context.Background(), auth.Session{}, TimeRange{From: time.Now().Add(-time.Hour), To: time.Now()})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("Node() error = %v, want ErrForbidden", err)
	}
}
