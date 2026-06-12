package main

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestWriteParseRoundTrip_AllFields is the regression guard for the 2026-06-11
// silent-field-loss bug: ParseMemoryEntry did not read consolidation_source,
// contributing_sessions, source_document, source_version, domain, or verified,
// and WriteMemoryEntry did not emit the last five — so every access-tracking
// rewrite (which happens on EVERY retrieval) stripped them from disk. For
// Knowledge entries that meant losing required provenance.
//
// This test populates every persisted frontmatter field, writes the entry,
// reparses it, and asserts each field survives the round trip. Against the
// pre-fix code it fails on the six dropped fields.
func TestWriteParseRoundTrip_AllFields(t *testing.T) {
	dir := t.TempDir()
	created := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	accessed := time.Date(2026, 6, 1, 12, 0, 0, 0, time.UTC)

	want := &MemoryEntry{
		Type:                 TypeKnowledge,
		Title:                "Windows Internals Ch.3 — Processes",
		Created:              created,
		LastAccessed:         accessed,
		AccessCount:          7,
		DecayRate:            0.10,
		Confidence:           0.85,
		SurpriseAtEncoding:   0.70,
		ConsolidationSource:  []string{"[[Buffer/2026-05-01_obs]]", "[[Buffer/2026-05-02_obs]]"},
		Tags:                 []string{"windows-internals", "processes"},
		Links:                []Link{{Target: "Memory/Knowledge/ntdll_structures", Relationship: "related-to"}},
		TrainingOverride:     true,
		OverrideContext:      "user corrected the EPROCESS layout",
		SourceAuthority:      "user-confirmed",
		ValidatedVia:         []string{"live binary", "windbg"},
		Fidelity:             "full",
		TargetFidelity:       "detailed",
		ArchiveRef:           "Archive/Knowledge/old.md",
		Importance:           "significant",
		Facet:                "expertise",
		ObservationCount:     3,
		Profile:              true,
		Evidence:             []string{"[[Buffer/e1]]"},
		ContributingSessions: []string{"sess-aaa", "sess-bbb"},
		SourceDocument:       "Windows Internals 7th Ed, Ch. 3",
		SourceVersion:        "Windows 11 24H2",
		Domain:               "windows-internals",
		Verified:             true,
		Body:                 "Body text describing the process structures.",
		FilePath:             filepath.Join(dir, "entry.md"),
	}

	if err := WriteMemoryEntry(want); err != nil {
		t.Fatalf("WriteMemoryEntry: %v", err)
	}
	got, err := ParseMemoryEntry(want.FilePath)
	if err != nil {
		t.Fatalf("ParseMemoryEntry: %v", err)
	}

	checks := []struct {
		name     string
		a, b     interface{}
	}{
		{"Type", want.Type, got.Type},
		{"Title", want.Title, got.Title},
		{"AccessCount", want.AccessCount, got.AccessCount},
		{"DecayRate", want.DecayRate, got.DecayRate},
		{"Confidence", want.Confidence, got.Confidence},
		{"SurpriseAtEncoding", want.SurpriseAtEncoding, got.SurpriseAtEncoding},
		{"ConsolidationSource", want.ConsolidationSource, got.ConsolidationSource},
		{"Tags", want.Tags, got.Tags},
		{"TrainingOverride", want.TrainingOverride, got.TrainingOverride},
		{"OverrideContext", want.OverrideContext, got.OverrideContext},
		{"SourceAuthority", want.SourceAuthority, got.SourceAuthority},
		{"ValidatedVia", want.ValidatedVia, got.ValidatedVia},
		{"Fidelity", want.Fidelity, got.Fidelity},
		{"TargetFidelity", want.TargetFidelity, got.TargetFidelity},
		{"ArchiveRef", want.ArchiveRef, got.ArchiveRef},
		{"Importance", want.Importance, got.Importance},
		{"Facet", want.Facet, got.Facet},
		{"ObservationCount", want.ObservationCount, got.ObservationCount},
		{"Profile", want.Profile, got.Profile},
		{"Evidence", want.Evidence, got.Evidence},
		{"ContributingSessions", want.ContributingSessions, got.ContributingSessions},
		{"SourceDocument", want.SourceDocument, got.SourceDocument},
		{"SourceVersion", want.SourceVersion, got.SourceVersion},
		{"Domain", want.Domain, got.Domain},
		{"Verified", want.Verified, got.Verified},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.a, c.b) {
			t.Errorf("%s lost in round trip: want %v, got %v", c.name, c.a, c.b)
		}
	}

	if len(got.Links) != 1 || normalizeLinkTarget(got.Links[0].Target) != "memory/knowledge/ntdll_structures" {
		t.Errorf("Links lost in round trip: %+v", got.Links)
	}
	if got.Created.UTC() != created || got.LastAccessed.UTC() != accessed {
		t.Errorf("timestamps lost: created=%v accessed=%v", got.Created, got.LastAccessed)
	}
}

// TestWriteParseRoundTrip_KnowledgeProvenanceMinimal isolates the highest-impact
// case: a knowledge entry must not lose source_document/source_version on the
// retrieval-rewrite path.
func TestWriteParseRoundTrip_KnowledgeProvenanceMinimal(t *testing.T) {
	dir := t.TempDir()
	k := &MemoryEntry{
		Type:           TypeKnowledge,
		Title:          "GKE Ch.4",
		SourceDocument: "A Guide to Kernel Exploitation, Ch. 4",
		SourceVersion:  "2011 ed.",
		Domain:         "offensive-security",
		Verified:       true,
		Body:           "x",
		FilePath:       filepath.Join(dir, "k.md"),
	}
	if err := WriteMemoryEntry(k); err != nil {
		t.Fatal(err)
	}
	got, err := ParseMemoryEntry(k.FilePath)
	if err != nil {
		t.Fatal(err)
	}
	if got.SourceDocument != k.SourceDocument || got.SourceVersion != k.SourceVersion ||
		got.Domain != k.Domain || !got.Verified {
		t.Fatalf("knowledge provenance lost: %q/%q/%q/%v", got.SourceDocument, got.SourceVersion, got.Domain, got.Verified)
	}
}
