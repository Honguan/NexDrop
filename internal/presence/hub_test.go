package presence

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"nexdrop/internal/auth"
)

func TestWebSocketTokenSources(t *testing.T) {
	request := httptest.NewRequest("GET", "http://example.test/ws?access_token=query-token", nil)
	request.Header.Set("Authorization", "Bearer header-token")
	if got := websocketToken(request); got != "header-token" {
		t.Fatalf("Authorization token = %q", got)
	}
	request.Header.Del("Authorization")
	request.Header.Set("Sec-WebSocket-Protocol", "nexdrop.v1, bearer.protocol-token")
	if got := websocketToken(request); got != "protocol-token" {
		t.Fatalf("protocol token = %q", got)
	}
	request.Header.Del("Sec-WebSocket-Protocol")
	if got := websocketToken(request); got != "query-token" {
		t.Fatalf("query token = %q", got)
	}
}

type fakeAuthenticator struct{ deviceID string }

func (authenticator fakeAuthenticator) Authenticate(_ context.Context, token string) (auth.Session, error) {
	if token != "valid" {
		return auth.Session{}, errors.New("invalid")
	}
	return auth.Session{DeviceID: &authenticator.deviceID}, nil
}

type fakeStore struct {
	mu           sync.Mutex
	connected    bool
	heartbeats   int
	disconnected bool
	acknowledged bool
}

func (store *fakeStore) ConnectDevice(context.Context, string, time.Time, string, string) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.connected = true
	return nil
}
func (store *fakeStore) HeartbeatDevice(context.Context, string, time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.heartbeats++
	return nil
}
func (store *fakeStore) DisconnectDevice(context.Context, string, time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.disconnected = true
	return nil
}
func (*fakeStore) PendingNotifications(context.Context, string) ([]Notification, error) {
	return []Notification{{ID: "notification-1", Type: "TRANSFER", Payload: map[string]any{"transferId": "transfer-1"}}}, nil
}
func (store *fakeStore) AcknowledgeNotification(context.Context, string, string, time.Time) error {
	store.mu.Lock()
	defer store.mu.Unlock()
	store.acknowledged = true
	return nil
}

func TestWebSocketHeartbeatAndNotification(t *testing.T) {
	store := &fakeStore{}
	hub := NewHub(fakeAuthenticator{deviceID: "device-1"}, store)
	hub.heartbeat = time.Hour
	hub.pollInterval = time.Hour
	server := httptest.NewServer(hub)
	defer server.Close()
	url := "ws" + strings.TrimPrefix(server.URL, "http") + "?access_token=valid&protocolVersion=1&clientVersion=test"
	connection, _, err := websocket.Dial(context.Background(), url, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.CloseNow()

	var first Message
	if err := wsjson.Read(context.Background(), connection, &first); err != nil {
		t.Fatal(err)
	}
	var second Message
	if err := wsjson.Read(context.Background(), connection, &second); err != nil {
		t.Fatal(err)
	}
	if first.Type != "notification" && second.Type != "notification" {
		t.Fatalf("messages = %+v, %+v", first, second)
	}
	if err := wsjson.Write(context.Background(), connection, Message{Type: "heartbeat"}); err != nil {
		t.Fatal(err)
	}
	var heartbeat Message
	if err := wsjson.Read(context.Background(), connection, &heartbeat); err != nil {
		t.Fatal(err)
	}
	if heartbeat.Type != "heartbeat_ack" {
		t.Fatalf("heartbeat response = %+v", heartbeat)
	}
	if err := wsjson.Write(context.Background(), connection, Message{Type: "notification_ack", NotificationID: "notification-1"}); err != nil {
		t.Fatal(err)
	}
	_ = connection.Close(websocket.StatusNormalClosure, "done")
	time.Sleep(20 * time.Millisecond)
	store.mu.Lock()
	defer store.mu.Unlock()
	if !store.connected || store.heartbeats != 1 || !store.acknowledged || !store.disconnected {
		t.Fatalf("store state = %+v", store)
	}
}
