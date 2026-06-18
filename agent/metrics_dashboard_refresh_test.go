package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildDashboardPayload_IncludesBuffer(t *testing.T) {
	vault := t.TempDir()
	for _, dir := range []string{"Metrics", "Buffer", "System"} {
		if err := os.MkdirAll(filepath.Join(vault, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vault, "System", "Config.md"), []byte("```yaml\nbuffer_threshold: 10\n```\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "Buffer", "2026-06-17_test.md"), []byte("---\ntype: buffer\n---\nbody\n"), 0644); err != nil {
		t.Fatal(err)
	}

	payload, err := buildDashboardPayload(vault)
	if err != nil {
		t.Fatal(err)
	}
	if payload.Buffer.Count != 1 {
		t.Fatalf("buffer count = %d, want 1", payload.Buffer.Count)
	}
	if payload.Buffer.Threshold != 10 {
		t.Fatalf("buffer threshold = %d, want 10", payload.Buffer.Threshold)
	}
}

func TestBuildDashboardPayload_UsageSummaryBackfill(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Metrics"), 0755); err != nil {
		t.Fatal(err)
	}
	log := `{"timestamp":"2026-06-16T10:00:00Z","memories_injected":8,"memories_referenced":2,"outcome":"partial","backfill":true,"model":"grok-composer-2.5-fast","runtime_host":"grok-build"}
{"timestamp":"2026-06-18T11:00:00Z","memories_injected":8,"memories_referenced":0,"outcome":"none","model":"claude-opus-4-8","runtime_host":"claude-code"}
`
	if err := os.WriteFile(filepath.Join(vault, "Metrics", "memory_usage_log.jsonl"), []byte(log), 0644); err != nil {
		t.Fatal(err)
	}

	payload, err := buildDashboardPayload(vault)
	if err != nil {
		t.Fatal(err)
	}
	if payload.UsageSummary.TotalTurns != 2 {
		t.Fatalf("total turns = %d, want 2", payload.UsageSummary.TotalTurns)
	}
	if payload.UsageSummary.BackfillTurns != 1 || payload.UsageSummary.LiveTurns != 1 {
		t.Fatalf("backfill/live = %d/%d", payload.UsageSummary.BackfillTurns, payload.UsageSummary.LiveTurns)
	}
	if payload.UsageByDay[0].BackfillTurns != 1 {
		t.Fatalf("day backfill turns: %+v", payload.UsageByDay[0])
	}
	if len(payload.UsageByModel) != 2 {
		t.Fatalf("usage_by_model len = %d, want 2", len(payload.UsageByModel))
	}
	for _, m := range payload.UsageByModel {
		if m.Model == "claude-opus-4-8" {
			if m.LiveInjected != 8 || m.LiveReferenced != 0 || m.LiveUsageRate != 0 {
				t.Fatalf("claude live stats: %+v", m)
			}
		}
		if m.Model == "grok-composer-2.5-fast" {
			if m.LiveInjected != 0 || m.LiveReferenced != 0 {
				t.Fatalf("grok should be backfill-only: %+v", m)
			}
		}
	}
}

func TestWriteMetricsDashboard_WritesHTML(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Metrics"), 0755); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(vault, "Metrics", "dashboard.html")
	if err := WriteMetricsDashboard(vault, out); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	body := string(data)
	for _, want := range []string{"Memory Health Cockpit", "chart-hero", "kpi-strip", "dashboard-data", "application/json"} {
		if !strings.Contains(body, want) {
			t.Errorf("dashboard HTML missing %q", want)
		}
	}
}

func TestWriteFileAtomic_ReplacesWithoutTruncatingVisibleFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dashboard.html")
	if err := writeFileAtomic(path, []byte("<html>v1</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := writeFileAtomic(path, []byte("<html>v2-complete</html>"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "<html>v2-complete</html>" {
		t.Fatalf("atomic write content = %q", string(data))
	}
}

func TestMaybeRefreshDashboard_Cooldown(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Metrics"), 0755); err != nil {
		t.Fatal(err)
	}

	MaybeRefreshDashboard(vault, dashReasonAutodream)
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "dashboard.html")); err != nil {
		t.Fatalf("first refresh should write dashboard: %v", err)
	}

	before, err := os.ReadFile(filepath.Join(vault, "Metrics", "dashboard.html"))
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(20 * time.Millisecond)
	MaybeRefreshDashboard(vault, dashReasonAutodream)
	after, err := os.ReadFile(filepath.Join(vault, "Metrics", "dashboard.html"))
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Error("second autodream refresh within cooldown should not rewrite dashboard")
	}
}

func TestMaybeRefreshDashboard_ConsolidationBypassesCooldown(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Metrics"), 0755); err != nil {
		t.Fatal(err)
	}

	MaybeRefreshDashboard(vault, dashReasonAutodream)
	first, _ := os.Stat(filepath.Join(vault, "Metrics", "dashboard.html"))
	time.Sleep(20 * time.Millisecond)
	MaybeRefreshDashboard(vault, dashReasonConsolidation)
	second, err := os.Stat(filepath.Join(vault, "Metrics", "dashboard.html"))
	if err != nil {
		t.Fatal(err)
	}
	if !second.ModTime().After(first.ModTime()) {
		t.Error("consolidation refresh should bypass cooldown and rewrite dashboard")
	}
}

func TestDeriveVaultRootFromPath(t *testing.T) {
	root := `D:\Repos\LLM\LittleJohnnyMnemonic`
	got := deriveVaultRootFromPath(filepath.Join(root, "Memory", "Knowledge", "foo.md"))
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(root)) {
		t.Fatalf("deriveVaultRootFromPath = %q, want %q", got, root)
	}
}

func TestAutodreamCountsForDashboard(t *testing.T) {
	if !autodreamCountsForDashboard("fired") {
		t.Error("fired should count")
	}
	if autodreamCountsForDashboard("dry-run") {
		t.Error("dry-run should not count")
	}
	if autodreamCountsForDashboard("skip: cooldown") {
		t.Error("skip should not count")
	}
}