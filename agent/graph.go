package main

import (
	"math"
	"path/filepath"
	"strings"
)

// MemoryGraph represents the associative map as an adjacency list.
type MemoryGraph struct {
	// Adjacency: normalized memory key → list of edges
	Edges map[string][]Edge
	// Memory lookup by normalized key
	Index map[string]*MemoryEntry
}

// Edge represents a typed connection between two memories.
type Edge struct {
	Source       string
	Target       string
	Relationship string
	Weight       float64
}

// BuildGraph constructs the associative map from all loaded memories.
func BuildGraph(memories []*MemoryEntry, cfg Config) *MemoryGraph {
	g := &MemoryGraph{
		Edges: make(map[string][]Edge),
		Index: make(map[string]*MemoryEntry),
	}

	for _, m := range memories {
		key := normalizeKey(m)
		g.Index[key] = m

		for _, link := range m.Links {
			targetKey := normalizeLinkTarget(link.Target)
			weight := cfg.EdgeWeights[link.Relationship]
			if weight == 0 {
				weight = 0.5 // default for unknown relationship types
			}

			edge := Edge{
				Source:       key,
				Target:       targetKey,
				Relationship: link.Relationship,
				Weight:       weight,
			}
			g.Edges[key] = append(g.Edges[key], edge)

			// Bidirectional for most relationship types
			if link.Relationship != "supersedes" {
				reverse := Edge{
					Source:       targetKey,
					Target:       key,
					Relationship: link.Relationship,
					Weight:       weight,
				}
				g.Edges[targetKey] = append(g.Edges[targetKey], reverse)
			}
		}
	}

	return g
}

// ApplySpreadingActivation boosts neighbors of already-activated memories.
// One hop only — no transitive spreading.
//
// Applies ACT-R-inspired fan discount: a hub memory with many connections
// spreads less activation per edge than a memory with few connections.
// Pure ACT-R defines strength as S − ln(fan); on linear scale that is
// roughly 1/fan. LJM defaults to a gentler log-based discount
// (1 / (1 + ln(fan))) to avoid silencing intentional hub memories.
// Controlled by cfg.FanDiscountFormula: "log" | "sqrt" | "linear" | "none".
func ApplySpreadingActivation(scored []ScoredMemory, graph *MemoryGraph, cfg Config) []ScoredMemory {
	// Collect activation values for memories above threshold
	activations := make(map[string]float64)
	for _, s := range scored {
		key := normalizeKey(s.Memory)
		if s.Total >= cfg.RetrievalThreshold {
			activations[key] = s.Activation
		}
	}

	// Compute boosts for neighbors
	boosts := make(map[string]float64)
	for sourceKey, sourceActivation := range activations {
		edges := graph.Edges[sourceKey]
		fan := len(edges)
		discount := fanDiscount(fan, cfg.FanDiscountFormula)
		for _, edge := range edges {
			// neighbor_boost = activation(source) × edge_weight × spreading_factor × fan_discount
			boost := sourceActivation * edge.Weight * cfg.SpreadingActivationFactor * discount
			if boost > boosts[edge.Target] {
				boosts[edge.Target] = boost // take max boost, not cumulative
			}
		}
	}

	// Apply boosts to scored memories
	for i := range scored {
		key := normalizeKey(scored[i].Memory)
		if b, ok := boosts[key]; ok {
			scored[i].Boost = b
			scored[i].Total += b
		}
	}

	// Re-sort after boost application
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].Total > scored[i].Total {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	return scored
}

// Neighbors returns all direct neighbors of a memory in the graph.
func (g *MemoryGraph) Neighbors(key string) []Edge {
	return g.Edges[key]
}

// fanDiscount scales the spreading-activation boost by the source's fan
// (number of connections) so hub memories don't dominate retrieval. All
// formulas return 1.0 for fan <= 1 (a memory with one connection is not
// a hub). Formula options, from gentlest to most aggressive:
//
//   log    (default): 1 / (1 + ln(fan))     — gentle, preserves hub utility
//   sqrt           : 1 / sqrt(fan)         — moderate
//   linear         : 1 / fan               — pure ACT-R on linear scale
//   none           : 1                     — disables fan effect
func fanDiscount(fan int, formula string) float64 {
	if fan <= 1 {
		return 1.0
	}
	switch formula {
	case "none":
		return 1.0
	case "sqrt":
		return 1.0 / math.Sqrt(float64(fan))
	case "linear":
		return 1.0 / float64(fan)
	case "log", "":
		return 1.0 / (1.0 + math.Log(float64(fan)))
	default:
		// Unknown formula → fail safe to log-based
		return 1.0 / (1.0 + math.Log(float64(fan)))
	}
}

// FindClusters identifies groups of tightly connected memories (3+ mutual connections).
func (g *MemoryGraph) FindClusters() [][]string {
	visited := make(map[string]bool)
	var clusters [][]string

	for key := range g.Index {
		if visited[key] {
			continue
		}

		// BFS to find connected component
		var cluster []string
		queue := []string{key}
		visited[key] = true

		for len(queue) > 0 {
			node := queue[0]
			queue = queue[1:]
			cluster = append(cluster, node)

			for _, edge := range g.Edges[node] {
				if !visited[edge.Target] {
					if _, exists := g.Index[edge.Target]; exists {
						visited[edge.Target] = true
						queue = append(queue, edge.Target)
					}
				}
			}
		}

		if len(cluster) >= 3 {
			clusters = append(clusters, cluster)
		}
	}

	return clusters
}

// normalizeKey creates a consistent lookup key from a memory's file path.
// Strips vault root and extension: "Memory/User/go_expertise.md" → "memory/user/go_expertise"
func normalizeKey(m *MemoryEntry) string {
	// Get relative path components
	parts := strings.Split(filepath.ToSlash(m.FilePath), "/")

	// Find "Memory" in path and take everything from there
	for i, p := range parts {
		if strings.EqualFold(p, "Memory") || strings.EqualFold(p, "Archive") {
			rel := strings.Join(parts[i:], "/")
			rel = strings.TrimSuffix(rel, ".md")
			return strings.ToLower(rel)
		}
	}

	// Fallback: just use filename without extension
	base := filepath.Base(m.FilePath)
	return strings.ToLower(strings.TrimSuffix(base, ".md"))
}

// normalizeLinkTarget normalizes a wiki-link target for graph lookup.
// "[[Memory/User/go_expertise]]" → "memory/user/go_expertise"
func normalizeLinkTarget(target string) string {
	target = strings.TrimPrefix(target, "[[")
	target = strings.TrimSuffix(target, "]]")
	target = strings.TrimSuffix(target, ".md")
	target = filepath.ToSlash(target)
	return strings.ToLower(target)
}
