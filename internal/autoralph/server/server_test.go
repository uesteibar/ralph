package server_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/uesteibar/ralph/internal/autoralph/server"
)

func newTestServer(t *testing.T, cfg server.Config) *server.Server {
	t.Helper()
	srv, err := server.New("127.0.0.1:0", cfg)
	if err != nil {
		t.Fatalf("failed to create server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()
	return srv
}

func TestServer_StatusEndpoint_ReturnsOK(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("expected Content-Type application/json, got %s", ct)
	}

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status 'ok', got %q", body["status"])
	}
}

func TestServer_UnknownAPIRoute_Returns404(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/api/nonexistent")
	if err != nil {
		t.Fatalf("GET /api/nonexistent failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("expected status 404, got %d", resp.StatusCode)
	}
}

func TestServer_RootPath_ServesIndexHTML(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	if !strings.Contains(html, "<title>autoralph</title>") {
		t.Fatalf("expected index.html with <title>autoralph</title>, got:\n%s", html[:min(200, len(html))])
	}
}

func TestServer_ClientSideRoute_ServesIndexHTML(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	resp, err := http.Get("http://" + srv.Addr() + "/issues/abc-123")
	if err != nil {
		t.Fatalf("GET /issues/abc-123 failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected status 200, got %d", resp.StatusCode)
	}

	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "text/html") {
		t.Fatalf("expected text/html content type, got %s", ct)
	}

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "<title>autoralph</title>") {
		t.Fatalf("expected index.html content for client-side route")
	}
}

func TestServer_StaticAsset_ServedDirectly(t *testing.T) {
	srv := newTestServer(t, server.Config{})

	// The Vite build creates hashed JS files in /assets/
	resp, err := http.Get("http://" + srv.Addr() + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	// The index.html should reference /assets/*.js
	if !strings.Contains(html, "/assets/") {
		t.Fatalf("expected index.html to reference /assets/, got:\n%s", html)
	}
}

func TestServer_DevMode_ProxiesToBackend(t *testing.T) {
	// Start a mock Vite server
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		w.Write([]byte("<html>vite dev</html>"))
	}))
	defer mock.Close()

	srv := newTestServer(t, server.Config{
		DevMode: true,
		ViteURL: mock.URL,
	})

	resp, err := http.Get("http://" + srv.Addr() + "/")
	if err != nil {
		t.Fatalf("GET / failed: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "vite dev") {
		t.Fatalf("expected proxied response from mock Vite server, got: %s", string(body))
	}
}

func TestServer_DevMode_APINotProxied(t *testing.T) {
	// Start a mock Vite server that would respond with something different
	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("from vite"))
	}))
	defer mock.Close()

	srv := newTestServer(t, server.Config{
		DevMode: true,
		ViteURL: mock.URL,
	})

	// /api/status should still be handled by the Go server
	resp, err := http.Get("http://" + srv.Addr() + "/api/status")
	if err != nil {
		t.Fatalf("GET /api/status failed: %v", err)
	}
	defer resp.Body.Close()

	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if body["status"] != "ok" {
		t.Fatalf("expected status 'ok', got %q", body["status"])
	}
}
