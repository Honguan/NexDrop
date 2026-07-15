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
	return collector.store.RecordSystemMetric(ctx, analytics.NodeMetric{
		RecordedAt:           collector.now().UTC(),
		CPUPercent:           sample.CPUPercent,
		MemoryBytes:          sample.MemoryBytes,
		DiskBytes:            sample.DiskBytes,
		CacheBytes:           sample.CacheBytes,
		NetworkUploadBytes:   sample.NetworkUploadBytes,
		NetworkDownloadBytes: sample.NetworkDownloadBytes,
	})
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
