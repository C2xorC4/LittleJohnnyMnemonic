package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// writeTestConfig builds a minimal vault layout (System/Config.md only) under a
// freshly-created tempdir and returns the vault root. The caller passes whatever
// markdown body it wants; LoadConfig only reads the YAML fenced blocks.
func writeTestConfig(t *testing.T, body string) string {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "System"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "System", "Config.md"), []byte(body), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return root
}

func TestLoadConfig_MissingFileReturnsDefaults(t *testing.T) {
	cfg := LoadConfig(t.TempDir()) // no System/Config.md
	def := DefaultConfig()
	if cfg.AutoDaydreamEnabled != def.AutoDaydreamEnabled {
		t.Errorf("AutoDaydreamEnabled = %v, want %v", cfg.AutoDaydreamEnabled, def.AutoDaydreamEnabled)
	}
	if cfg.AutoDaydreamIntervalMinMinutes != def.AutoDaydreamIntervalMinMinutes {
		t.Errorf("AutoDaydreamIntervalMinMinutes = %d, want %d", cfg.AutoDaydreamIntervalMinMinutes, def.AutoDaydreamIntervalMinMinutes)
	}
}

func TestLoadConfig_AutoDaydreamDefaultsApplied(t *testing.T) {
	// A Config.md with no auto_daydream_* keys should leave defaults intact.
	body := "```yaml\nretrieval_threshold: 0.4\n```"
	cfg := LoadConfig(writeTestConfig(t, body))

	def := DefaultConfig()
	if cfg.AutoDaydreamEnabled != def.AutoDaydreamEnabled {
		t.Errorf("default Enabled lost: got %v want %v", cfg.AutoDaydreamEnabled, def.AutoDaydreamEnabled)
	}
	if cfg.AutoDaydreamMaxPerDayActive != def.AutoDaydreamMaxPerDayActive {
		t.Errorf("default MaxPerDayActive lost: got %d want %d", cfg.AutoDaydreamMaxPerDayActive, def.AutoDaydreamMaxPerDayActive)
	}
	if !reflect.DeepEqual(cfg.AutoDaydreamActivitySources, def.AutoDaydreamActivitySources) {
		t.Errorf("default ActivitySources lost: got %v want %v", cfg.AutoDaydreamActivitySources, def.AutoDaydreamActivitySources)
	}
	if !reflect.DeepEqual(cfg.AutoDaydreamReplayStableCategories, def.AutoDaydreamReplayStableCategories) {
		t.Errorf("default ReplayStableCategories lost: got %v want %v", cfg.AutoDaydreamReplayStableCategories, def.AutoDaydreamReplayStableCategories)
	}
	if !reflect.DeepEqual(cfg.AutoDaydreamActiveSeedSources, def.AutoDaydreamActiveSeedSources) {
		t.Errorf("default ActiveSeedSources lost: got %v want %v", cfg.AutoDaydreamActiveSeedSources, def.AutoDaydreamActiveSeedSources)
	}
	// Sanity check that an unrelated scalar override still applies, so this test
	// is exercising the loader rather than a no-op path.
	if cfg.RetrievalThreshold != 0.4 {
		t.Errorf("RetrievalThreshold not overridden: got %v want 0.4", cfg.RetrievalThreshold)
	}
}

func TestLoadConfig_AutoDaydreamScalarOverrides(t *testing.T) {
	body := "```yaml\n" +
		"auto_daydream_enabled: true\n" +
		"auto_daydream_interval_min_minutes: 90\n" +
		"auto_daydream_interval_max_minutes: 240\n" +
		"auto_daydream_max_per_day_active: 24\n" +
		"auto_daydream_max_per_day_quiet: 9\n" +
		"auto_daydream_quiet_hours: \"23:00-06:00\"\n" +
		"auto_daydream_quiet_hours_timezone: utc\n" +
		"auto_daydream_active_skip_window_minutes: 30\n" +
		"auto_daydream_quiet_skip_window_minutes: 120\n" +
		"auto_daydream_strategy_exploration_base: 0.7\n" +
		"auto_daydream_strategy_replay_base: 0.3\n" +
		"auto_daydream_strategy_adaptive: true\n" +
		"auto_daydream_strategy_buffer_pressure_factor: 2.0\n" +
		"auto_daydream_replay_recent_source: recently_accessed_ltm\n" +
		"auto_daydream_replay_recent_max_age_days: 30\n" +
		"auto_daydream_replay_stable_filter: mature\n" +
		"auto_daydream_override_mode: replay-only\n" +
		"auto_daydream_surface_to_session: true\n" +
		"auto_daydream_surface_max_age_hours: 24\n" +
		"auto_daydream_surface_relevance_threshold: 0.6\n" +
		"auto_daydream_surface_max_per_prompt: 5\n" +
		"auto_daydream_log_rotation_threshold: 5000\n" +
		"```"
	cfg := LoadConfig(writeTestConfig(t, body))

	if !cfg.AutoDaydreamEnabled {
		t.Error("Enabled should be true")
	}
	if cfg.AutoDaydreamIntervalMinMinutes != 90 || cfg.AutoDaydreamIntervalMaxMinutes != 240 {
		t.Errorf("intervals = %d/%d", cfg.AutoDaydreamIntervalMinMinutes, cfg.AutoDaydreamIntervalMaxMinutes)
	}
	if cfg.AutoDaydreamMaxPerDayActive != 24 || cfg.AutoDaydreamMaxPerDayQuiet != 9 {
		t.Errorf("caps = %d/%d", cfg.AutoDaydreamMaxPerDayActive, cfg.AutoDaydreamMaxPerDayQuiet)
	}
	if cfg.AutoDaydreamQuietHours != "23:00-06:00" {
		t.Errorf("QuietHours = %q", cfg.AutoDaydreamQuietHours)
	}
	if cfg.AutoDaydreamQuietHoursTimezone != "utc" {
		t.Errorf("QuietHoursTimezone = %q", cfg.AutoDaydreamQuietHoursTimezone)
	}
	if cfg.AutoDaydreamActiveSkipWindowMinutes != 30 || cfg.AutoDaydreamQuietSkipWindowMinutes != 120 {
		t.Errorf("skip windows = %d/%d", cfg.AutoDaydreamActiveSkipWindowMinutes, cfg.AutoDaydreamQuietSkipWindowMinutes)
	}
	if cfg.AutoDaydreamStrategyExplorationBase != 0.7 || cfg.AutoDaydreamStrategyReplayBase != 0.3 {
		t.Errorf("strategy bases = %v/%v", cfg.AutoDaydreamStrategyExplorationBase, cfg.AutoDaydreamStrategyReplayBase)
	}
	if !cfg.AutoDaydreamStrategyAdaptive {
		t.Error("StrategyAdaptive should be true")
	}
	if cfg.AutoDaydreamStrategyBufferPressureFactor != 2.0 {
		t.Errorf("BufferPressureFactor = %v", cfg.AutoDaydreamStrategyBufferPressureFactor)
	}
	if cfg.AutoDaydreamReplayRecentSource != "recently_accessed_ltm" {
		t.Errorf("ReplayRecentSource = %q", cfg.AutoDaydreamReplayRecentSource)
	}
	if cfg.AutoDaydreamReplayRecentMaxAgeDays != 30 {
		t.Errorf("ReplayRecentMaxAgeDays = %d", cfg.AutoDaydreamReplayRecentMaxAgeDays)
	}
	if cfg.AutoDaydreamReplayStableFilter != "mature" {
		t.Errorf("ReplayStableFilter = %q", cfg.AutoDaydreamReplayStableFilter)
	}
	if cfg.AutoDaydreamOverrideMode != "replay-only" {
		t.Errorf("OverrideMode = %q", cfg.AutoDaydreamOverrideMode)
	}
	if !cfg.AutoDaydreamSurfaceToSession {
		t.Error("SurfaceToSession should be true")
	}
	if cfg.AutoDaydreamSurfaceMaxAgeHours != 24 || cfg.AutoDaydreamSurfaceMaxPerPrompt != 5 {
		t.Errorf("surface limits = %d/%d", cfg.AutoDaydreamSurfaceMaxAgeHours, cfg.AutoDaydreamSurfaceMaxPerPrompt)
	}
	if cfg.AutoDaydreamSurfaceRelevanceThreshold != 0.6 {
		t.Errorf("SurfaceRelevanceThreshold = %v", cfg.AutoDaydreamSurfaceRelevanceThreshold)
	}
	if cfg.AutoDaydreamLogRotationThreshold != 5000 {
		t.Errorf("LogRotationThreshold = %d", cfg.AutoDaydreamLogRotationThreshold)
	}
}

func TestLoadConfig_AutoDaydreamSeedSourceMaps(t *testing.T) {
	body := "```yaml\n" +
		"auto_daydream_active_seed_sources:\n" +
		"  buffer: 50\n" +
		"  knowledge: 30\n" +
		"  semantic: 20\n" +
		"auto_daydream_quiet_exploration_seed_sources:\n" +
		"  episodic: 100\n" +
		"```"
	cfg := LoadConfig(writeTestConfig(t, body))

	wantActive := map[string]float64{"buffer": 50, "knowledge": 30, "semantic": 20}
	if !reflect.DeepEqual(cfg.AutoDaydreamActiveSeedSources, wantActive) {
		t.Errorf("ActiveSeedSources = %v, want %v", cfg.AutoDaydreamActiveSeedSources, wantActive)
	}

	wantQuiet := map[string]float64{"episodic": 100}
	if !reflect.DeepEqual(cfg.AutoDaydreamQuietExplorationSeedSources, wantQuiet) {
		t.Errorf("QuietExplorationSeedSources = %v, want %v", cfg.AutoDaydreamQuietExplorationSeedSources, wantQuiet)
	}
}

func TestLoadConfig_AutoDaydreamCSVLists(t *testing.T) {
	body := "```yaml\n" +
		"auto_daydream_activity_sources: \"buffer\"\n" +
		"auto_daydream_replay_stable_categories: \"semantic, user, feedback, project\"\n" +
		"```"
	cfg := LoadConfig(writeTestConfig(t, body))

	if !reflect.DeepEqual(cfg.AutoDaydreamActivitySources, []string{"buffer"}) {
		t.Errorf("ActivitySources = %v, want [buffer]", cfg.AutoDaydreamActivitySources)
	}
	wantCats := []string{"semantic", "user", "feedback", "project"}
	if !reflect.DeepEqual(cfg.AutoDaydreamReplayStableCategories, wantCats) {
		t.Errorf("ReplayStableCategories = %v, want %v", cfg.AutoDaydreamReplayStableCategories, wantCats)
	}
}

func TestParseCSVList(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"", nil},
		{"\"\"", nil},
		{"buffer", []string{"buffer"}},
		{"buffer,heartbeat", []string{"buffer", "heartbeat"}},
		{" buffer , heartbeat ", []string{"buffer", "heartbeat"}},
		{"\"buffer,heartbeat\"", []string{"buffer", "heartbeat"}},
		{"a,,b", []string{"a", "b"}},
		{"a,b,", []string{"a", "b"}},
		{",a,b", []string{"a", "b"}},
	}
	for _, c := range cases {
		got := parseCSVList(c.in)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("parseCSVList(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLoadConfig_AutoDaydreamEmptyCSVKeepsDefault(t *testing.T) {
	// An empty CSV value should not clobber the default list — we expect the
	// loader to leave the default ["buffer", "heartbeat"] intact.
	body := "```yaml\nauto_daydream_activity_sources: \"\"\n```"
	cfg := LoadConfig(writeTestConfig(t, body))

	def := DefaultConfig()
	if !reflect.DeepEqual(cfg.AutoDaydreamActivitySources, def.AutoDaydreamActivitySources) {
		t.Errorf("ActivitySources clobbered by empty CSV: got %v, want %v", cfg.AutoDaydreamActivitySources, def.AutoDaydreamActivitySources)
	}
}
