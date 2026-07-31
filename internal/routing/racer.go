package routing

import (
	"math"
	"sort"
	"time"
)

type Kind string

const (
	KindLAN  Kind = "LAN"
	KindVPN  Kind = "VPN"
	KindIPv4 Kind = "IPV4"
	KindIPv6 Kind = "IPV6"
	KindNode Kind = "NODE"
)

type Candidate struct {
	ID               string
	Endpoint         string
	Kind             Kind
	Authenticated    bool
	Healthy          bool
	HandshakeLatency time.Duration
	ThroughputBPS    float64
	FailureRate      float64
	LastSuccess      time.Time
}

func (candidate Candidate) Direct() bool { return candidate.Kind != KindNode }

type Config struct {
	DirectHeadStart time.Duration
	MinimumHealth   float64
	StaleAfter      time.Duration
}

func (config Config) normalized() Config {
	if config.DirectHeadStart <= 0 {
		config.DirectHeadStart = 350 * time.Millisecond
	}
	if config.MinimumHealth <= 0 || config.MinimumHealth > 1 {
		config.MinimumHealth = 0.35
	}
	if config.StaleAfter <= 0 {
		config.StaleAfter = 7 * 24 * time.Hour
	}
	return config
}

type Attempt struct {
	Candidate  Candidate
	StartAfter time.Duration
	Score      float64
}

func Plan(candidates []Candidate, now time.Time, config Config) []Attempt {
	config = config.normalized()
	attempts := make([]Attempt, 0, len(candidates))
	hasDirect := false
	for _, candidate := range candidates {
		score := score(candidate, now, config)
		if math.IsInf(score, -1) {
			continue
		}
		if candidate.Direct() {
			hasDirect = true
		}
		attempts = append(attempts, Attempt{Candidate: candidate, Score: score})
	}
	sort.SliceStable(attempts, func(i, j int) bool {
		if attempts[i].Score != attempts[j].Score {
			return attempts[i].Score > attempts[j].Score
		}
		if attempts[i].Candidate.Direct() != attempts[j].Candidate.Direct() {
			return attempts[i].Candidate.Direct()
		}
		if attempts[i].Candidate.Kind != attempts[j].Candidate.Kind {
			return attempts[i].Candidate.Kind < attempts[j].Candidate.Kind
		}
		return attempts[i].Candidate.ID < attempts[j].Candidate.ID
	})
	if hasDirect {
		for index := range attempts {
			if !attempts[index].Candidate.Direct() {
				attempts[index].StartAfter = config.DirectHeadStart
			}
		}
	}
	return attempts
}

func score(candidate Candidate, now time.Time, config Config) float64 {
	if candidate.ID == "" || candidate.Endpoint == "" || !candidate.Authenticated || !candidate.Healthy {
		return math.Inf(-1)
	}
	health := 1 - clamp(candidate.FailureRate, 0, 1)
	if health < config.MinimumHealth {
		return math.Inf(-1)
	}
	value := 100 * health
	if candidate.Direct() {
		value += 30
	}
	if candidate.HandshakeLatency > 0 {
		value -= math.Min(40, float64(candidate.HandshakeLatency.Milliseconds())/20)
	}
	if candidate.ThroughputBPS > 0 {
		value += math.Min(20, math.Log10(candidate.ThroughputBPS+1)*2)
	}
	if !candidate.LastSuccess.IsZero() && now.Sub(candidate.LastSuccess) <= config.StaleAfter {
		value += 10 * (1 - now.Sub(candidate.LastSuccess).Seconds()/config.StaleAfter.Seconds())
	}
	return value
}

func clamp(value, minimum, maximum float64) float64 {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}

type Result struct {
	CandidateID   string
	Authenticated bool
	Healthy       bool
	CompletedAt   time.Time
	Latency       time.Duration
	ErrorCode     string
}

func Winner(results []Result) (Result, bool) {
	eligible := make([]Result, 0, len(results))
	for _, result := range results {
		if result.CandidateID != "" && result.Authenticated && result.Healthy && result.ErrorCode == "" && !result.CompletedAt.IsZero() {
			eligible = append(eligible, result)
		}
	}
	if len(eligible) == 0 {
		return Result{}, false
	}
	sort.SliceStable(eligible, func(i, j int) bool {
		if !eligible[i].CompletedAt.Equal(eligible[j].CompletedAt) {
			return eligible[i].CompletedAt.Before(eligible[j].CompletedAt)
		}
		if eligible[i].Latency != eligible[j].Latency {
			return eligible[i].Latency < eligible[j].Latency
		}
		return eligible[i].CandidateID < eligible[j].CandidateID
	})
	return eligible[0], true
}

func ReusableChunks(verified, available map[int]string) []int {
	indexes := make([]int, 0)
	for index, hash := range verified {
		if hash != "" && available[index] == hash {
			indexes = append(indexes, index)
		}
	}
	sort.Ints(indexes)
	return indexes
}
