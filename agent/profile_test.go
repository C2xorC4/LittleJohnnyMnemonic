package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestProfileEntry_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cognition_profile.md")

	original := &MemoryEntry{
		Type:               TypeUser,
		Title:              "Theory-first systems thinker",
		Created:            time.Now(),
		LastAccessed:       time.Now(),
		AccessCount:        1,
		DecayRate:          0.10,
		Confidence:         0.75,
		SurpriseAtEncoding: 0.3,
		Tags:               []string{"cognition", "methodology"},
		Profile:            true,
		Facet:              "cognition",
		ObservationCount:   3,
		Evidence: []string{
			"2026-04-06_memory-system-interest.md",
			"2026-04-02_blsim-architecture-first.md",
			"2026-03-28_eod-modular-design.md",
		},
		Body:     "User consistently grounds novel problems in established theoretical frameworks.",
		FilePath: path,
	}

	if err := WriteMemoryEntry(original); err != nil {
		t.Fatalf("WriteMemoryEntry: %v", err)
	}

	parsed, err := ParseMemoryEntry(path)
	if err != nil {
		t.Fatalf("ParseMemoryEntry: %v", err)
	}

	if !parsed.Profile {
		t.Error("profile should be true")
	}
	if parsed.Facet != "cognition" {
		t.Errorf("facet = %s, expected cognition", parsed.Facet)
	}
	if len(parsed.Evidence) != 3 {
		t.Errorf("evidence count = %d, expected 3", len(parsed.Evidence))
	}
	if parsed.DecayRate != 0.10 {
		t.Errorf("decay_rate = %.2f, expected 0.10", parsed.DecayRate)
	}
}

func TestProfileConfidenceFloor(t *testing.T) {
	cfg := DefaultConfig()

	m := &MemoryEntry{
		Type:       TypeUser,
		Profile:    true,
		Confidence: 0.3, // below floor
	}

	if m.Confidence < cfg.ProfileConfidenceFloor {
		m.Confidence = cfg.ProfileConfidenceFloor
	}

	if m.Confidence != 0.5 {
		t.Errorf("profile confidence should be floored at %.1f, got %.2f",
			cfg.ProfileConfidenceFloor, m.Confidence)
	}
}

func TestProfileImmuneToArchival(t *testing.T) {
	cfg := DefaultConfig()

	if !cfg.ProfileImmuneToArchival {
		t.Error("profile_immune_to_archival should default to true")
	}
}

func TestProfileDecayRates_SlowerThanObservations(t *testing.T) {
	cfg := DefaultConfig()

	facets := []string{"identity", "cognition", "communication", "expertise",
		"motivation", "patterns"}

	for _, f := range facets {
		obsRate, obsOK := cfg.UserFacetDecayRates[f]
		profRate, profOK := cfg.ProfileDecayRates[f]

		if !obsOK || !profOK {
			continue
		}

		if profRate >= obsRate {
			t.Errorf("facet %s: profile decay (%.2f) should be slower than observation decay (%.2f)",
				f, profRate, obsRate)
		}
	}
}

func TestProfileCreationThreshold(t *testing.T) {
	cfg := DefaultConfig()

	if cfg.ProfileCreationThreshold != 3 {
		t.Errorf("profile_creation_threshold = %d, expected 3", cfg.ProfileCreationThreshold)
	}
}

func TestProfilePersonalityIsStickiest(t *testing.T) {
	cfg := DefaultConfig()

	personalityRate := cfg.ProfileDecayRates["personality"]

	for facet, rate := range cfg.ProfileDecayRates {
		if rate < personalityRate && facet != "personality" {
			t.Errorf("personality (%.2f) should have lowest decay, but %s has %.2f",
				personalityRate, facet, rate)
		}
	}
}

func TestIntegration_ProfileSynthesisDetection(t *testing.T) {
	root := t.TempDir()

	// Create directory structure
	dirs := []string{"System", "Buffer", "Memory/User", "Memory/Feedback",
		"Memory/Project", "Memory/Reference", "Memory/Semantic", "Archive", "Metrics"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(root, d), 0755)
	}
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Test"), 0644)

	now := time.Now()

	// Create 3 cognition observations (enough for profile synthesis)
	observations := []MemoryEntry{
		{
			Type: TypeUser, Title: "Theory-first on memory system",
			Facet: "cognition", ObservationCount: 1,
			Created: now.Add(-72 * time.Hour), LastAccessed: now.Add(-24 * time.Hour),
			AccessCount: 2, DecayRate: 0.2, Confidence: 0.6, SurpriseAtEncoding: 0.4,
			Tags: []string{"cognition"}, Body: "Approached memory system by researching ACT-R first.",
			FilePath: filepath.Join(root, "Memory", "User", "cog_obs1.md"),
		},
		{
			Type: TypeUser, Title: "Architecture-first on blsim",
			Facet: "cognition", ObservationCount: 1,
			Created: now.Add(-96 * time.Hour), LastAccessed: now.Add(-48 * time.Hour),
			AccessCount: 2, DecayRate: 0.2, Confidence: 0.6, SurpriseAtEncoding: 0.3,
			Tags: []string{"cognition"}, Body: "Designed LB3 feature set before writing code.",
			FilePath: filepath.Join(root, "Memory", "User", "cog_obs2.md"),
		},
		{
			Type: TypeUser, Title: "Modular design for EOD toolkit",
			Facet: "cognition", ObservationCount: 1,
			Created: now.Add(-168 * time.Hour), LastAccessed: now.Add(-72 * time.Hour),
			AccessCount: 1, DecayRate: 0.2, Confidence: 0.6, SurpriseAtEncoding: 0.3,
			Tags: []string{"cognition"}, Body: "Built EOD with deliberate modular architecture.",
			FilePath: filepath.Join(root, "Memory", "User", "cog_obs3.md"),
		},
	}

	for i := range observations {
		WriteMemoryEntry(&observations[i])
	}

	// Load memories and check facet counts
	memories, _ := LoadAllMemories(root)
	cfg := DefaultConfig()

	facetObs := make(map[string]int)
	for _, m := range memories {
		if m.Type == TypeUser && m.Facet != "" && !m.Profile {
			facetObs[m.Facet] += m.ObservationCount
			if m.ObservationCount == 0 {
				facetObs[m.Facet]++
			}
		}
	}

	cogObs := facetObs["cognition"]
	if cogObs < cfg.ProfileCreationThreshold {
		t.Errorf("expected %d+ cognition observations, got %d", cfg.ProfileCreationThreshold, cogObs)
	}
}
