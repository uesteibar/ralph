package server_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/uesteibar/ralph/internal/autoralph/server"
)

func dialWS(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(url, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestNewWSMessage_MarshalsPayload(t *testing.T) {
	payload := map[string]string{"id": "abc", "state": "building"}
	msg, err := server.NewWSMessage(server.MsgIssueStateChanged, payload)
	if err != nil {
		t.Fatalf("NewWSMessage error: %v", err)
	}

	if msg.Type != server.MsgIssueStateChanged {
		t.Fatalf("expected type %q, got %q", server.MsgIssueStateChanged, msg.Type)
	}

	if msg.Timestamp == "" {
		t.Fatal("expected non-empty timestamp")
	}

	var decoded map[string]string
	if err := json.Unmarshal(msg.Payload, &decoded); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if decoded["id"] != "abc" {
		t.Fatalf("expected payload id 'abc', got %q", decoded["id"])
	}
}

func TestNewWSMessage_InvalidPayload_ReturnsError(t *testing.T) {
	_, err := server.NewWSMessage("test", make(chan int))
	if err == nil {
		t.Fatal("expected error for non-marshalable payload")
	}
}

func TestHub_ClientCount_StartsAtZero(t *testing.T) {
	hub := server.NewHub(nil)
	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients, got %d", hub.ClientCount())
	}
}

func TestHub_ServeWS_RegistersClient(t *testing.T) {
	hub := server.NewHub(nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer ts.Close()

	conn := dialWS(t, ts.URL)
	_ = conn

	// Give goroutines a moment to register
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}
}

func TestHub_ClientDisconnect_Unregisters(t *testing.T) {
	hub := server.NewHub(nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http")
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to dial ws: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}

	conn.Close()
	time.Sleep(100 * time.Millisecond)

	if hub.ClientCount() != 0 {
		t.Fatalf("expected 0 clients after disconnect, got %d", hub.ClientCount())
	}
}

func TestHub_Broadcast_DeliversToClient(t *testing.T) {
	hub := server.NewHub(nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer ts.Close()

	conn := dialWS(t, ts.URL)
	time.Sleep(50 * time.Millisecond)

	msg, _ := server.NewWSMessage(server.MsgBuildEvent, map[string]string{"detail": "iteration 1"})
	hub.Broadcast(msg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read message: %v", err)
	}

	var received server.WSMessage
	if err := json.Unmarshal(raw, &received); err != nil {
		t.Fatalf("failed to unmarshal received message: %v", err)
	}

	if received.Type != server.MsgBuildEvent {
		t.Fatalf("expected type %q, got %q", server.MsgBuildEvent, received.Type)
	}
}

func TestHub_Broadcast_DeliversToMultipleClients(t *testing.T) {
	hub := server.NewHub(nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer ts.Close()

	conn1 := dialWS(t, ts.URL)
	conn2 := dialWS(t, ts.URL)
	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 2 {
		t.Fatalf("expected 2 clients, got %d", hub.ClientCount())
	}

	msg, _ := server.NewWSMessage(server.MsgNewIssue, map[string]string{"title": "Add feature"})
	hub.Broadcast(msg)

	for i, conn := range []*websocket.Conn{conn1, conn2} {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("client %d: failed to read message: %v", i, err)
		}

		var received server.WSMessage
		if err := json.Unmarshal(raw, &received); err != nil {
			t.Fatalf("client %d: failed to unmarshal: %v", i, err)
		}
		if received.Type != server.MsgNewIssue {
			t.Fatalf("client %d: expected type %q, got %q", i, server.MsgNewIssue, received.Type)
		}
	}
}

func TestHub_Broadcast_NoClients_NoPanic(t *testing.T) {
	hub := server.NewHub(nil)
	msg, _ := server.NewWSMessage(server.MsgActivity, map[string]string{"action": "test"})
	hub.Broadcast(msg)
}

func TestHub_MessageTypes_AllDefined(t *testing.T) {
	types := []string{
		server.MsgIssueStateChanged,
		server.MsgBuildEvent,
		server.MsgNewIssue,
		server.MsgActivity,
	}

	for _, typ := range types {
		if typ == "" {
			t.Fatal("message type constant is empty")
		}
	}

	if len(types) != 4 {
		t.Fatalf("expected 4 message types, got %d", len(types))
	}
}

func TestHub_ConcurrentBroadcast_Safe(t *testing.T) {
	hub := server.NewHub(nil)
	ts := httptest.NewServer(http.HandlerFunc(hub.ServeWS))
	defer ts.Close()

	conn := dialWS(t, ts.URL)
	time.Sleep(50 * time.Millisecond)

	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			msg, _ := server.NewWSMessage(server.MsgActivity, map[string]int{"n": i})
			hub.Broadcast(msg)
		})
	}
	wg.Wait()

	received := 0
	for range 10 {
		conn.SetReadDeadline(time.Now().Add(2 * time.Second))
		_, _, err := conn.ReadMessage()
		if err != nil {
			break
		}
		received++
	}

	if received != 10 {
		t.Fatalf("expected 10 messages, got %d", received)
	}
}

func TestServer_WSEndpoint_WithHub(t *testing.T) {
	hub := server.NewHub(nil)
	srv := newTestServer(t, server.Config{Hub: hub})

	wsURL := "ws://" + srv.Addr() + "/api/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("failed to connect to /api/ws: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	if hub.ClientCount() != 1 {
		t.Fatalf("expected 1 client, got %d", hub.ClientCount())
	}

	msg, _ := server.NewWSMessage(server.MsgIssueStateChanged, map[string]string{"state": "building"})
	hub.Broadcast(msg)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("failed to read from /api/ws: %v", err)
	}

	var received server.WSMessage
	if err := json.Unmarshal(raw, &received); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if received.Type != server.MsgIssueStateChanged {
		t.Fatalf("expected type %q, got %q", server.MsgIssueStateChanged, received.Type)
	}
}

func TestServer_WSEndpoint_WithoutHub_Returns404(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/api/ws")
	if err != nil {
		t.Fatalf("GET /api/ws failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected 404 when hub is nil, got %d", resp.StatusCode)
	}
}
