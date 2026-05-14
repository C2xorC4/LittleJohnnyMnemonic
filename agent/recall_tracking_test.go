package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeRecallResult(memType MemoryType, fileName string) AssociatedMemory {
	return AssociatedMemory{
		Memory: &MemoryEntry{Type: memType, FileName: fileName},
	}
}

var testRecallResults = []AssociatedMemory{
	makeRecallResult(TypeFeedback, "Memory/Feedback/repo_trust_protocol.md"),
	makeRecallResult(TypeFeedback, "Memory/Feedback/tuning_heuristics.md"),
	makeRecallResult(TypeSemantic, "Memory/Semantic/ljm_design_history.md"),
	makeRecallResult(TypeUser, "Memory/User/user_profile.md"),
}

// --- writeRecallConsole ---

func TestWriteRecallConsole_Summary(t *testing.T) {
	var buf bytes.Buffer
	writeRecallConsole(&buf, testRecallResults, "summary")
	out := buf.String()

	if !strings.Contains(out, "[recall]") {
		t.Errorf("missing [recall] prefix: %q", out)
	}
	if !strings.Contains(out, "feedback:2") {
		t.Errorf("expected feedback:2 in %q", out)
	}
	if !strings.Contains(out, "semantic:1") {
		t.Errorf("expected semantic:1 in %q", out)
	}
	if !strings.Contains(out, "user:1") {
		t.Errorf("expected user:1 in %q", out)
	}
	if !strings.Contains(out, "total:4") {
		t.Errorf("expected total:4 in %q", out)
	}
	// Summary must not contain slug names
	if strings.Contains(out, "repo_trust_protocol") {
		t.Errorf("summary mode must not include slug names: %q", out)
	}
}

func TestWriteRecallConsole_Verbose(t *testing.T) {
	var buf bytes.Buffer
	writeRecallConsole(&buf, testRecallResults, "verbose")
	out := buf.String()

	if !strings.Contains(out, "[recall]") {
		t.Errorf("missing [recall] prefix: %q", out)
	}
	if !strings.Contains(out, "repo_trust_protocol") {
		t.Errorf("expected repo_trust_protocol slug in verbose output: %q", out)
	}
	if !strings.Contains(out, "tuning_heuristics") {
		t.Errorf("expected tuning_heuristics slug in verbose output: %q", out)
	}
	if !strings.Contains(out, "ljm_design_history") {
		t.Errorf("expected ljm_design_history slug in verbose output: %q", out)
	}
	if !strings.Contains(out, "total:4") {
		t.Errorf("expected total:4 in %q", out)
	}
}

func TestWriteRecallConsole_SortedTypes(t *testing.T) {
	// Type names must appear in alphabetical order for deterministic output.
	var buf bytes.Buffer
	writeRecallConsole(&buf, testRecallResults, "summary")
	out := buf.String()

	fi := strings.Index(out, "feedback")
	si := strings.Index(out, "semantic")
	ui := strings.Index(out, "user")
	if fi < 0 || si < 0 || ui < 0 {
		t.Fatalf("missing type in output: %q", out)
	}
	if !(fi < si && si < ui) {
		t.Errorf("types not in alphabetical order in %q", out)
	}
}

// --- buildRecallLogEntry ---

func TestBuildRecallLogEntry_Summary(t *testing.T) {
	entry := buildRecallLogEntry(testRecallResults, "sess-abc", 200, "summary")

	if entry.Total != 4 {
		t.Errorf("expected total 4, got %d", entry.Total)
	}
	if entry.Counts["feedback"] != 2 {
		t.Errorf("expected feedback:2, got %d", entry.Counts["feedback"])
	}
	if entry.Counts["semantic"] != 1 {
		t.Errorf("expected semantic:1, got %d", entry.Counts["semantic"])
	}
	if entry.SessionID != "sess-abc" {
		t.Errorf("expected session_id sess-abc, got %q", entry.SessionID)
	}
	if entry.PromptLen != 200 {
		t.Errorf("expected prompt_chars 200, got %d", entry.PromptLen)
	}
	if len(entry.Slugs) != 0 {
		t.Errorf("summary mode must not include slugs, got %v", entry.Slugs)
	}
}

func TestBuildRecallLogEntry_Verbose(t *testing.T) {
	entry := buildRecallLogEntry(testRecallResults, "sess-xyz", 100, "verbose")

	if len(entry.Slugs) != 4 {
		t.Errorf("expected 4 slugs, got %d: %v", len(entry.Slugs), entry.Slugs)
	}
	hasSlug := func(slug string) bool {
		for _, s := range entry.Slugs {
			if s == slug {
				return true
			}
		}
		return false
	}
	if !hasSlug("repo_trust_protocol") {
		t.Errorf("expected repo_trust_protocol in slugs: %v", entry.Slugs)
	}
	if !hasSlug("ljm_design_history") {
		t.Errorf("expected ljm_design_history in slugs: %v", entry.Slugs)
	}
}

// --- appendRecallLog ---

func TestAppendRecallLog_CreatesAndAppends(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "recall_log.jsonl")

	e1 := buildRecallLogEntry(testRecallResults[:2], "s1", 50, "summary")
	e2 := buildRecallLogEntry(testRecallResults[2:], "s2", 80, "verbose")

	appendRecallLog(logPath, e1)
	appendRecallLog(logPath, e2)

	data, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatalf("log file not created: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 JSONL lines, got %d", len(lines))
	}

	var parsed recallLogEntry
	if err := json.Unmarshal([]byte(lines[0]), &parsed); err != nil {
		t.Errorf("line 1 not valid JSON: %v", err)
	}
	if parsed.SessionID != "s1" {
		t.Errorf("expected session_id s1, got %q", parsed.SessionID)
	}
	if err := json.Unmarshal([]byte(lines[1]), &parsed); err != nil {
		t.Errorf("line 2 not valid JSON: %v", err)
	}
	if parsed.SessionID != "s2" {
		t.Errorf("expected session_id s2, got %q", parsed.SessionID)
	}
}

func TestAppendRecallLog_MissingDir(t *testing.T) {
	// Should not panic — just log error to stderr and return.
	logPath := filepath.Join(t.TempDir(), "nonexistent", "recall_log.jsonl")
	entry := buildRecallLogEntry(testRecallResults, "s1", 50, "summary")
	appendRecallLog(logPath, entry) // must not panic
}
