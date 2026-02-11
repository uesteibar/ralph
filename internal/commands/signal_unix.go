//go:build !windows

package commands

import (
	"os"
	"syscall"
)

// sendTermSignal sends SIGTERM to a process.
func sendTermSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}

// sendKillSignal sends SIGKILL to a process.
func sendKillSignal(proc *os.Process) error {
	return proc.Signal(syscall.SIGKILL)
}
