package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
)

// cmdRuleJudge is the async judge subprocess invoked by the Stop hook
// after a pattern match. Reads judgePayload from stdin, calls the
// Anthropic API for a verdict, and appends a "judge" stage record to
// Metrics/rule_firings.jsonl. All failure paths log a terminal record
// rather than crashing — the firing must always be accounted for.
func cmdRuleJudge(_ string, args []string) {
	var payloadFile string
	for i := 0; i < len(args); i++ {
		if args[i] == "--payload-file" && i+1 < len(args) {
			payloadFile = args[i+1]
			i++
		}
	}

	var data []byte
	var err error
	if payloadFile != "" {
		data, err = os.ReadFile(payloadFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[jm rule-judge] read payload file: %v\n", err)
			os.Exit(0)
		}
		defer os.Remove(payloadFile)
	} else {
		data, err = io.ReadAll(os.Stdin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[jm rule-judge] read stdin: %v\n", err)
			os.Exit(0)
		}
	}

	var payload judgePayload
	if err := json.Unmarshal(data, &payload); err != nil {
		fmt.Fprintf(os.Stderr, "[jm rule-judge] parse payload: %v\n", err)
		os.Exit(0)
	}

	verdict, reason, judgeErr := callJudge(payload)

	record := RuleFiring{
		FiringID:    payload.Firing.FiringID,
		Timestamp:   time.Now().UTC(),
		SessionID:   payload.Firing.SessionID,
		RuleID:      payload.Firing.RuleID,
		Stage:       "judge",
		Verdict:     verdict,
		JudgeReason: reason,
	}
	if judgeErr != nil {
		record.JudgeError = judgeErr.Error()
	}
	if err := appendFiring(payload.VaultRoot, record); err != nil {
		fmt.Fprintf(os.Stderr, "[jm rule-judge] append log: %v\n", err)
	}
}

// judgeModel pins the cheap-and-fast model. Worth revisiting if verdict
// quality turns out to be the bottleneck rather than throughput.
const judgeModel = "claude-haiku-4-5-20251001"

const judgeSystemPrompt = `You evaluate whether a behavioral rule fired in an assistant's response.

You will be given:
- A rule the assistant is supposed to follow
- The pattern signals (phrases) that matched in the assistant's response
- An excerpt of the assistant's response

Your job: decide whether the assistant's behavior in the excerpt actually instantiates the rule, or whether the pattern matched coincidentally.

Respond with VALID JSON only, no prose around it:
{"verdict": "confirmed" | "rejected" | "uncertain", "reason": "<one short sentence>"}

- confirmed: the assistant's behavior clearly follows the rule
- rejected: the pattern matched but the behavior is unrelated to the rule
- uncertain: the excerpt is too sparse or ambiguous to decide`

type anthropicReq struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens"`
	System    string             `json:"system"`
	Messages  []anthropicMessage `json:"messages"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResp struct {
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

type judgeVerdict struct {
	Verdict string `json:"verdict"`
	Reason  string `json:"reason"`
}

func callJudge(payload judgePayload) (verdict, reason string, err error) {
	userContent := buildJudgeUserMessage(payload)
	cfg := LoadConfig(payload.VaultRoot)
	rawText, _, err := callHaikuJudge(judgeSystemPrompt, userContent, 200, cfg.JudgeCLIFallbackEnabled, cfg.JudgeCLIMaxConcurrent)
	if err != nil {
		return "error", "", err
	}
	verdict, reason = parseJudgeVerdict(rawText)
	return verdict, reason, nil
}

func buildJudgeUserMessage(p judgePayload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Rule: %s\n\n", p.Rule.RuleText)
	fmt.Fprintf(&b, "Pattern signals matched: %s\n", strings.Join(p.Firing.FireSignalsMatched, ", "))
	if len(p.Firing.ContextSignalsMatched) > 0 {
		fmt.Fprintf(&b, "Context signals matched: %s\n", strings.Join(p.Firing.ContextSignalsMatched, ", "))
	}
	b.WriteString("\nAssistant response excerpt:\n---\n")
	b.WriteString(p.Firing.Excerpt)
	b.WriteString("\n---\n")
	return b.String()
}

// parseJudgeVerdict extracts the JSON verdict object from the model's reply.
// Tolerates fenced code blocks or trailing prose by finding the first JSON
// object in the text. Falls back to "uncertain" if no parseable verdict.
var jsonObjectRegex = regexp.MustCompile(`(?s)\{[^{}]*\}`)

func parseJudgeVerdict(text string) (verdict, reason string) {
	candidates := jsonObjectRegex.FindAllString(text, -1)
	for _, c := range candidates {
		var v judgeVerdict
		if err := json.Unmarshal([]byte(c), &v); err != nil {
			continue
		}
		switch v.Verdict {
		case "confirmed", "rejected", "uncertain":
			return v.Verdict, v.Reason
		}
	}
	return "uncertain", "judge response did not parse: " + truncate(text, 200)
}

