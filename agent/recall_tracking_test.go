package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// --- formatRecallLine ---

func TestWriteRecallConsole_Summary(t *testing.T) {
	var buf bytes.Buffer
	formatRecallLine(&buf, testRecallResults, "summary")
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
	formatRecallLine(&buf, testRecallResults, "verbose")
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
	formatRecallLine(&buf, testRecallResults, "summary")
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

// --- compactRecallLog ---

func writeTestLog(t *testing.T, logPath string, entries []string) {
	t.Helper()
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create log: %v", err)
	}
	defer f.Close()
	for _, e := range entries {
		f.WriteString(e + "\n")
	}
}

func makeGranularJSON(t *testing.T, timestamp, sessionID string, total int, counts map[string]int) string {
	t.Helper()
	e := recallLogEntry{
		Timestamp: timestamp,
		SessionID: sessionID,
		PromptLen: 100,
		Total:     total,
		Counts:    counts,
	}
	b, _ := json.Marshal(e)
	return string(b)
}

func makeDailyJSON(t *testing.T, date string, prompts, totalRecalls int, counts map[string]int) string {
	t.Helper()
	d := recallDayEntry{
		Date:         date,
		Granularity:  "day",
		Prompts:      prompts,
		TotalRecalls: totalRecalls,
		AvgTotal:     float64(totalRecalls) / float64(prompts),
		Counts:       counts,
	}
	b, _ := json.Marshal(d)
	return string(b)
}

func TestCompactRecallLog_NonExistentFile(t *testing.T) {
	n, err := compactRecallLog(filepath.Join(t.TempDir(), "nope.jsonl"), 30, false)
	if err != nil {
		t.Errorf("expected nil error for missing file, got: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 compacted, got %d", n)
	}
}

func TestCompactRecallLog_NothingToCompact(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "recall_log.jsonl")

	// Write two entries timestamped "now" — both within the 30-day window.
	now := time.Now().UTC().Format(time.RFC3339)
	lines := []string{
		makeGranularJSON(t, now, "s1", 3, map[string]int{"feedback": 2, "user": 1}),
		makeGranularJSON(t, now, "s2", 2, map[string]int{"semantic": 2}),
	}
	writeTestLog(t, logPath, lines)

	n, err := compactRecallLog(logPath, 30, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 0 {
		t.Errorf("expected 0 compacted, got %d", n)
	}

	// File should be unchanged (both lines preserved).
	data, _ := os.ReadFile(logPath)
	got := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(got) != 2 {
		t.Errorf("expected 2 lines unchanged, got %d", len(got))
	}
}

func TestCompactRecallLog_CompactsOldEntries(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "recall_log.jsonl")

	old := time.Now().UTC().AddDate(0, 0, -60).Format(time.RFC3339)
	recent := time.Now().UTC().Format(time.RFC3339)

	lines := []string{
		makeGranularJSON(t, old, "s1", 3, map[string]int{"feedback": 2, "user": 1}),
		makeGranularJSON(t, old, "s2", 2, map[string]int{"feedback": 1, "semantic": 1}),
		makeGranularJSON(t, recent, "s3", 4, map[string]int{"semantic": 4}),
	}
	writeTestLog(t, logPath, lines)

	n, err := compactRecallLog(logPath, 30, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 2 {
		t.Errorf("expected 2 old entries compacted, got %d", n)
	}

	data, _ := os.ReadFile(logPath)
	resultLines := strings.Split(strings.TrimSpace(string(data)), "\n")

	// Expect: 1 daily aggregate + 1 recent granular = 2 lines.
	if len(resultLines) != 2 {
		t.Fatalf("expected 2 output lines (1 daily + 1 recent), got %d: %v", len(resultLines), resultLines)
	}

	// First line must be a daily entry.
	var day recallDayEntry
	if err := json.Unmarshal([]byte(resultLines[0]), &day); err != nil {
		t.Fatalf("first line not a daily entry: %v", err)
	}
	if day.Granularity != "day" {
		t.Errorf("expected granularity=day, got %q", day.Granularity)
	}
	if day.Prompts != 2 {
		t.Errorf("expected 2 prompts in daily, got %d", day.Prompts)
	}
	if day.TotalRecalls != 5 {
		t.Errorf("expected total_recalls=5, got %d", day.TotalRecalls)
	}
	if day.Counts["feedback"] != 3 {
		t.Errorf("expected feedback:3 in daily counts, got %d", day.Counts["feedback"])
	}

	// Second line must be the recent granular entry.
	var granular recallLogEntry
	if err := json.Unmarshal([]byte(resultLines[1]), &granular); err != nil {
		t.Fatalf("second line not a granular entry: %v", err)
	}
	if granular.SessionID != "s3" {
		t.Errorf("expected session_id s3, got %q", granular.SessionID)
	}
}

func TestCompactRecallLog_DryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "recall_log.jsonl")

	old := time.Now().UTC().AddDate(0, 0, -60).Format(time.RFC3339)
	lines := []string{
		makeGranularJSON(t, old, "s1", 3, map[string]int{"feedback": 3}),
	}
	writeTestLog(t, logPath, lines)
	originalData, _ := os.ReadFile(logPath)

	n, err := compactRecallLog(logPath, 30, true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 reported even in dry-run, got %d", n)
	}

	afterData, _ := os.ReadFile(logPath)
	if string(originalData) != string(afterData) {
		t.Error("dry-run must not modify the file")
	}
}

func TestCompactRecallLog_MergesExistingDailyEntry(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "recall_log.jsonl")

	old := time.Now().UTC().AddDate(0, 0, -60)
	oldDate := old.UTC().Format("2006-01-02")
	oldTS := old.Format(time.RFC3339)

	// Pre-existing daily aggregate for the same date.
	existingDaily := makeDailyJSON(t, oldDate, 1, 2, map[string]int{"feedback": 2})
	// New granular entry for the same date.
	newGranular := makeGranularJSON(t, oldTS, "s1", 3, map[string]int{"user": 3})

	writeTestLog(t, logPath, []string{existingDaily, newGranular})

	n, err := compactRecallLog(logPath, 30, false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 1 {
		t.Errorf("expected 1 granular entry compacted, got %d", n)
	}

	data, _ := os.ReadFile(logPath)
	resultLines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(resultLines) != 1 {
		t.Fatalf("expected 1 merged daily entry, got %d: %v", len(resultLines), resultLines)
	}

	var day recallDayEntry
	if err := json.Unmarshal([]byte(resultLines[0]), &day); err != nil {
		t.Fatalf("not a valid daily entry: %v", err)
	}
	if day.Prompts != 2 {
		t.Errorf("expected 2 prompts after merge (1 existing + 1 new), got %d", day.Prompts)
	}
	if day.TotalRecalls != 5 {
		t.Errorf("expected total_recalls=5 after merge (2+3), got %d", day.TotalRecalls)
	}
	if day.Counts["feedback"] != 2 || day.Counts["user"] != 3 {
		t.Errorf("expected merged counts feedback:2 user:3, got %v", day.Counts)
	}
}

// --- aggregateDayEntry ---

func TestAggregateDayEntry(t *testing.T) {
	entries := []recallLogEntry{
		{Timestamp: "2026-01-01T10:00:00Z", SessionID: "a", Total: 3, Counts: map[string]int{"feedback": 2, "user": 1}},
		{Timestamp: "2026-01-01T14:00:00Z", SessionID: "b", Total: 2, Counts: map[string]int{"semantic": 2}},
	}
	day := aggregateDayEntry("2026-01-01", entries)

	if day.Date != "2026-01-01" {
		t.Errorf("expected date 2026-01-01, got %q", day.Date)
	}
	if day.Granularity != "day" {
		t.Errorf("expected granularity=day, got %q", day.Granularity)
	}
	if day.Prompts != 2 {
		t.Errorf("expected 2 prompts, got %d", day.Prompts)
	}
	if day.TotalRecalls != 5 {
		t.Errorf("expected total_recalls=5, got %d", day.TotalRecalls)
	}
	if day.AvgTotal != 2.5 {
		t.Errorf("expected avg_total=2.5, got %f", day.AvgTotal)
	}
	if day.Counts["feedback"] != 2 || day.Counts["user"] != 1 || day.Counts["semantic"] != 2 {
		t.Errorf("unexpected counts: %v", day.Counts)
	}
}

// --- mergeDayEntries ---

func TestMergeDayEntries(t *testing.T) {
	a := recallDayEntry{
		Date: "2026-01-01", Granularity: "day",
		Prompts: 2, TotalRecalls: 5, AvgTotal: 2.5,
		Counts: map[string]int{"feedback": 3, "user": 2},
	}
	b := recallDayEntry{
		Date: "2026-01-01", Granularity: "day",
		Prompts: 3, TotalRecalls: 6, AvgTotal: 2.0,
		Counts: map[string]int{"feedback": 1, "semantic": 5},
	}
	m := mergeDayEntries(a, b)

	if m.Prompts != 5 {
		t.Errorf("expected 5 prompts, got %d", m.Prompts)
	}
	if m.TotalRecalls != 11 {
		t.Errorf("expected total_recalls=11, got %d", m.TotalRecalls)
	}
	if m.AvgTotal != 2.2 {
		t.Errorf("expected avg_total=2.2, got %f", m.AvgTotal)
	}
	if m.Counts["feedback"] != 4 {
		t.Errorf("expected feedback:4, got %d", m.Counts["feedback"])
	}
	if m.Counts["user"] != 2 {
		t.Errorf("expected user:2, got %d", m.Counts["user"])
	}
	if m.Counts["semantic"] != 5 {
		t.Errorf("expected semantic:5, got %d", m.Counts["semantic"])
	}
	if m.Date != "2026-01-01" {
		t.Errorf("expected date preserved, got %q", m.Date)
	}
}
