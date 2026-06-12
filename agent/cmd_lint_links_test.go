package main

import (
	"os"
	"path/filepath"
	"testing"
)

// mem builds an in-memory MemoryEntry with a FilePath that normalizeKey resolves
// to "memory/semantic/<name>".
func mem(name, title, body string, links ...Link) *MemoryEntry {
	return &MemoryEntry{
		Type:     TypeSemantic,
		Title:    title,
		Body:     body,
		Links:    links,
		FilePath: filepath.ToSlash(filepath.Join("Memory", "Semantic", name+".md")),
		FileName: name + ".md",
	}
}

func link(target, rel string) Link { return Link{Target: target, Relationship: rel} }

func hasAsym(r lintReport, from, to string) *lintAsymmetric {
	for i := range r.Asymmetric {
		if r.Asymmetric[i].From == from && r.Asymmetric[i].To == to {
			return &r.Asymmetric[i]
		}
	}
	return nil
}

func TestComputeLinkLint_Asymmetric(t *testing.T) {
	// A --related-to--> B, B has no link back → asymmetric, auto-fixable.
	a := mem("a", "Alpha", "", link("Memory/Semantic/b", "related-to"))
	b := mem("b", "Bravo", "")
	r := computeLinkLint([]*MemoryEntry{a, b}, 2, false)

	got := hasAsym(r, "memory/semantic/a", "memory/semantic/b")
	if got == nil {
		t.Fatalf("expected asymmetric a→b, got %+v", r.Asymmetric)
	}
	if !got.AutoFixable {
		t.Errorf("related-to should be auto-fixable")
	}
}

func TestComputeLinkLint_Reciprocal_NotFlagged(t *testing.T) {
	a := mem("a", "Alpha", "", link("Memory/Semantic/b", "related-to"))
	b := mem("b", "Bravo", "", link("Memory/Semantic/a", "related-to"))
	r := computeLinkLint([]*MemoryEntry{a, b}, 2, false)
	if len(r.Asymmetric) != 0 {
		t.Fatalf("reciprocal pair must not be asymmetric, got %+v", r.Asymmetric)
	}
}

func TestComputeLinkLint_DirectionalIsManual(t *testing.T) {
	a := mem("a", "Alpha", "", link("Memory/Semantic/b", "depends-on"))
	b := mem("b", "Bravo", "")
	r := computeLinkLint([]*MemoryEntry{a, b}, 2, false)
	got := hasAsym(r, "memory/semantic/a", "memory/semantic/b")
	if got == nil {
		t.Fatal("expected asymmetric a→b for depends-on")
	}
	if got.AutoFixable {
		t.Errorf("depends-on (directional) must NOT be auto-fixable")
	}
}

func TestComputeLinkLint_SupersedesNotFlagged(t *testing.T) {
	a := mem("a", "Alpha", "", link("Memory/Semantic/b", "supersedes"))
	b := mem("b", "Bravo", "")
	r := computeLinkLint([]*MemoryEntry{a, b}, 2, false)
	if len(r.Asymmetric) != 0 {
		t.Fatalf("supersedes is one-way by design, must not be asymmetric: %+v", r.Asymmetric)
	}
}

func TestComputeLinkLint_Dangling(t *testing.T) {
	a := mem("a", "Alpha", "", link("Memory/Semantic/ghost", "related-to"))
	r := computeLinkLint([]*MemoryEntry{a}, 2, false)
	if len(r.Dangling) != 1 || r.Dangling[0].Target != "memory/semantic/ghost" {
		t.Fatalf("expected one dangling link to ghost, got %+v", r.Dangling)
	}
	// A dangling target must not also be reported as asymmetric.
	if len(r.Asymmetric) != 0 {
		t.Errorf("dangling target should not be asymmetric: %+v", r.Asymmetric)
	}
}

func TestComputeLinkLint_NonLTMTargetNotDangling(t *testing.T) {
	// Buffer/Archive targets are not LTM claims and must not be flagged dangling.
	a := mem("a", "Alpha", "", link("Buffer/2026-01-01_foo", "related-to"))
	r := computeLinkLint([]*MemoryEntry{a}, 2, false)
	if len(r.Dangling) != 0 {
		t.Fatalf("buffer target must not be dangling, got %+v", r.Dangling)
	}
}

func TestComputeLinkLint_ProseOnly(t *testing.T) {
	// A's body links to C in prose; frontmatter links only to B.
	a := mem("a", "Alpha", "context mentions [[Memory/Semantic/c]] here", link("Memory/Semantic/b", "related-to"))
	b := mem("b", "Bravo", "", link("Memory/Semantic/a", "related-to"))
	c := mem("c", "Charlie", "")
	r := computeLinkLint([]*MemoryEntry{a, b, c}, 2, false)
	if len(r.ProseOnly) != 1 || r.ProseOnly[0].From != "memory/semantic/a" || r.ProseOnly[0].To != "memory/semantic/c" {
		t.Fatalf("expected prose-only a→c, got %+v", r.ProseOnly)
	}
}

func TestComputeLinkLint_ProseLinkAlreadyInFrontmatterIgnored(t *testing.T) {
	a := mem("a", "Alpha", "see [[Memory/Semantic/b]]", link("Memory/Semantic/b", "related-to"))
	b := mem("b", "Bravo", "", link("Memory/Semantic/a", "related-to"))
	r := computeLinkLint([]*MemoryEntry{a, b}, 2, false)
	if len(r.ProseOnly) != 0 {
		t.Fatalf("prose link already in frontmatter must be ignored, got %+v", r.ProseOnly)
	}
}

func TestComputeLinkLint_ConceptMention_Stemmed(t *testing.T) {
	// A's body uses inflected forms ("scored", "memories"); D's title is
	// "Memory Scoring". Stemming must bridge tense/plural (scored/scoring→score,
	// memories→memory) so the mention is found despite no literal string match.
	a := mem("a", "Alpha", "every memory is scored before older memories decay", link("Memory/Semantic/b", "related-to"))
	b := mem("b", "Bravo", "", link("Memory/Semantic/a", "related-to"))
	d := mem("d", "Memory Scoring", "")
	r := computeLinkLint([]*MemoryEntry{a, b, d}, 2, true)

	found := false
	for _, c := range r.Concepts {
		if c.From == "memory/semantic/a" && c.To == "memory/semantic/d" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected stemmed concept mention a→d, got %+v", r.Concepts)
	}
}

func TestComputeLinkLint_ConceptSkippedWhenLinked(t *testing.T) {
	a := mem("a", "Alpha", "activation memory", link("Memory/Semantic/d", "related-to"))
	d := mem("d", "Activation Memory", "", link("Memory/Semantic/a", "related-to"))
	r := computeLinkLint([]*MemoryEntry{a, d}, 2, true)
	for _, c := range r.Concepts {
		if c.From == "memory/semantic/a" && c.To == "memory/semantic/d" {
			t.Fatalf("already-linked pair must not be a concept candidate: %+v", c)
		}
	}
}

// TestApplyReciprocalFixes_WritesBackLink exercises the --fix write path against
// real files: only the symmetric asymmetric edge gets a reciprocal, written to
// the target's frontmatter; the directional one is left alone.
func TestApplyReciprocalFixes_WritesBackLink(t *testing.T) {
	vault := t.TempDir()
	dir := filepath.Join(vault, "Memory", "Semantic")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}

	a := mem("a", "Alpha", "body a", link("Memory/Semantic/b", "related-to"))
	b := mem("b", "Bravo", "body b")
	a.FilePath = filepath.Join(dir, "a.md")
	b.FilePath = filepath.Join(dir, "b.md")
	if err := WriteMemoryEntry(a); err != nil {
		t.Fatal(err)
	}
	if err := WriteMemoryEntry(b); err != nil {
		t.Fatal(err)
	}

	report := computeLinkLint([]*MemoryEntry{a, b}, 2, false)
	written, added := applyReciprocalFixes([]*MemoryEntry{a, b}, report)
	if added != 1 || written != 1 {
		t.Fatalf("expected 1 link added across 1 file, got added=%d written=%d", added, written)
	}

	// Reload from disk and confirm B now links back to A, with no double-brackets.
	reloaded, err := LoadAllMemories(vault)
	if err != nil {
		t.Fatal(err)
	}
	var bb *MemoryEntry
	for _, m := range reloaded {
		if normalizeKey(m) == "memory/semantic/b" {
			bb = m
		}
	}
	if bb == nil {
		t.Fatal("reloaded b not found")
	}
	if len(bb.Links) != 1 || normalizeLinkTarget(bb.Links[0].Target) != "memory/semantic/a" {
		t.Fatalf("expected b→a reciprocal after fix, got %+v", bb.Links)
	}
	// Post-fix, the pair must be symmetric (no asymmetric finding).
	r2 := computeLinkLint(reloaded, 2, false)
	if len(r2.Asymmetric) != 0 {
		t.Errorf("post-fix should be symmetric, got %+v", r2.Asymmetric)
	}
}
