package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseMemoryEntry_TypeQuoteStripped guards the 2026-06-11 quoted-type bug:
// entries authored with `type: "knowledge"` must load as TypeKnowledge, not as
// the literal quoted string (which fails every type equality check).
func TestParseMemoryEntry_TypeQuoteStripped(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]MemoryType{
		`type: "knowledge"`: TypeKnowledge,
		`type: knowledge`:   TypeKnowledge,
		`type: 'semantic'`:  TypeSemantic,
		`type: project`:     TypeProject,
	}
	i := 0
	for typeLine, want := range cases {
		i++
		p := filepath.Join(dir, "e"+string(rune('a'+i))+".md")
		content := "---\n" + typeLine + "\ntitle: \"T\"\nconfidence: 0.5\n---\n\nbody\n"
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		got, err := ParseMemoryEntry(p)
		if err != nil {
			t.Fatalf("%q: %v", typeLine, err)
		}
		if got.Type != want {
			t.Errorf("%q -> Type %q, want %q", typeLine, got.Type, want)
		}
	}
}
