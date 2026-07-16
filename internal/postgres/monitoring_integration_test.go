package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"nexdrop/internal/analytics"
	"nexdrop/internal/auth"
)

func TestSystemMonitoringIntegration(t *testing.T) {
	databaseURL := os.Getenv("NEXDROP_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("NEXDROP_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	store, err := Open(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	recordedAt := time.Now().UTC().Truncate(time.Microsecond)
	defer func() { _, _ = store.pool.Exec(ctx, `DELETE FROM system_metrics WHERE recorded_at=$1`, recordedAt) }()
	metric := analytics.NodeMetric{RecordedAt: recordedAt, CPUPercent: 12.5, MemoryBytes: 100, DiskBytes: 200, CacheBytes: 300, NetworkUploadBytes: 400, NetworkDownloadBytes: 500}
	if err := store.RecordSystemMetric(ctx, metric); err != nil {
		t.Fatal(err)
	}
	result, err := store.NodeStatistics(ctx, auth.Session{User: auth.User{Admin: true}}, analytics.TimeRange{From: recordedAt.Add(-time.Second), To: recordedAt.Add(time.Second)})
	if err != nil {
		t.Fatalf("NodeStatistics() = %+v, %v", result, err)
	}
	for _, item := range result {
		if item.RecordedAt.Equal(recordedAt) {
			if item.CPUPercent != metric.CPUPercent || item.CacheBytes != metric.CacheBytes {
				t.Fatalf("metric = %+v", item)
			}
			return
		}
	}
	t.Fatalf("recorded metric not found in %+v", result)
}
