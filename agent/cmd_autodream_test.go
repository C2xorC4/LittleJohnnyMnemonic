package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func runVault(t *testing.T) string {
	t.Helper()
	vault := t.TempDir()
	// One non-daydream buffer entry, one knowledge memory, one crystallized semantic.
	bufPath := writeBufFile(t, vault, "", "obs.md",
		"type: buffer\ntimestamp: 2026-04-30T10:00:00Z\nsource: conversation\nsurprise: 0.5", "x")
	// Backdate the buffer mtime so quiet-mode activity-skip doesn't fire by
	// default in tests. Tests that want fresh activity overwrite this.
	old := time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	if err := os.Chtimes(bufPath, old, old); err != nil {
		t.Fatal(err)
	}
	writeMemFile(t, vault, "Knowledge", "k.md",
		"type: knowledge\ntitle: K\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 5", "x")
	writeMemFile(t, vault, "Semantic", "s.md",
		"type: semantic\ntitle: S\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 30", "x")
	return vault
}

func enabledCfg() Config {
	cfg := DefaultConfig()
	cfg.AutoDaydreamEnabled = true
	return cfg
}

func fakeInvoker(response string) AutodreamInvoker {
	return func(_ string) (string, string, error) {
		return response, "test-fake", nil
	}
}

func errorInvoker() AutodreamInvoker {
	return func(_ string) (string, string, error) {
		return "", "test-fake", errors.New("simulated invoker error")
	}
}

func appendLogEntry(t *testing.T, vault string, entry AutodreamLogEntry) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(vault, "Metrics"), 0o755); err != nil {
		t.Fatal(err)
	}
	line, _ := json.Marshal(entry)
	path := filepath.Join(vault, "Metrics", "autodream_log.jsonl")
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		t.Fatal(err)
	}
}

func TestRunAutodream_SkipCategoryMasterDisabled(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       DefaultConfig(),
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.SkipCategory != SkipMasterDisabled {
		t.Errorf("SkipCategory = %q, want %q", res.SkipCategory, SkipMasterDisabled)
	}
}

func TestRunAutodream_SkipCategoryActivityRecent(t *testing.T) {
	vault := runVault(t)
	// Force a fresh buffer mtime so activity-skip fires (quiet mode default
	// activity window is 60min in the test config).
	bufPath := filepath.Join(vault, "Buffer", "obs.md")
	now := time.Now()
	if err := os.Chtimes(bufPath, now, now); err != nil {
		t.Fatal(err)
	}
	in := AutodreamRunInputs{
		VaultRoot:    vault,
		Cfg:          enabledCfg(),
		Now:          now,
		ModeOverride: "quiet",
		Rand:         seededRand(1, 2),
		Invoker:      fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped (fresh activity should trigger)", res.Decision)
	}
	if res.SkipCategory != SkipActivityRecent {
		t.Errorf("SkipCategory = %q, want %q", res.SkipCategory, SkipActivityRecent)
	}
}

func TestRunAutodream_SkipCategoryJitterPending(t *testing.T) {
	vault := runVault(t)
	now := time.Now()
	// Plant a recent fired entry with NextFireMinutes=120, so jitter check
	// suppresses the run.
	appendLogEntry(t, vault, AutodreamLogEntry{
		Timestamp:       now.Add(-30 * time.Minute),
		Decision:        decisionFired,
		Mode:            string(ModeActive),
		NextFireMinutes: 120,
	})
	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped (jitter pending)", res.Decision)
	}
	if res.SkipCategory != SkipJitterPending {
		t.Errorf("SkipCategory = %q, want %q", res.SkipCategory, SkipJitterPending)
	}
}

func TestRunAutodream_SkipCategoryCapReached(t *testing.T) {
	vault := runVault(t)
	now := time.Now()
	cfg := enabledCfg()
	cfg.AutoDaydreamMaxPerDayActive = 1
	// Plant one fired active entry today.
	appendLogEntry(t, vault, AutodreamLogEntry{
		Timestamp: now.Add(-1 * time.Hour),
		Decision:  decisionFired,
		Mode:      string(ModeActive),
	})
	in := AutodreamRunInputs{
		VaultRoot:    vault,
		Cfg:          cfg,
		Now:          now,
		ModeOverride: "active",
		Rand:         seededRand(1, 2),
		Invoker:      fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped (cap reached)", res.Decision)
	}
	if res.SkipCategory != SkipCapActiveReached {
		t.Errorf("SkipCategory = %q, want %q", res.SkipCategory, SkipCapActiveReached)
	}
}

func TestRunAutodream_SkipCategoryEmptyOnFire(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       enabledCfg(),
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, want fired; reason=%q", res.Decision, res.Reason)
	}
	if res.SkipCategory != "" {
		t.Errorf("SkipCategory should be empty for a fired run; got %q", res.SkipCategory)
	}
}

func TestRunAutodream_SkipCategoryStrategyForcedNoPair(t *testing.T) {
	vault := t.TempDir()
	// No buffer entries; only one stable memory.
	writeMemFile(t, vault, "Semantic", "s.md",
		"type: semantic\ntitle: S\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 30", "x")

	in := AutodreamRunInputs{
		VaultRoot:        vault,
		Cfg:              enabledCfg(),
		Now:              time.Now(),
		ModeOverride:     "quiet",
		StrategyOverride: "replay",
		Rand:             seededRand(1, 2),
		Invoker:          fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.SkipCategory != SkipStrategyForcedNoPair {
		t.Errorf("SkipCategory = %q, want %q", res.SkipCategory, SkipStrategyForcedNoPair)
	}
}

func TestRunAutodream_BufferPressureLoggedTopLevel(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       enabledCfg(),
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, want fired; reason=%q", res.Decision, res.Reason)
	}
	if res.BufferPressure == nil {
		t.Fatal("BufferPressure not populated on a fired run")
	}
	if res.BufferPressure.Count < 0 {
		t.Errorf("Count negative: %d", res.BufferPressure.Count)
	}
	if res.BufferPressure.Threshold != in.Cfg.BufferThreshold {
		t.Errorf("Threshold = %d, want %d", res.BufferPressure.Threshold, in.Cfg.BufferThreshold)
	}
}

func TestRunAutodream_BufferPressureSerializedInLog(t *testing.T) {
	vault := runVault(t)
	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if err := appendAutodreamLog(vault, res, in.Now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(vault, "Metrics", "autodream_log.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"buffer_pressure"`) {
		t.Errorf("autodream_log missing buffer_pressure field; got: %s", data)
	}
}

func TestRunAutodream_StrategyRollNilForActive(t *testing.T) {
	// Active mode is always exploration with no roll — StrategyRoll should be nil.
	in := AutodreamRunInputs{
		VaultRoot:    runVault(t),
		Cfg:          enabledCfg(),
		Now:          time.Now(),
		ModeOverride: "active",
		Rand:         seededRand(1, 2),
		Invoker:      fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, want fired; reason=%q", res.Decision, res.Reason)
	}
	if res.StrategyRoll != nil {
		t.Errorf("StrategyRoll should be nil for active mode; got %+v", res.StrategyRoll)
	}
	// But buffer pressure should still be populated — universally meaningful.
	if res.BufferPressure == nil {
		t.Error("BufferPressure should be populated even for active mode")
	}
}

func TestRunAutodream_StrategyRollPopulatedForQuiet(t *testing.T) {
	vault := runVault(t)
	in := AutodreamRunInputs{
		VaultRoot:        vault,
		Cfg:              enabledCfg(),
		Now:              time.Now(),
		ModeOverride:     "quiet",
		StrategyOverride: "exploration",
		Rand:             seededRand(1, 2),
		Invoker:          fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, want fired; reason=%q", res.Decision, res.Reason)
	}
	if res.StrategyRoll == nil {
		t.Fatal("StrategyRoll not populated for quiet mode")
	}
	if res.StrategyRoll.Selected != string(StrategyExploration) {
		t.Errorf("Selected = %q, want exploration", res.StrategyRoll.Selected)
	}
	if res.StrategyRoll.ExplorationWeight <= 0 || res.StrategyRoll.ReplayWeight <= 0 {
		t.Errorf("weights not populated: exp=%v rep=%v",
			res.StrategyRoll.ExplorationWeight, res.StrategyRoll.ReplayWeight)
	}
	// Counts should be non-negative; specific values depend on vault content.
	if res.StrategyRoll.RecentCandidates < 0 || res.StrategyRoll.StableCandidates < 0 {
		t.Errorf("negative candidate counts: %+v", res.StrategyRoll)
	}
}

func TestRunAutodream_StrategyRollFellBackDistinguishedFromRolledExploration(t *testing.T) {
	// The rolled-vs-selected distinction is the entire point of the hook —
	// without it, fell_back=true is ambiguous between "no recent material"
	// and "rolled exploration cleanly".
	vault := t.TempDir()
	// No buffer entries → no recent material → forced fallback if replay rolls.
	writeMemFile(t, vault, "Semantic", "s.md",
		"type: semantic\ntitle: S\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 30", "x")

	cfg := enabledCfg()
	cfg.AutoDaydreamStrategyExplorationBase = 0.0 // force replay roll
	cfg.AutoDaydreamStrategyReplayBase = 1.0

	in := AutodreamRunInputs{
		VaultRoot:    vault,
		Cfg:          cfg,
		Now:          time.Now(),
		ModeOverride: "quiet",
		Rand:         seededRand(1, 2),
		Invoker:      fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, want fired; reason=%q", res.Decision, res.Reason)
	}
	if res.StrategyRoll == nil {
		t.Fatal("StrategyRoll not populated")
	}
	if res.StrategyRoll.Rolled != string(StrategyReplay) {
		t.Errorf("Rolled = %q, want replay (forced via weights)", res.StrategyRoll.Rolled)
	}
	if res.StrategyRoll.Selected != string(StrategyExploration) {
		t.Errorf("Selected = %q, want exploration (forced fallback)", res.StrategyRoll.Selected)
	}
	if !res.StrategyRoll.FellBack {
		t.Error("FellBack should be true when replay rolled but no pair available")
	}
	if res.StrategyRoll.RecentCandidates != 0 {
		t.Errorf("RecentCandidates = %d, want 0 (no buffer entries)", res.StrategyRoll.RecentCandidates)
	}
}

func recordingSnapshotFn() (*[]time.Time, AutodreamSnapshotFn) {
	calls := []time.Time{}
	calledRef := &calls
	fn := func(now time.Time) error {
		*calledRef = append(*calledRef, now)
		return nil
	}
	return calledRef, fn
}

func TestRunAutodream_SnapshotFiresOnFire(t *testing.T) {
	calls, fn := recordingSnapshotFn()
	now := time.Now()
	in := AutodreamRunInputs{
		VaultRoot:  runVault(t),
		Cfg:        enabledCfg(),
		Now:        now,
		Rand:       seededRand(1, 2),
		Invoker:    fakeInvoker("ok"),
		SnapshotFn: fn,
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, want fired; reason=%q", res.Decision, res.Reason)
	}
	if len(*calls) != 1 {
		t.Fatalf("snapshot fn called %d times, want 1", len(*calls))
	}
	if !(*calls)[0].Equal(now) {
		t.Errorf("snapshot called with %v, want %v (must use in.Now for join)", (*calls)[0], now)
	}
}

func TestRunAutodream_SnapshotSkippedWhenMasterDisabled(t *testing.T) {
	calls, fn := recordingSnapshotFn()
	in := AutodreamRunInputs{
		VaultRoot:  runVault(t),
		Cfg:        DefaultConfig(), // enabled=false
		Now:        time.Now(),
		Rand:       seededRand(1, 2),
		Invoker:    fakeInvoker("ok"),
		SnapshotFn: fn,
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped", res.Decision)
	}
	if len(*calls) != 0 {
		t.Errorf("snapshot fired during master-disabled skip (%d times); should only fire when run reaches strategy resolution", len(*calls))
	}
}

func TestRunAutodream_SnapshotFiresOnDryRun(t *testing.T) {
	calls, fn := recordingSnapshotFn()
	in := AutodreamRunInputs{
		VaultRoot:  runVault(t),
		Cfg:        enabledCfg(),
		Now:        time.Now(),
		DryRun:     true,
		Rand:       seededRand(1, 2),
		Invoker:    fakeInvoker("ok"),
		SnapshotFn: fn,
	}
	res := RunAutodream(in)
	if res.Decision != decisionDryRun {
		t.Fatalf("Decision = %s, want dry-run", res.Decision)
	}
	if len(*calls) != 1 {
		t.Errorf("snapshot fn called %d times, want 1 (dry-run should still snapshot)", len(*calls))
	}
}

func TestRunAutodream_NilSnapshotFnIsNoOp(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot:  runVault(t),
		Cfg:        enabledCfg(),
		Now:        time.Now(),
		Rand:       seededRand(1, 2),
		Invoker:    fakeInvoker("ok"),
		SnapshotFn: nil,
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("nil SnapshotFn should not break the run; Decision = %s reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_SnapshotFnErrorDoesNotFailRun(t *testing.T) {
	fn := func(_ time.Time) error { return errors.New("simulated snapshot failure") }
	in := AutodreamRunInputs{
		VaultRoot:  runVault(t),
		Cfg:        enabledCfg(),
		Now:        time.Now(),
		Rand:       seededRand(1, 2),
		Invoker:    fakeInvoker("ok"),
		SnapshotFn: fn,
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("snapshot error should not block fire; Decision = %s reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_DisabledMasterToggleSkips(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       DefaultConfig(), // enabled = false
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Errorf("Decision = %s, want skipped", res.Decision)
	}
	if !strings.Contains(res.Reason, "auto_daydream_enabled") {
		t.Errorf("Reason = %q, want mention of auto_daydream_enabled", res.Reason)
	}
}

func TestRunAutodream_ForceBypassesDisabledToggle(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       DefaultConfig(),
		Now:       time.Now(),
		Force:     true,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("Decision = %s, want fired (force should bypass disabled toggle); reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_DryRunRendersPromptWithoutInvoking(t *testing.T) {
	called := false
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       enabledCfg(),
		Now:       time.Now(),
		DryRun:    true,
		Rand:      seededRand(1, 2),
		Invoker: func(_ string) (string, string, error) {
			called = true
			return "should not be called", "", nil
		},
	}
	res := RunAutodream(in)
	if res.Decision != decisionDryRun {
		t.Errorf("Decision = %s, want dry-run; reason=%q", res.Decision, res.Reason)
	}
	if called {
		t.Error("invoker should not be called in dry-run mode")
	}
	if res.Prompt == "" {
		t.Error("dry-run should populate Prompt")
	}
}

func TestRunAutodream_ActiveModeOverride(t *testing.T) {
	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59" // would normally force quiet mode
	cfg.AutoDaydreamQuietHoursTimezone = "utc"
	in := AutodreamRunInputs{
		VaultRoot:    runVault(t),
		Cfg:          cfg,
		Now:          time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		ModeOverride: "active",
		Rand:         seededRand(1, 2),
		Invoker:      fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, reason=%q", res.Decision, res.Reason)
	}
	if res.Mode != ModeActive {
		t.Errorf("Mode = %s, want active (override should win)", res.Mode)
	}
}

func TestRunAutodream_StrategyOverrideExploration(t *testing.T) {
	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59"
	cfg.AutoDaydreamQuietHoursTimezone = "utc"
	cfg.AutoDaydreamQuietSkipWindowMinutes = 0 // disable activity skip for this test
	in := AutodreamRunInputs{
		VaultRoot:        runVault(t),
		Cfg:              cfg,
		Now:              time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		StrategyOverride: "exploration",
		Rand:             seededRand(1, 2),
		Invoker:          fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, reason=%q", res.Decision, res.Reason)
	}
	if res.Strategy != StrategyExploration {
		t.Errorf("Strategy = %s, want exploration", res.Strategy)
	}
	if res.Pair != nil {
		t.Errorf("Pair should be nil for exploration strategy")
	}
	if res.Seed == nil {
		t.Errorf("Seed should be populated for exploration")
	}
}

func TestRunAutodream_StrategyOverrideReplayWithPair(t *testing.T) {
	vault := runVault(t)
	// Make the buffer entry recent so a replay pair can be built.
	bufPath := filepath.Join(vault, "Buffer", "obs.md")
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)
	recent := now.Add(-1 * time.Hour)
	if err := os.Chtimes(bufPath, recent, recent); err != nil {
		t.Fatal(err)
	}

	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59"
	cfg.AutoDaydreamQuietHoursTimezone = "utc"
	cfg.AutoDaydreamQuietSkipWindowMinutes = 0 // disable activity skip — bufPath was just touched
	in := AutodreamRunInputs{
		VaultRoot:        vault,
		Cfg:              cfg,
		Now:              now,
		StrategyOverride: "replay",
		Rand:             seededRand(1, 2),
		Invoker:          fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, reason=%q", res.Decision, res.Reason)
	}
	if res.Strategy != StrategyReplay {
		t.Errorf("Strategy = %s, want replay", res.Strategy)
	}
	if res.Pair == nil {
		t.Errorf("Pair should be populated for replay")
	}
}

func TestRunAutodream_StrategyOverrideReplayNoPairErrors(t *testing.T) {
	vault := t.TempDir()
	// No buffer entries at all → no recent material → no replay pair
	writeMemFile(t, vault, "Semantic", "s.md",
		"type: semantic\ntitle: S\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 30", "x")

	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59"
	cfg.AutoDaydreamQuietHoursTimezone = "utc"
	cfg.AutoDaydreamQuietSkipWindowMinutes = 0
	in := AutodreamRunInputs{
		VaultRoot:        vault,
		Cfg:              cfg,
		Now:              time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC),
		StrategyOverride: "replay",
		Rand:             seededRand(1, 2),
		Invoker:          fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Errorf("Decision = %s, want skipped (no pair available); reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "no pair") {
		t.Errorf("Reason = %q, want mention of no pair", res.Reason)
	}
}

func TestRunAutodream_DailyCapReached(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)

	cfg := enabledCfg()
	cfg.AutoDaydreamMaxPerDayActive = 2

	// Pre-seed the log with 2 fired entries today
	for i := 0; i < 2; i++ {
		appendLogEntry(t, vault, AutodreamLogEntry{
			Timestamp: now.Add(-time.Duration(i) * time.Hour),
			Mode:      string(ModeActive),
			Decision:  decisionFired,
		})
	}

	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       cfg,
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped; reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "daily cap") {
		t.Errorf("Reason = %q, want mention of daily cap", res.Reason)
	}
}

func TestRunAutodream_NoCapBypassesDailyCap(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)

	cfg := enabledCfg()
	cfg.AutoDaydreamMaxPerDayActive = 1
	appendLogEntry(t, vault, AutodreamLogEntry{
		Timestamp: now.Add(-1 * time.Hour),
		Mode:      string(ModeActive),
		Decision:  decisionFired,
	})

	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       cfg,
		Now:       now,
		NoCap:     true,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("Decision = %s, want fired (no-cap should bypass); reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_ActiveModeIgnoresActivitySkipByDefault(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)

	// Heartbeat 1 minute ago — would skip in quiet mode, but active default is window=0
	if err := writeSessionHeartbeat(vault, "s", "/c", now.Add(-1*time.Minute)); err != nil {
		t.Fatal(err)
	}

	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("Decision = %s, want fired (active mode default skip window is 0); reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_QuietModeRespectsActivitySkip(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)

	// Heartbeat 5 minutes ago — well inside the default 60-min quiet skip window
	if err := writeSessionHeartbeat(vault, "s", "/c", now.Add(-5*time.Minute)); err != nil {
		t.Fatal(err)
	}

	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59"
	cfg.AutoDaydreamQuietHoursTimezone = "utc"

	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       cfg,
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped (activity within window); reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "activity") {
		t.Errorf("Reason = %q, want mention of activity", res.Reason)
	}
}

func TestRunAutodream_JitterSkipWhenTooSoon(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)

	// Last fire 30 minutes ago with a 60-minute target — should still skip
	appendLogEntry(t, vault, AutodreamLogEntry{
		Timestamp:       now.Add(-30 * time.Minute),
		Mode:            string(ModeActive),
		Decision:        decisionFired,
		NextFireMinutes: 60,
	})

	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Fatalf("Decision = %s, want skipped (jitter); reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "interval not elapsed") {
		t.Errorf("Reason = %q, want mention of interval", res.Reason)
	}
}

func TestRunAutodream_JitterAllowsAfterTargetElapsed(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)

	appendLogEntry(t, vault, AutodreamLogEntry{
		Timestamp:       now.Add(-90 * time.Minute),
		Mode:            string(ModeActive),
		Decision:        decisionFired,
		NextFireMinutes: 60,
	})

	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("Decision = %s, want fired (90m elapsed >= 60m target); reason=%q", res.Decision, res.Reason)
	}
	if res.NextFireMinutes <= 0 {
		t.Errorf("NextFireMinutes = %d, want > 0 (should re-roll on each fire)", res.NextFireMinutes)
	}
}

func TestRunAutodream_FirstEverRunFires(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       enabledCfg(),
		Now:       time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Errorf("Decision = %s, want fired (no log → no jitter constraint); reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_NoSeedAvailableSkips(t *testing.T) {
	vault := t.TempDir() // no Buffer/, no Memory/ → nothing to seed
	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped {
		t.Errorf("Decision = %s, want skipped (no seed available); reason=%q", res.Decision, res.Reason)
	}
}

func TestRunAutodream_InvokerErrorRecorded(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot: runVault(t),
		Cfg:       enabledCfg(),
		Now:       time.Now(),
		Rand:      seededRand(1, 2),
		Invoker:   errorInvoker(),
	}
	res := RunAutodream(in)
	if res.Decision != decisionError {
		t.Errorf("Decision = %s, want error; reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "simulated invoker error") {
		t.Errorf("Reason = %q, want forwarding of invoker error", res.Reason)
	}
}

func TestRunAutodream_UnknownModeOverrideErrors(t *testing.T) {
	in := AutodreamRunInputs{
		VaultRoot:    runVault(t),
		Cfg:          enabledCfg(),
		Now:          time.Now(),
		ModeOverride: "garbage",
		Rand:         seededRand(1, 2),
		Invoker:      fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionError {
		t.Errorf("Decision = %s, want error; reason=%q", res.Decision, res.Reason)
	}
}

func TestAppendAutodreamLog_RoundTrip(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 14, 0, 0, 0, time.UTC)
	res := AutodreamRunResult{
		Decision: decisionFired,
		Mode:     ModeActive,
		Strategy: StrategyExploration,
		Seed: &Seed{
			Source: "knowledge", Title: "K", FilePath: "/x/Memory/Knowledge/k.md",
		},
		NextFireMinutes: 90,
		Transport:       "test-fake",
		Response:        "agent said something",
	}
	if err := appendAutodreamLog(vault, res, now); err != nil {
		t.Fatal(err)
	}

	// Round-trip read it back via findLastFire and countTodayFires
	last, target, err := findLastFire(vault)
	if err != nil {
		t.Fatal(err)
	}
	if !last.Equal(now) {
		t.Errorf("findLastFire timestamp = %v, want %v", last, now)
	}
	if target != 90*time.Minute {
		t.Errorf("findLastFire target = %v, want 90m", target)
	}
	count, err := countTodayFires(vault, ModeActive, now)
	if err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Errorf("countTodayFires = %d, want 1", count)
	}
}

func TestRollJitterInterval_RespectsBounds(t *testing.T) {
	cfg := enabledCfg()
	cfg.AutoDaydreamIntervalMinMinutes = 60
	cfg.AutoDaydreamIntervalMaxMinutes = 180
	r := seededRand(99, 100)
	for i := 0; i < 200; i++ {
		got := rollJitterInterval(cfg, r)
		if got < 60 || got > 180 {
			t.Fatalf("got %d outside [60, 180]", got)
		}
	}
}

func TestRollJitterInterval_MinAtLeastEqualsMax(t *testing.T) {
	cfg := enabledCfg()
	cfg.AutoDaydreamIntervalMinMinutes = 100
	cfg.AutoDaydreamIntervalMaxMinutes = 100
	got := rollJitterInterval(cfg, seededRand(1, 2))
	if got != 100 {
		t.Errorf("got %d, want 100 (min == max)", got)
	}
}

func TestPickTemplate(t *testing.T) {
	if pickTemplate(ModeActive, StrategyExploration) != TemplateActive {
		t.Error("active+exploration should map to TemplateActive")
	}
	if pickTemplate(ModeActive, StrategyReplay) != TemplateActive {
		t.Error("active+replay (impossible) should still pick TemplateActive")
	}
	if pickTemplate(ModeQuiet, StrategyExploration) != TemplateQuietExploration {
		t.Error("quiet+exploration should map to TemplateQuietExploration")
	}
	if pickTemplate(ModeQuiet, StrategyReplay) != TemplateQuietReplay {
		t.Error("quiet+replay should map to TemplateQuietReplay")
	}
}
