package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// RetrievalSession records the set of memories loaded together during a
// single `jm retrieve` call. The session_id is referenced later by
// citation events to link a cited memory back to the other memories
// that were in scope at the time, providing the external reward signal
// for adaptive edge weighting.
//
// See System/AssociativeMap.md for the design and the endogeneity
// rationale (citation is the reward signal, not co-occurrence alone).
type RetrievalSession struct {
	SessionID             string    `json:"session_id"`
	Timestamp             time.Time `json:"timestamp"`
	Loaded                []string  `json:"loaded"`
	QueryContext          string    `json:"query_context,omitempty"`
	QueryTags             []string  `json:"query_tags,omitempty"`
	ConversationSessionID string    `json:"conversation_session_id,omitempty"`
	ScoringConfigHash     string    `json:"scoring_config_hash,omitempty"`
}

const (
	retrievalSessionLogPath = "Metrics/retrieval_sessions.jsonl"

	// InternalInvocationEnvVar marks subprocess invocations (consolidation
	// judges via claude -p, etc.) that must not pollute conversational
	// retrieval-session logs or session heartbeats.
	InternalInvocationEnvVar = "LJM_INTERNAL_INVOCATION"

	maxRetrievalSessionQueryContext = 2048

	// pruneLargeFileBytes — below this size, opportunistic prune on append
	// is cheap enough to run every time. Above it, throttle via marker file.
	pruneLargeFileBytes = 512 * 1024

	retrievalSessionPruneMinInterval = 6 * time.Hour
)

// internalEvalPromptMarkers are prefixes of LLM judge system prompts that
// leak into user-prompt-submit hooks when consolidation shells out to
// `claude -p`. These are not conversational retrievals and must not be
// logged as adaptive-edge substrate.
var internalEvalPromptMarkers = []string{
	"You evaluate whether a buffer entry",
	"You evaluate whether a daydream-sourced",
	"You evaluate whether a behavioral rule",
}

// IsInternalInvocation returns true when the current process was spawned
// as an internal LJM subprocess rather than a user-facing agent session.
func IsInternalInvocation() bool {
	return os.Getenv(InternalInvocationEnvVar) == "1"
}

// IsInternalEvalPrompt reports whether prompt text is a consolidation or
// rule-judge evaluation prompt rather than user conversation.
func IsInternalEvalPrompt(prompt string) bool {
	if IsInternalInvocation() {
		return true
	}
	p := strings.TrimSpace(prompt)
	for _, prefix := range internalEvalPromptMarkers {
		if strings.HasPrefix(p, prefix) {
			return true
		}
	}
	return false
}

// ShouldLogHookRetrievalSession gates hook-path session logging. Adaptive
// edge reinforcement requires a host conversation session ID for citation
// harvest correlation; judge subprocesses arrive without one and dominated
// log growth (~99% of entries as of 2026-06-16).
func ShouldLogHookRetrievalSession(conversationSessionID, prompt string) bool {
	if conversationSessionID == "" {
		return false
	}
	return !IsInternalEvalPrompt(prompt)
}

func truncateRetrievalQueryContext(prompt string) string {
	return truncate(prompt, maxRetrievalSessionQueryContext)
}

func retrievalSessionLogFile(vaultRoot string) string {
	return filepath.Join(vaultRoot, retrievalSessionLogPath)
}

// GenerateSessionID returns a UUID v4 string. No external dependency:
// 16 random bytes with version and variant bits set per RFC 4122.
func GenerateSessionID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failure is exceptional; fall back to timestamp-based
		// ID rather than panic — adaptive weighting tolerates collisions
		// gracefully (worst case: two sessions credit each other's loads).
		return fmt.Sprintf("ts-%d", time.Now().UnixNano())
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // variant 10
	return fmt.Sprintf("%s-%s-%s-%s-%s",
		hex.EncodeToString(b[0:4]),
		hex.EncodeToString(b[4:6]),
		hex.EncodeToString(b[6:8]),
		hex.EncodeToString(b[8:10]),
		hex.EncodeToString(b[10:16]),
	)
}

// AppendRetrievalSession persists a session record to the JSONL log.
// Append-only — no in-place updates. Returns nil if the log is disabled
// (caller already gated on cfg.RetrievalSessionLogEnabled before calling).
func AppendRetrievalSession(vaultRoot string, session RetrievalSession) error {
	path := retrievalSessionLogFile(vaultRoot)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
	}

	session.QueryContext = truncateRetrievalQueryContext(session.QueryContext)

	cfg := LoadConfig(vaultRoot)
	if cfg.AutoDaydreamLogRotationThreshold > 0 {
		if err := rotateJSONLIfNeeded(path, cfg.AutoDaydreamLogRotationThreshold, session.Timestamp); err != nil {
			fmt.Fprintf(os.Stderr, "[jm] retrieval session rotation: %v\n", err)
		}
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open session log: %w", err)
	}
	defer f.Close()

	enc, err := json.Marshal(session)
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	if _, err := f.Write(append(enc, '\n')); err != nil {
		return fmt.Errorf("write session: %w", err)
	}
	return nil
}

// LoadRetrievalSessions reads every session record from disk. Returns
// an empty slice if the file does not exist (not an error: the log is
// optional and may simply not have been written yet).
func LoadRetrievalSessions(vaultRoot string) ([]RetrievalSession, error) {
	path := retrievalSessionLogFile(vaultRoot)
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("open session log: %w", err)
	}
	defer f.Close()

	var sessions []RetrievalSession
	scanner := bufio.NewScanner(f)
	// Allow larger lines than the default 64 KiB — a retrieve call can
	// surface up to MaxMemoriesLoaded keys plus context strings.
	scanner.Buffer(make([]byte, 1<<16), 1<<20)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rs RetrievalSession
		if err := json.Unmarshal(line, &rs); err != nil {
			// Skip malformed lines rather than fail the whole load —
			// matches the resilience of other JSONL readers in this
			// codebase (rule_firings, consolidation_outcomes).
			continue
		}
		sessions = append(sessions, rs)
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan session log: %w", err)
	}
	return sessions, nil
}

// FindLatestRetrievalSessionForConversation returns the most recent
// retrieval session tagged with the given host conversation session ID
// (Claude/Grok sessionId from the hook input). Used by the stop hook to
// correlate the assistant turn with the prompt-submit retrieval that
// preceded it.
func FindLatestRetrievalSessionForConversation(vaultRoot, conversationSessionID string) (*RetrievalSession, error) {
	if conversationSessionID == "" {
		return nil, nil
	}
	sessions, err := LoadRetrievalSessions(vaultRoot)
	if err != nil {
		return nil, err
	}
	var latest *RetrievalSession
	for i := range sessions {
		s := &sessions[i]
		if s.ConversationSessionID != conversationSessionID {
			continue
		}
		if latest == nil || s.Timestamp.After(latest.Timestamp) {
			latest = s
		}
	}
	return latest, nil
}

// FindRetrievalSession returns the session with matching ID, or nil if
// not found. Linear scan — fine for the retention windows expected
// (≤ 14 days × ≤ a few dozen retrieves/day = at most a few hundred entries).
func FindRetrievalSession(vaultRoot, sessionID string) (*RetrievalSession, error) {
	sessions, err := LoadRetrievalSessions(vaultRoot)
	if err != nil {
		return nil, err
	}
	for i := range sessions {
		if sessions[i].SessionID == sessionID {
			return &sessions[i], nil
		}
	}
	return nil, nil
}

// retentionKeepSession decides whether a session survives retention pruning.
// Citation-linked sessions are kept even past retention so --cite reinforcement
// can still resolve the loaded set.
func retentionKeepSession(s RetrievalSession, cutoff time.Time, citedSessions map[string]bool) bool {
	if citedSessions[s.SessionID] {
		return true
	}
	if s.Timestamp.After(cutoff) {
		return !IsInternalEvalPrompt(s.QueryContext)
	}
	return false
}

// rewriteRetrievalSessions atomically replaces the live log with kept entries.
func rewriteRetrievalSessions(vaultRoot string, kept []RetrievalSession) error {
	path := retrievalSessionLogFile(vaultRoot)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return fmt.Errorf("create tmp session log: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, s := range kept {
		enc, err := json.Marshal(s)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("marshal session: %w", err)
		}
		if _, err := w.Write(append(enc, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return fmt.Errorf("write session: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return fmt.Errorf("flush session log: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("close tmp session log: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename tmp session log: %w", err)
	}
	return nil
}

func citedRetrievalSessionIDs(vaultRoot string) (map[string]bool, error) {
	cLog, err := LoadCitations(vaultRoot)
	if err != nil {
		return nil, err
	}
	ids := make(map[string]bool)
	for _, c := range cLog.Citations {
		if c.SessionID != "" {
			ids[c.SessionID] = true
		}
	}
	return ids, nil
}

// PruneRetrievalSessions rewrites the JSONL log keeping sessions within
// `retentionDays` (plus citation-linked outliers). Internal judge prompts
// are always dropped. Returns the number of entries removed.
func PruneRetrievalSessions(vaultRoot string, retentionDays int) (int, error) {
	if retentionDays <= 0 {
		return 0, nil
	}
	sessions, err := LoadRetrievalSessions(vaultRoot)
	if err != nil {
		return 0, err
	}
	if len(sessions) == 0 {
		return 0, nil
	}

	cited, err := citedRetrievalSessionIDs(vaultRoot)
	if err != nil {
		return 0, err
	}

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var kept []RetrievalSession
	for _, s := range sessions {
		if retentionKeepSession(s, cutoff, cited) {
			kept = append(kept, s)
		}
	}
	dropped := len(sessions) - len(kept)
	if dropped == 0 {
		return 0, nil
	}
	if err := rewriteRetrievalSessions(vaultRoot, kept); err != nil {
		return 0, err
	}
	return dropped, nil
}

// MaybePruneRetrievalSessions runs PruneRetrievalSessions opportunistically.
// Small logs prune on every call; large logs throttle to once per interval
// so hook latency is not dominated by scanning a multi-hundred-MB file.
func MaybePruneRetrievalSessions(vaultRoot string, retentionDays int) {
	if retentionDays <= 0 {
		return
	}
	path := retrievalSessionLogFile(vaultRoot)
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if info.Size() >= pruneLargeFileBytes {
		marker := path + ".last_prune"
		if data, err := os.ReadFile(marker); err == nil {
			if t, err := time.Parse(time.RFC3339, strings.TrimSpace(string(data))); err == nil {
				if time.Since(t) < retrievalSessionPruneMinInterval {
					return
				}
			}
		}
		if dropped, err := PruneRetrievalSessions(vaultRoot, retentionDays); err != nil {
			fmt.Fprintf(os.Stderr, "[jm] retrieval session prune: %v\n", err)
			return
		} else if dropped > 0 {
			_ = os.WriteFile(marker, []byte(time.Now().Format(time.RFC3339)), 0o644)
		}
		return
	}
	if _, err := PruneRetrievalSessions(vaultRoot, retentionDays); err != nil {
		fmt.Fprintf(os.Stderr, "[jm] retrieval session prune: %v\n", err)
	}
}

// CompactRetrievalSessionLog archives the current log and rewrites a
// conversational-only subset. Used to recover from judge-prompt pollution.
// Returns kept count, dropped count, archive path (if archived), error.
func CompactRetrievalSessionLog(vaultRoot string, retentionDays int, dryRun bool) (int, int, string, error) {
	path := retrievalSessionLogFile(vaultRoot)
	sessions, err := LoadRetrievalSessions(vaultRoot)
	if err != nil {
		return 0, 0, "", err
	}
	if len(sessions) == 0 {
		return 0, 0, "", nil
	}

	cited, err := citedRetrievalSessionIDs(vaultRoot)
	if err != nil {
		return 0, 0, "", err
	}

	var cutoff time.Time
	if retentionDays > 0 {
		cutoff = time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	}

	var kept []RetrievalSession
	for _, s := range sessions {
		keep := cited[s.SessionID] ||
			(s.ConversationSessionID != "" && !IsInternalEvalPrompt(s.QueryContext))
		if keep && retentionDays > 0 && !cited[s.SessionID] && s.Timestamp.Before(cutoff) {
			keep = false
		}
		if keep {
			kept = append(kept, s)
		}
	}
	dropped := len(sessions) - len(kept)
	if dropped == 0 {
		return len(kept), 0, "", nil
	}
	if dryRun {
		return len(kept), dropped, "", nil
	}

	var archivePath string
	if info, err := os.Stat(path); err == nil && info.Size() > 0 {
		archivePath = filepath.Join(filepath.Dir(path), "Archive",
			fmt.Sprintf("retrieval_sessions.%s.jsonl", time.Now().Format("20060102-150405")))
		if err := os.MkdirAll(filepath.Dir(archivePath), 0o755); err != nil {
			return 0, 0, "", fmt.Errorf("mkdir archive: %w", err)
		}
		if err := os.Rename(path, archivePath); err != nil {
			return 0, 0, "", fmt.Errorf("archive session log: %w", err)
		}
	}

	if err := rewriteRetrievalSessions(vaultRoot, kept); err != nil {
		return 0, 0, archivePath, err
	}
	return len(kept), dropped, archivePath, nil
}
