package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLearnTestMemory(t *testing.T, dir, relPath, linksYAML string) *MemoryEntry {
	t.Helper()
	now := time.Now().Format(time.RFC3339)
	fm := `type: project
title: Test
created: ` + now + `
last_accessed: ` + now + `
access_count: 1
decay_rate: 0.2
confidence: 0.9
tags: [test]
`
	if linksYAML != "" {
		fm += "links:\n" + linksYAML
	}
	writeTestMemory(t, dir, relPath, fm, "body")
	path := filepath.Join(dir, relPath)
	entry, err := loadMemoryEntryByPath(path)
	if err != nil {
		t.Fatal(err)
	}
	return entry
}

func TestHasLearnedEdgeBetween(t *testing.T) {
	a := &MemoryEntry{
		Links: []Link{{Target: "Memory/Project/b", Relationship: "learned"}},
	}
	b := &MemoryEntry{FilePath: "/v/Memory/Project/b.md"}
	if !hasLearnedEdgeBetween(a, b) {
		t.Fatal("expected learned edge detected")
	}
}

func TestApplyLearnedEdgeSpec_overlay(t *testing.T) {
	vault := t.TempDir()
	memA := writeLearnTestMemory(t, vault, "Memory/Project/a.md",
		`  - target: "[[Memory/Project/b]]"
    relationship: related-to
`)
	memB := writeLearnTestMemory(t, vault, "Memory/Project/b.md", "")

	cfg := DefaultConfig()
	graph := BuildGraph([]*MemoryEntry{memA, memB}, cfg)

	spec := BootstrapEdgeSpec{
		ID: 99, Overlay: true,
		MemoryA: MemoryKey(memA),
		MemoryB: MemoryKey(memB),
	}
	res := applyLearnedEdgeSpec(graph, spec, false)
	if !res.Applied {
		t.Fatalf("expected applied, got skipped: %s", res.Skipped)
	}

	freshA, _ := loadMemoryEntryByPath(memA.FilePath)
	freshB, _ := loadMemoryEntryByPath(memB.FilePath)
	if !hasLearnedEdgeBetween(freshA, freshB) {
		t.Error("expected learned edge on disk")
	}
	if !hasEdge(graph, spec.MemoryA, spec.MemoryB) {
		t.Error("related-to edge should remain")
	}
}

func TestApplyLearnedEdgeSpec_addsLearnedAlongsideAuthored(t *testing.T) {
	vault := t.TempDir()
	memA := writeLearnTestMemory(t, vault, "Memory/Project/a.md",
		`  - target: "[[Memory/Project/b]]"
    relationship: related-to
`)
	memB := writeLearnTestMemory(t, vault, "Memory/Project/b.md", "")

	cfg := DefaultConfig()
	graph := BuildGraph([]*MemoryEntry{memA, memB}, cfg)

	spec := BootstrapEdgeSpec{
		ID: 98, Overlay: false,
		MemoryA: MemoryKey(memA),
		MemoryB: MemoryKey(memB),
	}
	res := applyLearnedEdgeSpec(graph, spec, false)
	if !res.Applied {
		t.Fatalf("expected learned overlay on approved bootstrap pair, got: %s", res.Skipped)
	}
	freshA, _ := loadMemoryEntryByPath(memA.FilePath)
	if !hasLearnedEdgeFrom(freshA, memB) {
		t.Error("expected learned edge alongside related-to")
	}
}

func TestParseBootstrapIDs(t *testing.T) {
	ids, err := parseBootstrapIDs("1,2,6")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 3 || ids[2] != 6 {
		t.Fatalf("unexpected ids: %v", ids)
	}
	if _, err := parseBootstrapIDs("99"); err == nil {
		t.Fatal("expected error for unknown id")
	}
}

func TestBootstrapPropose_smoke(t *testing.T) {
	vault := t.TempDir()
	os.MkdirAll(filepath.Join(vault, "Memory", "Project"), 0o755)
	writeLearnTestMemory(t, vault, "Memory/Project/johnny_mnemonic.md", "")
	if err := printBootstrapPropose(vault); err != nil {
		t.Fatal(err)
	}
}