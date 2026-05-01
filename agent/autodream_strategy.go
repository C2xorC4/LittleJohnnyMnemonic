package main

import (
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"strings"
)

// AutodreamStrategy is the sub-strategy selected for a quiet-mode daydream.
// Active mode always runs as exploration; this distinction only matters in
// quiet mode where the scheduler chooses between dream-like wandering and
// CLS-style interleaved replay.
type AutodreamStrategy string

const (
	StrategyExploration AutodreamStrategy = "exploration"
	StrategyReplay      AutodreamStrategy = "replay"
)

// BufferPressure captures the integration pressure on the memory system —
// how full the short-term buffer is relative to its consolidation threshold.
// Used by adaptive replay weighting (post-initial-testing): higher pressure
// suggests more material is waiting to be integrated and replay should be
// favored.
type BufferPressure struct {
	NonDaydreamCount int     // .md files directly under Buffer/, excluding Buffer/Daydream/
	Threshold        int     // configured buffer_threshold
	FillRatio        float64 // count / threshold, capped at 1.0; 0 if threshold <= 0
}

// StrategyDecision is the resolved strategy plus diagnostic context. Callers
// use Selected; FellBack and Rolled exist so the orchestrator can log a
// useful reason ("replay rolled but no pair available, falling back").
type StrategyDecision struct {
	Selected          AutodreamStrategy
	Rolled            AutodreamStrategy
	FellBack          bool
	ExplorationWeight float64
	ReplayWeight      float64
}

// ComputeBufferPressure scans Buffer/ (excluding Buffer/Daydream/) and
// returns the count plus fill ratio against the configured threshold.
// Missing buffer directory is treated as zero pressure.
func ComputeBufferPressure(vaultRoot string, threshold int) (BufferPressure, error) {
	bufferDir := filepath.Join(vaultRoot, "Buffer")
	count := 0

	err := filepath.WalkDir(bufferDir, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			if os.IsNotExist(walkErr) {
				return nil
			}
			return walkErr
		}
		if d.IsDir() {
			if path != bufferDir && d.Name() == "Daydream" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(d.Name(), ".md") {
			count++
		}
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return BufferPressure{}, err
	}

	fill := 0.0
	if threshold > 0 {
		fill = float64(count) / float64(threshold)
		if fill > 1.0 {
			fill = 1.0
		}
	}
	return BufferPressure{
		NonDaydreamCount: count,
		Threshold:        threshold,
		FillRatio:        fill,
	}, nil
}

// ComputeAdaptiveWeights returns the effective exploration and replay
// weights to roll against. With AutoDaydreamStrategyAdaptive=false the base
// weights pass through unchanged. With adaptive=true, this is where buffer-
// pressure-driven shifting will live — the math is intentionally NOT
// implemented in v1; we ship the parameter wiring and revisit after
// initial-testing data tells us how aggressively to shift.
func ComputeAdaptiveWeights(cfg Config, pressure BufferPressure) (exploration, replay float64) {
	exploration = cfg.AutoDaydreamStrategyExplorationBase
	replay = cfg.AutoDaydreamStrategyReplayBase

	if !cfg.AutoDaydreamStrategyAdaptive {
		return exploration, replay
	}

	// TODO(post-initial-testing): adaptive shift driven by buffer pressure.
	// Sketch:
	//   multiplier := 1 + cfg.AutoDaydreamStrategyBufferPressureFactor * pressure.FillRatio
	//   replay *= multiplier
	//   then normalize so exploration + replay == base sum (preserves overall weight).
	// Not enabled until we have real data on whether the conditional fallback
	// in ResolveStrategy is sufficient on its own.
	_ = pressure
	return exploration, replay
}

// RollStrategy picks exploration vs replay weighted by the inputs. A nil rand
// uses the global math/rand/v2 source. Zero or negative total weights default
// to exploration as a safe fallback — never produce a panic on misconfig.
func RollStrategy(explorationWeight, replayWeight float64, r *rand.Rand) AutodreamStrategy {
	total := explorationWeight + replayWeight
	if total <= 0 {
		return StrategyExploration
	}
	var roll float64
	if r != nil {
		roll = r.Float64()
	} else {
		roll = rand.Float64()
	}
	if roll < explorationWeight/total {
		return StrategyExploration
	}
	return StrategyReplay
}

// ResolveStrategy rolls the strategy and applies the conditional fallback:
// when replay is rolled but no recent material exists for pairing, fall
// back to exploration. The decision struct exposes both the rolled value
// and whether fallback fired so callers can log the actual cause.
func ResolveStrategy(cfg Config, pressure BufferPressure, hasReplayPair bool, r *rand.Rand) StrategyDecision {
	exp, rep := ComputeAdaptiveWeights(cfg, pressure)
	rolled := RollStrategy(exp, rep, r)

	decision := StrategyDecision{
		Selected:          rolled,
		Rolled:            rolled,
		FellBack:          false,
		ExplorationWeight: exp,
		ReplayWeight:      rep,
	}

	if rolled == StrategyReplay && !hasReplayPair {
		decision.Selected = StrategyExploration
		decision.FellBack = true
	}
	return decision
}
