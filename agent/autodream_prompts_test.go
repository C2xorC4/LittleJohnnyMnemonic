package main

import (
	"errors"
	"strings"
	"testing"
	"time"
)

func sampleSeed() *Seed {
	return &Seed{
		Source:      "knowledge",
		FilePath:    "/vault/Memory/Knowledge/some_entry.md",
		Title:       "Some Entry",
		LastTouched: time.Date(2026, 4, 30, 10, 0, 0, 0, time.UTC),
	}
}

func samplePair() *SeedPair {
	return &SeedPair{
		Recent: Seed{
			Source:   "buffer",
			FilePath: "/vault/Buffer/recent_observation.md",
			Title:    "recent_observation",
		},
		Stable: Seed{
			Source:   "semantic",
			FilePath: "/vault/Memory/Semantic/stable_trait.md",
			Title:    "Stable Trait",
		},
	}
}

func sampleInputs() PromptInputs {
	return PromptInputs{
		Mode:      ModeActive,
		Strategy:  StrategyExploration,
		Seed:      sampleSeed(),
		VaultRoot: "/vault",
		Now:       time.Date(2026, 4, 30, 14, 30, 0, 0, time.UTC),
	}
}

func TestBuildPrompt_ActiveRequiresSeed(t *testing.T) {
	in := sampleInputs()
	in.Seed = nil
	_, err := BuildPrompt(TemplateActive, in)
	if !errors.Is(err, ErrInvalidPromptInputs) {
		t.Errorf("err = %v, want ErrInvalidPromptInputs", err)
	}
}

func TestBuildPrompt_QuietExplorationRequiresSeed(t *testing.T) {
	in := sampleInputs()
	in.Seed = nil
	_, err := BuildPrompt(TemplateQuietExploration, in)
	if !errors.Is(err, ErrInvalidPromptInputs) {
		t.Errorf("err = %v, want ErrInvalidPromptInputs", err)
	}
}

func TestBuildPrompt_QuietReplayRequiresPair(t *testing.T) {
	in := sampleInputs()
	in.Pair = nil
	_, err := BuildPrompt(TemplateQuietReplay, in)
	if !errors.Is(err, ErrInvalidPromptInputs) {
		t.Errorf("err = %v, want ErrInvalidPromptInputs", err)
	}
}

func TestBuildPrompt_UnknownTemplateErrors(t *testing.T) {
	_, err := BuildPrompt(PromptTemplate("garbage"), sampleInputs())
	if !errors.Is(err, ErrInvalidPromptInputs) {
		t.Errorf("err = %v, want ErrInvalidPromptInputs", err)
	}
}

func TestBuildPrompt_ActiveContainsSeedPathAndContext(t *testing.T) {
	prompt, err := BuildPrompt(TemplateActive, sampleInputs())
	if err != nil {
		t.Fatal(err)
	}
	wantSubstrings := []string{
		"automatic background daydream",
		"active work hours",
		"NOT available for triage",
		"/vault/Memory/Knowledge/some_entry.md",
		"Some Entry",
		"daydream_kind: exploration",
		"daydream_mode: active",
		"2026-04-30",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("active prompt missing substring %q\nprompt:\n%s", s, prompt)
		}
	}
}

func TestBuildPrompt_QuietExplorationContainsDreamLanguage(t *testing.T) {
	in := sampleInputs()
	in.Mode = ModeQuiet
	in.Strategy = StrategyExploration
	prompt, err := BuildPrompt(TemplateQuietExploration, in)
	if err != nil {
		t.Fatal(err)
	}
	wantSubstrings := []string{
		"quiet hours",
		"dream-like wandering",
		"NO\nexpectation of utility",
		"daydream_mode: quiet",
		"daydream_kind: exploration",
		"/vault/Memory/Knowledge/some_entry.md",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("quiet-exploration prompt missing substring %q\nprompt:\n%s", s, prompt)
		}
	}
}

func TestBuildPrompt_QuietReplayContainsBothPathsAndVerdicts(t *testing.T) {
	in := sampleInputs()
	in.Mode = ModeQuiet
	in.Strategy = StrategyReplay
	in.Seed = nil
	in.Pair = samplePair()
	prompt, err := BuildPrompt(TemplateQuietReplay, in)
	if err != nil {
		t.Fatal(err)
	}
	wantSubstrings := []string{
		"interleaved-replay",
		"CLS-style integration",
		"/vault/Buffer/recent_observation.md",
		"/vault/Memory/Semantic/stable_trait.md",
		"reinforce",
		"refine",
		"contradict",
		"unrelated",
		"daydream_kind: replay-",
		"relationship:",
		"paired_with:",
		"recent_seed:",
	}
	for _, s := range wantSubstrings {
		if !strings.Contains(prompt, s) {
			t.Errorf("quiet-replay prompt missing substring %q\nprompt:\n%s", s, prompt)
		}
	}
}

func TestBuildPrompt_QuietReplayBreadcrumbPolicyByVerdict(t *testing.T) {
	in := sampleInputs()
	in.Pair = samplePair()
	prompt, err := BuildPrompt(TemplateQuietReplay, in)
	if err != nil {
		t.Fatal(err)
	}
	// Breadcrumb routing is load-bearing for the audit pipeline. Verify each
	// verdict's policy is explicitly stated.
	for _, want := range []string{
		"reinforce",
		"refine",
		"contradict",
		"unrelated",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("replay prompt missing verdict %q", want)
		}
	}
	// Reinforce and unrelated must explicitly tell the agent NOT to write.
	if !strings.Contains(prompt, "DO NOT write a breadcrumb") {
		t.Errorf("replay prompt should restrict breadcrumb writing for reinforce + unrelated")
	}
	// Refine and contradict route through replay_reinforcements / contradictions.
	if !strings.Contains(prompt, "replay_reinforcements.jsonl") {
		t.Errorf("replay prompt should mention replay_reinforcements.jsonl routing for reinforce")
	}
}

func TestBuildPrompt_QuietReplayHasStrictVerdictLineFormat(t *testing.T) {
	in := sampleInputs()
	in.Pair = samplePair()
	prompt, err := BuildPrompt(TemplateQuietReplay, in)
	if err != nil {
		t.Fatal(err)
	}
	// The orchestrator's verdict parser requires "Verdict: <word>" at end of
	// response text. The prompt must instruct the agent to produce it.
	if !strings.Contains(prompt, "Verdict: <reinforce|refine|contradict|unrelated>") {
		t.Errorf("replay prompt missing verdict-line format spec")
	}
	if !strings.Contains(prompt, "REQUIRED") {
		t.Errorf("replay prompt should mark the verdict line as required")
	}
}

func TestBuildPrompt_QuietReplaySpecifiesPriorityRouting(t *testing.T) {
	in := sampleInputs()
	in.Pair = samplePair()
	prompt, err := BuildPrompt(TemplateQuietReplay, in)
	if err != nil {
		t.Fatal(err)
	}
	// Priority levels must match the routing rules in the verdict-parser
	// task: priority: high for refine, priority: critical for contradict.
	if !strings.Contains(prompt, "priority: high") {
		t.Errorf("replay prompt missing priority: high guidance (for refine verdict)")
	}
	if !strings.Contains(prompt, "priority: critical") {
		t.Errorf("replay prompt missing priority: critical guidance (for contradict verdict)")
	}
}

func TestBuildPrompt_BreadcrumbDateFollowsNow(t *testing.T) {
	// The suggested filename should reflect the Now field, not time.Now()
	// at template render time. Critical for deterministic test runs.
	in := sampleInputs()
	in.Now = time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)
	prompt, err := BuildPrompt(TemplateActive, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, "2025-12-31") {
		t.Errorf("active prompt should embed in.Now date 2025-12-31, got prompt without it")
	}
	if strings.Contains(prompt, "2026-04-30") {
		t.Errorf("active prompt should not contain default sample date when Now was overridden")
	}
}

func TestSeedRelatedRef_StripsToVaultRelative(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/vault/Memory/Knowledge/foo.md", "Memory/Knowledge/foo"},
		{"D:\\vault\\Memory\\Semantic\\bar.md", "Memory/Semantic/bar"},
		{"/vault/Buffer/something.md", "Buffer/something"},
		{"/vault/Buffer/Daydream/dd.md", "Buffer/Daydream/dd"},
		{"", ""},
		// Path with no Memory/Buffer anchor — leave as-is, minus suffix
		{"/etc/passwd", "/etc/passwd"},
	}
	for _, c := range cases {
		got := seedRelatedRef(&Seed{FilePath: c.path})
		if got != c.want {
			t.Errorf("seedRelatedRef(%q) = %q, want %q", c.path, got, c.want)
		}
	}
}

func TestSeedRelatedRef_NilSafe(t *testing.T) {
	if got := seedRelatedRef(nil); got != "" {
		t.Errorf("nil seed = %q, want empty", got)
	}
}
