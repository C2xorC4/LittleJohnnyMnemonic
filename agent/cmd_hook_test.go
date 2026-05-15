package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestRecordAndReadAutoConsolidationTrigger verifies the round-trip of the
// cooldown timestamp file used by spawnConsolidationIfNeeded.
func TestRecordAndReadAutoConsolidationTrigger(t *testing.T) {
	vault := t.TempDir()

	// Zero time before any record
	if got := readLastAutoConsolidationTrigger(vault); !got.IsZero() {
		t.Errorf("expected zero time before any record, got %v", got)
	}

	now := time.Now().UTC().Truncate(time.Second)
	recordAutoConsolidationTrigger(vault, now)

	got := readLastAutoConsolidationTrigger(vault)
	if got.IsZero() {
		t.Fatal("expected non-zero time after record")
	}
	if !got.Equal(now) {
		t.Errorf("round-trip mismatch: wrote %v, read %v", now, got)
	}
}

// TestSpawnConsolidationIfNeeded_BelowThreshold verifies that no trigger file
// is written when the buffer count is below the threshold.
func TestSpawnConsolidationIfNeeded_BelowThreshold(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Buffer"), 0o755); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.BufferThreshold = 5
	cfg.AutoConsolidationCooldownMinutes = 60

	// Buffer has 0 entries — below threshold
	spawnConsolidationIfNeeded(vault, cfg)

	triggerPath := filepath.Join(vault, "Metrics", "auto_consolidation_trigger.json")
	if _, err := os.Stat(triggerPath); !os.IsNotExist(err) {
		t.Error("trigger file should NOT exist when buffer is below threshold")
	}
}

// TestSpawnConsolidationIfNeeded_CooldownPreventsRespawn verifies that a
// second call within the cooldown window does not overwrite the trigger file.
func TestSpawnConsolidationIfNeeded_CooldownPreventsRespawn(t *testing.T) {
	vault := t.TempDir()

	cfg := DefaultConfig()
	cfg.BufferThreshold = 1
	cfg.AutoConsolidationCooldownMinutes = 60

	// Record a trigger 5 minutes ago — within the 60-minute cooldown
	recent := time.Now().Add(-5 * time.Minute)
	recordAutoConsolidationTrigger(vault, recent)

	// Write a buffer entry so threshold is crossed
	bufDir := filepath.Join(vault, "Buffer")
	if err := os.MkdirAll(bufDir, 0o755); err != nil {
		t.Fatal(err)
	}
	entry := `---
type: buffer
timestamp: 2026-01-01T00:00:00Z
source: conversation
surprise: 0.5
tags: [test]
---
test body`
	if err := os.WriteFile(filepath.Join(bufDir, "2026-01-01_test.md"), []byte(entry), 0o644); err != nil {
		t.Fatal(err)
	}

	spawnConsolidationIfNeeded(vault, cfg)

	// Trigger file should still hold the original "recent" timestamp, not be
	// overwritten to now.
	got := readLastAutoConsolidationTrigger(vault)
	if got.IsZero() {
		t.Fatal("trigger file unexpectedly missing")
	}
	// Allow 1-second tolerance for truncation
	if got.After(recent.Add(time.Second)) {
		t.Errorf("cooldown not respected: trigger file was overwritten (got %v, want ~%v)", got, recent)
	}
}

// TestWriteSessionHeartbeat_SuppressedWhenAutodreamEnvSet verifies the
// fix for the autodream self-throttling endogeneity loop: when an
// autodream-spawned claude session inherits LJM_AUTODREAM_INVOCATION=1,
// hooks running in that session must NOT write a heartbeat (which
// would otherwise trigger activity_recent skips for the next ~4 polls
// after every fire).
func TestWriteSessionHeartbeat_SuppressedWhenAutodreamEnvSet(t *testing.T) {
	vault := t.TempDir()
	t.Setenv(autodreamInvocationEnvVar, "1")

	err := writeSessionHeartbeat(vault, "test-session", vault, time.Now())
	if err != nil {
		t.Fatalf("writeSessionHeartbeat returned error under suppression: %v", err)
	}

	path := filepath.Join(vault, "Metrics", "session_heartbeat.jsonl")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("heartbeat file should NOT exist when env var is set; stat err = %v", err)
	}
}

// TestWriteSessionHeartbeat_WritesWhenEnvUnset verifies the normal user-
// session path still writes heartbeats. This is the inverse case — without
// the marker env var, the function must persist a record so legitimate
// user activity still gates quiet-mode appropriately.
func TestWriteSessionHeartbeat_WritesWhenEnvUnset(t *testing.T) {
	vault := t.TempDir()
	// Explicitly clear in case the test process inherited it from a parent
	t.Setenv(autodreamInvocationEnvVar, "")

	err := writeSessionHeartbeat(vault, "user-session", vault, time.Now())
	if err != nil {
		t.Fatalf("writeSessionHeartbeat: %v", err)
	}

	path := filepath.Join(vault, "Metrics", "session_heartbeat.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected heartbeat file: %v", err)
	}
	if len(data) == 0 {
		t.Error("heartbeat file is empty; expected one JSONL line")
	}
}

// TestWriteSessionHeartbeat_SuppressedOnlyForExactValue tests that the env
// var must be exactly "1". Other truthy strings (true, yes, etc.) should
// not trigger suppression — we want the marker to be a deliberate sentinel
// not a fuzzy boolean. This protects against accidental suppression from
// unrelated env-var pollution.
func TestWriteSessionHeartbeat_SuppressedOnlyForExactValue(t *testing.T) {
	vault := t.TempDir()
	t.Setenv(autodreamInvocationEnvVar, "true")

	err := writeSessionHeartbeat(vault, "ambiguous-session", vault, time.Now())
	if err != nil {
		t.Fatalf("writeSessionHeartbeat: %v", err)
	}

	path := filepath.Join(vault, "Metrics", "session_heartbeat.jsonl")
	if _, err := os.Stat(path); err != nil {
		t.Errorf("heartbeat file should exist when env var is not exactly '1'; got stat err %v", err)
	}
}
