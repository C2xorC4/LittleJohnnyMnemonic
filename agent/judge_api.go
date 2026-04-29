package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"
)

// Judge call layer — shared by cmd_rule_judge.go (behavioral rule judge)
// and consolidation_judge.go (daydream-redundancy judge). Provides a tiered
// fallback for how the API call is made:
//
//   1. ANTHROPIC_API_KEY env var present → direct Anthropic API call
//   2. `claude` CLI available on PATH   → CLI invocation using Claude Code's auth
//   3. Neither                          → return an error; caller falls back to heuristics
//
// This lets machines that don't export ANTHROPIC_API_KEY (e.g., ones that only
// use Claude Code interactively) still benefit from LLM judgment during
// consolidation and behavioral rule evaluation. Machines with the env var set
// skip the CLI shell-out and go direct (faster).

// judgeTransport is how the judge call succeeded — used for logging/metrics.
type judgeTransport string

const (
	judgeTransportAPI      judgeTransport = "api"
	judgeTransportCLI      judgeTransport = "cli"
	judgeTransportDisabled judgeTransport = "disabled"
)

// callHaikuJudge runs a single-shot Haiku-class completion for judge workflows.
// Returns the model's raw text response and which transport produced it.
// On both transports failing, returns an error; the caller is expected to
// fall back to a heuristic path.
func callHaikuJudge(systemPrompt, userMessage string, maxTokens int) (rawText string, via judgeTransport, err error) {
	// Tier 1: direct API call via ANTHROPIC_API_KEY
	if apiKey := os.Getenv("ANTHROPIC_API_KEY"); apiKey != "" {
		text, err := callHaikuViaAPI(apiKey, systemPrompt, userMessage, maxTokens)
		if err == nil {
			return text, judgeTransportAPI, nil
		}
		// Fall through to CLI attempt on API failure — don't fail hard if
		// there's a key but the key is invalid / rate-limited / network is
		// down, because the CLI path may still work.
		fmt.Fprintf(os.Stderr, "[jm judge] API tier failed, trying CLI: %v\n", err)
	}

	// Tier 2: shell out to `claude` CLI (uses Claude Code's stored auth)
	text, err := callHaikuViaCLI(systemPrompt, userMessage, maxTokens)
	if err == nil {
		return text, judgeTransportCLI, nil
	}

	return "", judgeTransportDisabled, fmt.Errorf("all transports failed: %w", err)
}

// callHaikuViaAPI performs the direct Anthropic API call. Same behavior the
// rule-judge has had from the start; now extracted for reuse.
func callHaikuViaAPI(apiKey, systemPrompt, userMessage string, maxTokens int) (string, error) {
	body, err := json.Marshal(anthropicReq{
		Model:     judgeModel,
		MaxTokens: maxTokens,
		System:    systemPrompt,
		Messages: []anthropicMessage{
			{Role: "user", Content: userMessage},
		},
	})
	if err != nil {
		return "", fmt.Errorf("marshal request: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.anthropic.com/v1/messages", bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("content-type", "application/json")
	req.Header.Set("x-api-key", apiKey)
	req.Header.Set("anthropic-version", "2023-06-01")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("api call: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read response: %w", err)
	}

	var apiResp anthropicResp
	if err := json.Unmarshal(respBody, &apiResp); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if apiResp.Error != nil {
		return "", fmt.Errorf("api error: %s: %s", apiResp.Error.Type, apiResp.Error.Message)
	}
	if len(apiResp.Content) == 0 {
		return "", fmt.Errorf("empty response content")
	}

	var text strings.Builder
	for _, c := range apiResp.Content {
		if c.Type == "text" {
			text.WriteString(c.Text)
		}
	}
	return text.String(), nil
}

// callHaikuViaCLI invokes `claude -p` as a subprocess, bundling the system
// prompt and user message into a single prompt argument. Uses Claude Code's
// stored credentials — no ANTHROPIC_API_KEY required.
//
// The CLI is located via PATH lookup. On Windows this finds claude.cmd /
// claude.exe; on Unix it finds the claude script. If the CLI isn't
// available, returns an error immediately so the caller can fall through
// to heuristics.
func callHaikuViaCLI(systemPrompt, userMessage string, maxTokens int) (string, error) {
	claudeBin, err := exec.LookPath("claude")
	if err != nil {
		return "", fmt.Errorf("claude CLI not available: %w", err)
	}

	// Bundle the system prompt into the user content with explicit framing.
	// -p (print mode) is a single-shot prompt without a separate system slot.
	// The framing reproduces the system/user distinction well enough for
	// structured-verdict workflows.
	combined := systemPrompt + "\n\n---\n\n" + userMessage

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// --model pins to Haiku-class for cost/speed parity with the API path.
	// -p is non-interactive print mode.
	cmd := exec.CommandContext(ctx, claudeBin, "-p", "--model", judgeModel, combined)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		stderrStr := strings.TrimSpace(stderr.String())
		if stderrStr != "" {
			return "", fmt.Errorf("claude CLI failed: %w (stderr: %s)", err, truncateJudge(stderrStr, 200))
		}
		return "", fmt.Errorf("claude CLI failed: %w", err)
	}

	text := strings.TrimSpace(stdout.String())
	if text == "" {
		return "", fmt.Errorf("claude CLI returned empty output")
	}
	return text, nil
}
