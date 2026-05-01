package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestQueueTypeFor_ClassifiesByKindAndPriority(t *testing.T) {
	cases := []struct {
		entry *BufferEntry
		want  string
	}{
		{&BufferEntry{DaydreamKind: "replay-contradict"}, "critical"},
		{&BufferEntry{Priority: "critical"}, "critical"},
		{&BufferEntry{Priority: "Critical"}, "critical"}, // case-insensitive
		{&BufferEntry{DaydreamKind: "replay-refine"}, "refine"},
		{&BufferEntry{DaydreamKind: "exploration"}, "exploration"},
		{&BufferEntry{DaydreamKind: ""}, ""},
		{nil, ""},
	}
	for i, c := range cases {
		got := queueTypeFor(c.entry)
		if got != c.want {
			t.Errorf("[%d] queueTypeFor=%q, want %q", i, got, c.want)
		}
	}
}

func TestBuildReviewActionRecord_FullPopulation(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	entry := &BufferEntry{
		FileName:     "x.md",
		FilePath:     "/v/Buffer/Daydream/x.md",
		DaydreamKind: "replay-contradict",
		Priority:     "high",
		Surprise:     0.85,
		Timestamp:    now.Add(-3 * time.Hour),
	}
	rec := buildReviewActionRecord(entry, "accept", true, now)
	if rec.EntryFile != "x.md" {
		t.Errorf("EntryFile = %q", rec.EntryFile)
	}
	if rec.QueueType != "critical" {
		t.Errorf("QueueType = %q, want critical", rec.QueueType)
	}
	if rec.Action != "accept" || !rec.Success {
		t.Errorf("action/success = %s/%v", rec.Action, rec.Success)
	}
	if rec.AgeHours < 2.99 || rec.AgeHours > 3.01 {
		t.Errorf("AgeHours = %v, want ~3.0", rec.AgeHours)
	}
	if rec.Surprise != 0.85 {
		t.Errorf("Surprise = %v, want 0.85", rec.Surprise)
	}
}

func TestBuildReviewActionRecord_NilEntry(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	rec := buildReviewActionRecord(nil, "skip", true, now)
	if rec.Action != "skip" {
		t.Errorf("Action = %q", rec.Action)
	}
	if rec.EntryFile != "" {
		t.Errorf("EntryFile should be empty for nil entry; got %q", rec.EntryFile)
	}
}

func TestAppendReviewActionLog_AppendsJSONL(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	rec := ReviewActionRecord{
		Timestamp:    now,
		EntryFile:    "x.md",
		DaydreamKind: "exploration",
		Action:       "accept",
		Success:      true,
	}
	if err := AppendReviewActionLog(vault, rec); err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(vault, "Metrics", "daydream_review_log.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Scan()
	var got ReviewActionRecord
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.EntryFile != "x.md" || got.Action != "accept" {
		t.Errorf("got %+v", got)
	}
}

func TestRunReviewLoop_LogsEachAction(t *testing.T) {
	// Inject a recorder for the review log fn so we can assert without
	// touching the filesystem.
	var recorded []ReviewActionRecord
	original := reviewActionLogFn
	reviewActionLogFn = func(_ string, rec ReviewActionRecord) error {
		recorded = append(recorded, rec)
		return nil
	}
	defer func() { reviewActionLogFn = original }()

	vault := t.TempDir()
	// Create a real buffer file so accept/reject have something to operate on.
	bufDir := filepath.Join(vault, "Buffer", "Daydream")
	if err := os.MkdirAll(bufDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md", "c.md"} {
		path := filepath.Join(bufDir, name)
		content := "---\ntype: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\n---\nbody"
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	entries, err := loadDaydreamEntries(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d", len(entries))
	}

	// Drive: accept, skip, reject
	in := strings.NewReader("a\ns\nx\n")
	var out bytes.Buffer
	stats := runReviewLoop(vault, entries, in, &out)

	if stats.Accepted != 1 || stats.Skipped != 1 || stats.Rejected != 1 {
		t.Errorf("stats = %+v, want a=1 s=1 x=1", stats)
	}
	if len(recorded) != 3 {
		t.Fatalf("logged %d actions, want 3; out=%s", len(recorded), out.String())
	}
	wants := []string{"accept", "skip", "reject"}
	for i, want := range wants {
		if recorded[i].Action != want {
			t.Errorf("recorded[%d].Action = %q, want %q", i, recorded[i].Action, want)
		}
	}
	for _, r := range recorded {
		if r.QueueType != "exploration" {
			t.Errorf("QueueType = %q, want exploration; rec=%+v", r.QueueType, r)
		}
	}
}

func TestRunReviewLoop_LogsQuitAction(t *testing.T) {
	var recorded []ReviewActionRecord
	original := reviewActionLogFn
	reviewActionLogFn = func(_ string, rec ReviewActionRecord) error {
		recorded = append(recorded, rec)
		return nil
	}
	defer func() { reviewActionLogFn = original }()

	vault := t.TempDir()
	bufDir := filepath.Join(vault, "Buffer", "Daydream")
	if err := os.MkdirAll(bufDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a.md", "b.md"} {
		path := filepath.Join(bufDir, name)
		content := "---\ntype: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\n---\nbody"
		os.WriteFile(path, []byte(content), 0o644)
	}
	entries, _ := loadDaydreamEntries(vault)

	in := strings.NewReader("q\n")
	var out bytes.Buffer
	runReviewLoop(vault, entries, in, &out)

	if len(recorded) != 1 {
		t.Fatalf("logged %d actions, want 1 (quit on first)", len(recorded))
	}
	if recorded[0].Action != "quit" {
		t.Errorf("Action = %q, want quit", recorded[0].Action)
	}
}
