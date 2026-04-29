package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseBufferEntry_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_entry.md")

	original := &BufferEntry{
		Type:             TypeBuffer,
		Timestamp:        time.Date(2026, 4, 6, 14, 30, 0, 0, time.FixedZone("EDT", -4*3600)),
		Source:           "conversation",
		Surprise:         0.7,
		ContextIntegrity: ContextFull,
		Tags:             []string{"go", "preference"},
		Related:          []string{},
		Body:             "User prefers Go for offensive tooling.",
		FilePath:         path,
	}

	if err := WriteBufferEntry(original); err != nil {
		t.Fatalf("WriteBufferEntry: %v", err)
	}

	parsed, err := ParseBufferEntry(path)
	if err != nil {
		t.Fatalf("ParseBufferEntry: %v", err)
	}

	if parsed.Surprise != 0.7 {
		t.Errorf("surprise = %.1f, expected 0.7", parsed.Surprise)
	}
	if parsed.ContextIntegrity != ContextFull {
		t.Errorf("context_integrity = %s, expected full", parsed.ContextIntegrity)
	}
	if len(parsed.Tags) != 2 {
		t.Errorf("tags count = %d, expected 2", len(parsed.Tags))
	}
	if !strings.Contains(parsed.Body, "Go for offensive tooling") {
		t.Errorf("body not preserved: %s", parsed.Body)
	}
}

func TestParseMemoryEntry_Roundtrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "test_memory.md")

	original := &MemoryEntry{
		Type:               TypeUser,
		Title:              "Go expertise",
		Created:            time.Date(2026, 3, 15, 10, 0, 0, 0, time.UTC),
		LastAccessed:       time.Date(2026, 4, 2, 14, 0, 0, 0, time.UTC),
		AccessCount:        7,
		DecayRate:          0.3,
		Confidence:         0.95,
		SurpriseAtEncoding: 0.2,
		Tags:               []string{"go", "tooling"},
		Links: []Link{
			{Target: "Memory/User/security", Relationship: "related-to"},
		},
		Body:     "User's primary language is Go.",
		FilePath: path,
	}

	if err := WriteMemoryEntry(original); err != nil {
		t.Fatalf("WriteMemoryEntry: %v", err)
	}

	parsed, err := ParseMemoryEntry(path)
	if err != nil {
		t.Fatalf("ParseMemoryEntry: %v", err)
	}

	if parsed.Title != "Go expertise" {
		t.Errorf("title = %s, expected 'Go expertise'", parsed.Title)
	}
	if parsed.AccessCount != 7 {
		t.Errorf("access_count = %d, expected 7", parsed.AccessCount)
	}
	if parsed.DecayRate != 0.3 {
		t.Errorf("decay_rate = %.1f, expected 0.3", parsed.DecayRate)
	}
	if parsed.Confidence != 0.95 {
		t.Errorf("confidence = %.2f, expected 0.95", parsed.Confidence)
	}
	if len(parsed.Links) != 1 {
		t.Errorf("links count = %d, expected 1", len(parsed.Links))
	}
	if len(parsed.Links) > 0 && parsed.Links[0].Relationship != "related-to" {
		t.Errorf("link relationship = %s, expected related-to", parsed.Links[0].Relationship)
	}
}

func TestParseMemoryEntry_TrainingOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "override.md")

	original := &MemoryEntry{
		Type:               TypeFeedback,
		Title:              "Correct understanding of X",
		Created:            time.Now(),
		LastAccessed:       time.Now(),
		AccessCount:        1,
		DecayRate:          0.1,
		Confidence:         0.9,
		SurpriseAtEncoding: 0.9,
		Tags:               []string{"correction"},
		TrainingOverride:   true,
		OverrideContext:    "Model says X; user confirmed Y",
		SourceAuthority:    "user-confirmed-with-evidence",
		ValidatedVia:       []string{"direct demonstration"},
		Body:               "The correct understanding is Y, not X.",
		FilePath:           path,
	}

	if err := WriteMemoryEntry(original); err != nil {
		t.Fatalf("WriteMemoryEntry: %v", err)
	}

	parsed, err := ParseMemoryEntry(path)
	if err != nil {
		t.Fatalf("ParseMemoryEntry: %v", err)
	}

	if !parsed.TrainingOverride {
		t.Error("training_override should be true")
	}
	if parsed.SourceAuthority != "user-confirmed-with-evidence" {
		t.Errorf("source_authority = %s, expected user-confirmed-with-evidence", parsed.SourceAuthority)
	}
	if parsed.DecayRate != 0.1 {
		t.Errorf("decay_rate = %.1f, expected 0.1 (training override rate)", parsed.DecayRate)
	}
}

func TestParseMemoryEntry_UserFacet(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cognition.md")

	original := &MemoryEntry{
		Type:               TypeUser,
		Title:              "Systematic debugger",
		Created:            time.Now(),
		LastAccessed:       time.Now(),
		AccessCount:        1,
		DecayRate:          0.2,
		Confidence:         0.6,
		SurpriseAtEncoding: 0.3,
		Tags:               []string{"cognition", "debugging"},
		Facet:              "cognition",
		ObservationCount:   2,
		Body:               "User approaches debugging systematically.",
		FilePath:           path,
	}

	if err := WriteMemoryEntry(original); err != nil {
		t.Fatalf("WriteMemoryEntry: %v", err)
	}

	parsed, err := ParseMemoryEntry(path)
	if err != nil {
		t.Fatalf("ParseMemoryEntry: %v", err)
	}

	if parsed.Facet != "cognition" {
		t.Errorf("facet = %s, expected cognition", parsed.Facet)
	}
	if parsed.ObservationCount != 2 {
		t.Errorf("observation_count = %d, expected 2", parsed.ObservationCount)
	}
}

func TestLoadAllBufferEntries_EmptyDir(t *testing.T) {
	dir := t.TempDir()
	bufferDir := filepath.Join(dir, "Buffer")
	os.MkdirAll(bufferDir, 0755)

	entries, err := LoadAllBufferEntries(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("expected 0 entries, got %d", len(entries))
	}
}

func TestSplitFrontmatter(t *testing.T) {
	input := "---\ntype: buffer\nsurprise: 0.5\n---\nBody content here."

	yaml, body, err := splitFrontmatter([]byte(input))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !strings.Contains(yaml, "type: buffer") {
		t.Errorf("yaml should contain 'type: buffer': %s", yaml)
	}
	if !strings.Contains(body, "Body content") {
		t.Errorf("body should contain 'Body content': %s", body)
	}
}

func TestSplitFrontmatter_NoFrontmatter(t *testing.T) {
	input := "Just plain text, no frontmatter."

	_, _, err := splitFrontmatter([]byte(input))
	if err == nil {
		t.Error("expected error for missing frontmatter")
	}
}

func TestParseStringList_Inline(t *testing.T) {
	result := parseStringList("[go, tooling, preference]")
	if len(result) != 3 {
		t.Fatalf("expected 3 items, got %d: %v", len(result), result)
	}
	if result[0] != "go" || result[1] != "tooling" || result[2] != "preference" {
		t.Errorf("unexpected items: %v", result)
	}
}

func TestParseStringList_Empty(t *testing.T) {
	result := parseStringList("[]")
	if len(result) != 0 {
		t.Errorf("expected 0 items, got %d", len(result))
	}
}
