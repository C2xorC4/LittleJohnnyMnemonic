package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

// PromptTemplate identifies which prompt body to construct for a given run.
// Active mode and quiet-exploration both take a single Seed; quiet-replay
// takes a SeedPair.
type PromptTemplate string

const (
	TemplateActive           PromptTemplate = "active"
	TemplateQuietExploration PromptTemplate = "quiet-exploration"
	TemplateQuietReplay      PromptTemplate = "quiet-replay"
)

// PromptInputs is the parameter bag for prompt construction. The orchestrator
// fills this from mode/strategy resolution + seed sampling, and BuildPrompt
// renders the appropriate template.
type PromptInputs struct {
	Mode      AutodreamMode
	Strategy  AutodreamStrategy
	Seed      *Seed     // required for Active and QuietExploration
	Pair      *SeedPair // required for QuietReplay
	VaultRoot string
	Now       time.Time
}

// ErrInvalidPromptInputs is returned when required fields are missing for
// the chosen template (e.g., no Seed passed to an Active template).
var ErrInvalidPromptInputs = errors.New("autodream: invalid prompt inputs")

// BuildPrompt renders the user-facing prompt body that gets passed to
// `claude -p` (alongside the memory-daydream agent definition). The agent
// definition supplies general daydreaming behavior; the prompt body
// supplies the run-specific operational context: which seed to start
// from, what mode/strategy is in play, and how to format the breadcrumb.
func BuildPrompt(template PromptTemplate, inputs PromptInputs) (string, error) {
	switch template {
	case TemplateActive:
		if inputs.Seed == nil {
			return "", fmt.Errorf("%w: active template requires a Seed", ErrInvalidPromptInputs)
		}
		return buildActivePrompt(inputs), nil
	case TemplateQuietExploration:
		if inputs.Seed == nil {
			return "", fmt.Errorf("%w: quiet-exploration template requires a Seed", ErrInvalidPromptInputs)
		}
		return buildQuietExplorationPrompt(inputs), nil
	case TemplateQuietReplay:
		if inputs.Pair == nil {
			return "", fmt.Errorf("%w: quiet-replay template requires a SeedPair", ErrInvalidPromptInputs)
		}
		return buildQuietReplayPrompt(inputs), nil
	default:
		return "", fmt.Errorf("%w: unknown template %q", ErrInvalidPromptInputs, template)
	}
}

func buildActivePrompt(in PromptInputs) string {
	dateStr := in.Now.Format("2006-01-02")
	bcDir := suggestBreadcrumbDir(in.VaultRoot)

	return fmt.Sprintf(`You are operating as an automatic background daydream during active work hours.
There is no live conversation — the user is engaged with another task and is
NOT available for triage right now. Your output is persisted to a buffer file
and routed through the standard daydream consolidation path; nobody is
waiting on it.

**Starting point:** %s/%s
**Path to read:** %s

Read the entry. Pick ONE exploration path that fits this run — link walk,
tag cluster, gap detection, adversarial inversion, cross-architecture, or
temporal check (web search). Active-mode runs are workflow-adjacent: the
goal is to surface connections between this entry and recent or current
work that the entry doesn't state explicitly. Recency-biased seeds make
that connection plausible — chase it.

If you find something genuinely interesting (a connection, a gap, a
question, an update), persist a breadcrumb:

  Path: %s/%s_daydream-<brief-description>.md
  Frontmatter:
    type: buffer
    timestamp: <ISO 8601>
    source: daydream
    daydream_kind: exploration
    daydream_mode: active
    surprise: <0.3-0.7>
    context_integrity: full
    tags: [daydream, ...topic tags]
    related: ["[[%s]]"]

Body: the finding itself, self-contained, under 300 words.

If the exploration dead-ends, return a null result and skip the breadcrumb.
Don't manufacture insights to justify the run.`,
		in.Seed.Source, in.Seed.Title,
		in.Seed.FilePath,
		bcDir, dateStr,
		seedRelatedRef(in.Seed))
}

func buildQuietExplorationPrompt(in PromptInputs) string {
	dateStr := in.Now.Format("2006-01-02")
	bcDir := suggestBreadcrumbDir(in.VaultRoot)

	return fmt.Sprintf(`You are operating as an automatic background daydream during quiet hours.
There is no live conversation. This is dream-like wandering — there is NO
expectation of utility to current work. The point is surprise: connections
that wouldn't surface during focused activity, gaps the daylight self
doesn't notice, juxtapositions that only sleep produces.

**Starting point:** %s/%s
**Path to read:** %s

Read the entry. Wander. Follow the most interesting thread — link walk,
tag cluster, gap detection, cross-architecture comparison, adversarial
inversion. Don't try to be useful. Don't connect this back to recent work
unless the connection is itself unexpected. The seed was chosen uniformly
across the graph; let the wandering match.

If you find something genuinely interesting, persist a breadcrumb:

  Path: %s/%s_daydream-<brief-description>.md
  Frontmatter:
    type: buffer
    timestamp: <ISO 8601>
    source: daydream
    daydream_kind: exploration
    daydream_mode: quiet
    surprise: <0.3-0.7>
    context_integrity: full
    tags: [daydream, ...topic tags]
    related: ["[[%s]]"]

Body: the finding itself, self-contained, under 300 words.

Null results are expected. Most dreams aren't useful — the value comes from
the rare ones that are. Report null cleanly; don't pad.`,
		in.Seed.Source, in.Seed.Title,
		in.Seed.FilePath,
		bcDir, dateStr,
		seedRelatedRef(in.Seed))
}

func buildQuietReplayPrompt(in PromptInputs) string {
	dateStr := in.Now.Format("2006-01-02")
	bcDir := suggestBreadcrumbDir(in.VaultRoot)
	recent := in.Pair.Recent
	stable := in.Pair.Stable

	return fmt.Sprintf(`You are operating as an automatic interleaved-replay daydream during quiet
hours. There is no live conversation. This run is NOT exploration — it is
CLS-style integration: you are evaluating whether a recent observation
reinforces, refines, contradicts, or simply doesn't relate to a stable
trait in long-term memory. The pairing is the point; do not wander off it.

**Recent trace:** %s/%s
  Path: %s

**Stable trait:** %s/%s
  Path: %s

Read both. Decide on ONE verdict:

- **reinforce**  — the recent trace strengthens the stable trait without
                   substantive new content. No new claim, just another
                   instance of the same pattern.
- **refine**     — the recent trace adds nuance, exception, or detail
                   that the stable trait should incorporate.
- **contradict** — the recent trace conflicts with the stable trait;
                   either the trait or the trace must be wrong.
                   Surface this as a flag, not an attempt to resolve it.
- **unrelated**  — the pairing was random; the recent trace and the
                   stable trait don't actually integrate.

**Breadcrumb policy** (per verdict):

- **reinforce** — DO NOT write a breadcrumb file. The orchestrator records
  the verdict to Metrics/replay_reinforcements.jsonl, which the next
  consolidation pass uses to adjust the stable trait's confidence. Just
  include the verdict line and your reasoning in the response text.

- **refine** — Write a breadcrumb to:
    %s/%s_daydream-replay-<brief-description>.md
  Frontmatter must include:
    daydream_kind: replay-refine
    daydream_mode: quiet
    priority: high
    relationship: refine

- **contradict** — Write a breadcrumb to:
    %s/%s_daydream-replay-<brief-description>.md
  Frontmatter must include:
    daydream_kind: replay-contradict
    daydream_mode: quiet
    priority: critical
    relationship: contradict

- **unrelated** — DO NOT write a breadcrumb. The orchestrator records this
  outcome to the audit log only.

For breadcrumb files (refine and contradict), the full frontmatter is:
    type: buffer
    timestamp: <ISO 8601>
    source: daydream
    daydream_kind: replay-<verdict>
    daydream_mode: quiet
    relationship: <verdict>
    priority: <high|critical>
    paired_with: ["[[%s]]"]
    recent_seed: ["[[%s]]"]
    surprise: <0.3-0.7>
    context_integrity: full
    tags: [daydream, replay, ...topic tags]

Body (under 250 words):
  - Recent trace summary: <one-line>
  - Stable trait summary: <one-line>
  - Reasoning: <2-4 sentences explaining the relationship>

**Verdict line format** (REQUIRED — parsed by the orchestrator):

End your response text with a single line in this exact form:

    Verdict: <reinforce|refine|contradict|unrelated>

The orchestrator's parser keys off this line to route the result. Without
it, the run is recorded as "verdict-parse-failed" and the routing falls
back to a no-op.`,
		recent.Source, recent.Title, recent.FilePath,
		stable.Source, stable.Title, stable.FilePath,
		bcDir, dateStr,
		bcDir, dateStr,
		seedRelatedRef(&stable),
		seedRelatedRef(&recent))
}

// suggestBreadcrumbDir returns the absolute path used in prompt-template
// breadcrumb examples. Centralized so any future move of the daydream
// drop dir only requires a single edit.
func suggestBreadcrumbDir(vaultRoot string) string {
	return filepath.Join(vaultRoot, "Buffer", "Daydream")
}

// seedRelatedRef builds the wikilink target string used in breadcrumb
// `related` and `paired_with` frontmatter fields. The form is what the
// existing parser and graph code already expects: relative-from-vault-root
// path without the .md suffix, e.g. "Memory/Semantic/cog_cls".
func seedRelatedRef(s *Seed) string {
	if s == nil || s.FilePath == "" {
		return ""
	}
	// Best-effort: strip everything before the first "Memory/" or "Buffer/"
	// segment so the link target is vault-root-relative.
	clean := filepath.ToSlash(s.FilePath)
	for _, anchor := range []string{"/Memory/", "/Buffer/"} {
		if i := strings.Index(clean, anchor); i >= 0 {
			clean = clean[i+1:]
			break
		}
	}
	clean = strings.TrimSuffix(clean, ".md")
	return clean
}
