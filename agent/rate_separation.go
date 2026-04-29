package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"
)

// stabilityTier returns the rate-separation tier for a memory.
// The tier determines whether same-session buffer entries can be merged.
//   "plastic"      — no gate; merge freely
//   "mature"       — cross-session confirmation required once
//   "crystallized" — cross-session confirmation required; 2+ contributing sessions for profile rewrites
func stabilityTier(m *MemoryEntry, cfg Config) string {
	if m == nil {
		return "plastic"
	}
	// Profile facets are always crystallized regardless of access count.
	if m.Profile {
		return "crystallized"
	}
	if m.Importance == "critical" {
		return "crystallized"
	}
	if m.AccessCount >= cfg.RateSeparationCrystallizedThreshold {
		return "crystallized"
	}
	if m.AccessCount >= cfg.RateSeparationMatureThreshold {
		return "mature"
	}
	return "plastic"
}

// findStableCandidateTarget returns the highest-tag-overlap LTM memory that
// is mature or crystallized. Returns nil if no stable candidate exists.
// Uses the same tag-overlap heuristic as computeRedundancy (threshold: 0.3).
func findStableCandidateTarget(entry *BufferEntry, memories []*MemoryEntry, cfg Config) (*MemoryEntry, string) {
	bestOverlap := 0.3 // minimum threshold to consider a memory a candidate
	var bestMemory *MemoryEntry
	var bestTier string

	for _, m := range memories {
		if m.Archived != nil {
			continue
		}
		tier := stabilityTier(m, cfg)
		if tier == "plastic" {
			continue
		}
		overlap := tagOverlap(entry.Tags, m.Tags)
		if overlap > bestOverlap {
			bestOverlap = overlap
			bestMemory = m
			bestTier = tier
		}
	}
	return bestMemory, bestTier
}

// isSameSession returns true if the buffer entry was created after the last
// consolidation — i.e., it's from the current editing session and hasn't
// had a consolidation pass between encoding and this merge attempt.
// Entries predating the last consolidation are "previous-session" and pass freely.
func isSameSession(entry *BufferEntry, lastConsolidation time.Time) bool {
	if lastConsolidation.IsZero() {
		// No prior consolidation — vault is brand new.
		// Treat everything as same-session; gate conservatively.
		return true
	}
	return entry.Timestamp.After(lastConsolidation)
}

// checkRateSeparationGate returns (gated bool, target *MemoryEntry, tier string).
// gated=true means the buffer entry should be held, not merged.
func checkRateSeparationGate(
	entry *BufferEntry,
	memories []*MemoryEntry,
	cfg Config,
	lastConsolidation time.Time,
) (bool, *MemoryEntry, string) {
	if !cfg.RateSeparationEnabled {
		return false, nil, ""
	}
	// Only gate same-session entries.
	if !isSameSession(entry, lastConsolidation) {
		return false, nil, ""
	}
	target, tier := findStableCandidateTarget(entry, memories, cfg)
	if target == nil {
		return false, nil, ""
	}
	return true, target, tier
}

// crystallizedSessionsOK returns true if a crystallized memory has received
// contributions from at least cfg.RateSeparationMinSessions distinct sessions.
// Used to gate profile-rewrite merges (additive appends remain ungated).
func crystallizedSessionsOK(m *MemoryEntry, cfg Config) bool {
	return len(m.ContributingSessions) >= cfg.RateSeparationMinSessions
}

// lastConsolidationTime parses the consolidation log for the timestamp of the
// most-recent run. Returns zero time if the log doesn't exist or can't be parsed.
// Log header format: "## YYYY-MM-DD HH:MM — trigger (depth)"
var consolidationHeaderRe = regexp.MustCompile(`^## (\d{4}-\d{2}-\d{2} \d{2}:\d{2}) —`)

func lastConsolidationTime(vaultRoot string) time.Time {
	logPath := filepath.Join(vaultRoot, "Metrics", "consolidation_log.md")
	f, err := os.Open(logPath)
	if err != nil {
		return time.Time{}
	}
	defer f.Close()

	var latest time.Time
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		m := consolidationHeaderRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		t, err := time.ParseInLocation("2006-01-02 15:04", m[1], time.UTC)
		if err != nil {
			continue
		}
		if t.After(latest) {
			latest = t
		}
	}
	return latest
}

// consolidationSessionID returns a stable string identifier for the current
// consolidation run, used to track contributing_sessions on profile entries.
// Format: "consolidation-YYYY-MM-DD-HHMM"
func consolidationSessionID(now time.Time) string {
	return fmt.Sprintf("consolidation-%s", now.UTC().Format("2006-01-02-1504"))
}
