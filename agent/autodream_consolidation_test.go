package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// withFakeValueJudge swaps the package-level daydreamValueJudgeFn for the
// duration of a test, restoring the real function on cleanup. Lets tests
// drive the value-judge routing without hitting the API.
func withFakeValueJudge(t *testing.T, fn func(*BufferEntry) (ValueVerdict, string, error)) {
	t.Helper()
	saved := daydreamValueJudgeFn
	daydreamValueJudgeFn = fn
	t.Cleanup(func() { daydreamValueJudgeFn = saved })
}

func TestAssessBufferEntry_ReplayContradictAlwaysPromotes(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.1, // would normally score out
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "replay-contradict",
		Priority:         "critical",
		Body:             "The recent observation conflicts with the stable trait in concrete ways.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if assessment.Action != ActionPromote {
		t.Errorf("Action = %s, want promote", assessment.Action)
	}
	if !strings.Contains(assessment.Reason, "replay-contradict") {
		t.Errorf("Reason = %q, want mention of replay-contradict", assessment.Reason)
	}
}

func TestAssessBufferEntry_PriorityCriticalAlsoPromotes(t *testing.T) {
	// Even without daydream_kind, priority=critical is enough to promote.
	cfg := DefaultConfig()
	now := time.Now()
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.1,
		ContextIntegrity: ContextFull,
		Priority:         "critical",
		Body:             "Important flag from somewhere — must surface.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if assessment.Action != ActionPromote {
		t.Errorf("Action = %s, want promote (priority=critical)", assessment.Action)
	}
}

func TestAssessBufferEntry_ReplayRefineGetsBonus(t *testing.T) {
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		return ValueMarginal, "test-marginal", nil
	})
	cfg := DefaultConfig()
	now := time.Now()
	// Surprise * recency * context * (1 - redundancy)
	// = 0.4 * (~1.0) * 1.0 * 1.0 = 0.4 (would HOLD without bonus)
	// + 0.3 bonus = 0.7 → PROMOTE
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.4,
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "replay-refine",
		Body:             "The recent observation refines the stable trait by adding nuance about X.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if assessment.Action != ActionPromote {
		t.Errorf("Action = %s (retention=%.3f), want promote (refine should get +0.3 bonus)",
			assessment.Action, assessment.RetentionScore)
	}
	if assessment.RetentionScore < 0.65 {
		t.Errorf("retention=%.3f, want ≥ 0.65 (0.4 base + 0.3 refine bonus)", assessment.RetentionScore)
	}
}

func TestAssessBufferEntry_ValueJudgeLowValueDiscards(t *testing.T) {
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		return ValueLowValue, "vague gesture, no claim", nil
	})
	cfg := DefaultConfig()
	now := time.Now()
	// Without judge: 0.9 * ~1.0 * 1.0 * 1.0 = 0.9 → would PROMOTE
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.9,
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "exploration",
		Body:             "There might be something interesting in this area.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if assessment.Action != ActionDiscard {
		t.Errorf("Action = %s, want discard (low-value verdict overrides high surprise)", assessment.Action)
	}
	if !strings.Contains(assessment.Reason, "low") {
		t.Errorf("Reason = %q, want mention of low value", assessment.Reason)
	}
	if assessment.DaydreamValueVerdict != ValueLowValue {
		t.Errorf("DaydreamValueVerdict = %s, want low-value", assessment.DaydreamValueVerdict)
	}
}

func TestAssessBufferEntry_ValueJudgeValuableFloors(t *testing.T) {
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		return ValueValuable, "concrete connection identified", nil
	})
	cfg := DefaultConfig()
	now := time.Now()
	// Without judge: 0.2 * ~1.0 * 1.0 * 1.0 = 0.2 → would HOLD
	// With valuable floor: max(0.2, 0.6) = 0.6 → PROMOTE
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.2,
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "exploration",
		Body:             "Recent X exhibits the same pattern as Y, suggesting a shared mechanism.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if assessment.Action != ActionPromote {
		t.Errorf("Action = %s (retention=%.3f), want promote (valuable should floor at 0.6)",
			assessment.Action, assessment.RetentionScore)
	}
	if assessment.RetentionScore < 0.6 {
		t.Errorf("retention=%.3f, want ≥ 0.6 (valuable floor)", assessment.RetentionScore)
	}
}

func TestAssessBufferEntry_ValueJudgeMarginalUnchanged(t *testing.T) {
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		return ValueMarginal, "real concept but vague", nil
	})
	cfg := DefaultConfig()
	now := time.Now()
	// 0.4 surprise → ~0.4 retention → HOLD band (between 0.2 and 0.5)
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.4,
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "exploration",
		Body:             "There seems to be a connection between A and B but I can't pin it down.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if assessment.Action == ActionPromote {
		t.Errorf("Action = %s; marginal verdict should not promote (no floor applied)", assessment.Action)
	}
	if assessment.DaydreamValueVerdict != ValueMarginal {
		t.Errorf("DaydreamValueVerdict = %s, want marginal", assessment.DaydreamValueVerdict)
	}
}

func TestAssessBufferEntry_ValueJudgeSkippedForNonDaydream(t *testing.T) {
	called := false
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		called = true
		return ValueLowValue, "should not be called", nil
	})
	cfg := DefaultConfig()
	now := time.Now()
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.9,
		ContextIntegrity: ContextFull,
		Source:           "conversation", // user-stated, NOT daydream
		Body:             "User said the API endpoint moved to /v2.",
	}
	_ = assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if called {
		t.Error("value judge should NOT fire on conversation-sourced entries")
	}
}

func TestAssessBufferEntry_ValueJudgeDisabledByConfig(t *testing.T) {
	called := false
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		called = true
		return ValueLowValue, "should not be called", nil
	})
	cfg := DefaultConfig()
	cfg.AutoDaydreamValueJudgeEnabled = false
	now := time.Now()
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.9,
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "exploration",
		Body:             "Connection between X and Y.",
	}
	_ = assessBufferEntry(entry, nil, cfg, now, time.Time{})
	if called {
		t.Error("value judge should not fire when AutoDaydreamValueJudgeEnabled=false")
	}
}

func TestAssessBufferEntry_ValueJudgeErrorFallsThrough(t *testing.T) {
	withFakeValueJudge(t, func(*BufferEntry) (ValueVerdict, string, error) {
		return "", "", os.ErrPermission // simulated judge failure
	})
	cfg := DefaultConfig()
	now := time.Now()
	entry := &BufferEntry{
		Timestamp:        now,
		Surprise:         0.9,
		ContextIntegrity: ContextFull,
		Source:           "daydream",
		DaydreamKind:     "exploration",
		Body:             "Concrete connection between things.",
	}
	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})
	// Judge unavailable → fall through to standard scoring. 0.9 surprise
	// should still PROMOTE.
	if assessment.Action != ActionPromote {
		t.Errorf("Action = %s, want promote (judge error should fall through, not block)", assessment.Action)
	}
	if assessment.DaydreamValueVerdict != "" {
		t.Errorf("DaydreamValueVerdict = %s, want empty (judge errored)", assessment.DaydreamValueVerdict)
	}
}

func TestIsExplorationOrRefine(t *testing.T) {
	cases := map[string]bool{
		"exploration":       true,
		"replay-refine":     true,
		"":                  true, // empty defaults to exploration
		"replay-contradict": false,
		"replay-reinforce":  false,
		"unknown":           false,
	}
	for k, want := range cases {
		got := isExplorationOrRefine(&BufferEntry{DaydreamKind: k})
		if got != want {
			t.Errorf("daydream_kind=%q: got %v, want %v", k, got, want)
		}
	}
}

// ─── Reinforcements processing ───

func writeMemoryFileForReinforcement(t *testing.T, vault, name string, confidence float64) string {
	t.Helper()
	frontmatter := "type: semantic\ntitle: Stable Trait\nlast_accessed: 2026-04-30T12:00:00Z\naccess_count: 30\nconfidence: " +
		formatFloat(confidence)
	return writeMemFile(t, vault, "Semantic", name, frontmatter, "stable trait body")
}

func formatFloat(f float64) string {
	// avoid pulling in fmt for a 1-line helper
	switch f {
	case 0.5:
		return "0.5"
	case 0.6:
		return "0.6"
	case 0.7:
		return "0.7"
	case 0.9:
		return "0.9"
	case 0.95:
		return "0.95"
	case 1.0:
		return "1.0"
	}
	return "0.5"
}

func TestProcessReplayReinforcements_AppliesDelta(t *testing.T) {
	vault := t.TempDir()
	memPath := writeMemoryFileForReinforcement(t, vault, "stable.md", 0.7)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Queue a reinforcement
	if err := appendReplayReinforcement(vault, ReplayReinforcementEntry{
		Timestamp:        now.Add(-1 * time.Hour),
		StableMemoryPath: memPath,
		RecentSeedPath:   "/x/Buffer/recent.md",
		ConfidenceDelta:  0.1,
		Applied:          false,
	}); err != nil {
		t.Fatal(err)
	}

	applied, skipped, err := ProcessReplayReinforcements(vault, now)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 || skipped != 0 {
		t.Errorf("applied=%d skipped=%d, want applied=1 skipped=0", applied, skipped)
	}

	// Verify confidence was bumped
	updated, err := ParseMemoryEntry(memPath)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Confidence < 0.79 || updated.Confidence > 0.81 {
		t.Errorf("Confidence = %v, want ≈0.8 (0.7 + 0.1)", updated.Confidence)
	}

	// Verify file was archived
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl")); !os.IsNotExist(err) {
		t.Errorf("source file should have been archived")
	}
	archives, _ := filepath.Glob(filepath.Join(vault, "Metrics", "Archive", "replay_reinforcements.*.jsonl"))
	if len(archives) != 1 {
		t.Errorf("expected 1 archive file, got %d", len(archives))
	}
}

func TestProcessReplayReinforcements_CapsAtOne(t *testing.T) {
	vault := t.TempDir()
	memPath := writeMemoryFileForReinforcement(t, vault, "stable.md", 0.95)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	if err := appendReplayReinforcement(vault, ReplayReinforcementEntry{
		Timestamp:        now,
		StableMemoryPath: memPath,
		ConfidenceDelta:  0.5, // would push to 1.45 uncapped
	}); err != nil {
		t.Fatal(err)
	}

	if _, _, err := ProcessReplayReinforcements(vault, now); err != nil {
		t.Fatal(err)
	}
	updated, _ := ParseMemoryEntry(memPath)
	if updated.Confidence != 1.0 {
		t.Errorf("Confidence = %v, want 1.0 (capped)", updated.Confidence)
	}
}

func TestProcessReplayReinforcements_SkipsMissingTarget(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	if err := appendReplayReinforcement(vault, ReplayReinforcementEntry{
		Timestamp:        now,
		StableMemoryPath: filepath.Join(vault, "Memory", "Semantic", "does-not-exist.md"),
		ConfidenceDelta:  0.1,
	}); err != nil {
		t.Fatal(err)
	}

	applied, skipped, err := ProcessReplayReinforcements(vault, now)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 0 || skipped != 1 {
		t.Errorf("applied=%d skipped=%d, want applied=0 skipped=1", applied, skipped)
	}
	// File should still archive — don't let a stale entry block the queue
	archives, _ := filepath.Glob(filepath.Join(vault, "Metrics", "Archive", "replay_reinforcements.*.jsonl"))
	if len(archives) != 1 {
		t.Errorf("expected 1 archive even when entry skipped, got %d", len(archives))
	}
}

func TestProcessReplayReinforcements_NoFileNoOps(t *testing.T) {
	applied, skipped, err := ProcessReplayReinforcements(t.TempDir(), time.Now())
	if err != nil {
		t.Errorf("err = %v, want nil", err)
	}
	if applied != 0 || skipped != 0 {
		t.Errorf("applied=%d skipped=%d, want 0 0 (no file)", applied, skipped)
	}
}

func TestProcessReplayReinforcements_AlreadyAppliedSkipped(t *testing.T) {
	vault := t.TempDir()
	memPath := writeMemoryFileForReinforcement(t, vault, "stable.md", 0.7)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	if err := appendReplayReinforcement(vault, ReplayReinforcementEntry{
		Timestamp:        now,
		StableMemoryPath: memPath,
		ConfidenceDelta:  0.1,
		Applied:          true, // already applied
	}); err != nil {
		t.Fatal(err)
	}

	applied, _, err := ProcessReplayReinforcements(vault, now)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 0 {
		t.Errorf("applied=%d, want 0 (entry was already marked Applied)", applied)
	}
	updated, _ := ParseMemoryEntry(memPath)
	if updated.Confidence != 0.7 {
		t.Errorf("Confidence = %v, want 0.7 unchanged", updated.Confidence)
	}
}

func TestParseBufferEntry_DaydreamFields(t *testing.T) {
	tmp := t.TempDir()
	frontmatter := `type: buffer
timestamp: 2026-04-30T12:00:00Z
source: daydream
surprise: 0.6
daydream_kind: replay-refine
daydream_mode: quiet
priority: high
relationship: refine`
	path := filepath.Join(tmp, "test.md")
	content := "---\n" + frontmatter + "\n---\nbody"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	entry, err := ParseBufferEntry(path)
	if err != nil {
		t.Fatal(err)
	}
	if entry.DaydreamKind != "replay-refine" {
		t.Errorf("DaydreamKind = %q", entry.DaydreamKind)
	}
	if entry.DaydreamMode != "quiet" {
		t.Errorf("DaydreamMode = %q", entry.DaydreamMode)
	}
	if entry.Priority != "high" {
		t.Errorf("Priority = %q", entry.Priority)
	}
	if entry.Relationship != "refine" {
		t.Errorf("Relationship = %q", entry.Relationship)
	}
}

// integration-style test: full pipeline from a queued reinforcement through
// JSONL parse and archive
func TestReinforcementJSONLRoundTrip(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	want := ReplayReinforcementEntry{
		Timestamp:        now,
		StableMemoryPath: "/v/Memory/Semantic/x.md",
		RecentSeedPath:   "/v/Buffer/y.md",
		ConfidenceDelta:  0.07,
		Reasoning:        "reinforces existing pattern",
		Applied:          false,
	}
	if err := appendReplayReinforcement(vault, want); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var got ReplayReinforcementEntry
	line := strings.TrimSpace(strings.Split(string(data), "\n")[0])
	if err := json.Unmarshal([]byte(line), &got); err != nil {
		t.Fatal(err)
	}
	if got.StableMemoryPath != want.StableMemoryPath {
		t.Errorf("StableMemoryPath = %q, want %q", got.StableMemoryPath, want.StableMemoryPath)
	}
	if got.ConfidenceDelta != want.ConfidenceDelta {
		t.Errorf("ConfidenceDelta = %v, want %v", got.ConfidenceDelta, want.ConfidenceDelta)
	}
	if got.Applied != false {
		t.Errorf("Applied = %v, want false (default)", got.Applied)
	}
}
