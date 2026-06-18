package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestExtractReferencedMemoryKeys_WikiLinkAndSlug(t *testing.T) {
	loaded := []string{
		"memory/feedback/repo_trust_protocol",
		"memory/semantic/ljm_design_history",
		"memory/user/user_profile",
	}
	text := `Per [[Memory/Feedback/repo_trust_protocol]] we should verify paths.
Also relevant: ljm design history and unrelated content.`

	refs := extractReferencedMemoryKeys(text, loaded)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d: %v", len(refs), refs)
	}
	seen := map[string]bool{}
	for _, r := range refs {
		seen[r] = true
	}
	if !seen["memory/feedback/repo_trust_protocol"] {
		t.Error("missing wiki-link match")
	}
	if !seen["memory/semantic/ljm_design_history"] {
		t.Error("missing slug match")
	}
}

func TestClassifyMemoryUsageOutcome(t *testing.T) {
	cases := []struct {
		inj, ref int
		want     string
	}{
		{0, 0, "no_injection"},
		{5, 0, "none"},
		{5, 2, "partial"},
		{5, 5, "referenced"},
		{3, 4, "referenced"},
	}
	for _, c := range cases {
		if got := classifyMemoryUsageOutcome(c.inj, c.ref); got != c.want {
			t.Errorf("classify(%d,%d) = %q, want %q", c.inj, c.ref, got, c.want)
		}
	}
}

func TestModelFromGrokUpdatesTranscript(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.jsonl")
	lines := []string{
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","_meta":{"modelId":"grok-composer-2.5-fast"}}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"hi"}}}}`,
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","_meta":{"modelId":"claude-opus-4-8"}}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := modelFromGrokUpdatesTranscript(path); got != "claude-opus-4-8" {
		t.Fatalf("model = %q, want claude-opus-4-8", got)
	}
}

func TestLoadMemoryUsage_AggregatesByDayAndModel(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "memory_usage_log.jsonl")
	rows := []string{
		`{"timestamp":"2026-06-17T10:00:00Z","model":"claude-opus-4-8","runtime_host":"claude-code","memories_injected":8,"memories_referenced":0,"reference_rate":0,"outcome":"none"}`,
		`{"timestamp":"2026-06-17T11:00:00Z","model":"grok-composer-2.5-fast","runtime_host":"grok-build","memories_injected":8,"memories_referenced":4,"reference_rate":0.5,"outcome":"partial"}`,
		`{"timestamp":"2026-06-18T09:00:00Z","model":"grok-composer-2.5-fast","runtime_host":"grok-build","memories_injected":8,"memories_referenced":8,"reference_rate":1,"outcome":"referenced"}`,
	}
	if err := os.WriteFile(logPath, []byte(strings.Join(rows, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	byDay, byModel, summary, err := loadMemoryUsage(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(byDay) != 2 {
		t.Fatalf("expected 2 days, got %d", len(byDay))
	}
	if byDay[0].Date != "2026-06-17" || byDay[0].ZeroUsageTurns != 1 {
		t.Fatalf("day1: %+v", byDay[0])
	}
	if byDay[1].UsageRate != 1.0 {
		t.Fatalf("day2 usage rate = %f, want 1.0", byDay[1].UsageRate)
	}
	if len(byModel) < 2 {
		t.Fatalf("expected >=2 model rows, got %d", len(byModel))
	}
	if summary.TotalTurns != 3 || summary.LiveTurns != 3 {
		t.Fatalf("summary: %+v", summary)
	}
	if byDay[0].LiveInjected != 16 || byDay[0].LiveReferenced != 4 {
		t.Fatalf("day1 live injected/referenced: %+v", byDay[0])
	}
	for _, m := range byModel {
		if m.Model == "grok-composer-2.5-fast" {
			if m.LiveInjected != 16 || m.LiveReferenced != 12 {
				t.Fatalf("grok live injected/referenced = %d/%d, want 16/12", m.LiveInjected, m.LiveReferenced)
			}
			if m.LiveUsageRate != 0.75 {
				t.Fatalf("grok live usage rate = %f, want 0.75", m.LiveUsageRate)
			}
		}
	}
}

func TestBuildZeroRecallLogEntry(t *testing.T) {
	e := buildZeroRecallLogEntry("sess-1", 42)
	if !e.ZeroRecall || e.Total != 0 {
		t.Fatalf("unexpected entry: %+v", e)
	}
	if e.SessionID != "sess-1" || e.PromptLen != 42 {
		t.Fatalf("session/prompt: %+v", e)
	}
}