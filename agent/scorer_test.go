package main

import (
	"math"
	"testing"
	"time"
)

func TestComputeActivation_WorkedExample(t *testing.T) {
	// From Scoring.md: access_count=7, recency=96h, decay=0.3
	// Expected: ln(7 × 96^(-0.3)) = ln(7 × 0.247) ≈ ln(1.73) ≈ 0.549
	now := time.Now()
	m := &MemoryEntry{
		LastAccessed: now.Add(-96 * time.Hour),
		AccessCount:  7,
		DecayRate:    0.3,
	}

	activation := ComputeActivation(m, now)
	expected := math.Log(7 * math.Pow(96, -0.3))

	if math.Abs(activation-expected) > 0.01 {
		t.Errorf("activation = %.4f, expected ≈ %.4f (from Scoring.md worked example)", activation, expected)
	}
}

func TestComputeActivation_StaleMemory(t *testing.T) {
	// From Scoring.md: 720 hours (30 days), should give negative activation
	// Expected: ln(7 × 720^(-0.3)) ≈ ln(0.917) ≈ -0.087
	now := time.Now()
	m := &MemoryEntry{
		LastAccessed: now.Add(-720 * time.Hour),
		AccessCount:  7,
		DecayRate:    0.3,
	}

	activation := ComputeActivation(m, now)
	if activation >= 0 {
		t.Errorf("stale memory should have negative activation, got %.4f", activation)
	}

	expected := math.Log(7 * math.Pow(720, -0.3))
	if math.Abs(activation-expected) > 0.01 {
		t.Errorf("activation = %.4f, expected ≈ %.4f", activation, expected)
	}
}

func TestComputeActivation_RecentHighAccess(t *testing.T) {
	now := time.Now()
	m := &MemoryEntry{
		LastAccessed: now.Add(-1 * time.Hour),
		AccessCount:  20,
		DecayRate:    0.3,
	}

	activation := ComputeActivation(m, now)
	if activation <= 0 {
		t.Errorf("recently accessed high-count memory should have positive activation, got %.4f", activation)
	}
}

func TestComputeActivation_DecayRateEffect(t *testing.T) {
	now := time.Now()
	base := MemoryEntry{
		LastAccessed: now.Add(-48 * time.Hour),
		AccessCount:  5,
	}

	// Lower decay should give higher activation
	slow := base
	slow.DecayRate = 0.2
	fast := base
	fast.DecayRate = 0.7

	slowAct := ComputeActivation(&slow, now)
	fastAct := ComputeActivation(&fast, now)

	if slowAct <= fastAct {
		t.Errorf("slower decay should give higher activation: slow=%.4f, fast=%.4f", slowAct, fastAct)
	}
}

func TestComputeRelevance_TagMatching(t *testing.T) {
	tests := []struct {
		name        string
		memoryTags  []string
		contextTags []string
		minExpected float64
		maxExpected float64
	}{
		{"no overlap", []string{"go", "tooling"}, []string{"python", "web"}, 0.0, 0.01},
		{"one match", []string{"go", "tooling"}, []string{"go", "web"}, 0.19, 0.21},
		{"two matches", []string{"go", "tooling", "offensive"}, []string{"go", "tooling"}, 0.39, 0.41},
		{"full overlap", []string{"go", "tooling"}, []string{"go", "tooling", "offensive", "security", "red-team"}, 0.39, 0.41},
		{"case insensitive", []string{"Go", "TOOLING"}, []string{"go", "tooling"}, 0.39, 0.41},
		{"no context", []string{"go", "tooling"}, nil, 0.49, 0.51}, // returns 0.5 neutral
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			m := &MemoryEntry{Tags: tt.memoryTags}
			relevance := ComputeRelevance(m, tt.contextTags)
			if relevance < tt.minExpected || relevance > tt.maxExpected {
				t.Errorf("relevance = %.4f, expected [%.2f, %.2f]", relevance, tt.minExpected, tt.maxExpected)
			}
		})
	}
}

func TestScoreMemory_FullWorkflow(t *testing.T) {
	// Reproduce the Scoring.md worked example
	now := time.Now()
	cfg := DefaultConfig()

	m := &MemoryEntry{
		Title:              "User prefers Go for offensive tooling",
		Type:               TypeUser,
		Created:            now.Add(-504 * time.Hour), // ~21 days
		LastAccessed:       now.Add(-96 * time.Hour),  // 4 days
		AccessCount:        7,
		DecayRate:          0.3,
		Confidence:         0.95,
		SurpriseAtEncoding: 0.2,
		Tags:               []string{"go", "tooling", "preference"},
	}

	contextTags := []string{"go", "development", "tooling"}
	scored := ScoreMemory(m, contextTags, "", cfg, now)

	// activation ≈ 0.549, relevance ≈ 0.4, confidence = 0.95, surprise = 0.1
	// total ≈ 0.549 × 0.4 × 0.95 + 0.1 ≈ 0.309
	if scored.Total < 0.2 || scored.Total > 0.5 {
		t.Errorf("total score = %.4f, expected around 0.3 (per Scoring.md)", scored.Total)
	}

	if scored.Total < cfg.RetrievalThreshold {
		t.Logf("note: score %.4f is below threshold %.1f — borderline as expected", scored.Total, cfg.RetrievalThreshold)
	}
}

func TestScoreAllMemories_Ordering(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	memories := []*MemoryEntry{
		{
			Title: "Low score", Type: TypeProject,
			LastAccessed: now.Add(-720 * time.Hour), AccessCount: 1,
			DecayRate: 0.7, Confidence: 0.5, SurpriseAtEncoding: 0.1,
			Tags: []string{"unrelated"},
		},
		{
			Title: "High score", Type: TypeUser,
			LastAccessed: now.Add(-2 * time.Hour), AccessCount: 15,
			DecayRate: 0.2, Confidence: 0.95, SurpriseAtEncoding: 0.5,
			Tags: []string{"go", "security"},
		},
		{
			Title: "Medium score", Type: TypeFeedback,
			LastAccessed: now.Add(-48 * time.Hour), AccessCount: 5,
			DecayRate: 0.3, Confidence: 0.8, SurpriseAtEncoding: 0.3,
			Tags: []string{"go"},
		},
	}

	scored := ScoreAllMemories(memories, []string{"go", "security"}, "", cfg, now)

	if scored[0].Memory.Title != "High score" {
		t.Errorf("expected 'High score' first, got '%s'", scored[0].Memory.Title)
	}
	if scored[len(scored)-1].Memory.Title != "Low score" {
		t.Errorf("expected 'Low score' last, got '%s'", scored[len(scored)-1].Memory.Title)
	}

	// Verify descending order
	for i := 1; i < len(scored); i++ {
		if scored[i].Total > scored[i-1].Total {
			t.Errorf("scores not descending at index %d: %.4f > %.4f", i, scored[i].Total, scored[i-1].Total)
		}
	}
}

func TestKnowledgeScoring_NoActivationDecay(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	// Knowledge entry last accessed a long time ago
	knowledge := &MemoryEntry{
		Type:               TypeKnowledge,
		Title:              "NTDLL Syscall Stubs",
		LastAccessed:       now.Add(-720 * time.Hour), // 30 days ago
		AccessCount:        2,
		DecayRate:          0.0,
		Confidence:         0.90,
		SurpriseAtEncoding: 0.3,
		Tags:               []string{"windows", "ntdll", "syscalls"},
	}

	// Equivalent standard memory — same age and stats
	standard := &MemoryEntry{
		Type:               TypeSemantic,
		Title:              "Some old semantic memory",
		LastAccessed:       now.Add(-720 * time.Hour),
		AccessCount:        2,
		DecayRate:          0.2,
		Confidence:         0.90,
		SurpriseAtEncoding: 0.3,
		Tags:               []string{"windows", "ntdll", "syscalls"},
	}

	tags := []string{"windows", "ntdll"}

	kScore := ScoreMemory(knowledge, tags, "", cfg, now)
	sScore := ScoreMemory(standard, tags, "", cfg, now)

	// Knowledge should have activation=1.0 (fixed), standard will have decayed activation
	if kScore.Activation != 1.0 {
		t.Errorf("knowledge activation should be 1.0, got %.3f", kScore.Activation)
	}
	if sScore.Activation >= 1.0 {
		t.Errorf("standard activation should have decayed below 1.0, got %.3f", sScore.Activation)
	}

	// With same tags matching, knowledge should score higher due to stable activation
	if kScore.Total <= sScore.Total {
		t.Errorf("knowledge (%.3f) should outscore equivalent decayed standard memory (%.3f)",
			kScore.Total, sScore.Total)
	}
}

func TestKnowledgeScoring_RelevanceDriven(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	knowledge := &MemoryEntry{
		Type:               TypeKnowledge,
		Title:              "PE Header Structure",
		LastAccessed:       now.Add(-720 * time.Hour),
		AccessCount:        1,
		DecayRate:          0.0,
		Confidence:         0.90,
		SurpriseAtEncoding: 0.2,
		Tags:               []string{"windows", "pe", "binary"},
	}

	// With matching tags — should score well
	matchScore := ScoreMemory(knowledge, []string{"pe", "binary"}, "", cfg, now)
	// With no matching tags — should score low (relevance is 0.5 neutral)
	noMatchScore := ScoreMemory(knowledge, []string{"linux", "elf"}, "", cfg, now)

	if matchScore.Total <= noMatchScore.Total {
		t.Errorf("matched knowledge (%.3f) should outscore unmatched (%.3f)",
			matchScore.Total, noMatchScore.Total)
	}
}

func TestSurpriseBonus(t *testing.T) {
	cfg := DefaultConfig()

	high := &MemoryEntry{SurpriseAtEncoding: 0.9}
	low := &MemoryEntry{SurpriseAtEncoding: 0.1}

	highBonus := ComputeSurpriseBonus(high, cfg)
	lowBonus := ComputeSurpriseBonus(low, cfg)

	if highBonus <= lowBonus {
		t.Errorf("high surprise should give bigger bonus: high=%.4f, low=%.4f", highBonus, lowBonus)
	}

	expectedHigh := 0.9 * cfg.SurpriseBonusWeight
	if math.Abs(highBonus-expectedHigh) > 0.001 {
		t.Errorf("surprise bonus = %.4f, expected %.4f", highBonus, expectedHigh)
	}
}
