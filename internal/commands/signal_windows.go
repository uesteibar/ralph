//go:build windows

package commands

import "os"

// sendTermSignal kills the process on Windows (no SIGTERM equivalent).
func sendTermSignal(proc *os.Process) error {
	return proc.Kill()
}

// sendKillSignal kills the process on Windows.
func sendKillSignal(proc *os.Process) error {
	return proc.Kill()
}
