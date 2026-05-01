package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// ConsolidationOutcome records what consolidation decided for a single
// daydream-sourced buffer entry. One JSONL line per daydream assessment per
// consolidation pass; written to Metrics/consolidation_outcomes.jsonl.
//
// The point of this stream is to close the loop on the value judge: did
// "valuable" verdicts correlate with downstream LTM promotion? It's the
// contamination-free signal flagged in the endogeneity buffer entry —
// consolidation outcomes are user-decision-driven (manually-edited LTM,
// manually-applied promotions), not autodream-self-inflated.
//
// AttributionDegraded fires when the delta between the daydream fire and
// this consolidation pass exceeds AttributionDegradedThresholdHours. Beyond
// that window, other daydream runs likely touched the same memory regions,
// so attributing this outcome cleanly to the original fire overstates
// confidence. Outcomes with attribution_degraded=true should be treated as
// weak evidence, not sharp signal.
type ConsolidationOutcome struct {
	Timestamp            time.Time `json:"timestamp"`
	SourceEntry          string    `json:"source_entry"`
	SourcePath           string    `json:"source_path"`
	DaydreamKind         string    `json:"daydream_kind,omitempty"`
	Action               string    `json:"action"`
	Reason               string    `json:"reason,omitempty"`
	RetentionScore       float64   `json:"retention_score"`
	ValueVerdict         string    `json:"value_verdict,omitempty"`
	ValueReason          string    `json:"value_reason,omitempty"`
	Redundancy           float64   `json:"redundancy"`
	RedundancyVerdict    string    `json:"redundancy_verdict,omitempty"`
	DaydreamFireAt       time.Time `json:"daydream_fire_at,omitempty"`
	HoursSinceFire       float64   `json:"hours_since_fire,omitempty"`
	AttributionDegraded  bool      `json:"attribution_degraded,omitempty"`
}

// AttributionDegradedThresholdHours is the cutoff beyond which a daydream
// fire's downstream consolidation outcome can no longer be cleanly
// attributed to the originating run. Empirical default — twelve hours
// covers a typical sleep/work cycle during which other fires typically
// occur. Tunable later via Config if it turns out to be miscalibrated.
const AttributionDegradedThresholdHours = 12.0

// WriteConsolidationOutcomes appends one JSONL record per daydream-sourced
// buffer assessment to Metrics/consolidation_outcomes.jsonl. Non-daydream
// entries are skipped — this stream exists to support tuning of the
// daydream-specific signals (value judge, strategy mix), not as a general
// consolidation audit.
//
// Failures return to the caller for stderr logging; partial writes are
// preferable to dropping the whole pass.
func WriteConsolidationOutcomes(vaultRoot string, assessments []BufferAssessment, now time.Time) error {
	dir := filepath.Join(vaultRoot, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir Metrics: %w", err)
	}
	path := filepath.Join(dir, "consolidation_outcomes.jsonl")

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open outcomes log: %w", err)
	}
	defer f.Close()

	for _, a := range assessments {
		if a.Entry == nil || !IsDaydreamSourced(a.Entry) {
			continue
		}
		outcome := buildOutcomeRecord(a, now)
		line, err := json.Marshal(outcome)
		if err != nil {
			return fmt.Errorf("marshal outcome for %q: %w", a.Entry.FileName, err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write outcome for %q: %w", a.Entry.FileName, err)
		}
	}
	return nil
}

func buildOutcomeRecord(a BufferAssessment, now time.Time) ConsolidationOutcome {
	out := ConsolidationOutcome{
		Timestamp:         now,
		SourceEntry:       a.Entry.FileName,
		SourcePath:        a.Entry.FilePath,
		DaydreamKind:      a.Entry.DaydreamKind,
		Action:            string(a.Action),
		Reason:            a.Reason,
		RetentionScore:    a.RetentionScore,
		ValueVerdict:      string(a.DaydreamValueVerdict),
		ValueReason:       a.DaydreamValueReason,
		Redundancy:        a.Redundancy,
		RedundancyVerdict: a.DaydreamVerdict,
	}

	if !a.Entry.Timestamp.IsZero() {
		out.DaydreamFireAt = a.Entry.Timestamp
		hours := now.Sub(a.Entry.Timestamp).Hours()
		out.HoursSinceFire = hours
		if hours > AttributionDegradedThresholdHours {
			out.AttributionDegraded = true
		}
	}
	return out
}
