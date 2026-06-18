package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// memoryUsageLogEntry records whether an assistant turn referenced memories
// that were injected on the preceding user-prompt-submit hook call.
// Injection is model-independent (deterministic hook); usage is model-dependent.
type memoryUsageLogEntry struct {
	Timestamp          string   `json:"timestamp"`
	SessionID          string   `json:"session_id"`
	RetrievalSessionID string   `json:"retrieval_session_id,omitempty"`
	ScoringConfigHash  string   `json:"scoring_config_hash,omitempty"`
	Model              string   `json:"model"`
	RuntimeHost        string   `json:"runtime_host"`
	MemoriesInjected   int      `json:"memories_injected"`
	MemoriesReferenced int      `json:"memories_referenced"`
	ReferenceRate      float64  `json:"reference_rate"`
	Outcome            string   `json:"outcome"` // no_injection, none, partial, referenced
	Backfill           bool     `json:"backfill,omitempty"`
	SlugsInjected      []string `json:"slugs_injected,omitempty"`
	SlugsReferenced    []string `json:"slugs_referenced,omitempty"`
}

const minSlugMatchLen = 4

// recordMemoryUsage scores assistant text against a retrieval session's loaded
// memories and appends a JSONL record. Never blocks the hook on failure.
func recordMemoryUsage(vaultRoot string, rs *RetrievalSession, assistantText string, input *hookInput, transcriptPath string) {
	cfg := LoadConfig(vaultRoot)
	if !cfg.MemoryUsageTrackingEnabled {
		return
	}
	if rs == nil {
		return
	}

	injected := len(rs.Loaded)
	referencedKeys := extractReferencedMemoryKeys(assistantText, rs.Loaded)
	referenced := len(referencedKeys)

	entry := buildMemoryUsageLogEntry(rs, referencedKeys, injected, referenced, input, transcriptPath, cfg.MemoryUsageTrackingVerbosity)
	logPath := filepath.Join(vaultRoot, cfg.MemoryUsageTrackingLogPath)
	appendMemoryUsageLog(logPath, entry)
}

func buildMemoryUsageLogEntry(rs *RetrievalSession, referencedKeys []string, injected, referenced int, input *hookInput, transcriptPath, verbosity string) memoryUsageLogEntry {
	sessionID := ""
	if input != nil {
		sessionID = input.SessionID
	}

	rate := 0.0
	if injected > 0 {
		rate = float64(referenced) / float64(injected)
	}

	entry := memoryUsageLogEntry{
		Timestamp:          time.Now().UTC().Format(time.RFC3339),
		SessionID:          sessionID,
		RetrievalSessionID: rs.SessionID,
		ScoringConfigHash:  rs.ScoringConfigHash,
		Model:              DetectModel(input, transcriptPath),
		RuntimeHost:        DetectRuntimeHost(),
		MemoriesInjected:   injected,
		MemoriesReferenced: referenced,
		ReferenceRate:      rate,
		Outcome:            classifyMemoryUsageOutcome(injected, referenced),
	}

	if verbosity == "verbose" {
		entry.SlugsInjected = slugsFromMemoryKeys(rs.Loaded)
		entry.SlugsReferenced = slugsFromMemoryKeys(referencedKeys)
	}
	return entry
}

func classifyMemoryUsageOutcome(injected, referenced int) string {
	switch {
	case injected == 0:
		return "no_injection"
	case referenced == 0:
		return "none"
	case referenced >= injected:
		return "referenced"
	default:
		return "partial"
	}
}

func slugsFromMemoryKeys(keys []string) []string {
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		if slug := memorySlugFromKey(k); slug != "" {
			out = append(out, slug)
		}
	}
	return out
}

func memorySlugFromKey(key string) string {
	key = strings.ToLower(strings.TrimSpace(key))
	parts := strings.Split(key, "/")
	if len(parts) >= 3 {
		return parts[len(parts)-1]
	}
	base := filepath.Base(key)
	return strings.TrimSuffix(base, ".md")
}

// extractReferencedMemoryKeys returns loaded memory keys referenced in text via
// wiki-links or slug/title mentions (broader than citation harvest alone).
func extractReferencedMemoryKeys(text string, loaded []string) []string {
	if text == "" || len(loaded) == 0 {
		return nil
	}

	loadedSet := make(map[string]bool, len(loaded))
	for _, k := range loaded {
		loadedSet[strings.ToLower(k)] = true
	}

	seen := make(map[string]bool)
	var refs []string

	for _, key := range extractCitedMemoryKeys(text) {
		if !loadedSet[key] || seen[key] {
			continue
		}
		seen[key] = true
		refs = append(refs, key)
	}

	textLower := strings.ToLower(text)
	for _, key := range loaded {
		key = strings.ToLower(key)
		if seen[key] {
			continue
		}
		slug := memorySlugFromKey(key)
		if len(slug) < minSlugMatchLen {
			continue
		}
		if matchesSlugInText(textLower, slug) {
			seen[key] = true
			refs = append(refs, key)
		}
	}
	return refs
}

func matchesSlugInText(textLower, slug string) bool {
	if strings.Contains(textLower, slug) {
		return true
	}
	spaced := strings.ReplaceAll(slug, "_", " ")
	if spaced != slug && strings.Contains(textLower, spaced) {
		return true
	}
	hyphenated := strings.ReplaceAll(slug, "_", "-")
	if hyphenated != slug && strings.Contains(textLower, hyphenated) {
		return true
	}
	return false
}

func appendMemoryUsageLog(logPath string, entry memoryUsageLogEntry) {
	data, err := json.Marshal(entry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[memory-usage] marshal: %v\n", err)
		return
	}
	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[memory-usage] open log %s: %v\n", logPath, err)
		return
	}
	defer f.Close()
	_, _ = f.Write(data)
	_, _ = f.Write([]byte("\n"))
}