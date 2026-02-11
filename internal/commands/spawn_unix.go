//go:build !windows

package commands

import "syscall"

// spawnSysProcAttr returns SysProcAttr for detaching the spawned daemon.
func spawnSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setsid: true}
}
