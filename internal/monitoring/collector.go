package monitoring

import (
	"context"
	"time"

	"nexdrop/internal/analytics"
)

type Sample struct {
	CPUPercent           float32
	MemoryBytes          int64
	DiskBytes            int64
	CacheBytes           int64
	NetworkUploadBytes   int64
	NetworkDownloadBytes int64
}

type Sampler interface {
	Sample(string) (Sample, error)
}

type Store interface {
	RecordSystemMetric(context.Context, analytics.NodeMetric) error
}

type OperationalSnapshot struct {
	Queued         int64
	Stalled        int64
	Unacknowledged int64
	DeadLetter     int64
}

type OperationalStore interface {
	OperationalSnapshot(context.Context) (OperationalSnapshot, error)
}

type Collector struct {
	store       Store
	sampler     Sampler
	storagePath string
	now         func() time.Time
}

func NewCollector(store Store, sampler Sampler, storagePath string) *Collector {
	return &Collector{store: store, sampler: sampler, storagePath: storagePath, now: time.Now}
}

func (collector *Collector) RunOnce(ctx context.Context) error {
	sample, err := collector.sampler.Sample(collector.storagePath)
	if err != nil {
		return err
	}
	if err := collector.store.RecordSystemMetric(ctx, analytics.NodeMetric{
		RecordedAt:           collector.now().UTC(),
		CPUPercent:           sample.CPUPercent,
		MemoryBytes:          sample.MemoryBytes,
		DiskBytes:            sample.DiskBytes,
		CacheBytes:           sample.CacheBytes,
		NetworkUploadBytes:   sample.NetworkUploadBytes,
		NetworkDownloadBytes: sample.NetworkDownloadBytes,
	}); err != nil {
		return err
	}
	operationalStore, ok := collector.store.(OperationalStore)
	if !ok {
		return nil
	}
	snapshot, err := operationalStore.OperationalSnapshot(ctx)
	if err != nil {
		return err
	}
	for _, metric := range []struct {
		name   string
		value  int64
		labels map[string]string
	}{
		{"nexdrop_transfer_queue_current", snapshot.Queued, map[string]string{"status": "QUEUED"}},
		{"nexdrop_transfer_stalled_current", snapshot.Stalled, map[string]string{"status": "PAUSED"}},
		{"nexdrop_transfer_unacknowledged_current", snapshot.Unacknowledged, map[string]string{"status": "DELIVERED"}},
		{"nexdrop_dead_letter_current", snapshot.DeadLetter, map[string]string{"worker": "transfer"}},
	} {
		if err := DefaultRegistry.Set(metric.name, float64(metric.value), metric.labels); err != nil {
			return err
		}
	}
	return nil
}

func (collector *Collector) Start(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = collector.RunOnce(ctx)
		}
	}
}
