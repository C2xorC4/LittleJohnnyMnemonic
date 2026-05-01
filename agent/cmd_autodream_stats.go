package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// `jm autodream stats` summarizes the autodream telemetry streams. By design
// it labels metrics as either:
//
//   - CONTAMINATION-FREE — comes from user judgment (review CLI accept/reject)
//     or downstream consolidation outcomes that the user manually approved.
//     Trustworthy as a tuning input.
//
//   - SELF-INFLATED — comes from autodream's own logs (verdict distribution,
//     activation snapshots, strategy mix). The autodream system is both the
//     producer and the consumer of these signals; tuning against them risks
//     fitting parameters to the system's own feedback loops rather than
//     measuring real value. Useful as a diagnostic; treat as qualitative
//     reference, not optimization target.
//
// See Buffer/2026-05-01_tuning-under-endogeneity-robust-heuristics-not-optimization.md.

// AutodreamStatsReport is the aggregated output of the stats subcommand. The
// renderer walks the structure top-to-bottom; each section tags its own
// contamination class explicitly.
type AutodreamStatsReport struct {
	WindowDays  int
	WindowStart time.Time
	WindowEnd   time.Time

	Activity         ActivitySummary         // self-inflated (counts of system's own runs)
	Strategy         StrategySummary         // self-inflated
	Verdicts         VerdictSummary          // self-inflated
	Activation       ActivationSummary       // self-inflated
	Outcomes         OutcomeSummary          // mixed: depends on user-driven consolidation
	Review           ReviewSummary           // contamination-free (user judgment)
}

type ActivitySummary struct {
	TotalRecords      int
	FiredActive       int
	FiredQuiet        int
	DryRuns           int
	Errors            int
	Skipped           int
	SkipsByCategory   map[string]int
	MeanDurationMS    int64
}

type StrategySummary struct {
	QuietRuns          int
	RolledExploration  int
	RolledReplay       int
	SelectedExploration int
	SelectedReplay     int
	FellBack           int
	MeanRecentPool     float64
	MeanStablePool     float64
	MeanPairTagOverlap float64
	NPairs             int
}

type VerdictSummary struct {
	Total          int
	Reinforce      int
	Refine         int
	Contradict     int
	Unrelated      int
	OverlapByVerdict map[string]float64 // mean pair_tag_overlap per verdict
}

type ActivationSummary struct {
	NumSnapshots   int
	MeanActivation float64
	P95Activation  float64
	MaxTopNStable  int // count of memories that appear in TopN across at least 2 snapshots in window
}

type OutcomeSummary struct {
	Total                    int
	Promoted                 int
	Held                     int
	Discarded                int
	AttributionDegradedCount int
	ValueVerdictDistribution map[string]int
	PromotionRateByValue     map[string]float64 // promote_count / total_for_verdict
}

type ReviewSummary struct {
	Total           int
	ByAction        map[string]int
	ByQueue         map[string]int
	AcceptanceByKind map[string]float64 // (accept+promote)/total per daydream_kind
}

// AggregateAutodreamStats reads all telemetry streams and returns the report.
// Errors from individual streams are surfaced as zero-counts rather than
// fatal — a corrupted snapshot file shouldn't prevent stats from rendering
// the rest of the picture.
func AggregateAutodreamStats(vaultRoot string, windowDays int, now time.Time) AutodreamStatsReport {
	report := AutodreamStatsReport{
		WindowDays: windowDays,
		WindowEnd:  now,
	}
	if windowDays > 0 {
		report.WindowStart = now.Add(-time.Duration(windowDays) * 24 * time.Hour)
	}

	logEntries, _ := loadAutodreamLogJSONL(filepath.Join(vaultRoot, "Metrics", "autodream_log.jsonl"))
	logEntries = filterByWindow(logEntries, report.WindowStart)
	report.Activity = aggregateActivity(logEntries)
	report.Strategy = aggregateStrategy(logEntries)

	replayEntries, _ := loadReplayLogJSONL(filepath.Join(vaultRoot, "Metrics", "replay_log.jsonl"))
	replayEntries = filterReplayByWindow(replayEntries, report.WindowStart)
	report.Verdicts = aggregateVerdicts(replayEntries, logEntries)

	snapshots, _ := loadSnapshotsJSONL(filepath.Join(vaultRoot, "Metrics", "autodream_activation_snapshots.jsonl"))
	snapshots = filterSnapshotsByWindow(snapshots, report.WindowStart)
	report.Activation = aggregateActivation(snapshots)

	outcomes, _ := loadOutcomesJSONL(filepath.Join(vaultRoot, "Metrics", "consolidation_outcomes.jsonl"))
	outcomes = filterOutcomesByWindow(outcomes, report.WindowStart)
	report.Outcomes = aggregateOutcomes(outcomes)

	reviews, _ := loadReviewLogJSONL(filepath.Join(vaultRoot, "Metrics", "daydream_review_log.jsonl"))
	reviews = filterReviewsByWindow(reviews, report.WindowStart)
	report.Review = aggregateReview(reviews)

	return report
}

// ─── JSONL loaders ────────────────────────────────────────────────────────

func loadAutodreamLogJSONL(path string) ([]AutodreamLogEntry, error) {
	return loadJSONLGeneric[AutodreamLogEntry](path)
}

type replayLogRecord struct {
	Timestamp  time.Time `json:"timestamp"`
	Verdict    string    `json:"verdict"`
	RecentPath string    `json:"recent_path"`
	StablePath string    `json:"stable_path"`
}

func loadReplayLogJSONL(path string) ([]replayLogRecord, error) {
	return loadJSONLGeneric[replayLogRecord](path)
}

func loadSnapshotsJSONL(path string) ([]ActivationSnapshot, error) {
	return loadJSONLGeneric[ActivationSnapshot](path)
}

func loadOutcomesJSONL(path string) ([]ConsolidationOutcome, error) {
	return loadJSONLGeneric[ConsolidationOutcome](path)
}

func loadReviewLogJSONL(path string) ([]ReviewActionRecord, error) {
	return loadJSONLGeneric[ReviewActionRecord](path)
}

func loadJSONLGeneric[T any](path string) ([]T, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var out []T
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec T
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		out = append(out, rec)
	}
	return out, scanner.Err()
}

// ─── Window filters ───────────────────────────────────────────────────────

func filterByWindow(in []AutodreamLogEntry, start time.Time) []AutodreamLogEntry {
	if start.IsZero() {
		return in
	}
	out := make([]AutodreamLogEntry, 0, len(in))
	for _, e := range in {
		if e.Timestamp.Before(start) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func filterReplayByWindow(in []replayLogRecord, start time.Time) []replayLogRecord {
	if start.IsZero() {
		return in
	}
	out := make([]replayLogRecord, 0, len(in))
	for _, e := range in {
		if e.Timestamp.Before(start) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func filterSnapshotsByWindow(in []ActivationSnapshot, start time.Time) []ActivationSnapshot {
	if start.IsZero() {
		return in
	}
	out := make([]ActivationSnapshot, 0, len(in))
	for _, e := range in {
		if e.Timestamp.Before(start) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func filterOutcomesByWindow(in []ConsolidationOutcome, start time.Time) []ConsolidationOutcome {
	if start.IsZero() {
		return in
	}
	out := make([]ConsolidationOutcome, 0, len(in))
	for _, e := range in {
		if e.Timestamp.Before(start) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func filterReviewsByWindow(in []ReviewActionRecord, start time.Time) []ReviewActionRecord {
	if start.IsZero() {
		return in
	}
	out := make([]ReviewActionRecord, 0, len(in))
	for _, e := range in {
		if e.Timestamp.Before(start) {
			continue
		}
		out = append(out, e)
	}
	return out
}

// ─── Aggregators ──────────────────────────────────────────────────────────

func aggregateActivity(entries []AutodreamLogEntry) ActivitySummary {
	s := ActivitySummary{
		SkipsByCategory: make(map[string]int),
	}
	var sumDuration int64
	var firedCount int
	for _, e := range entries {
		s.TotalRecords++
		switch e.Decision {
		case decisionFired:
			if e.Mode == string(ModeQuiet) {
				s.FiredQuiet++
			} else {
				s.FiredActive++
			}
			sumDuration += e.DurationMS
			firedCount++
		case decisionDryRun:
			s.DryRuns++
		case decisionError:
			s.Errors++
		case decisionSkipped:
			s.Skipped++
		}
		if e.SkipCategory != "" {
			s.SkipsByCategory[e.SkipCategory]++
		}
	}
	if firedCount > 0 {
		s.MeanDurationMS = sumDuration / int64(firedCount)
	}
	return s
}

func aggregateStrategy(entries []AutodreamLogEntry) StrategySummary {
	s := StrategySummary{}
	var sumRecent, sumStable, sumOverlap int
	var sumOverlapVal float64
	for _, e := range entries {
		if e.Mode != string(ModeQuiet) || e.StrategyRoll == nil {
			continue
		}
		s.QuietRuns++
		switch e.StrategyRoll.Rolled {
		case string(StrategyExploration):
			s.RolledExploration++
		case string(StrategyReplay):
			s.RolledReplay++
		}
		switch e.StrategyRoll.Selected {
		case string(StrategyExploration):
			s.SelectedExploration++
		case string(StrategyReplay):
			s.SelectedReplay++
		}
		if e.StrategyRoll.FellBack {
			s.FellBack++
		}
		sumRecent += e.StrategyRoll.RecentCandidates
		sumStable += e.StrategyRoll.StableCandidates
		if e.StrategyRoll.PairTagOverlap > 0 || e.StrategyRoll.Selected == string(StrategyReplay) {
			sumOverlap++
			sumOverlapVal += e.StrategyRoll.PairTagOverlap
		}
	}
	if s.QuietRuns > 0 {
		s.MeanRecentPool = float64(sumRecent) / float64(s.QuietRuns)
		s.MeanStablePool = float64(sumStable) / float64(s.QuietRuns)
	}
	if sumOverlap > 0 {
		s.MeanPairTagOverlap = sumOverlapVal / float64(sumOverlap)
		s.NPairs = sumOverlap
	}
	return s
}

func aggregateVerdicts(replays []replayLogRecord, logs []AutodreamLogEntry) VerdictSummary {
	s := VerdictSummary{
		OverlapByVerdict: make(map[string]float64),
	}
	// Build a lookup from replay timestamp → log entry's pair_tag_overlap so
	// we can correlate verdict ↔ overlap. Replays log on the same timestamp
	// the autodream_log used for that fire.
	overlapByTS := make(map[time.Time]float64)
	for _, e := range logs {
		if e.StrategyRoll != nil && e.StrategyRoll.PairTagOverlap > 0 {
			overlapByTS[e.Timestamp] = e.StrategyRoll.PairTagOverlap
		}
	}

	overlapSums := make(map[string]float64)
	overlapCounts := make(map[string]int)

	for _, r := range replays {
		s.Total++
		switch strings.ToLower(r.Verdict) {
		case "reinforce":
			s.Reinforce++
		case "refine":
			s.Refine++
		case "contradict":
			s.Contradict++
		case "unrelated":
			s.Unrelated++
		}
		if ov, ok := overlapByTS[r.Timestamp]; ok {
			overlapSums[r.Verdict] += ov
			overlapCounts[r.Verdict]++
		}
	}
	for v, sum := range overlapSums {
		if overlapCounts[v] > 0 {
			s.OverlapByVerdict[v] = sum / float64(overlapCounts[v])
		}
	}
	return s
}

func aggregateActivation(snapshots []ActivationSnapshot) ActivationSummary {
	s := ActivationSummary{NumSnapshots: len(snapshots)}
	if len(snapshots) == 0 {
		return s
	}
	var sumMean, sumP95 float64
	for _, snap := range snapshots {
		sumMean += snap.Stats.Mean
		sumP95 += snap.Stats.P95
	}
	s.MeanActivation = sumMean / float64(len(snapshots))
	s.P95Activation = sumP95 / float64(len(snapshots))

	// Stability of TopN: count titles that appear in 2+ snapshots' top-N.
	if len(snapshots) >= 2 {
		seen := make(map[string]int)
		for _, snap := range snapshots {
			titles := make(map[string]bool)
			for _, m := range snap.TopN {
				titles[m.Title] = true
			}
			for t := range titles {
				seen[t]++
			}
		}
		for _, n := range seen {
			if n >= 2 {
				s.MaxTopNStable++
			}
		}
	}
	return s
}

func aggregateOutcomes(outcomes []ConsolidationOutcome) OutcomeSummary {
	s := OutcomeSummary{
		ValueVerdictDistribution: make(map[string]int),
		PromotionRateByValue:     make(map[string]float64),
	}
	promoteByValue := make(map[string]int)
	totalByValue := make(map[string]int)

	for _, o := range outcomes {
		s.Total++
		switch o.Action {
		case string(ActionPromote):
			s.Promoted++
		case string(ActionHold), string(ActionHoldRateSep):
			s.Held++
		case string(ActionDiscard):
			s.Discarded++
		}
		if o.AttributionDegraded {
			s.AttributionDegradedCount++
		}
		if o.ValueVerdict != "" {
			s.ValueVerdictDistribution[o.ValueVerdict]++
			totalByValue[o.ValueVerdict]++
			if o.Action == string(ActionPromote) {
				promoteByValue[o.ValueVerdict]++
			}
		}
	}
	for v, total := range totalByValue {
		s.PromotionRateByValue[v] = float64(promoteByValue[v]) / float64(total)
	}
	return s
}

func aggregateReview(reviews []ReviewActionRecord) ReviewSummary {
	s := ReviewSummary{
		ByAction:         make(map[string]int),
		ByQueue:          make(map[string]int),
		AcceptanceByKind: make(map[string]float64),
	}
	acceptByKind := make(map[string]int)
	totalByKind := make(map[string]int)

	for _, r := range reviews {
		s.Total++
		s.ByAction[r.Action]++
		if r.QueueType != "" {
			s.ByQueue[r.QueueType]++
		}
		if r.DaydreamKind != "" {
			totalByKind[r.DaydreamKind]++
			if r.Action == "accept" || r.Action == "promote" {
				acceptByKind[r.DaydreamKind]++
			}
		}
	}
	for k, total := range totalByKind {
		s.AcceptanceByKind[k] = float64(acceptByKind[k]) / float64(total)
	}
	return s
}

// ─── Renderer ─────────────────────────────────────────────────────────────

func RenderAutodreamStats(w io.Writer, r AutodreamStatsReport) {
	fmt.Fprintf(w, "=== jm autodream stats — last %d days ===\n", r.WindowDays)
	if !r.WindowStart.IsZero() {
		fmt.Fprintf(w, "Window: %s → %s\n\n",
			r.WindowStart.Format("2006-01-02 15:04"),
			r.WindowEnd.Format("2006-01-02 15:04"))
	}

	// Activity (self-inflated counts but useful for orientation)
	fmt.Fprintln(w, "── Activity (self-inflated counts) ──")
	fmt.Fprintf(w, "  Total log records: %d\n", r.Activity.TotalRecords)
	fmt.Fprintf(w, "  Fired: %d (active=%d, quiet=%d), dry-runs=%d, errors=%d, skipped=%d\n",
		r.Activity.FiredActive+r.Activity.FiredQuiet,
		r.Activity.FiredActive, r.Activity.FiredQuiet,
		r.Activity.DryRuns, r.Activity.Errors, r.Activity.Skipped)
	if r.Activity.MeanDurationMS > 0 {
		fmt.Fprintf(w, "  Mean fire duration: %dms\n", r.Activity.MeanDurationMS)
	}
	if len(r.Activity.SkipsByCategory) > 0 {
		fmt.Fprintln(w, "  Skips by category:")
		for _, k := range sortedKeys(r.Activity.SkipsByCategory) {
			fmt.Fprintf(w, "    %-30s %d\n", k, r.Activity.SkipsByCategory[k])
		}
	}
	fmt.Fprintln(w)

	// Strategy mix (self-inflated)
	if r.Strategy.QuietRuns > 0 {
		fmt.Fprintln(w, "── Strategy mix (self-inflated) ──")
		fmt.Fprintf(w, "  Quiet runs: %d\n", r.Strategy.QuietRuns)
		fmt.Fprintf(w, "  Rolled:    exploration=%d  replay=%d\n",
			r.Strategy.RolledExploration, r.Strategy.RolledReplay)
		fmt.Fprintf(w, "  Selected:  exploration=%d  replay=%d  (fell-back=%d)\n",
			r.Strategy.SelectedExploration, r.Strategy.SelectedReplay, r.Strategy.FellBack)
		fmt.Fprintf(w, "  Mean pool sizes: recent=%.1f, stable=%.1f\n",
			r.Strategy.MeanRecentPool, r.Strategy.MeanStablePool)
		if r.Strategy.NPairs > 0 {
			fmt.Fprintf(w, "  Mean pair tag overlap (n=%d): %.3f\n",
				r.Strategy.NPairs, r.Strategy.MeanPairTagOverlap)
		}
		fmt.Fprintln(w)
	}

	// Verdict distribution (self-inflated; warn loudly)
	if r.Verdicts.Total > 0 {
		fmt.Fprintln(w, "── Verdict distribution (SELF-INFLATED — diagnostic only) ──")
		fmt.Fprintf(w, "  Total replay verdicts: %d\n", r.Verdicts.Total)
		fmt.Fprintf(w, "  reinforce=%d  refine=%d  contradict=%d  unrelated=%d\n",
			r.Verdicts.Reinforce, r.Verdicts.Refine, r.Verdicts.Contradict, r.Verdicts.Unrelated)
		if len(r.Verdicts.OverlapByVerdict) > 0 {
			fmt.Fprintln(w, "  Mean pair tag overlap by verdict:")
			for _, v := range sortedKeys(r.Verdicts.OverlapByVerdict) {
				fmt.Fprintf(w, "    %-12s %.3f\n", v, r.Verdicts.OverlapByVerdict[v])
			}
			fmt.Fprintln(w, "  (high overlap + unrelated verdict = adjacent-but-no-bridge — most informative)")
		}
		fmt.Fprintln(w)
	}

	// Activation drift (self-inflated)
	if r.Activation.NumSnapshots > 0 {
		fmt.Fprintln(w, "── Activation drift (self-inflated) ──")
		fmt.Fprintf(w, "  Snapshots: %d\n", r.Activation.NumSnapshots)
		fmt.Fprintf(w, "  Mean activation (mean of snapshot means): %.3f\n", r.Activation.MeanActivation)
		fmt.Fprintf(w, "  Mean p95 activation: %.3f\n", r.Activation.P95Activation)
		fmt.Fprintf(w, "  TopN-stable memories (appear across ≥2 snapshots): %d\n", r.Activation.MaxTopNStable)
		fmt.Fprintln(w)
	}

	// Consolidation outcomes (mixed)
	if r.Outcomes.Total > 0 {
		fmt.Fprintln(w, "── Consolidation outcomes (mixed: depends on user-driven consolidation) ──")
		fmt.Fprintf(w, "  Total daydream entries processed: %d\n", r.Outcomes.Total)
		fmt.Fprintf(w, "  Promoted=%d  Held=%d  Discarded=%d\n",
			r.Outcomes.Promoted, r.Outcomes.Held, r.Outcomes.Discarded)
		if r.Outcomes.AttributionDegradedCount > 0 {
			fmt.Fprintf(w, "  Attribution-degraded (>%dh since fire): %d\n",
				int(AttributionDegradedThresholdHours), r.Outcomes.AttributionDegradedCount)
		}
		if len(r.Outcomes.ValueVerdictDistribution) > 0 {
			fmt.Fprintln(w, "  Value verdict → promotion rate:")
			for _, v := range sortedKeys(r.Outcomes.ValueVerdictDistribution) {
				count := r.Outcomes.ValueVerdictDistribution[v]
				rate := r.Outcomes.PromotionRateByValue[v]
				fmt.Fprintf(w, "    %-12s n=%-4d  %.0f%% promoted\n", v, count, rate*100)
			}
		}
		fmt.Fprintln(w)
	}

	// Review actions (CONTAMINATION-FREE — load-bearing)
	if r.Review.Total > 0 {
		fmt.Fprintln(w, "── Review actions (CONTAMINATION-FREE — most trustworthy tuning input) ──")
		fmt.Fprintf(w, "  Total review actions: %d\n", r.Review.Total)
		if len(r.Review.ByAction) > 0 {
			fmt.Fprintln(w, "  By action:")
			for _, k := range sortedKeys(r.Review.ByAction) {
				fmt.Fprintf(w, "    %-12s %d\n", k, r.Review.ByAction[k])
			}
		}
		if len(r.Review.ByQueue) > 0 {
			fmt.Fprintln(w, "  By queue:")
			for _, k := range sortedKeys(r.Review.ByQueue) {
				fmt.Fprintf(w, "    %-12s %d\n", k, r.Review.ByQueue[k])
			}
		}
		if len(r.Review.AcceptanceByKind) > 0 {
			fmt.Fprintln(w, "  Acceptance rate (accept+promote / total) by kind:")
			for _, k := range sortedKeys(r.Review.AcceptanceByKind) {
				fmt.Fprintf(w, "    %-20s %.0f%%\n", k, r.Review.AcceptanceByKind[k]*100)
			}
		}
		fmt.Fprintln(w)
	} else {
		fmt.Fprintln(w, "── Review actions ──")
		fmt.Fprintln(w, "  (no review actions in window — run `jm daydream review` to start producing the contamination-free signal)")
		fmt.Fprintln(w)
	}
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// cmdAutodreamStats parses flags and renders the report.
func cmdAutodreamStats(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("autodream stats", flag.ExitOnError)
	days := fs.Int("days", 7, "Window size in days (0 = all-time)")
	asJSON := fs.Bool("json", false, "Output JSON instead of formatted text")
	fs.Parse(args)

	report := AggregateAutodreamStats(vaultRoot, *days, time.Now())

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			fmt.Fprintf(os.Stderr, "[!] %v\n", err)
			os.Exit(1)
		}
		return
	}
	RenderAutodreamStats(os.Stdout, report)
}

// formatPercent rounds to one decimal — used in aggregation tests; keeps the
// renderer code paths consistent with what tests assert.
func formatPercent(v float64) string {
	if math.IsNaN(v) {
		return "—"
	}
	return fmt.Sprintf("%.1f%%", v*100)
}
