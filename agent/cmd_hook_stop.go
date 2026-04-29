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
	// Backup hook — independent of behavioral rules. Defer so it runs no
	// matter which early-return branch the rule scanner takes (or doesn't
	// take). Cooldown-gated and fail-soft inside MaybeRunBackup.
	defer func() {
		cfg := LoadConfig(vaultRoot)
		_, _ = MaybeRunBackup(cfg, vaultRoot, "stop")
	}()

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

		spawnJudge(vaultRoot, rule, patternRecord)
	}
}

// lastAssistantTurn scans a Claude Code transcript JSONL file from the
// beginning and returns the text of the last assistant message. The format
// is one JSON event per line. Assistant text events vary slightly by
// transcript version, so this parser is intentionally permissive.
func lastAssistantTurn(transcriptPath string) (string, error) {
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
