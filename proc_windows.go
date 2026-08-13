//go:build windows

package main

import (
	"os/exec"
	"strconv"
)

// configureSysProcAttr is a no-op on Windows; process-tree termination is
// handled by taskkill instead of process groups.
func configureSysProcAttr(cmd *exec.Cmd) {}

// terminateProcessTree force-kills the child and all its descendants. Windows
// has no graceful signal for a console process tree, so force is ignored.
func terminateProcessTree(cmd *exec.Cmd, force bool) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	_ = exec.Command("taskkill", "/T", "/F", "/PID", strconv.Itoa(cmd.Process.Pid)).Run()
}
