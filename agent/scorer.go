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
		tagSet[Stem(strings.ToLower(t))] = true
	}
	for _, ct := range contextTags {
		if tagSet[Stem(strings.ToLower(ct))] {
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

// ActivationForType returns the effective activation for a memory, applying a
// per-type floor so durable types (user, feedback, semantic) don't become
// unretrievable during topic-dormant periods. Types representing ephemeral
// context (project, reference) have a floor of 0.0 — their full decay is
// intentional. The floor is configured via ActivationFloors in Config.
func ActivationForType(m *MemoryEntry, now time.Time, cfg Config) float64 {
	var raw float64
	if m.Type == TypeKnowledge {
		raw = 1.0
	} else {
		raw = squashActivation(ComputeActivation(m, now), cfg.MaxActivation)
	}
	floor := cfg.ActivationFloors[string(m.Type)]
	if raw < floor {
		return floor
	}
	return raw
}

// squashActivation soft-bounds base-level activation with a smooth, monotonic
// transform — M·tanh(raw/M) — so it asymptotes toward M without ever tying (unlike
// a hard cap). This keeps a single count-inflated memory (e.g. counts inflated by
// the historical access loop) from dominating the additive score: above the normal
// range activation compresses into the band where β·relevance can compete, while
// relative order among memories is preserved. max ≤ 0 disables the squash.
func squashActivation(raw, max float64) float64 {
	if max <= 0 {
		return raw
	}
	return max * math.Tanh(raw/max)
}

// ScoreMemory computes the full retrieval score for a single memory.
//
//	score = base-level activation + β·relevance·confidence + surprise_bonus
//
// Additive (not multiplicative) so relevance can steer ranking instead of being
// dominated by the unbounded activation term. Knowledge memories use a fixed
// base-level activation of 1.0 (no time-based decay).
func ScoreMemory(m *MemoryEntry, contextTags []string, queryIntent string, cfg Config, now time.Time) ScoredMemory {
	relevance := ComputeRelevance(m, contextTags) + ComputeTypeBoost(m, queryIntent)
	if relevance > 1.0 {
		relevance = 1.0
	}
	confidence := m.Confidence
	surprise := ComputeSurpriseBonus(m, cfg)

	activation := ActivationForType(m, now, cfg)
	total := activation + cfg.RelevanceWeight*relevance*confidence + surprise

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
