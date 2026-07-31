package recovery

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"time"
)

type Classification string

const (
	RetryImmediately Classification = "RETRY_IMMEDIATELY"
	RetryWithBackoff Classification = "RETRY_WITH_BACKOFF"
	WaitForReceiver  Classification = "WAIT_FOR_RECEIVER"
	ReconcileState   Classification = "RECONCILE_STATE"
	TerminalFailure  Classification = "TERMINAL_FAILURE"
)

type WorkItem struct {
	TransferID   string
	TargetID     string
	RetryCount   int
	NextAttempt  time.Time
	ErrorCode    string
	Verified     map[int]string
	UpdatedAt    time.Time
}

type Outcome struct {
	Completed       bool
	ReceiverOffline bool
	Inconsistent    bool
	Repaired        bool
	ErrorCode       string
}

type Store interface {
	ListRecoverable(context.Context, time.Time, int) ([]WorkItem, error)
	TryLease(context.Context, WorkItem, time.Time) (bool, error)
	ReleaseLease(context.Context, WorkItem) error
	MarkCompleted(context.Context, WorkItem, time.Time) error
	MarkWaiting(context.Context, WorkItem, Classification, time.Time, string) error
	MarkDeadLetter(context.Context, WorkItem, string, time.Time) error
	RecordRepair(context.Context, WorkItem, time.Time) error
}

type Runner interface {
	Resume(context.Context, WorkItem) (Outcome, error)
}

type Config struct {
	BaseBackoff time.Duration
	MaxBackoff  time.Duration
	MaxRetries  int
	LeaseTTL    time.Duration
}

func (config Config) normalized() Config {
	if config.BaseBackoff <= 0 {
		config.BaseBackoff = 5 * time.Second
	}
	if config.MaxBackoff < config.BaseBackoff {
		config.MaxBackoff = 30 * time.Minute
	}
	if config.MaxRetries <= 0 {
		config.MaxRetries = 8
	}
	if config.LeaseTTL <= 0 {
		config.LeaseTTL = 2 * time.Minute
	}
	return config
}

type Report struct {
	Scanned     int
	Leased      int
	Completed   int
	Waiting     int
	Repaired    int
	DeadLetters int
}

type Coordinator struct {
	store  Store
	runner Runner
	config Config
	now    func() time.Time
}

func New(store Store, runner Runner, config Config) *Coordinator {
	return &Coordinator{store: store, runner: runner, config: config.normalized(), now: time.Now}
}

func (coordinator *Coordinator) RunOnce(ctx context.Context, limit int) (Report, error) {
	if limit < 1 {
		limit = 100
	}
	now := coordinator.now().UTC()
	items, err := coordinator.store.ListRecoverable(ctx, now, limit)
	if err != nil {
		return Report{}, err
	}
	report := Report{Scanned: len(items)}
	for _, item := range items {
		if item.TransferID == "" || item.TargetID == "" || (!item.NextAttempt.IsZero() && item.NextAttempt.After(now)) {
			continue
		}
		leased, err := coordinator.store.TryLease(ctx, item, now.Add(coordinator.config.LeaseTTL))
		if err != nil {
			return report, err
		}
		if !leased {
			continue
		}
		report.Leased++
		outcome, runErr := coordinator.runner.Resume(ctx, item)
		classification := Classify(item.ErrorCode, outcome, runErr)
		switch classification {
		case RetryImmediately:
			report.Waiting++
			err = coordinator.store.MarkWaiting(ctx, item, classification, now, stableError(outcome, runErr))
		case RetryWithBackoff:
			if item.RetryCount >= coordinator.config.MaxRetries {
				report.DeadLetters++
				err = coordinator.store.MarkDeadLetter(ctx, item, stableError(outcome, runErr), now)
			} else {
				report.Waiting++
				next := now.Add(Backoff(item, coordinator.config))
				err = coordinator.store.MarkWaiting(ctx, item, classification, next, stableError(outcome, runErr))
			}
		case WaitForReceiver:
			report.Waiting++
			err = coordinator.store.MarkWaiting(ctx, item, classification, now.Add(coordinator.config.MaxBackoff), stableError(outcome, runErr))
		case ReconcileState:
			if outcome.Repaired {
				report.Repaired++
				err = coordinator.store.RecordRepair(ctx, item, now)
			} else {
				report.Waiting++
				err = coordinator.store.MarkWaiting(ctx, item, classification, now.Add(coordinator.config.BaseBackoff), stableError(outcome, runErr))
			}
		case TerminalFailure:
			report.DeadLetters++
			err = coordinator.store.MarkDeadLetter(ctx, item, stableError(outcome, runErr), now)
		default:
			if outcome.Completed && runErr == nil {
				report.Completed++
				err = coordinator.store.MarkCompleted(ctx, item, now)
			} else {
				err = errors.New("unclassified recovery result")
			}
		}
		if err == nil {
			err = coordinator.store.ReleaseLease(ctx, item)
		}
		if err != nil {
			return report, err
		}
	}
	return report, nil
}

func Classify(previousError string, outcome Outcome, err error) Classification {
	if outcome.Completed && err == nil {
		return ""
	}
	if outcome.ReceiverOffline || previousError == "RECEIVER_OFFLINE" || outcome.ErrorCode == "RECEIVER_OFFLINE" {
		return WaitForReceiver
	}
	if outcome.Inconsistent || previousError == "MISSING_CHUNK" || previousError == "LOST_ACK" {
		return ReconcileState
	}
	code := stableError(outcome, err)
	switch code {
	case "DATABASE_UNAVAILABLE", "NETWORK_UNAVAILABLE", "TIMEOUT", "NODE_RESTARTED":
		return RetryWithBackoff
	case "STALE_LOCK":
		return RetryImmediately
	case "INVALID_MANIFEST", "HASH_MISMATCH", "PERMISSION_DENIED", "DEVICE_REVOKED", "STORAGE_QUOTA_EXHAUSTED":
		return TerminalFailure
	default:
		if err != nil {
			return RetryWithBackoff
		}
		return TerminalFailure
	}
}

func Backoff(item WorkItem, config Config) time.Duration {
	config = config.normalized()
	shift := item.RetryCount
	if shift > 20 {
		shift = 20
	}
	backoff := config.BaseBackoff * time.Duration(1<<shift)
	if backoff > config.MaxBackoff {
		backoff = config.MaxBackoff
	}
	digest := sha256.Sum256([]byte(item.TransferID + ":" + item.TargetID))
	jitter := time.Duration(binary.BigEndian.Uint32(digest[:4])%1000) * time.Millisecond
	if backoff+jitter > config.MaxBackoff {
		return config.MaxBackoff
	}
	return backoff + jitter
}

func stableError(outcome Outcome, err error) string {
	if outcome.ErrorCode != "" {
		return outcome.ErrorCode
	}
	if err == nil {
		return "NONE"
	}
	return "RECOVERY_FAILED"
}
