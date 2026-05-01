package main

import (
	"math"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// seededRand returns a deterministic *rand.Rand built from a fixed PCG seed.
// Use a different seed per test that needs distinct rolls; reuse the same
// seed when verifying reproducibility.
func seededRand(s1, s2 uint64) *rand.Rand {
	return rand.New(rand.NewPCG(s1, s2))
}

func TestComputeBufferPressure_NoBufferDir(t *testing.T) {
	vault := t.TempDir()
	p, err := ComputeBufferPressure(vault, 20)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if p.NonDaydreamCount != 0 || p.FillRatio != 0 {
		t.Errorf("got %+v, want zero", p)
	}
	if p.Threshold != 20 {
		t.Errorf("Threshold = %d, want 20", p.Threshold)
	}
}

func TestComputeBufferPressure_CountsExcludingDaydream(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Buffer", "Daydream"), 0o755); err != nil {
		t.Fatal(err)
	}

	// 3 top-level entries, 5 daydream entries
	for i := 0; i < 3; i++ {
		path := filepath.Join(vault, "Buffer", "entry"+string(rune('a'+i))+".md")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 5; i++ {
		path := filepath.Join(vault, "Buffer", "Daydream", "dd"+string(rune('a'+i))+".md")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	p, err := ComputeBufferPressure(vault, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p.NonDaydreamCount != 3 {
		t.Errorf("Count = %d, want 3 (Daydream/ should be excluded)", p.NonDaydreamCount)
	}
	if p.FillRatio != 0.3 {
		t.Errorf("FillRatio = %v, want 0.3", p.FillRatio)
	}
}

func TestComputeBufferPressure_FillRatioCappedAtOne(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Buffer"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 30 entries against threshold of 10 → would be 3.0 uncapped
	for i := 0; i < 30; i++ {
		path := filepath.Join(vault, "Buffer", "e"+string(rune('a'+i%26))+string(rune('0'+i/26))+".md")
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	p, err := ComputeBufferPressure(vault, 10)
	if err != nil {
		t.Fatal(err)
	}
	if p.FillRatio != 1.0 {
		t.Errorf("FillRatio = %v, want 1.0 (capped)", p.FillRatio)
	}
}

func TestComputeBufferPressure_ZeroThreshold(t *testing.T) {
	vault := t.TempDir()
	if err := os.MkdirAll(filepath.Join(vault, "Buffer"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(vault, "Buffer", "x.md"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := ComputeBufferPressure(vault, 0)
	if err != nil {
		t.Fatal(err)
	}
	if p.NonDaydreamCount != 1 {
		t.Errorf("Count = %d, want 1", p.NonDaydreamCount)
	}
	if p.FillRatio != 0 {
		t.Errorf("FillRatio = %v, want 0 (threshold=0 is treated as no pressure signal)", p.FillRatio)
	}
}

func TestComputeAdaptiveWeights_AdaptiveDisabledReturnsBase(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDaydreamStrategyAdaptive = false
	cfg.AutoDaydreamStrategyExplorationBase = 0.6
	cfg.AutoDaydreamStrategyReplayBase = 0.4

	pressure := BufferPressure{NonDaydreamCount: 100, Threshold: 10, FillRatio: 1.0}
	exp, rep := ComputeAdaptiveWeights(cfg, pressure)

	if exp != 0.6 || rep != 0.4 {
		t.Errorf("got (%v, %v), want (0.6, 0.4) — pressure should not shift weights when adaptive=false", exp, rep)
	}
}

func TestComputeAdaptiveWeights_AdaptiveEnabledStillReturnsBaseInV1(t *testing.T) {
	// Adaptive math is intentionally TODO until post-initial-testing data is
	// in. Document the v1 contract: enabling adaptive does not yet change
	// behavior. When the math lands, this test should be promoted to verify
	// the actual shift.
	t.Skip("adaptive math is TODO until post-initial-testing — see ComputeAdaptiveWeights")

	cfg := DefaultConfig()
	cfg.AutoDaydreamStrategyAdaptive = true
	cfg.AutoDaydreamStrategyExplorationBase = 0.5
	cfg.AutoDaydreamStrategyReplayBase = 0.5
	cfg.AutoDaydreamStrategyBufferPressureFactor = 1.5

	full := BufferPressure{FillRatio: 1.0}
	exp, rep := ComputeAdaptiveWeights(cfg, full)
	// Once implemented: replay should rise relative to exploration at full pressure.
	if rep <= exp {
		t.Errorf("at full pressure, replay should dominate, got exp=%v rep=%v", exp, rep)
	}
}

func TestRollStrategy_ZeroWeightsDefaultsToExploration(t *testing.T) {
	got := RollStrategy(0, 0, seededRand(1, 2))
	if got != StrategyExploration {
		t.Errorf("zero weights = %s, want exploration", got)
	}
	got = RollStrategy(-1, -1, seededRand(1, 2))
	if got != StrategyExploration {
		t.Errorf("negative weights = %s, want exploration", got)
	}
}

func TestRollStrategy_AlwaysExplorationWhenReplayWeightZero(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := RollStrategy(1.0, 0.0, seededRand(uint64(i), 99))
		if got != StrategyExploration {
			t.Fatalf("iteration %d: got %s, want exploration (replay weight is 0)", i, got)
		}
	}
}

func TestRollStrategy_AlwaysReplayWhenExplorationWeightZero(t *testing.T) {
	for i := 0; i < 100; i++ {
		got := RollStrategy(0.0, 1.0, seededRand(uint64(i), 99))
		if got != StrategyReplay {
			t.Fatalf("iteration %d: got %s, want replay (exploration weight is 0)", i, got)
		}
	}
}

func TestRollStrategy_FiftyFiftyDistribution(t *testing.T) {
	// Statistical sanity check — 50/50 weights should yield roughly equal
	// outcomes over a large sample. Tolerance is generous to avoid flakiness.
	const trials = 10000
	r := seededRand(42, 1729)
	expCount := 0
	for i := 0; i < trials; i++ {
		if RollStrategy(0.5, 0.5, r) == StrategyExploration {
			expCount++
		}
	}
	expected := trials / 2
	tolerance := trials / 20 // 5%
	if math.Abs(float64(expCount-expected)) > float64(tolerance) {
		t.Errorf("50/50 produced %d exploration in %d trials (expected ~%d ±%d)", expCount, trials, expected, tolerance)
	}
}

func TestRollStrategy_RespectsBiasedWeights(t *testing.T) {
	// 90/10 should produce roughly 90% exploration.
	const trials = 10000
	r := seededRand(7, 11)
	expCount := 0
	for i := 0; i < trials; i++ {
		if RollStrategy(0.9, 0.1, r) == StrategyExploration {
			expCount++
		}
	}
	if expCount < 8500 || expCount > 9500 {
		t.Errorf("90/10 produced %d exploration in %d trials (expected ~9000)", expCount, trials)
	}
}

func TestResolveStrategy_ExplorationRolledNoFallback(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDaydreamStrategyExplorationBase = 1.0 // force exploration
	cfg.AutoDaydreamStrategyReplayBase = 0.0
	d := ResolveStrategy(cfg, BufferPressure{}, false, seededRand(1, 2))
	if d.Selected != StrategyExploration {
		t.Errorf("Selected = %s, want exploration", d.Selected)
	}
	if d.FellBack {
		t.Error("FellBack should be false when exploration was rolled")
	}
	if d.Rolled != StrategyExploration {
		t.Errorf("Rolled = %s, want exploration", d.Rolled)
	}
}

func TestResolveStrategy_ReplayWithPairAvailable(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDaydreamStrategyExplorationBase = 0.0 // force replay
	cfg.AutoDaydreamStrategyReplayBase = 1.0
	d := ResolveStrategy(cfg, BufferPressure{}, true, seededRand(1, 2))
	if d.Selected != StrategyReplay {
		t.Errorf("Selected = %s, want replay", d.Selected)
	}
	if d.FellBack {
		t.Error("FellBack should be false when pair was available")
	}
}

func TestResolveStrategy_ReplayFallsBackWhenNoPair(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDaydreamStrategyExplorationBase = 0.0 // force replay
	cfg.AutoDaydreamStrategyReplayBase = 1.0
	d := ResolveStrategy(cfg, BufferPressure{}, false, seededRand(1, 2))
	if d.Selected != StrategyExploration {
		t.Errorf("Selected = %s, want exploration (fallback)", d.Selected)
	}
	if !d.FellBack {
		t.Error("FellBack should be true when replay was rolled but no pair available")
	}
	if d.Rolled != StrategyReplay {
		t.Errorf("Rolled = %s, want replay (the original roll, before fallback)", d.Rolled)
	}
}

func TestResolveStrategy_ExposesEffectiveWeights(t *testing.T) {
	cfg := DefaultConfig()
	cfg.AutoDaydreamStrategyExplorationBase = 0.7
	cfg.AutoDaydreamStrategyReplayBase = 0.3
	d := ResolveStrategy(cfg, BufferPressure{}, true, seededRand(1, 2))
	if d.ExplorationWeight != 0.7 || d.ReplayWeight != 0.3 {
		t.Errorf("got (%v, %v), want (0.7, 0.3)", d.ExplorationWeight, d.ReplayWeight)
	}
}
