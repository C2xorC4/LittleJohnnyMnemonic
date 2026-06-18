package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// LoadConfig builds a Config by starting from DefaultConfig() and applying
// any overrides found in System/Config.md. The Config.md file is the single
// source of override truth; values not present there fall back to defaults.
//
// Currently supports tunable map-of-floats sections (decay_rates,
// compression_thresholds, edge_weights, profile_decay_rates,
// user_facet_decay_rates). Scalar overrides and other section types are
// not yet wired up — extend parseConfigSections to handle them as needed.
//
// Failures (file missing, parse error) silently return DefaultConfig().
// The intent is graceful degradation: a malformed Config.md should never
// stop a consolidation run from completing on the baked-in defaults.
func LoadConfig(vaultRoot string) Config {
	cfg := DefaultConfig()

	configPath := filepath.Join(vaultRoot, "System", "Config.md")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return cfg
	}

	blocks := extractYAMLBlocks(string(data))
	if len(blocks) == 0 {
		return cfg
	}

	combined := strings.Join(blocks, "\n")

	overrides := parseConfigSections(combined)
	if vals, ok := overrides["decay_rates"]; ok && len(vals) > 0 {
		cfg.DecayRates = vals
	}
	if vals, ok := overrides["compression_thresholds"]; ok && len(vals) > 0 {
		cfg.CompressionThresholds = vals
	}
	if vals, ok := overrides["edge_weights"]; ok && len(vals) > 0 {
		cfg.EdgeWeights = vals
	}
	if vals, ok := overrides["user_facet_decay_rates"]; ok && len(vals) > 0 {
		cfg.UserFacetDecayRates = vals
	}
	if vals, ok := overrides["profile_decay_rates"]; ok && len(vals) > 0 {
		cfg.ProfileDecayRates = vals
	}
	if vals, ok := overrides["activation_floors"]; ok && len(vals) > 0 {
		cfg.ActivationFloors = vals
	}
	if vals, ok := overrides["auto_daydream_active_seed_sources"]; ok && len(vals) > 0 {
		cfg.AutoDaydreamActiveSeedSources = vals
	}
	if vals, ok := overrides["auto_daydream_quiet_exploration_seed_sources"]; ok && len(vals) > 0 {
		cfg.AutoDaydreamQuietExplorationSeedSources = vals
	}

	scalars := parseConfigScalars(combined)
	applyScalarOverrides(&cfg, scalars)

	return cfg
}

// parseConfigScalars walks a YAML body and collects every top-level
// `key: value` pair (where value is non-empty and the value is on the same
// line). Section headers (`key:` with no inline value) are skipped — those
// are picked up by parseConfigSections instead. Inline comments are stripped.
func parseConfigScalars(yaml string) map[string]string {
	result := make(map[string]string)
	lines := strings.Split(yaml, "\n")

	for _, raw := range lines {
		line := raw
		if hashIdx := strings.Index(line, "#"); hashIdx >= 0 {
			line = line[:hashIdx]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t") {
			continue
		}
		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		valStr := strings.TrimSpace(trimmed[idx+1:])
		if valStr == "" {
			continue // section header — handled elsewhere
		}
		result[strings.TrimSpace(trimmed[:idx])] = valStr
	}
	return result
}

// applyScalarOverrides applies any top-level scalar overrides parsed from
// Config.md onto a pre-populated Config. Unknown keys are silently ignored;
// type coercion failures keep the existing default. Add new fields here as
// they need to become tunable on the fly.
func applyScalarOverrides(cfg *Config, s map[string]string) {
	if v, ok := s["retrieval_threshold"]; ok {
		cfg.RetrievalThreshold = atofOrKeep(v, cfg.RetrievalThreshold)
	}
	if v, ok := s["max_memories_loaded"]; ok {
		cfg.MaxMemoriesLoaded = atoiOrKeep(v, cfg.MaxMemoriesLoaded)
	}

	if v, ok := s["buffer_threshold"]; ok {
		cfg.BufferThreshold = atoiOrKeep(v, cfg.BufferThreshold)
	}
	if v, ok := s["consolidation_depth"]; ok {
		cfg.ConsolidationDepth = stripQuotes(v)
	}
	if v, ok := s["max_holds"]; ok {
		cfg.MaxHolds = atoiOrKeep(v, cfg.MaxHolds)
	}
	if v, ok := s["auto_consolidation_enabled"]; ok {
		cfg.AutoConsolidationEnabled = atobOrKeep(v, cfg.AutoConsolidationEnabled)
	}
	if v, ok := s["auto_consolidation_cooldown_minutes"]; ok {
		cfg.AutoConsolidationCooldownMinutes = atoiOrKeep(v, cfg.AutoConsolidationCooldownMinutes)
	}

	if v, ok := s["context_penalty_partial"]; ok {
		cfg.ContextPenaltyPartial = atofOrKeep(v, cfg.ContextPenaltyPartial)
	}
	if v, ok := s["context_penalty_orphan"]; ok {
		cfg.ContextPenaltyOrphan = atofOrKeep(v, cfg.ContextPenaltyOrphan)
	}
	if v, ok := s["discard_ambiguous_orphans"]; ok {
		cfg.DiscardAmbiguousOrphans = atobOrKeep(v, cfg.DiscardAmbiguousOrphans)
	}

	if v, ok := s["confidence_reinforce"]; ok {
		cfg.ConfidenceReinforce = atofOrKeep(v, cfg.ConfidenceReinforce)
	}
	if v, ok := s["confidence_contradict"]; ok {
		cfg.ConfidenceContradict = atofOrKeep(v, cfg.ConfidenceContradict)
	}
	if v, ok := s["confidence_stale_factor"]; ok {
		cfg.ConfidenceStaleFactor = atofOrKeep(v, cfg.ConfidenceStaleFactor)
	}
	if v, ok := s["stale_threshold_days"]; ok {
		cfg.StaleThresholdDays = atoiOrKeep(v, cfg.StaleThresholdDays)
	}

	if v, ok := s["override_confidence_floor"]; ok {
		cfg.OverrideConfidenceFloor = atofOrKeep(v, cfg.OverrideConfidenceFloor)
	}
	if v, ok := s["override_immune_to_archival"]; ok {
		cfg.OverrideImmuneToArchival = atobOrKeep(v, cfg.OverrideImmuneToArchival)
	}

	if v, ok := s["surprise_bonus_weight"]; ok {
		cfg.SurpriseBonusWeight = atofOrKeep(v, cfg.SurpriseBonusWeight)
	}

	if v, ok := s["relevance_weight"]; ok {
		cfg.RelevanceWeight = atofOrKeep(v, cfg.RelevanceWeight)
	}
	if v, ok := s["max_activation"]; ok {
		cfg.MaxActivation = atofOrKeep(v, cfg.MaxActivation)
	}
	if v, ok := s["citation_gated_activation"]; ok {
		cfg.CitationGatedActivation = atobOrKeep(v, cfg.CitationGatedActivation)
	}

	if v, ok := s["archive_instead_of_delete"]; ok {
		cfg.ArchiveInsteadOfDelete = atobOrKeep(v, cfg.ArchiveInsteadOfDelete)
	}

	if v, ok := s["references_dir"]; ok {
		cfg.ReferencesDir = stripQuotes(v)
	}

	if v, ok := s["rate_separation_enabled"]; ok {
		cfg.RateSeparationEnabled = atobOrKeep(v, cfg.RateSeparationEnabled)
	}
	if v, ok := s["rate_separation_mature_threshold"]; ok {
		cfg.RateSeparationMatureThreshold = atoiOrKeep(v, cfg.RateSeparationMatureThreshold)
	}
	if v, ok := s["rate_separation_crystallized_threshold"]; ok {
		cfg.RateSeparationCrystallizedThreshold = atoiOrKeep(v, cfg.RateSeparationCrystallizedThreshold)
	}
	if v, ok := s["rate_separation_min_sessions"]; ok {
		cfg.RateSeparationMinSessions = atoiOrKeep(v, cfg.RateSeparationMinSessions)
	}

	if v, ok := s["daydream_judge_enabled"]; ok {
		cfg.DaydreamJudgeEnabled = atobOrKeep(v, cfg.DaydreamJudgeEnabled)
	}
	if v, ok := s["daydream_judge_threshold"]; ok {
		cfg.DaydreamJudgeThreshold = atofOrKeep(v, cfg.DaydreamJudgeThreshold)
	}
	if v, ok := s["daydream_judge_candidates"]; ok {
		cfg.DaydreamJudgeCandidates = atoiOrKeep(v, cfg.DaydreamJudgeCandidates)
	}
	if v, ok := s["daydream_redundancy_fallback_dampening"]; ok {
		cfg.DaydreamRedundancyFallbackDampening = atofOrKeep(v, cfg.DaydreamRedundancyFallbackDampening)
	}

	if v, ok := s["judge_cli_fallback_enabled"]; ok {
		cfg.JudgeCLIFallbackEnabled = atobOrKeep(v, cfg.JudgeCLIFallbackEnabled)
	}
	if v, ok := s["judge_cli_max_concurrent"]; ok {
		cfg.JudgeCLIMaxConcurrent = atoiOrKeep(v, cfg.JudgeCLIMaxConcurrent)
	}

	if v, ok := s["auto_daydream_enabled"]; ok {
		cfg.AutoDaydreamEnabled = atobOrKeep(v, cfg.AutoDaydreamEnabled)
	}
	if v, ok := s["auto_daydream_interval_min_minutes"]; ok {
		cfg.AutoDaydreamIntervalMinMinutes = atoiOrKeep(v, cfg.AutoDaydreamIntervalMinMinutes)
	}
	if v, ok := s["auto_daydream_interval_max_minutes"]; ok {
		cfg.AutoDaydreamIntervalMaxMinutes = atoiOrKeep(v, cfg.AutoDaydreamIntervalMaxMinutes)
	}
	if v, ok := s["auto_daydream_max_per_day_active"]; ok {
		cfg.AutoDaydreamMaxPerDayActive = atoiOrKeep(v, cfg.AutoDaydreamMaxPerDayActive)
	}
	if v, ok := s["auto_daydream_max_per_day_quiet"]; ok {
		cfg.AutoDaydreamMaxPerDayQuiet = atoiOrKeep(v, cfg.AutoDaydreamMaxPerDayQuiet)
	}
	if v, ok := s["auto_daydream_quiet_hours"]; ok {
		cfg.AutoDaydreamQuietHours = stripQuotes(v)
	}
	if v, ok := s["auto_daydream_quiet_hours_timezone"]; ok {
		cfg.AutoDaydreamQuietHoursTimezone = stripQuotes(v)
	}
	if v, ok := s["auto_daydream_active_skip_window_minutes"]; ok {
		cfg.AutoDaydreamActiveSkipWindowMinutes = atoiOrKeep(v, cfg.AutoDaydreamActiveSkipWindowMinutes)
	}
	if v, ok := s["auto_daydream_quiet_skip_window_minutes"]; ok {
		cfg.AutoDaydreamQuietSkipWindowMinutes = atoiOrKeep(v, cfg.AutoDaydreamQuietSkipWindowMinutes)
	}
	if v, ok := s["auto_daydream_activity_sources"]; ok {
		if list := parseCSVList(v); len(list) > 0 {
			cfg.AutoDaydreamActivitySources = list
		}
	}
	if v, ok := s["auto_daydream_strategy_exploration_base"]; ok {
		cfg.AutoDaydreamStrategyExplorationBase = atofOrKeep(v, cfg.AutoDaydreamStrategyExplorationBase)
	}
	if v, ok := s["auto_daydream_strategy_replay_base"]; ok {
		cfg.AutoDaydreamStrategyReplayBase = atofOrKeep(v, cfg.AutoDaydreamStrategyReplayBase)
	}
	if v, ok := s["auto_daydream_strategy_adaptive"]; ok {
		cfg.AutoDaydreamStrategyAdaptive = atobOrKeep(v, cfg.AutoDaydreamStrategyAdaptive)
	}
	if v, ok := s["auto_daydream_strategy_buffer_pressure_factor"]; ok {
		cfg.AutoDaydreamStrategyBufferPressureFactor = atofOrKeep(v, cfg.AutoDaydreamStrategyBufferPressureFactor)
	}
	if v, ok := s["auto_daydream_replay_recent_source"]; ok {
		cfg.AutoDaydreamReplayRecentSource = stripQuotes(v)
	}
	if v, ok := s["auto_daydream_replay_recent_max_age_days"]; ok {
		cfg.AutoDaydreamReplayRecentMaxAgeDays = atoiOrKeep(v, cfg.AutoDaydreamReplayRecentMaxAgeDays)
	}
	if v, ok := s["auto_daydream_replay_stable_filter"]; ok {
		cfg.AutoDaydreamReplayStableFilter = stripQuotes(v)
	}
	if v, ok := s["auto_daydream_replay_stable_categories"]; ok {
		if list := parseCSVList(v); len(list) > 0 {
			cfg.AutoDaydreamReplayStableCategories = list
		}
	}
	if v, ok := s["auto_daydream_override_mode"]; ok {
		cfg.AutoDaydreamOverrideMode = stripQuotes(v)
	}
	if v, ok := s["auto_daydream_surface_to_session"]; ok {
		cfg.AutoDaydreamSurfaceToSession = atobOrKeep(v, cfg.AutoDaydreamSurfaceToSession)
	}
	if v, ok := s["auto_daydream_surface_max_age_hours"]; ok {
		cfg.AutoDaydreamSurfaceMaxAgeHours = atoiOrKeep(v, cfg.AutoDaydreamSurfaceMaxAgeHours)
	}
	if v, ok := s["auto_daydream_surface_relevance_threshold"]; ok {
		cfg.AutoDaydreamSurfaceRelevanceThreshold = atofOrKeep(v, cfg.AutoDaydreamSurfaceRelevanceThreshold)
	}
	if v, ok := s["auto_daydream_surface_max_per_prompt"]; ok {
		cfg.AutoDaydreamSurfaceMaxPerPrompt = atoiOrKeep(v, cfg.AutoDaydreamSurfaceMaxPerPrompt)
	}
	if v, ok := s["auto_daydream_log_rotation_threshold"]; ok {
		cfg.AutoDaydreamLogRotationThreshold = atoiOrKeep(v, cfg.AutoDaydreamLogRotationThreshold)
	}
	if v, ok := s["auto_daydream_value_judge_enabled"]; ok {
		cfg.AutoDaydreamValueJudgeEnabled = atobOrKeep(v, cfg.AutoDaydreamValueJudgeEnabled)
	}
	if v, ok := s["daydream_scheduler_host"]; ok {
		cfg.DaydreamSchedulerHost = stripQuotes(v)
	}
	if v, ok := s["daydream_scheduler_missing_invoker"]; ok {
		cfg.DaydreamSchedulerMissingInvoker = stripQuotes(v)
	}
	if v, ok := s["daydream_volley_policy"]; ok {
		cfg.DaydreamVolleyPolicy = stripQuotes(v)
	}
	if v, ok := s["daydream_volley_commitment_ttl_minutes"]; ok {
		cfg.DaydreamVolleyCommitmentTTLMinutes = atoiOrKeep(v, cfg.DaydreamVolleyCommitmentTTLMinutes)
	}

	if v, ok := s["spreading_activation_factor"]; ok {
		cfg.SpreadingActivationFactor = atofOrKeep(v, cfg.SpreadingActivationFactor)
	}
	if v, ok := s["max_activation_hops"]; ok {
		cfg.MaxActivationHops = atoiOrKeep(v, cfg.MaxActivationHops)
	}
	if v, ok := s["fan_discount_formula"]; ok {
		cfg.FanDiscountFormula = stripQuotes(v)
	}

	if v, ok := s["adaptive_edge_weighting_enabled"]; ok {
		cfg.AdaptiveEdgeWeightingEnabled = atobOrKeep(v, cfg.AdaptiveEdgeWeightingEnabled)
	}
	if v, ok := s["adaptive_edge_scope"]; ok {
		if list := parseCSVList(v); len(list) > 0 {
			cfg.AdaptiveEdgeScope = list
		}
	}
	if v, ok := s["adaptive_edge_alpha"]; ok {
		cfg.AdaptiveEdgeAlpha = atofOrKeep(v, cfg.AdaptiveEdgeAlpha)
	}
	if v, ok := s["adaptive_edge_cap"]; ok {
		cfg.AdaptiveEdgeCap = atofOrKeep(v, cfg.AdaptiveEdgeCap)
	}
	if v, ok := s["adaptive_edge_decay_lambda"]; ok {
		cfg.AdaptiveEdgeDecayLambda = atofOrKeep(v, cfg.AdaptiveEdgeDecayLambda)
	}

	if v, ok := s["retrieval_session_log_enabled"]; ok {
		cfg.RetrievalSessionLogEnabled = atobOrKeep(v, cfg.RetrievalSessionLogEnabled)
	}
	if v, ok := s["retrieval_session_log_retention_days"]; ok {
		cfg.RetrievalSessionLogRetentionDays = atoiOrKeep(v, cfg.RetrievalSessionLogRetentionDays)
	}

	if v, ok := s["profile_creation_threshold"]; ok {
		cfg.ProfileCreationThreshold = atoiOrKeep(v, cfg.ProfileCreationThreshold)
	}
	if v, ok := s["profile_confidence_floor"]; ok {
		cfg.ProfileConfidenceFloor = atofOrKeep(v, cfg.ProfileConfidenceFloor)
	}
	if v, ok := s["profile_revision_threshold"]; ok {
		cfg.ProfileRevisionThreshold = atoiOrKeep(v, cfg.ProfileRevisionThreshold)
	}
	if v, ok := s["profile_immune_to_archival"]; ok {
		cfg.ProfileImmuneToArchival = atobOrKeep(v, cfg.ProfileImmuneToArchival)
	}

	if v, ok := s["recall_tracking_enabled"]; ok {
		cfg.RecallTrackingEnabled = atobOrKeep(v, cfg.RecallTrackingEnabled)
	}
	if v, ok := s["recall_tracking_verbosity"]; ok {
		cfg.RecallTrackingVerbosity = stripQuotes(v)
	}
	if v, ok := s["recall_tracking_log_path"]; ok {
		cfg.RecallTrackingLogPath = stripQuotes(v)
	}
	if v, ok := s["recall_log_retention_days"]; ok {
		cfg.RecallLogRetentionDays = atoiOrKeep(v, cfg.RecallLogRetentionDays)
	}

	if v, ok := s["memory_usage_tracking_enabled"]; ok {
		cfg.MemoryUsageTrackingEnabled = atobOrKeep(v, cfg.MemoryUsageTrackingEnabled)
	}
	if v, ok := s["memory_usage_tracking_verbosity"]; ok {
		cfg.MemoryUsageTrackingVerbosity = stripQuotes(v)
	}
	if v, ok := s["memory_usage_tracking_log_path"]; ok {
		cfg.MemoryUsageTrackingLogPath = stripQuotes(v)
	}

	if v, ok := s["dashboard_auto_refresh_enabled"]; ok {
		cfg.DashboardAutoRefreshEnabled = atobOrKeep(v, cfg.DashboardAutoRefreshEnabled)
	}
	if v, ok := s["dashboard_refresh_cooldown_minutes"]; ok {
		cfg.DashboardRefreshCooldownMinutes = atoiOrKeep(v, cfg.DashboardRefreshCooldownMinutes)
	}

	if v, ok := s["backup_enabled"]; ok {
		cfg.BackupEnabled = atobOrKeep(v, cfg.BackupEnabled)
	}
	if v, ok := s["backup_age_recipient"]; ok {
		cfg.BackupAgeRecipient = stripQuotes(v)
	}
	if v, ok := s["backup_age_identity_path"]; ok {
		cfg.BackupAgeIdentityPath = stripQuotes(v)
	}
	if v, ok := s["backup_local_target_dir"]; ok {
		cfg.BackupLocalTargetDir = stripQuotes(v)
	}
	if v, ok := s["backup_remote_url"]; ok {
		cfg.BackupRemoteURL = stripQuotes(v)
	}
	if v, ok := s["backup_remote_clone_path"]; ok {
		cfg.BackupRemoteClonePath = stripQuotes(v)
	}
	if v, ok := s["backup_push_on_backup"]; ok {
		cfg.BackupPushOnBackup = atobOrKeep(v, cfg.BackupPushOnBackup)
	}
	if v, ok := s["backup_retention_keep_last"]; ok {
		cfg.BackupRetentionKeepLast = atoiOrKeep(v, cfg.BackupRetentionKeepLast)
	}
	if v, ok := s["backup_cooldown_minutes"]; ok {
		cfg.BackupCooldownMinutes = atoiOrKeep(v, cfg.BackupCooldownMinutes)
	}
}

func atofOrKeep(s string, fallback float64) float64 {
	if f, err := strconv.ParseFloat(strings.TrimSpace(s), 64); err == nil {
		return f
	}
	return fallback
}

func atoiOrKeep(s string, fallback int) int {
	if i, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
		return i
	}
	return fallback
}

func atobOrKeep(s string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "yes", "1":
		return true
	case "false", "no", "0":
		return false
	}
	return fallback
}

func stripQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && (s[0] == '"' || s[0] == '\'') && s[len(s)-1] == s[0] {
		return s[1 : len(s)-1]
	}
	return s
}

// parseCSVList accepts a (possibly quoted) comma-separated string and returns
// the list of trimmed, non-empty entries. Used for config keys that hold
// short ordered lists (activity sources, replay stable categories).
func parseCSVList(s string) []string {
	s = stripQuotes(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

var yamlBlockRe = regexp.MustCompile("(?s)```yaml\\s*\\n(.*?)\\n```")

// extractYAMLBlocks pulls the contents of every ```yaml fenced block out of
// a markdown document and returns them in document order.
func extractYAMLBlocks(content string) []string {
	matches := yamlBlockRe.FindAllStringSubmatch(content, -1)
	out := make([]string, 0, len(matches))
	for _, m := range matches {
		out = append(out, m[1])
	}
	return out
}

// parseConfigSections walks a YAML body and extracts every top-level key
// whose value is a nested map of float-parseable values. Returns
// section_name → (subkey → float).
//
// Intentionally simple. Recognizes:
//
//	section_name:
//	  subkey: 0.42
//	  another: 17
//
// Inline comments (`# ...`) are stripped from values. Non-numeric values
// are skipped. Nesting deeper than two levels is ignored. Anything that
// doesn't look like a section header (top-level key with no inline value)
// is skipped silently — this lets the parser coexist with scalar overrides
// without choking on them.
func parseConfigSections(yaml string) map[string]map[string]float64 {
	result := make(map[string]map[string]float64)
	lines := strings.Split(yaml, "\n")

	var currentSection string

	for _, raw := range lines {
		// Strip trailing comment but preserve indentation.
		line := raw
		if hashIdx := strings.Index(line, "#"); hashIdx >= 0 {
			// Don't strip if # appears inside a quoted string. The Config.md
			// values are all numeric or unquoted scalars, so this is safe.
			line = line[:hashIdx]
		}

		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}

		indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")

		if !indented {
			// Top-level line. If it ends with a bare colon, it opens a section.
			// Anything else (scalar override) is ignored by this parser.
			if strings.HasSuffix(trimmed, ":") {
				currentSection = strings.TrimSuffix(trimmed, ":")
				if _, ok := result[currentSection]; !ok {
					result[currentSection] = make(map[string]float64)
				}
			} else {
				currentSection = ""
			}
			continue
		}

		if currentSection == "" {
			continue
		}

		// Indented line under a section: expect "key: value".
		idx := strings.Index(trimmed, ":")
		if idx <= 0 {
			continue
		}
		key := strings.TrimSpace(trimmed[:idx])
		valStr := strings.TrimSpace(trimmed[idx+1:])
		if valStr == "" {
			// Nested section header — not supported; skip.
			continue
		}
		f, err := strconv.ParseFloat(valStr, 64)
		if err != nil {
			continue
		}
		result[currentSection][key] = f
	}

	// Drop empty sections so callers can use len() > 0 as a "has overrides" test.
	for k, v := range result {
		if len(v) == 0 {
			delete(result, k)
		}
	}
	return result
}
