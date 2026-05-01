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

func TestParseReplayVerdict_AllFourStrict(t *testing.T) {
	cases := []struct {
		body string
		want ReplayVerdict
	}{
		{"some reasoning here.\n\nVerdict: reinforce\n", VerdictReinforce},
		{"...\nVerdict: refine", VerdictRefine},
		{"Verdict: contradict", VerdictContradict},
		{"long body\nVerdict: unrelated\n", VerdictUnrelated},
	}
	for _, c := range cases {
		got, err := ParseReplayVerdict(c.body)
		if err != nil {
			t.Errorf("body=%q: %v", c.body, err)
			continue
		}
		if got != c.want {
			t.Errorf("body=%q: got %s, want %s", c.body, got, c.want)
		}
	}
}

func TestParseReplayVerdict_RelationshipKeyAlsoAccepted(t *testing.T) {
	got, err := ParseReplayVerdict("...\nRelationship: refine\n")
	if err != nil {
		t.Fatal(err)
	}
	if got != VerdictRefine {
		t.Errorf("got %s, want refine", got)
	}
}

func TestParseReplayVerdict_CaseInsensitive(t *testing.T) {
	for _, s := range []string{"VERDICT: REINFORCE", "verdict: REINFORCE", "Verdict: Reinforce"} {
		got, err := ParseReplayVerdict(s)
		if err != nil {
			t.Errorf("body=%q: %v", s, err)
			continue
		}
		if got != VerdictReinforce {
			t.Errorf("body=%q: got %s, want reinforce", s, got)
		}
	}
}

func TestParseReplayVerdict_RequiresKeyDoesNotKeywordFish(t *testing.T) {
	// Reasoning text mentions every verdict word but never produces a key.
	// Strict parser must return ErrVerdictNotFound — false-positive routing
	// is worse than no routing.
	body := `The trace could either reinforce, refine, contradict, or be unrelated.
After thinking about it, I'm not sure.`
	_, err := ParseReplayVerdict(body)
	if !errors.Is(err, ErrVerdictNotFound) {
		t.Errorf("err = %v, want ErrVerdictNotFound", err)
	}
}

func TestParseReplayVerdict_LastVerdictLineWins(t *testing.T) {
	// Last-wins semantics: when the agent self-revises (drafts one verdict
	// then changes to another), the LAST line is its commitment. The prompt
	// explicitly tells the agent to end its response with a single Verdict
	// line, so the final occurrence is authoritative.
	body := `Verdict: refine

Actually on reflection:
Verdict: reinforce
`
	got, err := ParseReplayVerdict(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != VerdictReinforce {
		t.Errorf("got %s, want reinforce (last-wins)", got)
	}
}

func TestParseReplayVerdict_LastWinsAcrossThreeVerdicts(t *testing.T) {
	// Three distinct verdicts in order. Verify the parser returns the last
	// regardless of which verdicts appear earlier.
	body := `My initial read suggests reinforce.

Verdict: refine

After re-reading:

Verdict: contradict

Final answer:

Verdict: unrelated
`
	got, err := ParseReplayVerdict(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != VerdictUnrelated {
		t.Errorf("got %s, want unrelated (last of three)", got)
	}
}

func TestParseReplayVerdict_HandlesMarkdownBoldEmphasis(t *testing.T) {
	// Live-fire responses commonly format the verdict line with markdown
	// bold (e.g., **Verdict:** refine). The parser must accept this.
	cases := []struct {
		body string
		want ReplayVerdict
	}{
		{"**Verdict:** refine", VerdictRefine},
		{"*Verdict:* contradict", VerdictContradict},
		{"**Verdict: reinforce**", VerdictReinforce},
		{"**Relationship:** refine", VerdictRefine},
		{"prefix text\n**Verdict:** unrelated\nsuffix", VerdictUnrelated},
	}
	for _, c := range cases {
		got, err := ParseReplayVerdict(c.body)
		if err != nil {
			t.Errorf("body=%q: %v", c.body, err)
			continue
		}
		if got != c.want {
			t.Errorf("body=%q: got %s, want %s", c.body, got, c.want)
		}
	}
}

func TestParseReplayVerdict_LastWinsWithMixedFormatting(t *testing.T) {
	// Plain verdict early, markdown verdict late — last (markdown) wins.
	body := `Initial thought:

Verdict: reinforce

Wait, on reflection these are quite different.

**Verdict:** unrelated`
	got, err := ParseReplayVerdict(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != VerdictUnrelated {
		t.Errorf("got %s, want unrelated (last-wins regardless of formatting)", got)
	}
}

func TestParseReplayVerdict_LineContextRequired(t *testing.T) {
	// Verdict word inline with prose (no preceding "Verdict:" or
	// "Relationship:" key) does NOT match. This is the safety property
	// that prevents accidental routing.
	body := "I think the verdict is reinforce, but I'm not sure."
	_, err := ParseReplayVerdict(body)
	if !errors.Is(err, ErrVerdictNotFound) {
		t.Errorf("err = %v, want ErrVerdictNotFound (no Verdict: key prefix)", err)
	}
}

// helper to read a JSONL file as a slice of decoded entries (any type)
func readJSONLLines(t *testing.T, path string) [][]byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines [][]byte
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		if line == "" {
			continue
		}
		lines = append(lines, []byte(line))
	}
	return lines
}

func samplePairForRouting() *SeedPair {
	return &SeedPair{
		Recent: Seed{
			Source: "buffer", Title: "recent_obs",
			FilePath: "/v/Buffer/recent_obs.md",
		},
		Stable: Seed{
			Source: "semantic", Title: "Stable Trait",
			FilePath: "/v/Memory/Semantic/stable_trait.md",
		},
	}
}

func TestRouteReplayResult_NilPairErrors(t *testing.T) {
	err := RouteReplayResult(t.TempDir(), DefaultConfig(), time.Now(), VerdictReinforce, nil, "x")
	if err == nil {
		t.Error("expected error for nil pair")
	}
}

func TestRouteReplayResult_AllVerdictsAppendAuditLog(t *testing.T) {
	for _, v := range []ReplayVerdict{VerdictReinforce, VerdictRefine, VerdictContradict, VerdictUnrelated} {
		t.Run(string(v), func(t *testing.T) {
			vault := t.TempDir()
			now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)
			err := RouteReplayResult(vault, DefaultConfig(), now, v, samplePairForRouting(), "Verdict: "+string(v)+"\nbecause reasons")
			if err != nil {
				t.Fatalf("err: %v", err)
			}
			lines := readJSONLLines(t, filepath.Join(vault, "Metrics", "replay_log.jsonl"))
			if len(lines) != 1 {
				t.Fatalf("audit log has %d lines, want 1", len(lines))
			}
			var entry ReplayLogEntry
			if err := json.Unmarshal(lines[0], &entry); err != nil {
				t.Fatal(err)
			}
			if entry.Verdict != string(v) {
				t.Errorf("audit verdict = %s, want %s", entry.Verdict, v)
			}
		})
	}
}

func TestRouteReplayResult_ReinforceQueuesConfidenceDelta(t *testing.T) {
	vault := t.TempDir()
	cfg := DefaultConfig()
	cfg.ConfidenceReinforce = 0.15
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)

	if err := RouteReplayResult(vault, cfg, now, VerdictReinforce, samplePairForRouting(), "Verdict: reinforce"); err != nil {
		t.Fatal(err)
	}
	lines := readJSONLLines(t, filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("reinforcements has %d lines, want 1", len(lines))
	}
	var entry ReplayReinforcementEntry
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.ConfidenceDelta != 0.15 {
		t.Errorf("delta = %v, want 0.15 (from cfg.ConfidenceReinforce)", entry.ConfidenceDelta)
	}
	if entry.StableMemoryPath != "/v/Memory/Semantic/stable_trait.md" {
		t.Errorf("StableMemoryPath = %q", entry.StableMemoryPath)
	}
	if entry.Applied {
		t.Error("Applied should be false until consolidation processes it")
	}
}

func TestRouteReplayResult_ContradictQueuesReview(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)

	if err := RouteReplayResult(vault, DefaultConfig(), now, VerdictContradict, samplePairForRouting(), "Verdict: contradict"); err != nil {
		t.Fatal(err)
	}
	lines := readJSONLLines(t, filepath.Join(vault, "Metrics", "replay_contradictions.jsonl"))
	if len(lines) != 1 {
		t.Fatalf("contradictions has %d lines, want 1", len(lines))
	}
	var entry ReplayContradictionEntry
	if err := json.Unmarshal(lines[0], &entry); err != nil {
		t.Fatal(err)
	}
	if entry.Reviewed {
		t.Error("Reviewed should be false until user adjudicates")
	}
}

func TestRouteReplayResult_RefineHasNoExtraRouting(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)

	if err := RouteReplayResult(vault, DefaultConfig(), now, VerdictRefine, samplePairForRouting(), "Verdict: refine"); err != nil {
		t.Fatal(err)
	}
	// Audit log should exist; no reinforcements, no contradictions files
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_log.jsonl")); err != nil {
		t.Errorf("expected audit log to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl")); !os.IsNotExist(err) {
		t.Errorf("reinforcements file should not exist for refine verdict")
	}
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_contradictions.jsonl")); !os.IsNotExist(err) {
		t.Errorf("contradictions file should not exist for refine verdict")
	}
}

func TestRouteReplayResult_UnrelatedHasOnlyAuditEntry(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC)

	if err := RouteReplayResult(vault, DefaultConfig(), now, VerdictUnrelated, samplePairForRouting(), "Verdict: unrelated"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_log.jsonl")); err != nil {
		t.Errorf("expected audit log to exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl")); !os.IsNotExist(err) {
		t.Errorf("reinforcements file should not exist for unrelated verdict")
	}
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_contradictions.jsonl")); !os.IsNotExist(err) {
		t.Errorf("contradictions file should not exist for unrelated verdict")
	}
}

func TestRouteReplayResult_UnknownVerdictErrors(t *testing.T) {
	err := RouteReplayResult(t.TempDir(), DefaultConfig(), time.Now(), ReplayVerdict("xyz"), samplePairForRouting(), "")
	if err == nil {
		t.Error("expected error for unknown verdict")
	}
}

// Integration test: RunAutodream → replay fire → verdict parsed → routed.
func TestRunAutodream_ReplayFireRoutesByVerdict(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	// Make buffer entry recent so a replay pair can be built.
	bufPath := filepath.Join(vault, "Buffer", "obs.md")
	recent := now.Add(-1 * time.Hour)
	if err := os.Chtimes(bufPath, recent, recent); err != nil {
		t.Fatal(err)
	}

	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59"
	cfg.AutoDaydreamQuietHoursTimezone = "utc"
	cfg.AutoDaydreamQuietSkipWindowMinutes = 0

	in := AutodreamRunInputs{
		VaultRoot:        vault,
		Cfg:              cfg,
		Now:              now,
		StrategyOverride: "replay",
		Rand:             seededRand(1, 2),
		Invoker: func(_ string) (string, string, error) {
			// Agent's response with a strict verdict line
			return "After analysis, the recent trace strengthens the existing\npattern.\n\nVerdict: reinforce", "test-fake", nil
		},
	}
	res := RunAutodream(in)
	if res.Decision != decisionFired {
		t.Fatalf("Decision = %s, reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "verdict: reinforce") {
		t.Errorf("Reason = %q, want mention of 'verdict: reinforce'", res.Reason)
	}
	// Audit log should have one entry
	lines := readJSONLLines(t, filepath.Join(vault, "Metrics", "replay_log.jsonl"))
	if len(lines) != 1 {
		t.Errorf("replay_log has %d lines, want 1", len(lines))
	}
	// Reinforcements should have one entry
	rlines := readJSONLLines(t, filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl"))
	if len(rlines) != 1 {
		t.Errorf("replay_reinforcements has %d lines, want 1", len(rlines))
	}
}

func TestRunAutodream_ReplayFireWithUnparseableVerdict(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 4, 30, 12, 0, 0, 0, time.UTC)

	bufPath := filepath.Join(vault, "Buffer", "obs.md")
	recent := now.Add(-1 * time.Hour)
	if err := os.Chtimes(bufPath, recent, recent); err != nil {
		t.Fatal(err)
	}

	cfg := enabledCfg()
	cfg.AutoDaydreamQuietHours = "00:00-23:59"
	cfg.AutoDaydreamQuietHoursTimezone = "utc"
	cfg.AutoDaydreamQuietSkipWindowMinutes = 0

	in := AutodreamRunInputs{
		VaultRoot:        vault,
		Cfg:              cfg,
		Now:              now,
		StrategyOverride: "replay",
		Rand:             seededRand(1, 2),
		Invoker: func(_ string) (string, string, error) {
			// Response with no verdict line
			return "I read both. They seem related. Not sure what to call it.", "test-fake", nil
		},
	}
	res := RunAutodream(in)
	// The fire still succeeds — verdict-parse-failed is recorded as the
	// reason but the run is not downgraded. Breadcrumb file (if agent wrote
	// one) is the source of truth at consolidation time.
	if res.Decision != decisionFired {
		t.Errorf("Decision = %s, want fired (parse failure shouldn't fail the run); reason=%q", res.Decision, res.Reason)
	}
	if !strings.Contains(res.Reason, "verdict-parse-failed") {
		t.Errorf("Reason = %q, want verdict-parse-failed marker", res.Reason)
	}
	// No replay_log / reinforcements / contradictions should be written
	// when parsing fails.
	if _, err := os.Stat(filepath.Join(vault, "Metrics", "replay_log.jsonl")); !os.IsNotExist(err) {
		t.Errorf("replay_log should not be written on parse failure")
	}
}
