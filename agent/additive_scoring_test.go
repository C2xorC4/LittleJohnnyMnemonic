package main

import (
	"testing"
	"time"
)

// TestAdditiveScoring_RelevanceSteers is the direct test of the change's intent:
// under the additive ACT-R score, a strongly on-topic but dormant memory must be
// able to out-rank an off-topic but recently/frequently accessed one — which the
// old multiplicative form could not do (activation dominated relevance ~50×).
func TestAdditiveScoring_RelevanceSteers(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()
	ctx := []string{"go", "security", "tooling"}

	// Mostly off-topic but warm: moderate base-level activation, weak (1-tag)
	// relevance. Under the old multiplicative score this out-ranks the dormant
	// on-topic memory; under additive it must not (activation gap < β·Δrelevance).
	offTopicActive := &MemoryEntry{
		Type: TypeSemantic, Title: "Mostly off-topic but warm",
		LastAccessed: now.Add(-24 * time.Hour), AccessCount: 5,
		DecayRate: 0.2, Confidence: 0.9, Tags: []string{"go", "noise"},
	}
	// On-topic but cold: low base-level activation, strong relevance.
	onTopicDormant := &MemoryEntry{
		Type: TypeSemantic, Title: "On-topic but cold",
		LastAccessed: now.Add(-2000 * time.Hour), AccessCount: 1,
		DecayRate: 0.2, Confidence: 0.9, Tags: []string{"go", "security", "tooling"},
	}

	hot := ScoreMemory(offTopicActive, ctx, "", cfg, now)
	cold := ScoreMemory(onTopicDormant, ctx, "", cfg, now)
	if cold.Total <= hot.Total {
		t.Errorf("relevance failed to steer: on-topic dormant %.3f did not beat off-topic active %.3f",
			cold.Total, hot.Total)
	}

	// Confidence scales the relevance term: with identical activation and tags,
	// higher confidence yields a higher score (and can't be dragged up by raw
	// activation alone).
	base := MemoryEntry{
		Type: TypeSemantic, LastAccessed: now.Add(-50 * time.Hour), AccessCount: 5,
		DecayRate: 0.2, Tags: []string{"go", "security"},
	}
	hi, lo := base, base
	hi.Confidence, lo.Confidence = 0.95, 0.3
	if ScoreMemory(&hi, ctx, "", cfg, now).Total <= ScoreMemory(&lo, ctx, "", cfg, now).Total {
		t.Error("confidence did not scale the relevance term")
	}
}

// TestActivationSquash verifies the soft squash bounds a count-inflated memory's
// activation (so it cannot dominate), stays monotonic (no ties at the ceiling),
// and lets a relevant memory win once the inflated activation is compressed.
func TestActivationSquash(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig() // MaxActivation = 2.0, β = 8.0

	if got := squashActivation(12.0, cfg.MaxActivation); got >= cfg.MaxActivation {
		t.Errorf("squash did not bound: %.4f >= %.1f", got, cfg.MaxActivation)
	}
	if squashActivation(12.0, cfg.MaxActivation) <= squashActivation(6.0, cfg.MaxActivation) {
		t.Error("squash not monotonic near the top (would tie under a hard cap)")
	}
	if squashActivation(12.0, 0) != 12.0 {
		t.Error("squash should be disabled when max <= 0")
	}

	// End-to-end: a wildly count-inflated off-topic memory must lose to a relevant
	// one once activation is squashed.
	ctx := []string{"go", "security", "tooling"}
	inflatedOffTopic := &MemoryEntry{
		Type: TypeSemantic, Title: "Inflated off-topic",
		LastAccessed: now.Add(-1 * time.Hour), AccessCount: 168117,
		DecayRate: 0.2, Confidence: 0.9, Tags: []string{"unrelated"},
	}
	relevant := &MemoryEntry{
		Type: TypeSemantic, Title: "Relevant but cold",
		LastAccessed: now.Add(-200 * time.Hour), AccessCount: 5,
		DecayRate: 0.2, Confidence: 0.9, Tags: []string{"go", "security", "tooling"},
	}
	if ScoreMemory(relevant, ctx, "", cfg, now).Total <= ScoreMemory(inflatedOffTopic, ctx, "", cfg, now).Total {
		t.Error("squash failed: count-inflated off-topic memory still out-ranks a relevant one")
	}
}

// TestScoringConfigHash_Stable verifies the hash is deterministic, flips on a
// scoring-relevant change, and is inert to a non-scoring change.
func TestScoringConfigHash_Stable(t *testing.T) {
	cfg := DefaultConfig()
	h1 := scoringConfigHash(cfg)
	if h1 != scoringConfigHash(cfg) {
		t.Fatal("hash not deterministic across calls")
	}

	cfg2 := DefaultConfig()
	cfg2.RelevanceWeight = cfg.RelevanceWeight + 1.0
	if scoringConfigHash(cfg2) == h1 {
		t.Error("hash unchanged after a scoring-relevant (RelevanceWeight) change")
	}

	cfg3 := DefaultConfig()
	cfg3.BackupEnabled = !cfg3.BackupEnabled
	if scoringConfigHash(cfg3) != h1 {
		t.Error("hash changed after a non-scoring (BackupEnabled) change")
	}
}

// TestCitationGatedActivation verifies that with gating on, injection access
// (hook/session-start) reinforces neither count nor recency, while genuine use
// (citation) and CLI access do — and that turning gating off restores legacy
// all-sources reinforcement.
func TestCitationGatedActivation(t *testing.T) {
	vault := t.TempDir()
	cfg := DefaultConfig() // CitationGatedActivation = true
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	t1 := t0.Add(24 * time.Hour)

	_ = recordAccess(vault, "memory/m", t0, "cli")  // genuine → reinforces
	_ = recordAccess(vault, "memory/m", t1, "hook") // injection → gated out

	idx := loadAccessIndex(vault, cfg)
	if idx["memory/m"].Count != 1 {
		t.Errorf("injection inflated count: got %d, want 1", idx["memory/m"].Count)
	}
	if !idx["memory/m"].LastAccessed.Equal(t0) {
		t.Errorf("injection advanced recency: got %v, want %v", idx["memory/m"].LastAccessed, t0)
	}

	_ = recordAccess(vault, "memory/m", t1, "citation") // genuine use → reinforces
	idx = loadAccessIndex(vault, cfg)
	if idx["memory/m"].Count != 2 {
		t.Errorf("citation did not reinforce: count = %d, want 2", idx["memory/m"].Count)
	}
	if !idx["memory/m"].LastAccessed.Equal(t1) {
		t.Errorf("citation did not advance recency: got %v, want %v", idx["memory/m"].LastAccessed, t1)
	}

	// Gating off → legacy behavior: all three events reinforce.
	off := DefaultConfig()
	off.CitationGatedActivation = false
	if got := loadAccessIndex(vault, off)["memory/m"].Count; got != 3 {
		t.Errorf("gating-off count = %d, want 3 (cli+hook+citation)", got)
	}
}

// TestAccessIndex_FoldPreservesGating verifies the gating filter is applied at
// fold time, so a folded base reflects only reinforcing access. foldAccessLog
// loads config internally; a bare temp vault yields DefaultConfig (gating on).
func TestAccessIndex_FoldPreservesGating(t *testing.T) {
	vault := t.TempDir()
	t0 := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	_ = recordAccess(vault, "memory/m", t0, "citation")           // reinforces
	_ = recordAccess(vault, "memory/m", t0.Add(time.Hour), "hook") // gated out

	if err := foldAccessLog(vault); err != nil {
		t.Fatal(err)
	}
	idx := loadAccessIndex(vault, DefaultConfig())
	if idx["memory/m"].Count != 1 {
		t.Errorf("fold did not gate injection: count = %d, want 1", idx["memory/m"].Count)
	}
	if !idx["memory/m"].LastAccessed.Equal(t0) {
		t.Errorf("fold recency = %v, want %v (citation only)", idx["memory/m"].LastAccessed, t0)
	}
}
