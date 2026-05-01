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

func writeDaydreamBuf(t *testing.T, vault, name, frontmatter string) string {
	return writeBufFile(t, vault, "Daydream", name, frontmatter, "daydream body")
}

func TestLoadDaydreamEntries_OnlyDaydreamSubdir(t *testing.T) {
	vault := t.TempDir()
	writeBufFile(t, vault, "", "regular.md",
		"type: buffer\ntimestamp: 2026-04-30T10:00:00Z\nsource: conversation\nsurprise: 0.5", "x")
	writeDaydreamBuf(t, vault, "dd1.md",
		"type: buffer\ntimestamp: 2026-04-30T11:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	writeDaydreamBuf(t, vault, "dd2.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: replay-refine")

	got, err := loadDaydreamEntries(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Errorf("got %d entries, want 2 (Daydream/ only)", len(got))
	}
	for _, e := range got {
		if !strings.Contains(filepath.ToSlash(e.FilePath), "/Daydream/") {
			t.Errorf("non-Daydream entry leaked: %s", e.FilePath)
		}
	}
}

func TestLoadDaydreamEntries_StableTimestampOrder(t *testing.T) {
	vault := t.TempDir()
	writeDaydreamBuf(t, vault, "newer.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	writeDaydreamBuf(t, vault, "older.md",
		"type: buffer\ntimestamp: 2026-04-29T12:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")

	got, err := loadDaydreamEntries(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d entries, want 2", len(got))
	}
	if got[0].FileName != "older.md" {
		t.Errorf("want older.md first (ascending timestamp), got %s", got[0].FileName)
	}
}

func TestFilterDaydreamEntries_ByKind(t *testing.T) {
	now := time.Now()
	entries := []*BufferEntry{
		{FileName: "a.md", DaydreamKind: "exploration", Timestamp: now},
		{FileName: "b.md", DaydreamKind: "replay-refine", Timestamp: now},
		{FileName: "c.md", DaydreamKind: "replay-contradict", Timestamp: now},
	}
	got := filterDaydreamEntries(entries, DaydreamFilter{Kind: "replay-refine"}, now)
	if len(got) != 1 || got[0].FileName != "b.md" {
		t.Errorf("got %v, want [b.md]", got)
	}
}

func TestFilterDaydreamEntries_ByPriority(t *testing.T) {
	now := time.Now()
	entries := []*BufferEntry{
		{FileName: "a.md", Priority: "", Timestamp: now},
		{FileName: "b.md", Priority: "high", Timestamp: now},
		{FileName: "c.md", Priority: "critical", Timestamp: now},
	}
	got := filterDaydreamEntries(entries, DaydreamFilter{Priority: "critical"}, now)
	if len(got) != 1 || got[0].FileName != "c.md" {
		t.Errorf("got %v, want [c.md]", got)
	}
}

func TestFilterDaydreamEntries_ByAge(t *testing.T) {
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	entries := []*BufferEntry{
		{FileName: "fresh.md", Timestamp: now.Add(-2 * 24 * time.Hour)},
		{FileName: "stale.md", Timestamp: now.Add(-30 * 24 * time.Hour)},
	}
	got := filterDaydreamEntries(entries, DaydreamFilter{MaxAgeDays: 7}, now)
	if len(got) != 1 || got[0].FileName != "fresh.md" {
		t.Errorf("got %v, want [fresh.md]", got)
	}
}

func TestFilterDaydreamEntries_EmptyFilterReturnsAll(t *testing.T) {
	now := time.Now()
	entries := []*BufferEntry{
		{FileName: "a.md", DaydreamKind: "exploration", Timestamp: now},
		{FileName: "b.md", DaydreamKind: "replay-refine", Timestamp: now},
	}
	got := filterDaydreamEntries(entries, DaydreamFilter{}, now)
	if len(got) != 2 {
		t.Errorf("got %d, want 2 (empty filter matches everything)", len(got))
	}
}

func TestAcceptEntry_PinsAndWrites(t *testing.T) {
	vault := t.TempDir()
	path := writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	entry, err := ParseBufferEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := acceptEntry(entry); err != nil {
		t.Fatal(err)
	}
	// Re-parse and verify pinned
	updated, err := ParseBufferEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !updated.Pinned {
		t.Error("Pinned should be true after accept")
	}
	if updated.DaydreamKind != "exploration" {
		t.Errorf("DaydreamKind not preserved: got %q", updated.DaydreamKind)
	}
}

func TestRejectEntry_DeletesFile(t *testing.T) {
	vault := t.TempDir()
	path := writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.5")
	entry, _ := ParseBufferEntry(path)
	if err := rejectEntry(entry); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("file should have been deleted")
	}
}

func TestPromoteEntry_WritesMemoryAndRemovesBuffer(t *testing.T) {
	vault := t.TempDir()
	path := writeDaydreamBuf(t, vault, "daydream-some-finding.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.6\ntags: [daydream, x]")
	entry, _ := ParseBufferEntry(path)

	var w bytes.Buffer
	if err := promoteEntry(vault, entry, &w); err != nil {
		t.Fatal(err)
	}

	// Buffer file removed
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("buffer file should have been removed")
	}
	// Memory/Semantic/ has the new entry
	memPath := filepath.Join(vault, "Memory", "Semantic", "daydream-some-finding.md")
	mem, err := ParseMemoryEntry(memPath)
	if err != nil {
		t.Fatalf("memory entry not written: %v", err)
	}
	if mem.Type != TypeSemantic {
		t.Errorf("Type = %s, want semantic", mem.Type)
	}
	if mem.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7", mem.Confidence)
	}
	// Title should strip "daydream-" prefix and replace underscores
	if !strings.Contains(mem.Title, "some-finding") && !strings.Contains(mem.Title, "some finding") {
		t.Errorf("Title %q should derive from filename", mem.Title)
	}
}

func TestRunReviewLoop_ProcessesActionsInOrder(t *testing.T) {
	vault := t.TempDir()
	// Three entries
	pa := writeDaydreamBuf(t, vault, "a.md",
		"type: buffer\ntimestamp: 2026-04-30T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	pb := writeDaydreamBuf(t, vault, "b.md",
		"type: buffer\ntimestamp: 2026-04-30T11:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	pc := writeDaydreamBuf(t, vault, "c.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")

	entries, _ := loadDaydreamEntries(vault)
	if len(entries) != 3 {
		t.Fatalf("got %d entries, want 3", len(entries))
	}

	// Drive: a → accept, b → skip, c → reject
	in := strings.NewReader("a\ns\nx\n")
	var out bytes.Buffer

	stats := runReviewLoop(vault, entries, in, &out)
	if stats.Accepted != 1 || stats.Skipped != 1 || stats.Rejected != 1 {
		t.Errorf("stats = %+v, want 1 accepted / 1 skipped / 1 rejected", stats)
	}
	// Verify a is pinned
	updatedA, _ := ParseBufferEntry(pa)
	if !updatedA.Pinned {
		t.Error("a.md should be pinned")
	}
	// b unchanged (not pinned)
	updatedB, _ := ParseBufferEntry(pb)
	if updatedB.Pinned {
		t.Error("b.md should not be pinned (skipped)")
	}
	// c deleted
	if _, err := os.Stat(pc); !os.IsNotExist(err) {
		t.Error("c.md should have been deleted")
	}
}

func TestRunReviewLoop_QuitStopsProcessing(t *testing.T) {
	vault := t.TempDir()
	writeDaydreamBuf(t, vault, "a.md",
		"type: buffer\ntimestamp: 2026-04-30T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	writeDaydreamBuf(t, vault, "b.md",
		"type: buffer\ntimestamp: 2026-04-30T11:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")

	entries, _ := loadDaydreamEntries(vault)

	// Drive: a → quit (b should not be processed)
	in := strings.NewReader("q\n")
	var out bytes.Buffer
	stats := runReviewLoop(vault, entries, in, &out)

	if stats.Accepted+stats.Refined+stats.Rejected+stats.Promoted+stats.Skipped != 0 {
		t.Errorf("stats = %+v, want all zero (quit before any action)", stats)
	}
}

func TestRunReviewLoop_HelpDoesNotConsume(t *testing.T) {
	vault := t.TempDir()
	writeDaydreamBuf(t, vault, "a.md",
		"type: buffer\ntimestamp: 2026-04-30T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	entries, _ := loadDaydreamEntries(vault)

	// Drive: ? then s — help should print and continue waiting for action.
	in := strings.NewReader("?\ns\n")
	var out bytes.Buffer
	stats := runReviewLoop(vault, entries, in, &out)
	if stats.Skipped != 1 {
		t.Errorf("stats = %+v, want 1 skipped after ? + s", stats)
	}
	if !strings.Contains(out.String(), "a=accept") {
		t.Error("help line should be printed for ?")
	}
}

func TestMarkContradictionReviewed_FlipsMatchingEntry(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Pre-populate replay_contradictions.jsonl with two entries
	if err := appendReplayContradiction(vault, ReplayContradictionEntry{
		Timestamp:        now,
		StableMemoryPath: "/v/Memory/Semantic/stable.md",
		RecentSeedPath:   "/v/Buffer/recent.md",
		Reviewed:         false,
	}); err != nil {
		t.Fatal(err)
	}
	if err := appendReplayContradiction(vault, ReplayContradictionEntry{
		Timestamp:        now.Add(-30 * 24 * time.Hour), // far older — should NOT match
		StableMemoryPath: "/v/Memory/Semantic/other.md",
		RecentSeedPath:   "/v/Buffer/other.md",
		Reviewed:         false,
	}); err != nil {
		t.Fatal(err)
	}

	// Buffer entry referencing the recent path via wikilink
	entry := &BufferEntry{
		FileName:     "x.md",
		DaydreamKind: "replay-contradict",
		Timestamp:    now,
		Related:      []string{"[[Buffer/recent]]"},
	}

	flipped, err := markContradictionReviewed(vault, entry)
	if err != nil {
		t.Fatal(err)
	}
	if flipped < 1 {
		t.Errorf("flipped = %d, want at least 1", flipped)
	}

	// Re-read file and verify Reviewed=true on matching entry
	data, _ := os.ReadFile(filepath.Join(vault, "Metrics", "replay_contradictions.jsonl"))
	var entries []ReplayContradictionEntry
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var rec ReplayContradictionEntry
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		entries = append(entries, rec)
	}
	if len(entries) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(entries))
	}

	// The first entry (recent path matches) should be reviewed
	matchingReviewed := false
	for _, e := range entries {
		if strings.Contains(e.RecentSeedPath, "recent.md") && e.Reviewed {
			matchingReviewed = true
		}
	}
	if !matchingReviewed {
		t.Error("matching contradiction should have Reviewed=true")
	}
}

func TestMarkContradictionReviewed_NoFileNoOp(t *testing.T) {
	flipped, err := markContradictionReviewed(t.TempDir(), &BufferEntry{})
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if flipped != 0 {
		t.Errorf("flipped = %d, want 0", flipped)
	}
}

func TestDefaultEditor_RespectsEnvVar(t *testing.T) {
	t.Setenv("EDITOR", "my-favorite-editor")
	if got := defaultEditor(); got != "my-favorite-editor" {
		t.Errorf("got %q, want my-favorite-editor", got)
	}
}

func TestFormatAge(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{15 * time.Minute, "15m"},
		{59 * time.Minute, "59m"},
		{3 * time.Hour, "3h"},
		{23 * time.Hour, "23h"},
		{2 * 24 * time.Hour, "2d"},
		{30 * 24 * time.Hour, "30d"},
	}
	for _, c := range cases {
		if got := formatAge(c.d); got != c.want {
			t.Errorf("formatAge(%v) = %q, want %q", c.d, got, c.want)
		}
	}
}

func TestNormalizeRelatedRef(t *testing.T) {
	cases := map[string]string{
		"[[Memory/Semantic/x]]": "Memory/Semantic/x",
		"[[Buffer/y]]":          "Buffer/y",
		"plain":                 "plain",
		"file.md":               "file",
		"":                      "",
	}
	for in, want := range cases {
		if got := normalizeRelatedRef(in); got != want {
			t.Errorf("normalizeRelatedRef(%q) = %q, want %q", in, got, want)
		}
	}
}
