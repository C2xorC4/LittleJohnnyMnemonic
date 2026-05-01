package main

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// ActivationSnapshot is a point-in-time capture of memory activation state,
// written before each fired or dry-run autodream invocation so per-run
// activation inflation is attributable.
//
// Joining to autodream_log: snapshots and log entries share Timestamp, so
// the two streams join 1-to-1 on equality. The snapshot is captured BEFORE
// the run touches anything; the run's effect on activation is implicit in
// the next snapshot.
type ActivationSnapshot struct {
	Timestamp     time.Time          `json:"timestamp"`
	TotalMemories int                `json:"total_memories"`
	ByType        map[string]int     `json:"by_type"`
	Stats         ActivationStats    `json:"stats"`
	TopN          []MemoryActivation `json:"top_n"`
}

// ActivationStats summarizes the activation distribution across the
// non-knowledge memory population. Knowledge entries are excluded — their
// activation is fixed at 1.0 (no time-based decay) so they would dominate
// the high-activation tail without telling us anything about decay drift.
type ActivationStats struct {
	NonKnowledgeCount int     `json:"non_knowledge_count"`
	Mean              float64 `json:"mean"`
	Median            float64 `json:"median"`
	P95               float64 `json:"p95"`
	P99               float64 `json:"p99"`
	Max               float64 `json:"max"`
	Min               float64 `json:"min"`
}

// MemoryActivation is the per-memory record in TopN. Title is the
// human-readable name from frontmatter; Path is the absolute file path so
// later analysis can re-load the entry. AccessCount and LastAccessed are
// the inputs that drove the activation, captured here so the value can be
// recomputed later if scoring changes.
type MemoryActivation struct {
	Title        string    `json:"title"`
	Type         string    `json:"type"`
	Path         string    `json:"path"`
	Activation   float64   `json:"activation"`
	AccessCount  int       `json:"access_count"`
	LastAccessed time.Time `json:"last_accessed"`
}

// activationSnapshotTopN is how many highest-activation memories we record
// per snapshot. Twenty is enough to characterize the right tail of the
// distribution without bloating the JSONL — at ~200 memories, this is a
// 10% sample weighted toward what's currently most retrievable.
const activationSnapshotTopN = 20

// CaptureActivationSnapshot computes a snapshot from already-loaded memories.
// Pure: no I/O, no global state. The caller is responsible for calling
// LoadAllMemories beforehand, which by contract MUST be read-only — if
// memory loading is ever extended to update last_accessed or access_count,
// the snapshot becomes mid-run rather than pre-run and silently shifts
// from baseline to contamination.
func CaptureActivationSnapshot(memories []*MemoryEntry, now time.Time) ActivationSnapshot {
	snap := ActivationSnapshot{
		Timestamp:     now,
		TotalMemories: len(memories),
		ByType:        make(map[string]int),
	}

	activations := make([]float64, 0, len(memories))
	all := make([]MemoryActivation, 0, len(memories))

	for _, m := range memories {
		typeStr := string(m.Type)
		snap.ByType[typeStr]++

		// Skip knowledge entries from the activation distribution — they
		// don't decay, so their activation is fixed at 1.0 and would skew
		// the right tail meaninglessly.
		if m.Type == TypeKnowledge {
			continue
		}

		a := ComputeActivation(m, now)
		activations = append(activations, a)
		all = append(all, MemoryActivation{
			Title:        m.Title,
			Type:         typeStr,
			Path:         m.FilePath,
			Activation:   a,
			AccessCount:  m.AccessCount,
			LastAccessed: m.LastAccessed,
		})
	}

	snap.Stats = computeActivationStats(activations)
	snap.TopN = topNByActivation(all, activationSnapshotTopN)
	return snap
}

func computeActivationStats(values []float64) ActivationStats {
	stats := ActivationStats{NonKnowledgeCount: len(values)}
	if len(values) == 0 {
		return stats
	}

	sorted := make([]float64, len(values))
	copy(sorted, values)
	sort.Float64s(sorted)

	stats.Min = sorted[0]
	stats.Max = sorted[len(sorted)-1]
	stats.Median = percentile(sorted, 0.50)
	stats.P95 = percentile(sorted, 0.95)
	stats.P99 = percentile(sorted, 0.99)

	sum := 0.0
	for _, v := range values {
		sum += v
	}
	stats.Mean = sum / float64(len(values))
	return stats
}

// percentile uses nearest-rank: idx = ceil(p * n) - 1, clamped to [0, n-1].
// Simple and stable; we don't need interpolation accuracy for distribution
// monitoring at N≈200.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return sorted[idx]
}

func topNByActivation(all []MemoryActivation, n int) []MemoryActivation {
	if len(all) == 0 {
		return nil
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].Activation > all[j].Activation
	})
	if len(all) < n {
		n = len(all)
	}
	out := make([]MemoryActivation, n)
	copy(out, all[:n])
	return out
}

// WriteActivationSnapshot appends a single JSONL record to
// Metrics/autodream_activation_snapshots.jsonl. Failures return to the
// caller for stderr logging — losing one snapshot shouldn't stop the
// autodream run from completing.
//
// Honors AutoDaydreamLogRotationThreshold for rotation, sharing the same
// rotation policy as autodream_log/replay_log.
func WriteActivationSnapshot(vaultRoot string, snap ActivationSnapshot, threshold int) error {
	dir := filepath.Join(vaultRoot, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir Metrics: %w", err)
	}
	path := filepath.Join(dir, "autodream_activation_snapshots.jsonl")

	if threshold > 0 {
		if err := rotateJSONLIfNeeded(path, threshold, snap.Timestamp); err != nil {
			fmt.Fprintf(os.Stderr, "[autodream] snapshot rotation: %v\n", err)
		}
	}

	line, err := json.Marshal(snap)
	if err != nil {
		return fmt.Errorf("marshal snapshot: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open snapshot file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write snapshot: %w", err)
	}
	return nil
}

// LoadAndCaptureSnapshot is the production convenience: load all memories
// from the vault, compute the snapshot, return it. The caller writes it.
// Loading errors are non-fatal — return whatever loaded plus the error so
// the snapshot is still useful for partial state.
func LoadAndCaptureSnapshot(vaultRoot string, now time.Time) (ActivationSnapshot, error) {
	memories, err := LoadAllMemories(vaultRoot)
	snap := CaptureActivationSnapshot(memories, now)
	return snap, err
}
