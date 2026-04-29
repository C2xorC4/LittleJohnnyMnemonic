//go:build !windows

package main

import (
	"os/exec"
	"syscall"
)

// detachSysProcAttr configures the subprocess to start a new session so
// it survives the hook returning. Setsid detaches from the parent's
// controlling terminal and process group.
func detachSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}
}
