package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ActivityState captures the most recent real activity in the vault, broken
// down by source so callers can audit what fed into the skip decision.
//
// "Real activity" means user-driven work, NOT autodream output. Specifically:
//   - buffer    → most recent mtime of any .md file under Buffer/, excluding
//                 the Buffer/Daydream/ subtree (daydream entries don't count
//                 as activity since they're produced BY autodream).
//   - heartbeat → timestamp of the last line in Metrics/session_heartbeat.jsonl,
//                 written by the user-prompt-submit and session-start hooks.
//
// LatestActivity is the max of the enabled sources. A zero-valued time means
// no activity was found from any source — treat as "no activity ever recorded."
type ActivityState struct {
	LatestActivity    time.Time
	BufferActivity    time.Time
	HeartbeatActivity time.Time
	SourcesUsed       []string
}

// ReadActivityState scans the configured sources and returns the most recent
// activity timestamp from each. Unknown source names are silently ignored.
// Errors from individual sources are collected as the first-error return —
// the caller can decide whether to treat partial state as fatal or proceed.
func ReadActivityState(vaultRoot string, sources []string) (ActivityState, error) {
	state := ActivityState{}
	var firstErr error

	for _, src := range sources {
		switch src {
		case "buffer":
			t, err := latestNonDaydreamBufferMTime(vaultRoot)
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("buffer activity: %w", err)
			}
			state.BufferActivity = t
			state.SourcesUsed = append(state.SourcesUsed, "buffer")
		case "heartbeat":
			t, err := latestHeartbeat(vaultRoot)
			if err != nil && firstErr == nil {
				firstErr = fmt.Errorf("heartbeat activity: %w", err)
			}
			state.HeartbeatActivity = t
			state.SourcesUsed = append(state.SourcesUsed, "heartbeat")
		}
	}

	if state.BufferActivity.After(state.LatestActivity) {
		state.LatestActivity = state.BufferActivity
	}
	if state.HeartbeatActivity.After(state.LatestActivity) {
		state.LatestActivity = state.HeartbeatActivity
	}

	return state, firstErr
}

// ShouldSkipForActivity returns true when activity within the configured
// window means autodream should hold off. windowMinutes <= 0 disables the
// skip entirely (used by active mode, where the whole point is to fire
// during sessions). A zero LatestActivity means no signal was found, so
// don't skip — fire normally.
//
// Returns the activity age alongside the decision so callers can log a
// useful reason: "skipped (last activity 12m ago, window 60m)".
func ShouldSkipForActivity(state ActivityState, windowMinutes int, now time.Time) (bool, time.Duration) {
	if windowMinutes <= 0 {
		return false, 0
	}
	if state.LatestActivity.IsZero() {
		return false, 0
	}
	age := now.Sub(state.LatestActivity)
	window := time.Duration(windowMinutes) * time.Minute
	return age < window, age
}

// latestNonDaydreamBufferMTime walks Buffer/ and returns the latest mtime of
// any .md file outside the Buffer/Daydream/ subtree. Missing buffer dir is
// not an error — returns zero time.
func latestNonDaydreamBufferMTime(vaultRoot string) (time.Time, error) {
	bufferDir := filepath.Join(vaultRoot, "Buffer")
	var latest time.Time

	err := filepath.WalkDir(bufferDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			// Skip the Daydream subdirectory at any depth — those are autodream's
			// own output and would create a self-reinforcing loop if counted as
			// activity.
			if path != bufferDir && d.Name() == "Daydream" {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.ModTime().After(latest) {
			latest = info.ModTime()
		}
		return nil
	})

	if err != nil && !os.IsNotExist(err) {
		return time.Time{}, err
	}
	return latest, nil
}

// latestHeartbeat parses the timestamp of the last line in
// Metrics/session_heartbeat.jsonl. Missing file or empty file returns zero
// time without error — both are valid "no heartbeat yet" states.
func latestHeartbeat(vaultRoot string) (time.Time, error) {
	path := filepath.Join(vaultRoot, "Metrics", "session_heartbeat.jsonl")
	line, err := readLastJSONLine(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, nil
		}
		return time.Time{}, err
	}
	if len(line) == 0 {
		return time.Time{}, nil
	}

	var rec struct {
		Timestamp string `json:"timestamp"`
	}
	if err := json.Unmarshal(line, &rec); err != nil {
		return time.Time{}, fmt.Errorf("parse heartbeat: %w", err)
	}
	if rec.Timestamp == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, rec.Timestamp)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse heartbeat timestamp: %w", err)
	}
	return t, nil
}

// readLastJSONLine returns the last non-empty line of a file. Reads only the
// trailing 4KB to avoid loading large append-only logs into memory.
// Returns nil with no error if the file is empty or contains only blank lines.
func readLastJSONLine(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	size := info.Size()
	if size == 0 {
		return nil, nil
	}

	const tailWindow = int64(4096)
	bufSize := tailWindow
	if size < bufSize {
		bufSize = size
	}
	buf := make([]byte, bufSize)
	if _, err := f.ReadAt(buf, size-bufSize); err != nil && err != io.EOF {
		return nil, err
	}

	end := len(buf)
	for end > 0 && (buf[end-1] == '\n' || buf[end-1] == '\r') {
		end--
	}
	if end == 0 {
		return nil, nil
	}
	start := bytes.LastIndexByte(buf[:end], '\n')
	if start < 0 {
		return buf[:end], nil
	}
	return buf[start+1 : end], nil
}
