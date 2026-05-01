package main

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Windows Task Scheduler integration for `jm autodream`. Registering the
// task means cron-style polling: schtasks fires `jm autodream` every N
// minutes; jitter, daily caps, mode resolution, and activity-based skip
// are all enforced inside the binary on each invocation.
//
// schtasks command reference:
//   /Create  — creates a new scheduled task
//   /Delete  — removes an existing task
//   /Query   — checks for an existing task (used to confirm install/uninstall)
//   /SC MINUTE /MO N — fire every N minutes
//   /TN <name>       — task name
//   /TR <command>    — what to run
//   /IT              — interactive task; runs only when the user is logged on
//                      (no /RU; default principal is the user creating the task,
//                      so install does not require admin elevation)
//   /F               — force overwrite if exists (install) or skip prompt (delete)
//
// Why no /RU INTERACTIVE: setting the principal to the special INTERACTIVE
// SID is a system-level operation that requires admin elevation. We don't
// need it — claude CLI auth is per-user, and /IT already constrains the
// task to only fire while the creating user is logged on, which is the
// behavior we actually want.

const autodreamTaskName = "JmAutodream"

// schtasksRunner is the function used to invoke schtasks.exe. Production
// code uses defaultSchtasksRunner; tests swap this to capture invocations
// without touching the real Task Scheduler.
var schtasksRunner = defaultSchtasksRunner

func defaultSchtasksRunner(args ...string) (string, error) {
	cmd := exec.Command("schtasks", args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("%w: %s", err, stderrStr)
		}
		return "", err
	}
	return strings.TrimSpace(stdout.String()), nil
}

// installAutodreamSchedule registers the scheduled task. Returns an error
// if schtasks fails. Caller is responsible for the OS check (Windows-only)
// and for logging the result.
//
// Splitting this from cmdAutodream's flag-parsing surface lets tests
// inject a fake schtasksRunner without going through CLI argv.
func installAutodreamSchedule(intervalMinutes int) (string, error) {
	if intervalMinutes <= 0 {
		intervalMinutes = 15
	}
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("cannot resolve jm executable: %w", err)
	}
	exe = filepath.Clean(exe)

	out, err := schtasksRunner(
		"/Create",
		"/SC", "MINUTE",
		"/MO", strconv.Itoa(intervalMinutes),
		"/TN", autodreamTaskName,
		"/TR", exe+" autodream",
		"/IT",
		"/F",
	)
	if err != nil {
		return "", fmt.Errorf("schtasks /Create failed: %w", err)
	}
	return out, nil
}

// uninstallAutodreamSchedule removes the scheduled task. Returns an error
// if schtasks fails OR if the task didn't exist (the caller can decide
// whether that's a failure or a no-op).
func uninstallAutodreamSchedule() (string, error) {
	out, err := schtasksRunner("/Delete", "/TN", autodreamTaskName, "/F")
	if err != nil {
		return "", fmt.Errorf("schtasks /Delete failed: %w", err)
	}
	return out, nil
}

// autodreamScheduleStatus checks whether the task is currently registered.
// Returns (true, nil) if installed, (false, nil) if not, error for other
// failure modes.
func autodreamScheduleStatus() (bool, error) {
	_, err := schtasksRunner("/Query", "/TN", autodreamTaskName)
	if err == nil {
		return true, nil
	}
	// schtasks /Query exits non-zero when the task doesn't exist; the
	// stderr typically contains "ERROR: The system cannot find the file
	// specified." Treat any non-zero as "not installed" rather than fatal.
	return false, nil
}

// requireWindows returns an error on non-Windows platforms. The autodream
// scheduler is Windows-only for v1; cron-based Linux/macOS support can be
// added later by feature-gating on runtime.GOOS.
func requireWindows() error {
	if runtime.GOOS != "windows" {
		return errors.New("autodream scheduler is currently Windows-only (uses schtasks.exe)")
	}
	return nil
}
