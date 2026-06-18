package main

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

// transcriptTurnPair is one user prompt and the assistant reply that followed.
type transcriptTurnPair struct {
	Model         string
	UserText      string
	AssistantText string
}

func parseTranscriptTurnPairs(transcriptPath string) ([]transcriptTurnPair, error) {
	switch {
	case transcriptLooksLikeGrokUpdates(transcriptPath):
		return parseGrokUpdatesTurnPairs(transcriptPath)
	default:
		return parseJSONLTranscriptTurnPairs(transcriptPath)
	}
}

func parseGrokUpdatesTurnPairs(transcriptPath string) ([]transcriptTurnPair, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var turns []transcriptTurnPair
	var current transcriptTurnPair
	var assistantChunks []string
	inTurn := false

	flush := func() {
		if !inTurn {
			return
		}
		current.AssistantText = strings.Join(assistantChunks, "")
		if current.UserText != "" || current.AssistantText != "" {
			turns = append(turns, current)
		}
		current = transcriptTurnPair{}
		assistantChunks = nil
		inTurn = false
	}

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
			flush()
			inTurn = true
			if content, _ := update["content"].(map[string]any); content != nil {
				if text, ok := content["text"].(string); ok {
					current.UserText = text
				}
			}
			if meta, _ := update["_meta"].(map[string]any); meta != nil {
				if id, ok := meta["modelId"].(string); ok {
					current.Model = id
				}
			}
			if meta, _ := params["_meta"].(map[string]any); meta != nil {
				if id, ok := meta["modelId"].(string); ok && current.Model == "" {
					current.Model = id
				}
			}
		case "agent_message_chunk":
			if !inTurn {
				continue
			}
			if content, _ := update["content"].(map[string]any); content != nil {
				if text, ok := content["text"].(string); ok && text != "" {
					assistantChunks = append(assistantChunks, text)
				}
			}
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return turns, nil
}

func parseJSONLTranscriptTurnPairs(transcriptPath string) ([]transcriptTurnPair, error) {
	file, err := os.Open(transcriptPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var turns []transcriptTurnPair
	var current transcriptTurnPair
	var assistantParts []string
	inTurn := false

	flush := func() {
		if !inTurn {
			return
		}
		current.AssistantText = strings.Join(assistantParts, "\n\n")
		if current.UserText != "" || current.AssistantText != "" {
			turns = append(turns, current)
		}
		current = transcriptTurnPair{}
		assistantParts = nil
		inTurn = false
	}

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

		if t, ok := event["type"].(string); ok && t == "user" {
			flush()
			inTurn = true
			if text := readContentField(event["content"]); text != "" {
				current.UserText = text
			}
			if m := modelFromTranscriptEvent(event); m != "unknown" {
				current.Model = m
			}
			continue
		}
		if msg, ok := event["message"].(map[string]any); ok {
			if role, ok := msg["role"].(string); ok && role == "user" {
				flush()
				inTurn = true
				if text := readContentField(msg["content"]); text != "" {
					current.UserText = text
				}
				if m := modelFromTranscriptEvent(event); m != "unknown" {
					current.Model = m
				}
				continue
			}
		}

		if isAssistantEvent(event) {
			if !inTurn {
				inTurn = true
			}
			if text := extractAssistantText(event); text != "" {
				assistantParts = append(assistantParts, text)
			}
			if current.Model == "" {
				if m := modelFromTranscriptEvent(event); m != "unknown" {
					current.Model = m
				}
			}
		}
	}
	flush()
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return turns, nil
}