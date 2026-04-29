package main

import (
	"math"
	"strings"
	"time"
)

// ComputeActivation calculates the ACT-R base-level activation for a memory.
//
//	B_i = ln(access_count × recency^(-d))
//
// Where recency = hours since last access, d = decay rate.
func ComputeActivation(m *MemoryEntry, now time.Time) float64 {
	recencyHours := now.Sub(m.LastAccessed).Hours()
	if recencyHours < 0.01 {
		recencyHours = 0.01 // avoid log(0) for very recent memories
	}

	d := m.DecayRate
	if d <= 0 {
		d = 0.5
	}

	count := float64(m.AccessCount)
	if count < 1 {
		count = 1
	}

	// B_i = ln(n × t^(-d))
	activation := math.Log(count * math.Pow(recencyHours, -d))
	return activation
}

// ComputeRelevance estimates semantic relevance between a memory and query context.
// Uses tag matching as a heuristic proxy for embedding similarity.
func ComputeRelevance(m *MemoryEntry, contextTags []string) float64 {
	if len(contextTags) == 0 {
		return 0.5 // neutral when no context provided
	}

	matches := 0
	tagSet := make(map[string]bool)
	for _, t := range m.Tags {
		tagSet[strings.ToLower(t)] = true
	}
	for _, ct := range contextTags {
		if tagSet[strings.ToLower(ct)] {
			matches++
		}
	}

	// Each shared tag adds 0.2, capped at 1.0
	relevance := float64(matches) * 0.2
	if relevance > 1.0 {
		relevance = 1.0
	}
	return relevance
}

// ComputeTypeBoost adds a small boost when the memory type matches the query intent.
func ComputeTypeBoost(m *MemoryEntry, queryIntent string) float64 {
	switch queryIntent {
	case "guidance", "how":
		if m.Type == TypeFeedback {
			return 0.2
		}
	case "who", "user":
		if m.Type == TypeUser {
			return 0.2
		}
	case "what", "project":
		if m.Type == TypeProject {
			return 0.2
		}
	case "where", "reference":
		if m.Type == TypeReference {
			return 0.2
		}
	}
	return 0.0
}

// ComputeSurpriseBonus returns the persistent retrieval advantage from
// high-surprise encoding (Von Restorff / isolation effect).
func ComputeSurpriseBonus(m *MemoryEntry, cfg Config) float64 {
	return m.SurpriseAtEncoding * cfg.SurpriseBonusWeight
}

// ScoreMemory computes the full retrieval score for a single memory.
//
//	Standard:  score = activation × relevance × confidence + surprise_bonus
//	Knowledge: score = relevance × confidence + surprise_bonus (no time-based decay)
func ScoreMemory(m *MemoryEntry, contextTags []string, queryIntent string, cfg Config, now time.Time) ScoredMemory {
	relevance := ComputeRelevance(m, contextTags) + ComputeTypeBoost(m, queryIntent)
	if relevance > 1.0 {
		relevance = 1.0
	}
	confidence := m.Confidence
	surprise := ComputeSurpriseBonus(m, cfg)

	var activation, total float64

	if m.Type == TypeKnowledge {
		// Knowledge entries don't decay with time — scored purely on
		// relevance and confidence. Activation is fixed at 1.0.
		activation = 1.0
		total = relevance*confidence + surprise
	} else {
		activation = ComputeActivation(m, now)
		total = activation*relevance*confidence + surprise
	}

	return ScoredMemory{
		Memory:     m,
		Activation: activation,
		Relevance:  relevance,
		Confidence: confidence,
		Surprise:   surprise,
		Total:      total,
	}
}

// ScoreAllMemories scores every memory and returns them sorted by score descending.
func ScoreAllMemories(memories []*MemoryEntry, contextTags []string, queryIntent string, cfg Config, now time.Time) []ScoredMemory {
	scored := make([]ScoredMemory, 0, len(memories))
	for _, m := range memories {
		s := ScoreMemory(m, contextTags, queryIntent, cfg, now)
		scored = append(scored, s)
	}

	// Sort descending by total score
	for i := 0; i < len(scored); i++ {
		for j := i + 1; j < len(scored); j++ {
			if scored[j].Total > scored[i].Total {
				scored[i], scored[j] = scored[j], scored[i]
			}
		}
	}

	return scored
}
