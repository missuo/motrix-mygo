package engine

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps aria2c from opening a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}

// terminate asks the process to quit, and reports whether it could:
// Windows has no signal for it.
func terminate(*exec.Cmd) bool { return false }
