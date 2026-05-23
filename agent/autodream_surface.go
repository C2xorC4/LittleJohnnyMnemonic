package main

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

// Hook-surfacing path. When auto_daydream_surface_to_session is enabled, the
// UserPromptSubmit hook scans Buffer/Daydream/ for fresh, unsurfaced
// daydream findings, scores them against the current prompt, and emits a
// <fresh-daydream-findings> block alongside the existing <active-context>.
//
// Default off: real-time engagement signals during task focus aren't
// reliable triage (see the 2026-04-30 buffer entry on the topic). The
// LLM judge in consolidation handles routing without depending on user
// attention. Surfacing here is an opt-in extra for users who want passive
// surfacing.

// SurfacedDaydream pairs a buffer entry with its computed relevance score
// for hook output.
type SurfacedDaydream struct {
	Entry   *BufferEntry
	Score   float64
	Excerpt string
}

// SurfaceFreshDaydreams returns matching daydream entries to render in the
// hook block. Returns nil when the toggle is disabled, no entries qualify,
// or scoring is below threshold. Caller is responsible for marking entries
// as surfaced after writing them.
//
// The currentSessionID parameter drives within-session deduplication:
// entries already surfaced in this session are skipped. Cross-session
// re-eligibility is intentional — a finding may be relevant in different
// design contexts. Per-session suppression beats time-based cooldown
// because the latter produces repetition fatigue on long single-topic
// sessions (same finding re-surfacing every cooldown window).
func SurfaceFreshDaydreams(vaultRoot, prompt, currentSessionID string, cfg Config, now time.Time) []SurfacedDaydream {
	if !cfg.AutoDaydreamSurfaceToSession {
		return nil
	}

	entries, err := loadDaydreamEntries(vaultRoot)
	if err != nil || len(entries) == 0 {
		return nil
	}

	keywords := ExtractKeywords(prompt)
	if len(keywords) == 0 {
		return nil
	}

	maxAge := time.Duration(cfg.AutoDaydreamSurfaceMaxAgeHours) * time.Hour
	cutoff := now.Add(-maxAge)
	threshold := cfg.AutoDaydreamSurfaceRelevanceThreshold
	if threshold <= 0 {
		threshold = 0.4 // safety default if config omitted
	}

	var scored []SurfacedDaydream
	for _, e := range entries {
		// Within-session dedup: skip entries already surfaced this session.
		// An empty currentSessionID disables the dedup (every match surfaces),
		// which is the right behavior for tests and for hooks that don't
		// have session context.
		if currentSessionID != "" && containsString(e.SurfacedInSessions, currentSessionID) {
			continue
		}
		if maxAge > 0 && !e.Timestamp.IsZero() && e.Timestamp.Before(cutoff) {
			continue
		}
		score := scoreDaydreamRelevance(e, keywords)
		if score < threshold {
			continue
		}
		scored = append(scored, SurfacedDaydream{
			Entry:   e,
			Score:   score,
			Excerpt: excerptText(e.Body, 250),
		})
	}

	sort.Slice(scored, func(i, j int) bool {
		return scored[i].Score > scored[j].Score
	})
	if cap := cfg.AutoDaydreamSurfaceMaxPerPrompt; cap > 0 && len(scored) > cap {
		scored = scored[:cap]
	}
	return scored
}

// scoreDaydreamRelevance is a lightweight tag+body keyword overlap score
// for a buffer entry. We don't use AssociateMemories here because that's
// shaped for MemoryEntry (with last_accessed/confidence/decay) and buffer
// entries don't have those signals — surfacing should be purely topical.
//
// Weights mirror AssociateMemories: 60% tag overlap, 40% body. Returns a
// value in [0, 1].
func scoreDaydreamRelevance(entry *BufferEntry, keywords []string) float64 {
	if entry == nil || len(keywords) == 0 {
		return 0
	}
	tagSet := make(map[string]bool, len(entry.Tags))
	for _, t := range entry.Tags {
		tagSet[Stem(strings.ToLower(t))] = true
	}
	tagHits := 0
	for _, kw := range keywords {
		if tagSet[kw] {
			tagHits++
		}
	}
	tagRel := float64(tagHits) / float64(len(keywords))

	bodySet := stemTextSet(strings.TrimSuffix(entry.FileName, ".md") + " " + entry.Body)
	bodyHits := 0
	for _, kw := range keywords {
		if bodySet[kw] {
			bodyHits++
		}
	}
	bodyRel := float64(bodyHits) / float64(len(keywords))

	combined := tagRel*0.6 + bodyRel*0.4
	if combined > 1.0 {
		combined = 1.0
	}
	return combined
}

// MarkDaydreamSurfaced appends the current session ID to the entry's
// SurfacedInSessions list and persists it. Called after the hook emits
// the entry. Within-session: the entry won't surface again because the
// session ID is already in the list. Cross-session: a different session
// ID makes it eligible again in different design contexts.
//
// Empty session IDs are allowed (some test paths) and produce a no-op
// dedup — the entry will re-surface on the next call.
func MarkDaydreamSurfaced(entry *BufferEntry, sessionID string) error {
	if sessionID != "" && !containsString(entry.SurfacedInSessions, sessionID) {
		entry.SurfacedInSessions = append(entry.SurfacedInSessions, sessionID)
	}
	return WriteBufferEntry(entry)
}

// containsString returns true if needle is present in haystack.
func containsString(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

// writeFreshDaydreamFindings emits the <fresh-daydream-findings> block to
// w. No-op when the surfaced list is empty.
func writeFreshDaydreamFindings(w io.Writer, surfaced []SurfacedDaydream) {
	if len(surfaced) == 0 {
		return
	}
	fmt.Fprintln(w)
	fmt.Fprintln(w, "<fresh-daydream-findings>")
	for _, s := range surfaced {
		title := strings.TrimSuffix(s.Entry.FileName, ".md")
		kind := s.Entry.DaydreamKind
		if kind == "" {
			kind = "exploration"
		}
		fmt.Fprintf(w, "## %s [%s]\n", title, kind)
		fmt.Fprintln(w, s.Excerpt)
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w, "</fresh-daydream-findings>")
}
