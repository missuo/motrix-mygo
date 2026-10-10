//go:build !windows

package engine

import "os/exec"

func hideWindow(*exec.Cmd) {}
