package main

import (
	"os"
	"path/filepath"
	"testing"
)

func memAt(typeDir, name string, links ...Link) *MemoryEntry {
	return &MemoryEntry{
		Type:     MemoryType(typeDirToType(typeDir)),
		Title:    name,
		Links:    links,
		FilePath: filepath.ToSlash(filepath.Join("Memory", typeDir, name+".md")),
		FileName: name + ".md",
	}
}

func typeDirToType(d string) string {
	switch d {
	case "Knowledge":
		return "knowledge"
	case "Semantic":
		return "semantic"
	case "User":
		return "user"
	}
	return "semantic"
}

func findDangling(d []lintDangling, target string) *lintDangling {
	for i := range d {
		if d[i].Target == target {
			return &d[i]
		}
	}
	return nil
}

func TestClassifyDangling(t *testing.T) {
	vault := t.TempDir()
	// Seed an archived target.
	archDir := filepath.Join(vault, "Archive", "User")
	if err := os.MkdirAll(archDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(archDir, "gone_obs.md"), []byte("---\ntype: user\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// foo lives in Knowledge; src links to it as Semantic (wrong dir → repoint),
	// to an archived target, and to a truly-missing one.
	foo := memAt("Knowledge", "foo")
	src := memAt("Semantic", "src",
		link("Memory/Semantic/foo", "related-to"),      // repoint → knowledge/foo
		link("Memory/User/gone_obs", "related-to"),     // archived
		link("Memory/Semantic/ghost", "related-to"),    // missing
	)
	mems := []*MemoryEntry{foo, src}

	r := computeLinkLint(mems, 2, false)
	classifyDangling(r.Dangling, mems, vault)

	if d := findDangling(r.Dangling, "memory/semantic/foo"); d == nil || d.Resolution != "repoint" || d.RepointTo != "memory/knowledge/foo" {
		t.Errorf("foo should repoint to knowledge/foo, got %+v", d)
	}
	if d := findDangling(r.Dangling, "memory/user/gone_obs"); d == nil || d.Resolution != "archived" {
		t.Errorf("gone_obs should be archived, got %+v", d)
	}
	if d := findDangling(r.Dangling, "memory/semantic/ghost"); d == nil || d.Resolution != "missing" {
		t.Errorf("ghost should be missing, got %+v", d)
	}
}

func TestApplyDanglingRepoints(t *testing.T) {
	vault := t.TempDir()
	kdir := filepath.Join(vault, "Memory", "Knowledge")
	sdir := filepath.Join(vault, "Memory", "Semantic")
	if err := os.MkdirAll(kdir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sdir, 0o755); err != nil {
		t.Fatal(err)
	}

	foo := &MemoryEntry{Type: TypeKnowledge, Title: "Foo", Body: "x", FilePath: filepath.Join(kdir, "foo.md"), FileName: "foo.md"}
	src := &MemoryEntry{Type: TypeSemantic, Title: "Src", Body: "y", FilePath: filepath.Join(sdir, "src.md"), FileName: "src.md",
		Links: []Link{{Target: "Memory/Semantic/foo", Relationship: "related-to"}}} // wrong dir
	if err := WriteMemoryEntry(foo); err != nil {
		t.Fatal(err)
	}
	if err := WriteMemoryEntry(src); err != nil {
		t.Fatal(err)
	}

	mems := []*MemoryEntry{foo, src}
	r := computeLinkLint(mems, 2, false)
	classifyDangling(r.Dangling, mems, vault)
	written, repointed := applyDanglingRepoints(mems, r.Dangling)
	if written != 1 || repointed != 1 {
		t.Fatalf("expected 1 repoint across 1 file, got written=%d repointed=%d", written, repointed)
	}

	// Reload src; its link must now resolve to the live knowledge/foo, and be clean.
	reloaded, err := LoadAllMemories(vault)
	if err != nil {
		t.Fatal(err)
	}
	r2 := computeLinkLint(reloaded, 2, false)
	if len(r2.Dangling) != 0 {
		t.Fatalf("expected no dangling after repoint, got %+v", r2.Dangling)
	}
}
