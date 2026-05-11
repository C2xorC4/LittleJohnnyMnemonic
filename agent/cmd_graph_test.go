package main

import (
	"strings"
	"testing"
	"time"
)

func graphTestMemories(now time.Time) []*MemoryEntry {
	return []*MemoryEntry{
		{
			Title: "Go expertise", Type: TypeUser,
			FilePath:     "Memory/User/go_expertise.md",
			LastAccessed: now.Add(-24 * time.Hour), AccessCount: 7, DecayRate: 0.3,
			Confidence: 0.95, Tags: []string{"go", "tooling"},
			Links: []Link{
				{Target: "Memory/User/security_expertise", Relationship: "related-to"},
			},
		},
		{
			Title: "Security expertise", Type: TypeUser,
			FilePath:     "Memory/User/security_expertise.md",
			LastAccessed: now.Add(-2 * time.Hour), AccessCount: 12, DecayRate: 0.3,
			Confidence: 0.95, Tags: []string{"security"},
			Links: []Link{
				{Target: "Memory/User/security_expertise_old", Relationship: "supersedes"},
				{Target: "Memory/Feedback/clarification", Relationship: "depends-on"},
			},
		},
		{
			Title: "Security expertise (old version)", Type: TypeUser,
			FilePath:     "Memory/User/security_expertise_old.md",
			LastAccessed: now.Add(-1000 * time.Hour), AccessCount: 1, DecayRate: 0.3,
			Confidence: 0.5, Tags: []string{"security"},
		},
		{
			Title: "Clarification over refusal", Type: TypeFeedback,
			FilePath:     "Memory/Feedback/clarification.md",
			LastAccessed: now.Add(-48 * time.Hour), AccessCount: 4, DecayRate: 0.3,
			Confidence: 0.9, Tags: []string{"communication"},
			Links: []Link{
				{Target: "Memory/User/go_expertise", Relationship: "refines"},
			},
		},
		{
			Title: "Process injection techniques", Type: TypeKnowledge,
			FilePath: "Memory/Knowledge/process_injection.md",
			Confidence: 1.0, Tags: []string{"windows", "offensive"},
		},
	}
}

func findNode(p graphPayload, idSubstring string) *graphNode {
	for i, n := range p.Nodes {
		if strings.Contains(n.ID, idSubstring) {
			return &p.Nodes[i]
		}
	}
	return nil
}

func countEdges(p graphPayload, kind string) int {
	c := 0
	for _, e := range p.Edges {
		if e.Kind == kind {
			c++
		}
	}
	return c
}

func TestBuildGraphPayload_BasicCounts(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -100})

	if len(p.Nodes) != 5 {
		t.Errorf("expected 5 nodes, got %d", len(p.Nodes))
	}
	// Edges: go<->security (related-to, dedup), security->security_old (supersedes, directed),
	// security<->clarification (depends-on), clarification<->go (refines).
	// Total expected: 4
	if len(p.Edges) != 4 {
		t.Errorf("expected 4 edges, got %d: %+v", len(p.Edges), p.Edges)
	}
}

func TestBuildGraphPayload_EdgeDedup(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -100})

	// related-to between go_expertise and security_expertise should appear once,
	// not twice (BuildGraph stores it bidirectionally)
	relatedToCount := 0
	for _, e := range p.Edges {
		if e.Relationship == "related-to" &&
			(strings.Contains(e.Source, "go_expertise") || strings.Contains(e.Target, "go_expertise")) &&
			(strings.Contains(e.Source, "security_expertise") || strings.Contains(e.Target, "security_expertise")) {
			relatedToCount++
		}
	}
	if relatedToCount != 1 {
		t.Errorf("expected 1 related-to edge between go and security, got %d", relatedToCount)
	}
}

func TestBuildGraphPayload_SupersedesDirected(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -100})

	var supersedes *graphEdge
	for i, e := range p.Edges {
		if e.Relationship == "supersedes" {
			supersedes = &p.Edges[i]
			break
		}
	}
	if supersedes == nil {
		t.Fatal("expected a supersedes edge")
	}
	if !supersedes.Directed {
		t.Error("supersedes edge should be directed")
	}
	if !strings.Contains(supersedes.Source, "security_expertise") ||
		strings.Contains(supersedes.Source, "_old") {
		t.Errorf("supersedes source should be the new entry, got %q", supersedes.Source)
	}
	if !strings.Contains(supersedes.Target, "security_expertise_old") {
		t.Errorf("supersedes target should be the old entry, got %q", supersedes.Target)
	}
}

func TestBuildGraphPayload_KnowledgeActivationFixed(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -100})

	knowledge := findNode(p, "process_injection")
	if knowledge == nil {
		t.Fatal("expected knowledge node in payload")
	}
	if knowledge.Activation != 1.0 {
		t.Errorf("knowledge activation should be 1.0, got %f", knowledge.Activation)
	}
	if knowledge.Type != "knowledge" {
		t.Errorf("knowledge type field wrong: %q", knowledge.Type)
	}
}

func TestBuildGraphPayload_CoactivationOverlay_AddsNewPair(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	// process_injection has no explicit edges. Coactivation pair with go_expertise.
	log := &CoactivationLog{
		Pairs: []CoactivationPair{
			{
				MemoryA: "memory/user/go_expertise",
				MemoryB: "memory/knowledge/process_injection",
				Count:   5,
			},
		},
	}

	p := buildGraphPayload(mems, log, cfg, now,
		graphOpts{IncludeCoactivation: true, CoactivationThreshold: 3, MinActivation: -100})

	if countEdges(p, "coactivation") != 1 {
		t.Errorf("expected 1 coactivation edge, got %d", countEdges(p, "coactivation"))
	}
}

func TestBuildGraphPayload_CoactivationOverlay_SkipsExisting(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	// go_expertise <-> security_expertise already has an explicit related-to edge.
	// A coactivation pair for the same pair should NOT add a second edge.
	log := &CoactivationLog{
		Pairs: []CoactivationPair{
			{
				MemoryA: "memory/user/go_expertise",
				MemoryB: "memory/user/security_expertise",
				Count:   10,
			},
		},
	}

	p := buildGraphPayload(mems, log, cfg, now,
		graphOpts{IncludeCoactivation: true, CoactivationThreshold: 3, MinActivation: -100})

	if countEdges(p, "coactivation") != 0 {
		t.Errorf("coactivation pair already covered by explicit edge should be skipped, got %d", countEdges(p, "coactivation"))
	}
}

func TestBuildGraphPayload_CoactivationOverlay_BelowThreshold(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	log := &CoactivationLog{
		Pairs: []CoactivationPair{
			{
				MemoryA: "memory/user/go_expertise",
				MemoryB: "memory/knowledge/process_injection",
				Count:   2, // below default threshold of 3
			},
		},
	}

	p := buildGraphPayload(mems, log, cfg, now,
		graphOpts{IncludeCoactivation: true, CoactivationThreshold: 3, MinActivation: -100})

	if countEdges(p, "coactivation") != 0 {
		t.Errorf("pair below threshold should be skipped, got %d coactivation edges", countEdges(p, "coactivation"))
	}
}

func TestBuildGraphPayload_EmptyVault(t *testing.T) {
	now := time.Now()
	cfg := DefaultConfig()

	p := buildGraphPayload(nil, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: true, MinActivation: -100})

	if len(p.Nodes) != 0 {
		t.Errorf("expected 0 nodes for empty vault, got %d", len(p.Nodes))
	}
	if len(p.Edges) != 0 {
		t.Errorf("expected 0 edges for empty vault, got %d", len(p.Edges))
	}
}

func TestBuildGraphPayload_TypeFilter(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeTypes: []string{"user"}, MinActivation: -100})

	for _, n := range p.Nodes {
		if n.Type != "user" {
			t.Errorf("type filter should only allow user, got %q", n.Type)
		}
	}
	if len(p.Nodes) != 3 {
		t.Errorf("expected 3 user nodes after filter, got %d", len(p.Nodes))
	}
}

func TestBuildGraphPayload_DegreeMatchesEdges(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -100})

	expected := make(map[string]int)
	for _, e := range p.Edges {
		expected[e.Source]++
		expected[e.Target]++
	}
	for _, n := range p.Nodes {
		if expected[n.ID] != n.Degree {
			t.Errorf("node %q: expected degree %d, got %d", n.ID, expected[n.ID], n.Degree)
		}
	}
}

func TestBuildGraphPayload_MinActivationDropsEdges(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	// Pick a min-activation that drops the cold security_expertise_old node
	// (1 access, 1000 hours ago, decay 0.3 → activation ≈ -2.07).
	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -1.0})

	if findNode(p, "security_expertise_old") != nil {
		t.Error("security_expertise_old should be filtered out by min-activation")
	}

	for _, e := range p.Edges {
		if strings.Contains(e.Source, "security_expertise_old") || strings.Contains(e.Target, "security_expertise_old") {
			t.Errorf("edge %v references a filtered-out node", e)
		}
	}
}

func TestBuildGraphPayload_RenderHTMLSucceeds(t *testing.T) {
	now := time.Now()
	mems := graphTestMemories(now)
	cfg := DefaultConfig()

	p := buildGraphPayload(mems, &CoactivationLog{}, cfg, now,
		graphOpts{IncludeCoactivation: false, MinActivation: -100})

	html, err := renderGraphHTML(p, "test")
	if err != nil {
		t.Fatalf("renderGraphHTML failed: %v", err)
	}
	if len(html) == 0 {
		t.Fatal("rendered HTML is empty")
	}
	s := string(html)
	if !strings.Contains(s, "GRAPH_DATA") {
		t.Error("rendered HTML missing GRAPH_DATA injection")
	}
	if !strings.Contains(s, "vis-network") {
		t.Error("rendered HTML missing vis-network library")
	}
	// Should not have any external resource references (offline-safe)
	if strings.Contains(s, "src=\"http") || strings.Contains(s, "href=\"http") {
		t.Error("rendered HTML references external URLs — not offline-safe")
	}
}
