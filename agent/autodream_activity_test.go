package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestWriteSessionHeartbeat_CreatesFileAndAppends(t *testing.T) {
	vault := t.TempDir()

	t1 := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	if err := writeSessionHeartbeat(vault, "session-1", "/cwd/a", t1); err != nil {
		t.Fatalf("first heartbeat: %v", err)
	}

	t2 := time.Date(2026, 4, 30, 10, 5, 0, 0, time.UTC)
	if err := writeSessionHeartbeat(vault, "session-2", "/cwd/b", t2); err != nil {
		t.Fatalf("second heartbeat: %v", err)
	}

	path := filepath.Join(vault, "Metrics", "session_heartbeat.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read heartbeat file: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), data)
	}

	var rec1, rec2 struct {
		Timestamp string `json:"timestamp"`
		SessionID string `json:"session_id"`
		Cwd       string `json:"cwd"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &rec1); err != nil {
		t.Fatalf("parse line 1: %v", err)
	}
	if err := json.Unmarshal([]byte(lines[1]), &rec2); err != nil {
		t.Fatalf("parse line 2: %v", err)
	}
	if rec1.SessionID != "session-1" || rec1.Cwd != "/cwd/a" {
		t.Errorf("line 1 = %+v", rec1)
	}
	if rec2.SessionID != "session-2" || rec2.Cwd != "/cwd/b" {
		t.Errorf("line 2 = %+v", rec2)
	}
}

func TestReadActivityState_NoSourcesNoFiles(t *testing.T) {
	vault := t.TempDir()
	state, err := ReadActivityState(vault, []string{"buffer", "heartbeat"})
	if err != nil {
		t.Fatalf("ReadActivityState: %v", err)
	}
	if !state.LatestActivity.IsZero() {
		t.Errorf("expected zero LatestActivity, got %v", state.LatestActivity)
	}
	wantSources := []string{"buffer", "heartbeat"}
	if !reflect.DeepEqual(state.SourcesUsed, wantSources) {
		t.Errorf("SourcesUsed = %v, want %v", state.SourcesUsed, wantSources)
	}
}

func TestReadActivityState_BufferActivityExcludesDaydream(t *testing.T) {
	vault := t.TempDir()

	// Create Buffer/ with a top-level entry and a Daydream/ entry
	if err := os.MkdirAll(filepath.Join(vault, "Buffer", "Daydream"), 0o755); err != nil {
		t.Fatal(err)
	}
	topLevel := filepath.Join(vault, "Buffer", "user-said-something.md")
	daydream := filepath.Join(vault, "Buffer", "Daydream", "wandered-somewhere.md")
	if err := os.WriteFile(topLevel, []byte("---\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(daydream, []byte("---\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Set explicit mtimes — daydream is more recent, but should NOT be picked
	old := time.Date(2026, 4, 28, 10, 0, 0, 0, time.UTC)
	recent := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	if err := os.Chtimes(topLevel, old, old); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(daydream, recent, recent); err != nil {
		t.Fatal(err)
	}

	state, err := ReadActivityState(vault, []string{"buffer"})
	if err != nil {
		t.Fatalf("ReadActivityState: %v", err)
	}
	if !state.BufferActivity.Equal(old) {
		t.Errorf("BufferActivity = %v, want %v (Daydream/ should have been excluded)", state.BufferActivity, old)
	}
	if !state.LatestActivity.Equal(old) {
		t.Errorf("LatestActivity = %v, want %v", state.LatestActivity, old)
	}
}

func TestReadActivityState_HeartbeatLatestLineWins(t *testing.T) {
	vault := t.TempDir()

	t1 := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 4, 30, 10, 30, 0, 0, time.UTC)
	t3 := time.Date(2026, 4, 30, 11, 0, 0, 0, time.UTC)
	for _, ts := range []time.Time{t1, t2, t3} {
		if err := writeSessionHeartbeat(vault, "s", "/c", ts); err != nil {
			t.Fatal(err)
		}
	}

	state, err := ReadActivityState(vault, []string{"heartbeat"})
	if err != nil {
		t.Fatalf("ReadActivityState: %v", err)
	}
	if !state.HeartbeatActivity.Equal(t3) {
		t.Errorf("HeartbeatActivity = %v, want %v", state.HeartbeatActivity, t3)
	}
}

func TestReadActivityState_TakesMaxAcrossSources(t *testing.T) {
	vault := t.TempDir()

	// Buffer mtime older
	if err := os.MkdirAll(filepath.Join(vault, "Buffer"), 0o755); err != nil {
		t.Fatal(err)
	}
	bufFile := filepath.Join(vault, "Buffer", "old.md")
	if err := os.WriteFile(bufFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bufTime := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(bufFile, bufTime, bufTime); err != nil {
		t.Fatal(err)
	}

	// Heartbeat newer
	hbTime := time.Date(2026, 4, 30, 11, 0, 0, 0, time.UTC)
	if err := writeSessionHeartbeat(vault, "s", "/c", hbTime); err != nil {
		t.Fatal(err)
	}

	state, err := ReadActivityState(vault, []string{"buffer", "heartbeat"})
	if err != nil {
		t.Fatalf("ReadActivityState: %v", err)
	}
	if !state.LatestActivity.Equal(hbTime) {
		t.Errorf("LatestActivity = %v, want %v (heartbeat should win)", state.LatestActivity, hbTime)
	}
}

func TestReadActivityState_SourcesOptOut(t *testing.T) {
	vault := t.TempDir()

	// Buffer is recent, heartbeat is recent — but only request "buffer"
	if err := os.MkdirAll(filepath.Join(vault, "Buffer"), 0o755); err != nil {
		t.Fatal(err)
	}
	bufFile := filepath.Join(vault, "Buffer", "x.md")
	if err := os.WriteFile(bufFile, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	bufTime := time.Date(2026, 4, 30, 9, 0, 0, 0, time.UTC)
	if err := os.Chtimes(bufFile, bufTime, bufTime); err != nil {
		t.Fatal(err)
	}
	hbTime := time.Date(2026, 4, 30, 11, 0, 0, 0, time.UTC)
	if err := writeSessionHeartbeat(vault, "s", "/c", hbTime); err != nil {
		t.Fatal(err)
	}

	state, err := ReadActivityState(vault, []string{"buffer"})
	if err != nil {
		t.Fatalf("ReadActivityState: %v", err)
	}
	if !state.BufferActivity.Equal(bufTime) {
		t.Errorf("BufferActivity = %v, want %v", state.BufferActivity, bufTime)
	}
	if !state.HeartbeatActivity.IsZero() {
		t.Errorf("HeartbeatActivity = %v, want zero (heartbeat not requested)", state.HeartbeatActivity)
	}
	if !state.LatestActivity.Equal(bufTime) {
		t.Errorf("LatestActivity = %v, want %v", state.LatestActivity, bufTime)
	}
	if !reflect.DeepEqual(state.SourcesUsed, []string{"buffer"}) {
		t.Errorf("SourcesUsed = %v, want [buffer]", state.SourcesUsed)
	}
}

func TestReadActivityState_UnknownSourceIgnored(t *testing.T) {
	vault := t.TempDir()
	state, err := ReadActivityState(vault, []string{"buffer", "unknown-thing", "heartbeat"})
	if err != nil {
		t.Fatalf("ReadActivityState: %v", err)
	}
	sort.Strings(state.SourcesUsed)
	want := []string{"buffer", "heartbeat"}
	if !reflect.DeepEqual(state.SourcesUsed, want) {
		t.Errorf("SourcesUsed = %v, want %v (unknown should be silently skipped)", state.SourcesUsed, want)
	}
}

func TestShouldSkipForActivity_WindowZeroNeverSkips(t *testing.T) {
	now := time.Now()
	state := ActivityState{LatestActivity: now} // activity at the exact moment
	skip, _ := ShouldSkipForActivity(state, 0, now)
	if skip {
		t.Error("window=0 should never skip")
	}
}

func TestShouldSkipForActivity_NoActivityDoesNotSkip(t *testing.T) {
	now := time.Now()
	state := ActivityState{} // zero LatestActivity
	skip, _ := ShouldSkipForActivity(state, 60, now)
	if skip {
		t.Error("zero LatestActivity should not skip")
	}
}

func TestShouldSkipForActivity_InsideWindowSkips(t *testing.T) {
	now := time.Now()
	state := ActivityState{LatestActivity: now.Add(-30 * time.Minute)}
	skip, age := ShouldSkipForActivity(state, 60, now)
	if !skip {
		t.Error("activity 30min ago with 60min window should skip")
	}
	wantAge := 30 * time.Minute
	delta := age - wantAge
	if delta < -time.Second || delta > time.Second {
		t.Errorf("age = %v, want ≈30m", age)
	}
}

func TestShouldSkipForActivity_OutsideWindowDoesNotSkip(t *testing.T) {
	now := time.Now()
	state := ActivityState{LatestActivity: now.Add(-90 * time.Minute)}
	skip, _ := ShouldSkipForActivity(state, 60, now)
	if skip {
		t.Error("activity 90min ago with 60min window should not skip")
	}
}

func TestReadLastJSONLine_HandlesTrailingNewlines(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "log.jsonl")
	body := "{\"a\":1}\n{\"b\":2}\n\n\n"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := readLastJSONLine(tmp)
	if err != nil {
		t.Fatalf("readLastJSONLine: %v", err)
	}
	if string(line) != "{\"b\":2}" {
		t.Errorf("got %q, want {\"b\":2}", line)
	}
}

func TestReadLastJSONLine_EmptyFile(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(tmp, []byte(""), 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := readLastJSONLine(tmp)
	if err != nil {
		t.Fatalf("readLastJSONLine: %v", err)
	}
	if line != nil {
		t.Errorf("got %q, want nil", line)
	}
}

func TestReadLastJSONLine_SingleLineNoTrailingNewline(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "log.jsonl")
	if err := os.WriteFile(tmp, []byte("{\"only\":1}"), 0o644); err != nil {
		t.Fatal(err)
	}
	line, err := readLastJSONLine(tmp)
	if err != nil {
		t.Fatalf("readLastJSONLine: %v", err)
	}
	if string(line) != "{\"only\":1}" {
		t.Errorf("got %q", line)
	}
}
