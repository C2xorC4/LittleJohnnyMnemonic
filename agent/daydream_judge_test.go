package main

import (
	"strings"
	"testing"
)

func TestIsDaydreamSourced(t *testing.T) {
	cases := []struct {
		entry *BufferEntry
		want  bool
	}{
		{nil, false},
		{&BufferEntry{Source: "conversation"}, false},
		{&BufferEntry{Source: "daydream"}, true},
		{&BufferEntry{Source: "DAYDREAM"}, true},
		{&BufferEntry{Source: "  daydream  "}, true},
		{&BufferEntry{Source: ""}, false},
	}
	for _, c := range cases {
		got := IsDaydreamSourced(c.entry)
		if got != c.want {
			source := "<nil>"
			if c.entry != nil {
				source = c.entry.Source
			}
			t.Errorf("source=%q: got %v, want %v", source, got, c.want)
		}
	}
}

func TestParseValueVerdict_AllThree(t *testing.T) {
	cases := []struct {
		body         string
		wantVerdict  ValueVerdict
		wantContains string
	}{
		{`{"verdict": "valuable", "reason": "concrete cross-domain connection"}`, ValueValuable, "concrete"},
		{`{"verdict": "marginal", "reason": "interesting but vague"}`, ValueMarginal, "vague"},
		{`{"verdict": "low-value", "reason": "rephrases known concept"}`, ValueLowValue, "rephrases"},
		{`Sure, here's my answer:\n{"verdict": "valuable", "reason": "x"}\nDone.`, ValueValuable, "x"},
	}
	for _, c := range cases {
		v, r := parseValueVerdict(c.body)
		if v != c.wantVerdict {
			t.Errorf("body=%q: verdict = %s, want %s", c.body, v, c.wantVerdict)
		}
		if !strings.Contains(r, c.wantContains) {
			t.Errorf("body=%q: reason %q missing %q", c.body, r, c.wantContains)
		}
	}
}

func TestParseValueVerdict_InvalidVerdictRejected(t *testing.T) {
	cases := []string{
		`{"verdict": "great", "reason": "x"}`,
		`{"verdict": "", "reason": "x"}`,
		`{"verdict": "novel", "reason": "x"}`, // valid for redundancy judge but not value judge
	}
	for _, c := range cases {
		v, _ := parseValueVerdict(c)
		if v != "" {
			t.Errorf("body=%q: got verdict %s, want empty (invalid for value judge)", c, v)
		}
	}
}

func TestParseValueVerdict_NoJSONReturnsEmpty(t *testing.T) {
	v, _ := parseValueVerdict("just plain text, no json")
	if v != "" {
		t.Errorf("got %s, want empty", v)
	}
}

func TestBuildValueJudgeMessage_IncludesEntryAndMetadata(t *testing.T) {
	entry := &BufferEntry{
		FileName: "2026-04-30_daydream-some-finding.md",
		Body:     "There is a structural connection between X and Y that nobody has named yet.",
		Tags:     []string{"daydream", "cross-domain"},
		Surprise: 0.65,
	}
	msg := buildValueJudgeMessage(entry)

	if !strings.Contains(msg, entry.FileName) {
		t.Error("message missing filename")
	}
	if !strings.Contains(msg, "structural connection between X and Y") {
		t.Error("message missing entry body")
	}
	if !strings.Contains(msg, "daydream, cross-domain") {
		t.Error("message missing tags")
	}
	if !strings.Contains(msg, "0.65") {
		t.Error("message missing surprise score")
	}
	if !strings.Contains(msg, "Does this entry have substantive insight value") {
		t.Error("message missing closing question")
	}
}

func TestBuildValueJudgeMessage_HandlesEmptyOptionalFields(t *testing.T) {
	entry := &BufferEntry{
		FileName: "x.md",
		Body:     "minimal body",
		// no Tags, no Surprise
	}
	msg := buildValueJudgeMessage(entry)

	// Should not include Tags or Surprise sections when fields are empty.
	if strings.Contains(msg, "Tags:") {
		t.Error("message should omit Tags section when entry has no tags")
	}
	if strings.Contains(msg, "Self-assessed surprise") {
		t.Error("message should omit surprise section when entry has zero surprise")
	}
	// But still includes core context.
	if !strings.Contains(msg, "minimal body") {
		t.Error("message missing entry body")
	}
}

func TestJudgeDaydreamValue_NilEntryErrors(t *testing.T) {
	_, _, err := JudgeDaydreamValue(nil, true, 2)
	if err == nil {
		t.Error("expected error for nil entry")
	}
}

func TestDaydreamValueSystemPrompt_DefinesAllThreeVerdicts(t *testing.T) {
	for _, want := range []string{"valuable", "marginal", "low-value"} {
		if !strings.Contains(daydreamValueSystemPrompt, want) {
			t.Errorf("system prompt missing verdict %q definition", want)
		}
	}
}

func TestDaydreamValueSystemPrompt_RequiresJSONOutput(t *testing.T) {
	if !strings.Contains(daydreamValueSystemPrompt, "VALID JSON only") {
		t.Error("system prompt should explicitly require JSON-only output (matches parser)")
	}
}
