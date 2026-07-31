package messagelifecycle

import (
	"testing"
	"time"
)

func TestPageUsesStableCreatedAtAndIDOrdering(t *testing.T) {
	now := time.Date(2026, 7, 31, 12, 0, 0, 0, time.UTC)
	items := []Message{{ID: "a", CreatedAt: now}, {ID: "b", CreatedAt: now}, {ID: "c", CreatedAt: now.Add(-time.Second)}}
	first, cursor := Page(items, Cursor{}, 2)
	if len(first) != 2 || first[0].ID != "b" || first[1].ID != "a" || cursor.ID != "a" {
		t.Fatalf("unexpected first page: %#v %#v", first, cursor)
	}
	second, next := Page(items, cursor, 2)
	if len(second) != 1 || second[0].ID != "c" || !next.CreatedAt.IsZero() {
		t.Fatalf("unexpected second page: %#v %#v", second, next)
	}
}

func TestCursorRejectsTampering(t *testing.T) {
	codec, _ := NewCursorCodec([]byte("0123456789abcdef0123456789abcdef"))
	value, _ := codec.Encode(Cursor{CreatedAt: time.Now().UTC(), ID: "message"})
	tampered := value[:len(value)-1] + "x"
	if _, err := codec.Decode(tampered); err != ErrInvalidCursor {
		t.Fatalf("expected invalid cursor, got %v", err)
	}
}

func TestReadCursorIsMonotonicAndIdempotent(t *testing.T) {
	now := time.Now().UTC()
	current := ReadCursor{CreatedAt: now, MessageID: "b"}
	if got := AdvanceRead(current, ReadCursor{CreatedAt: now, MessageID: "a"}); got != current {
		t.Fatalf("read cursor regressed: %#v", got)
	}
	if got := AdvanceRead(current, current); got != current {
		t.Fatalf("idempotent update changed cursor: %#v", got)
	}
}

func TestTombstoneSynchronizesGlobalDeletion(t *testing.T) {
	signer, _ := NewTombstoneSigner([]byte("0123456789abcdef0123456789abcdef"))
	value, err := signer.Issue("message", "device", DeleteEverywhere, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	tombstone, err := signer.Verify(value)
	if err != nil || tombstone.Scope != DeleteEverywhere || tombstone.MessageID != "message" {
		t.Fatalf("unexpected tombstone: %#v %v", tombstone, err)
	}
}

func TestRetentionSeparatesBodyCleanupFromMessageDeletion(t *testing.T) {
	now := time.Now().UTC()
	message := Message{CreatedAt: now.Add(-time.Hour), AttachmentBytes: 100, EveryTargetFetched: true}
	if action := EvaluateRetention(message, RetentionPolicy{Mode: RetentionAfterAllDownloaded}, now); action != RetentionDeleteBody {
		t.Fatalf("unexpected retention action: %s", action)
	}
	message.Pinned = true
	if action := EvaluateRetention(message, RetentionPolicy{Mode: RetentionAfterAllDownloaded}, now); action != RetentionKeep {
		t.Fatalf("pinned message should be kept: %s", action)
	}
}
