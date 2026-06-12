package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLearnedEdgeTarget_SingleBrackets guards the learn-edges double-bracket bug:
// Link.Target must hold the bracketless canonical path (WriteMemoryEntry wraps it
// in [[ ]]). The old toWikiLink returned an already-bracketed value, producing
// [[[[...]]]] on disk.
func TestLearnedEdgeTarget_SingleBrackets(t *testing.T) {
	dir := t.TempDir()
	a := &MemoryEntry{Type: TypeSemantic, Title: "A", Body: "x", FilePath: filepath.Join(dir, "a.md")}
	b := &MemoryEntry{Type: TypeSemantic, Title: "B", FilePath: filepath.ToSlash(filepath.Join("Memory", "Semantic", "b.md"))}

	// Emulate the learn-edges graft (same construction as cmd_learn.go).
	a.Links = append(a.Links, Link{Target: relMemoryPath(b), Relationship: "learned"})
	if err := WriteMemoryEntry(a); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(a.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "[[[[") {
		t.Fatalf("double brackets written to disk:\n%s", raw)
	}
	if !strings.Contains(string(raw), "[[Memory/Semantic/b]]") {
		t.Fatalf("expected single-bracket target, got:\n%s", raw)
	}

	got, err := ParseMemoryEntry(a.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Links) != 1 || normalizeLinkTarget(got.Links[0].Target) != "memory/semantic/b" {
		t.Fatalf("learned edge did not round-trip: %+v", got.Links)
	}
}
