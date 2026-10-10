//go:build !windows

package engine

import (
	"os/exec"
	"syscall"
)

func hideWindow(*exec.Cmd) {}

// terminate asks the process to quit, and reports whether it could.
func terminate(cmd *exec.Cmd) bool {
	return cmd.Process.Signal(syscall.SIGTERM) == nil
}
