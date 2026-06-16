package main

import "time"

// BenchmarkGroundTruth defines automated grading checks on assistant text.
type BenchmarkGroundTruth struct {
	RequiredSubstrings    []string `json:"required_substrings,omitempty"`
	RequiredSubstringsAny []string `json:"required_substrings_any,omitempty"`
	ForbiddenSubstrings   []string `json:"forbidden_substrings,omitempty"`
}

// BenchmarkGrading holds per-task scoring weights.
type BenchmarkGrading struct {
	FactualWeight     float64 `json:"factual_weight,omitempty"`
	ComplianceWeight  float64 `json:"compliance_weight,omitempty"`
	PassThreshold     float64 `json:"pass_threshold"`
	Note              string  `json:"note,omitempty"`
}

// BenchmarkTask is a single benchmark scenario definition.
type BenchmarkTask struct {
	ID                   string               `json:"id"`
	Tier                 int                  `json:"tier"`
	Title                string               `json:"title"`
	Prompt               string               `json:"prompt"`
	Cwd                  string               `json:"cwd"`
	ToolPolicy           string               `json:"tool_policy"`
	ExpectedMemoryKeys   []string             `json:"expected_memory_keys,omitempty"`
	AcceptableMemoryKeys []string             `json:"acceptable_memory_keys,omitempty"`
	ForbiddenMemoryKeys  []string             `json:"forbidden_memory_keys,omitempty"`
	GroundTruth          BenchmarkGroundTruth `json:"ground_truth"`
	Grading              BenchmarkGrading     `json:"grading"`
}

// BenchmarkManifest indexes the benchmark fixture.
type BenchmarkManifest struct {
	Version     int      `json:"version"`
	Description string   `json:"description"`
	FixtureVault string  `json:"fixture_vault"`
	FixtureRepo  string  `json:"fixture_repo"`
	TasksDir     string  `json:"tasks_dir"`
	Arms         []string `json:"arms"`
	ToolPolicies []string `json:"tool_policies"`
}

// BenchmarkRunMeta records a manual or semi-automated benchmark run.
type BenchmarkRunMeta struct {
	RunID       string    `json:"run_id"`
	StartedAt   time.Time `json:"started_at"`
	Host        string    `json:"host"`
	Arm         string    `json:"arm"`
	ToolPolicy  string    `json:"tool_policy,omitempty"`
	VaultRoot   string    `json:"vault_root"`
	RepoRoot    string    `json:"repo_root"`
	Notes       string    `json:"notes,omitempty"`
}

// BenchmarkRetrieveResult is output from retrieve-check per task.
type BenchmarkRetrieveResult struct {
	TaskID              string   `json:"task_id"`
	Prompt              string   `json:"prompt"`
	LoadedKeys          []string `json:"loaded_keys"`
	ExpectedHit         bool     `json:"expected_hit"`
	ForbiddenLoaded     []string `json:"forbidden_loaded,omitempty"`
	AcceptableOnly      bool     `json:"acceptable_only,omitempty"`
	TopScore            float64  `json:"top_score"`
}

// TranscriptTurnMetrics captures per-turn timing and tool usage.
type TranscriptTurnMetrics struct {
	TurnIndex       int      `json:"turn_index"`
	DurationSeconds float64  `json:"duration_seconds,omitempty"`
	DurationSource  string   `json:"duration_source,omitempty"`
	ToolCalls       []string `json:"tool_calls,omitempty"`
	WebSearchCount  int      `json:"web_search_count"`
	MemoryCiteCount int      `json:"memory_cite_count"`
}

// TranscriptMetrics is the parsed output of a host transcript.
type TranscriptMetrics struct {
	Host              string                  `json:"host"`
	TranscriptPath    string                  `json:"transcript_path"`
	ParsedAt          time.Time               `json:"parsed_at"`
	TurnCount         int                     `json:"turn_count"`
	TotalDurationSecs float64                 `json:"total_duration_seconds,omitempty"`
	Turns             []TranscriptTurnMetrics `json:"turns"`
	ToolTotals        map[string]int          `json:"tool_totals"`
}

// BenchmarkGradeResult is automated grading output for one task answer.
type BenchmarkGradeResult struct {
	TaskID      string    `json:"task_id"`
	Passed      bool      `json:"passed"`
	Score       float64   `json:"score"`
	Threshold   float64   `json:"threshold"`
	Matched     []string  `json:"matched,omitempty"`
	Missing     []string  `json:"missing,omitempty"`
	ForbiddenHit []string `json:"forbidden_hit,omitempty"`
	GradedAt    time.Time `json:"graded_at"`
}