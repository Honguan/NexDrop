package transfer

import (
	"context"
	"errors"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/domain"
	"nexdrop/internal/monitoring"
	"nexdrop/internal/version"
)

type fakeStore struct {
	resolved []string
	prepared Prepared
	progress Progress
	timeline []TimelineEvent
	report   TimelineEventReport
}

func (store *fakeStore) ResolveTransferTargets(context.Context, auth.Session, TargetType, string, []string) ([]string, error) {
	return store.resolved, nil
}
func (store *fakeStore) CreateTransfer(_ context.Context, _ auth.Session, prepared Prepared) (Transfer, error) {
	store.prepared = prepared
	return Transfer{ID: "transfer-1", Targets: prepared.Targets, FileTargets: prepared.FileTargets, Status: prepared.Status}, nil
}
func (*fakeStore) ListTransfers(context.Context, auth.Session) ([]Transfer, error) { return nil, nil }
func (*fakeStore) GetTransfer(context.Context, auth.Session, string) (Transfer, error) {
	return Transfer{}, nil
}
func (*fakeStore) CancelTransfer(context.Context, auth.Session, string, time.Time) (Transfer, error) {
	return Transfer{}, nil
}

func (*fakeStore) HideTransfer(context.Context, auth.Session, string, time.Time) error {
	return nil
}
func (*fakeStore) ReadTransfer(context.Context, auth.Session, string, time.Time) (Transfer, error) {
	return Transfer{}, nil
}
func (store *fakeStore) ReportTransferProgress(_ context.Context, _ auth.Session, id string, progress Progress, _ time.Time) (Transfer, error) {
	store.progress = progress
	return Transfer{ID: id, Status: progress.Status}, nil
}
func (store *fakeStore) TransferTimeline(context.Context, auth.Session, string) ([]TimelineEvent, error) {
	return store.timeline, nil
}
func (store *fakeStore) ReportTransferTimelineEvent(_ context.Context, _ auth.Session, transferID string, report TimelineEventReport, occurredAt time.Time) (TimelineEvent, error) {
	store.report = report
	durationMillis := int64(0)
	if report.Code == EventTLSAuthenticated || report.Code == EventFallbackSelected {
		durationMillis = 250
	}
	return TimelineEvent{
		Sequence: 1, TransferID: transferID, Code: report.Code, FileID: report.FileID,
		TargetDeviceID: report.TargetDeviceID, ExecutionID: report.ExecutionID,
		Route: report.Route, ErrorCode: report.ErrorCode, DurationMillis: durationMillis, OccurredAt: occurredAt,
	}, nil
}

func TestCreateTextUsesLANBeforeNode(t *testing.T) {
	store := &fakeStore{resolved: []string{"lan-device", "remote-device"}}
	service := NewService(store)
	deviceID := "sender-device"
	result, err := service.Create(context.Background(), auth.Session{DeviceID: &deviceID}, Request{
		TargetType: TargetMultiple, TargetDeviceIDs: []string{"lan-device", "remote-device"},
		LANAvailableDeviceIDs: []string{"lan-device"}, ContentType: ContentText, Content: []byte("hello"),
		WrappedContentKeys: map[string][]byte{"lan-device": {1}, "remote-device": {2}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Targets[0].SelectedRoute != domain.SelectedRouteLAN || result.Targets[1].SelectedRoute != domain.SelectedRouteNode {
		t.Fatalf("targets = %+v", result.Targets)
	}
}

func TestCreateFilesRoutesEachFileIndependently(t *testing.T) {
	store := &fakeStore{resolved: []string{"device-1"}}
	service := NewService(store)
	deviceID := "sender-device"
	result, err := service.Create(context.Background(), auth.Session{DeviceID: &deviceID}, Request{
		TargetType: TargetSingle, TargetDeviceIDs: []string{"device-1"}, ContentType: ContentFile,
		Content:            []byte("encrypted file metadata"),
		WrappedContentKeys: map[string][]byte{"device-1": {1}},
		Files: []File{
			{Name: "small.bin", Size: 1, SHA256: make([]byte, 32), ChunkSize: int(8 * 1024 * 1024), ChunkCount: 1},
			{Name: "large.bin", Size: domain.DefaultLargeFileThreshold + 1, SHA256: make([]byte, 32), ChunkSize: int(8 * 1024 * 1024), ChunkCount: 13},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.FileTargets) != 2 || result.FileTargets[0].SelectedRoute != domain.SelectedRouteNode || result.FileTargets[1].SelectedRoute != domain.SelectedRouteWaitingLAN {
		t.Fatalf("file targets = %+v", result.FileTargets)
	}
	if result.Targets[0].SelectedRoute != domain.SelectedRouteMixed {
		t.Fatalf("target route = %q, want MIXED", result.Targets[0].SelectedRoute)
	}
	if string(store.prepared.Content) != "encrypted file metadata" {
		t.Fatal("encrypted file metadata was not preserved")
	}
}

func TestCreateRejectsInvalidPayloads(t *testing.T) {
	service := NewService(&fakeStore{resolved: []string{"device-1"}})
	tests := []Request{
		{TargetType: TargetSingle, ContentType: ContentText, Content: []byte("text")},
		{TargetType: TargetSingle, TargetDeviceIDs: []string{"device-1"}, ContentType: ContentText},
		{TargetType: TargetSingle, TargetDeviceIDs: []string{"device-1"}, ContentType: ContentFile, Files: []File{{Name: "bad"}}},
		{TargetType: TargetGroupAll, GroupID: "", ContentType: ContentText, Content: []byte("text")},
		{TargetType: TargetSingle, TargetDeviceIDs: []string{"device-1"}, ContentType: ContentFile, Files: []File{{Name: "../secret.txt", SHA256: make([]byte, 32), ChunkSize: 1, ChunkCount: 0}}},
	}
	deviceID := "sender-device"
	for index, request := range tests {
		if _, err := service.Create(context.Background(), auth.Session{DeviceID: &deviceID}, request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("test %d error = %v, want ErrInvalid", index, err)
		}
	}
}

func TestCreateRejectsMoreThanAdvertisedRecipients(t *testing.T) {
	recipients := make([]string, version.CurrentLimits().MaxRecipients+1)
	keys := make(map[string][]byte, len(recipients))
	for index := range recipients {
		recipients[index] = fmt.Sprintf("device-%03d", index)
		keys[recipients[index]] = []byte{1}
	}
	service := NewService(&fakeStore{resolved: recipients})
	deviceID := "sender-device"

	_, err := service.Create(context.Background(), auth.Session{DeviceID: &deviceID}, Request{
		TargetType: TargetAllDevices, ContentType: ContentText, Content: []byte("text"), WrappedContentKeys: keys,
	})

	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("recipient limit error = %v, want ErrInvalid", err)
	}
}

func TestReportProgressValidatesClientState(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	deviceID := "sender-device"
	result, err := service.ReportProgress(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", Progress{
		IdempotencyKey: "11111111-1111-1111-1111-111111111111",
		DeviceID:       "target-device", Status: domain.TransferTransferringLAN, Route: domain.SelectedRouteLAN, BytesTransferred: 42,
	})
	if err != nil || result.Status != domain.TransferTransferringLAN || store.progress.BytesTransferred != 42 || store.progress.IdempotencyKey == "" {
		t.Fatalf("ReportProgress() = %+v, %v; stored = %+v", result, err, store.progress)
	}
	if _, err := service.ReportProgress(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", Progress{DeviceID: "target-device", Status: domain.TransferCancelled}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cancel report error = %v, want ErrInvalid", err)
	}
}

func TestTimelineReturnsStableContentFreeEvents(t *testing.T) {
	occurredAt := time.Date(2026, 7, 30, 1, 2, 3, 0, time.UTC)
	store := &fakeStore{timeline: []TimelineEvent{{
		Sequence: 1, Code: EventTaskCreated, Status: domain.TransferCheckingRoute, OccurredAt: occurredAt,
	}}}
	service := NewService(store)

	events, err := service.Timeline(context.Background(), auth.Session{User: auth.User{ID: "user-1"}}, "transfer-1")

	if err != nil || len(events) != 1 {
		t.Fatalf("Timeline() = %+v, %v", events, err)
	}
	if events[0].Code != EventTaskCreated || !events[0].Code.Valid() {
		t.Fatalf("timeline event code = %q", events[0].Code)
	}
	if _, ok := EventCodeForStatus(domain.TransferDelivered); !ok {
		t.Fatal("DELIVERED status has no stable timeline event code")
	}
	if _, ok := EventCodeForStatus(domain.TransferWaitingForLAN); !ok {
		t.Fatal("WAITING_LAN status has no stable timeline event code")
	}
}

func TestReportTimelineEventAllowsOnlyStableClientPhases(t *testing.T) {
	monitoring.DefaultRegistry = monitoring.NewRegistry()
	store := &fakeStore{}
	service := NewService(store)
	deviceID := "sender-device"
	event, err := service.ReportTimelineEvent(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", TimelineEventReport{
		IdempotencyKey: "11111111-1111-1111-1111-111111111111",
		Code:           EventDirectAttempted, TargetDeviceID: "target-device", Route: domain.SelectedRouteLAN,
	})
	if err != nil || event.Code != EventDirectAttempted || store.report.Code != EventDirectAttempted {
		t.Fatalf("ReportTimelineEvent() = %+v, %v; stored = %+v", event, err, store.report)
	}
	if _, err := service.ReportTimelineEvent(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", TimelineEventReport{
		IdempotencyKey: "22222222-2222-4222-8222-222222222222",
		Code:           EventRoutesDiscovered, TargetDeviceID: "target-device", Route: domain.SelectedRouteMixed,
	}); err != nil {
		t.Fatalf("mixed route timeline error = %v", err)
	}
	for index, code := range []EventCode{
		EventDirectAttempted, EventTLSAuthenticated, EventFallbackSelected, EventEncryptionReady,
		EventChunkUpload, EventChunkDownload, EventChunkRetry, EventWSInterrupted,
		EventReceiverLimited, EventNetworkChanged,
	} {
		if _, err := service.ReportTimelineEvent(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", TimelineEventReport{
			IdempotencyKey: fmt.Sprintf("33333333-3333-4333-8333-%012d", index),
			Code:           code, TargetDeviceID: "target-device", Route: domain.SelectedRouteLAN,
		}); err != nil {
			t.Fatalf("client event %q error = %v", code, err)
		}
	}
	response := httptest.NewRecorder()
	monitoring.DefaultRegistry.ServeHTTP(response, httptest.NewRequest("GET", "/metrics", nil))
	metrics := response.Body.String()
	for _, name := range []string{"nexdrop_direct_handshake_seconds_sum", "nexdrop_chunk_retry_total"} {
		if !strings.Contains(metrics, name) {
			t.Fatalf("metrics do not contain %q: %s", name, metrics)
		}
	}
	for _, code := range []EventCode{EventTaskCreated, EventTargetDelivered, EventRetryStarted} {
		_, err := service.ReportTimelineEvent(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", TimelineEventReport{
			IdempotencyKey: "11111111-1111-1111-1111-111111111111",
			Code:           code, TargetDeviceID: "target-device",
		})
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("server-owned event %q error = %v, want ErrInvalid", code, err)
		}
	}
}
