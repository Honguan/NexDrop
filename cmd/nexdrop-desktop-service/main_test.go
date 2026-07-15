package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"nexdrop/internal/nativebridge"
)

func TestSpoolQueueWritesValidatedPayload(t *testing.T) {
	directory := t.TempDir()
	id, err := (spoolQueue{directory}).Enqueue(context.Background(), nativebridge.SharePayload{
		Kind: "LINK",
		URL:  "https://example.com",
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(directory, id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	var payload nativebridge.SharePayload
	if json.Unmarshal(content, &payload) != nil || payload.URL != "https://example.com" {
		t.Fatalf("payload = %s", content)
	}
}

func TestDesktopStatusDefaultsToOnline(t *testing.T) {
	value, err := (desktopStatus{filepath.Join(t.TempDir(), "missing.json")}).Status(context.Background())
	if err != nil || !json.Valid(value) {
		t.Fatalf("Status() = %s, %v", value, err)
	}
}
