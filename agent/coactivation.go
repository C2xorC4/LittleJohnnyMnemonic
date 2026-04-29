package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

// CoactivationPair tracks how often two memories appear together in association results.
type CoactivationPair struct {
	MemoryA   string    `json:"memory_a"`
	MemoryB   string    `json:"memory_b"`
	Count     int       `json:"count"`
	LastSeen  time.Time `json:"last_seen"`
	Contexts  []string  `json:"contexts,omitempty"` // sample context strings that triggered co-activation
}

// CoactivationLog persists co-activation data for edge learning.
type CoactivationLog struct {
	Pairs   []CoactivationPair `json:"pairs"`
	Updated time.Time          `json:"updated"`
}

// coactivationPath returns the path to the co-activation log file.
func coactivationPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "coactivation.json")
}

// LoadCoactivation reads the co-activation log from disk.
func LoadCoactivation(vaultRoot string) (*CoactivationLog, error) {
	path := coactivationPath(vaultRoot)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &CoactivationLog{}, nil
		}
		return nil, err
	}

	var log CoactivationLog
	if err := json.Unmarshal(data, &log); err != nil {
		return nil, err
	}
	return &log, nil
}

// SaveCoactivation writes the co-activation log to disk.
func SaveCoactivation(vaultRoot string, log *CoactivationLog) error {
	path := coactivationPath(vaultRoot)

	// Ensure Metrics/ exists
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	log.Updated = time.Now()
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// RecordCoactivation updates the log with a new set of co-activated memories.
// Every pair in the result set gets its count incremented.
func RecordCoactivation(log *CoactivationLog, memoryKeys []string, context string, maxContexts int) {
	if len(memoryKeys) < 2 {
		return
	}

	now := time.Now()

	// Build index for fast lookup
	pairIndex := make(map[string]int) // "a|b" → index in Pairs
	for i, p := range log.Pairs {
		key := pairKey(p.MemoryA, p.MemoryB)
		pairIndex[key] = i
	}

	// Increment all pairs
	for i := 0; i < len(memoryKeys); i++ {
		for j := i + 1; j < len(memoryKeys); j++ {
			key := pairKey(memoryKeys[i], memoryKeys[j])
			if idx, exists := pairIndex[key]; exists {
				log.Pairs[idx].Count++
				log.Pairs[idx].LastSeen = now
				if len(log.Pairs[idx].Contexts) < maxContexts {
					log.Pairs[idx].Contexts = append(log.Pairs[idx].Contexts, truncate(context, 100))
				}
			} else {
				log.Pairs = append(log.Pairs, CoactivationPair{
					MemoryA:  memoryKeys[i],
					MemoryB:  memoryKeys[j],
					Count:    1,
					LastSeen: now,
					Contexts: []string{truncate(context, 100)},
				})
				pairIndex[key] = len(log.Pairs) - 1
			}
		}
	}
}

// FindLearnedEdgeCandidates returns pairs that have been co-activated enough
// times to warrant a graph edge, but don't already have one.
func FindLearnedEdgeCandidates(log *CoactivationLog, graph *MemoryGraph, threshold int) []CoactivationPair {
	var candidates []CoactivationPair
	for _, pair := range log.Pairs {
		if pair.Count < threshold {
			continue
		}

		// Check if edge already exists
		if hasEdge(graph, pair.MemoryA, pair.MemoryB) {
			continue
		}

		candidates = append(candidates, pair)
	}

	// Sort by count descending
	for i := 0; i < len(candidates); i++ {
		for j := i + 1; j < len(candidates); j++ {
			if candidates[j].Count > candidates[i].Count {
				candidates[i], candidates[j] = candidates[j], candidates[i]
			}
		}
	}

	return candidates
}

// hasEdge checks if any edge exists between two memories in the graph.
func hasEdge(graph *MemoryGraph, a, b string) bool {
	for _, edge := range graph.Edges[a] {
		if edge.Target == b {
			return true
		}
	}
	for _, edge := range graph.Edges[b] {
		if edge.Target == a {
			return true
		}
	}
	return false
}

// pairKey creates a canonical key for a memory pair (alphabetically ordered).
func pairKey(a, b string) string {
	if a > b {
		a, b = b, a
	}
	return a + "|" + b
}
