package adaptive

import "time"

const (
	DefaultMinChunkSize int64 = 1 * 1024 * 1024
	DefaultMaxChunkSize int64 = 32 * 1024 * 1024
)

type Limits struct {
	MinChunkSize      int64
	MaxChunkSize      int64
	MaxParallelChunks int
	MemoryBudget      int64
}

func (limits Limits) normalized() Limits {
	if limits.MinChunkSize <= 0 {
		limits.MinChunkSize = DefaultMinChunkSize
	}
	if limits.MaxChunkSize < limits.MinChunkSize {
		limits.MaxChunkSize = DefaultMaxChunkSize
	}
	if limits.MaxParallelChunks < 1 {
		limits.MaxParallelChunks = 1
	}
	if limits.MemoryBudget < limits.MinChunkSize*2 {
		limits.MemoryBudget = limits.MinChunkSize * 2
	}
	return limits
}

type Profile struct {
	ChunkSize      int64
	ParallelChunks int
}

type Observation struct {
	RTT                 time.Duration
	ThroughputBPS       float64
	RetryRate           float64
	ChecksumFailureRate float64
	Backpressure        bool
	StoragePressure     bool
	ThermalPressure     bool
	LowBattery          bool
	Background          bool
}

func Initial(limits Limits) Profile {
	limits = limits.normalized()
	return bounded(Profile{ChunkSize: max(limits.MinChunkSize, 4*1024*1024), ParallelChunks: min(2, limits.MaxParallelChunks)}, limits)
}

func Recommend(fileSize int64, current Profile, limits Limits, observation Observation) Profile {
	limits = limits.normalized()
	if current.ChunkSize <= 0 || current.ParallelChunks <= 0 {
		current = Initial(limits)
	}
	profile := current
	unstable := observation.RTT >= 250*time.Millisecond || observation.RetryRate >= 0.05 || observation.ChecksumFailureRate > 0
	constrained := observation.Backpressure || observation.StoragePressure || observation.ThermalPressure || observation.LowBattery || observation.Background

	switch {
	case unstable:
		profile.ChunkSize = max(limits.MinChunkSize, profile.ChunkSize/2)
		profile.ParallelChunks = maxInt(1, profile.ParallelChunks-1)
	case constrained:
		profile.ChunkSize = max(limits.MinChunkSize, min(profile.ChunkSize, 4*1024*1024))
		profile.ParallelChunks = 1
	case observation.RTT <= 30*time.Millisecond && observation.ThroughputBPS >= 50*1024*1024 && observation.RetryRate < 0.01:
		profile.ChunkSize = min(limits.MaxChunkSize, profile.ChunkSize*2)
		profile.ParallelChunks = min(limits.MaxParallelChunks, profile.ParallelChunks+1)
	case observation.RTT <= 120*time.Millisecond && observation.ThroughputBPS >= 8*1024*1024 && observation.RetryRate < 0.02:
		profile.ChunkSize = min(limits.MaxChunkSize, max(profile.ChunkSize, 8*1024*1024))
	}
	if fileSize > 0 && fileSize <= 4*limits.MinChunkSize {
		profile.ParallelChunks = 1
		profile.ChunkSize = min(profile.ChunkSize, max(limits.MinChunkSize, fileSize))
	}
	return bounded(profile, limits)
}

func bounded(profile Profile, limits Limits) Profile {
	profile.ChunkSize = max(limits.MinChunkSize, min(limits.MaxChunkSize, profile.ChunkSize))
	profile.ParallelChunks = maxInt(1, min(limits.MaxParallelChunks, profile.ParallelChunks))
	for profile.ChunkSize*int64(profile.ParallelChunks) > limits.MemoryBudget && profile.ParallelChunks > 1 {
		profile.ParallelChunks--
	}
	for profile.ChunkSize*int64(profile.ParallelChunks) > limits.MemoryBudget && profile.ChunkSize > limits.MinChunkSize {
		profile.ChunkSize = max(limits.MinChunkSize, profile.ChunkSize/2)
	}
	return profile
}

func SafeBoundary(completedChunkIndex int) bool { return completedChunkIndex >= -1 }

func min[T ~int | ~int64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func max[T ~int64](a, b T) T {
	if a > b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
