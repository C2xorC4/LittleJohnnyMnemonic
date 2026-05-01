package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ReplayVerdict is the relationship the agent assessed between the recent
// trace and the stable trait. The four values come from CLS-style integration
// semantics (see Agents/memory-daydream.md and the replay prompt template).
type ReplayVerdict string

const (
	VerdictReinforce  ReplayVerdict = "reinforce"
	VerdictRefine     ReplayVerdict = "refine"
	VerdictContradict ReplayVerdict = "contradict"
	VerdictUnrelated  ReplayVerdict = "unrelated"
)

// ErrVerdictNotFound is returned when the agent's response text does not
// contain a parseable verdict line. The orchestrator records this as a
// soft-error condition rather than a hard failure — the breadcrumb file
// (if written) is still authoritative for consolidation.
var ErrVerdictNotFound = errors.New("autodream: replay verdict not found in response")

// ReplayLogEntry is one record in Metrics/replay_log.jsonl — the audit trail
// for every replay event regardless of verdict. This is the file consolidation
// reads to understand what replay activity has happened.
type ReplayLogEntry struct {
	Timestamp  time.Time `json:"timestamp"`
	Verdict    string    `json:"verdict"`
	RecentPath string    `json:"recent_path"`
	StablePath string    `json:"stable_path"`
	Reasoning  string    `json:"reasoning,omitempty"`
}

// ReplayReinforcementEntry queues a confidence delta to be applied to the
// stable memory at the next consolidation pass. Applied=false means
// pending; the consolidation pass flips it to true after applying.
type ReplayReinforcementEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	StableMemoryPath string    `json:"stable_memory_path"`
	RecentSeedPath   string    `json:"recent_seed_path"`
	ConfidenceDelta  float64   `json:"confidence_delta"`
	Reasoning        string    `json:"reasoning,omitempty"`
	Applied          bool      `json:"applied"`
}

// ReplayContradictionEntry queues a contradiction for explicit user review.
// These do NOT auto-apply — the user must adjudicate via `jm daydream review`
// or by editing the buffer entry directly. Reviewed=true marks adjudicated.
type ReplayContradictionEntry struct {
	Timestamp        time.Time `json:"timestamp"`
	StableMemoryPath string    `json:"stable_memory_path"`
	RecentSeedPath   string    `json:"recent_seed_path"`
	Reasoning        string    `json:"reasoning,omitempty"`
	Reviewed         bool      `json:"reviewed"`
}

// verdictKey matches "Verdict: <word>" or "Relationship: <word>" with
// tolerance for markdown emphasis (`**Verdict:** refine`, `*Verdict: refine*`,
// etc.). Up-to-two asterisks around the key word, the colon side, and the
// verdict word are accepted. Plain `Verdict: refine` continues to match.
var verdictKey = regexp.MustCompile(`(?i)(?:^|\n)\s*\*{0,2}\s*(?:verdict|relationship)\s*\*{0,2}\s*:\s*\*{0,2}\s*(reinforce|refine|contradict|unrelated)\b`)

// ParseReplayVerdict extracts the verdict from an agent response.
//
// **Last-wins semantics:** when the response contains multiple verdict
// lines (e.g., the agent drafted one verdict, reasoned more, and revised
// to another), the LAST match wins. This matches the prompt instruction to
// "end your response text with a single line `Verdict: <word>`" — the
// final line is the agent's commitment, not whatever it considered first.
//
// Returns ErrVerdictNotFound when no parseable line exists. We don't fall
// through to keyword-fishing because every verdict word commonly appears
// in reasoning text and false-positive routing is worse than no routing.
func ParseReplayVerdict(response string) (ReplayVerdict, error) {
	matches := verdictKey.FindAllStringSubmatch(response, -1)
	if len(matches) == 0 {
		return "", ErrVerdictNotFound
	}
	last := matches[len(matches)-1]
	return ReplayVerdict(strings.ToLower(last[1])), nil
}

// RouteReplayResult dispatches a parsed verdict to the correct audit/queue
// files. ALL verdicts go into Metrics/replay_log.jsonl (audit trail).
// Reinforce queues a confidence delta. Contradict queues a critical-priority
// review item. Refine and Unrelated have no routing beyond the audit log —
// refine's breadcrumb (written by the agent) carries the rest of the state;
// unrelated has nothing to do.
//
// The reasoning argument is the agent's response text (or a relevant slice).
// Empty pair returns an error — replay routing requires both seeds.
func RouteReplayResult(vault string, cfg Config, now time.Time, verdict ReplayVerdict, pair *SeedPair, reasoning string) error {
	if pair == nil {
		return errors.New("autodream: replay routing requires a SeedPair")
	}

	if err := appendReplayLog(vault, ReplayLogEntry{
		Timestamp:  now,
		Verdict:    string(verdict),
		RecentPath: pair.Recent.FilePath,
		StablePath: pair.Stable.FilePath,
		Reasoning:  truncateResponse(reasoning, 500),
	}); err != nil {
		return fmt.Errorf("replay log: %w", err)
	}

	switch verdict {
	case VerdictReinforce:
		return appendReplayReinforcement(vault, ReplayReinforcementEntry{
			Timestamp:        now,
			StableMemoryPath: pair.Stable.FilePath,
			RecentSeedPath:   pair.Recent.FilePath,
			ConfidenceDelta:  cfg.ConfidenceReinforce,
			Reasoning:        truncateResponse(reasoning, 500),
			Applied:          false,
		})
	case VerdictContradict:
		return appendReplayContradiction(vault, ReplayContradictionEntry{
			Timestamp:        now,
			StableMemoryPath: pair.Stable.FilePath,
			RecentSeedPath:   pair.Recent.FilePath,
			Reasoning:        truncateResponse(reasoning, 500),
			Reviewed:         false,
		})
	case VerdictRefine, VerdictUnrelated:
		// No further routing. Refine: the agent's breadcrumb carries the
		// substance; consolidation handles it via priority=high. Unrelated:
		// nothing to do beyond the audit record.
		return nil
	default:
		return fmt.Errorf("autodream: unknown verdict %q", verdict)
	}
}

func appendReplayLog(vault string, entry ReplayLogEntry) error {
	path := filepath.Join(vault, "Metrics", "replay_log.jsonl")
	if cfg := LoadConfig(vault); cfg.AutoDaydreamLogRotationThreshold > 0 {
		if err := rotateJSONLIfNeeded(path, cfg.AutoDaydreamLogRotationThreshold, entry.Timestamp); err != nil {
			// Non-fatal — the append below still happens.
			fmt.Fprintf(os.Stderr, "[autodream] replay log rotation: %v\n", err)
		}
	}
	return appendJSONL(path, entry)
}

func appendReplayReinforcement(vault string, entry ReplayReinforcementEntry) error {
	return appendJSONL(filepath.Join(vault, "Metrics", "replay_reinforcements.jsonl"), entry)
}

func appendReplayContradiction(vault string, entry ReplayContradictionEntry) error {
	return appendJSONL(filepath.Join(vault, "Metrics", "replay_contradictions.jsonl"), entry)
}

// appendJSONL writes one JSON-encoded record + newline to path, creating
// the parent directory and the file if needed. Single shared helper so all
// three replay-output files behave identically (same atomicity guarantees,
// same error shape).
func appendJSONL(path string, record any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(path), err)
	}
	line, err := json.Marshal(record)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open %s: %w", path, err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write %s: %w", path, err)
	}
	return nil
}
