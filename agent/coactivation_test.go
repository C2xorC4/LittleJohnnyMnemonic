package main

import (
	"testing"
	"time"
)

func TestRecordCoactivation_NewPairs(t *testing.T) {
	log := &CoactivationLog{}
	keys := []string{"memory/user/profile_a", "memory/user/profile_b", "memory/project/mimic"}
	RecordCoactivation(log, keys, "test context", 5)

	// 3 memories = 3 pairs: A-B, A-Mimic, B-Mimic
	if len(log.Pairs) != 3 {
		t.Fatalf("expected 3 pairs, got %d", len(log.Pairs))
	}
	for _, p := range log.Pairs {
		if p.Count != 1 {
			t.Errorf("expected count 1, got %d for %s↔%s", p.Count, p.MemoryA, p.MemoryB)
		}
	}
}

func TestRecordCoactivation_IncrementExisting(t *testing.T) {
	log := &CoactivationLog{}
	keys := []string{"memory/user/a", "memory/user/b"}

	RecordCoactivation(log, keys, "first", 5)
	RecordCoactivation(log, keys, "second", 5)
	RecordCoactivation(log, keys, "third", 5)

	if len(log.Pairs) != 1 {
		t.Fatalf("expected 1 pair, got %d", len(log.Pairs))
	}
	if log.Pairs[0].Count != 3 {
		t.Errorf("expected count 3, got %d", log.Pairs[0].Count)
	}
	if len(log.Pairs[0].Contexts) != 3 {
		t.Errorf("expected 3 contexts, got %d", len(log.Pairs[0].Contexts))
	}
}

func TestRecordCoactivation_ContextCap(t *testing.T) {
	log := &CoactivationLog{}
	keys := []string{"memory/user/a", "memory/user/b"}

	for i := 0; i < 10; i++ {
		RecordCoactivation(log, keys, "context", 3) // cap at 3
	}

	if len(log.Pairs[0].Contexts) != 3 {
		t.Errorf("expected 3 contexts (capped), got %d", len(log.Pairs[0].Contexts))
	}
	if log.Pairs[0].Count != 10 {
		t.Errorf("count should still be 10, got %d", log.Pairs[0].Count)
	}
}

func TestRecordCoactivation_SingleMemory(t *testing.T) {
	log := &CoactivationLog{}
	RecordCoactivation(log, []string{"memory/user/a"}, "solo", 5)

	if len(log.Pairs) != 0 {
		t.Error("single memory should produce no pairs")
	}
}

func TestPairKey_Canonical(t *testing.T) {
	// Order shouldn't matter
	k1 := pairKey("memory/a", "memory/b")
	k2 := pairKey("memory/b", "memory/a")
	if k1 != k2 {
		t.Errorf("pair keys should be canonical: %s != %s", k1, k2)
	}
}

func TestFindLearnedEdgeCandidates(t *testing.T) {
	now := time.Now()

	log := &CoactivationLog{
		Pairs: []CoactivationPair{
			{MemoryA: "memory/user/a", MemoryB: "memory/user/b", Count: 5, LastSeen: now},
			{MemoryA: "memory/user/a", MemoryB: "memory/project/c", Count: 2, LastSeen: now}, // below threshold
			{MemoryA: "memory/user/b", MemoryB: "memory/project/c", Count: 4, LastSeen: now},
		},
	}

	// Build a graph with an existing edge between A and B
	cfg := DefaultConfig()
	memA := &MemoryEntry{
		FilePath: "/vault/Memory/User/a.md",
		Type:     TypeUser,
		Links: []Link{
			{Target: "[[Memory/User/b]]", Relationship: "related-to"},
		},
	}
	memB := &MemoryEntry{
		FilePath: "/vault/Memory/User/b.md",
		Type:     TypeUser,
	}
	memC := &MemoryEntry{
		FilePath: "/vault/Memory/Project/c.md",
		Type:     TypeProject,
	}
	graph := BuildGraph([]*MemoryEntry{memA, memB, memC}, cfg)

	candidates := FindLearnedEdgeCandidates(log, graph, 3)

	// A-B already linked, A-C below threshold → only B-C should be candidate
	if len(candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(candidates))
	}
	if candidates[0].Count != 4 {
		t.Errorf("expected count 4, got %d", candidates[0].Count)
	}
}
