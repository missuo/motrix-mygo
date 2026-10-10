package engine

import (
	"os/exec"
	"syscall"
)

// hideWindow keeps aria2c from opening a console window.
func hideWindow(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
}
