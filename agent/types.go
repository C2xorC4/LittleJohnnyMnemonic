package main

import "time"

// MemoryType classifies a memory entry.
type MemoryType string

const (
	TypeBuffer    MemoryType = "buffer"
	TypeUser      MemoryType = "user"
	TypeFeedback  MemoryType = "feedback"
	TypeProject   MemoryType = "project"
	TypeReference MemoryType = "reference"
	TypeSemantic  MemoryType = "semantic"
	TypeEpisodic  MemoryType = "episodic"
	TypeKnowledge MemoryType = "knowledge"
)

// ContextIntegrity tracks whether the original conversation context is available.
type ContextIntegrity string

const (
	ContextFull    ContextIntegrity = "full"
	ContextPartial ContextIntegrity = "partial"
	ContextOrphan  ContextIntegrity = "orphan"
)

// Link represents a typed associative connection between memories.
//
// Weight is an optional per-link override for the relationship-type weight
// looked up from Config.EdgeWeights. When nil, retrieval uses the type
// default (e.g., "related-to" → 0.5). When non-nil, the value overrides
// the default for spreading-activation purposes. See System/AssociativeMap.md
// for the adaptive-weighting design and the averages-collapse-context
// tradeoff. The adaptive-weighting pilot (Config.AdaptiveEdgeWeighting*)
// further modulates effective weight via citation-driven usage counters,
// but that path is opt-in and out-of-pilot scope for authored edges.
type Link struct {
	Target       string   `yaml:"target"`
	Relationship string   `yaml:"relationship"`
	Weight       *float64 `yaml:"weight,omitempty"`
}

// BufferEntry represents a short-term memory buffer item.
type BufferEntry struct {
	// Frontmatter
	Type             MemoryType       `yaml:"type"`
	Timestamp        time.Time        `yaml:"timestamp"`
	Source           string           `yaml:"source"`
	Surprise         float64          `yaml:"surprise"`
	ContextIntegrity ContextIntegrity `yaml:"context_integrity"`
	Tags             []string         `yaml:"tags"`
	Related          []string         `yaml:"related"`
	Pinned           bool             `yaml:"pinned,omitempty"`
	HoldCount        int              `yaml:"hold_count,omitempty"`
	HeldForCrossSession bool          `yaml:"held_for_cross_session,omitempty"` // rate-separation gate

	// Daydream-routing fields. Populated by autodream agent breadcrumbs.
	// daydream_kind values: exploration | replay-refine | replay-contradict
	// (replay-reinforce and unrelated never produce a buffer entry).
	// priority values: high | critical (omitted = normal).
	DaydreamKind        string   `yaml:"daydream_kind,omitempty"`
	DaydreamMode        string   `yaml:"daydream_mode,omitempty"`
	Priority            string   `yaml:"priority,omitempty"`
	Relationship        string   `yaml:"relationship,omitempty"`         // replay-only: reinforce|refine|contradict|unrelated
	SurfacedInSessions  []string `yaml:"surfaced_in_sessions,omitempty"` // session IDs where this entry was surfaced — within-session dedup

	// Derived
	Body     string `yaml:"-"`
	FilePath string `yaml:"-"`
	FileName string `yaml:"-"`
}

// MemoryEntry represents a long-term memory item.
type MemoryEntry struct {
	// Frontmatter
	Type                MemoryType `yaml:"type"`
	Title               string     `yaml:"title"`
	Created             time.Time  `yaml:"created"`
	LastAccessed        time.Time  `yaml:"last_accessed"`
	AccessCount         int        `yaml:"access_count"`
	DecayRate           float64    `yaml:"decay_rate"`
	Confidence          float64    `yaml:"confidence"`
	SurpriseAtEncoding  float64    `yaml:"surprise_at_encoding"`
	ConsolidationSource []string   `yaml:"consolidation_source,omitempty"`
	Tags                []string   `yaml:"tags"`
	Links               []Link     `yaml:"links,omitempty"`

	// Training override fields
	TrainingOverride bool     `yaml:"training_override,omitempty"`
	OverrideContext  string   `yaml:"override_context,omitempty"`
	SourceAuthority  string   `yaml:"source_authority,omitempty"`
	ValidatedVia     []string `yaml:"validated_via,omitempty"`

	// Progressive compression
	Fidelity       string `yaml:"fidelity,omitempty"`        // full | detailed | summary | gist
	TargetFidelity string `yaml:"target_fidelity,omitempty"` // set by decay pass when compression is pending
	ArchiveRef     string `yaml:"archive_ref,omitempty"`     // relative path to Archive/<type>/<file> (set at gist boundary)
	Importance     string `yaml:"importance,omitempty"`      // critical | significant | moderate | minor

	// User modeling fields
	Facet                string   `yaml:"facet,omitempty"`
	ObservationCount     int      `yaml:"observation_count,omitempty"`
	Profile              bool     `yaml:"profile,omitempty"`               // true = synthesized profile trait (very sticky)
	Evidence             []string `yaml:"evidence,omitempty"`              // buffer entries that built this profile trait
	ContributingSessions []string `yaml:"contributing_sessions,omitempty"` // consolidation session IDs that have updated this entry

	// Knowledge base fields (persistent reference material)
	SourceDocument string `yaml:"source_document,omitempty"` // e.g., "Windows Internals 7th Ed, Ch. 3"
	SourceVersion  string `yaml:"source_version,omitempty"`  // e.g., "Windows 11 24H2", "NTDLL 10.0.26100"
	Domain         string `yaml:"domain,omitempty"`           // e.g., "windows-internals", "offensive-security"
	Verified       bool   `yaml:"verified,omitempty"`         // cross-referenced against live binary/source

	// Archive fields
	Archived      *time.Time `yaml:"archived,omitempty"`
	ArchiveReason string     `yaml:"archive_reason,omitempty"`
	FinalScore    float64    `yaml:"final_score,omitempty"`
	SupersededBy  string     `yaml:"superseded_by,omitempty"`

	// Derived
	Body     string  `yaml:"-"`
	FilePath string  `yaml:"-"`
	FileName string  `yaml:"-"`
	Score    float64 `yaml:"-"` // computed at retrieval time
}

// Config holds all tunable parameters, parsed from System/Config.md.
type Config struct {
	// Retrieval
	RetrievalThreshold float64 `yaml:"retrieval_threshold"`
	MaxMemoriesLoaded  int     `yaml:"max_memories_loaded"`

	// Consolidation
	BufferThreshold                  int    `yaml:"buffer_threshold"`
	ConsolidationDepth               string `yaml:"consolidation_depth"`
	MaxHolds                         int    `yaml:"max_holds"`
	AutoConsolidationEnabled         bool   `yaml:"auto_consolidation_enabled"`
	AutoConsolidationCooldownMinutes int    `yaml:"auto_consolidation_cooldown_minutes"`

	// Context integrity
	ContextPenaltyPartial    float64 `yaml:"context_penalty_partial"`
	ContextPenaltyOrphan     float64 `yaml:"context_penalty_orphan"`
	DiscardAmbiguousOrphans  bool    `yaml:"discard_ambiguous_orphans"`

	// Decay rates by type
	DecayRates map[string]float64 `yaml:"decay_rates"`

	// Activation floors by type — minimum activation value used in retrieval
	// scoring. Prevents time-based decay from making durable memory types
	// (user, feedback, semantic) unretrievable during topic-dormant periods.
	// Project and reference default to 0.0 (full decay is appropriate).
	ActivationFloors map[string]float64 `yaml:"activation_floors"`

	// Progressive compression thresholds (days since last_accessed before
	// fidelity transitions). Flat-key map with format
	// "<importance>_<from>_to_<to>" — e.g. "moderate_full_to_detailed".
	// Loader: ApplyConfigCompressionThresholds in decay_model.go.
	CompressionThresholds map[string]float64 `yaml:"compression_thresholds"`

	// Confidence
	ConfidenceReinforce   float64 `yaml:"confidence_reinforce"`
	ConfidenceContradict  float64 `yaml:"confidence_contradict"`
	ConfidenceStaleFactor float64 `yaml:"confidence_stale_factor"`
	StaleThresholdDays    int     `yaml:"stale_threshold_days"`

	// Training overrides
	OverrideConfidenceFloor  float64 `yaml:"override_confidence_floor"`
	OverrideImmuneToArchival bool    `yaml:"override_immune_to_archival"`

	// Surprise
	SurpriseBonusWeight float64 `yaml:"surprise_bonus_weight"`

	// Archival
	ArchiveInsteadOfDelete bool `yaml:"archive_instead_of_delete"`

	// Ingestion
	ReferencesDir string `yaml:"references_dir"` // directory containing PDFs for ingestion

	// Rate separation (CLS-inspired — protects stable semantics from same-session burst updates)
	RateSeparationEnabled              bool `yaml:"rate_separation_enabled"`
	RateSeparationMatureThreshold      int  `yaml:"rate_separation_mature_threshold"`
	RateSeparationCrystallizedThreshold int `yaml:"rate_separation_crystallized_threshold"`
	RateSeparationMinSessions          int  `yaml:"rate_separation_min_sessions"`

	// Daydream redundancy judge — content-based redundancy assessment for daydream entries.
	// Scales past the point where tag overlap is dominated by taxonomy coincidence.
	DaydreamJudgeEnabled               bool    `yaml:"daydream_judge_enabled"`
	DaydreamJudgeThreshold             float64 `yaml:"daydream_judge_threshold"`              // min redundancy to trigger judge
	DaydreamJudgeCandidates            int     `yaml:"daydream_judge_candidates"`             // top N related memories to include as context
	DaydreamRedundancyFallbackDampening float64 `yaml:"daydream_redundancy_fallback_dampening"` // multiplier when API unavailable

	// Judge transport guards — bound the resource cost of LLM judge calls
	// (consolidation redundancy + behavioral rule judging). When no
	// ANTHROPIC_API_KEY is set, judge calls fall back to shelling out to
	// `claude -p`, which cold-boots a full ~320MB CLI per call. On a key-less
	// host with frequent hooks + scheduled autodream, those spawns swarm and
	// exhaust memory. JudgeCLIFallbackEnabled=false is the kill switch: judges
	// degrade to heuristics instead of spawning CLIs. JudgeCLIMaxConcurrent
	// host-wide-caps simultaneous `claude -p` judge processes when the
	// fallback IS enabled (over-cap calls degrade to heuristics, never block).
	JudgeCLIFallbackEnabled bool `yaml:"judge_cli_fallback_enabled"`
	JudgeCLIMaxConcurrent   int  `yaml:"judge_cli_max_concurrent"`

	// Auto-daydream — autonomous background daydreams, jitter-scheduled, opt-in.
	// See Config.md "## Auto Daydream" for the full spec. Two modes (active/quiet)
	// with mode-aware seed weighting; quiet mode mixes exploration and CLS-style
	// interleaved replay sub-strategies. Activity-based skip detection replaces
	// the lockfile approach (see auto_daydream_activity_sources).
	AutoDaydreamEnabled                      bool               `yaml:"auto_daydream_enabled"`
	AutoDaydreamIntervalMinMinutes           int                `yaml:"auto_daydream_interval_min_minutes"`
	AutoDaydreamIntervalMaxMinutes           int                `yaml:"auto_daydream_interval_max_minutes"`
	AutoDaydreamMaxPerDayActive              int                `yaml:"auto_daydream_max_per_day_active"`
	AutoDaydreamMaxPerDayQuiet               int                `yaml:"auto_daydream_max_per_day_quiet"`
	AutoDaydreamQuietHours                   string             `yaml:"auto_daydream_quiet_hours"`
	AutoDaydreamQuietHoursTimezone           string             `yaml:"auto_daydream_quiet_hours_timezone"`
	AutoDaydreamActiveSkipWindowMinutes      int                `yaml:"auto_daydream_active_skip_window_minutes"`
	AutoDaydreamQuietSkipWindowMinutes       int                `yaml:"auto_daydream_quiet_skip_window_minutes"`
	AutoDaydreamActivitySources              []string           `yaml:"auto_daydream_activity_sources"`
	AutoDaydreamActiveSeedSources            map[string]float64 `yaml:"auto_daydream_active_seed_sources"`
	AutoDaydreamQuietExplorationSeedSources  map[string]float64 `yaml:"auto_daydream_quiet_exploration_seed_sources"`
	AutoDaydreamStrategyExplorationBase      float64            `yaml:"auto_daydream_strategy_exploration_base"`
	AutoDaydreamStrategyReplayBase           float64            `yaml:"auto_daydream_strategy_replay_base"`
	AutoDaydreamStrategyAdaptive             bool               `yaml:"auto_daydream_strategy_adaptive"`
	AutoDaydreamStrategyBufferPressureFactor float64            `yaml:"auto_daydream_strategy_buffer_pressure_factor"`
	AutoDaydreamReplayRecentSource           string             `yaml:"auto_daydream_replay_recent_source"`
	AutoDaydreamReplayRecentMaxAgeDays       int                `yaml:"auto_daydream_replay_recent_max_age_days"`
	AutoDaydreamReplayStableFilter           string             `yaml:"auto_daydream_replay_stable_filter"`
	AutoDaydreamReplayStableCategories       []string           `yaml:"auto_daydream_replay_stable_categories"`
	AutoDaydreamOverrideMode                 string             `yaml:"auto_daydream_override_mode"`
	AutoDaydreamSurfaceToSession             bool               `yaml:"auto_daydream_surface_to_session"`
	AutoDaydreamSurfaceMaxAgeHours           int                `yaml:"auto_daydream_surface_max_age_hours"`
	AutoDaydreamSurfaceRelevanceThreshold    float64            `yaml:"auto_daydream_surface_relevance_threshold"`
	AutoDaydreamSurfaceMaxPerPrompt          int                `yaml:"auto_daydream_surface_max_per_prompt"`
	AutoDaydreamLogRotationThreshold         int                `yaml:"auto_daydream_log_rotation_threshold"`
	AutoDaydreamValueJudgeEnabled            bool               `yaml:"auto_daydream_value_judge_enabled"`

	// Daydream dispatch — host-aware volley vs scheduler coordination.
	DaydreamSchedulerHost              string `yaml:"daydream_scheduler_host"`                // preferred headless host when vault idle (default claude-code)
	DaydreamSchedulerMissingInvoker    string `yaml:"daydream_scheduler_missing_invoker"`     // skip (hard) when preferred host has no headless invoker
	DaydreamVolleyPolicy               string `yaml:"daydream_volley_policy"`                 // delegate_active | disabled
	DaydreamVolleyCommitmentTTLMinutes int    `yaml:"daydream_volley_commitment_ttl_minutes"` // scheduler defer while nudge pending

	// Associative retrieval
	SpreadingActivationFactor float64            `yaml:"spreading_activation_factor"`
	MaxActivationHops         int                `yaml:"max_activation_hops"`
	EdgeWeights               map[string]float64 `yaml:"edge_weights"`
	FanDiscountFormula        string             `yaml:"fan_discount_formula"` // "log" | "sqrt" | "linear" | "none"

	// Adaptive edge weighting (pilot) — usage-derived multiplier applied to
	// edges in `AdaptiveEdgeScope`. Off by default; even when enabled the
	// multiplier is 1.0 (no effect) until citation events accumulate usage.
	// See System/AssociativeMap.md for the design and the
	// averages-collapse-context tradeoff.
	AdaptiveEdgeWeightingEnabled bool     `yaml:"adaptive_edge_weighting_enabled"`
	AdaptiveEdgeScope            []string `yaml:"adaptive_edge_scope"`         // relationship types eligible; pilot default = ["learned"]
	AdaptiveEdgeAlpha            float64  `yaml:"adaptive_edge_alpha"`         // log multiplier coefficient
	AdaptiveEdgeCap              float64  `yaml:"adaptive_edge_cap"`           // max effective multiplier vs base weight
	AdaptiveEdgeDecayLambda      float64  `yaml:"adaptive_edge_decay_lambda"`  // temporal decay constant λ; uplift × exp(-λ × days); 0 = no decay

	// Retrieval session logging — required for adaptive reinforcement.
	// Off by default; persists session ID + loaded memory list per retrieve
	// call so a later citation can identify which neighbors of the cited
	// memory were loaded together.
	RetrievalSessionLogEnabled        bool `yaml:"retrieval_session_log_enabled"`
	RetrievalSessionLogRetentionDays  int  `yaml:"retrieval_session_log_retention_days"`

	// User modeling
	ObservationConfidenceCaps map[int]float64    `yaml:"observation_confidence_caps"`
	UserFacetDecayRates       map[string]float64 `yaml:"user_facet_decay_rates"`
	ProfileDecayRates         map[string]float64 `yaml:"profile_decay_rates"`
	ProfileCreationThreshold  int                `yaml:"profile_creation_threshold"`
	ProfileConfidenceFloor    float64            `yaml:"profile_confidence_floor"`
	ProfileRevisionThreshold  int                `yaml:"profile_revision_threshold"`
	ProfileImmuneToArchival   bool               `yaml:"profile_immune_to_archival"`

	// Recall tracking — logs memory retrieval events per user-prompt-submit.
	// Measures system utilization by category; feeds a time-series log for
	// graphing recall frequency vs vault depth over time.
	// verbosity: summary (counts only) | verbose (counts + memory slugs)
	RecallTrackingEnabled   bool   `yaml:"recall_tracking_enabled"`
	RecallTrackingVerbosity string `yaml:"recall_tracking_verbosity"`
	RecallTrackingLogPath   string `yaml:"recall_tracking_log_path"`
	RecallLogRetentionDays  int    `yaml:"recall_log_retention_days"`

	// Encrypted backup (cloud-password-manager model — local age encryption,
	// blob-only transport, key never leaves the machine).
	// Flat-key namespace: backup_* scalars in Config.md.
	BackupEnabled         bool   `yaml:"backup_enabled"`
	BackupAgeRecipient    string `yaml:"backup_age_recipient"`     // age1... public key (plaintext-safe)
	BackupAgeIdentityPath string `yaml:"backup_age_identity_path"` // path to AGE-SECRET-KEY file (never committed)
	BackupLocalTargetDir  string `yaml:"backup_local_target_dir"`  // durability floor — always written first
	BackupRemoteURL       string `yaml:"backup_remote_url"`        // git URL for encrypted-blob repo (optional)
	BackupRemoteClonePath string `yaml:"backup_remote_clone_path"` // local working clone of remote_url
	BackupPushOnBackup    bool   `yaml:"backup_push_on_backup"`
	BackupRetentionKeepLast int  `yaml:"backup_retention_keep_last"` // keep N most recent blobs in local dir
	BackupCooldownMinutes   int  `yaml:"backup_cooldown_minutes"`    // min interval between auto-backups
}

// DefaultConfig returns the default configuration matching Config.md.
func DefaultConfig() Config {
	return Config{
		RetrievalThreshold: 0.3,
		MaxMemoriesLoaded:  15,

		BufferThreshold:                  10,
		ConsolidationDepth:               "standard",
		MaxHolds:                         2,
		AutoConsolidationEnabled:         true,
		AutoConsolidationCooldownMinutes: 30,

		ContextPenaltyPartial:   0.7,
		ContextPenaltyOrphan:    0.5,
		DiscardAmbiguousOrphans: true,

		DecayRates: map[string]float64{
			"user":              0.3,
			"feedback":          0.3,
			"project":           0.4,
			"reference":         0.35,
			"semantic":          0.2,
			"episodic":          0.05,
			"training_override": 0.1,
			"knowledge":         0.0, // no time-based decay — only superseded or marked obsolete
		},

		// Activation floors prevent durable memory types from becoming
		// unretrievable during topic-dormant periods. The formula
		// activation × relevance × confidence goes negative for memories not
		// accessed recently; a floor clamps the multiplier so relevance still
		// contributes. Types representing ephemeral context (project, reference)
		// intentionally have no floor — their decay is load-bearing.
		ActivationFloors: map[string]float64{
			"knowledge":         1.0, // no time decay — fixed at 1.0
			"episodic":          0.7, // session summaries — most durable
			"training_override": 0.6, // immune to archival, high floor
			"user":              0.4, // durable profile data
			"feedback":          0.4, // durable behavioral rules
			"semantic":          0.3, // topic-dormant abstractions, not forgotten
			"project":           0.0, // ephemeral context — full decay intended
			"reference":         0.0, // can go stale — full decay intended
		},

		// Progressive compression thresholds — days since last access before
		// fidelity transitions. Tuned for an early-stage memory system where
		// observation history is short; kept generous to give memories time
		// to demonstrate staying power before any compression decision.
		CompressionThresholds: map[string]float64{
			"significant_full_to_detailed":    120,
			"significant_detailed_to_summary": 365,
			"significant_summary_to_gist":     1095,
			"moderate_full_to_detailed":       60,
			"moderate_detailed_to_summary":    180,
			"moderate_summary_to_gist":        540,
			"minor_full_to_detailed":          21,
			"minor_detailed_to_summary":       60,
			"minor_summary_to_gist":           180,
		},

		ConfidenceReinforce:   0.1,
		ConfidenceContradict:  0.3,
		ConfidenceStaleFactor: 0.9,
		StaleThresholdDays:    30,

		OverrideConfidenceFloor:  0.7,
		OverrideImmuneToArchival: true,

		SurpriseBonusWeight: 0.5,

		ArchiveInsteadOfDelete: true,

		ReferencesDir: "D:/References",

		RateSeparationEnabled:               true,
		RateSeparationMatureThreshold:       10,
		RateSeparationCrystallizedThreshold: 25,
		RateSeparationMinSessions:           2,

		DaydreamJudgeEnabled:                true,
		DaydreamJudgeThreshold:              0.4,
		DaydreamJudgeCandidates:             3,
		DaydreamRedundancyFallbackDampening: 0.3,

		JudgeCLIFallbackEnabled: true,
		JudgeCLIMaxConcurrent:   2,

		AutoDaydreamEnabled:                      false,
		AutoDaydreamIntervalMinMinutes:           60,
		AutoDaydreamIntervalMaxMinutes:           180,
		AutoDaydreamMaxPerDayActive:              12,
		AutoDaydreamMaxPerDayQuiet:               6,
		AutoDaydreamQuietHours:                   "",
		AutoDaydreamQuietHoursTimezone:           "local",
		AutoDaydreamActiveSkipWindowMinutes:      45,
		AutoDaydreamQuietSkipWindowMinutes:       60,
		AutoDaydreamActivitySources:              []string{"buffer", "heartbeat"},
		AutoDaydreamActiveSeedSources: map[string]float64{
			"buffer":    30,
			"project":   20,
			"knowledge": 20,
			"semantic":  15,
			"episodic":  10,
			"reference": 5,
		},
		AutoDaydreamQuietExplorationSeedSources: map[string]float64{
			"knowledge": 25,
			"semantic":  25,
			"episodic":  20,
			"project":   15,
			"reference": 10,
			"buffer":    5,
		},
		AutoDaydreamStrategyExplorationBase:      0.5,
		AutoDaydreamStrategyReplayBase:           0.5,
		AutoDaydreamStrategyAdaptive:             false,
		AutoDaydreamStrategyBufferPressureFactor: 1.5,
		AutoDaydreamReplayRecentSource:           "buffer",
		AutoDaydreamReplayRecentMaxAgeDays:       14,
		AutoDaydreamReplayStableFilter:           "crystallized",
		AutoDaydreamReplayStableCategories:       []string{"semantic", "user", "feedback"},
		AutoDaydreamOverrideMode:                 "",
		AutoDaydreamSurfaceToSession:             true,
		AutoDaydreamSurfaceMaxAgeHours:           12,
		AutoDaydreamSurfaceRelevanceThreshold:    0.4,
		AutoDaydreamSurfaceMaxPerPrompt:          4,
		AutoDaydreamLogRotationThreshold:         1000,
		AutoDaydreamValueJudgeEnabled:            true,

		DaydreamSchedulerHost:              HostClaudeCode,
		DaydreamSchedulerMissingInvoker:    DaydreamSchedulerMissingInvokerSkip,
		DaydreamVolleyPolicy:               DaydreamVolleyPolicyDelegateActive,
		DaydreamVolleyCommitmentTTLMinutes: 20,

		SpreadingActivationFactor: 0.3,
		MaxActivationHops:         1,
		FanDiscountFormula:        "log",
		EdgeWeights: map[string]float64{
			"related-to":   0.5,
			"refines":      0.7,
			"contradicts":  0.8,
			"depends-on":   0.6,
			"supersedes":   0.2,
			"instance-of":  0.4,
			"learned":      0.4,
		},

		AdaptiveEdgeWeightingEnabled: false,
		AdaptiveEdgeScope:            []string{"learned"},
		AdaptiveEdgeAlpha:            0.2,
		AdaptiveEdgeCap:              2.0,
		AdaptiveEdgeDecayLambda:      0.003851, // 180-day half-life: ln(2)/180

		RetrievalSessionLogEnabled:       false,
		RetrievalSessionLogRetentionDays: 14,

		ObservationConfidenceCaps: map[int]float64{
			1: 0.6,
			2: 0.8,
			3: 0.8,
			4: 0.95,
		},
		UserFacetDecayRates: map[string]float64{
			"identity":      0.3,
			"cognition":     0.2,
			"communication": 0.2,
			"expertise":     0.3,
			"motivation":    0.4,
			"personality":   0.15,
			"preferences":   0.3,
			"patterns":      0.15,
		},
		ProfileDecayRates: map[string]float64{
			"identity":      0.15,
			"cognition":     0.10,
			"communication": 0.10,
			"expertise":     0.15,
			"motivation":    0.20,
			"personality":   0.05,
			"preferences":   0.12,
			"patterns":      0.08,
		},
		ProfileCreationThreshold: 3,
		ProfileConfidenceFloor:   0.5,
		ProfileRevisionThreshold: 2,
		ProfileImmuneToArchival:  true,

		RecallTrackingEnabled:   true,
		RecallTrackingVerbosity: "summary",
		RecallTrackingLogPath:   "Metrics/recall_log.jsonl",
		RecallLogRetentionDays:  30,

		// Backup defaults — disabled until the user runs `jm backup --init-key`
		// and explicitly enables. Local target defaults to a sibling of the
		// vault root so a fresh install lands somewhere predictable; override
		// in Config.md or via JM_BACKUP_LOCAL_TARGET_DIR.
		BackupEnabled:           false,
		BackupAgeRecipient:      "",
		BackupAgeIdentityPath:   "", // resolved at runtime — see resolveBackupIdentityPath
		BackupLocalTargetDir:    "", // resolved at runtime — see resolveBackupLocalTargetDir
		BackupRemoteURL:         "",
		BackupRemoteClonePath:   "",
		BackupPushOnBackup:      true,
		BackupRetentionKeepLast: 0, // 0 = keep every backup (recovery + conflict-resolution intent)
		BackupCooldownMinutes:   60,
	}
}

// MachineRegistry is the top-level structure for System/machines.json.
type MachineRegistry struct {
	SchemaVersion int                       `json:"schema_version"`
	Machines      map[string]MachineEntry   `json:"machines"`
	Tooling       map[string]ToolEntry      `json:"tooling"`
}

// MachineEntry describes a host Claude may interact with.
type MachineEntry struct {
	DisplayName   string     `json:"display_name"`
	Platform      string     `json:"platform"`        // windows | linux | linux-wsl | macos
	Current       bool       `json:"current,omitempty"`
	Hostname      string     `json:"hostname,omitempty"`
	IP            string     `json:"ip,omitempty"`
	User          string     `json:"user,omitempty"`
	Elevation     string     `json:"elevation"`       // none | user | prompt | full
	ElevationNote string     `json:"elevation_note,omitempty"`
	SSH           *SSHConfig `json:"ssh,omitempty"`
	Status        string     `json:"status,omitempty"` // "" = ready | "unconfigured"
	Notes         string     `json:"notes,omitempty"`
}

// SSHConfig describes how to connect to a machine via SSH.
type SSHConfig struct {
	Method string   `json:"method"` // windows-native-openssh | standard
	Binary string   `json:"binary"` // full path to ssh binary
	Key    string   `json:"key"`    // full path to private key
	Flags  []string `json:"flags,omitempty"`
	Notes  string   `json:"notes,omitempty"`
}

// ToolEntry describes a tool and where it is installed across machines.
type ToolEntry struct {
	Description string                     `json:"description"`
	Install     string                     `json:"install"` // auto | manual | n/a
	Type        string                     `json:"type"`    // executable | mcp | service | library
	Machines    map[string]ToolMachineEntry `json:"machines"`
}

// ToolMachineEntry describes a tool's installation on a specific machine.
type ToolMachineEntry struct {
	Path  string `json:"path"`
	Notes string `json:"notes,omitempty"`
}

// ScoredMemory pairs a memory with its computed retrieval score and breakdown.
type ScoredMemory struct {
	Memory     *MemoryEntry
	Activation float64
	Relevance  float64
	Confidence float64
	Surprise   float64
	Boost      float64 // spreading activation from neighbors
	Total      float64
}

// ConsolidationAction represents what to do with a buffer entry.
type ConsolidationAction string

const (
	ActionPromote     ConsolidationAction = "promote"
	ActionHold        ConsolidationAction = "hold"
	ActionHoldRateSep ConsolidationAction = "hold-rate-sep" // held by CLS rate-separation gate
	ActionDiscard     ConsolidationAction = "discard"
)

// BufferAssessment is the result of evaluating a buffer entry for consolidation.
type BufferAssessment struct {
	Entry            *BufferEntry
	ContextIntegrity ContextIntegrity
	ContextPenalty   float64
	Redundancy       float64
	RecencyFactor    float64
	RetentionScore   float64
	Action           ConsolidationAction
	Reason           string

	// Daydream redundancy judgment (only populated when the daydream judge fires)
	DaydreamVerdict       string // "novel" | "redundant" | "partial" | "" (not evaluated) | "fallback" (API failure)
	DaydreamVerdictReason string

	// Daydream value judgment — gates retention by insight density. Only
	// populated for daydream-sourced entries when AutoDaydreamValueJudgeEnabled.
	DaydreamValueVerdict ValueVerdict // "valuable" | "marginal" | "low-value" | ""
	DaydreamValueReason  string
}

// ConsolidationReport summarizes a consolidation run.
type ConsolidationReport struct {
	Timestamp         time.Time
	Trigger           string
	Depth             string
	BufferAssessments []BufferAssessment
	MemoriesDecayed   []string
	MemoriesArchived  []string // legacy — kept for backward compat; consolidation no longer hard-archives
	MemoriesQueued    []string // memories with target_fidelity set by this consolidation pass
	Promoted          int
	Held              int
	RateSepHeld       int // entries held specifically by CLS rate-separation gate
	Discarded         int
	LLMPrompts        []string // judgment calls for the LLM
}
