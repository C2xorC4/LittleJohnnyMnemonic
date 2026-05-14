package main

import (
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
type recallLogEntry struct {
	Timestamp string         `json:"timestamp"`
	SessionID string         `json:"session_id"`
	PromptLen int            `json:"prompt_chars"`
	Total     int            `json:"total"`
	Counts    map[string]int `json:"counts"`
	Slugs     []string       `json:"slugs,omitempty"` // only in verbose mode
}

// writeRecallMetrics is called after AssociateMemories in runUserPromptSubmit.
// Emits recall metrics to console (stderr, not injected into model context)
// and/or the JSONL recall log, based on Config. Never blocks or panics —
// all errors are logged to stderr and swallowed.
func writeRecallMetrics(vaultRoot string, results []AssociatedMemory, sessionID string, promptLen int) {
	if len(results) == 0 {
		return
	}
	cfg := LoadConfig(vaultRoot)
	if !cfg.RecallTrackingEnabled {
		return
	}

	toConsole := cfg.RecallTrackingDisplay == "console" || cfg.RecallTrackingDisplay == "both"
	toLog := cfg.RecallTrackingDisplay == "log" || cfg.RecallTrackingDisplay == "both"

	if toConsole {
		writeRecallConsole(os.Stderr, results, cfg.RecallTrackingVerbosity)
	}
	if toLog {
		entry := buildRecallLogEntry(results, sessionID, promptLen, cfg.RecallTrackingVerbosity)
		appendRecallLog(filepath.Join(vaultRoot, cfg.RecallTrackingLogPath), entry)
	}
}

// writeRecallConsole writes a compact recall line to w (stderr).
//
// Summary: [recall] feedback:2 semantic:3 user:1 (total:6)
// Verbose:  [recall] feedback:2 (slug_a, slug_b) | semantic:3 (slug_c) | user:1 (slug_d) (total:6)
func writeRecallConsole(w io.Writer, results []AssociatedMemory, verbosity string) {
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
	for _, r := range results {
		counts[string(r.Memory.Type)]++
		if verbosity == "verbose" {
			base := filepath.Base(r.Memory.FileName)
			slugs = append(slugs, strings.TrimSuffix(base, ".md"))
		}
	}
	return recallLogEntry{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		SessionID: sessionID,
		PromptLen: promptLen,
		Total:     len(results),
		Counts:    counts,
		Slugs:     slugs,
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
