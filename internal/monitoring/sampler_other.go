//go:build !linux

package monitoring

import "runtime"

type systemSampler struct{}

func NewSystemSampler() Sampler { return &systemSampler{} }

func (*systemSampler) Sample(storagePath string) (Sample, error) {
	cache, err := cacheBytes(storagePath)
	if err != nil {
		return Sample{}, err
	}
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	return Sample{MemoryBytes: int64(memory.Sys), CacheBytes: cache}, nil
}
