//go:build windows

package main

import (
	"os/exec"
	"syscall"
)

// detachSysProcAttr configures the subprocess to survive the parent
// returning. Minimum-needed flags only — DETACHED_PROCESS is intentionally
// omitted because it disrupts Go's stdin pipe inheritance, and judge
// receives its payload via stdin.
//   CREATE_NEW_PROCESS_GROUP (0x00000200) — independent process group
//   CREATE_NO_WINDOW         (0x08000000) — no console flash
func detachSysProcAttr(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		CreationFlags: 0x00000200 | 0x08000000,
	}
}
