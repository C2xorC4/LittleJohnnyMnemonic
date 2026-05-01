package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func surfaceCfg(enabled bool) Config {
	cfg := DefaultConfig()
	cfg.AutoDaydreamSurfaceToSession = enabled
	cfg.AutoDaydreamSurfaceMaxAgeHours = 12
	cfg.AutoDaydreamSurfaceRelevanceThreshold = 0.4
	cfg.AutoDaydreamSurfaceMaxPerPrompt = 2
	return cfg
}

func TestSurfaceFreshDaydreams_DisabledReturnsNil(t *testing.T) {
	vault := t.TempDir()
	writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-04-30T12:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [auto, daydream]")

	cfg := surfaceCfg(false)
	got := SurfaceFreshDaydreams(vault, "auto daydream surfacing query", "", cfg, time.Now())
	if got != nil {
		t.Errorf("got %v, want nil (toggle disabled)", got)
	}
}

func TestSurfaceFreshDaydreams_ScoresAndCaps(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Three fresh entries: two strongly relevant (high tag overlap), one
	// weakly relevant. Cap=2 should drop the weak one.
	writeDaydreamBuf(t, vault, "strong1.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream, jitter, scheduling]")
	writeDaydreamBuf(t, vault, "strong2.md",
		"type: buffer\ntimestamp: 2026-05-01T11:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream, scheduling]")
	writeDaydreamBuf(t, vault, "weak.md",
		"type: buffer\ntimestamp: 2026-05-01T11:30:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [unrelated]")

	cfg := surfaceCfg(true)
	got := SurfaceFreshDaydreams(vault, "autodream jitter scheduling work", "", cfg, now)
	if len(got) > 2 {
		t.Errorf("got %d, want <= cap=2", len(got))
	}
	for _, s := range got {
		if !strings.Contains(s.Entry.FileName, "strong") {
			t.Errorf("expected strong matches; got %s", s.Entry.FileName)
		}
	}
}

func TestSurfaceFreshDaydreams_ExcludesSurfacedInCurrentSession(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// "old.md" was already surfaced in session-1; "fresh.md" wasn't.
	writeDaydreamBuf(t, vault, "old.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream, scheduling]\nsurfaced_in_sessions: [\"session-1\"]")
	writeDaydreamBuf(t, vault, "fresh.md",
		"type: buffer\ntimestamp: 2026-05-01T11:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream, scheduling]")

	cfg := surfaceCfg(true)
	got := SurfaceFreshDaydreams(vault, "autodream scheduling", "session-1", cfg, now)
	if len(got) != 1 {
		t.Fatalf("got %d, want 1", len(got))
	}
	if got[0].Entry.FileName != "fresh.md" {
		t.Errorf("got %s, want fresh.md (already-surfaced-in-session should be excluded)", got[0].Entry.FileName)
	}
}

func TestSurfaceFreshDaydreams_ReSurfacesInDifferentSession(t *testing.T) {
	// An entry surfaced in session-1 should re-surface in session-2 — the
	// dedup is per-session, not permanent.
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream, scheduling]\nsurfaced_in_sessions: [\"session-1\"]")

	cfg := surfaceCfg(true)
	got := SurfaceFreshDaydreams(vault, "autodream scheduling", "session-2", cfg, now)
	if len(got) != 1 {
		t.Errorf("got %d, want 1 (different session should re-surface)", len(got))
	}
}

func TestSurfaceFreshDaydreams_ExcludesStaleByAge(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	writeDaydreamBuf(t, vault, "ancient.md",
		"type: buffer\ntimestamp: 2026-04-25T00:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream]")
	writeDaydreamBuf(t, vault, "recent.md",
		"type: buffer\ntimestamp: 2026-05-01T08:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream]")

	cfg := surfaceCfg(true)
	cfg.AutoDaydreamSurfaceMaxAgeHours = 6 // recent entry is ~4h old, ancient is 6 days
	got := SurfaceFreshDaydreams(vault, "autodream", "", cfg, now)
	for _, s := range got {
		if s.Entry.FileName == "ancient.md" {
			t.Errorf("ancient entry should have been filtered by age")
		}
	}
}

func TestSurfaceFreshDaydreams_BelowThresholdSkipped(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	writeDaydreamBuf(t, vault, "weak.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [unrelated-tag]")

	cfg := surfaceCfg(true)
	cfg.AutoDaydreamSurfaceRelevanceThreshold = 0.5
	got := SurfaceFreshDaydreams(vault, "very different topic about something else", "", cfg, now)
	if len(got) != 0 {
		t.Errorf("got %d, want 0 (below threshold)", len(got))
	}
}

func TestScoreDaydreamRelevance_TagWeightHigherThanBody(t *testing.T) {
	// Tag hit (60% weight) should outscore body hit (40% weight) for the
	// same number of matches.
	tagOnly := &BufferEntry{
		FileName: "x.md",
		Tags:     []string{"foo"},
		Body:     "no relevant words here",
	}
	bodyOnly := &BufferEntry{
		FileName: "y.md",
		Tags:     []string{"unrelated"},
		Body:     "this body contains foo prominently",
	}
	keywords := []string{"foo"}
	tagScore := scoreDaydreamRelevance(tagOnly, keywords)
	bodyScore := scoreDaydreamRelevance(bodyOnly, keywords)
	if tagScore <= bodyScore {
		t.Errorf("tag score %v should exceed body score %v", tagScore, bodyScore)
	}
}

func TestScoreDaydreamRelevance_NilOrEmptyReturnsZero(t *testing.T) {
	if got := scoreDaydreamRelevance(nil, []string{"x"}); got != 0 {
		t.Errorf("nil entry: got %v, want 0", got)
	}
	if got := scoreDaydreamRelevance(&BufferEntry{Tags: []string{"x"}}, nil); got != 0 {
		t.Errorf("nil keywords: got %v, want 0", got)
	}
}

func TestMarkDaydreamSurfaced_PersistsSessionID(t *testing.T) {
	vault := t.TempDir()
	path := writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	entry, err := ParseBufferEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(entry.SurfacedInSessions) != 0 {
		t.Fatal("SurfacedInSessions should default empty")
	}
	if err := MarkDaydreamSurfaced(entry, "session-abc"); err != nil {
		t.Fatal(err)
	}
	updated, err := ParseBufferEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if !containsString(updated.SurfacedInSessions, "session-abc") {
		t.Errorf("SurfacedInSessions = %v, want session-abc", updated.SurfacedInSessions)
	}
	if updated.DaydreamKind != "exploration" {
		t.Errorf("DaydreamKind not preserved: got %q", updated.DaydreamKind)
	}
}

func TestMarkDaydreamSurfaced_DoesNotDuplicate(t *testing.T) {
	vault := t.TempDir()
	path := writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	entry, _ := ParseBufferEntry(path)

	// Mark twice with the same session ID
	if err := MarkDaydreamSurfaced(entry, "session-abc"); err != nil {
		t.Fatal(err)
	}
	if err := MarkDaydreamSurfaced(entry, "session-abc"); err != nil {
		t.Fatal(err)
	}
	updated, _ := ParseBufferEntry(path)
	count := 0
	for _, s := range updated.SurfacedInSessions {
		if s == "session-abc" {
			count++
		}
	}
	if count != 1 {
		t.Errorf("session-abc appears %d times, want 1 (no duplication)", count)
	}
}

func TestMarkDaydreamSurfaced_EmptySessionIsNoOp(t *testing.T) {
	vault := t.TempDir()
	path := writeDaydreamBuf(t, vault, "x.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration")
	entry, _ := ParseBufferEntry(path)

	if err := MarkDaydreamSurfaced(entry, ""); err != nil {
		t.Fatal(err)
	}
	updated, _ := ParseBufferEntry(path)
	if len(updated.SurfacedInSessions) != 0 {
		t.Errorf("empty session should not append; got %v", updated.SurfacedInSessions)
	}
}

func TestWriteFreshDaydreamFindings_FormatsBlock(t *testing.T) {
	var buf bytes.Buffer
	surfaced := []SurfacedDaydream{
		{
			Entry:   &BufferEntry{FileName: "test1.md", DaydreamKind: "exploration"},
			Score:   0.7,
			Excerpt: "A finding about X.",
		},
		{
			Entry:   &BufferEntry{FileName: "test2.md", DaydreamKind: "replay-refine"},
			Score:   0.5,
			Excerpt: "A refinement of Y.",
		},
	}
	writeFreshDaydreamFindings(&buf, surfaced)
	out := buf.String()
	if !strings.Contains(out, "<fresh-daydream-findings>") {
		t.Error("missing opening tag")
	}
	if !strings.Contains(out, "</fresh-daydream-findings>") {
		t.Error("missing closing tag")
	}
	if !strings.Contains(out, "test1") || !strings.Contains(out, "test2") {
		t.Error("missing entry titles")
	}
	if !strings.Contains(out, "[exploration]") || !strings.Contains(out, "[replay-refine]") {
		t.Error("missing kind annotations")
	}
}

func TestWriteFreshDaydreamFindings_EmptyIsNoOp(t *testing.T) {
	var buf bytes.Buffer
	writeFreshDaydreamFindings(&buf, nil)
	if buf.Len() != 0 {
		t.Errorf("empty surfaced list should produce no output, got %q", buf.String())
	}
}

func TestSurfaceFreshDaydreams_MarksAfterSurfacingViaHookHelper(t *testing.T) {
	// End-to-end within one session: SurfaceFreshDaydreams returns matches,
	// MarkDaydreamSurfaced records the session, next call same-session returns
	// empty, but a different session re-surfaces.
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	const sess = "session-1"

	writeDaydreamBuf(t, vault, "fresh.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [autodream, scheduling]")

	cfg := surfaceCfg(true)
	first := SurfaceFreshDaydreams(vault, "autodream scheduling", sess, cfg, now)
	if len(first) != 1 {
		t.Fatalf("first call: got %d, want 1", len(first))
	}
	if err := MarkDaydreamSurfaced(first[0].Entry, sess); err != nil {
		t.Fatal(err)
	}

	second := SurfaceFreshDaydreams(vault, "autodream scheduling", sess, cfg, now)
	if len(second) != 0 {
		t.Errorf("second call same session: got %d, want 0 (already surfaced)", len(second))
	}

	updated, _ := ParseBufferEntry(filepath.Join(vault, "Buffer", "Daydream", "fresh.md"))
	if !containsString(updated.SurfacedInSessions, sess) {
		t.Errorf("file should record %q in SurfacedInSessions, got %v", sess, updated.SurfacedInSessions)
	}
}

func TestSurfaceFreshDaydreams_RespectsMaxPerPrompt(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 5; i++ {
		writeDaydreamBuf(t, vault, "match"+string(rune('a'+i))+".md",
			"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: daydream\nsurprise: 0.5\ndaydream_kind: exploration\ntags: [matchtag]")
	}

	cfg := surfaceCfg(true)
	cfg.AutoDaydreamSurfaceMaxPerPrompt = 3
	got := SurfaceFreshDaydreams(vault, "matchtag query", "", cfg, now)
	if len(got) > 3 {
		t.Errorf("got %d, want <= cap=3", len(got))
	}
}
