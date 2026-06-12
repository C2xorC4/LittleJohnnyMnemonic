package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestBuildManifestUnion_PrefixMapping verifies knowledge entries are mapped to
// their source book by longest-prefix match, and non-knowledge entries are
// ignored.
func TestBuildManifestUnion_PrefixMapping(t *testing.T) {
	vault := t.TempDir()
	ing := filepath.Join(vault, "Ingestion")
	if err := os.MkdirAll(ing, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(ing, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("manifest_ee.md", "---\ntype: ingestion-manifest\nprefix: \"ee\"\nbook_title: \"Evasion Engineering\"\nbook_year: 2026\n---\n")
	write("manifest_eedr.md", "---\ntype: ingestion-manifest\nprefix: \"eedr\"\nbook_title: \"Evading EDR\"\nbook_year: 2024\n---\n")

	mems := []*MemoryEntry{
		{Type: TypeKnowledge, FileName: "ee_go_lolbin.md"},
		{Type: TypeKnowledge, FileName: "eedr_minifilter.md"}, // must map to eedr, not ee
		{Type: TypeKnowledge, FileName: "unmatched_entry.md"}, // no manifest prefix
		{Type: TypeSemantic, FileName: "ee_not_knowledge.md"}, // non-knowledge, skip
	}
	// FilePath drives normalizeKey; set it so keys resolve under Memory/Knowledge.
	for _, m := range mems {
		m.FilePath = filepath.ToSlash(filepath.Join("Memory", "Knowledge", m.FileName))
	}

	union, err := buildManifestUnion(vault, mems)
	if err != nil {
		t.Fatal(err)
	}

	if u := union["memory/knowledge/ee_go_lolbin"]; u == nil || u.SourceDocument != "Evasion Engineering" || u.SourceVersion != "2026" {
		t.Errorf("ee mapping wrong: %+v", u)
	}
	if u := union["memory/knowledge/eedr_minifilter"]; u == nil || u.SourceDocument != "Evading EDR" {
		t.Errorf("eedr must map to Evading EDR (longest prefix), got: %+v", u)
	}
	if _, ok := union["memory/knowledge/unmatched_entry"]; ok {
		t.Errorf("unmatched entry should not be in union")
	}
	if _, ok := union["memory/knowledge/ee_not_knowledge"]; ok {
		t.Errorf("non-knowledge entry should be skipped")
	}
}
