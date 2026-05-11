package main

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
	SessionID    string    `json:"session_id"`
	Timestamp    time.Time `json:"timestamp"`
	Loaded       []string  `json:"loaded"`
	QueryContext string    `json:"query_context,omitempty"`
	QueryTags    []string  `json:"query_tags,omitempty"`
}

const retrievalSessionLogPath = "Metrics/retrieval_sessions.jsonl"

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
	path := filepath.Join(vaultRoot, retrievalSessionLogPath)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("create metrics dir: %w", err)
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
	path := filepath.Join(vaultRoot, retrievalSessionLogPath)
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

// PruneRetrievalSessions rewrites the JSONL log keeping only sessions
// whose Timestamp is within `retentionDays` of now. Called by
// `cmd_retrieve` opportunistically (cheap rewrite given the small log
// size). Returns the number of entries dropped.
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

	cutoff := time.Now().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	var kept []RetrievalSession
	for _, s := range sessions {
		if s.Timestamp.After(cutoff) {
			kept = append(kept, s)
		}
	}
	dropped := len(sessions) - len(kept)
	if dropped == 0 {
		return 0, nil
	}

	// Atomic rewrite: write to temp, rename over original.
	path := filepath.Join(vaultRoot, retrievalSessionLogPath)
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return 0, fmt.Errorf("create tmp session log: %w", err)
	}
	w := bufio.NewWriter(f)
	for _, s := range kept {
		enc, err := json.Marshal(s)
		if err != nil {
			f.Close()
			os.Remove(tmp)
			return 0, fmt.Errorf("marshal session: %w", err)
		}
		if _, err := w.Write(append(enc, '\n')); err != nil {
			f.Close()
			os.Remove(tmp)
			return 0, fmt.Errorf("write session: %w", err)
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		os.Remove(tmp)
		return 0, fmt.Errorf("flush session log: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return 0, fmt.Errorf("close tmp session log: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return 0, fmt.Errorf("rename tmp session log: %w", err)
	}
	return dropped, nil
}
