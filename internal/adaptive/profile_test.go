package adaptive

import (
	"testing"
	"time"
)

func TestFastLANIncreasesChunkAndParallelismWithinBounds(t *testing.T) {
	limits := Limits{MinChunkSize: 1 << 20, MaxChunkSize: 32 << 20, MaxParallelChunks: 6, MemoryBudget: 128 << 20}
	profile := Recommend(1<<30, Profile{ChunkSize: 8 << 20, ParallelChunks: 2}, limits, Observation{
		RTT: 10 * time.Millisecond, ThroughputBPS: 100 << 20,
	})
	if profile.ChunkSize != 16<<20 || profile.ParallelChunks != 3 {
		t.Fatalf("unexpected fast profile: %#v", profile)
	}
}

func TestUnstableLinkReducesRetryCost(t *testing.T) {
	limits := Limits{MinChunkSize: 1 << 20, MaxChunkSize: 32 << 20, MaxParallelChunks: 6, MemoryBudget: 64 << 20}
	profile := Recommend(1<<30, Profile{ChunkSize: 8 << 20, ParallelChunks: 4}, limits, Observation{
		RTT: 300 * time.Millisecond, RetryRate: 0.10,
	})
	if profile.ChunkSize != 4<<20 || profile.ParallelChunks != 3 {
		t.Fatalf("unexpected unstable profile: %#v", profile)
	}
}

func TestMemoryBudgetIsNeverExceeded(t *testing.T) {
	limits := Limits{MinChunkSize: 1 << 20, MaxChunkSize: 32 << 20, MaxParallelChunks: 6, MemoryBudget: 8 << 20}
	profile := Recommend(1<<30, Profile{ChunkSize: 16 << 20, ParallelChunks: 6}, limits, Observation{})
	if profile.ChunkSize*int64(profile.ParallelChunks) > limits.MemoryBudget {
		t.Fatalf("profile exceeds memory budget: %#v", profile)
	}
}

func TestSmallFileUsesSingleStream(t *testing.T) {
	profile := Recommend(2<<20, Profile{ChunkSize: 8 << 20, ParallelChunks: 4}, Limits{MaxParallelChunks: 6, MemoryBudget: 64 << 20}, Observation{})
	if profile.ParallelChunks != 1 || profile.ChunkSize > 2<<20 {
		t.Fatalf("unexpected small-file profile: %#v", profile)
	}
}
