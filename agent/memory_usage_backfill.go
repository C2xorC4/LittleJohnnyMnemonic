package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type backfillUsageResult struct {
	Scanned      int
	Eligible     int
	Matched      int
	Written      int
	SkippedExist int
	NoTranscript int
	NoTurnMatch  int
	NoAssistant  int
}

// backfillMemoryUsage replays retrieval_sessions against saved transcripts and
// appends historical rows to memory_usage_log.jsonl.
func backfillMemoryUsage(vaultRoot string, opts backfillUsageOpts) (backfillUsageResult, error) {
	cfg := LoadConfig(vaultRoot)
	logPath := filepath.Join(vaultRoot, cfg.MemoryUsageTrackingLogPath)

	existing, err := loadExistingUsageSessionIDs(logPath)
	if err != nil {
		return backfillUsageResult{}, err
	}

	sessions, err := LoadRetrievalSessions(vaultRoot)
	if err != nil {
		return backfillUsageResult{}, err
	}

	hostBySession := loadHeartbeatRuntimeHosts(vaultRoot)
	transcriptCache := map[string][]transcriptTurnPair{}
	transcriptPathCache := map[string]string{}

	var res backfillUsageResult
	var toWrite []memoryUsageLogEntry

	for _, rs := range sessions {
		res.Scanned++
		if rs.ConversationSessionID == "" || len(rs.Loaded) == 0 {
			continue
		}
		if !ShouldLogHookRetrievalSession(rs.ConversationSessionID, rs.QueryContext) {
			continue
		}
		if isDaydreamAgentPrompt(rs.QueryContext) {
			continue
		}
		if !opts.since.IsZero() && rs.Timestamp.Before(opts.since) {
			continue
		}
		res.Eligible++

		if existing[rs.SessionID] {
			res.SkippedExist++
			continue
		}

		transcriptPath, ok := transcriptPathCache[rs.ConversationSessionID]
		if !ok {
			transcriptPath = findTranscriptForConversation(rs.ConversationSessionID, opts)
			transcriptPathCache[rs.ConversationSessionID] = transcriptPath
		}
		if transcriptPath == "" {
			res.NoTranscript++
			continue
		}

		turns, ok := transcriptCache[rs.ConversationSessionID]
		if !ok {
			turns, err = parseTranscriptTurnPairs(transcriptPath)
			if err != nil {
				return res, fmt.Errorf("parse transcript %s: %w", transcriptPath, err)
			}
			transcriptCache[rs.ConversationSessionID] = turns
		}

		turn, ok := matchRetrievalSessionToTurn(rs, turns)
		if !ok {
			res.NoTurnMatch++
			continue
		}
		if strings.TrimSpace(turn.AssistantText) == "" {
			res.NoAssistant++
			continue
		}
		res.Matched++

		entry := buildBackfillUsageEntry(rs, turn, hostBySession[rs.ConversationSessionID], cfg.MemoryUsageTrackingVerbosity)
		toWrite = append(toWrite, entry)
	}

	if opts.dryRun {
		res.Written = len(toWrite)
		return res, nil
	}

	for _, entry := range toWrite {
		appendMemoryUsageLog(logPath, entry)
		res.Written++
	}
	return res, nil
}

type backfillUsageOpts struct {
	dryRun      bool
	since       time.Time
	grokHome    string
	claudeHome  string
}

func buildBackfillUsageEntry(rs RetrievalSession, turn transcriptTurnPair, runtimeHost, verbosity string) memoryUsageLogEntry {
	referencedKeys := extractReferencedMemoryKeys(turn.AssistantText, rs.Loaded)
	injected := len(rs.Loaded)
	referenced := len(referencedKeys)

	rate := 0.0
	if injected > 0 {
		rate = float64(referenced) / float64(injected)
	}

	model := normalizeModelID(turn.Model)
	if model == "unknown" {
		model = "unknown"
	}
	if runtimeHost == "" {
		if strings.HasPrefix(rs.ConversationSessionID, "019") {
			runtimeHost = HostGrokBuild
		} else {
			runtimeHost = HostClaudeCode
		}
	}

	entry := memoryUsageLogEntry{
		Timestamp:          rs.Timestamp.UTC().Format(time.RFC3339),
		SessionID:          rs.ConversationSessionID,
		RetrievalSessionID: rs.SessionID,
		Model:              model,
		RuntimeHost:        runtimeHost,
		MemoriesInjected:   injected,
		MemoriesReferenced: referenced,
		ReferenceRate:      rate,
		Outcome:            classifyMemoryUsageOutcome(injected, referenced),
		Backfill:           true,
	}
	if verbosity == "verbose" {
		entry.SlugsInjected = slugsFromMemoryKeys(rs.Loaded)
		entry.SlugsReferenced = slugsFromMemoryKeys(referencedKeys)
	}
	return entry
}

func loadExistingUsageSessionIDs(logPath string) (map[string]bool, error) {
	seen := map[string]bool{}
	data, err := os.ReadFile(logPath)
	if os.IsNotExist(err) {
		return seen, nil
	}
	if err != nil {
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var e memoryUsageLogEntry
		if json.Unmarshal([]byte(line), &e) != nil {
			continue
		}
		if e.RetrievalSessionID != "" {
			seen[e.RetrievalSessionID] = true
		}
	}
	return seen, nil
}

func loadHeartbeatRuntimeHosts(vaultRoot string) map[string]string {
	path := filepath.Join(vaultRoot, "Metrics", "session_heartbeat.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec SessionHeartbeatRecord
		if json.Unmarshal([]byte(line), &rec) != nil || rec.SessionID == "" {
			continue
		}
		if rec.RuntimeHost != "" {
			out[rec.SessionID] = rec.RuntimeHost
		}
	}
	return out
}

func findTranscriptForConversation(conversationSessionID string, opts backfillUsageOpts) string {
	if conversationSessionID == "" {
		return ""
	}

	grokHome := opts.grokHome
	if grokHome == "" {
		grokHome = os.Getenv("GROK_HOME")
		if grokHome == "" {
			if home, err := os.UserHomeDir(); err == nil {
				grokHome = filepath.Join(home, ".grok")
			}
		}
	}
	if grokHome != "" {
		sessionsRoot := filepath.Join(grokHome, "sessions")
		for _, name := range []string{"updates.jsonl", "chat_history.jsonl"} {
			matches, _ := filepath.Glob(filepath.Join(sessionsRoot, "*", conversationSessionID, name))
			for _, p := range matches {
				if _, err := os.Stat(p); err == nil {
					return p
				}
			}
		}
	}

	claudeHome := opts.claudeHome
	if claudeHome == "" {
		if home, err := os.UserHomeDir(); err == nil {
			claudeHome = filepath.Join(home, ".claude", "projects")
		}
	}
	if claudeHome != "" {
		matches, _ := filepath.Glob(filepath.Join(claudeHome, "*", conversationSessionID+".jsonl"))
		for _, p := range matches {
			if _, err := os.Stat(p); err == nil {
				return p
			}
		}
	}
	return ""
}

func matchRetrievalSessionToTurn(rs RetrievalSession, turns []transcriptTurnPair) (transcriptTurnPair, bool) {
	query := normalizePromptForMatch(rs.QueryContext)
	if query == "" {
		return transcriptTurnPair{}, false
	}

	bestIdx := -1
	bestScore := 0
	for i, turn := range turns {
		user := normalizePromptForMatch(turn.UserText)
		if user == "" {
			continue
		}
		score := promptMatchScore(query, user)
		if score > bestScore {
			bestScore = score
			bestIdx = i
		}
	}
	if bestIdx < 0 || bestScore < 40 {
		return transcriptTurnPair{}, false
	}
	return turns[bestIdx], true
}

func normalizePromptForMatch(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "<user_query>")
	s = strings.TrimSuffix(s, "</user_query>")
	s = strings.TrimSpace(s)
	s = strings.ToLower(s)
	s = strings.Join(strings.Fields(s), " ")
	return s
}

func promptMatchScore(query, user string) int {
	if query == "" || user == "" {
		return 0
	}
	if query == user {
		return 100
	}
	if strings.Contains(user, query) {
		return 90
	}
	if strings.Contains(query, user) && len(user) > 40 {
		return 85
	}
	// Prefix overlap on truncated retrieval query_context.
	n := minInt(120, len(query))
	if n > 20 && strings.Contains(user, query[:n]) {
		return 75
	}
	// Token overlap ratio.
	qTokens := strings.Fields(query)
	if len(qTokens) == 0 {
		return 0
	}
	overlap := 0
	for _, tok := range qTokens {
		if len(tok) < 4 {
			continue
		}
		if strings.Contains(user, tok) {
			overlap++
		}
	}
	return overlap * 100 / len(qTokens)
}

func isDaydreamAgentPrompt(prompt string) bool {
	p := strings.TrimSpace(prompt)
	return strings.HasPrefix(p, "# Memory Daydream Agent") ||
		strings.Contains(p, "Memory Daydream Agent\n")
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// backfillZeroRecallFromHeartbeat appends zero_recall rows to recall_log for
// user-prompt-submit heartbeats where memories_retrieved == 0.
func backfillZeroRecallFromHeartbeat(vaultRoot string, dryRun bool, since time.Time) (int, int, error) {
	cfg := LoadConfig(vaultRoot)
	if !cfg.RecallTrackingEnabled {
		return 0, 0, nil
	}

	hbPath := filepath.Join(vaultRoot, "Metrics", "session_heartbeat.jsonl")
	hbData, err := os.ReadFile(hbPath)
	if os.IsNotExist(err) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, err
	}

	existing := map[string]bool{}
	logPath := filepath.Join(vaultRoot, cfg.RecallTrackingLogPath)
	if data, err := os.ReadFile(logPath); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
			if line == "" {
				continue
			}
			var e recallLogEntry
			if json.Unmarshal([]byte(line), &e) != nil || !e.ZeroRecall {
				continue
			}
			key := e.SessionID + "|" + e.Timestamp
			existing[key] = true
		}
	}

	var candidates []recallLogEntry
	for _, line := range strings.Split(strings.TrimSpace(string(hbData)), "\n") {
		if line == "" {
			continue
		}
		var rec SessionHeartbeatRecord
		if json.Unmarshal([]byte(line), &rec) != nil {
			continue
		}
		if rec.Event != "" && rec.Event != "user-prompt-submit" {
			continue
		}
		if rec.MemoriesRetrieved != 0 {
			continue
		}
		if rec.PromptChars == 0 {
			continue
		}
		ts, err := time.Parse(time.RFC3339, rec.Timestamp)
		if err != nil {
			ts, err = time.Parse("2006-01-02T15:04:05-07:00", rec.Timestamp)
			if err != nil {
				continue
			}
		}
		if !since.IsZero() && ts.Before(since) {
			continue
		}
		entry := recallLogEntry{
			Timestamp:  ts.UTC().Format(time.RFC3339),
			SessionID:  rec.SessionID,
			PromptLen:  rec.PromptChars,
			Total:      0,
			Counts:     map[string]int{},
			ZeroRecall: true,
		}
		key := entry.SessionID + "|" + entry.Timestamp
		if existing[key] {
			continue
		}
		candidates = append(candidates, entry)
	}

	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].Timestamp < candidates[j].Timestamp
	})

	if dryRun {
		return len(candidates), 0, nil
	}
	for _, entry := range candidates {
		appendRecallLog(logPath, entry)
	}
	return len(candidates), len(candidates), nil
}