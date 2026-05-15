package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// recallLogEntry is one JSONL record appended to Metrics/recall_log.jsonl.
// Designed for time-series analysis: recall frequency by category over time,
// cross-referenced against vault depth from the consolidation log.
//
// AvgBodyHits is the mean number of prompt-keyword hits in the body text
// across all retrieved memories. A memory with zero body hits matched on tags
// only — it crossed the retrieval threshold but had no textual contact with
// the prompt. This is a partial proxy for content-level influence: it captures
// episodic and factual activation well, but is blind to semantic memories
// that shape responses through framing rather than keyword co-occurrence.
// Low AvgBodyHits on a high-Total recall suggests broad taxonomic loading
// with thin content-level contact. High AvgBodyHits suggests prompt terms
// are finding body-level matches across the loaded set.
type recallLogEntry struct {
	Timestamp     string         `json:"timestamp"`
	SessionID     string         `json:"session_id"`
	PromptLen     int            `json:"prompt_chars"`
	Total         int            `json:"total"`
	Counts        map[string]int `json:"counts"`
	Slugs         []string       `json:"slugs,omitempty"`          // verbose only
	BodyHitCounts map[string]int `json:"body_hit_counts,omitempty"` // verbose only: slug → body hit count
	AvgBodyHits   float64        `json:"avg_body_hits"`             // mean body keyword hits per recalled memory
	AvgRelevance  float64        `json:"avg_relevance"`             // mean combined relevance score per recalled memory
}

// recallDayEntry is a compressed daily aggregate that replaces per-prompt
// entries for dates older than the configured retention window.
type recallDayEntry struct {
	Date         string         `json:"date"`          // "2026-05-14" UTC
	Granularity  string         `json:"granularity"`   // always "day"
	Prompts      int            `json:"prompts"`
	TotalRecalls int            `json:"total_recalls"`
	AvgTotal     float64        `json:"avg_total"`
	Counts       map[string]int `json:"counts"`
	AvgBodyHits  float64        `json:"avg_body_hits"`  // weighted mean across all recalled memories that day
	AvgRelevance float64        `json:"avg_relevance"`  // weighted mean relevance across all recalled memories that day
}

// writeRecallMetrics is called before writePromptAssociationContext in
// runUserPromptSubmit. Appends a recall record to the JSONL log. Never
// blocks or panics — all errors are logged to stderr and swallowed.
func writeRecallMetrics(vaultRoot string, results []AssociatedMemory, sessionID string, promptLen int) {
	if len(results) == 0 {
		return
	}
	cfg := LoadConfig(vaultRoot)
	if !cfg.RecallTrackingEnabled {
		return
	}
	entry := buildRecallLogEntry(results, sessionID, promptLen, cfg.RecallTrackingVerbosity)
	appendRecallLog(filepath.Join(vaultRoot, cfg.RecallTrackingLogPath), entry)
}

// compactRecallLog compresses per-prompt entries older than windowDays into
// daily aggregates. Entries within the window are preserved verbatim.
// Existing daily entries are merged if their date overlaps with newly
// compacted granular entries. Returns the number of granular entries replaced.
// If dryRun is true, no file is written and the count is still returned.
func compactRecallLog(logPath string, windowDays int, dryRun bool) (int, error) {
	data, err := os.ReadFile(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	cutoff := time.Now().UTC().Truncate(24 * time.Hour).AddDate(0, 0, -windowDays)

	var oldGranular []recallLogEntry
	var recentGranular []recallLogEntry
	existingDaily := map[string]recallDayEntry{}

	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		// Discriminate by presence of "granularity" key.
		var disc struct {
			Granularity string `json:"granularity"`
		}
		_ = json.Unmarshal([]byte(line), &disc)

		if disc.Granularity == "day" {
			var d recallDayEntry
			if err := json.Unmarshal([]byte(line), &d); err == nil {
				existingDaily[d.Date] = d
			}
			continue
		}

		var g recallLogEntry
		if err := json.Unmarshal([]byte(line), &g); err != nil || g.Timestamp == "" {
			continue
		}
		t, err := time.Parse(time.RFC3339, g.Timestamp)
		if err != nil {
			continue
		}
		if t.UTC().Before(cutoff) {
			oldGranular = append(oldGranular, g)
		} else {
			recentGranular = append(recentGranular, g)
		}
	}

	if len(oldGranular) == 0 {
		return 0, nil
	}

	// Group old granular entries by UTC date.
	byDate := map[string][]recallLogEntry{}
	for _, g := range oldGranular {
		t, _ := time.Parse(time.RFC3339, g.Timestamp)
		date := t.UTC().Format("2006-01-02")
		byDate[date] = append(byDate[date], g)
	}

	// Aggregate each date group, merging with any existing daily entry.
	for date, entries := range byDate {
		newDay := aggregateDayEntry(date, entries)
		if prev, ok := existingDaily[date]; ok {
			newDay = mergeDayEntries(prev, newDay)
		}
		existingDaily[date] = newDay
	}

	if dryRun {
		return len(oldGranular), nil
	}

	// Rebuild: daily entries (sorted by date) then recent granular (sorted by timestamp).
	dates := make([]string, 0, len(existingDaily))
	for d := range existingDaily {
		dates = append(dates, d)
	}
	sort.Strings(dates)

	var buf bytes.Buffer
	for _, d := range dates {
		b, _ := json.Marshal(existingDaily[d])
		buf.Write(b)
		buf.WriteByte('\n')
	}
	for _, g := range recentGranular {
		b, _ := json.Marshal(g)
		buf.Write(b)
		buf.WriteByte('\n')
	}

	return len(oldGranular), os.WriteFile(logPath, buf.Bytes(), 0o644)
}

// aggregateDayEntry builds a recallDayEntry from a slice of same-day granular entries.
func aggregateDayEntry(date string, entries []recallLogEntry) recallDayEntry {
	counts := map[string]int{}
	totalRecalls := 0
	weightedBodyHits := 0.0
	weightedRelevance := 0.0
	for _, e := range entries {
		totalRecalls += e.Total
		weightedBodyHits += e.AvgBodyHits * float64(e.Total)
		weightedRelevance += e.AvgRelevance * float64(e.Total)
		for k, v := range e.Counts {
			counts[k] += v
		}
	}
	avgTotal, avgBodyHits, avgRelevance := 0.0, 0.0, 0.0
	if len(entries) > 0 {
		avgTotal = float64(totalRecalls) / float64(len(entries))
	}
	if totalRecalls > 0 {
		avgBodyHits = weightedBodyHits / float64(totalRecalls)
		avgRelevance = weightedRelevance / float64(totalRecalls)
	}
	return recallDayEntry{
		Date:         date,
		Granularity:  "day",
		Prompts:      len(entries),
		TotalRecalls: totalRecalls,
		AvgTotal:     avgTotal,
		AvgBodyHits:  avgBodyHits,
		AvgRelevance: avgRelevance,
		Counts:       counts,
	}
}

// mergeDayEntries combines two daily entries for the same date (e.g. when
// running compact a second time after new entries have been added for an
// already-compacted date).
func mergeDayEntries(a, b recallDayEntry) recallDayEntry {
	counts := map[string]int{}
	for k, v := range a.Counts {
		counts[k] += v
	}
	for k, v := range b.Counts {
		counts[k] += v
	}
	prompts := a.Prompts + b.Prompts
	total := a.TotalRecalls + b.TotalRecalls
	avgTotal, avgBodyHits, avgRelevance := 0.0, 0.0, 0.0
	if prompts > 0 {
		avgTotal = float64(total) / float64(prompts)
	}
	if total > 0 {
		avgBodyHits = (a.AvgBodyHits*float64(a.TotalRecalls) + b.AvgBodyHits*float64(b.TotalRecalls)) / float64(total)
		avgRelevance = (a.AvgRelevance*float64(a.TotalRecalls) + b.AvgRelevance*float64(b.TotalRecalls)) / float64(total)
	}
	return recallDayEntry{
		Date:         a.Date,
		Granularity:  "day",
		Prompts:      prompts,
		TotalRecalls: total,
		AvgTotal:     avgTotal,
		AvgBodyHits:  avgBodyHits,
		AvgRelevance: avgRelevance,
		Counts:       counts,
	}
}

// formatRecallLine formats a compact recall summary for display or logging.
//
// Summary: [recall] feedback:2 semantic:3 user:1 (total:6)
// Verbose:  [recall] feedback:2 (slug_a, slug_b) | semantic:3 (slug_c) | user:1 (slug_d) (total:6)
func formatRecallLine(w io.Writer, results []AssociatedMemory, verbosity string) {
	counts := make(map[string]int)
	byType := make(map[string][]string)
	for _, r := range results {
		typeName := string(r.Memory.Type)
		counts[typeName]++
		if verbosity == "verbose" {
			base := filepath.Base(r.Memory.FileName)
			slug := strings.TrimSuffix(base, ".md")
			byType[typeName] = append(byType[typeName], slug)
		}
	}

	types := sortedIntMapKeys(counts)

	if verbosity != "verbose" {
		parts := make([]string, 0, len(types))
		for _, t := range types {
			parts = append(parts, fmt.Sprintf("%s:%d", t, counts[t]))
		}
		fmt.Fprintf(w, "[recall] %s (total:%d)\n", strings.Join(parts, " "), len(results))
		return
	}

	parts := make([]string, 0, len(types))
	for _, t := range types {
		parts = append(parts, fmt.Sprintf("%s:%d (%s)", t, counts[t], strings.Join(byType[t], ", ")))
	}
	fmt.Fprintf(w, "[recall] %s (total:%d)\n", strings.Join(parts, " | "), len(results))
}

// buildRecallLogEntry constructs the JSONL record from a retrieval result set.
func buildRecallLogEntry(results []AssociatedMemory, sessionID string, promptLen int, verbosity string) recallLogEntry {
	counts := make(map[string]int)
	var slugs []string
	var bodyHitCounts map[string]int
	if verbosity == "verbose" {
		bodyHitCounts = map[string]int{}
	}

	totalBodyHits := 0
	totalRelevance := 0.0
	for _, r := range results {
		counts[string(r.Memory.Type)]++
		hits := len(r.BodyKeywordHits)
		totalBodyHits += hits
		totalRelevance += r.Relevance
		if verbosity == "verbose" {
			base := filepath.Base(r.Memory.FileName)
			slug := strings.TrimSuffix(base, ".md")
			slugs = append(slugs, slug)
			if hits > 0 {
				bodyHitCounts[slug] = hits
			}
		}
	}

	avgBodyHits, avgRelevance := 0.0, 0.0
	if len(results) > 0 {
		avgBodyHits = float64(totalBodyHits) / float64(len(results))
		avgRelevance = totalRelevance / float64(len(results))
	}

	return recallLogEntry{
		Timestamp:     time.Now().UTC().Format(time.RFC3339),
		SessionID:     sessionID,
		PromptLen:     promptLen,
		Total:         len(results),
		Counts:        counts,
		Slugs:         slugs,
		BodyHitCounts: bodyHitCounts,
		AvgBodyHits:   avgBodyHits,
		AvgRelevance:  avgRelevance,
	}
}

// appendRecallLog appends entry as a JSONL record to logPath, creating the
// file if it doesn't exist. Errors are logged to stderr and swallowed.
func appendRecallLog(logPath string, entry recallLogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[recall] marshal: %v\n", err)
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[recall] open log %s: %v\n", logPath, err)
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
	_, _ = f.Write([]byte("\n"))
}

func sortedIntMapKeys(m map[string]int) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
