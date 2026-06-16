package main

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

// resolveTranscriptPath returns the conversation transcript file for citation
// harvest. Claude Code passes transcript_path/transcriptPath on Stop; Grok does
// not, so we locate chat_history.jsonl under ~/.grok/sessions via session ID.
func resolveTranscriptPath(input *hookInput) string {
	if input != nil && input.TranscriptPath != "" {
		return input.TranscriptPath
	}

	sessionID := ""
	if input != nil && input.SessionID != "" {
		sessionID = input.SessionID
	}
	if sessionID == "" {
		sessionID = os.Getenv("GROK_SESSION_ID")
	}
	if sessionID == "" {
		return ""
	}

	grokHome := os.Getenv("GROK_HOME")
	if grokHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			grokHome = filepath.Join(home, ".grok")
		}
	}
	if grokHome == "" {
		return ""
	}

	sessionsRoot := filepath.Join(grokHome, "sessions")
	for _, name := range []string{"chat_history.jsonl", "updates.jsonl"} {
		matches, err := filepath.Glob(filepath.Join(sessionsRoot, "*", sessionID, name))
		if err != nil {
			continue
		}
		for _, p := range matches {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func transcriptLooksLikeGrokUpdates(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "updates.jsonl"
}

func transcriptLooksLikeGrokChatHistory(path string) bool {
	base := strings.ToLower(filepath.Base(path))
	return base == "chat_history.jsonl"
}

// lastTurnTranscriptBlob returns searchable text for the assistant turn after the
// final user message. For Grok updates.jsonl this includes tool_call events
// (spawn_subagent, Task, etc.) — not only agent_message_chunk text.
func lastTurnTranscriptBlob(transcriptPath string) (string, error) {
	switch {
	case transcriptLooksLikeGrokUpdates(transcriptPath):
		return lastTurnBlobFromGrokUpdates(transcriptPath)
	default:
		return lastAssistantTurn(transcriptPath)
	}
}

// lastTurnBlobFromGrokUpdates aggregates agent_message_chunk text and tool_call
// JSON from the turn after the final user_message_chunk in updates.jsonl.
func lastTurnBlobFromGrokUpdates(transcriptPath string) (string, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var parts []string
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
			parts = nil
		case "agent_message_chunk":
			if !sawUser {
				continue
			}
			if content, _ := update["content"].(map[string]any); content != nil {
				if text, ok := content["text"].(string); ok && text != "" {
					parts = append(parts, text)
				}
			}
		case "tool_call", "tool_call_update":
			if !sawUser {
				continue
			}
			parts = append(parts, string(line))
			if title, ok := update["title"].(string); ok && title != "" {
				parts = append(parts, title)
			}
			if raw, ok := update["rawInput"]; ok {
				if b, err := json.Marshal(raw); err == nil {
					parts = append(parts, string(b))
				}
			}
		}
	}
	if err := scanner.Err(); err != nil {
		return "", err
	}
	return strings.Join(parts, "\n"), nil
}