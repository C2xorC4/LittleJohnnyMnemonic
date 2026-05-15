package main

import (
	"fmt"
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

// bufferEntry is a minimal valid buffer file body for use in consolidation gate tests.
const minimalBufferEntry = `---
type: buffer
timestamp: 2026-01-01T00:00:00Z
source: conversation
surprise: 0.5
tags: [test]
---
test body`

// writeBufferEntries populates vault/Buffer with n synthetic entries.
func writeBufferEntries(t *testing.T, vault string, n int) {
	t.Helper()
	dir := filepath.Join(vault, "Buffer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := range n {
		name := fmt.Sprintf("2026-01-01_test-%02d.md", i)
		if err := os.WriteFile(filepath.Join(dir, name), []byte(minimalBufferEntry), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestSpawnConsolidationIfNeeded_KillSwitchDisables verifies that setting
// AutoConsolidationEnabled=false prevents consolidation from firing even when
// the buffer is above threshold and no cooldown applies.
func TestSpawnConsolidationIfNeeded_KillSwitchDisables(t *testing.T) {
	vault := t.TempDir()
	writeBufferEntries(t, vault, 5)

	cfg := DefaultConfig()
	cfg.BufferThreshold = 1
	cfg.AutoConsolidationEnabled = false
	cfg.AutoConsolidationCooldownMinutes = 0

	spawnConsolidationIfNeeded(vault, cfg)

	triggerPath := filepath.Join(vault, "Metrics", "auto_consolidation_trigger.json")
	if _, err := os.Stat(triggerPath); !os.IsNotExist(err) {
		t.Error("trigger file should NOT exist when kill switch is false")
	}
}

// TestSpawnConsolidationIfNeeded_KillSwitchWithSuspension verifies that when
// the kill switch is false AND a suspension record exists, the reminder message
// path is exercised without panicking (smoke test — we can't capture stderr
// without refactoring, so we just verify no trigger file is written).
func TestSpawnConsolidationIfNeeded_KillSwitchWithSuspension(t *testing.T) {
	vault := t.TempDir()
	writeBufferEntries(t, vault, 5)
	writeAutoConsolidationSuspended(vault, "both-sentinels-missing", time.Now())

	cfg := DefaultConfig()
	cfg.BufferThreshold = 1
	cfg.AutoConsolidationEnabled = false
	cfg.AutoConsolidationCooldownMinutes = 0

	spawnConsolidationIfNeeded(vault, cfg)

	triggerPath := filepath.Join(vault, "Metrics", "auto_consolidation_trigger.json")
	if _, err := os.Stat(triggerPath); !os.IsNotExist(err) {
		t.Error("trigger file should NOT exist when kill switch is false")
	}
}

// TestSpawnConsolidationIfNeeded_SuspensionFileBlocks verifies that an existing
// suspension record prevents consolidation and does not overwrite itself.
func TestSpawnConsolidationIfNeeded_SuspensionFileBlocks(t *testing.T) {
	vault := t.TempDir()
	writeBufferEntries(t, vault, 5)

	suspendedAt := time.Now().Add(-1 * time.Hour).UTC().Truncate(time.Second)
	writeAutoConsolidationSuspended(vault, "both-sentinels-missing", suspendedAt)

	// Give it a recent backup so the both-missing guard does not re-fire.
	recordLastBackup(vault, time.Now())

	cfg := DefaultConfig()
	cfg.BufferThreshold = 1
	cfg.AutoConsolidationEnabled = true
	cfg.AutoConsolidationCooldownMinutes = 0

	spawnConsolidationIfNeeded(vault, cfg)

	triggerPath := filepath.Join(vault, "Metrics", "auto_consolidation_trigger.json")
	if _, err := os.Stat(triggerPath); !os.IsNotExist(err) {
		t.Error("trigger file should NOT exist when suspension record is present")
	}

	// Suspension record should be unchanged.
	suspended, reason, since := readAutoConsolidationSuspended(vault)
	if !suspended {
		t.Fatal("suspension record should still be present")
	}
	if reason != "both-sentinels-missing" {
		t.Errorf("reason changed: got %q", reason)
	}
	if !since.Equal(suspendedAt) {
		t.Errorf("suspended_at changed: got %v, want %v", since, suspendedAt)
	}
}

// TestSpawnConsolidationIfNeeded_BothMissingWritesSuspension verifies that when
// both sentinels are absent, the both-missing guard writes the suspension record
// and does not fire consolidation.
func TestSpawnConsolidationIfNeeded_BothMissingWritesSuspension(t *testing.T) {
	vault := t.TempDir()
	writeBufferEntries(t, vault, 5)

	cfg := DefaultConfig()
	cfg.BufferThreshold = 1
	cfg.AutoConsolidationEnabled = true
	cfg.AutoConsolidationCooldownMinutes = 0

	// Both sentinel files are absent — neither trigger nor backup has ever run.
	spawnConsolidationIfNeeded(vault, cfg)

	triggerPath := filepath.Join(vault, "Metrics", "auto_consolidation_trigger.json")
	if _, err := os.Stat(triggerPath); !os.IsNotExist(err) {
		t.Error("trigger file should NOT be written when both-missing guard fires")
	}

	suspended, reason, since := readAutoConsolidationSuspended(vault)
	if !suspended {
		t.Fatal("suspension record should have been written by both-missing guard")
	}
	if reason != "both-sentinels-missing" {
		t.Errorf("unexpected reason: %q", reason)
	}
	if since.IsZero() {
		t.Error("suspended_at should not be zero")
	}
}

// TestSpawnConsolidationIfNeeded_DeleteSuspensionReenables verifies that
// removing the suspension file allows consolidation to proceed normally.
func TestSpawnConsolidationIfNeeded_DeleteSuspensionReenables(t *testing.T) {
	vault := t.TempDir()
	writeBufferEntries(t, vault, 5)

	// Seed both sentinels so the both-missing guard doesn't re-fire.
	recordAutoConsolidationTrigger(vault, time.Now().Add(-2*time.Hour))
	recordLastBackup(vault, time.Now().Add(-1*time.Hour))

	// Write then delete the suspension record — simulates user re-enable.
	writeAutoConsolidationSuspended(vault, "both-sentinels-missing", time.Now().Add(-30*time.Minute))
	if err := os.Remove(autoConsolidationSuspendedPath(vault)); err != nil {
		t.Fatal(err)
	}

	cfg := DefaultConfig()
	cfg.BufferThreshold = 1
	cfg.AutoConsolidationEnabled = true
	cfg.AutoConsolidationCooldownMinutes = 0

	spawnConsolidationIfNeeded(vault, cfg)

	// The trigger timestamp should be updated (consolidation was allowed to proceed).
	got := readLastAutoConsolidationTrigger(vault)
	if got.Before(time.Now().Add(-5 * time.Second)) {
		t.Errorf("trigger timestamp not updated after re-enable: %v", got)
	}
}

// TestWriteAndReadAutoConsolidationSuspended verifies round-trip of the
// suspension sentinel.
func TestWriteAndReadAutoConsolidationSuspended(t *testing.T) {
	vault := t.TempDir()

	suspended, _, _ := readAutoConsolidationSuspended(vault)
	if suspended {
		t.Error("expected no suspension before any write")
	}

	now := time.Now().UTC().Truncate(time.Second)
	writeAutoConsolidationSuspended(vault, "test-reason", now)

	suspended, reason, since := readAutoConsolidationSuspended(vault)
	if !suspended {
		t.Fatal("expected suspended=true after write")
	}
	if reason != "test-reason" {
		t.Errorf("reason: got %q, want %q", reason, "test-reason")
	}
	if !since.Equal(now) {
		t.Errorf("since: got %v, want %v", since, now)
	}
}
