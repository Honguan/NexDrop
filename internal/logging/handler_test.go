package logging

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestJSONHandlerUsesUTCAndRedactsSensitiveValues(t *testing.T) {
	var output bytes.Buffer
	handler := NewJSONHandler(&output, slog.LevelInfo)
	record := slog.NewRecord(
		time.Date(2026, 7, 17, 12, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60)),
		slog.LevelError,
		"request failed",
		0,
	)
	record.AddAttrs(
		slog.String("module", "api"),
		slog.String("access_token", "access-secret"),
		slog.Any("error", errors.New("connect postgres://nexdrop:database-secret@db/nexdrop Authorization=Bearer bearer-secret")),
	)
	if err := handler.Handle(context.Background(), record); err != nil {
		t.Fatal(err)
	}

	var value map[string]any
	if err := json.Unmarshal(output.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	if value[slog.TimeKey] != "2026-07-17T04:00:00Z" {
		t.Fatalf("time = %v", value[slog.TimeKey])
	}
	if value[slog.LevelKey] != "ERROR" || value["module"] != "api" {
		t.Fatalf("level/module = %v/%v", value[slog.LevelKey], value["module"])
	}
	encoded := output.String()
	for _, secret := range []string{"access-secret", "database-secret", "bearer-secret"} {
		if strings.Contains(encoded, secret) {
			t.Fatalf("log contains sensitive value %q: %s", secret, encoded)
		}
	}
	if value["access_token"] != Redacted {
		t.Fatalf("access_token = %v", value["access_token"])
	}
}
