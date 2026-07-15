package presence

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"
	"github.com/coder/websocket/wsjson"
	"nexdrop/internal/auth"
	"nexdrop/internal/version"
)

const (
	ProtocolVersion      = version.CurrentProtocol
	HeartbeatInterval    = 15 * time.Second
	DisconnectedTimeout  = 45 * time.Second
	notificationInterval = 5 * time.Second
)

type Authenticator interface {
	Authenticate(context.Context, string) (auth.Session, error)
}

type Notification struct {
	ID      string         `json:"id"`
	Type    string         `json:"type"`
	Payload map[string]any `json:"payload"`
}

type Store interface {
	ConnectDevice(context.Context, string, time.Time, string, string) error
	HeartbeatDevice(context.Context, string, time.Time) error
	DisconnectDevice(context.Context, string, time.Time) error
	PendingNotifications(context.Context, string) ([]Notification, error)
	AcknowledgeNotification(context.Context, string, string, time.Time) error
}

type Message struct {
	Type           string         `json:"type"`
	NotificationID string         `json:"notificationId,omitempty"`
	Notification   *Notification  `json:"notification,omitempty"`
	Payload        map[string]any `json:"payload,omitempty"`
}

type client struct {
	connection *websocket.Conn
	cancel     context.CancelFunc
	send       chan Message
}

type Hub struct {
	authenticator Authenticator
	store         Store
	now           func() time.Time
	heartbeat     time.Duration
	timeout       time.Duration
	pollInterval  time.Duration
	mu            sync.Mutex
	clients       map[string]*client
}

func NewHub(authenticator Authenticator, store Store) *Hub {
	return &Hub{
		authenticator: authenticator,
		store:         store,
		now:           time.Now,
		heartbeat:     HeartbeatInterval,
		timeout:       DisconnectedTimeout,
		pollInterval:  notificationInterval,
		clients:       make(map[string]*client),
	}
}

func (hub *Hub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	token := websocketToken(r)
	session, err := hub.authenticator.Authenticate(r.Context(), token)
	if err != nil || session.DeviceID == nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !version.SupportedProtocol(r.URL.Query().Get("protocolVersion")) {
		http.Error(w, "protocol version unsupported", http.StatusUpgradeRequired)
		return
	}
	clientVersion := r.URL.Query().Get("clientVersion")
	if !version.SupportedClient(clientVersion) {
		http.Error(w, "client version unsupported", http.StatusUpgradeRequired)
		return
	}
	connection, err := websocket.Accept(w, r, &websocket.AcceptOptions{Subprotocols: []string{"nexdrop.v1"}})
	if err != nil {
		return
	}
	deviceID := *session.DeviceID
	now := hub.now().UTC()
	if err := hub.store.ConnectDevice(r.Context(), deviceID, now, ProtocolVersion, clientVersion); err != nil {
		_ = connection.Close(websocket.StatusPolicyViolation, "device unavailable")
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	current := &client{connection: connection, cancel: cancel, send: make(chan Message, 32)}
	hub.register(deviceID, current)
	defer func() {
		cancel()
		_ = connection.Close(websocket.StatusNormalClosure, "connection closed")
		if hub.unregister(deviceID, current) {
			_ = hub.store.DisconnectDevice(context.Background(), deviceID, hub.now().UTC())
		}
	}()

	writeErrors := make(chan error, 1)
	go func() { writeErrors <- hub.writeLoop(ctx, deviceID, current) }()
	if !hub.enqueue(current, Message{Type: "connected", Payload: map[string]any{"heartbeatIntervalSeconds": int(hub.heartbeat.Seconds()), "versions": version.Current()}}) {
		return
	}

	for {
		readContext, readCancel := context.WithTimeout(ctx, hub.timeout)
		var message Message
		err := wsjson.Read(readContext, connection, &message)
		readCancel()
		if err != nil {
			return
		}
		switch message.Type {
		case "heartbeat":
			if err := hub.store.HeartbeatDevice(ctx, deviceID, hub.now().UTC()); err != nil {
				return
			}
			if !hub.enqueue(current, Message{Type: "heartbeat_ack"}) {
				return
			}
		case "notification_ack":
			if message.NotificationID == "" || hub.store.AcknowledgeNotification(ctx, deviceID, message.NotificationID, hub.now().UTC()) != nil {
				return
			}
		default:
			return
		}
		select {
		case <-writeErrors:
			return
		default:
		}
	}
}

func websocketToken(r *http.Request) string {
	if header := r.Header.Get("Authorization"); strings.HasPrefix(header, "Bearer ") {
		return strings.TrimSpace(strings.TrimPrefix(header, "Bearer "))
	}
	for _, protocol := range strings.Split(r.Header.Get("Sec-WebSocket-Protocol"), ",") {
		protocol = strings.TrimSpace(protocol)
		if strings.HasPrefix(protocol, "bearer.") {
			return strings.TrimPrefix(protocol, "bearer.")
		}
	}
	return r.URL.Query().Get("access_token")
}

func (hub *Hub) writeLoop(ctx context.Context, deviceID string, current *client) error {
	heartbeatTicker := time.NewTicker(hub.heartbeat)
	pollTicker := time.NewTicker(hub.pollInterval)
	defer heartbeatTicker.Stop()
	defer pollTicker.Stop()
	if err := hub.sendNotifications(ctx, deviceID, current); err != nil {
		return err
	}
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case message := <-current.send:
			if err := wsjson.Write(ctx, current.connection, message); err != nil {
				return err
			}
		case <-heartbeatTicker.C:
			pingContext, cancel := context.WithTimeout(ctx, 5*time.Second)
			err := current.connection.Ping(pingContext)
			cancel()
			if err != nil {
				return err
			}
		case <-pollTicker.C:
			if err := hub.sendNotifications(ctx, deviceID, current); err != nil {
				return err
			}
		}
	}
}

func (hub *Hub) sendNotifications(ctx context.Context, deviceID string, current *client) error {
	notifications, err := hub.store.PendingNotifications(ctx, deviceID)
	if err != nil {
		return err
	}
	for index := range notifications {
		if !hub.enqueue(current, Message{Type: "notification", Notification: &notifications[index]}) {
			return errors.New("notification queue full")
		}
	}
	return nil
}

func (hub *Hub) register(deviceID string, current *client) {
	hub.mu.Lock()
	previous := hub.clients[deviceID]
	hub.clients[deviceID] = current
	hub.mu.Unlock()
	if previous != nil {
		previous.cancel()
		_ = previous.connection.Close(websocket.StatusNormalClosure, "replaced by newer connection")
	}
}

func (hub *Hub) unregister(deviceID string, current *client) bool {
	hub.mu.Lock()
	defer hub.mu.Unlock()
	if hub.clients[deviceID] != current {
		return false
	}
	delete(hub.clients, deviceID)
	return true
}

func (hub *Hub) enqueue(current *client, message Message) bool {
	select {
	case current.send <- message:
		return true
	default:
		return false
	}
}
