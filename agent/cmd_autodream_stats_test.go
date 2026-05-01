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

func writeStatsJSONL(t *testing.T, vault, name string, records []any) {
	t.Helper()
	dir := filepath.Join(vault, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, name)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, r := range records {
		line, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatal(err)
		}
	}
}

func TestAggregateAutodreamStats_EmptyReturnsZeroes(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	report := AggregateAutodreamStats(vault, 7, now)
	if report.Activity.TotalRecords != 0 {
		t.Errorf("TotalRecords=%d, want 0", report.Activity.TotalRecords)
	}
	if report.Strategy.QuietRuns != 0 {
		t.Error("expected zero quiet runs")
	}
	if report.Review.Total != 0 {
		t.Error("expected zero review actions")
	}
}

func TestAggregateAutodreamStats_ActivityCountsByDecisionAndMode(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	logRecs := []any{
		AutodreamLogEntry{Timestamp: now.Add(-1 * time.Hour), Decision: decisionFired, Mode: string(ModeActive), DurationMS: 1000},
		AutodreamLogEntry{Timestamp: now.Add(-2 * time.Hour), Decision: decisionFired, Mode: string(ModeQuiet), DurationMS: 3000},
		AutodreamLogEntry{Timestamp: now.Add(-3 * time.Hour), Decision: decisionDryRun, Mode: string(ModeActive)},
		AutodreamLogEntry{Timestamp: now.Add(-4 * time.Hour), Decision: decisionSkipped, SkipCategory: SkipMasterDisabled},
		AutodreamLogEntry{Timestamp: now.Add(-5 * time.Hour), Decision: decisionSkipped, SkipCategory: SkipJitterPending},
		AutodreamLogEntry{Timestamp: now.Add(-6 * time.Hour), Decision: decisionSkipped, SkipCategory: SkipJitterPending},
		AutodreamLogEntry{Timestamp: now.Add(-100 * 24 * time.Hour), Decision: decisionFired}, // outside 7d window
	}
	writeStatsJSONL(t, vault, "autodream_log.jsonl", logRecs)

	report := AggregateAutodreamStats(vault, 7, now)
	if report.Activity.FiredActive != 1 {
		t.Errorf("FiredActive=%d, want 1", report.Activity.FiredActive)
	}
	if report.Activity.FiredQuiet != 1 {
		t.Errorf("FiredQuiet=%d, want 1", report.Activity.FiredQuiet)
	}
	if report.Activity.DryRuns != 1 {
		t.Errorf("DryRuns=%d, want 1", report.Activity.DryRuns)
	}
	if report.Activity.Skipped != 3 {
		t.Errorf("Skipped=%d, want 3", report.Activity.Skipped)
	}
	if report.Activity.SkipsByCategory[SkipJitterPending] != 2 {
		t.Errorf("jitter skips=%d, want 2", report.Activity.SkipsByCategory[SkipJitterPending])
	}
	if report.Activity.MeanDurationMS != 2000 {
		t.Errorf("MeanDurationMS=%d, want 2000", report.Activity.MeanDurationMS)
	}
}

func TestAggregateAutodreamStats_StrategyMixOnlyFromQuiet(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	logRecs := []any{
		// Active fire — should NOT count toward strategy mix
		AutodreamLogEntry{
			Timestamp: now.Add(-1 * time.Hour),
			Decision:  decisionFired,
			Mode:      string(ModeActive),
		},
		// Quiet fires
		AutodreamLogEntry{
			Timestamp: now.Add(-2 * time.Hour),
			Decision:  decisionFired,
			Mode:      string(ModeQuiet),
			StrategyRoll: &StrategyRollRecord{
				Rolled: string(StrategyReplay), Selected: string(StrategyReplay),
				ExplorationWeight: 0.5, ReplayWeight: 0.5,
				RecentCandidates: 5, StableCandidates: 10,
				PairTagOverlap: 0.4,
			},
		},
		AutodreamLogEntry{
			Timestamp: now.Add(-3 * time.Hour),
			Decision:  decisionFired,
			Mode:      string(ModeQuiet),
			StrategyRoll: &StrategyRollRecord{
				Rolled: string(StrategyReplay), Selected: string(StrategyExploration),
				FellBack: true,
				RecentCandidates: 0, StableCandidates: 10,
			},
		},
	}
	writeStatsJSONL(t, vault, "autodream_log.jsonl", logRecs)

	report := AggregateAutodreamStats(vault, 7, now)
	if report.Strategy.QuietRuns != 2 {
		t.Errorf("QuietRuns=%d, want 2", report.Strategy.QuietRuns)
	}
	if report.Strategy.RolledReplay != 2 {
		t.Errorf("RolledReplay=%d, want 2", report.Strategy.RolledReplay)
	}
	if report.Strategy.SelectedReplay != 1 {
		t.Errorf("SelectedReplay=%d, want 1", report.Strategy.SelectedReplay)
	}
	if report.Strategy.FellBack != 1 {
		t.Errorf("FellBack=%d, want 1", report.Strategy.FellBack)
	}
	if report.Strategy.MeanRecentPool != 2.5 {
		t.Errorf("MeanRecentPool=%v, want 2.5", report.Strategy.MeanRecentPool)
	}
	if report.Strategy.NPairs != 1 {
		t.Errorf("NPairs=%d, want 1 (only 1 actual replay-selected)", report.Strategy.NPairs)
	}
}

func TestAggregateAutodreamStats_VerdictsAndOverlap(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	t1 := now.Add(-1 * time.Hour)
	t2 := now.Add(-2 * time.Hour)

	logRecs := []any{
		AutodreamLogEntry{
			Timestamp: t1, Decision: decisionFired, Mode: string(ModeQuiet),
			StrategyRoll: &StrategyRollRecord{
				Rolled: string(StrategyReplay), Selected: string(StrategyReplay), PairTagOverlap: 0.7,
			},
		},
		AutodreamLogEntry{
			Timestamp: t2, Decision: decisionFired, Mode: string(ModeQuiet),
			StrategyRoll: &StrategyRollRecord{
				Rolled: string(StrategyReplay), Selected: string(StrategyReplay), PairTagOverlap: 0.1,
			},
		},
	}
	replays := []any{
		map[string]any{"timestamp": t1, "verdict": "unrelated", "recent_path": "/a", "stable_path": "/b"},
		map[string]any{"timestamp": t2, "verdict": "unrelated", "recent_path": "/c", "stable_path": "/d"},
	}
	writeStatsJSONL(t, vault, "autodream_log.jsonl", logRecs)
	writeStatsJSONL(t, vault, "replay_log.jsonl", replays)

	report := AggregateAutodreamStats(vault, 7, now)
	if report.Verdicts.Total != 2 {
		t.Errorf("Total=%d, want 2", report.Verdicts.Total)
	}
	if report.Verdicts.Unrelated != 2 {
		t.Errorf("Unrelated=%d, want 2", report.Verdicts.Unrelated)
	}
	mean := report.Verdicts.OverlapByVerdict["unrelated"]
	if mean < 0.39 || mean > 0.41 {
		t.Errorf("mean overlap for unrelated = %v, want ~0.4", mean)
	}
}

func TestAggregateAutodreamStats_ReviewIsContaminationFreeSignal(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	reviews := []any{
		ReviewActionRecord{Timestamp: now.Add(-1 * time.Hour), Action: "accept", DaydreamKind: "exploration", QueueType: "exploration"},
		ReviewActionRecord{Timestamp: now.Add(-2 * time.Hour), Action: "reject", DaydreamKind: "exploration", QueueType: "exploration"},
		ReviewActionRecord{Timestamp: now.Add(-3 * time.Hour), Action: "promote", DaydreamKind: "replay-refine", QueueType: "refine"},
		ReviewActionRecord{Timestamp: now.Add(-4 * time.Hour), Action: "skip", DaydreamKind: "replay-contradict", QueueType: "critical"},
	}
	writeStatsJSONL(t, vault, "daydream_review_log.jsonl", reviews)

	report := AggregateAutodreamStats(vault, 7, now)
	if report.Review.Total != 4 {
		t.Errorf("Total=%d, want 4", report.Review.Total)
	}
	if report.Review.ByAction["accept"] != 1 {
		t.Errorf("accept count = %d, want 1", report.Review.ByAction["accept"])
	}
	if report.Review.ByQueue["exploration"] != 2 {
		t.Errorf("exploration queue count = %d, want 2", report.Review.ByQueue["exploration"])
	}
	rate := report.Review.AcceptanceByKind["exploration"]
	if rate != 0.5 {
		t.Errorf("exploration acceptance rate = %v, want 0.5", rate)
	}
}

func TestAggregateAutodreamStats_OutcomesPromotionRateByValue(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	outcomes := []any{
		ConsolidationOutcome{Timestamp: now.Add(-1 * time.Hour), Action: string(ActionPromote), ValueVerdict: string(ValueValuable)},
		ConsolidationOutcome{Timestamp: now.Add(-2 * time.Hour), Action: string(ActionPromote), ValueVerdict: string(ValueValuable)},
		ConsolidationOutcome{Timestamp: now.Add(-3 * time.Hour), Action: string(ActionDiscard), ValueVerdict: string(ValueValuable)},
		ConsolidationOutcome{Timestamp: now.Add(-4 * time.Hour), Action: string(ActionDiscard), ValueVerdict: string(ValueLowValue)},
		ConsolidationOutcome{Timestamp: now.Add(-30 * time.Hour), Action: string(ActionPromote), AttributionDegraded: true, ValueVerdict: string(ValueValuable)},
	}
	writeStatsJSONL(t, vault, "consolidation_outcomes.jsonl", outcomes)

	report := AggregateAutodreamStats(vault, 7, now)
	if report.Outcomes.Total != 5 {
		t.Errorf("Total=%d, want 5", report.Outcomes.Total)
	}
	if report.Outcomes.AttributionDegradedCount != 1 {
		t.Errorf("AttributionDegradedCount=%d, want 1", report.Outcomes.AttributionDegradedCount)
	}
	rate := report.Outcomes.PromotionRateByValue[string(ValueValuable)]
	// 3 promoted out of 4 valuable = 0.75
	if rate < 0.74 || rate > 0.76 {
		t.Errorf("valuable promotion rate = %v, want 0.75", rate)
	}
	if report.Outcomes.PromotionRateByValue[string(ValueLowValue)] != 0 {
		t.Errorf("low-value promotion rate = %v, want 0",
			report.Outcomes.PromotionRateByValue[string(ValueLowValue)])
	}
}

func TestAggregateAutodreamStats_WindowFilters(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	// Two records: one inside 7d, one outside
	writeStatsJSONL(t, vault, "autodream_log.jsonl", []any{
		AutodreamLogEntry{Timestamp: now.Add(-2 * 24 * time.Hour), Decision: decisionFired, Mode: string(ModeActive)},
		AutodreamLogEntry{Timestamp: now.Add(-15 * 24 * time.Hour), Decision: decisionFired, Mode: string(ModeActive)},
	})

	report := AggregateAutodreamStats(vault, 7, now)
	if report.Activity.FiredActive != 1 {
		t.Errorf("7d window: FiredActive=%d, want 1", report.Activity.FiredActive)
	}

	allTime := AggregateAutodreamStats(vault, 0, now)
	if allTime.Activity.FiredActive != 2 {
		t.Errorf("all-time: FiredActive=%d, want 2", allTime.Activity.FiredActive)
	}
}

func TestRenderAutodreamStats_ContainsContaminationLabels(t *testing.T) {
	report := AutodreamStatsReport{
		WindowDays: 7,
		Activity:   ActivitySummary{TotalRecords: 5, FiredActive: 2},
		Review:     ReviewSummary{Total: 3, ByAction: map[string]int{"accept": 2, "reject": 1}},
	}
	var buf bytes.Buffer
	RenderAutodreamStats(&buf, report)
	out := buf.String()

	wants := []string{
		"self-inflated",
		"CONTAMINATION-FREE",
		"Activity",
		"Review actions",
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			t.Errorf("output missing %q\noutput:\n%s", w, out)
		}
	}
}

func TestRenderAutodreamStats_NoReviewShowsTuningPrompt(t *testing.T) {
	report := AutodreamStatsReport{WindowDays: 7}
	var buf bytes.Buffer
	RenderAutodreamStats(&buf, report)
	out := buf.String()
	if !strings.Contains(out, "no review actions") {
		t.Errorf("expected the empty-review prompt; got:\n%s", out)
	}
}

func TestFormatPercent(t *testing.T) {
	if got := formatPercent(0.5); got != "50.0%" {
		t.Errorf("got %q, want 50.0%%", got)
	}
}
