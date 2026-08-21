//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// configureSysProcAttr puts the child into its own process group on Unix so the
// whole tree (npm -> pnpm -> node -> dsh) can be signalled together.
func configureSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// terminateProcessTree signals the child's process group: SIGTERM normally,
// SIGKILL when force is true.
func terminateProcessTree(cmd *exec.Cmd, force bool) {
	if cmd == nil || cmd.Process == nil {
		return
	}
	sig := syscall.SIGTERM
	if force {
		sig = syscall.SIGKILL
	}
	if pgid, err := syscall.Getpgid(cmd.Process.Pid); err == nil {
		_ = syscall.Kill(-pgid, sig)
	} else {
		_ = cmd.Process.Signal(sig)
	}
}
