package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteConsolidationOutcomes_FiltersToDaydreamOnly(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	assessments := []BufferAssessment{
		{
			Entry: &BufferEntry{
				FileName:  "dd1.md",
				FilePath:  "/x/dd1.md",
				Source:    "daydream",
				Timestamp: now.Add(-1 * time.Hour),
			},
			Action:         ActionPromote,
			Reason:         "promoted",
			RetentionScore: 0.7,
		},
		{
			Entry: &BufferEntry{
				FileName:  "conv1.md",
				FilePath:  "/x/conv1.md",
				Source:    "conversation",
				Timestamp: now.Add(-1 * time.Hour),
			},
			Action: ActionPromote,
		},
	}
	if err := WriteConsolidationOutcomes(vault, assessments, now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(vault, "Metrics", "consolidation_outcomes.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	count := 0
	for scanner.Scan() {
		count++
		var rec ConsolidationOutcome
		if err := json.Unmarshal(scanner.Bytes(), &rec); err != nil {
			t.Fatal(err)
		}
		if rec.SourceEntry == "conv1.md" {
			t.Errorf("non-daydream entry %q should not be in outcomes", rec.SourceEntry)
		}
	}
	if count != 1 {
		t.Errorf("got %d records, want 1 (only daydream-sourced)", count)
	}
}

func TestWriteConsolidationOutcomes_AttributionDegradedAfter12h(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	assessments := []BufferAssessment{
		{
			Entry: &BufferEntry{
				FileName:  "fresh.md",
				FilePath:  "/x/fresh.md",
				Source:    "daydream",
				Timestamp: now.Add(-2 * time.Hour),
			},
			Action: ActionPromote,
		},
		{
			Entry: &BufferEntry{
				FileName:  "stale.md",
				FilePath:  "/x/stale.md",
				Source:    "daydream",
				Timestamp: now.Add(-24 * time.Hour),
			},
			Action: ActionHold,
		},
	}
	if err := WriteConsolidationOutcomes(vault, assessments, now); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(vault, "Metrics", "consolidation_outcomes.jsonl"))

	records := []ConsolidationOutcome{}
	for _, line := range strings.Split(strings.TrimRight(string(data), "\n"), "\n") {
		var rec ConsolidationOutcome
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatal(err)
		}
		records = append(records, rec)
	}
	if len(records) != 2 {
		t.Fatalf("got %d, want 2", len(records))
	}

	var fresh, stale ConsolidationOutcome
	for _, r := range records {
		if r.SourceEntry == "fresh.md" {
			fresh = r
		}
		if r.SourceEntry == "stale.md" {
			stale = r
		}
	}
	if fresh.AttributionDegraded {
		t.Error("2h-old entry should not have AttributionDegraded=true")
	}
	if !stale.AttributionDegraded {
		t.Error("24h-old entry should have AttributionDegraded=true")
	}
	if stale.HoursSinceFire < 23 || stale.HoursSinceFire > 25 {
		t.Errorf("stale.HoursSinceFire = %v, want ~24", stale.HoursSinceFire)
	}
}

func TestWriteConsolidationOutcomes_PreservesValueVerdict(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	assessments := []BufferAssessment{
		{
			Entry: &BufferEntry{
				FileName:  "v.md",
				FilePath:  "/x/v.md",
				Source:    "daydream",
				Timestamp: now.Add(-1 * time.Hour),
			},
			Action:               ActionPromote,
			RetentionScore:       0.65,
			DaydreamValueVerdict: ValueValuable,
			DaydreamValueReason:  "concrete cross-domain finding",
			Redundancy:           0.4,
			DaydreamVerdict:      "novel",
		},
	}
	if err := WriteConsolidationOutcomes(vault, assessments, now); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(vault, "Metrics", "consolidation_outcomes.jsonl"))
	var rec ConsolidationOutcome
	if err := json.Unmarshal([]byte(strings.TrimRight(string(data), "\n")), &rec); err != nil {
		t.Fatal(err)
	}
	if rec.ValueVerdict != string(ValueValuable) {
		t.Errorf("ValueVerdict = %q, want %q", rec.ValueVerdict, ValueValuable)
	}
	if rec.RedundancyVerdict != "novel" {
		t.Errorf("RedundancyVerdict = %q, want novel", rec.RedundancyVerdict)
	}
	if rec.RetentionScore != 0.65 {
		t.Errorf("RetentionScore = %v, want 0.65", rec.RetentionScore)
	}
}

func TestWriteConsolidationOutcomes_EmptyAssessmentsNoFile(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	if err := WriteConsolidationOutcomes(vault, nil, now); err != nil {
		t.Fatal(err)
	}
	// File creation is OK even with no records — empty file is valid JSONL.
	// Just verify no crash and no records.
	path := filepath.Join(vault, "Metrics", "consolidation_outcomes.jsonl")
	data, _ := os.ReadFile(path)
	if len(strings.TrimSpace(string(data))) != 0 {
		t.Errorf("expected empty file, got %q", data)
	}
}

func TestWriteConsolidationOutcomes_NilEntryIgnored(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	assessments := []BufferAssessment{
		{Entry: nil, Action: ActionPromote},
		{Entry: &BufferEntry{FileName: "x.md", Source: "daydream", Timestamp: now}, Action: ActionPromote},
	}
	if err := WriteConsolidationOutcomes(vault, assessments, now); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(filepath.Join(vault, "Metrics", "consolidation_outcomes.jsonl"))
	if !strings.Contains(string(data), "x.md") {
		t.Error("daydream entry should be present")
	}
}
