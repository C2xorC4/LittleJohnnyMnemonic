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
type Link struct {
	Target       string `yaml:"target"`
	Relationship string `yaml:"relationship"`
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
	BufferThreshold    int    `yaml:"buffer_threshold"`
	ConsolidationDepth string `yaml:"consolidation_depth"`
	MaxHolds           int    `yaml:"max_holds"`

	// Context integrity
	ContextPenaltyPartial    float64 `yaml:"context_penalty_partial"`
	ContextPenaltyOrphan     float64 `yaml:"context_penalty_orphan"`
	DiscardAmbiguousOrphans  bool    `yaml:"discard_ambiguous_orphans"`

	// Decay rates by type
	DecayRates map[string]float64 `yaml:"decay_rates"`

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

	// Associative retrieval
	SpreadingActivationFactor float64            `yaml:"spreading_activation_factor"`
	MaxActivationHops         int                `yaml:"max_activation_hops"`
	EdgeWeights               map[string]float64 `yaml:"edge_weights"`
	FanDiscountFormula        string             `yaml:"fan_discount_formula"` // "log" | "sqrt" | "linear" | "none"

	// User modeling
	ObservationConfidenceCaps map[int]float64    `yaml:"observation_confidence_caps"`
	UserFacetDecayRates       map[string]float64 `yaml:"user_facet_decay_rates"`
	ProfileDecayRates         map[string]float64 `yaml:"profile_decay_rates"`
	ProfileCreationThreshold  int                `yaml:"profile_creation_threshold"`
	ProfileConfidenceFloor    float64            `yaml:"profile_confidence_floor"`
	ProfileRevisionThreshold  int                `yaml:"profile_revision_threshold"`
	ProfileImmuneToArchival   bool               `yaml:"profile_immune_to_archival"`

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

		BufferThreshold:    20,
		ConsolidationDepth: "standard",
		MaxHolds:           2,

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
		BackupRetentionKeepLast: 30,
		BackupCooldownMinutes:   60,
	}
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
