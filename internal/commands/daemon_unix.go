//go:build !windows

package commands

import "syscall"

// detachFromTerminal creates a new session so the daemon is not killed
// when the parent terminal closes.
func detachFromTerminal() {
	syscall.Setsid()
}
