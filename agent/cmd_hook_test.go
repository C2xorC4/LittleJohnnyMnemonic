package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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
