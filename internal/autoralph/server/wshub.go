package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Message types sent over the WebSocket connection.
const (
	MsgIssueStateChanged = "issue_state_changed"
	MsgBuildEvent        = "build_event"
	MsgNewIssue          = "new_issue"
	MsgActivity          = "activity"
)

// WSMessage is the envelope sent to WebSocket clients.
type WSMessage struct {
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
	Timestamp string          `json:"timestamp"`
}

// NewWSMessage creates a WSMessage with the given type and payload.
// Returns an error if payload cannot be marshaled to JSON.
func NewWSMessage(msgType string, payload any) (WSMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return WSMessage{}, err
	}
	return WSMessage{
		Type:      msgType,
		Payload:   data,
		Timestamp: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// Hub manages WebSocket clients and broadcasts messages to all connected
// clients. It is safe for concurrent use.
type Hub struct {
	mu      sync.RWMutex
	clients map[*wsClient]struct{}
	logger  *slog.Logger
}

// NewHub creates a Hub ready to accept client connections.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	return &Hub{
		clients: make(map[*wsClient]struct{}),
		logger:  logger,
	}
}

// Broadcast sends a message to all connected clients. Clients whose send
// buffer is full are dropped.
func (h *Hub) Broadcast(msg WSMessage) {
	data, err := json.Marshal(msg)
	if err != nil {
		h.logger.Error("marshaling ws message", "error", err)
		return
	}

	h.mu.RLock()
	defer h.mu.RUnlock()

	for c := range h.clients {
		select {
		case c.send <- data:
		default:
			go h.removeClient(c)
		}
	}
}

// ClientCount returns the number of currently connected clients.
func (h *Hub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *Hub) addClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.clients[c] = struct{}{}
}

func (h *Hub) removeClient(c *wsClient) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
		close(c.send)
	}
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// ServeWS upgrades the HTTP connection to a WebSocket and registers the
// client with the hub. It handles the read and write pumps for the
// connection's lifetime.
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		h.logger.Error("upgrading to websocket", "error", err)
		return
	}

	c := &wsClient{
		hub:  h,
		conn: conn,
		send: make(chan []byte, 64),
	}
	h.addClient(c)

	go c.writePump()
	go c.readPump()
}

const (
	writeWait  = 10 * time.Second
	pongWait   = 60 * time.Second
	pingPeriod = (pongWait * 9) / 10
)

type wsClient struct {
	hub  *Hub
	conn *websocket.Conn
	send chan []byte
}

// readPump reads messages from the WebSocket connection. We don't expect
// meaningful client-to-server messages; the pump exists to detect disconnects
// and respond to pings/pongs.
func (c *wsClient) readPump() {
	defer func() {
		c.hub.removeClient(c)
		c.conn.Close()
	}()

	c.conn.SetReadLimit(512)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})

	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			break
		}
	}
}

// writePump sends messages from the send channel to the WebSocket connection.
// It also sends periodic pings to keep the connection alive.
func (c *wsClient) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()

	for {
		select {
		case msg, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}
