package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	grokTurnCompletedRE = regexp.MustCompile(`(?i)turn completed in\s+([\d.]+)\s*(s|sec|secs|seconds|ms|m)`)
	memoryPathBenchRE   = regexp.MustCompile(`(?i)(?:\[\[(Memory/[A-Za-z0-9_./-]+)\]\]|(Memory/[A-Za-z0-9_./-]+))`)
)

func parseBenchmarkTranscript(transcriptPath string) (*TranscriptMetrics, error) {
	base := strings.ToLower(filepath.Base(transcriptPath))
	switch base {
	case "updates.jsonl":
		return parseGrokUpdatesTranscript(transcriptPath)
	case "chat_history.jsonl":
		return parseGrokChatHistoryTranscript(transcriptPath)
	default:
		return parsePlainTranscript(transcriptPath)
	}
}

func parsePlainTranscript(path string) (*TranscriptMetrics, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	text := string(data)
	m := &TranscriptMetrics{
		TranscriptPath: path,
		ParsedAt:       time.Now().UTC(),
		ToolTotals:     map[string]int{},
	}

	var total float64
	for _, match := range grokTurnCompletedRE.FindAllStringSubmatch(text, -1) {
		d, unit := match[1], strings.ToLower(match[2])
		var secs float64
		fmt.Sscanf(d, "%f", &secs)
		if strings.HasPrefix(unit, "m") && unit != "m" {
			secs /= 1000
		}
		total += secs
		m.Turns = append(m.Turns, TranscriptTurnMetrics{
			TurnIndex:       len(m.Turns) + 1,
			DurationSeconds: secs,
			DurationSource:  "grok_turn_completed_line",
		})
	}
	m.TurnCount = len(m.Turns)
	m.TotalDurationSecs = total
	m.ToolTotals = countToolMentions(text)
	for i := range m.Turns {
		m.Turns[i].WebSearchCount = m.ToolTotals["WebSearch"]
		m.Turns[i].MemoryCiteCount = len(memoryPathBenchRE.FindAllString(text, -1))
	}
	return m, nil
}

func parseGrokUpdatesTranscript(path string) (*TranscriptMetrics, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	m := &TranscriptMetrics{
		Host:           "grok",
		TranscriptPath: path,
		ParsedAt:       time.Now().UTC(),
		ToolTotals:     map[string]int{},
	}

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	turnIndex := 0
	var turnTools []string
	var turnText strings.Builder
	flushTurn := func(dur float64, durSource string) {
		if turnText.Len() == 0 && len(turnTools) == 0 && dur == 0 {
			return
		}
		turnIndex++
		text := turnText.String()
		ws := strings.Count(strings.ToLower(text), "websearch")
		cites := len(memoryPathBenchRE.FindAllString(text, -1))
		m.Turns = append(m.Turns, TranscriptTurnMetrics{
			TurnIndex:       turnIndex,
			DurationSeconds: dur,
			DurationSource:  durSource,
			ToolCalls:       append([]string(nil), turnTools...),
			WebSearchCount:  ws,
			MemoryCiteCount: cites,
		})
		if dur > 0 {
			m.TotalDurationSecs += dur
		}
		for _, t := range turnTools {
			m.ToolTotals[t]++
		}
		turnTools = nil
		turnText.Reset()
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
		chunkType, _ := params["chunk_type"].(string)
		switch chunkType {
		case "user_message_chunk":
			flushTurn(0, "")
		case "agent_message_chunk":
			if t, ok := params["text"].(string); ok {
				turnText.WriteString(t)
				turnText.WriteByte('\n')
				if match := grokTurnCompletedRE.FindStringSubmatch(t); len(match) > 1 {
					var secs float64
					fmt.Sscanf(match[1], "%f", &secs)
					unit := strings.ToLower(match[2])
					if strings.HasPrefix(unit, "m") && unit != "m" {
						secs /= 1000
					}
					flushTurn(secs, "grok_turn_completed_line")
					continue
				}
			}
		case "tool_call":
			if name, ok := params["tool_name"].(string); ok {
				turnTools = append(turnTools, name)
			} else if name, ok := params["name"].(string); ok {
				turnTools = append(turnTools, name)
			}
		}
	}
	flushTurn(0, "")
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	m.TurnCount = len(m.Turns)
	return m, nil
}

func parseGrokChatHistoryTranscript(path string) (*TranscriptMetrics, error) {
	// chat_history.jsonl structure varies; fall back to plain text aggregation.
	m, err := parsePlainTranscript(path)
	if err != nil {
		return nil, err
	}
	m.Host = "grok"
	return m, nil
}

func countToolMentions(text string) map[string]int {
	lower := strings.ToLower(text)
	tools := []string{
		"WebSearch", "WebFetch", "Grep", "Read", "Shell",
		"run_terminal_command", "search_replace", "jm retrieve", "jm associate",
	}
	totals := map[string]int{}
	for _, t := range tools {
		c := strings.Count(lower, strings.ToLower(t))
		if c > 0 {
			totals[t] = c
		}
	}
	return totals
}