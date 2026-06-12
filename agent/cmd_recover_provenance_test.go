package main

import (
	"path/filepath"
	"reflect"
	"testing"
)

func knEntry(name, sourceDoc string) *MemoryEntry {
	return &MemoryEntry{
		Type:     TypeKnowledge,
		Title:    name,
		SourceDocument: sourceDoc,
		FilePath: filepath.ToSlash(filepath.Join("Memory", "Knowledge", name+".md")),
		FileName: name + ".md",
	}
}

// TestGraftProvenance_FillsOnlyMissing verifies the core safety property: graft
// missing fields from the backup union, never overwrite a field the current
// entry already has, and only touch entries the union knows about.
func TestGraftProvenance_FillsOnlyMissing(t *testing.T) {
	stripped := knEntry("a", "")        // missing source_document → should be grafted
	intact := knEntry("b", "Book B")    // has source_document → must NOT change
	unknown := knEntry("c", "")         // not in union → must NOT change
	stripped.Verified = false
	intact.SourceVersion = "v-current"  // present; union must not override

	union := map[string]*provenanceFields{
		"memory/knowledge/a": {SourceDocument: "Book A", SourceVersion: "2011", Domain: "offsec", HasVerified: true, Verified: true, FromBackup: "vault-x.age"},
		"memory/knowledge/b": {SourceDocument: "WRONG", SourceVersion: "WRONG"}, // must be ignored (present)
	}

	mems := []*MemoryEntry{stripped, intact, unknown}
	changes := graftProvenance(mems, union, true)

	if len(changes) != 1 || changes[0].Key != "memory/knowledge/a" {
		t.Fatalf("expected exactly 1 change to entry a, got %+v", changes)
	}
	if stripped.SourceDocument != "Book A" || stripped.SourceVersion != "2011" ||
		stripped.Domain != "offsec" || !stripped.Verified {
		t.Errorf("entry a not fully grafted: %+v", stripped)
	}
	// Present field must be untouched.
	if intact.SourceDocument != "Book B" || intact.SourceVersion != "v-current" {
		t.Errorf("intact entry b was overwritten: %+v", intact)
	}
	// Unknown entry untouched.
	if unknown.SourceDocument != "" {
		t.Errorf("unknown entry c was modified: %+v", unknown)
	}
	if !reflect.DeepEqual(changes[0].Fields, []string{"source_document", "source_version", "domain", "verified"}) {
		t.Errorf("unexpected grafted field set: %v", changes[0].Fields)
	}
}

// TestGraftProvenance_DryRunDoesNotMutate ensures dry-run computes the diff
// without touching the entry structs.
func TestGraftProvenance_DryRunDoesNotMutate(t *testing.T) {
	stripped := knEntry("a", "")
	union := map[string]*provenanceFields{
		"memory/knowledge/a": {SourceDocument: "Book A"},
	}
	changes := graftProvenance([]*MemoryEntry{stripped}, union, false)
	if len(changes) != 1 {
		t.Fatalf("expected 1 change, got %d", len(changes))
	}
	if stripped.SourceDocument != "" {
		t.Errorf("dry-run mutated the entry: %q", stripped.SourceDocument)
	}
}

// TestGraftProvenance_VerifiedNeverDowngraded ensures a backup with no verified
// field (HasVerified=false) cannot flip a current entry, and never sets false.
func TestGraftProvenance_VerifiedNeverDowngraded(t *testing.T) {
	e := knEntry("a", "")
	union := map[string]*provenanceFields{
		"memory/knowledge/a": {SourceDocument: "Book A", HasVerified: false},
	}
	changes := graftProvenance([]*MemoryEntry{e}, union, true)
	if e.Verified {
		t.Errorf("verified should not have been set")
	}
	// Only source_document grafted.
	if len(changes) != 1 || !reflect.DeepEqual(changes[0].Fields, []string{"source_document"}) {
		t.Errorf("unexpected fields: %+v", changes)
	}
}
