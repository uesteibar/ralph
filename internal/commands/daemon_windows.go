//go:build windows

package commands

// detachFromTerminal is a no-op on Windows.
func detachFromTerminal() {}
