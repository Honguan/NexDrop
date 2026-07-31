package relay

import (
	"testing"
	"time"
)

func TestSelectExcludesDrainingAndFullRelays(t *testing.T) {
	selected, err := Select([]Relay{
		{ID: "draining", Endpoint: "https://a", Healthy: true, Draining: true, CapacityBytes: 1000},
		{ID: "full", Endpoint: "https://b", Healthy: true, CapacityBytes: 1000, UsedBytes: 950},
		{ID: "ready", Endpoint: "https://c", Healthy: true, Region: "tw", CapacityBytes: 1000, UsedBytes: 100, Latency: 10 * time.Millisecond},
	}, 100, "tw")
	if err != nil || selected.ID != "ready" {
		t.Fatalf("unexpected relay: %#v %v", selected, err)
	}
}

func TestGrantIsBoundToRelayTransferFileOperationAndSize(t *testing.T) {
	signer, err := NewGrantSigner([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatal(err)
	}
	token, err := signer.Issue("relay-a", "transfer-a", "file-a", OperationUpload, 100, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := signer.Verify(token, "relay-a", "transfer-a", "file-a", OperationUpload, 100); err != nil {
		t.Fatalf("expected valid grant: %v", err)
	}
	if _, err := signer.Verify(token, "relay-b", "transfer-a", "file-a", OperationUpload, 100); err != ErrInvalidGrant {
		t.Fatalf("expected relay binding failure, got %v", err)
	}
	if _, err := signer.Verify(token, "relay-a", "transfer-a", "file-a", OperationUpload, 101); err != ErrInvalidGrant {
		t.Fatalf("expected size binding failure, got %v", err)
	}
}

func TestExpiredGrantIsRejected(t *testing.T) {
	signer, _ := NewGrantSigner([]byte("0123456789abcdef0123456789abcdef"))
	base := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	signer.now = func() time.Time { return base }
	token, _ := signer.Issue("relay", "transfer", "file", OperationDownload, 100, time.Second)
	signer.now = func() time.Time { return base.Add(2 * time.Second) }
	if _, err := signer.Verify(token, "relay", "transfer", "file", OperationDownload, 1); err != ErrExpiredGrant {
		t.Fatalf("expected expired grant, got %v", err)
	}
}
