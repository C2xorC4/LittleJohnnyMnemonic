package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ReviewActionRecord is one user-driven decision in the daydream review CLI.
// Written to Metrics/daydream_review_log.jsonl after each action.
//
// This is the contamination-free signal flagged in the endogeneity buffer
// entry: every other instrumentation stream the autodream system produces
// reflects autodream's own state. The review log reflects user judgment —
// it's the only signal that's structurally independent of the system's
// self-reinforcing loops, and therefore the most trustworthy input for
// tuning the value judge and strategy mix.
type ReviewActionRecord struct {
	Timestamp    time.Time `json:"timestamp"`
	EntryFile    string    `json:"entry_file"`
	EntryPath    string    `json:"entry_path,omitempty"`
	DaydreamKind string    `json:"daydream_kind,omitempty"`
	Priority     string    `json:"priority,omitempty"`
	QueueType    string    `json:"queue_type,omitempty"` // "critical" | "exploration" | "refine" | ""
	Action       string    `json:"action"`               // "accept" | "refine" | "reject" | "promote" | "skip" | "quit"
	Success      bool      `json:"success"`
	Surprise     float64   `json:"surprise,omitempty"`
	AgeHours     float64   `json:"age_hours,omitempty"`
}

// AppendReviewActionLog is the production writer. Failures fall through to
// stderr so a logging glitch never blocks the user's review flow — the
// action itself has already been applied at this point.
func AppendReviewActionLog(vaultRoot string, rec ReviewActionRecord) error {
	dir := filepath.Join(vaultRoot, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir Metrics: %w", err)
	}
	path := filepath.Join(dir, "daydream_review_log.jsonl")

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal review action: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open review log: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write review action: %w", err)
	}
	return nil
}

// buildReviewActionRecord assembles a record from the loop's per-iteration
// state. queueTypeFor() derives the queue from the entry attributes — it's
// a heuristic mapping, not authoritative. The review CLI doesn't currently
// surface explicit queues to the user; this is implicit categorization for
// post-hoc analysis only.
func buildReviewActionRecord(entry *BufferEntry, action string, success bool, now time.Time) ReviewActionRecord {
	rec := ReviewActionRecord{
		Timestamp: now,
		Action:    action,
		Success:   success,
	}
	if entry == nil {
		return rec
	}
	rec.EntryFile = entry.FileName
	rec.EntryPath = entry.FilePath
	rec.DaydreamKind = entry.DaydreamKind
	rec.Priority = entry.Priority
	rec.QueueType = queueTypeFor(entry)
	rec.Surprise = entry.Surprise
	if !entry.Timestamp.IsZero() {
		rec.AgeHours = now.Sub(entry.Timestamp).Hours()
	}
	return rec
}

// queueTypeFor maps an entry to a queue label for the review log.
//   - "critical": replay-contradict or priority=critical (immune to drop, requires user adjudication)
//   - "refine":   replay-refine (consolidation has retention bonus and confirmed integration)
//   - "exploration": exploration kind (default daydream output)
//   - "" empty: not classifiable from the entry alone
func queueTypeFor(entry *BufferEntry) string {
	if entry == nil {
		return ""
	}
	if entry.DaydreamKind == "replay-contradict" || strings.EqualFold(entry.Priority, "critical") {
		return "critical"
	}
	switch entry.DaydreamKind {
	case "replay-refine":
		return "refine"
	case "exploration":
		return "exploration"
	}
	return ""
}
