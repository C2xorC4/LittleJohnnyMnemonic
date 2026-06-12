package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseKeyMapAndSet(t *testing.T) {
	m := parseKeyMap("user/obs_x=user/profile_x, Memory/Semantic/a=semantic/b")
	if m["memory/user/obs_x"] != "memory/user/profile_x" {
		t.Errorf("obs_x mapping wrong: %v", m)
	}
	if m["memory/semantic/a"] != "memory/semantic/b" {
		t.Errorf("a mapping wrong: %v", m)
	}
	s := parseKeySet("project/lockbit_sim, reference/testing_environment")
	if !s["memory/project/lockbit_sim"] || !s["memory/reference/testing_environment"] {
		t.Errorf("key set wrong: %v", s)
	}
}

func TestApplyRepointAndRemove(t *testing.T) {
	vault := t.TempDir()
	udir := filepath.Join(vault, "Memory", "User")
	sdir := filepath.Join(vault, "Memory", "Semantic")
	for _, d := range []string{udir, sdir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	profile := &MemoryEntry{Type: TypeUser, Title: "P", Body: "x", FilePath: filepath.Join(udir, "profile_x.md"), FileName: "profile_x.md"}
	src := &MemoryEntry{Type: TypeSemantic, Title: "S", Body: "y", FilePath: filepath.Join(sdir, "src.md"), FileName: "src.md",
		Links: []Link{
			{Target: "Memory/User/obs_x", Relationship: "related-to"},   // repoint → profile_x
			{Target: "Memory/Project/dead", Relationship: "related-to"}, // remove
		}}
	if err := WriteMemoryEntry(profile); err != nil {
		t.Fatal(err)
	}
	if err := WriteMemoryEntry(src); err != nil {
		t.Fatal(err)
	}
	mems := []*MemoryEntry{profile, src}

	if w, n := applyRepointMap(mems, parseKeyMap("user/obs_x=user/profile_x")); w != 1 || n != 1 {
		t.Fatalf("repoint: written=%d n=%d", w, n)
	}
	if w, n := applyRemoveLinks(mems, parseKeySet("project/dead")); w != 1 || n != 1 {
		t.Fatalf("remove: written=%d n=%d", w, n)
	}

	reloaded, err := LoadAllMemories(vault)
	if err != nil {
		t.Fatal(err)
	}
	var s *MemoryEntry
	for _, m := range reloaded {
		if m.FileName == "src.md" {
			s = m
		}
	}
	if s == nil {
		t.Fatal("src not reloaded")
	}
	if len(s.Links) != 1 {
		t.Fatalf("expected 1 link after repoint+remove, got %+v", s.Links)
	}
	if normalizeLinkTarget(s.Links[0].Target) != "memory/user/profile_x" {
		t.Fatalf("link not repointed to profile_x: %+v", s.Links[0])
	}
}
