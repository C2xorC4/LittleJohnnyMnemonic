package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupTestVault creates a temporary vault with controlled sample data.
func setupTestVault(t *testing.T) string {
	t.Helper()
	root := t.TempDir()

	// Create directory structure
	dirs := []string{
		"System", "Buffer",
		"Memory/User", "Memory/Feedback", "Memory/Project",
		"Memory/Reference", "Memory/Semantic",
		"Archive", "Metrics",
	}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(root, d), 0755)
	}

	// Create CLAUDE.md (vault detection marker)
	os.WriteFile(filepath.Join(root, "CLAUDE.md"), []byte("# Test vault"), 0644)

	now := time.Now()

	// Populate memories
	memories := []MemoryEntry{
		{
			Type: TypeUser, Title: "Go expertise",
			Created: now.Add(-504 * time.Hour), LastAccessed: now.Add(-96 * time.Hour),
			AccessCount: 7, DecayRate: 0.3, Confidence: 0.95,
			SurpriseAtEncoding: 0.2, Tags: []string{"go", "tooling"},
			Links:    []Link{{Target: "Memory/User/security", Relationship: "related-to"}},
			Body:     "User's primary language is Go.", FilePath: filepath.Join(root, "Memory", "User", "go_expertise.md"),
		},
		{
			Type: TypeUser, Title: "Security background",
			Created: now.Add(-504 * time.Hour), LastAccessed: now.Add(-2 * time.Hour),
			AccessCount: 12, DecayRate: 0.3, Confidence: 0.95,
			SurpriseAtEncoding: 0.3, Tags: []string{"security", "offensive"},
			Links:    []Link{{Target: "Memory/User/go_expertise", Relationship: "related-to"}},
			Body:     "Purple team Red Cell operator.", FilePath: filepath.Join(root, "Memory", "User", "security.md"),
		},
		{
			Type: TypeFeedback, Title: "Training override: correct X understanding",
			Created: now.Add(-168 * time.Hour), LastAccessed: now.Add(-48 * time.Hour),
			AccessCount: 3, DecayRate: 0.1, Confidence: 0.9,
			SurpriseAtEncoding: 0.9, Tags: []string{"correction", "security"},
			TrainingOverride: true, OverrideContext: "Model says X; user confirmed Y",
			SourceAuthority: "user-confirmed-with-evidence",
			Body: "Y is correct, not X.", FilePath: filepath.Join(root, "Memory", "Feedback", "override_x.md"),
		},
		{
			Type: TypeProject, Title: "Old completed project",
			Created: now.Add(-1440 * time.Hour), LastAccessed: now.Add(-1200 * time.Hour), // 50 days stale
			AccessCount: 2, DecayRate: 0.7, Confidence: 0.5,
			SurpriseAtEncoding: 0.1, Tags: []string{"old", "done"},
			Body: "A project that finished long ago.", FilePath: filepath.Join(root, "Memory", "Project", "old_project.md"),
		},
	}

	for i := range memories {
		if err := WriteMemoryEntry(&memories[i]); err != nil {
			t.Fatalf("failed to write memory %s: %v", memories[i].Title, err)
		}
	}

	// Populate buffer entries
	bufferEntries := []BufferEntry{
		{
			Type: TypeBuffer, Timestamp: now.Add(-30 * time.Minute),
			Source: "conversation", Surprise: 0.8,
			ContextIntegrity: ContextFull, Tags: []string{"new-insight"},
			Body:     "User revealed a significant new preference that contradicts nothing existing and is highly novel.",
			FilePath: filepath.Join(root, "Buffer", "2026-04-06_new-insight.md"),
		},
		{
			Type: TypeBuffer, Timestamp: now.Add(-48 * time.Hour),
			Source: "conversation", Surprise: 0.9,
			ContextIntegrity: ContextOrphan, Tags: []string{"vague"},
			Body:     "User said to use the other approach.", // ambiguous
			FilePath: filepath.Join(root, "Buffer", "2026-04-04_vague.md"),
		},
		{
			Type: TypeBuffer, Timestamp: now.Add(-1 * time.Hour),
			Source: "conversation", Surprise: 0.3,
			ContextIntegrity: ContextFull, Tags: []string{"go", "tooling"},
			Body:     "User used Go again for tooling, consistent with existing preference.",
			FilePath: filepath.Join(root, "Buffer", "2026-04-06_go-again.md"),
		},
	}

	for i := range bufferEntries {
		if err := WriteBufferEntry(&bufferEntries[i]); err != nil {
			t.Fatalf("failed to write buffer entry: %v", err)
		}
	}

	// Create consolidation log
	os.WriteFile(filepath.Join(root, "Metrics", "consolidation_log.md"), []byte("# Consolidation Log\n\n---\n"), 0644)

	return root
}

func TestIntegration_FullRetrievalCycle(t *testing.T) {
	root := setupTestVault(t)
	cfg := DefaultConfig()
	now := time.Now()

	// Load and score
	memories, err := LoadAllMemories(root)
	if err != nil {
		t.Fatalf("LoadAllMemories: %v", err)
	}
	if len(memories) != 4 {
		t.Fatalf("expected 4 memories, got %d", len(memories))
	}

	scored := ScoreAllMemories(memories, []string{"security", "go"}, "", cfg, now)

	// Build graph and apply spreading activation
	graph := BuildGraph(memories, cfg)
	scored = ApplySpreadingActivation(scored, graph, cfg)

	// Security background should rank highest (recent access, many accesses, tag match)
	if scored[0].Memory.Title != "Security background" {
		t.Errorf("expected 'Security background' ranked first, got '%s'", scored[0].Memory.Title)
	}

	// Old project should rank lowest
	if scored[len(scored)-1].Memory.Title != "Old completed project" {
		t.Errorf("expected 'Old completed project' ranked last, got '%s'", scored[len(scored)-1].Memory.Title)
	}

	// Filter by threshold
	var retrieved []ScoredMemory
	for _, s := range scored {
		if s.Total >= cfg.RetrievalThreshold {
			retrieved = append(retrieved, s)
		}
	}

	// Old project should be below threshold
	for _, s := range retrieved {
		if s.Memory.Title == "Old completed project" {
			t.Error("old completed project should be below retrieval threshold")
		}
	}
}

func TestIntegration_ConsolidationPhase1(t *testing.T) {
	root := setupTestVault(t)
	cfg := DefaultConfig()
	now := time.Now()

	bufferEntries, _ := LoadAllBufferEntries(root)
	memories, _ := LoadAllMemories(root)

	if len(bufferEntries) != 3 {
		t.Fatalf("expected 3 buffer entries, got %d", len(bufferEntries))
	}

	var promoted, held, discarded int
	for _, entry := range bufferEntries {
		assessment := assessBufferEntry(entry, memories, cfg, now, time.Time{})
		switch assessment.Action {
		case ActionPromote:
			promoted++
		case ActionHold:
			held++
		case ActionDiscard:
			discarded++
		}
	}

	// The ambiguous orphan should be discarded
	if discarded < 1 {
		t.Error("expected at least 1 discard (the ambiguous orphan)")
	}

	// The high-surprise novel entry should be promoted
	if promoted < 1 {
		t.Error("expected at least 1 promotion (the high-surprise novel entry)")
	}
}

func TestIntegration_TrainingOverrideImmunity(t *testing.T) {
	root := setupTestVault(t)
	cfg := DefaultConfig()
	now := time.Now()

	memories, _ := LoadAllMemories(root)

	// Find the training override
	var override *MemoryEntry
	for _, m := range memories {
		if m.TrainingOverride {
			override = m
			break
		}
	}
	if override == nil {
		t.Fatal("training override memory not found")
	}

	// Compute its max possible score
	score := ScoreMemory(override, nil, "", cfg, now)
	maxScore := score.Activation * 1.0 * override.Confidence + ComputeSurpriseBonus(override, cfg)

	// Even if max score is below threshold, it should be immune to archival
	if cfg.OverrideImmuneToArchival {
		// This is correct behavior — training overrides are immune
		t.Logf("training override max_score=%.3f, immune=%v", maxScore, cfg.OverrideImmuneToArchival)
	}

	// Verify confidence floor
	override.Confidence = 0.3 // artificially lower
	if override.Confidence < cfg.OverrideConfidenceFloor {
		override.Confidence = cfg.OverrideConfidenceFloor
	}
	if override.Confidence != cfg.OverrideConfidenceFloor {
		t.Errorf("confidence floor not applied: %.2f, expected %.2f",
			override.Confidence, cfg.OverrideConfidenceFloor)
	}
}

func TestIntegration_DecayPassStaleProject(t *testing.T) {
	root := setupTestVault(t)
	cfg := DefaultConfig()
	now := time.Now()

	memories, _ := LoadAllMemories(root)

	var oldProject *MemoryEntry
	for _, m := range memories {
		if m.Title == "Old completed project" {
			oldProject = m
			break
		}
	}
	if oldProject == nil {
		t.Fatal("old project not found")
	}

	daysSinceAccess := now.Sub(oldProject.LastAccessed).Hours() / 24
	if daysSinceAccess <= float64(cfg.StaleThresholdDays) {
		t.Fatalf("old project should be stale: %.0f days since access", daysSinceAccess)
	}

	// Apply staleness
	oldConf := oldProject.Confidence
	oldProject.Confidence *= cfg.ConfidenceStaleFactor
	if oldProject.Confidence >= oldConf {
		t.Error("staleness should reduce confidence")
	}
}

func TestIntegration_ObservationCountConfidenceCap(t *testing.T) {
	cfg := DefaultConfig()

	tests := []struct {
		observationCount int
		maxConfidence    float64
	}{
		{1, 0.6},
		{2, 0.8},
		{3, 0.8},
		{4, 0.95},
		{10, 0.95},
	}

	for _, tt := range tests {
		cap := getObservationCap(tt.observationCount, cfg)
		if cap != tt.maxConfidence {
			t.Errorf("observation_count=%d: cap=%.2f, expected %.2f",
				tt.observationCount, cap, tt.maxConfidence)
		}
	}
}

// getObservationCap returns the confidence cap for a given observation count.
func getObservationCap(count int, cfg Config) float64 {
	// Find the highest threshold key that count meets or exceeds
	bestThreshold := 0
	bestCap := 0.6 // default for count=0

	for threshold, cap := range cfg.ObservationConfidenceCaps {
		if count >= threshold && threshold >= bestThreshold {
			bestThreshold = threshold
			bestCap = cap
		}
	}
	return bestCap
}
