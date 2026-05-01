package main

import (
	"errors"
	"fmt"
	"math/rand/v2"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Seed is a single seeded starting point for an auto-daydream run. The
// orchestrator passes FilePath into the daydream agent's prompt so the agent
// reads it as the conversation's anchor.
type Seed struct {
	Source      string    // category that produced the seed: "buffer" | "knowledge" | "semantic" | …
	FilePath    string    // absolute path to the .md file
	Title       string    // for log lines and prompt construction
	LastTouched time.Time // mtime for buffer entries, last_accessed for memory entries
}

// SeedPair is the (recent, stable) pair used by interleaved replay daydreams.
// The Recent partner is current cognitive material being integrated; the
// Stable partner is a crystallized trait that the integration is being
// checked against (CLS-style — see Memory/Knowledge/cog_cls_*).
type SeedPair struct {
	Recent Seed
	Stable Seed
}

var (
	// ErrNoSeedAvailable indicates that none of the configured categories
	// yielded any usable entry (every category was empty or all weights
	// were zero).
	ErrNoSeedAvailable = errors.New("autodream: no seed available for the configured sources")

	// ErrNoPairAvailable indicates that an interleaved-replay pair could not
	// be built — typically because no recent material exists within the
	// configured age window. Triggers the strategy resolver's conditional
	// fallback to exploration.
	ErrNoPairAvailable = errors.New("autodream: no replay pair available — no recent material")
)

// catWeight is a tiny pair used internally by the weighted category pick.
// File-level so PickSeed and pickWeightedIndex share a concrete type.
type catWeight struct {
	name   string
	weight float64
}

// PickSeed selects a single seed using the configured category weighting.
// recencyBias=true narrows each category's pool to the upper half of entries
// sorted by LastTouched DESC (active mode). recencyBias=false samples
// uniformly across all entries (quiet exploration mode).
//
// If a weighted-chosen category yields zero candidates (e.g., the user has
// no Episodic entries yet), PickSeed falls through to the next-weighted
// category rather than aborting. This keeps a misweighted config from
// killing daydreams entirely as the vault grows in.
func PickSeed(vaultRoot string, sources map[string]float64, recencyBias bool, r *rand.Rand) (Seed, error) {
	var available []catWeight
	for name, weight := range sources {
		if weight > 0 {
			available = append(available, catWeight{name: name, weight: weight})
		}
	}
	if len(available) == 0 {
		return Seed{}, ErrNoSeedAvailable
	}
	// Deterministic ordering — Go map iteration is randomized; without this,
	// the cumulative-sum lookup below would behave non-deterministically even
	// with a seeded *rand.Rand.
	sort.Slice(available, func(i, j int) bool {
		return available[i].name < available[j].name
	})

	for len(available) > 0 {
		idx := pickWeightedIndex(available, r)
		chosen := available[idx]
		candidates, err := listSeedCandidates(vaultRoot, chosen.name)
		if err == nil && len(candidates) > 0 {
			seed := pickFromCandidates(candidates, recencyBias, r)
			seed.Source = chosen.name
			return seed, nil
		}
		available = append(available[:idx], available[idx+1:]...)
	}
	return Seed{}, ErrNoSeedAvailable
}

// CountReplayPoolCandidates returns how many candidates passed each filter
// without sampling either side. Surfaces the seed-pool sizes for the
// autodream log so a fell_back=true entry can be attributed to the right
// cause (no recent material vs no stable pool vs simply rolled exploration).
//
// Errors fall through silently — partial counts are more informative than
// a hard failure for a diagnostic field. The strategy resolver still gets
// the real error from BuildReplayPair when it actually tries to build the
// pair.
func CountReplayPoolCandidates(vaultRoot string, cfg Config, now time.Time) (recent, stable int) {
	maxAge := time.Duration(cfg.AutoDaydreamReplayRecentMaxAgeDays) * 24 * time.Hour

	switch strings.ToLower(strings.TrimSpace(cfg.AutoDaydreamReplayRecentSource)) {
	case "", "buffer":
		seeds, err := listBufferSeeds(vaultRoot)
		if err == nil {
			for _, s := range seeds {
				if s.LastTouched.IsZero() {
					continue
				}
				if now.Sub(s.LastTouched) <= maxAge {
					recent++
				}
			}
		}
	case "recently_accessed_ltm":
		memories, err := LoadAllMemories(vaultRoot)
		if err == nil {
			for _, m := range memories {
				if m.Archived != nil {
					continue
				}
				if m.LastAccessed.IsZero() || now.Sub(m.LastAccessed) > maxAge {
					continue
				}
				recent++
			}
		}
	}

	memories, err := LoadAllMemories(vaultRoot)
	if err == nil {
		allowed := make(map[string]bool, len(cfg.AutoDaydreamReplayStableCategories))
		for _, c := range cfg.AutoDaydreamReplayStableCategories {
			allowed[strings.ToLower(strings.TrimSpace(c))] = true
		}
		for _, m := range memories {
			if m.Archived != nil {
				continue
			}
			if len(allowed) > 0 && !allowed[strings.ToLower(string(m.Type))] {
				continue
			}
			if !meetsStabilityFilter(m, cfg) {
				continue
			}
			stable++
		}
	}
	return recent, stable
}

// BuildReplayPair constructs a (recent, stable) seed pair for an interleaved
// replay daydream. Returns ErrNoPairAvailable if either side cannot be
// populated — the strategy resolver uses this signal to fall back to
// exploration.
func BuildReplayPair(vaultRoot string, cfg Config, now time.Time, r *rand.Rand) (SeedPair, error) {
	maxAge := time.Duration(cfg.AutoDaydreamReplayRecentMaxAgeDays) * 24 * time.Hour

	recent, err := pickRecentTrace(vaultRoot, cfg.AutoDaydreamReplayRecentSource, now, maxAge, r)
	if err != nil {
		return SeedPair{}, err
	}
	stable, err := pickStableTrace(vaultRoot, cfg, r)
	if err != nil {
		return SeedPair{}, err
	}
	return SeedPair{Recent: recent, Stable: stable}, nil
}

// pickWeightedIndex performs a cumulative-sum weighted random pick over a
// slice in deterministic order. Returns the index of the chosen element.
func pickWeightedIndex(items []catWeight, r *rand.Rand) int {
	var total float64
	for _, it := range items {
		total += it.weight
	}
	roll := randFloat(r) * total
	var cumulative float64
	for i, it := range items {
		cumulative += it.weight
		if roll < cumulative {
			return i
		}
	}
	return len(items) - 1
}

func randFloat(r *rand.Rand) float64 {
	if r != nil {
		return r.Float64()
	}
	return rand.Float64()
}

func randIntN(r *rand.Rand, n int) int {
	if r != nil {
		return r.IntN(n)
	}
	return rand.IntN(n)
}

// listSeedCandidates returns all eligible Seed entries for a category.
// Returns nil (no error) for unknown categories so a misconfigured weight
// degrades gracefully instead of bringing down the whole pick.
func listSeedCandidates(vaultRoot, category string) ([]Seed, error) {
	if strings.EqualFold(category, "buffer") {
		return listBufferSeeds(vaultRoot)
	}
	return listMemorySeeds(vaultRoot, category)
}

func listBufferSeeds(vaultRoot string) ([]Seed, error) {
	entries, err := LoadAllBufferEntries(vaultRoot)
	if err != nil {
		return nil, err
	}
	var seeds []Seed
	for _, e := range entries {
		if isDaydreamBufferEntry(e) {
			continue
		}
		var lastTouched time.Time
		if info, statErr := os.Stat(e.FilePath); statErr == nil {
			lastTouched = info.ModTime()
		} else if !e.Timestamp.IsZero() {
			lastTouched = e.Timestamp
		}
		seeds = append(seeds, Seed{
			Source:      "buffer",
			FilePath:    e.FilePath,
			Title:       strings.TrimSuffix(e.FileName, ".md"),
			LastTouched: lastTouched,
		})
	}
	return seeds, nil
}

func isDaydreamBufferEntry(e *BufferEntry) bool {
	if strings.EqualFold(strings.TrimSpace(e.Source), "daydream") {
		return true
	}
	if strings.Contains(filepath.ToSlash(e.FilePath), "/Daydream/") {
		return true
	}
	return false
}

func listMemorySeeds(vaultRoot, category string) ([]Seed, error) {
	typeName := categoryToType(category)
	if typeName == "" {
		return nil, nil
	}
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		return nil, err
	}
	var seeds []Seed
	for _, m := range memories {
		if m.Archived != nil {
			continue
		}
		if string(m.Type) != typeName {
			continue
		}
		title := m.Title
		if title == "" {
			title = strings.TrimSuffix(m.FileName, ".md")
		}
		seeds = append(seeds, Seed{
			Source:      category,
			FilePath:    m.FilePath,
			Title:       title,
			LastTouched: m.LastAccessed,
		})
	}
	return seeds, nil
}

// categoryToType maps the config-side category names (used in seed_sources
// maps) to the MemoryType strings the parser stores. The mapping is a
// straight lowercase identity for current categories — kept as an explicit
// switch so future renames or aliases stay obvious.
func categoryToType(category string) string {
	switch strings.ToLower(category) {
	case "user":
		return string(TypeUser)
	case "feedback":
		return string(TypeFeedback)
	case "project":
		return string(TypeProject)
	case "reference":
		return string(TypeReference)
	case "semantic":
		return string(TypeSemantic)
	case "episodic":
		return string(TypeEpisodic)
	case "knowledge":
		return string(TypeKnowledge)
	}
	return ""
}

// pickFromCandidates picks one seed from a list, optionally narrowing to the
// top half by LastTouched. Recency bias only narrows the pool — it does not
// weight within the narrowed pool, which keeps the implementation simple
// and the behavior easy to reason about.
func pickFromCandidates(candidates []Seed, recencyBias bool, r *rand.Rand) Seed {
	if len(candidates) == 0 {
		return Seed{}
	}
	pool := candidates
	if recencyBias && len(candidates) > 1 {
		sorted := make([]Seed, len(candidates))
		copy(sorted, candidates)
		sort.SliceStable(sorted, func(i, j int) bool {
			return sorted[i].LastTouched.After(sorted[j].LastTouched)
		})
		topN := (len(sorted) + 1) / 2 // upper half, rounds up for odd counts
		pool = sorted[:topN]
	}
	return pool[randIntN(r, len(pool))]
}

// pickRecentTrace samples one entry from the configured recent_source that
// falls within maxAge of now. Returns ErrNoPairAvailable when no candidate
// passes the age filter — that's the "no recent material" signal.
func pickRecentTrace(vaultRoot, source string, now time.Time, maxAge time.Duration, r *rand.Rand) (Seed, error) {
	var candidates []Seed

	switch strings.ToLower(strings.TrimSpace(source)) {
	case "", "buffer":
		seeds, err := listBufferSeeds(vaultRoot)
		if err != nil {
			return Seed{}, err
		}
		for _, s := range seeds {
			if s.LastTouched.IsZero() {
				continue
			}
			if now.Sub(s.LastTouched) <= maxAge {
				candidates = append(candidates, s)
			}
		}
	case "recently_accessed_ltm":
		memories, err := LoadAllMemories(vaultRoot)
		if err != nil {
			return Seed{}, err
		}
		for _, m := range memories {
			if m.Archived != nil {
				continue
			}
			if m.LastAccessed.IsZero() || now.Sub(m.LastAccessed) > maxAge {
				continue
			}
			title := m.Title
			if title == "" {
				title = strings.TrimSuffix(m.FileName, ".md")
			}
			candidates = append(candidates, Seed{
				Source:      string(m.Type),
				FilePath:    m.FilePath,
				Title:       title,
				LastTouched: m.LastAccessed,
			})
		}
	default:
		return Seed{}, fmt.Errorf("unknown replay_recent_source %q", source)
	}

	if len(candidates) == 0 {
		return Seed{}, ErrNoPairAvailable
	}
	return candidates[randIntN(r, len(candidates))], nil
}

// pickStableTrace samples one crystallized (or mature) memory from the
// allowed categories. Returns ErrNoPairAvailable when no LTM passes the
// stability filter — replay can't run without a stable partner to integrate
// against.
func pickStableTrace(vaultRoot string, cfg Config, r *rand.Rand) (Seed, error) {
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		return Seed{}, err
	}

	allowed := make(map[string]bool, len(cfg.AutoDaydreamReplayStableCategories))
	for _, c := range cfg.AutoDaydreamReplayStableCategories {
		allowed[strings.ToLower(strings.TrimSpace(c))] = true
	}

	var candidates []Seed
	for _, m := range memories {
		if m.Archived != nil {
			continue
		}
		if len(allowed) > 0 && !allowed[strings.ToLower(string(m.Type))] {
			continue
		}
		if !meetsStabilityFilter(m, cfg) {
			continue
		}
		title := m.Title
		if title == "" {
			title = strings.TrimSuffix(m.FileName, ".md")
		}
		candidates = append(candidates, Seed{
			Source:      string(m.Type),
			FilePath:    m.FilePath,
			Title:       title,
			LastTouched: m.LastAccessed,
		})
	}
	if len(candidates) == 0 {
		return Seed{}, ErrNoPairAvailable
	}
	return candidates[randIntN(r, len(candidates))], nil
}

// meetsStabilityFilter checks whether a memory entry qualifies as a stable
// partner for replay. "Crystallized" requires either profile=true or
// access_count >= rate_separation_crystallized_threshold (matches the rate-
// separation tier). "Mature" is the lower bar. Anything else falls back to
// the mature threshold so a typo doesn't silently allow plastic entries.
func meetsStabilityFilter(m *MemoryEntry, cfg Config) bool {
	switch strings.ToLower(strings.TrimSpace(cfg.AutoDaydreamReplayStableFilter)) {
	case "crystallized":
		if m.Profile {
			return true
		}
		return m.AccessCount >= cfg.RateSeparationCrystallizedThreshold
	case "mature":
		return m.AccessCount >= cfg.RateSeparationMatureThreshold
	default:
		return m.AccessCount >= cfg.RateSeparationMatureThreshold
	}
}
