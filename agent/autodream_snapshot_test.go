package main

import (
	"bufio"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCaptureActivationSnapshot_EmptyInput(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	snap := CaptureActivationSnapshot(nil, now)
	if snap.TotalMemories != 0 {
		t.Errorf("TotalMemories=%d, want 0", snap.TotalMemories)
	}
	if snap.Stats.NonKnowledgeCount != 0 {
		t.Errorf("NonKnowledgeCount=%d, want 0", snap.Stats.NonKnowledgeCount)
	}
	if len(snap.TopN) != 0 {
		t.Errorf("TopN len=%d, want 0", len(snap.TopN))
	}
	if !snap.Timestamp.Equal(now) {
		t.Errorf("Timestamp=%v, want %v", snap.Timestamp, now)
	}
}

func TestCaptureActivationSnapshot_ExcludesKnowledgeFromStats(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	memories := []*MemoryEntry{
		{Title: "u1", Type: TypeUser, AccessCount: 5, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Title: "k1", Type: TypeKnowledge, AccessCount: 3, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Title: "k2", Type: TypeKnowledge, AccessCount: 3, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
	}
	snap := CaptureActivationSnapshot(memories, now)
	if snap.TotalMemories != 3 {
		t.Errorf("TotalMemories=%d, want 3", snap.TotalMemories)
	}
	if snap.ByType["knowledge"] != 2 {
		t.Errorf("ByType[knowledge]=%d, want 2", snap.ByType["knowledge"])
	}
	if snap.ByType["user"] != 1 {
		t.Errorf("ByType[user]=%d, want 1", snap.ByType["user"])
	}
	if snap.Stats.NonKnowledgeCount != 1 {
		t.Errorf("NonKnowledgeCount=%d, want 1 (knowledge excluded from stats)", snap.Stats.NonKnowledgeCount)
	}
	for _, mn := range snap.TopN {
		if mn.Type == "knowledge" {
			t.Errorf("knowledge entry %q leaked into TopN", mn.Title)
		}
	}
}

func TestCaptureActivationSnapshot_ByTypeCountsAllKinds(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	memories := []*MemoryEntry{
		{Title: "u1", Type: TypeUser, AccessCount: 1, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Title: "u2", Type: TypeUser, AccessCount: 1, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Title: "f1", Type: TypeFeedback, AccessCount: 1, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Title: "p1", Type: TypeProject, AccessCount: 1, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Title: "s1", Type: TypeSemantic, AccessCount: 1, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
	}
	snap := CaptureActivationSnapshot(memories, now)
	wants := map[string]int{"user": 2, "feedback": 1, "project": 1, "semantic": 1}
	for tp, want := range wants {
		if got := snap.ByType[tp]; got != want {
			t.Errorf("ByType[%q]=%d, want %d", tp, got, want)
		}
	}
}

func TestCaptureActivationSnapshot_TopNIsSortedAndCapped(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	// 25 memories with descending recency → descending activation
	memories := make([]*MemoryEntry, 25)
	for i := range memories {
		memories[i] = &MemoryEntry{
			Title:        "m" + string(rune('a'+i)),
			Type:         TypeSemantic,
			AccessCount:  1,
			LastAccessed: now.Add(-time.Duration(i+1) * time.Hour),
			DecayRate:    0.5,
		}
	}
	snap := CaptureActivationSnapshot(memories, now)
	if len(snap.TopN) != activationSnapshotTopN {
		t.Fatalf("TopN len=%d, want %d", len(snap.TopN), activationSnapshotTopN)
	}
	for i := 1; i < len(snap.TopN); i++ {
		if snap.TopN[i].Activation > snap.TopN[i-1].Activation {
			t.Errorf("TopN not sorted descending at idx %d: %f > %f",
				i, snap.TopN[i].Activation, snap.TopN[i-1].Activation)
		}
	}
}

func TestCaptureActivationSnapshot_PercentileMonotonic(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	memories := make([]*MemoryEntry, 100)
	for i := range memories {
		memories[i] = &MemoryEntry{
			Title:        "m",
			Type:         TypeSemantic,
			AccessCount:  i + 1,
			LastAccessed: now.Add(-1 * time.Hour),
			DecayRate:    0.5,
		}
	}
	snap := CaptureActivationSnapshot(memories, now)
	s := snap.Stats
	if !(s.Min <= s.Median && s.Median <= s.P95 && s.P95 <= s.P99 && s.P99 <= s.Max) {
		t.Errorf("percentiles not monotonic: min=%f med=%f p95=%f p99=%f max=%f",
			s.Min, s.Median, s.P95, s.P99, s.Max)
	}
}

func TestCaptureActivationSnapshot_MeanInRange(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	memories := []*MemoryEntry{
		{Type: TypeUser, AccessCount: 10, LastAccessed: now.Add(-1 * time.Hour), DecayRate: 0.5},
		{Type: TypeUser, AccessCount: 10, LastAccessed: now.Add(-10 * time.Hour), DecayRate: 0.5},
		{Type: TypeUser, AccessCount: 10, LastAccessed: now.Add(-100 * time.Hour), DecayRate: 0.5},
	}
	snap := CaptureActivationSnapshot(memories, now)
	if math.IsNaN(snap.Stats.Mean) || math.IsInf(snap.Stats.Mean, 0) {
		t.Errorf("Mean is non-finite: %v", snap.Stats.Mean)
	}
	if !(snap.Stats.Min <= snap.Stats.Mean && snap.Stats.Mean <= snap.Stats.Max) {
		t.Errorf("Mean=%f not in [Min=%f, Max=%f]", snap.Stats.Mean, snap.Stats.Min, snap.Stats.Max)
	}
}

func TestPercentile_NearestRank(t *testing.T) {
	sorted := []float64{1, 2, 3, 4, 5, 6, 7, 8, 9, 10}
	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 5},
		{0.95, 10},
		{0.99, 10},
		{0.10, 1},
	}
	for _, c := range cases {
		got := percentile(sorted, c.p)
		if got != c.want {
			t.Errorf("percentile(p=%.2f)=%f, want %f", c.p, got, c.want)
		}
	}
}

func TestPercentile_EmptyReturnsZero(t *testing.T) {
	if got := percentile(nil, 0.5); got != 0 {
		t.Errorf("percentile(nil)=%f, want 0", got)
	}
}

func TestWriteActivationSnapshot_AppendsJSONL(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	snap := ActivationSnapshot{
		Timestamp:     now,
		TotalMemories: 5,
		ByType:        map[string]int{"user": 2, "semantic": 3},
		Stats:         ActivationStats{NonKnowledgeCount: 5, Mean: 1.5, Median: 1.4, P95: 2.5, P99: 2.7, Max: 3.0, Min: 0.1},
		TopN:          []MemoryActivation{{Title: "x", Type: "user", Activation: 3.0}},
	}
	if err := WriteActivationSnapshot(vault, snap, 1000); err != nil {
		t.Fatalf("write: %v", err)
	}

	path := filepath.Join(vault, "Metrics", "autodream_activation_snapshots.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.HasSuffix(string(data), "\n") {
		t.Error("file should end with newline")
	}
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Scan()
	var got ActivationSnapshot
	if err := json.Unmarshal(scanner.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.TotalMemories != 5 {
		t.Errorf("TotalMemories=%d, want 5", got.TotalMemories)
	}
	if got.ByType["semantic"] != 3 {
		t.Errorf("ByType[semantic]=%d, want 3", got.ByType["semantic"])
	}
	if !got.Timestamp.Equal(now) {
		t.Errorf("Timestamp=%v, want %v", got.Timestamp, now)
	}
}

func TestWriteActivationSnapshot_AppendsToExisting(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	for i := 0; i < 3; i++ {
		snap := ActivationSnapshot{
			Timestamp:     now.Add(time.Duration(i) * time.Minute),
			TotalMemories: i + 1,
			ByType:        map[string]int{},
		}
		if err := WriteActivationSnapshot(vault, snap, 0); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}

	path := filepath.Join(vault, "Metrics", "autodream_activation_snapshots.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) != 3 {
		t.Errorf("got %d lines, want 3", len(lines))
	}
}

func TestLoadAndCaptureSnapshot_NoMemoryDir(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)

	snap, _ := LoadAndCaptureSnapshot(vault, now)
	// Errors are non-fatal — snapshot may still be empty/zero. Just verify
	// timestamp is set so downstream join still works.
	if !snap.Timestamp.Equal(now) {
		t.Errorf("Timestamp=%v, want %v", snap.Timestamp, now)
	}
}
