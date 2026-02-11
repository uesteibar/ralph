package runstate

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestWritePID_WritesCurrentPID(t *testing.T) {
	dir := t.TempDir()

	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	data, err := os.ReadFile(filepath.Join(dir, pidFile))
	if err != nil {
		t.Fatalf("reading pid file: %v", err)
	}

	got, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatalf("parsing pid: %v", err)
	}
	if got != os.Getpid() {
		t.Errorf("PID = %d, want %d", got, os.Getpid())
	}
}

func TestReadPID_ReturnsWrittenPID(t *testing.T) {
	dir := t.TempDir()

	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	pid, err := ReadPID(dir)
	if err != nil {
		t.Fatalf("ReadPID: %v", err)
	}
	if pid != os.Getpid() {
		t.Errorf("PID = %d, want %d", pid, os.Getpid())
	}
}

func TestReadPID_ErrorWhenNoFile(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadPID(dir)
	if err == nil {
		t.Fatal("expected error when PID file missing")
	}
}

func TestIsRunning_TrueForCurrentProcess(t *testing.T) {
	dir := t.TempDir()

	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	if !IsRunning(dir) {
		t.Error("IsRunning = false, want true for current process")
	}
}

func TestIsRunning_FalseWhenNoPIDFile(t *testing.T) {
	dir := t.TempDir()

	if IsRunning(dir) {
		t.Error("IsRunning = true, want false when no PID file exists")
	}
}

func TestIsRunning_CleansUpStalePID(t *testing.T) {
	dir := t.TempDir()

	// Write a PID that definitely doesn't exist (very high number)
	stalePID := 2147483647
	os.WriteFile(filepath.Join(dir, pidFile), []byte(strconv.Itoa(stalePID)), 0644)

	if IsRunning(dir) {
		t.Error("IsRunning = true, want false for dead process")
	}

	// Verify PID file was cleaned up
	if _, err := os.Stat(filepath.Join(dir, pidFile)); !os.IsNotExist(err) {
		t.Error("stale PID file was not cleaned up")
	}
}

func TestCleanupPID_RemovesFile(t *testing.T) {
	dir := t.TempDir()

	if err := WritePID(dir); err != nil {
		t.Fatalf("WritePID: %v", err)
	}

	if err := CleanupPID(dir); err != nil {
		t.Fatalf("CleanupPID: %v", err)
	}

	if _, err := os.Stat(filepath.Join(dir, pidFile)); !os.IsNotExist(err) {
		t.Error("PID file still exists after cleanup")
	}
}

func TestCleanupPID_NoErrorWhenFileAbsent(t *testing.T) {
	dir := t.TempDir()

	if err := CleanupPID(dir); err != nil {
		t.Fatalf("CleanupPID on absent file: %v", err)
	}
}

func TestWriteStatus_Success(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	err := WriteStatus(dir, Status{
		Result:    ResultSuccess,
		Timestamp: ts,
	})
	if err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, err := ReadStatus(dir)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.Result != ResultSuccess {
		t.Errorf("Result = %q, want %q", got.Result, ResultSuccess)
	}
	if !got.Timestamp.Equal(ts) {
		t.Errorf("Timestamp = %v, want %v", got.Timestamp, ts)
	}
	if got.Error != "" {
		t.Errorf("Error = %q, want empty", got.Error)
	}
}

func TestWriteStatus_Failed_WithError(t *testing.T) {
	dir := t.TempDir()
	ts := time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC)

	err := WriteStatus(dir, Status{
		Result:    ResultFailed,
		Timestamp: ts,
		Error:     "quality checks failed",
	})
	if err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, err := ReadStatus(dir)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.Result != ResultFailed {
		t.Errorf("Result = %q, want %q", got.Result, ResultFailed)
	}
	if got.Error != "quality checks failed" {
		t.Errorf("Error = %q, want %q", got.Error, "quality checks failed")
	}
}

func TestWriteStatus_Cancelled(t *testing.T) {
	dir := t.TempDir()

	err := WriteStatus(dir, Status{
		Result:    ResultCancelled,
		Timestamp: time.Date(2025, 6, 15, 10, 30, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, err := ReadStatus(dir)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.Result != ResultCancelled {
		t.Errorf("Result = %q, want %q", got.Result, ResultCancelled)
	}
}

func TestWriteStatus_DefaultTimestamp(t *testing.T) {
	dir := t.TempDir()
	before := time.Now()

	err := WriteStatus(dir, Status{Result: ResultSuccess})
	if err != nil {
		t.Fatalf("WriteStatus: %v", err)
	}

	got, err := ReadStatus(dir)
	if err != nil {
		t.Fatalf("ReadStatus: %v", err)
	}
	if got.Timestamp.Before(before) {
		t.Errorf("Timestamp %v is before call time %v", got.Timestamp, before)
	}
}

func TestReadStatus_ErrorWhenNoFile(t *testing.T) {
	dir := t.TempDir()

	_, err := ReadStatus(dir)
	if err == nil {
		t.Fatal("expected error when status file missing")
	}
}
