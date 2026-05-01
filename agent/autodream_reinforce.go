package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ProcessReplayReinforcements drains Metrics/replay_reinforcements.jsonl,
// applies each pending entry's confidence delta to its target memory, and
// archives the file so the next run starts fresh.
//
// Returns counts of applied entries and skipped entries (target memory not
// found, parse error, etc.) plus any fatal error. Skips do not block the
// archive — losing a stale reinforcement is acceptable; corrupting the
// queue forever is not.
//
// The archive path mirrors the rotation strategy from Task #13:
// Metrics/Archive/replay_reinforcements.{timestamp}.jsonl. This preserves
// the audit trail for post-hoc analysis.
func ProcessReplayReinforcements(vaultRoot string, now time.Time) (applied int, skipped int, err error) {
	path := filepath.Join(vaultRoot, "Metrics", "replay_reinforcements.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, 0, nil
		}
		return 0, 0, fmt.Errorf("open reinforcements: %w", err)
	}

	var entries []ReplayReinforcementEntry
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		var entry ReplayReinforcementEntry
		if uerr := json.Unmarshal(scanner.Bytes(), &entry); uerr != nil {
			skipped++
			continue
		}
		entries = append(entries, entry)
	}
	f.Close()

	if len(entries) == 0 {
		return 0, skipped, nil
	}

	for _, e := range entries {
		if e.Applied {
			continue
		}
		if aerr := applyReinforcement(e); aerr != nil {
			fmt.Fprintf(os.Stderr, "  [reinforce] skip %s: %v\n", filepath.Base(e.StableMemoryPath), aerr)
			skipped++
			continue
		}
		applied++
	}

	// Archive the file. Even when nothing applied (all entries skipped),
	// archive so the queue doesn't grow unbounded.
	archive := filepath.Join(vaultRoot, "Metrics", "Archive",
		fmt.Sprintf("replay_reinforcements.%s.jsonl", now.Format("20060102-150405")))
	if mkErr := os.MkdirAll(filepath.Dir(archive), 0o755); mkErr != nil {
		return applied, skipped, fmt.Errorf("mkdir archive: %w", mkErr)
	}
	if rnErr := os.Rename(path, archive); rnErr != nil {
		return applied, skipped, fmt.Errorf("archive reinforcements: %w", rnErr)
	}

	return applied, skipped, nil
}

// applyReinforcement reads the target memory, bumps confidence by the
// queued delta (capped at 1.0), and writes it back. Returns an error if
// the target file is missing or unwritable — the caller decides whether
// that's fatal (it's not — we just skip and move on).
func applyReinforcement(e ReplayReinforcementEntry) error {
	if e.StableMemoryPath == "" {
		return fmt.Errorf("empty stable_memory_path")
	}
	m, err := ParseMemoryEntry(e.StableMemoryPath)
	if err != nil {
		return fmt.Errorf("load %s: %w", e.StableMemoryPath, err)
	}
	if m.Archived != nil {
		// Don't reinforce an archived memory — the user moved it for a reason.
		return fmt.Errorf("target archived: %s", e.StableMemoryPath)
	}
	m.Confidence += e.ConfidenceDelta
	if m.Confidence > 1.0 {
		m.Confidence = 1.0
	}
	return WriteMemoryEntry(m)
}
