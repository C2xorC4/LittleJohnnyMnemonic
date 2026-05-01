package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeJSONLLines fills path with `n` minimal JSONL records of approximate
// `bytesPerLine`. Used to push a file past the rotation threshold cheaply.
func writeJSONLLines(t *testing.T, path string, n int, bytesPerLine int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	pad := strings.Repeat("x", bytesPerLine-30) // approx
	for i := 0; i < n; i++ {
		fmt.Fprintf(f, "{\"i\":%d,\"pad\":\"%s\"}\n", i, pad)
	}
}

func TestRotateJSONLIfNeeded_BelowThresholdNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	writeJSONLLines(t, path, 5, 200)

	if err := rotateJSONLIfNeeded(path, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	// File still exists, no archive created
	if _, err := os.Stat(path); err != nil {
		t.Errorf("file should still exist: %v", err)
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "Archive", "*.jsonl"))
	if len(archives) != 0 {
		t.Errorf("expected no archive, got %d", len(archives))
	}
}

func TestRotateJSONLIfNeeded_AtOrAboveThresholdRotates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	writeJSONLLines(t, path, 100, 200)
	now := time.Date(2026, 5, 1, 14, 30, 0, 0, time.UTC)

	if err := rotateJSONLIfNeeded(path, 100, now); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("source should have been moved")
	}
	archives, _ := filepath.Glob(filepath.Join(dir, "Archive", "log.*.jsonl"))
	if len(archives) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(archives))
	}
	if !strings.Contains(archives[0], "20260501-143000") {
		t.Errorf("archive name should include timestamp: %s", archives[0])
	}
}

func TestRotateJSONLIfNeeded_MissingFileNoOp(t *testing.T) {
	if err := rotateJSONLIfNeeded(filepath.Join(t.TempDir(), "absent.jsonl"), 100, time.Now()); err != nil {
		t.Errorf("err = %v, want nil for missing file", err)
	}
}

func TestRotateJSONLIfNeeded_EmptyFileNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := rotateJSONLIfNeeded(path, 100, time.Now()); err != nil {
		t.Errorf("err = %v, want nil for empty file", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("empty file should not have been moved")
	}
}

func TestRotateJSONLIfNeeded_ZeroThresholdNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	writeJSONLLines(t, path, 1000, 200)

	if err := rotateJSONLIfNeeded(path, 0, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("threshold=0 should disable rotation, got %v", err)
	}
}

func TestRotateJSONLIfNeeded_SizePrefilterShortCircuits(t *testing.T) {
	// Tiny file with many lines (each line is just a few bytes) — the size
	// prefilter sees < threshold * 80 and returns early without scanning.
	// Verify rotation does NOT fire even though the line count exceeds
	// threshold, as long as the file is small.
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	f, _ := os.Create(path)
	for i := 0; i < 200; i++ {
		fmt.Fprintf(f, "{\"i\":%d}\n", i)
	}
	f.Close()

	// 200 lines but file is tiny (~2KB). With threshold=100, size prefilter
	// at 100 * 80 = 8000 bytes means 2KB doesn't trigger the line count, so
	// rotation does NOT fire even though count > threshold.
	if err := rotateJSONLIfNeeded(path, 100, time.Now()); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Errorf("size prefilter should have prevented rotation; file is gone: %v", err)
	}
}

func TestCountJSONLLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.jsonl")
	body := "{\"a\":1}\n{\"b\":2}\n\n{\"c\":3}\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := countJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != 3 {
		t.Errorf("got %d, want 3 (blank line should be skipped)", got)
	}
}

// Integration: heartbeat rotation kicks in when threshold is met.
func TestWriteSessionHeartbeat_RotatesAtThreshold(t *testing.T) {
	vault := t.TempDir()
	// Write a System/Config.md with a low rotation threshold to force rotation.
	if err := os.MkdirAll(filepath.Join(vault, "System"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "```yaml\nauto_daydream_log_rotation_threshold: 5\n```"
	if err := os.WriteFile(filepath.Join(vault, "System", "Config.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	// Pre-fill the heartbeat file with 6 lines, each well above the size prefilter.
	// At threshold=5, file size needs to be >= 5*80 = 400 bytes to trigger
	// the actual line count. Use ~200 bytes per line to ensure we cross.
	path := filepath.Join(vault, "Metrics", "session_heartbeat.jsonl")
	writeJSONLLines(t, path, 6, 200)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := writeSessionHeartbeat(vault, "s", "/c", now); err != nil {
		t.Fatal(err)
	}

	// After rotation: archive exists with 6 entries, fresh file has 1 (the new heartbeat)
	archives, _ := filepath.Glob(filepath.Join(vault, "Metrics", "Archive", "session_heartbeat.*.jsonl"))
	if len(archives) != 1 {
		t.Fatalf("expected 1 archive, got %d", len(archives))
	}
	freshCount, err := countJSONLLines(path)
	if err != nil {
		t.Fatal(err)
	}
	if freshCount != 1 {
		t.Errorf("fresh file = %d entries, want 1 (the heartbeat just written)", freshCount)
	}
}

// Integration: replay log rotation honors threshold.
func TestAppendReplayLog_RotatesAtThreshold(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "System"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "```yaml\nauto_daydream_log_rotation_threshold: 4\n```"
	if err := os.WriteFile(filepath.Join(vault, "System", "Config.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(vault, "Metrics", "replay_log.jsonl")
	writeJSONLLines(t, path, 5, 250)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := appendReplayLog(vault, ReplayLogEntry{
		Timestamp:  now,
		Verdict:    "reinforce",
		RecentPath: "/x",
		StablePath: "/y",
	}); err != nil {
		t.Fatal(err)
	}

	archives, _ := filepath.Glob(filepath.Join(vault, "Metrics", "Archive", "replay_log.*.jsonl"))
	if len(archives) != 1 {
		t.Errorf("expected 1 archive, got %d", len(archives))
	}
	count, _ := countJSONLLines(path)
	if count != 1 {
		t.Errorf("fresh file count = %d, want 1", count)
	}
}

// Integration: autodream log rotation honors threshold.
func TestAppendAutodreamLog_RotatesAtThreshold(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "System"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "```yaml\nauto_daydream_log_rotation_threshold: 3\n```"
	if err := os.WriteFile(filepath.Join(vault, "System", "Config.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(vault, "Metrics", "autodream_log.jsonl")
	writeJSONLLines(t, path, 4, 300)

	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	res := AutodreamRunResult{Decision: decisionFired, Mode: ModeActive}
	if err := appendAutodreamLog(vault, res, now); err != nil {
		t.Fatal(err)
	}

	archives, _ := filepath.Glob(filepath.Join(vault, "Metrics", "Archive", "autodream_log.*.jsonl"))
	if len(archives) != 1 {
		t.Errorf("expected 1 archive, got %d", len(archives))
	}
}
