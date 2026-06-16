package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Daydream value judge: scores a daydream-flavored buffer entry on whether
// it has substantive insight value, independent of redundancy. Replaces
// user-engagement as the consolidation gate (see the
// 2026-04-30 "user-engagement-not-reliable-triage-signal" buffer entry for
// the design rationale).
//
// This judge is orthogonal to the redundancy judge (consolidation_judge.go):
//   - redundancy judge: "is this already in LTM?"
//   - value judge:      "even if novel, is the finding substantive enough to keep?"
//
// Both may fire on the same entry; consolidation combines the verdicts.
//
// Fires only on daydream-sourced entries (source == "daydream"). Other
// buffer sources are user-stated and don't need value gating.

const daydreamValueSystemPrompt = `You evaluate whether a daydream-sourced buffer entry has substantive insight value, independent of whether it overlaps with existing memories.

A daydream is a brief observation produced by an autonomous exploration process — not a user-stated fact, not a deliberately-curated memory. Many daydreams are vague, speculative, or just rephrasings of well-known concepts. A few articulate a real connection, gap, question, or update that would be worth retaining.

You will be given the buffer entry's body. Your job: decide whether it has enough insight density to be worth keeping.

Respond with VALID JSON only, no prose around it:
{"verdict": "valuable" | "marginal" | "low-value", "reason": "<one short sentence>"}

- valuable: states a concrete connection, gap, question, or update with specific named entities or structural claims. The entry says something — not just gestures at a topic. Worth keeping even if other consolidation signals are weak.
- marginal: identifies a real concept but stays at the level of vague gesture. Defensible to keep or drop; consolidation logic should default to drop unless surprise is high.
- low-value: rephrases existing knowledge in less precise terms, makes no specific claim, or identifies a "topic" without an actual finding. Drop.

Score on what the entry SAYS, not what it gestures at. A vague pointer to an interesting area without a concrete finding is low-value, not valuable. Length alone does not imply value — long entries can be padded; short entries can be sharp.`

// ValueVerdict is the daydream-value-judge's classification.
type ValueVerdict string

const (
	ValueValuable ValueVerdict = "valuable"
	ValueMarginal ValueVerdict = "marginal"
	ValueLowValue ValueVerdict = "low-value"
)

// IsDaydreamSourced returns true if a buffer entry came from autodream
// (or the manual memory-daydream subagent). The value judge fires only
// on these.
func IsDaydreamSourced(entry *BufferEntry) bool {
	if entry == nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(entry.Source), "daydream")
}

// JudgeDaydreamValue runs the value judge on a single daydream-sourced
// buffer entry. Uses the tiered transport from judge_api.go (API → CLI →
// error). On error, the caller should keep the entry by default — better
// to retain a marginal daydream than to drop it because the judge was
// unreachable.
func JudgeDaydreamValue(entry *BufferEntry, cliFallback bool, cliMaxConcurrent int) (ValueVerdict, string, error) {
	if entry == nil {
		return "", "", fmt.Errorf("daydream value judge: nil entry")
	}
	userContent := buildValueJudgeMessage(entry)
	rawText, _, err := callHaikuJudge(daydreamValueSystemPrompt, userContent, 250, cliFallback, cliMaxConcurrent)
	if err != nil {
		return "", "", err
	}
	verdict, reason := parseValueVerdict(rawText)
	if verdict == "" {
		return "", "", fmt.Errorf("unparseable verdict: %s", truncateJudge(rawText, 200))
	}
	return verdict, reason, nil
}

func buildValueJudgeMessage(entry *BufferEntry) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Buffer entry (file=%s):\n---\n", entry.FileName)
	b.WriteString(excerptText(entry.Body, 1500))
	b.WriteString("\n---\n\n")

	// Surface daydream-specific frontmatter that may inform the judgment.
	// Tags help the judge see the breadth of topics; surprise tells it the
	// agent's own self-assessment of how unexpected the finding was.
	if len(entry.Tags) > 0 {
		fmt.Fprintf(&b, "Tags: %s\n", strings.Join(entry.Tags, ", "))
	}
	if entry.Surprise > 0 {
		fmt.Fprintf(&b, "Self-assessed surprise: %.2f\n", entry.Surprise)
	}

	b.WriteString("\nDoes this entry have substantive insight value?")
	return b.String()
}

// parseValueVerdict extracts the JSON verdict object from the model's reply.
// Empty verdict means unparseable. Reuses the jsonObjectRegex from the
// rule judge.
func parseValueVerdict(text string) (ValueVerdict, string) {
	candidates := jsonObjectRegex.FindAllString(text, -1)
	for _, c := range candidates {
		var v judgeVerdict
		if err := json.Unmarshal([]byte(c), &v); err != nil {
			continue
		}
		switch ValueVerdict(v.Verdict) {
		case ValueValuable, ValueMarginal, ValueLowValue:
			return ValueVerdict(v.Verdict), v.Reason
		}
	}
	return "", ""
}
