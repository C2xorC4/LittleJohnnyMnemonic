package main

import (
	"time"
)

// Fidelity levels form the progressive compression ladder:
//
//	full → detailed → summary → gist
//
// Each step reduces body content while preserving title, tags, and links.
// The gist level is the permanent remnant — persists nearly indefinitely.
const (
	FidelityFull     = "full"
	FidelityDetailed = "detailed"
	FidelitySummary  = "summary"
	FidelityGist     = "gist"
)

// fidelityOrder is used to compare fidelity levels. Higher index = more
// compressed. Compression only advances forward along this axis.
var fidelityOrder = map[string]int{
	FidelityFull:     0,
	FidelityDetailed: 1,
	FidelitySummary:  2,
	FidelityGist:     3,
}

// Soft character budgets per fidelity level. These are guidance for
// Claude's compression judgment, not hard limits — `jm compress --apply`
// emits a warning if the new body exceeds the budget for its target
// fidelity but still writes the file.
const (
	MaxCharsDetailed = 2000
	MaxCharsSummary  = 800
	MaxCharsGist     = 400
)

// FidelityBudget returns the soft character budget for a given fidelity
// level. Returns 0 for "full" (no budget) or unknown values.
func FidelityBudget(fidelity string) int {
	switch fidelity {
	case FidelityDetailed:
		return MaxCharsDetailed
	case FidelitySummary:
		return MaxCharsSummary
	case FidelityGist:
		return MaxCharsGist
	}
	return 0
}

// ImportanceLevel controls how aggressively a memory compresses over time.
type ImportanceLevel string

const (
	ImportanceCritical    ImportanceLevel = "critical"
	ImportanceSignificant ImportanceLevel = "significant"
	ImportanceModerate    ImportanceLevel = "moderate"
	ImportanceMinor       ImportanceLevel = "minor"
)

// compressionThresholds defines when a memory transitions to each fidelity
// level based on days since last access. Critical memories never compress.
//
// Aligned with the CLAUDE.md progressive compression spec:
//   - Critical: never compresses
//   - Significant: months at full, years to gist
//   - Moderate: weeks at full, months to gist
//   - Minor: days at full, weeks to gist
type compressionSchedule struct {
	fullToDetailed    float64 // days
	detailedToSummary float64
	summaryToGist     float64
}

// compressionThresholds holds the active fidelity-transition schedule.
// Defaults match DefaultConfig().CompressionThresholds and are tuned for
// an early-stage memory system. ApplyConfigCompressionThresholds(cfg)
// overrides this map at runtime when System/Config.md specifies a
// compression_thresholds section.
var compressionThresholds = map[ImportanceLevel]compressionSchedule{
	ImportanceCritical: {
		// Never compresses — sentinel values far in the future.
		fullToDetailed:    1e9,
		detailedToSummary: 1e9,
		summaryToGist:     1e9,
	},
	ImportanceSignificant: {
		fullToDetailed:    120,
		detailedToSummary: 365,
		summaryToGist:     1095,
	},
	ImportanceModerate: {
		fullToDetailed:    60,
		detailedToSummary: 180,
		summaryToGist:     540,
	},
	ImportanceMinor: {
		fullToDetailed:    21,
		detailedToSummary: 60,
		summaryToGist:     180,
	},
}

// ApplyConfigCompressionThresholds overrides the package-level
// compressionThresholds map from a parsed Config. Each importance tier
// requires all three transition values to be present and non-zero before
// the override is applied — partial maps fall back to existing values
// rather than silently zeroing transitions (which would force everything
// to gist immediately).
//
// Critical is never overridable — it is always immune via IsCompressionImmune.
func ApplyConfigCompressionThresholds(cfg Config) {
	if cfg.CompressionThresholds == nil {
		return
	}
	tiers := []ImportanceLevel{
		ImportanceSignificant,
		ImportanceModerate,
		ImportanceMinor,
	}
	for _, imp := range tiers {
		prefix := string(imp) + "_"
		fullToDet, ok1 := cfg.CompressionThresholds[prefix+"full_to_detailed"]
		detToSum, ok2 := cfg.CompressionThresholds[prefix+"detailed_to_summary"]
		sumToGist, ok3 := cfg.CompressionThresholds[prefix+"summary_to_gist"]
		if !ok1 || !ok2 || !ok3 {
			continue
		}
		if fullToDet <= 0 || detToSum <= 0 || sumToGist <= 0 {
			continue
		}
		compressionThresholds[imp] = compressionSchedule{
			fullToDetailed:    fullToDet,
			detailedToSummary: detToSum,
			summaryToGist:     sumToGist,
		}
	}
}

// IsCompressionImmune returns true if a memory should never compress
// regardless of age. Profile traits, training overrides, episodic memory,
// and ingested knowledge are all immutable by design.
func IsCompressionImmune(m *MemoryEntry) bool {
	if m.Profile {
		return true
	}
	if m.TrainingOverride {
		return true
	}
	switch m.Type {
	case TypeEpisodic, TypeKnowledge, TypeBuffer:
		return true
	}
	return false
}

// InferImportance produces a default importance level for a memory that
// doesn't have one set. Uses type as the base signal, with surprise at
// encoding as a tiebreaker: high surprise bumps up one level, very low
// surprise bumps down one level.
//
// This is used by `jm reclassify` to backfill the importance field on
// existing memories, and as a fallback during classification if a
// memory somehow still has no importance set.
func InferImportance(m *MemoryEntry) ImportanceLevel {
	// Immune types all map to critical — they're never compressed anyway,
	// but giving them an explicit importance keeps the field populated.
	if IsCompressionImmune(m) {
		return ImportanceCritical
	}

	var base ImportanceLevel
	switch m.Type {
	case TypeFeedback, TypeProject, TypeSemantic:
		base = ImportanceSignificant
	case TypeUser, TypeReference:
		base = ImportanceModerate
	default:
		base = ImportanceModerate
	}

	// Surprise tiebreaker: ±1 level based on encoding surprise.
	switch {
	case m.SurpriseAtEncoding >= 0.7:
		base = bumpImportanceUp(base)
	case m.SurpriseAtEncoding < 0.3 && m.SurpriseAtEncoding > 0:
		base = bumpImportanceDown(base)
	}

	return base
}

func bumpImportanceUp(i ImportanceLevel) ImportanceLevel {
	switch i {
	case ImportanceMinor:
		return ImportanceModerate
	case ImportanceModerate:
		return ImportanceSignificant
	case ImportanceSignificant:
		return ImportanceCritical
	}
	return i
}

func bumpImportanceDown(i ImportanceLevel) ImportanceLevel {
	switch i {
	case ImportanceCritical:
		return ImportanceSignificant
	case ImportanceSignificant:
		return ImportanceModerate
	case ImportanceModerate:
		return ImportanceMinor
	}
	return i
}

// ClassifyTargetFidelity determines what fidelity level a memory SHOULD be
// at based on its importance and time since last access. Returns the
// current fidelity if the memory is immune or not yet due for compression.
//
// This function is pure classification — it does not modify the memory or
// perform any transformation. The decay pass writes the result to the
// memory's target_fidelity field, and a separate compress pass handles
// the actual content transformation.
func ClassifyTargetFidelity(m *MemoryEntry, now time.Time) string {
	if IsCompressionImmune(m) {
		return currentFidelity(m)
	}

	imp := ImportanceLevel(m.Importance)
	if imp == "" {
		imp = InferImportance(m)
	}

	schedule, ok := compressionThresholds[imp]
	if !ok {
		schedule = compressionThresholds[ImportanceModerate]
	}

	ageDays := now.Sub(m.LastAccessed).Hours() / 24

	// Determine the target based on age thresholds. We compare against the
	// gist boundary first — a very old memory should classify directly to
	// gist without walking through intermediate levels.
	switch {
	case ageDays >= schedule.summaryToGist:
		return FidelityGist
	case ageDays >= schedule.detailedToSummary:
		return FidelitySummary
	case ageDays >= schedule.fullToDetailed:
		return FidelityDetailed
	default:
		return FidelityFull
	}
}

// currentFidelity returns the memory's current fidelity, defaulting to
// "full" if the field is unset.
func currentFidelity(m *MemoryEntry) string {
	if m.Fidelity == "" {
		return FidelityFull
	}
	return m.Fidelity
}

// FidelityIsHigherThan returns true if a is more compressed than b.
// e.g., FidelityIsHigherThan("summary", "detailed") == true.
func FidelityIsHigherThan(a, b string) bool {
	return fidelityOrder[a] > fidelityOrder[b]
}
