package main

import (
	"math"
	"testing"
	"time"
)

// Helper: build a Config with adaptive weighting enabled for `learned`
// edges, baseline alpha and cap matching DefaultConfig.
func adaptiveConfig() Config {
	cfg := DefaultConfig()
	cfg.AdaptiveEdgeWeightingEnabled = true
	cfg.AdaptiveEdgeScope = []string{"learned"}
	cfg.AdaptiveEdgeAlpha = 0.2
	cfg.AdaptiveEdgeCap = 2.0
	return cfg
}

func TestEffectiveEdgeWeight_BaseRelationshipDefault(t *testing.T) {
	cfg := DefaultConfig()
	link := Link{Target: "memory/x/b", Relationship: "related-to"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, nil)
	if w != 0.5 {
		t.Errorf("base related-to weight = %f, expected 0.5", w)
	}
}

func TestEffectiveEdgeWeight_UnmappedRelationshipFallback(t *testing.T) {
	cfg := DefaultConfig()
	link := Link{Target: "memory/x/b", Relationship: "made-up-type"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, nil)
	if w != 0.5 {
		t.Errorf("unmapped relationship fallback = %f, expected 0.5", w)
	}
}

func TestEffectiveEdgeWeight_AuthoredOverride(t *testing.T) {
	cfg := DefaultConfig()
	override := 0.85
	link := Link{Target: "memory/x/b", Relationship: "related-to", Weight: &override}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, nil)
	if w != 0.85 {
		t.Errorf("authored override = %f, expected 0.85", w)
	}
}

func TestEffectiveEdgeWeight_AdaptiveDisabled_NoEffect(t *testing.T) {
	cfg := DefaultConfig() // master toggle off
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 100, LastUsed: time.Now(),
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	if w != 0.4 {
		t.Errorf("adaptive disabled: weight = %f, expected base learned = 0.4 (ignoring usage)", w)
	}
}

func TestEffectiveEdgeWeight_OutOfScope_NoEffect(t *testing.T) {
	cfg := adaptiveConfig() // scope = ["learned"]
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "related-to"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "related-to",
			UsageCount: 100, LastUsed: time.Now(),
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "related-to"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	if w != 0.5 {
		t.Errorf("out-of-scope (related-to) with usage=100: weight = %f, expected base 0.5", w)
	}
}

func TestEffectiveEdgeWeight_InScope_WithUsage(t *testing.T) {
	cfg := adaptiveConfig()
	// usage_count = 5 → mult = 1 + 0.2*ln(6) ≈ 1.358; learned base = 0.4
	// expected ≈ 0.4 * 1.358 = 0.543
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 5, LastUsed: time.Now(),
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	expected := 0.4 * (1.0 + 0.2*math.Log(6.0))
	if math.Abs(w-expected) > 1e-9 {
		t.Errorf("adaptive learned with usage=5: weight = %f, expected %f", w, expected)
	}
}

func TestEffectiveEdgeWeight_CapRespected(t *testing.T) {
	cfg := adaptiveConfig() // cap = 2.0
	// Very high usage → uncapped mult would exceed 2.0; capped multiplier = 2.0
	// → effective = base * 2.0 = 0.4 * 2.0 = 0.8
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 100000, LastUsed: time.Now(),
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	if w != 0.8 {
		t.Errorf("capped multiplier: weight = %f, expected 0.8 (base 0.4 × cap 2.0)", w)
	}
}

func TestEffectiveEdgeWeight_NoUsageEntry_BaseOnly(t *testing.T) {
	cfg := adaptiveConfig()
	// Empty usage map → no multiplier
	usage := map[string]EdgeUsage{}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	if w != 0.4 {
		t.Errorf("adaptive enabled but no usage data: weight = %f, expected base 0.4", w)
	}
}

func TestEffectiveEdgeWeight_AuthoredOverrideMultipliedByAdaptive(t *testing.T) {
	cfg := adaptiveConfig()
	override := 0.6 // bumps learned from 0.4 base to 0.6
	link := Link{Target: "memory/x/b", Relationship: "learned", Weight: &override}
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 3, LastUsed: time.Now(),
		},
	}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	expected := 0.6 * (1.0 + 0.2*math.Log(4.0))
	if math.Abs(w-expected) > 1e-9 {
		t.Errorf("override + adaptive: weight = %f, expected %f", w, expected)
	}
}

func TestBuildGraphWithUsage_TogglesOffPreservesBaseWeight(t *testing.T) {
	cfg := DefaultConfig() // toggle off
	mems := []*MemoryEntry{
		{
			Type:     TypeProject,
			Title:    "A",
			FilePath: "/vault/Memory/Project/a.md",
			Links:    []Link{{Target: "Memory/Project/b", Relationship: "learned"}},
		},
		{
			Type:     TypeProject,
			Title:    "B",
			FilePath: "/vault/Memory/Project/b.md",
		},
	}
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/project/a", "memory/project/b", "learned"): {
			Source: "memory/project/a", Target: "memory/project/b", Relationship: "learned",
			UsageCount: 50, LastUsed: time.Now(),
		},
	}
	g := BuildGraphWithUsage(mems, cfg, usage)
	edges := g.Edges["memory/project/a"]
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge from a, got %d", len(edges))
	}
	if edges[0].Weight != 0.4 {
		t.Errorf("toggle off: edge weight = %f, expected base 0.4", edges[0].Weight)
	}
}

func TestBuildGraphWithUsage_AdaptiveLayerApplied(t *testing.T) {
	cfg := adaptiveConfig()
	mems := []*MemoryEntry{
		{
			Type:     TypeProject,
			Title:    "A",
			FilePath: "/vault/Memory/Project/a.md",
			Links:    []Link{{Target: "Memory/Project/b", Relationship: "learned"}},
		},
		{
			Type:     TypeProject,
			Title:    "B",
			FilePath: "/vault/Memory/Project/b.md",
		},
	}
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/project/a", "memory/project/b", "learned"): {
			Source: "memory/project/a", Target: "memory/project/b", Relationship: "learned",
			UsageCount: 5, LastUsed: time.Now(),
		},
	}
	g := BuildGraphWithUsage(mems, cfg, usage)
	edges := g.Edges["memory/project/a"]
	if len(edges) != 1 {
		t.Fatalf("expected 1 edge from a, got %d", len(edges))
	}
	expected := 0.4 * (1.0 + 0.2*math.Log(6.0))
	if math.Abs(edges[0].Weight-expected) > 1e-9 {
		t.Errorf("adaptive edge weight = %f, expected %f", edges[0].Weight, expected)
	}
}

// --- Temporal decay tests ---

func TestEffectiveEdgeWeight_Decay_LambdaZero_NoDecay(t *testing.T) {
	cfg := adaptiveConfig()
	cfg.AdaptiveEdgeDecayLambda = 0 // explicitly disabled
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 5, LastUsed: time.Now().Add(-365 * 24 * time.Hour), // 1 year ago — no effect when λ=0
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	expected := 0.4 * (1.0 + 0.2*math.Log(6.0)) // same as pre-decay formula
	if math.Abs(w-expected) > 1e-9 {
		t.Errorf("lambda=0 should not decay: weight = %f, expected %f", w, expected)
	}
}

func TestEffectiveEdgeWeight_Decay_FreshEdge_FullUplift(t *testing.T) {
	cfg := adaptiveConfig() // decay lambda = 0.003851 (default)
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 5, LastUsed: time.Now(), // just now — decay ≈ 1.0
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	expected := 0.4 * (1.0 + 0.2*math.Log(6.0))
	if math.Abs(w-expected) > 1e-4 { // small tolerance for sub-millisecond timing
		t.Errorf("fresh edge: weight = %f, expected ~%f", w, expected)
	}
}

func TestEffectiveEdgeWeight_Decay_HalfLife180Days(t *testing.T) {
	cfg := adaptiveConfig()
	cfg.AdaptiveEdgeDecayLambda = 0.003851 // 180-day half-life
	lastUsed := time.Now().Add(-180 * 24 * time.Hour)
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 5, LastUsed: lastUsed,
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	days := time.Since(lastUsed).Hours() / 24
	decay := math.Exp(-0.003851 * days)
	expected := 0.4 * (1.0 + 0.2*math.Log(6.0)*decay)
	if math.Abs(w-expected) > 1e-6 {
		t.Errorf("180-day decay: weight = %f, expected %f (decay=%.4f)", w, expected, decay)
	}
	// Decay at ~180 days with λ=ln(2)/180 should be close to 0.5.
	if decay < 0.49 || decay > 0.51 {
		t.Errorf("decay factor at ~180 days = %f, expected ~0.5", decay)
	}
}

func TestEffectiveEdgeWeight_Decay_BasePreserved_VeryOldEdge(t *testing.T) {
	cfg := adaptiveConfig()
	cfg.AdaptiveEdgeDecayLambda = 0.003851
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 1000, LastUsed: time.Now().Add(-10 * 365 * 24 * time.Hour), // 10 years
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	base := 0.4
	if w < base {
		t.Errorf("10-year-old edge: weight = %f dropped below base %f", w, base)
	}
	// With 10 years of decay the uplift is essentially gone — weight should be very close to base.
	if w-base > 1e-4 {
		t.Errorf("10-year-old edge: weight = %f, expected near base %f (residual uplift too high)", w, base)
	}
}

func TestEffectiveEdgeWeight_Decay_ZeroLastUsed_NoDecay(t *testing.T) {
	cfg := adaptiveConfig()
	cfg.AdaptiveEdgeDecayLambda = 0.003851
	usage := map[string]EdgeUsage{
		edgeUsageKey("memory/x/a", "memory/x/b", "learned"): {
			Source: "memory/x/a", Target: "memory/x/b", Relationship: "learned",
			UsageCount: 5, // LastUsed is zero value — guard should skip decay
		},
	}
	link := Link{Target: "memory/x/b", Relationship: "learned"}
	w := effectiveEdgeWeight("memory/x/a", "memory/x/b", link, cfg, usage)
	expected := 0.4 * (1.0 + 0.2*math.Log(6.0))
	if math.Abs(w-expected) > 1e-9 {
		t.Errorf("zero LastUsed: decay should not apply; weight = %f, expected %f", w, expected)
	}
}
