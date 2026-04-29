package main

import (
	"testing"
	"time"
)

func TestAssessBufferEntry_FullContext_HighSurprise(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	entry := &BufferEntry{
		Timestamp:        now.Add(-30 * time.Minute),
		Surprise:         0.8,
		ContextIntegrity: ContextFull,
		Tags:             []string{"novel-topic"},
		Body:             "User revealed a completely new preference for X over Y in context Z.",
	}

	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})

	if assessment.Action != ActionPromote {
		t.Errorf("high surprise + full context should promote, got %s (retention=%.3f)",
			assessment.Action, assessment.RetentionScore)
	}
	if assessment.ContextPenalty != 1.0 {
		t.Errorf("full context should have penalty=1.0, got %.1f", assessment.ContextPenalty)
	}
}

func TestAssessBufferEntry_OrphanAmbiguous_Discarded(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	entry := &BufferEntry{
		Timestamp:        now.Add(-48 * time.Hour),
		Surprise:         0.9,
		ContextIntegrity: ContextOrphan,
		Tags:             []string{"vague"},
		Body:             "User said to use the other approach.", // ambiguous signal
	}

	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})

	if assessment.Action != ActionDiscard {
		t.Errorf("orphan-ambiguous entry should be discarded, got %s", assessment.Action)
	}
	if assessment.Reason != "orphan-ambiguous: fails self-containment test" {
		t.Errorf("unexpected reason: %s", assessment.Reason)
	}
}

func TestAssessBufferEntry_OrphanClear_PenaltyApplied(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	entry := &BufferEntry{
		Timestamp:        now.Add(-2 * time.Hour),
		Surprise:         0.9,
		ContextIntegrity: ContextOrphan,
		Tags:             []string{"clear-observation"},
		Body:             "User corrected: when writing Go tests for the EOD toolkit, always use table-driven tests rather than individual functions. Reason: consistency with existing patterns.",
	}

	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})

	if assessment.ContextPenalty != cfg.ContextPenaltyOrphan {
		t.Errorf("orphan penalty should be %.1f, got %.1f", cfg.ContextPenaltyOrphan, assessment.ContextPenalty)
	}
}

func TestAssessBufferEntry_PartialContext_Penalty(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	entry := &BufferEntry{
		Timestamp:        now.Add(-1 * time.Hour),
		Surprise:         0.7,
		ContextIntegrity: ContextPartial,
		Tags:             []string{"some-topic"},
		Body:             "User explicitly stated preference for bundled PRs in infrastructure refactors.",
	}

	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})

	if assessment.ContextPenalty != cfg.ContextPenaltyPartial {
		t.Errorf("partial penalty should be %.1f, got %.1f", cfg.ContextPenaltyPartial, assessment.ContextPenalty)
	}

	// Retention should be lower than if context were full
	fullEntry := *entry
	fullEntry.ContextIntegrity = ContextFull
	fullAssessment := assessBufferEntry(&fullEntry, nil, cfg, now, time.Time{})

	if assessment.RetentionScore >= fullAssessment.RetentionScore {
		t.Errorf("partial context should have lower retention: partial=%.3f, full=%.3f",
			assessment.RetentionScore, fullAssessment.RetentionScore)
	}
}

func TestAssessBufferEntry_Pinned_AlwaysPromotes(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	entry := &BufferEntry{
		Timestamp:        now.Add(-168 * time.Hour), // a week old
		Surprise:         0.1,                        // low surprise
		ContextIntegrity: ContextOrphan,              // worst context
		Tags:             []string{"pinned-thing"},
		Pinned:           true,
		Body:             "Something the user explicitly pinned.",
	}

	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})

	if assessment.Action != ActionPromote {
		t.Errorf("pinned entries should always promote, got %s", assessment.Action)
	}
}

func TestAssessBufferEntry_ExceededMaxHolds_Discarded(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	entry := &BufferEntry{
		Timestamp:        now.Add(-1 * time.Hour),
		Surprise:         0.4, // moderate
		ContextIntegrity: ContextFull,
		Tags:             []string{"held-too-long"},
		HoldCount:        2, // at max
		Body:             "Some observation that kept getting held.",
	}

	assessment := assessBufferEntry(entry, nil, cfg, now, time.Time{})

	if assessment.Action != ActionDiscard {
		t.Errorf("entry at max holds should be discarded, got %s", assessment.Action)
	}
}

func TestAssessBufferEntry_Redundancy_LowersRetention(t *testing.T) {
	cfg := DefaultConfig()
	now := time.Now()

	existingMemories := []*MemoryEntry{
		{
			Title: "Existing Go preference",
			Type:  TypeUser,
			Tags:  []string{"go", "preference"},
		},
	}

	// Entry with overlapping tags → redundant
	redundantEntry := &BufferEntry{
		Timestamp:        now.Add(-30 * time.Minute),
		Surprise:         0.6,
		ContextIntegrity: ContextFull,
		Tags:             []string{"go", "preference"},
		Body:             "User again mentioned Go preference in a different context.",
	}

	// Entry with unique tags → novel
	novelEntry := &BufferEntry{
		Timestamp:        now.Add(-30 * time.Minute),
		Surprise:         0.6,
		ContextIntegrity: ContextFull,
		Tags:             []string{"python", "scripting"},
		Body:             "User mentioned using Python for quick prototyping scripts.",
	}

	redundantAssessment := assessBufferEntry(redundantEntry, existingMemories, cfg, now, time.Time{})
	novelAssessment := assessBufferEntry(novelEntry, existingMemories, cfg, now, time.Time{})

	if redundantAssessment.RetentionScore >= novelAssessment.RetentionScore {
		t.Errorf("redundant entry should score lower: redundant=%.3f, novel=%.3f",
			redundantAssessment.RetentionScore, novelAssessment.RetentionScore)
	}
}

func TestIsAmbiguous_Signals(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected bool
	}{
		{"clear observation", "User prefers Go for all offensive tooling due to cross-compilation and static binaries.", false},
		{"dangling 'the other'", "User said to use the other approach instead.", true},
		{"dangling 'that approach'", "Stick with that approach going forward.", true},
		{"dangling 'it worked'", "It worked, so we should keep doing that.", true},
		{"too short", "Use Python.", true},
		{"as discussed", "As discussed, the config should change.", true},
		{"clear correction", "User corrected: always use table-driven tests in Go test files for consistency.", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			entry := &BufferEntry{Body: tt.body}
			result := isAmbiguous(entry)
			if result != tt.expected {
				t.Errorf("isAmbiguous(%q) = %v, expected %v", tt.body, result, tt.expected)
			}
		})
	}
}

func TestTagOverlap(t *testing.T) {
	tests := []struct {
		a, b     []string
		expected float64
	}{
		{[]string{"go", "tooling"}, []string{"go", "tooling"}, 1.0},
		{[]string{"go", "tooling"}, []string{"go"}, 0.5},
		{[]string{"go"}, []string{"python"}, 0.0},
		{nil, []string{"go"}, 0.0},
		{[]string{"go"}, nil, 0.0},
	}

	for _, tt := range tests {
		result := tagOverlap(tt.a, tt.b)
		if result != tt.expected {
			t.Errorf("tagOverlap(%v, %v) = %.1f, expected %.1f", tt.a, tt.b, result, tt.expected)
		}
	}
}
