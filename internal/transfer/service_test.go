package transfer

import (
	"context"
	"errors"
	"testing"
	"time"

	"nexdrop/internal/auth"
	"nexdrop/internal/domain"
)

type fakeStore struct {
	resolved []string
	prepared Prepared
	progress Progress
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
func (*fakeStore) ReadTransfer(context.Context, auth.Session, string, time.Time) (Transfer, error) {
	return Transfer{}, nil
}
func (store *fakeStore) ReportTransferProgress(_ context.Context, _ auth.Session, id string, progress Progress, _ time.Time) (Transfer, error) {
	store.progress = progress
	return Transfer{ID: id, Status: progress.Status}, nil
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
	}
	deviceID := "sender-device"
	for index, request := range tests {
		if _, err := service.Create(context.Background(), auth.Session{DeviceID: &deviceID}, request); !errors.Is(err, ErrInvalid) {
			t.Fatalf("test %d error = %v, want ErrInvalid", index, err)
		}
	}
}

func TestReportProgressValidatesClientState(t *testing.T) {
	store := &fakeStore{}
	service := NewService(store)
	deviceID := "sender-device"
	result, err := service.ReportProgress(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", Progress{
		DeviceID: "target-device", Status: domain.TransferTransferringLAN, Route: domain.SelectedRouteLAN, BytesTransferred: 42,
	})
	if err != nil || result.Status != domain.TransferTransferringLAN || store.progress.BytesTransferred != 42 {
		t.Fatalf("ReportProgress() = %+v, %v; stored = %+v", result, err, store.progress)
	}
	if _, err := service.ReportProgress(context.Background(), auth.Session{DeviceID: &deviceID}, "transfer-1", Progress{DeviceID: "target-device", Status: domain.TransferCancelled}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("cancel report error = %v, want ErrInvalid", err)
	}
}
