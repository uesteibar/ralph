//go:build windows

package commands

import "syscall"

// spawnSysProcAttr returns SysProcAttr for Windows (no Setsid).
func spawnSysProcAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{}
}
