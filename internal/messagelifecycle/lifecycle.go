package messagelifecycle

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"
)

var (
	ErrInvalidCursor    = errors.New("invalid message cursor")
	ErrInvalidTombstone = errors.New("invalid message tombstone")
	ErrExpiredTombstone = errors.New("expired message tombstone")
)

type Message struct {
	ID                 string
	CreatedAt          time.Time
	AttachmentBytes    int64
	EveryTargetFetched bool
	ExpiresAt          time.Time
	Pinned             bool
	TombstonedAt       *time.Time
}

type Cursor struct {
	CreatedAt time.Time `json:"createdAt"`
	ID        string    `json:"id"`
}

type CursorCodec struct{ secret []byte }

func NewCursorCodec(secret []byte) (*CursorCodec, error) {
	if len(secret) < 32 {
		return nil, ErrInvalidCursor
	}
	return &CursorCodec{secret: append([]byte(nil), secret...)}, nil
}

func (codec *CursorCodec) Encode(cursor Cursor) (string, error) {
	if cursor.ID == "" || cursor.CreatedAt.IsZero() {
		return "", ErrInvalidCursor
	}
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + codec.signature(encoded), nil
}

func (codec *CursorCodec) Decode(value string) (Cursor, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return Cursor{}, ErrInvalidCursor
	}
	expected := codec.signature(parts[0])
	if len(expected) != len(parts[1]) || subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return Cursor{}, ErrInvalidCursor
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Cursor{}, ErrInvalidCursor
	}
	var cursor Cursor
	if json.Unmarshal(payload, &cursor) != nil || cursor.ID == "" || cursor.CreatedAt.IsZero() {
		return Cursor{}, ErrInvalidCursor
	}
	return cursor, nil
}

func (codec *CursorCodec) signature(value string) string {
	mac := hmac.New(sha256.New, codec.secret)
	_, _ = mac.Write([]byte("nexdrop/message-cursor/v1\n"))
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func Page(messages []Message, after Cursor, limit int) ([]Message, Cursor) {
	if limit < 1 || limit > 1000 {
		limit = 100
	}
	ordered := append([]Message(nil), messages...)
	sort.SliceStable(ordered, func(i, j int) bool {
		if !ordered[i].CreatedAt.Equal(ordered[j].CreatedAt) {
			return ordered[i].CreatedAt.After(ordered[j].CreatedAt)
		}
		return ordered[i].ID > ordered[j].ID
	})
	result := make([]Message, 0, limit)
	for _, message := range ordered {
		if !after.CreatedAt.IsZero() && (message.CreatedAt.After(after.CreatedAt) || (message.CreatedAt.Equal(after.CreatedAt) && message.ID >= after.ID)) {
			continue
		}
		result = append(result, message)
		if len(result) == limit {
			break
		}
	}
	if len(result) == 0 || len(result) < limit {
		return result, Cursor{}
	}
	last := result[len(result)-1]
	return result, Cursor{CreatedAt: last.CreatedAt, ID: last.ID}
}

type ReadCursor struct {
	CreatedAt time.Time
	MessageID string
}

func AdvanceRead(current, candidate ReadCursor) ReadCursor {
	if candidate.CreatedAt.After(current.CreatedAt) || (candidate.CreatedAt.Equal(current.CreatedAt) && candidate.MessageID > current.MessageID) {
		return candidate
	}
	return current
}

type DeleteScope string

const (
	DeleteLocal          DeleteScope = "LOCAL_ONLY"
	DeleteAttachmentBody DeleteScope = "ATTACHMENT_BODY_ONLY"
	DeleteEverywhere     DeleteScope = "ALL_DEVICES"
)

type Tombstone struct {
	Version   int         `json:"version"`
	MessageID string      `json:"messageId"`
	Scope     DeleteScope `json:"scope"`
	IssuedAt  int64       `json:"issuedAt"`
	ExpiresAt int64       `json:"expiresAt"`
	ActorID   string      `json:"actorId"`
}

type TombstoneSigner struct {
	secret []byte
	now    func() time.Time
}

func NewTombstoneSigner(secret []byte) (*TombstoneSigner, error) {
	if len(secret) < 32 {
		return nil, ErrInvalidTombstone
	}
	return &TombstoneSigner{secret: append([]byte(nil), secret...), now: time.Now}, nil
}

func (signer *TombstoneSigner) Issue(messageID, actorID string, scope DeleteScope, ttl time.Duration) (string, error) {
	if messageID == "" || actorID == "" || !validDeleteScope(scope) || ttl <= 0 {
		return "", ErrInvalidTombstone
	}
	now := signer.now().UTC()
	tombstone := Tombstone{Version: 1, MessageID: messageID, Scope: scope, IssuedAt: now.Unix(), ExpiresAt: now.Add(ttl).Unix(), ActorID: actorID}
	payload, err := json.Marshal(tombstone)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	return encoded + "." + signer.signature(encoded), nil
}

func (signer *TombstoneSigner) Verify(value string) (Tombstone, error) {
	parts := strings.Split(value, ".")
	if len(parts) != 2 {
		return Tombstone{}, ErrInvalidTombstone
	}
	expected := signer.signature(parts[0])
	if len(expected) != len(parts[1]) || subtle.ConstantTimeCompare([]byte(expected), []byte(parts[1])) != 1 {
		return Tombstone{}, ErrInvalidTombstone
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Tombstone{}, ErrInvalidTombstone
	}
	var tombstone Tombstone
	if json.Unmarshal(payload, &tombstone) != nil || tombstone.Version != 1 || tombstone.MessageID == "" || tombstone.ActorID == "" || !validDeleteScope(tombstone.Scope) {
		return Tombstone{}, ErrInvalidTombstone
	}
	if signer.now().UTC().After(time.Unix(tombstone.ExpiresAt, 0)) {
		return Tombstone{}, ErrExpiredTombstone
	}
	return tombstone, nil
}

func (signer *TombstoneSigner) signature(value string) string {
	mac := hmac.New(sha256.New, signer.secret)
	_, _ = mac.Write([]byte("nexdrop/message-tombstone/v1\n"))
	_, _ = mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func validDeleteScope(scope DeleteScope) bool {
	return scope == DeleteLocal || scope == DeleteAttachmentBody || scope == DeleteEverywhere
}

type RetentionMode string

const (
	RetentionPermanent          RetentionMode = "PERMANENT"
	RetentionFixedDays          RetentionMode = "FIXED_DAYS"
	RetentionAfterAllDownloaded RetentionMode = "AFTER_ALL_DOWNLOADED"
	RetentionBodyAfterExpiry    RetentionMode = "BODY_AFTER_EXPIRY"
)

type RetentionPolicy struct {
	Mode RetentionMode
	Days int
}

type RetentionAction string

const (
	RetentionKeep       RetentionAction = "KEEP"
	RetentionDeleteBody RetentionAction = "DELETE_BODY"
	RetentionTombstone  RetentionAction = "CREATE_TOMBSTONE"
)

func EvaluateRetention(message Message, policy RetentionPolicy, now time.Time) RetentionAction {
	if message.Pinned {
		return RetentionKeep
	}
	switch policy.Mode {
	case RetentionPermanent:
		return RetentionKeep
	case RetentionFixedDays:
		if policy.Days > 0 && !now.Before(message.CreatedAt.Add(time.Duration(policy.Days)*24*time.Hour)) {
			return RetentionTombstone
		}
	case RetentionAfterAllDownloaded:
		if message.EveryTargetFetched && message.AttachmentBytes > 0 {
			return RetentionDeleteBody
		}
	case RetentionBodyAfterExpiry:
		if !message.ExpiresAt.IsZero() && !now.Before(message.ExpiresAt) && message.AttachmentBytes > 0 {
			return RetentionDeleteBody
		}
	}
	return RetentionKeep
}
