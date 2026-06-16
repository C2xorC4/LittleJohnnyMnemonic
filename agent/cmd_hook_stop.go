package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// runStop fires after each assistant turn. Cheap synchronous work only:
// load rules, scan the last assistant turn for fire_signals, log a "pattern"
// stage record per match, and spawn a detached judge subprocess for each.
// The judge does the LLM call asynchronously and appends a separate
// "judge" stage record. Hook returns in well under 100ms.
func runStop(vaultRoot string, input *hookInput) {
	// Backup and consolidation triggers — independent of behavioral rules.
	// Defer so they run regardless of which early-return branch the rule
	// scanner takes. Both are cooldown-gated and fail-soft.
	defer func() {
		cfg := LoadConfig(vaultRoot)
		_, _ = MaybeRunBackup(cfg, vaultRoot, "stop")
		spawnConsolidationIfNeeded(vaultRoot, cfg)
	}()

	// Adaptive-edge loop closure: harvest Memory/ citations from the last
	// assistant turn against the preceding prompt-submit retrieval session.
	harvestCitationsFromStop(vaultRoot, input)

	// Try volley fulfillment at Stop; release is deferred to UserPromptSubmit
	// on Grok where the transcript is not flushed yet (see citation harvest).
	if err := tryFulfillVolleyCommitmentOnStop(vaultRoot, input.SessionID, time.Now(), input); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: volley commitment: %v\n", err)
	}

	rules, err := loadBehavioralRules(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: load rules: %v\n", err)
		return
	}
	if len(rules) == 0 {
		return
	}

	if input.TranscriptPath == "" {
		return
	}

	turn, err := lastAssistantTurn(input.TranscriptPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: read transcript: %v\n", err)
		return
	}
	if turn == "" {
		return
	}

	// If no judge transport can run (no API key and CLI fallback disabled),
	// skip spawning judge subprocesses — they would only cold-start and log an
	// error verdict. Pattern firings are still recorded below for accounting.
	judgeOK := judgeTransportAvailable(LoadConfig(vaultRoot))

	for _, rule := range rules {
		fireHits := scanForFireSignals(turn, rule)
		if len(fireHits) == 0 {
			continue
		}
		ctxHits := scanForContextSignals(turn, rule)
		excerpt := excerptText(turn, 1500)
		firingID := newFiringID(rule.ID)

		patternRecord := RuleFiring{
			FiringID:              firingID,
			Timestamp:             time.Now().UTC(),
			SessionID:             input.SessionID,
			RuleID:                rule.ID,
			Stage:                 "pattern",
			FireSignalsMatched:    fireHits,
			ContextSignalsMatched: ctxHits,
			Excerpt:               excerpt,
		}
		if err := appendFiring(vaultRoot, patternRecord); err != nil {
			fmt.Fprintf(os.Stderr, "[jm hook] stop: log pattern: %v\n", err)
			continue
		}

		if judgeOK {
			spawnJudge(vaultRoot, rule, patternRecord)
		}
	}
}

// lastAssistantTurn returns the final assistant text from a transcript JSONL
// file. Supports Claude Code transcripts, Grok chat_history.jsonl, and Grok
// updates.jsonl (agent_message_chunk aggregation for the last user turn).
func lastAssistantTurn(transcriptPath string) (string, error) {
	switch {
	case transcriptLooksLikeGrokUpdates(transcriptPath):
		return lastAssistantTurnFromGrokUpdates(transcriptPath)
	default:
		return lastAssistantTurnFromJSONL(transcriptPath)
	}
}

func lastAssistantTurnFromJSONL(transcriptPath string) (string, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var lastText string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if isSkippedTranscriptEvent(event) {
			continue
		}
		if !isAssistantEvent(event) {
			continue
		}
		if text := extractAssistantText(event); text != "" {
			lastText = text
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return lastText, nil
}

// lastAssistantTurnFromGrokUpdates aggregates agent_message_chunk text after
// the final user_message_chunk in a Grok updates.jsonl stream.
func lastAssistantTurnFromGrokUpdates(transcriptPath string) (string, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var chunks []string
	sawUser := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var root map[string]any
		if err := json.Unmarshal(line, &root); err != nil {
			continue
		}
		params, _ := root["params"].(map[string]any)
		if params == nil {
			continue
		}
		update, _ := params["update"].(map[string]any)
		if update == nil {
			continue
		}
		kind, _ := update["sessionUpdate"].(string)
		switch kind {
		case "user_message_chunk":
			sawUser = true
			chunks = nil
		case "agent_message_chunk":
			if !sawUser {
				continue
			}
			content, _ := update["content"].(map[string]any)
			if content == nil {
				continue
			}
			if text, ok := content["text"].(string); ok && text != "" {
				chunks = append(chunks, text)
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(chunks, ""), nil
}

func isSkippedTranscriptEvent(event map[string]any) bool {
	if t, ok := event["type"].(string); ok {
		switch t {
		case "user", "tool_result", "reasoning":
			return true
		}
	}
	return false
}

func isAssistantEvent(event map[string]any) bool {
	if t, ok := event["type"].(string); ok && t == "assistant" {
		return true
	}
	if msg, ok := event["message"].(map[string]any); ok {
		if role, ok := msg["role"].(string); ok && role == "assistant" {
			return true
		}
	}
	return false
}

// extractAssistantText handles the common message shapes:
//   - {message: {content: [{type:"text", text:"..."}]}}
//   - {message: {content: "..."}}
//   - {content: [...]} or {content: "..."} at top level
//   - {text: "..."}
func extractAssistantText(event map[string]any) string {
	if msg, ok := event["message"].(map[string]any); ok {
		if t := readContentField(msg["content"]); t != "" {
			return t
		}
		if t, ok := msg["text"].(string); ok {
			return t
		}
	}
	if t := readContentField(event["content"]); t != "" {
		return t
	}
	if t, ok := event["text"].(string); ok {
		return t
	}
	return ""
}

func readContentField(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, item := range c {
			block, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if t, ok := block["type"].(string); !ok || t != "text" {
				continue
			}
			if txt, ok := block["text"].(string); ok && txt != "" {
				parts = append(parts, txt)
			}
		}
		return strings.Join(parts, "\n\n")
	}
	return ""
}

// spawnJudge launches a detached `jm rule-judge` subprocess. Payload is
// written to a temp file and the path passed as a flag — using stdin
// would race against parent exit (Go's stdin-copy goroutine dies with
// the parent before the child finishes reading). The child deletes the
// payload file once consumed.
func spawnJudge(vaultRoot string, rule BehavioralRule, firing RuleFiring) {
	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: locate executable: %v\n", err)
		return
	}

	payload := judgePayload{
		VaultRoot: vaultRoot,
		Firing:    firing,
		Rule:      rule,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: marshal judge payload: %v\n", err)
		return
	}

	payloadFile, err := os.CreateTemp("", "jm-judge-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: create payload file: %v\n", err)
		return
	}
	if _, err := payloadFile.Write(data); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: write payload file: %v\n", err)
		payloadFile.Close()
		os.Remove(payloadFile.Name())
		return
	}
	payloadFile.Close()

	cmd := exec.Command(exe, "rule-judge", "--payload-file", payloadFile.Name())
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err == nil {
		cmd.Stdout = devnull
		cmd.Stderr = devnull
	}
	detachSysProcAttr(cmd)

	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] stop: spawn judge: %v\n", err)
		os.Remove(payloadFile.Name())
		errRecord := RuleFiring{
			FiringID:   firing.FiringID,
			Timestamp:  time.Now().UTC(),
			SessionID:  firing.SessionID,
			RuleID:     firing.RuleID,
			Stage:      "judge",
			Verdict:    "error",
			JudgeError: fmt.Sprintf("spawn failed: %v", err),
		}
		_ = appendFiring(vaultRoot, errRecord)
		return
	}
	_ = cmd.Process.Release()
}

// judgePayload is what the judge subprocess reads from stdin.
type judgePayload struct {
	VaultRoot string         `json:"vault_root"`
	Firing    RuleFiring     `json:"firing"`
	Rule      BehavioralRule `json:"rule"`
}

// suppress unused-import warning for filepath in environments where it
// might otherwise be elided by formatters.
var _ = filepath.Join
