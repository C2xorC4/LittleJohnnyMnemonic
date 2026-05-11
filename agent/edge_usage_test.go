package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEdgeUsage_LoadEmptyOrAbsent(t *testing.T) {
	vault := t.TempDir()
	usage, err := LoadEdgeUsage(vault)
	if err != nil {
		t.Fatalf("load absent: %v", err)
	}
	if len(usage) != 0 {
		t.Errorf("expected empty map, got %d entries", len(usage))
	}
}

func TestEdgeUsage_SaveAndLoadRoundtrip(t *testing.T) {
	vault := t.TempDir()

	now := time.Now().Truncate(time.Second)
	original := map[string]EdgeUsage{
		edgeUsageKey("A", "B", "learned"): {
			Source: "A", Target: "B", Relationship: "learned",
			UsageCount: 3, LastUsed: now,
		},
		edgeUsageKey("B", "A", "learned"): {
			Source: "B", Target: "A", Relationship: "learned",
			UsageCount: 1, LastUsed: now,
		},
	}

	if err := SaveEdgeUsage(vault, original); err != nil {
		t.Fatalf("save: %v", err)
	}

	loaded, err := LoadEdgeUsage(vault)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d entries, expected 2", len(loaded))
	}

	ab, ok := loaded[edgeUsageKey("A", "B", "learned")]
	if !ok {
		t.Fatalf("A->B entry missing after roundtrip")
	}
	if ab.UsageCount != 3 {
		t.Errorf("A->B UsageCount = %d, expected 3", ab.UsageCount)
	}
}

func TestEdgeUsage_LogPathLocation(t *testing.T) {
	vault := t.TempDir()
	usage := map[string]EdgeUsage{
		edgeUsageKey("A", "B", "learned"): {
			Source: "A", Target: "B", Relationship: "learned", UsageCount: 1, LastUsed: time.Now(),
		},
	}
	if err := SaveEdgeUsage(vault, usage); err != nil {
		t.Fatalf("save: %v", err)
	}
	expected := filepath.Join(vault, "Metrics", "edge_usage.jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected log at %s, got error: %v", expected, err)
	}
}

func TestLinkTargetMatches(t *testing.T) {
	cases := []struct {
		linkTarget string
		sessionKey string
		want       bool
	}{
		{"Memory/Project/argus", "Memory/Project/argus", true},
		{"[[Memory/Project/argus]]", "Memory/Project/argus", true},
		{"Memory/Project/argus.md", "Memory/Project/argus", true},
		{"[[Memory/Project/argus.md]]", "Memory/Project/argus", true},
		{"Memory/Project/argus", "Memory/Project/different", false},
	}
	for _, c := range cases {
		got := linkTargetMatches(c.linkTarget, c.sessionKey)
		if got != c.want {
			t.Errorf("linkTargetMatches(%q, %q) = %v, want %v", c.linkTarget, c.sessionKey, got, c.want)
		}
	}
}

func TestRecordEdgeUsageFromCitation_NoSession(t *testing.T) {
	vault := t.TempDir()
	// Missing session ID — should no-op without error.
	reinforced, err := RecordEdgeUsageFromCitation(vault, "", "Memory/X/a", []string{"learned"})
	if err != nil {
		t.Errorf("no-session call returned error: %v", err)
	}
	if len(reinforced) != 0 {
		t.Errorf("no-session call reinforced %d edges, expected 0", len(reinforced))
	}
}

func TestRecordEdgeUsageFromCitation_EmptyScope(t *testing.T) {
	vault := t.TempDir()
	// Empty scope — should no-op without error.
	reinforced, err := RecordEdgeUsageFromCitation(vault, "session-x", "Memory/X/a", nil)
	if err != nil {
		t.Errorf("empty-scope call returned error: %v", err)
	}
	if len(reinforced) != 0 {
		t.Errorf("empty-scope call reinforced %d edges, expected 0", len(reinforced))
	}
}

func TestRecordEdgeUsageFromCitation_SessionNotFound(t *testing.T) {
	vault := t.TempDir()
	// Session ID not in log — silent no-op (expected after pruning).
	reinforced, err := RecordEdgeUsageFromCitation(vault, "ghost-session", "Memory/X/a", []string{"learned"})
	if err != nil {
		t.Errorf("ghost-session call returned error: %v", err)
	}
	if len(reinforced) != 0 {
		t.Errorf("ghost-session call reinforced %d edges, expected 0", len(reinforced))
	}
}

func TestRecordEdgeUsageFromCitation_BumpsLearnedEdges(t *testing.T) {
	vault := t.TempDir()

	// Set up vault structure with two memories and a learned edge between them.
	memoryDir := filepath.Join(vault, "Memory", "Project")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	// Memory A — has a learned edge pointing to B.
	pathA := filepath.Join(memoryDir, "alpha.md")
	memA := &MemoryEntry{
		Type:               TypeProject,
		Title:              "Alpha",
		Created:            time.Now(),
		LastAccessed:       time.Now(),
		AccessCount:        1,
		DecayRate:          0.3,
		Confidence:         0.9,
		SurpriseAtEncoding: 0.5,
		Tags:               []string{"test"},
		Links: []Link{
			{Target: "Memory/Project/beta", Relationship: "learned"},
		},
		Body:     "alpha body",
		FilePath: pathA,
	}
	if err := WriteMemoryEntry(memA); err != nil {
		t.Fatalf("write alpha: %v", err)
	}

	// Memory B — no outgoing edges.
	pathB := filepath.Join(memoryDir, "beta.md")
	memB := &MemoryEntry{
		Type:               TypeProject,
		Title:              "Beta",
		Created:            time.Now(),
		LastAccessed:       time.Now(),
		AccessCount:        1,
		DecayRate:          0.3,
		Confidence:         0.9,
		SurpriseAtEncoding: 0.5,
		Tags:               []string{"test"},
		Body:               "beta body",
		FilePath:           pathB,
	}
	if err := WriteMemoryEntry(memB); err != nil {
		t.Fatalf("write beta: %v", err)
	}

	// Set up a retrieval session that loaded both alpha and beta.
	session := RetrievalSession{
		SessionID: "test-session-1",
		Timestamp: time.Now(),
		Loaded:    []string{"Memory/Project/alpha", "Memory/Project/beta"},
	}
	if err := AppendRetrievalSession(vault, session); err != nil {
		t.Fatalf("append session: %v", err)
	}

	// Now record a citation on beta with the session ID, scope = learned.
	reinforced, err := RecordEdgeUsageFromCitation(vault, "test-session-1", "Memory/Project/beta", []string{"learned"})
	if err != nil {
		t.Fatalf("RecordEdgeUsageFromCitation: %v", err)
	}
	if len(reinforced) != 1 {
		t.Fatalf("reinforced %d edges, expected 1; entries: %+v", len(reinforced), reinforced)
	}
	if reinforced[0].Source != "memory/project/alpha" || reinforced[0].Target != "memory/project/beta" {
		t.Errorf("reinforced edge = %+v, expected alpha→beta (lower-cased)", reinforced[0])
	}
	if reinforced[0].UsageCount != 1 {
		t.Errorf("UsageCount = %d, expected 1", reinforced[0].UsageCount)
	}

	// Confirm persistence.
	usage, err := LoadEdgeUsage(vault)
	if err != nil {
		t.Fatalf("load usage: %v", err)
	}
	key := edgeUsageKey("memory/project/alpha", "memory/project/beta", "learned")
	if e, ok := usage[key]; !ok {
		t.Errorf("edge usage not persisted; keys present: %v", keysOf(usage))
	} else if e.UsageCount != 1 {
		t.Errorf("persisted UsageCount = %d, expected 1", e.UsageCount)
	}

	// Repeat citation — usage_count should now be 2.
	if _, err := RecordEdgeUsageFromCitation(vault, "test-session-1", "Memory/Project/beta", []string{"learned"}); err != nil {
		t.Fatalf("second reinforcement: %v", err)
	}
	usage2, _ := LoadEdgeUsage(vault)
	if e, ok := usage2[key]; !ok || e.UsageCount != 2 {
		t.Errorf("after second citation: UsageCount = %d, expected 2", e.UsageCount)
	}
}

func TestRecordEdgeUsageFromCitation_OutOfScopeIgnored(t *testing.T) {
	vault := t.TempDir()

	memoryDir := filepath.Join(vault, "Memory", "Project")
	if err := os.MkdirAll(memoryDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	pathA := filepath.Join(memoryDir, "alpha.md")
	memA := &MemoryEntry{
		Type:               TypeProject,
		Title:              "Alpha",
		Created:            time.Now(),
		LastAccessed:       time.Now(),
		AccessCount:        1,
		DecayRate:          0.3,
		Confidence:         0.9,
		SurpriseAtEncoding: 0.5,
		Tags:               []string{"test"},
		Links: []Link{
			// Two edges with different relationships
			{Target: "Memory/Project/beta", Relationship: "learned"},
			{Target: "Memory/Project/gamma", Relationship: "related-to"},
		},
		Body:     "alpha body",
		FilePath: pathA,
	}
	if err := WriteMemoryEntry(memA); err != nil {
		t.Fatalf("write alpha: %v", err)
	}

	// Need beta and gamma to exist as files so LoadAllMemories sees them.
	for _, name := range []string{"beta", "gamma"} {
		path := filepath.Join(memoryDir, name+".md")
		mem := &MemoryEntry{
			Type:               TypeProject,
			Title:              name,
			Created:            time.Now(),
			LastAccessed:       time.Now(),
			AccessCount:        1,
			DecayRate:          0.3,
			Confidence:         0.9,
			SurpriseAtEncoding: 0.5,
			Tags:               []string{"test"},
			Body:               name + " body",
			FilePath:           path,
		}
		if err := WriteMemoryEntry(mem); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	session := RetrievalSession{
		SessionID: "test-session-scope",
		Timestamp: time.Now(),
		Loaded: []string{
			"Memory/Project/alpha",
			"Memory/Project/beta",
			"Memory/Project/gamma",
		},
	}
	if err := AppendRetrievalSession(vault, session); err != nil {
		t.Fatalf("append session: %v", err)
	}

	// Cite alpha; scope is only "learned" — should reinforce alpha→beta
	// (learned) but NOT alpha→gamma (related-to).
	reinforced, err := RecordEdgeUsageFromCitation(vault, "test-session-scope", "Memory/Project/alpha", []string{"learned"})
	if err != nil {
		t.Fatalf("reinforcement: %v", err)
	}
	if len(reinforced) != 1 {
		t.Fatalf("reinforced %d edges, expected 1 (learned only); entries: %+v", len(reinforced), reinforced)
	}
	if reinforced[0].Target != "memory/project/beta" || reinforced[0].Relationship != "learned" {
		t.Errorf("expected alpha→beta learned reinforcement, got %+v", reinforced[0])
	}
}

func keysOf(m map[string]EdgeUsage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
