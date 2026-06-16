package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"math/rand/v2"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// AutodreamLogEntry is one record in Metrics/autodream_log.jsonl.
//
// Decision is one of: "fired" | "skipped" | "dry-run" | "error". Reason is a
// human-readable explanation, especially load-bearing for the "skipped" case
// (jitter, daily cap, activity, master toggle, etc.) so the user can tune
// thresholds against actual telemetry.
//
// NextFireMinutes is the jitter target rolled at the time of THIS fire — it
// determines the earliest moment the NEXT fire is allowed. Stored on the
// entry so the next polling can read it back without re-rolling.
type AutodreamLogEntry struct {
	Timestamp       time.Time              `json:"timestamp"`
	Mode            string                 `json:"mode,omitempty"`
	Strategy        string                 `json:"strategy,omitempty"`
	Decision        string                 `json:"decision"`
	Reason          string                 `json:"reason,omitempty"`
	Seed            *SeedRecord            `json:"seed,omitempty"`
	Pair            *PairRecord            `json:"pair,omitempty"`
	FellBack        bool                   `json:"fell_back,omitempty"`
	DurationMS      int64                  `json:"duration_ms,omitempty"`
	NextFireMinutes int                    `json:"next_fire_minutes,omitempty"`
	AgentResponse   string                 `json:"agent_response,omitempty"` // truncated; full content lives in Buffer/Daydream/
	Transport       string                 `json:"transport,omitempty"`
	OverrideFlags   []string               `json:"override_flags,omitempty"`
	BufferPressure  *BufferPressureRecord  `json:"buffer_pressure,omitempty"`
	StrategyRoll    *StrategyRollRecord    `json:"strategy_roll,omitempty"`
	SkipCategory    string                 `json:"skip_category,omitempty"`
}

// Skip categories — written to AutodreamLogEntry.SkipCategory when
// Decision == "skipped" or when an error short-circuits the run. The reason
// string remains the human-readable diagnostic; the category is the
// machine-friendly bucket for counting and tuning.
//
// Empty string means "ran without skipping" — which is the normal state for
// decision="fired" or "dry-run".
const (
	SkipMasterDisabled       = "master_disabled"
	SkipModeResolveError     = "mode_resolve_error"
	SkipActivityRecent       = "activity_recent"
	SkipJitterPending        = "jitter_pending"
	SkipCapActiveReached     = "cap_active_reached"
	SkipCapQuietReached      = "cap_quiet_reached"
	SkipSeedUnavailable      = "seed_unavailable"
	SkipPromptBuildError     = "prompt_build_error"
	SkipInvokerMissing       = "invoker_missing"
	SkipInvokerError         = "invoker_error"
	SkipStrategyForcedNoPair   = "strategy_forced_no_pair"
	SkipStrategyInvalid        = "strategy_invalid"
	SkipVolleyCommitment       = "volley_commitment"
	SkipSchedulerHostUnavailable = "scheduler_host_unavailable"
)

// BufferPressureRecord captures the integration backlog at fire time:
// how many non-daydream buffer entries are awaiting consolidation, against
// the configured threshold. Logged at top level (not inside strategy_roll)
// because it's universally meaningful — active mode runs report it too,
// even though they don't roll a strategy.
type BufferPressureRecord struct {
	Count     int     `json:"count"`
	Threshold int     `json:"threshold"`
	FillRatio float64 `json:"fill_ratio"`
}

// StrategyRollRecord captures the inputs and outputs of strategy resolution
// for quiet-mode runs (active mode is always exploration, no roll). Surfaces
// what the diceroll got, what was actually selected after the conditional
// fallback, and the seed-pool sizes that determined whether fallback fired.
//
// PairTagOverlap is the Jaccard overlap between the recent and stable tag
// sets, set only when the run actually produced a pair (replay strategy
// without fallback). 0 when not applicable. Lets analysis distinguish
// "unrelated verdict from genuinely distant pairing" from "unrelated verdict
// despite adjacent tag space" — the latter is a much more informative signal.
type StrategyRollRecord struct {
	Rolled            string  `json:"rolled"`
	Selected          string  `json:"selected"`
	FellBack          bool    `json:"fell_back"`
	ExplorationWeight float64 `json:"exploration_weight"`
	ReplayWeight      float64 `json:"replay_weight"`
	RecentCandidates  int     `json:"recent_candidates"`
	StableCandidates  int     `json:"stable_candidates"`
	PairTagOverlap    float64 `json:"pair_tag_overlap,omitempty"`
}

type SeedRecord struct {
	Source string `json:"source"`
	Title  string `json:"title"`
	Path   string `json:"path"`
}

type PairRecord struct {
	Recent SeedRecord `json:"recent"`
	Stable SeedRecord `json:"stable"`
}

// AutodreamInvoker is the function signature the orchestrator uses to call
// the daydream agent. Production code wraps the `claude -p` CLI; tests pass
// a fake that records inputs and returns canned responses. Returning the
// transport name in the same call lets the orchestrator log which path was
// taken without separately probing it.
type AutodreamInvoker func(prompt string) (response string, transport string, err error)

// AutodreamSnapshotFn captures activation state before the run touches
// memories. Injected so production wires it to LoadAndCaptureSnapshot +
// WriteActivationSnapshot while tests pass a recorder or no-op.
//
// CONTRACT: this function MUST be read-only with respect to memory state.
// It runs just before resolveStrategyAndSeed, which currently uses
// LoadAllMemories without mutating last_accessed/access_count. If memory
// loading is ever extended to update access fields, the snapshot silently
// shifts from pre-run baseline to mid-run contamination — at which point
// this contract has been violated and the tuning data goes downhill.
type AutodreamSnapshotFn func(now time.Time) error

// AutodreamRunInputs is the parameter bag for one orchestrator invocation.
// Pure inputs — the function reads no globals.
type AutodreamRunInputs struct {
	VaultRoot        string
	Cfg              Config
	Now              time.Time
	Force            bool
	NoCap            bool
	DryRun           bool
	ModeOverride     string // "" | "active" | "quiet"
	StrategyOverride string // "" | "exploration" | "replay"
	Rand             *rand.Rand
	Invoker          AutodreamInvoker
	SnapshotFn       AutodreamSnapshotFn // optional; nil means no snapshot
}

// AutodreamRunResult is what the orchestrator decided + the artifacts it
// produced. The CLI wrapper renders it to stdout and the JSONL log.
type AutodreamRunResult struct {
	Decision        string
	Reason          string
	Mode            AutodreamMode
	Strategy        AutodreamStrategy
	FellBack        bool
	Seed            *Seed
	Pair            *SeedPair
	Prompt          string
	Response        string
	Transport       string
	DurationMS      int64
	NextFireMinutes int
	OverrideFlags   []string
	BufferPressure  *BufferPressureRecord
	StrategyRoll    *StrategyRollRecord
	SkipCategory    string
}

const (
	decisionFired       = "fired"
	decisionSkipped     = "skipped"
	decisionDryRun      = "dry-run"
	decisionError       = "error"
	decisionInstalled   = "installed"
	decisionUninstalled = "uninstalled"
)

// cmdAutodream is the CLI entry point. Parses flags, calls RunAutodream,
// appends a log entry, and prints a single-line summary.
//
// Subcommand dispatch: `jm autodream stats [...]` is a separate report that
// reads the telemetry streams; everything else is the run path. Stats has
// to come first because flag.FlagSet would otherwise treat "stats" as a
// positional arg and error out.
func cmdAutodream(vaultRoot string, args []string) {
	if len(args) > 0 && args[0] == "stats" {
		cmdAutodreamStats(vaultRoot, args[1:])
		return
	}
	fs := flag.NewFlagSet("autodream", flag.ExitOnError)
	force := fs.Bool("force", false, "Bypass jitter timing")
	noCap := fs.Bool("no-cap", false, "Bypass daily cap")
	modeFlag := fs.String("mode", "", "Force mode (active|quiet)")
	strategyFlag := fs.String("strategy", "", "Force strategy (exploration|replay)")
	dryRun := fs.Bool("dry-run", false, "Build prompt but skip claude invocation")
	install := fs.Bool("install", false, "Install Windows Task Scheduler entry (every --interval-minutes)")
	uninstall := fs.Bool("uninstall", false, "Remove the Windows Task Scheduler entry")
	intervalMinutes := fs.Int("interval-minutes", 15, "Polling cadence for the scheduler entry (used with --install)")
	fs.Parse(args)

	if *install || *uninstall {
		runScheduleAdmin(vaultRoot, *install, *uninstall, *intervalMinutes)
		return
	}

	cfg := LoadConfig(vaultRoot)
	now := time.Now()

	inputs := AutodreamRunInputs{
		VaultRoot:        vaultRoot,
		Cfg:              cfg,
		Now:              now,
		Force:            *force,
		NoCap:            *noCap,
		DryRun:           *dryRun,
		ModeOverride:     *modeFlag,
		StrategyOverride: *strategyFlag,
		Rand:             rand.New(rand.NewPCG(uint64(now.UnixNano()), uint64(now.Unix()+1))),
		Invoker:          invokeClaudeCLIDaydream(vaultRoot),
		SnapshotFn:       productionSnapshotFn(vaultRoot, cfg.AutoDaydreamLogRotationThreshold),
	}

	result := RunAutodream(inputs)

	if err := appendAutodreamLog(vaultRoot, result, now); err != nil {
		fmt.Fprintf(os.Stderr, "[jm autodream] log write: %v\n", err)
	}

	printAutodreamSummary(os.Stdout, result, *dryRun)
}

// RunAutodream is the testable orchestrator. Given a fully-populated input
// struct (config, now, overrides, rand, invoker), it returns a complete
// result describing what it decided to do, why, and what the agent
// produced. Pure: no global state, no os.Exit, no log writes.
func RunAutodream(in AutodreamRunInputs) AutodreamRunResult {
	res := AutodreamRunResult{Decision: decisionSkipped}
	res.OverrideFlags = collectOverrideFlags(in)

	// Master toggle. --force or --dry-run can bypass for testing.
	if !in.Cfg.AutoDaydreamEnabled && !in.Force && !in.DryRun {
		res.Reason = "auto_daydream_enabled is false"
		res.SkipCategory = SkipMasterDisabled
		return res
	}

	// Mode resolution (with override)
	mode, err := resolveModeForRun(in)
	if err != nil {
		res.Decision = decisionError
		res.Reason = "mode resolve: " + err.Error()
		res.SkipCategory = SkipModeResolveError
		return res
	}
	res.Mode = mode

	// Scheduler host — hard skip when preferred host has no headless invoker.
	if !in.Force && !in.DryRun {
		if reason, skip := checkSchedulerHostSkip(in); skip {
			res.Reason = reason
			res.SkipCategory = SkipSchedulerHostUnavailable
			logSchedulerDispatch(in, in.Cfg.DaydreamSchedulerHost, "skipped", reason, SkipSchedulerHostUnavailable)
			return res
		}
	}

	// Volley commitment — defer while an in-session nudge is pending.
	if !in.Force {
		if reason, skip := checkVolleyCommitmentSkip(in); skip {
			res.Reason = reason
			res.SkipCategory = SkipVolleyCommitment
			logSchedulerDispatch(in, in.Cfg.DaydreamSchedulerHost, "skipped", reason, SkipVolleyCommitment)
			return res
		}
	}

	// Activity-based skip — orthogonal to jitter; cheap to check first.
	if !in.Force {
		if reason, skip := checkActivitySkip(in, mode); skip {
			res.Reason = reason
			res.SkipCategory = SkipActivityRecent
			logSchedulerDispatch(in, in.Cfg.DaydreamSchedulerHost, "skipped", reason, SkipActivityRecent)
			return res
		}
	}

	// Jitter check — use the last "fired" entry's NextFireMinutes
	if !in.Force {
		if reason, skip := checkJitterSkip(in); skip {
			res.Reason = reason
			res.SkipCategory = SkipJitterPending
			return res
		}
	}

	// Daily cap check
	if !in.NoCap && !in.Force {
		if reason, skip := checkDailyCap(in, mode); skip {
			res.Reason = reason
			if mode == ModeQuiet {
				res.SkipCategory = SkipCapQuietReached
			} else {
				res.SkipCategory = SkipCapActiveReached
			}
			return res
		}
	}

	// Pre-run activation snapshot. Fires once we've decided this run is going
	// to attempt a fire (all skip checks passed) but BEFORE resolveStrategyAndSeed
	// touches LoadAllMemories. The snapshot's timestamp matches the autodream_log
	// entry's timestamp (both use in.Now), so the two streams join 1-to-1.
	// Failures are stderr-only — losing one snapshot must not block the run.
	if in.SnapshotFn != nil {
		if err := in.SnapshotFn(in.Now); err != nil {
			fmt.Fprintf(os.Stderr, "[autodream] snapshot: %v\n", err)
		}
	}

	// Buffer pressure — computed once before strategy resolution and stashed
	// at top level. Universally meaningful (active mode reports it too) but
	// it's also the input that drives quiet-mode strategy weighting, so the
	// strategy_roll record references the same value rather than duplicating.
	if pressure, perr := ComputeBufferPressure(in.VaultRoot, in.Cfg.BufferThreshold); perr == nil {
		res.BufferPressure = &BufferPressureRecord{
			Count:     pressure.NonDaydreamCount,
			Threshold: pressure.Threshold,
			FillRatio: pressure.FillRatio,
		}
	}

	// Strategy + seed sampling
	resolution := resolveStrategyAndSeed(in, mode)
	if resolution.Err != nil {
		res.Reason = resolution.Err.Error()
		res.SkipCategory = resolution.SkipCategory
		return res
	}
	res.Strategy = resolution.Strategy
	res.Seed = resolution.Seed
	res.Pair = resolution.Pair
	res.FellBack = resolution.FellBack
	res.StrategyRoll = resolution.Roll

	// Build prompt
	template := pickTemplate(mode, resolution.Strategy)
	prompt, err := BuildPrompt(template, PromptInputs{
		Mode:      mode,
		Strategy:  resolution.Strategy,
		Seed:      resolution.Seed,
		Pair:      resolution.Pair,
		VaultRoot: in.VaultRoot,
		Now:       in.Now,
	})
	if err != nil {
		res.Decision = decisionError
		res.Reason = "prompt build: " + err.Error()
		res.SkipCategory = SkipPromptBuildError
		return res
	}
	res.Prompt = prompt

	// Roll the next-fire target so the next polling has a fresh interval.
	res.NextFireMinutes = rollJitterInterval(in.Cfg, in.Rand)

	if in.DryRun {
		res.Decision = decisionDryRun
		return res
	}

	// Live invocation
	if in.Invoker == nil {
		res.Decision = decisionError
		res.Reason = "no invoker configured"
		res.SkipCategory = SkipInvokerMissing
		return res
	}
	start := time.Now()
	response, transport, err := in.Invoker(prompt)
	res.DurationMS = time.Since(start).Milliseconds()
	res.Transport = transport
	if err != nil {
		res.Decision = decisionError
		res.Reason = "invoke: " + err.Error()
		res.SkipCategory = SkipInvokerError
		return res
	}
	res.Response = truncateResponse(response, 500)
	res.Decision = decisionFired
	logSchedulerDispatch(in, in.Cfg.DaydreamSchedulerHost, "fired", describeRun(res), "")

	// For replay strategies, parse the verdict line and route to the
	// appropriate audit/queue file. Failures here are recorded on the result
	// but don't downgrade the decision — the breadcrumb file (if the agent
	// wrote one) is still authoritative, and consolidation will pick it up.
	if res.Strategy == StrategyReplay && res.Pair != nil {
		verdict, parseErr := ParseReplayVerdict(response)
		if parseErr != nil {
			res.Reason = "verdict-parse-failed: " + parseErr.Error()
		} else {
			if err := RouteReplayResult(in.VaultRoot, in.Cfg, in.Now, verdict, res.Pair, response); err != nil {
				res.Reason = fmt.Sprintf("verdict %s, routing warning: %v", verdict, err)
			} else {
				res.Reason = "verdict: " + string(verdict)
			}
		}
	}
	return res
}

func collectOverrideFlags(in AutodreamRunInputs) []string {
	var out []string
	if in.Force {
		out = append(out, "force")
	}
	if in.NoCap {
		out = append(out, "no-cap")
	}
	if in.DryRun {
		out = append(out, "dry-run")
	}
	if in.ModeOverride != "" {
		out = append(out, "mode="+in.ModeOverride)
	}
	if in.StrategyOverride != "" {
		out = append(out, "strategy="+in.StrategyOverride)
	}
	return out
}

func resolveModeForRun(in AutodreamRunInputs) (AutodreamMode, error) {
	switch strings.ToLower(strings.TrimSpace(in.ModeOverride)) {
	case "active":
		return ModeActive, nil
	case "quiet":
		return ModeQuiet, nil
	case "":
		return ResolveMode(in.Now, in.Cfg.AutoDaydreamQuietHours, in.Cfg.AutoDaydreamQuietHoursTimezone)
	default:
		return ModeActive, fmt.Errorf("unknown --mode value %q", in.ModeOverride)
	}
}

func checkSchedulerHostSkip(in AutodreamRunInputs) (reason string, skip bool) {
	host := normalizeSchedulerHost(in.Cfg.DaydreamSchedulerHost)
	if SchedulerHostAvailable(host) {
		return "", false
	}
	policy := in.Cfg.DaydreamSchedulerMissingInvoker
	if policy == "" {
		policy = DaydreamSchedulerMissingInvokerSkip
	}
	if policy != DaydreamSchedulerMissingInvokerSkip {
		return "", false
	}
	return fmt.Sprintf("scheduler host %q has no headless invoker (daydream_scheduler_missing_invoker: skip)", host), true
}

func checkVolleyCommitmentSkip(in AutodreamRunInputs) (reason string, skip bool) {
	pending, err := PendingVolleyCommitments(in.VaultRoot, in.Now)
	if err != nil || len(pending) == 0 {
		return "", false
	}
	if len(pending) == 1 {
		c := pending[0]
		return fmt.Sprintf("volley commitment pending for %s (%s)", c.SessionID, c.RuntimeHost), true
	}
	return fmt.Sprintf("volley commitments pending (%d sessions)", len(pending)), true
}

func checkActivitySkip(in AutodreamRunInputs, mode AutodreamMode) (reason string, skip bool) {
	window := skipWindowForMode(in.Cfg, mode)
	if window <= 0 {
		return "", false
	}

	if activitySourceEnabled(in.Cfg.AutoDaydreamActivitySources, "heartbeat") {
		sessions, err := ReadActiveSessions(in.VaultRoot, window, in.Now)
		if err == nil && len(sessions) > 0 {
			return formatActiveSessionsReason(sessions, in.Now), true
		}
	}

	state, _ := ReadActivityState(in.VaultRoot, in.Cfg.AutoDaydreamActivitySources)
	skipNow, age := ShouldSkipForActivity(state, window, in.Now)
	if !skipNow {
		return "", false
	}
	return fmt.Sprintf("activity %dm ago (window %dm)", int(age.Minutes()), window), true
}

func activitySourceEnabled(sources []string, name string) bool {
	for _, s := range sources {
		if s == name {
			return true
		}
	}
	return false
}

func logSchedulerDispatch(in AutodreamRunInputs, hostPreferred, decision, reason, skipCategory string) {
	_ = appendDaydreamDispatchLog(in.VaultRoot, DaydreamDispatchLogEntry{
		Timestamp:     in.Now,
		Channel:       "scheduled",
		HostPreferred: normalizeSchedulerHost(hostPreferred),
		Decision:      decision,
		Reason:        reason,
		SkipCategory:  skipCategory,
	})
}

func skipWindowForMode(cfg Config, mode AutodreamMode) int {
	if mode == ModeQuiet {
		return cfg.AutoDaydreamQuietSkipWindowMinutes
	}
	return cfg.AutoDaydreamActiveSkipWindowMinutes
}

func checkJitterSkip(in AutodreamRunInputs) (reason string, skip bool) {
	last, target, err := findLastFire(in.VaultRoot)
	if err != nil || last.IsZero() {
		return "", false
	}
	if target <= 0 {
		return "", false
	}
	elapsed := in.Now.Sub(last)
	if elapsed >= target {
		return "", false
	}
	return fmt.Sprintf("interval not elapsed (target %dm, elapsed %dm)",
		int(target.Minutes()), int(elapsed.Minutes())), true
}

func checkDailyCap(in AutodreamRunInputs, mode AutodreamMode) (reason string, skip bool) {
	cap := in.Cfg.AutoDaydreamMaxPerDayActive
	if mode == ModeQuiet {
		cap = in.Cfg.AutoDaydreamMaxPerDayQuiet
	}
	if cap <= 0 {
		return "", false
	}
	count, _ := countTodayFires(in.VaultRoot, mode, in.Now)
	if count < cap {
		return "", false
	}
	return fmt.Sprintf("daily cap reached (%d/%d for %s)", count, cap, mode), true
}

// strategyResolution bundles the output of resolveStrategyAndSeed so the
// orchestrator can plumb the diagnostic StrategyRollRecord through to the
// log without a six-return-value signature.
type strategyResolution struct {
	Strategy     AutodreamStrategy
	Seed         *Seed
	Pair         *SeedPair
	FellBack     bool
	Roll         *StrategyRollRecord // nil for active mode; populated for quiet mode
	Err          error
	SkipCategory string // populated alongside Err for skip-bucket tagging
}

func resolveStrategyAndSeed(in AutodreamRunInputs, mode AutodreamMode) strategyResolution {
	if mode == ModeActive {
		// Active mode is always exploration with recency-biased seed; no roll.
		s, err := PickSeed(in.VaultRoot, in.Cfg.AutoDaydreamActiveSeedSources, true, in.Rand)
		if err != nil {
			return strategyResolution{
				Err:          fmt.Errorf("no seed available: %w", err),
				SkipCategory: SkipSeedUnavailable,
			}
		}
		return strategyResolution{Strategy: StrategyExploration, Seed: &s}
	}

	// Quiet mode — count seed pools BEFORE attempting to build a pair so the
	// log can attribute fell_back=true to "no recent" vs "no stable" vs
	// "rolled exploration". The counts are diagnostic-only; the actual pair
	// builder still has authoritative say over availability.
	recentN, stableN := CountReplayPoolCandidates(in.VaultRoot, in.Cfg, in.Now)

	pair, pairErr := BuildReplayPair(in.VaultRoot, in.Cfg, in.Now, in.Rand)
	hasPair := pairErr == nil

	pressure, _ := ComputeBufferPressure(in.VaultRoot, in.Cfg.BufferThreshold)
	expWeight, repWeight := ComputeAdaptiveWeights(in.Cfg, pressure)

	roll := &StrategyRollRecord{
		ExplorationWeight: expWeight,
		ReplayWeight:      repWeight,
		RecentCandidates:  recentN,
		StableCandidates:  stableN,
	}

	switch strings.ToLower(strings.TrimSpace(in.StrategyOverride)) {
	case "exploration":
		s, err := PickSeed(in.VaultRoot, in.Cfg.AutoDaydreamQuietExplorationSeedSources, false, in.Rand)
		if err != nil {
			return strategyResolution{
				Err:          fmt.Errorf("no seed available: %w", err),
				SkipCategory: SkipSeedUnavailable,
			}
		}
		roll.Rolled = string(StrategyExploration) // override is bookkept as forced
		roll.Selected = string(StrategyExploration)
		return strategyResolution{Strategy: StrategyExploration, Seed: &s, Roll: roll}
	case "replay":
		if !hasPair {
			return strategyResolution{
				Err:          fmt.Errorf("strategy=replay forced but no pair available"),
				SkipCategory: SkipStrategyForcedNoPair,
			}
		}
		roll.Rolled = string(StrategyReplay)
		roll.Selected = string(StrategyReplay)
		roll.PairTagOverlap = ComputePairTagOverlap(pair.Recent.FilePath, pair.Stable.FilePath)
		return strategyResolution{Strategy: StrategyReplay, Pair: &pair, Roll: roll}
	case "":
		// Normal roll path
		decision := ResolveStrategy(in.Cfg, pressure, hasPair, in.Rand)
		roll.Rolled = string(decision.Rolled)
		roll.Selected = string(decision.Selected)
		roll.FellBack = decision.FellBack
		if decision.Selected == StrategyReplay {
			roll.PairTagOverlap = ComputePairTagOverlap(pair.Recent.FilePath, pair.Stable.FilePath)
			return strategyResolution{Strategy: StrategyReplay, Pair: &pair, FellBack: decision.FellBack, Roll: roll}
		}
		s, err := PickSeed(in.VaultRoot, in.Cfg.AutoDaydreamQuietExplorationSeedSources, false, in.Rand)
		if err != nil {
			return strategyResolution{
				Err:          fmt.Errorf("no seed available: %w", err),
				SkipCategory: SkipSeedUnavailable,
			}
		}
		return strategyResolution{Strategy: StrategyExploration, Seed: &s, FellBack: decision.FellBack, Roll: roll}
	default:
		return strategyResolution{
			Err:          fmt.Errorf("unknown --strategy value %q", in.StrategyOverride),
			SkipCategory: SkipStrategyInvalid,
		}
	}
}

func pickTemplate(mode AutodreamMode, strategy AutodreamStrategy) PromptTemplate {
	if mode == ModeActive {
		return TemplateActive
	}
	if strategy == StrategyReplay {
		return TemplateQuietReplay
	}
	return TemplateQuietExploration
}

// rollJitterInterval picks a uniform-random duration in
// [interval_min_minutes, interval_max_minutes]. If min >= max, returns min.
// A zero or negative result means "no jitter required" — next polling can
// fire freely.
func rollJitterInterval(cfg Config, r *rand.Rand) int {
	mn := cfg.AutoDaydreamIntervalMinMinutes
	mx := cfg.AutoDaydreamIntervalMaxMinutes
	if mn <= 0 && mx <= 0 {
		return 0
	}
	if mn >= mx {
		return mn
	}
	span := mx - mn + 1
	if r != nil {
		return mn + r.IntN(span)
	}
	return mn + rand.IntN(span)
}

// findLastFire scans Metrics/autodream_log.jsonl forward (cheap for a small
// rotated file) and returns the timestamp + rolled NextFireMinutes of the
// most recent "fired" entry. Returns zero values when no fire has occurred.
func findLastFire(vaultRoot string) (time.Time, time.Duration, error) {
	path := filepath.Join(vaultRoot, "Metrics", "autodream_log.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return time.Time{}, 0, nil
		}
		return time.Time{}, 0, err
	}
	defer f.Close()

	var lastTS time.Time
	var lastTarget time.Duration

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry AutodreamLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Decision != decisionFired {
			continue
		}
		if entry.Timestamp.After(lastTS) {
			lastTS = entry.Timestamp
			lastTarget = time.Duration(entry.NextFireMinutes) * time.Minute
		}
	}
	return lastTS, lastTarget, nil
}

// countTodayFires counts log entries with decision="fired" and matching mode
// whose timestamp falls within now's local-timezone calendar day.
func countTodayFires(vaultRoot string, mode AutodreamMode, now time.Time) (int, error) {
	path := filepath.Join(vaultRoot, "Metrics", "autodream_log.jsonl")
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	loc := now.Location()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	dayEnd := dayStart.Add(24 * time.Hour)

	count := 0
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 1024*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		var entry AutodreamLogEntry
		if err := json.Unmarshal(line, &entry); err != nil {
			continue
		}
		if entry.Decision != decisionFired {
			continue
		}
		if entry.Mode != string(mode) {
			continue
		}
		ts := entry.Timestamp.In(loc)
		if ts.Before(dayStart) || !ts.Before(dayEnd) {
			continue
		}
		count++
	}
	return count, nil
}

// appendAutodreamLog writes one JSONL record describing the run. Failures
// are returned to the caller for stderr logging — they don't propagate as
// an exit code, since losing a single log entry shouldn't stop autodream
// from completing other work.
func appendAutodreamLog(vaultRoot string, res AutodreamRunResult, now time.Time) error {
	dir := filepath.Join(vaultRoot, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir Metrics: %w", err)
	}
	path := filepath.Join(dir, "autodream_log.jsonl")

	if cfg := LoadConfig(vaultRoot); cfg.AutoDaydreamLogRotationThreshold > 0 {
		if err := rotateJSONLIfNeeded(path, cfg.AutoDaydreamLogRotationThreshold, now); err != nil {
			fmt.Fprintf(os.Stderr, "[autodream] log rotation: %v\n", err)
		}
	}

	entry := AutodreamLogEntry{
		Timestamp:       now,
		Mode:            string(res.Mode),
		Strategy:        string(res.Strategy),
		Decision:        res.Decision,
		Reason:          res.Reason,
		FellBack:        res.FellBack,
		DurationMS:      res.DurationMS,
		NextFireMinutes: res.NextFireMinutes,
		AgentResponse:   res.Response,
		Transport:       res.Transport,
		OverrideFlags:   res.OverrideFlags,
		BufferPressure:  res.BufferPressure,
		StrategyRoll:    res.StrategyRoll,
		SkipCategory:    res.SkipCategory,
	}
	if res.Seed != nil {
		entry.Seed = &SeedRecord{Source: res.Seed.Source, Title: res.Seed.Title, Path: res.Seed.FilePath}
	}
	if res.Pair != nil {
		entry.Pair = &PairRecord{
			Recent: SeedRecord{Source: res.Pair.Recent.Source, Title: res.Pair.Recent.Title, Path: res.Pair.Recent.FilePath},
			Stable: SeedRecord{Source: res.Pair.Stable.Source, Title: res.Pair.Stable.Title, Path: res.Pair.Stable.FilePath},
		}
	}

	line, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("marshal log entry: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write log entry: %w", err)
	}
	return nil
}

func printAutodreamSummary(w *os.File, res AutodreamRunResult, dryRun bool) {
	switch res.Decision {
	case decisionFired:
		desc := describeRun(res)
		fmt.Fprintf(w, "[autodream] fired: %s (%dms via %s)\n", desc, res.DurationMS, res.Transport)
	case decisionSkipped:
		fmt.Fprintf(w, "[autodream] skipped: %s\n", res.Reason)
	case decisionDryRun:
		fmt.Fprintf(w, "[autodream] dry-run: %s\n", describeRun(res))
		fmt.Fprintln(w, "--- prompt ---")
		fmt.Fprintln(w, res.Prompt)
	case decisionError:
		fmt.Fprintf(w, "[autodream] error: %s\n", res.Reason)
	case decisionInstalled:
		fmt.Fprintf(w, "[autodream] installed: %s\n", res.Reason)
	case decisionUninstalled:
		fmt.Fprintf(w, "[autodream] uninstalled: %s\n", res.Reason)
	}
	if dryRun && res.Decision != decisionDryRun {
		// dry-run flag was set but we exited before the dry-run branch (e.g.,
		// disabled toggle without --force). Still useful to surface why.
		fmt.Fprintf(w, "  (dry-run requested but exited before render: %s)\n", res.Reason)
	}
}

// runScheduleAdmin handles --install / --uninstall. Wraps the schtasks
// invocations in the same log + summary shape that RunAutodream uses, so
// schedule administration shows up alongside fire records in the audit
// trail.
func runScheduleAdmin(vaultRoot string, install, uninstall bool, intervalMinutes int) {
	if err := requireWindows(); err != nil {
		fmt.Fprintf(os.Stderr, "[autodream] %v\n", err)
		os.Exit(1)
	}

	now := time.Now()
	res := AutodreamRunResult{}

	switch {
	case install && uninstall:
		fmt.Fprintln(os.Stderr, "[autodream] --install and --uninstall are mutually exclusive")
		os.Exit(1)
	case install:
		out, err := installAutodreamSchedule(intervalMinutes)
		if err != nil {
			res.Decision = decisionError
			res.Reason = "install: " + err.Error()
		} else {
			res.Decision = decisionInstalled
			res.Reason = fmt.Sprintf("scheduled task %q every %dm", autodreamTaskName, intervalMinutes)
			if out != "" {
				res.Reason += " (" + truncateResponse(out, 80) + ")"
			}
		}
	case uninstall:
		out, err := uninstallAutodreamSchedule()
		if err != nil {
			res.Decision = decisionError
			res.Reason = "uninstall: " + err.Error()
		} else {
			res.Decision = decisionUninstalled
			res.Reason = fmt.Sprintf("removed scheduled task %q", autodreamTaskName)
			if out != "" {
				res.Reason += " (" + truncateResponse(out, 80) + ")"
			}
		}
	}

	if err := appendAutodreamLog(vaultRoot, res, now); err != nil {
		fmt.Fprintf(os.Stderr, "[autodream] log write: %v\n", err)
	}
	printAutodreamSummary(os.Stdout, res, false)
}

func describeRun(res AutodreamRunResult) string {
	parts := []string{string(res.Mode), string(res.Strategy)}
	if res.FellBack {
		parts = append(parts, "fell-back")
	}
	if res.Seed != nil {
		parts = append(parts, fmt.Sprintf("seed=%s/%s", res.Seed.Source, res.Seed.Title))
	}
	if res.Pair != nil {
		parts = append(parts, fmt.Sprintf("pair=%s+%s", res.Pair.Recent.Title, res.Pair.Stable.Title))
	}
	return strings.Join(parts, " ")
}

// invokeClaudeCLIDaydream returns an AutodreamInvoker that calls
// `claude -p` with the daydream agent's instructions inlined as a preamble.
// We don't go through the direct Anthropic API because the daydream agent
// uses Read/Glob/Grep/WebSearch/Write tools — those are only available in
// the Claude Code runtime, not in the bare API.
//
// Sandbox handling: by default Claude Code's print mode restricts tool
// access to the cwd AND denies file-writing tools (Write/Edit) without
// per-call permission prompts. We need the agent to:
//
//  1. Read seed files and write breadcrumbs anywhere under the vault →
//     `cmd.Dir = vaultRoot` (project context) + `--add-dir <vaultRoot>`
//     (explicit tool-access grant).
//  2. Write breadcrumb files without a prompt that nobody is there to
//     accept → `--permission-mode acceptEdits` auto-accepts Write/Edit.
//
// History:
//   - First live fire (2026-05-01): "sandbox restrictions prevent me from
//     reading the required files" — fixed by adding cmd.Dir + --add-dir.
//   - First scheduled fires (2026-05-02): agent runs but writes "Write tool
//     was denied. Providing inline fallback" — breadcrumbs not persisted.
//     Fixed by adding --permission-mode acceptEdits.
func invokeClaudeCLIDaydream(vaultRoot string) AutodreamInvoker {
	return func(prompt string) (string, string, error) {
		claudeBin, err := exec.LookPath("claude")
		if err != nil {
			return "", "disabled", fmt.Errorf("claude CLI not available: %w", err)
		}

		preamble := loadDaydreamAgentBody(vaultRoot)
		full := preamble
		if full != "" {
			full += "\n\n--- RUN-SPECIFIC INPUT ---\n\n"
		}
		full += prompt

		// 5-minute ceiling — daydream agents can take a bit, especially with
		// web search; but we don't want a hung CLI to wedge the cron forever.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		cmd := exec.CommandContext(ctx, claudeBin,
			"--add-dir", vaultRoot,
			"--permission-mode", "acceptEdits",
			"-p", full,
		)
		cmd.Dir = vaultRoot
		// Mark this claude session as autodream-spawned so the hook layer
		// can suppress its session_heartbeat write. Without this, every
		// autodream fire triggers a SessionStart hook that writes a
		// heartbeat which then suppresses the next ~4 polls via
		// activity_recent skip — a self-throttling endogeneity loop. The
		// env var is inherited by the spawned `claude` process and read
		// by `jm hook session-start` / `jm hook user-prompt-submit`.
		cmd.Env = append(os.Environ(), "LJM_AUTODREAM_INVOCATION=1")
		// Suppress console window on Windows (CREATE_NO_WINDOW). On Unix,
		// detach from the controlling terminal via Setsid.
		detachSysProcAttr(cmd)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr

		if err := cmd.Run(); err != nil {
			return "", "cli", fmt.Errorf("claude CLI: %w (stderr: %s)", err, truncateResponse(stderr.String(), 200))
		}
		return strings.TrimSpace(stdout.String()), "cli", nil
	}
}

// productionSnapshotFn returns the AutodreamSnapshotFn used by the CLI: it
// loads all memories, captures the pre-run activation snapshot, and appends
// it to Metrics/autodream_activation_snapshots.jsonl. Honors the same log
// rotation threshold as the other autodream JSONL streams.
func productionSnapshotFn(vaultRoot string, rotationThreshold int) AutodreamSnapshotFn {
	return func(now time.Time) error {
		snap, loadErr := LoadAndCaptureSnapshot(vaultRoot, now)
		// Write whatever we captured, even on partial-load errors — partial
		// state is more informative than no snapshot when correlating runs.
		if writeErr := WriteActivationSnapshot(vaultRoot, snap, rotationThreshold); writeErr != nil {
			return writeErr
		}
		return loadErr
	}
}

// loadDaydreamAgentBody reads Agents/memory-daydream.md and returns the body
// (frontmatter stripped). Returns empty string if the file is unavailable —
// in that case the prompt alone has to carry the agent's behavior.
func loadDaydreamAgentBody(vaultRoot string) string {
	path := filepath.Join(vaultRoot, "Agents", "memory-daydream.md")
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	body := string(data)
	// Strip YAML frontmatter
	if strings.HasPrefix(body, "---\n") {
		if end := strings.Index(body[4:], "\n---\n"); end >= 0 {
			body = body[4+end+5:]
		}
	}
	return strings.TrimSpace(body)
}

// truncateResponse cuts a string at maxLen with an ellipsis marker, used for
// log-line trimming so the JSONL doesn't grow unbounded with full agent
// responses (the breadcrumb file holds the full content).
func truncateResponse(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + " …"
}
