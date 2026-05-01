package main

import (
	"path/filepath"
	"testing"
)

func TestComputePairTagOverlap_NoOverlap(t *testing.T) {
	vault := t.TempDir()
	a := writeBufFile(t, vault, "", "a.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [foo, bar]", "x")
	b := writeBufFile(t, vault, "", "b.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [baz, qux]", "x")

	overlap := ComputePairTagOverlap(a, b)
	if overlap != 0 {
		t.Errorf("disjoint tags: got %v, want 0", overlap)
	}
}

func TestComputePairTagOverlap_FullOverlap(t *testing.T) {
	vault := t.TempDir()
	a := writeBufFile(t, vault, "", "a.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [foo, bar]", "x")
	b := writeBufFile(t, vault, "", "b.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [foo, bar]", "x")

	overlap := ComputePairTagOverlap(a, b)
	if overlap != 1.0 {
		t.Errorf("identical tags: got %v, want 1.0", overlap)
	}
}

func TestComputePairTagOverlap_PartialOverlap(t *testing.T) {
	vault := t.TempDir()
	a := writeBufFile(t, vault, "", "a.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [foo, bar, baz]", "x")
	b := writeBufFile(t, vault, "", "b.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [bar, baz, qux]", "x")

	overlap := ComputePairTagOverlap(a, b)
	// Jaccard = |{bar,baz}| / |{foo,bar,baz,qux}| = 2/4 = 0.5
	if overlap != 0.5 {
		t.Errorf("partial overlap: got %v, want 0.5", overlap)
	}
}

func TestComputePairTagOverlap_CaseInsensitive(t *testing.T) {
	vault := t.TempDir()
	a := writeBufFile(t, vault, "", "a.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [Foo, BAR]", "x")
	b := writeBufFile(t, vault, "", "b.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [foo, bar]", "x")

	overlap := ComputePairTagOverlap(a, b)
	if overlap != 1.0 {
		t.Errorf("case-insensitive match: got %v, want 1.0", overlap)
	}
}

func TestComputePairTagOverlap_MissingFileReturnsZero(t *testing.T) {
	overlap := ComputePairTagOverlap("/nonexistent/a.md", "/nonexistent/b.md")
	if overlap != 0 {
		t.Errorf("missing files: got %v, want 0", overlap)
	}
}

func TestComputePairTagOverlap_ParsesMemoryEntries(t *testing.T) {
	vault := t.TempDir()
	mem := writeMemFile(t, vault, "Semantic", "m.md",
		"type: semantic\ntitle: M\nlast_accessed: 2026-04-30T10:00:00Z\naccess_count: 30\ntags: [pattern, observation]", "x")
	buf := writeBufFile(t, vault, "", "b.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [pattern, observation]", "x")

	overlap := ComputePairTagOverlap(buf, mem)
	if overlap != 1.0 {
		t.Errorf("memory + buffer with same tags: got %v, want 1.0", overlap)
	}
	// Sanity: mem path is under Memory/Semantic
	if !filepath.IsAbs(mem) {
		t.Error("expected absolute mem path")
	}
}

func TestComputePairTagOverlap_EmptyTagsReturnsZero(t *testing.T) {
	vault := t.TempDir()
	a := writeBufFile(t, vault, "", "a.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5", "x") // no tags
	b := writeBufFile(t, vault, "", "b.md",
		"type: buffer\ntimestamp: 2026-05-01T10:00:00Z\nsource: conversation\nsurprise: 0.5\ntags: [foo]", "x")

	overlap := ComputePairTagOverlap(a, b)
	if overlap != 0 {
		t.Errorf("empty tag set: got %v, want 0", overlap)
	}
}

func TestJaccardTags_SymmetryAndBoundedness(t *testing.T) {
	a := []string{"x", "y", "z"}
	b := []string{"y", "z", "w"}
	o1 := jaccardTags(a, b)
	o2 := jaccardTags(b, a)
	if o1 != o2 {
		t.Errorf("Jaccard not symmetric: %v vs %v", o1, o2)
	}
	if o1 < 0 || o1 > 1 {
		t.Errorf("Jaccard out of [0,1]: %v", o1)
	}
}
