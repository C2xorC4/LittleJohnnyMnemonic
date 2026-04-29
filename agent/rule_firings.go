package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// BehavioralRule is one entry in System/behavioral_rules.json.
// Signal lists are case-insensitive substring patterns. Pattern matching is
// the cheap synchronous filter; the LLM judge is the authoritative verdict.
type BehavioralRule struct {
	ID             string   `json:"id"`
	SourceMemory   string   `json:"source_memory"`
	RuleText       string   `json:"rule_text"`
	FireSignals    []string `json:"fire_signals"`
	ContextSignals []string `json:"context_signals,omitempty"`
	Encoded        string   `json:"encoded,omitempty"`
	Notes          string   `json:"notes,omitempty"`
}

// RuleFiring is one record in Metrics/rule_firings.jsonl.
// Written in two stages: the Stop hook writes pattern matches with
// verdict="pending"; the async judge appends a separate record with
// the final verdict. Records are linked by firing_id.
type RuleFiring struct {
	FiringID              string    `json:"firing_id"`
	Timestamp             time.Time `json:"timestamp"`
	SessionID             string    `json:"session_id,omitempty"`
	RuleID                string    `json:"rule_id"`
	Stage                 string    `json:"stage"` // "pattern" | "judge"
	FireSignalsMatched    []string  `json:"fire_signals_matched,omitempty"`
	ContextSignalsMatched []string  `json:"context_signals_matched,omitempty"`
	Excerpt               string    `json:"excerpt,omitempty"`
	Verdict               string    `json:"verdict,omitempty"` // confirmed | rejected | uncertain | error
	JudgeReason           string    `json:"judge_reason,omitempty"`
	JudgeError            string    `json:"judge_error,omitempty"`
}

// loadBehavioralRules reads System/behavioral_rules.json. Returns an empty
// slice (no error) if the file is missing — the system should boot cleanly
// when no rules are configured yet.
func loadBehavioralRules(vaultRoot string) ([]BehavioralRule, error) {
	path := filepath.Join(vaultRoot, "System", "behavioral_rules.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var rules []BehavioralRule
	if err := json.Unmarshal(data, &rules); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return rules, nil
}

// scanForFireSignals returns the list of fire signals (case-insensitive)
// that appear in text. Empty result means the rule didn't pattern-match.
func scanForFireSignals(text string, rule BehavioralRule) []string {
	return scanSignals(text, rule.FireSignals)
}

func scanForContextSignals(text string, rule BehavioralRule) []string {
	return scanSignals(text, rule.ContextSignals)
}

func scanSignals(text string, signals []string) []string {
	if len(signals) == 0 {
		return nil
	}
	lower := strings.ToLower(text)
	var hits []string
	for _, s := range signals {
		if s == "" {
			continue
		}
		if strings.Contains(lower, strings.ToLower(s)) {
			hits = append(hits, s)
		}
	}
	return hits
}

// ruleFiringsLogPath returns the canonical path. Created on first write.
func ruleFiringsLogPath(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Metrics", "rule_firings.jsonl")
}

// firingLogMu serializes writes within a process. Cross-process safety
// relies on O_APPEND atomicity for small writes — JSONL records are
// always under a few KB, well within the atomic-write guarantee on both
// POSIX and Windows for local filesystems.
var firingLogMu sync.Mutex

// appendFiring writes one record as a single JSON line.
func appendFiring(vaultRoot string, f RuleFiring) error {
	firingLogMu.Lock()
	defer firingLogMu.Unlock()

	path := ruleFiringsLogPath(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	if _, err := file.Write(append(data, '\n')); err != nil {
		return err
	}
	return nil
}

// newFiringID produces a sortable, collision-resistant ID. Format: ts-rule-rand.
func newFiringID(ruleID string) string {
	return fmt.Sprintf("%d-%s-%04d", time.Now().UnixNano(), ruleID, os.Getpid()%10000)
}

// excerptText returns up to maxChars from the text, preferring a clean cut
// at the last period or space. Used to keep firing log entries bounded.
func excerptText(text string, maxChars int) string {
	text = strings.TrimSpace(text)
	if len(text) <= maxChars {
		return text
	}
	cut := maxChars
	if idx := strings.LastIndex(text[:maxChars], ". "); idx > maxChars/2 {
		cut = idx + 1
	} else if idx := strings.LastIndex(text[:maxChars], " "); idx > maxChars/2 {
		cut = idx
	}
	return strings.TrimSpace(text[:cut]) + " …"
}
