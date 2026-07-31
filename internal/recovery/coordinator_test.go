package recovery

import (
	"context"
	"errors"
	"testing"
	"time"
)

type memoryStore struct {
	items       []WorkItem
	waiting     int
	completed   int
	deadLetters int
	repairs     int
}

func (store *memoryStore) ListRecoverable(context.Context, time.Time, int) ([]WorkItem, error) {
	return append([]WorkItem(nil), store.items...), nil
}
func (store *memoryStore) TryLease(context.Context, WorkItem, time.Time) (bool, error) {
	return true, nil
}
func (store *memoryStore) ReleaseLease(context.Context, WorkItem) error { return nil }
func (store *memoryStore) MarkCompleted(context.Context, WorkItem, time.Time) error {
	store.completed++
	return nil
}
func (store *memoryStore) MarkWaiting(context.Context, WorkItem, Classification, time.Time, string) error {
	store.waiting++
	return nil
}
func (store *memoryStore) MarkDeadLetter(context.Context, WorkItem, string, time.Time) error {
	store.deadLetters++
	return nil
}
func (store *memoryStore) RecordRepair(context.Context, WorkItem, time.Time) error {
	store.repairs++
	return nil
}

type runnerFunc func(context.Context, WorkItem) (Outcome, error)

func (runner runnerFunc) Resume(ctx context.Context, item WorkItem) (Outcome, error) {
	return runner(ctx, item)
}

func TestCoordinatorCompletesAndReleasesLease(t *testing.T) {
	store := &memoryStore{items: []WorkItem{{TransferID: "t", TargetID: "d"}}}
	coordinator := New(store, runnerFunc(func(context.Context, WorkItem) (Outcome, error) {
		return Outcome{Completed: true}, nil
	}), Config{})
	report, err := coordinator.RunOnce(context.Background(), 10)
	if err != nil || report.Completed != 1 || store.completed != 1 {
		t.Fatalf("unexpected completion: %#v %v", report, err)
	}
}

func TestCoordinatorMovesExhaustedWorkToDeadLetter(t *testing.T) {
	store := &memoryStore{items: []WorkItem{{TransferID: "t", TargetID: "d", RetryCount: 3, ErrorCode: "TIMEOUT"}}}
	coordinator := New(store, runnerFunc(func(context.Context, WorkItem) (Outcome, error) {
		return Outcome{}, errors.New("offline")
	}), Config{MaxRetries: 3})
	report, err := coordinator.RunOnce(context.Background(), 10)
	if err != nil || report.DeadLetters != 1 || store.deadLetters != 1 {
		t.Fatalf("unexpected dead-letter result: %#v %v", report, err)
	}
}

func TestCoordinatorRepairsInconsistentWork(t *testing.T) {
	store := &memoryStore{items: []WorkItem{{TransferID: "t", TargetID: "d", ErrorCode: "MISSING_CHUNK"}}}
	coordinator := New(store, runnerFunc(func(context.Context, WorkItem) (Outcome, error) {
		return Outcome{Inconsistent: true, Repaired: true}, nil
	}), Config{})
	report, err := coordinator.RunOnce(context.Background(), 10)
	if err != nil || report.Repaired != 1 || store.repairs != 1 {
		t.Fatalf("unexpected repair result: %#v %v", report, err)
	}
}

func TestBackoffIsDeterministicAndBounded(t *testing.T) {
	item := WorkItem{TransferID: "transfer", TargetID: "device", RetryCount: 20}
	config := Config{BaseBackoff: time.Second, MaxBackoff: time.Minute}
	first := Backoff(item, config)
	second := Backoff(item, config)
	if first != second || first > time.Minute {
		t.Fatalf("unexpected backoff: %v %v", first, second)
	}
}
