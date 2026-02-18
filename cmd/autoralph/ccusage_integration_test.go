package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/uesteibar/ralph/internal/autoralph/ccusage"
	"github.com/uesteibar/ralph/internal/autoralph/server"
)

// Compile-time check: ccusage.Poller satisfies server.CCUsageProvider.
var _ server.CCUsageProvider = (*ccusage.Poller)(nil)

// TestIntegration_CCUsage_PollerParsesAndServesViaAPI (IT-001)
// End-to-end: ccusage poller parses real-format output and serves via API.
func TestIntegration_CCUsage_PollerParsesAndServesViaAPI(t *testing.T) {
	// 1. Create a mock ccstats script with known values.
	script := writeMockCCStatsScript(t, `Claude Code Usage Statistics
────────────────────────────────────────────────────────────
5-hour         [██████████░░░░░░░░░░]  50%  resets in 3h 13m
7-day          [████████████████░░░░]  83%  resets in 22h 8m
`)

	// 2. Create poller with mock script and long interval (we only need initial poll).
	p := ccusage.NewPoller(script, time.Hour, testLogger())

	// 3. Start poller and wait for initial poll.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	waitForPollResult(t, p)

	// 4. Verify parsed data via Poller.Current().
	data := p.Current()
	if data == nil {
		t.Fatal("expected non-nil usage data")
	}
	if len(data) != 1 {
		t.Fatalf("expected 1 group, got %d", len(data))
	}
	if data[0].GroupLabel != "Claude Code Usage Statistics" {
		t.Fatalf("expected group label 'Claude Code Usage Statistics', got %q", data[0].GroupLabel)
	}
	if len(data[0].Lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(data[0].Lines))
	}
	if data[0].Lines[0].Label != "5-hour" || data[0].Lines[0].Percentage != 50 {
		t.Fatalf("unexpected line[0]: %+v", data[0].Lines[0])
	}
	if data[0].Lines[1].Label != "7-day" || data[0].Lines[1].Percentage != 83 {
		t.Fatalf("unexpected line[1]: %+v", data[0].Lines[1])
	}

	// 5. Create server with the poller as CCUsageProvider.
	srv, err := server.New("127.0.0.1:0", server.Config{CCUsageProvider: p})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	// 6. Send GET /api/cc-usage and verify response.
	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)

	if result["available"] != true {
		t.Fatalf("expected available=true, got %v", result["available"])
	}

	groups := result["groups"].([]any)
	if len(groups) != 1 {
		t.Fatalf("expected 1 group in response, got %d", len(groups))
	}

	group := groups[0].(map[string]any)
	if group["group_label"] != "Claude Code Usage Statistics" {
		t.Fatalf("expected group_label 'Claude Code Usage Statistics', got %v", group["group_label"])
	}

	lines := group["lines"].([]any)
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}

	line1 := lines[0].(map[string]any)
	if line1["label"] != "5-hour" {
		t.Fatalf("expected label '5-hour', got %v", line1["label"])
	}
	if int(line1["percentage"].(float64)) != 50 {
		t.Fatalf("expected percentage 50, got %v", line1["percentage"])
	}
	if line1["reset_duration"] != "3h 13m" {
		t.Fatalf("expected reset_duration '3h 13m', got %v", line1["reset_duration"])
	}

	line2 := lines[1].(map[string]any)
	if line2["label"] != "7-day" {
		t.Fatalf("expected label '7-day', got %v", line2["label"])
	}
	if int(line2["percentage"].(float64)) != 83 {
		t.Fatalf("expected percentage 83, got %v", line2["percentage"])
	}
	if line2["reset_duration"] != "22h 8m" {
		t.Fatalf("expected reset_duration '22h 8m', got %v", line2["reset_duration"])
	}

	cancel()
	<-done
}

// TestIntegration_CCUsage_GracefulDegradation_BinaryNotFound (IT-002)
// Graceful degradation when ccstats is unavailable.
func TestIntegration_CCUsage_GracefulDegradation_BinaryNotFound(t *testing.T) {
	// 1. Create poller with a non-existent binary.
	p := ccusage.NewPoller("nonexistent-ccstats-binary-xxxxx", time.Hour, testLogger())

	// 2. Start poller — should return immediately.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		p.Start(ctx)
		close(done)
	}()

	select {
	case <-done:
		// Start returned immediately — correct.
	case <-ctx.Done():
		t.Fatal("Start did not return immediately when binary not found")
	}

	// 3. Verify Current() returns nil.
	if got := p.Current(); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}

	// 4. Create server with the poller as CCUsageProvider.
	srv, err := server.New("127.0.0.1:0", server.Config{CCUsageProvider: p})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	// 5. Verify the API returns {available: false}.
	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
}

// TestIntegration_CCUsage_NilProvider (IT-003)
// API returns unavailable when no CCUsageProvider is configured.
func TestIntegration_CCUsage_NilProvider(t *testing.T) {
	// 1. Create server with CCUsageProvider set to nil.
	srv, err := server.New("127.0.0.1:0", server.Config{})
	if err != nil {
		t.Fatalf("creating server: %v", err)
	}
	t.Cleanup(func() { srv.Close() })
	go srv.Serve()

	// 2. Send GET /api/cc-usage.
	resp, err := http.Get("http://" + srv.Addr() + "/api/cc-usage")
	if err != nil {
		t.Fatalf("GET /api/cc-usage failed: %v", err)
	}
	defer resp.Body.Close()

	// 3. Verify response is {available: false} with HTTP 200.
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.StatusCode)
	}

	var result map[string]any
	json.NewDecoder(resp.Body).Decode(&result)
	if result["available"] != false {
		t.Fatalf("expected available=false, got %v", result["available"])
	}
}

// writeMockCCStatsScript creates a temporary executable script that prints the given output.
func writeMockCCStatsScript(t *testing.T, output string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mock-ccstats")
	script := fmt.Sprintf("#!/bin/sh\ncat <<'CCSTATS_EOF'\n%sCCSTATS_EOF\n", output)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("writing mock script: %v", err)
	}
	return path
}

// testLogger returns a quiet logger for tests.
func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

// waitForPollResult polls until Current() returns non-nil or times out.
func waitForPollResult(t *testing.T, p *ccusage.Poller) {
	t.Helper()
	deadline := time.After(5 * time.Second)
	for {
		if p.Current() != nil {
			return
		}
		select {
		case <-deadline:
			t.Fatal("timed out waiting for poller to populate results")
		default:
			time.Sleep(50 * time.Millisecond)
		}
	}
}
