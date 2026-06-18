package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// DetectModel resolves the active LLM model ID for telemetry. Priority:
// hook payload model field, host-specific env vars, transcript metadata.
func DetectModel(input *hookInput, transcriptPath string) string {
	if input != nil {
		if m := normalizeModelID(input.Model); m != "unknown" {
			return m
		}
	}

	for _, key := range []string{
		"ANTHROPIC_MODEL",
		"CLAUDE_MODEL",
		"GROK_MODEL",
		"CURSOR_MODEL",
		"XAI_MODEL",
	} {
		if m := normalizeModelID(os.Getenv(key)); m != "unknown" {
			return m
		}
	}

	if transcriptPath != "" {
		if m := modelFromTranscript(transcriptPath); m != "unknown" {
			return m
		}
	}
	return "unknown"
}

func normalizeModelID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "unknown"
	}
	return raw
}

func modelFromTranscript(transcriptPath string) string {
	switch {
	case transcriptLooksLikeGrokUpdates(transcriptPath):
		return modelFromGrokUpdatesTranscript(transcriptPath)
	default:
		return modelFromJSONLTranscript(transcriptPath)
	}
}

// modelFromGrokUpdatesTranscript reads modelId from the final user_message_chunk
// _meta in a Grok updates.jsonl stream (the turn currently being scored).
func modelFromGrokUpdatesTranscript(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "unknown"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var lastModel string
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
		if kind != "user_message_chunk" {
			continue
		}
		if meta, _ := update["_meta"].(map[string]any); meta != nil {
			if id, ok := meta["modelId"].(string); ok && id != "" {
				lastModel = id
			}
		}
		if meta, _ := params["_meta"].(map[string]any); meta != nil {
			if id, ok := meta["modelId"].(string); ok && id != "" {
				lastModel = id
			}
		}
	}
	return normalizeModelID(lastModel)
}

// modelFromJSONLTranscript scans Claude Code-style transcripts for model metadata
// on the last assistant event before EOF.
func modelFromJSONLTranscript(transcriptPath string) string {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "unknown"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var lastModel string
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var event map[string]any
		if err := json.Unmarshal(line, &event); err != nil {
			continue
		}
		if m := modelFromTranscriptEvent(event); m != "unknown" {
			lastModel = m
		}
	}
	return normalizeModelID(lastModel)
}

func modelFromTranscriptEvent(event map[string]any) string {
	for _, key := range []string{"model", "modelId", "model_id"} {
		if v, ok := event[key].(string); ok && v != "" {
			return v
		}
	}
	if msg, ok := event["message"].(map[string]any); ok {
		for _, key := range []string{"model", "modelId", "model_id"} {
			if v, ok := msg[key].(string); ok && v != "" {
				return v
			}
		}
	}
	return "unknown"
}