package runstate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

const (
	pidFile    = "run.pid"
	statusFile = "run.status.json"
)

// Result represents the outcome of a daemon run.
type Result string

const (
	ResultSuccess   Result = "success"
	ResultFailed    Result = "failed"
	ResultCancelled Result = "cancelled"
)

// Status holds the final state of a completed daemon run.
type Status struct {
	Result    Result    `json:"result"`
	Timestamp time.Time `json:"timestamp"`
	Error     string    `json:"error,omitempty"`
}

// WritePID writes the current process PID to run.pid in workspacePath.
func WritePID(workspacePath string) error {
	pid := os.Getpid()
	return os.WriteFile(filepath.Join(workspacePath, pidFile), []byte(strconv.Itoa(pid)), 0644)
}

// ReadPID reads the PID from run.pid in workspacePath.
func ReadPID(workspacePath string) (int, error) {
	data, err := os.ReadFile(filepath.Join(workspacePath, pidFile))
	if err != nil {
		return 0, fmt.Errorf("reading PID file: %w", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0, fmt.Errorf("parsing PID file: %w", err)
	}
	return pid, nil
}

// IsRunning returns true if a PID file exists in workspacePath and the
// referenced process is alive. If the PID file exists but the process is
// dead, the stale PID file is removed and false is returned.
func IsRunning(workspacePath string) bool {
	pid, err := ReadPID(workspacePath)
	if err != nil {
		return false
	}
	if !processAlive(pid) {
		os.Remove(filepath.Join(workspacePath, pidFile))
		return false
	}
	return true
}

// CleanupPID removes the PID file from workspacePath.
func CleanupPID(workspacePath string) error {
	err := os.Remove(filepath.Join(workspacePath, pidFile))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

// WriteStatus writes a run.status.json file to workspacePath.
func WriteStatus(workspacePath string, status Status) error {
	if status.Timestamp.IsZero() {
		status.Timestamp = time.Now()
	}
	data, err := json.MarshalIndent(status, "", "  ")
	if err != nil {
		return fmt.Errorf("marshaling status: %w", err)
	}
	return os.WriteFile(filepath.Join(workspacePath, statusFile), data, 0644)
}

// ReadStatus reads and returns the Status from run.status.json in workspacePath.
func ReadStatus(workspacePath string) (*Status, error) {
	data, err := os.ReadFile(filepath.Join(workspacePath, statusFile))
	if err != nil {
		return nil, fmt.Errorf("reading status file: %w", err)
	}
	var s Status
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parsing status file: %w", err)
	}
	return &s, nil
}

// processAlive checks whether a process with the given PID is running.
// On Unix, sending signal 0 checks for existence without affecting the process.
func processAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}
