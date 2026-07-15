package monitoring

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"nexdrop/internal/analytics"
)

type fakeSampler struct {
	sample Sample
	err    error
}

func (sampler fakeSampler) Sample(string) (Sample, error) { return sampler.sample, sampler.err }

type fakeStore struct{ metric analytics.NodeMetric }

func (store *fakeStore) RecordSystemMetric(_ context.Context, metric analytics.NodeMetric) error {
	store.metric = metric
	return nil
}

func TestCollectorRecordsResourceSample(t *testing.T) {
	store := &fakeStore{}
	collector := NewCollector(store, fakeSampler{sample: Sample{CPUPercent: 25.5, MemoryBytes: 10, DiskBytes: 20, CacheBytes: 30, NetworkUploadBytes: 40, NetworkDownloadBytes: 50}}, "/storage")
	now := time.Date(2026, 7, 15, 12, 0, 0, 0, time.FixedZone("test", 8*60*60))
	collector.now = func() time.Time { return now }
	if err := collector.RunOnce(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.metric.RecordedAt != now.UTC() || store.metric.CPUPercent != 25.5 || store.metric.CacheBytes != 30 || store.metric.NetworkDownloadBytes != 50 {
		t.Fatalf("metric = %+v", store.metric)
	}
}

func TestCollectorDoesNotRecordFailedSample(t *testing.T) {
	store := &fakeStore{}
	collector := NewCollector(store, fakeSampler{err: errors.New("sample failed")}, "/storage")
	if err := collector.RunOnce(context.Background()); err == nil {
		t.Fatal("RunOnce() error = nil")
	}
	if !store.metric.RecordedAt.IsZero() {
		t.Fatalf("metric = %+v", store.metric)
	}
}

func TestCacheBytes(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "chunk"), []byte("12345"), 0o600); err != nil {
		t.Fatal(err)
	}
	result, err := cacheBytes(root)
	if err != nil || result != 5 {
		t.Fatalf("cacheBytes() = %d, %v", result, err)
	}
}
