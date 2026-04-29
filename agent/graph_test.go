package main

import (
	"testing"
	"time"
)

func makeTestMemories() []*MemoryEntry {
	now := time.Now()
	return []*MemoryEntry{
		{
			Title: "Go expertise", Type: TypeUser, FilePath: "Memory/User/go_expertise.md",
			LastAccessed: now.Add(-24 * time.Hour), AccessCount: 7, DecayRate: 0.3,
			Confidence: 0.95, SurpriseAtEncoding: 0.2, Tags: []string{"go", "tooling"},
			Links: []Link{
				{Target: "Memory/User/security_expertise", Relationship: "related-to"},
			},
		},
		{
			Title: "Security expertise", Type: TypeUser, FilePath: "Memory/User/security_expertise.md",
			LastAccessed: now.Add(-2 * time.Hour), AccessCount: 12, DecayRate: 0.3,
			Confidence: 0.95, SurpriseAtEncoding: 0.3, Tags: []string{"security", "offensive"},
			Links: []Link{
				{Target: "Memory/User/go_expertise", Relationship: "related-to"},
				{Target: "Memory/Feedback/clarification", Relationship: "depends-on"},
			},
		},
		{
			Title: "Clarification over refusal", Type: TypeFeedback, FilePath: "Memory/Feedback/clarification.md",
			LastAccessed: now.Add(-48 * time.Hour), AccessCount: 4, DecayRate: 0.3,
			Confidence: 0.9, SurpriseAtEncoding: 0.6, Tags: []string{"communication", "security"},
			Links: []Link{
				{Target: "Memory/User/security_expertise", Relationship: "depends-on"},
			},
		},
	}
}

func TestBuildGraph_Edges(t *testing.T) {
	memories := makeTestMemories()
	cfg := DefaultConfig()
	graph := BuildGraph(memories, cfg)

	// Check that go_expertise has edges
	goKey := normalizeKey(memories[0])
	edges := graph.Edges[goKey]
	if len(edges) == 0 {
		t.Fatalf("go_expertise should have edges, got 0")
	}

	// Should have at least the declared link + reverse from security_expertise
	foundRelatedTo := false
	for _, e := range edges {
		if e.Relationship == "related-to" {
			foundRelatedTo = true
		}
	}
	if !foundRelatedTo {
		t.Error("expected related-to edge from go_expertise")
	}
}

func TestBuildGraph_Bidirectional(t *testing.T) {
	memories := makeTestMemories()
	cfg := DefaultConfig()
	graph := BuildGraph(memories, cfg)

	goKey := normalizeKey(memories[0])
	secKey := normalizeKey(memories[1])

	// Check go → security exists
	goEdges := graph.Edges[goKey]
	found := false
	for _, e := range goEdges {
		if e.Target == secKey {
			found = true
		}
	}
	if !found {
		t.Errorf("expected edge from go_expertise to security_expertise")
	}

	// Check security → go exists (bidirectional for related-to)
	secEdges := graph.Edges[secKey]
	found = false
	for _, e := range secEdges {
		if e.Target == goKey {
			found = true
		}
	}
	if !found {
		t.Errorf("expected reverse edge from security_expertise to go_expertise")
	}
}

func TestBuildGraph_SupersedesNotBidirectional(t *testing.T) {
	memories := []*MemoryEntry{
		{
			Title: "New plan", Type: TypeProject, FilePath: "Memory/Project/new.md",
			Links: []Link{{Target: "Memory/Project/old", Relationship: "supersedes"}},
		},
		{
			Title: "Old plan", Type: TypeProject, FilePath: "Memory/Project/old.md",
		},
	}

	cfg := DefaultConfig()
	graph := BuildGraph(memories, cfg)

	newKey := normalizeKey(memories[0])
	oldKey := normalizeKey(memories[1])

	// new → old should exist
	found := false
	for _, e := range graph.Edges[newKey] {
		if e.Target == oldKey {
			found = true
		}
	}
	if !found {
		t.Error("expected supersedes edge from new to old")
	}

	// old → new should NOT exist (supersedes is unidirectional)
	for _, e := range graph.Edges[oldKey] {
		if e.Target == newKey {
			t.Error("supersedes should not create reverse edge")
		}
	}
}

func TestSpreadingActivation_BoostsNeighbors(t *testing.T) {
	memories := makeTestMemories()
	cfg := DefaultConfig()
	now := time.Now()

	// Score with tags that match security but not go
	scored := ScoreAllMemories(memories, []string{"security", "offensive"}, "", cfg, now)
	graph := BuildGraph(memories, cfg)

	// Get go_expertise score before boost
	var goScoreBefore float64
	for _, s := range scored {
		if s.Memory.Title == "Go expertise" {
			goScoreBefore = s.Total
		}
	}

	// Apply spreading activation
	boosted := ApplySpreadingActivation(scored, graph, cfg)

	var goScoreAfter float64
	var goBoost float64
	for _, s := range boosted {
		if s.Memory.Title == "Go expertise" {
			goScoreAfter = s.Total
			goBoost = s.Boost
		}
	}

	// go_expertise should get a boost from security_expertise (which matches the tags)
	if goBoost <= 0 {
		t.Errorf("go_expertise should receive spreading activation boost, got %.4f", goBoost)
	}
	if goScoreAfter <= goScoreBefore {
		t.Errorf("go_expertise total should increase after boost: before=%.4f, after=%.4f", goScoreBefore, goScoreAfter)
	}
}

func TestSpreadingActivation_OneHopOnly(t *testing.T) {
	// Create a chain: A → B → C
	// If A is activated, B should get boost but C should NOT (one hop only)
	memories := []*MemoryEntry{
		{
			Title: "A", Type: TypeUser, FilePath: "Memory/User/a.md",
			LastAccessed: time.Now().Add(-1 * time.Hour), AccessCount: 10,
			DecayRate: 0.3, Confidence: 0.9, SurpriseAtEncoding: 0.5,
			Tags: []string{"target"},
			Links: []Link{{Target: "Memory/User/b", Relationship: "related-to"}},
		},
		{
			Title: "B", Type: TypeUser, FilePath: "Memory/User/b.md",
			LastAccessed: time.Now().Add(-100 * time.Hour), AccessCount: 1,
			DecayRate: 0.5, Confidence: 0.5, SurpriseAtEncoding: 0.1,
			Tags: []string{"other"},
			Links: []Link{{Target: "Memory/User/c", Relationship: "related-to"}},
		},
		{
			Title: "C", Type: TypeUser, FilePath: "Memory/User/c.md",
			LastAccessed: time.Now().Add(-200 * time.Hour), AccessCount: 1,
			DecayRate: 0.5, Confidence: 0.5, SurpriseAtEncoding: 0.1,
			Tags: []string{"distant"},
		},
	}

	cfg := DefaultConfig()
	now := time.Now()
	scored := ScoreAllMemories(memories, []string{"target"}, "", cfg, now)
	graph := BuildGraph(memories, cfg)
	boosted := ApplySpreadingActivation(scored, graph, cfg)

	var bBoost, cBoost float64
	for _, s := range boosted {
		switch s.Memory.Title {
		case "B":
			bBoost = s.Boost
		case "C":
			cBoost = s.Boost
		}
	}

	if bBoost <= 0 {
		t.Errorf("B should get boost from A, got %.4f", bBoost)
	}
	if cBoost != 0 {
		t.Errorf("C should NOT get boost (two hops away), got %.4f", cBoost)
	}
}

func TestFindClusters(t *testing.T) {
	memories := makeTestMemories() // 3 interconnected memories
	cfg := DefaultConfig()
	graph := BuildGraph(memories, cfg)

	clusters := graph.FindClusters()
	if len(clusters) == 0 {
		t.Error("expected at least one cluster from 3 interconnected memories")
	}

	// The cluster should contain all 3
	found := false
	for _, c := range clusters {
		if len(c) >= 3 {
			found = true
		}
	}
	if !found {
		t.Error("expected a cluster of size >= 3")
	}
}

func TestEdgeWeights(t *testing.T) {
	cfg := DefaultConfig()

	// contradicts should have highest weight
	if cfg.EdgeWeights["contradicts"] <= cfg.EdgeWeights["related-to"] {
		t.Error("contradicts should have higher weight than related-to")
	}

	// supersedes should have lowest weight
	for rel, w := range cfg.EdgeWeights {
		if rel != "supersedes" && w < cfg.EdgeWeights["supersedes"] {
			t.Errorf("%s (%.1f) should not be less than supersedes (%.1f)", rel, w, cfg.EdgeWeights["supersedes"])
		}
	}
}
