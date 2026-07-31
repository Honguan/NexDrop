package routing

import (
	"reflect"
	"testing"
	"time"
)

func TestPlanPrefersHealthyDirectAndStartsNodeAfterHeadStart(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	attempts := Plan([]Candidate{
		{ID: "node", Endpoint: "https://node", Kind: KindNode, Authenticated: true, Healthy: true, HandshakeLatency: 50 * time.Millisecond},
		{ID: "lan", Endpoint: "https://192.0.2.10", Kind: KindLAN, Authenticated: true, Healthy: true, HandshakeLatency: 20 * time.Millisecond, LastSuccess: now.Add(-time.Minute)},
	}, now, Config{DirectHeadStart: 200 * time.Millisecond})
	if len(attempts) != 2 || attempts[0].Candidate.ID != "lan" {
		t.Fatalf("unexpected plan: %#v", attempts)
	}
	if attempts[1].StartAfter != 200*time.Millisecond {
		t.Fatalf("node fallback should start after head start: %#v", attempts[1])
	}
}

func TestPlanStartsNodeImmediatelyWithoutUsableDirectCandidate(t *testing.T) {
	attempts := Plan([]Candidate{
		{ID: "bad-lan", Endpoint: "https://lan", Kind: KindLAN, Authenticated: false, Healthy: true},
		{ID: "node", Endpoint: "https://node", Kind: KindNode, Authenticated: true, Healthy: true},
	}, time.Now(), Config{})
	if len(attempts) != 1 || attempts[0].Candidate.ID != "node" || attempts[0].StartAfter != 0 {
		t.Fatalf("unexpected plan: %#v", attempts)
	}
}

func TestWinnerIsDeterministic(t *testing.T) {
	completed := time.Now().UTC()
	winner, ok := Winner([]Result{
		{CandidateID: "b", Authenticated: true, Healthy: true, CompletedAt: completed, Latency: 10 * time.Millisecond},
		{CandidateID: "a", Authenticated: true, Healthy: true, CompletedAt: completed, Latency: 10 * time.Millisecond},
	})
	if !ok || winner.CandidateID != "a" {
		t.Fatalf("unexpected winner: %#v, %v", winner, ok)
	}
}

func TestReusableChunksKeepsOnlyMatchingHashes(t *testing.T) {
	got := ReusableChunks(map[int]string{0: "a", 1: "b", 2: "c"}, map[int]string{0: "a", 1: "x", 2: "c"})
	if !reflect.DeepEqual(got, []int{0, 2}) {
		t.Fatalf("unexpected reusable chunks: %v", got)
	}
}
