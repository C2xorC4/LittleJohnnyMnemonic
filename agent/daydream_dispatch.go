package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Runtime host identifiers for daydream dispatch. Extensible — new hosts
// register here and in ResolveSchedulerInvoker.
const (
	HostClaudeCode = "claude-code"
	HostGrokBuild  = "grok-build"
	HostUnknown    = "unknown"
)

// Daydream volley / scheduler policy constants (config keys mirror these).
const (
	DaydreamVolleyPolicyDelegateActive = "delegate_active"
	DaydreamSchedulerMissingInvokerSkip = "skip" // hard skip when headless invoker absent
)

const (
	daydreamDispatchLogPath        = "Metrics/daydream_dispatch.jsonl"
	daydreamVolleyCommitmentsPath  = "Metrics/daydream_volley_commitments.json"
	daydreamAgentPromptMarker      = "Memory Daydream Agent"
)

// SessionHeartbeatRecord is one append-only line in session_heartbeat.jsonl.
type SessionHeartbeatRecord struct {
	Timestamp            string `json:"timestamp"`
	SessionID            string `json:"session_id"`
	Cwd                  string `json:"cwd"`
	RuntimeHost          string `json:"runtime_host,omitempty"`
	Event                string `json:"event,omitempty"`
	PromptChars          int    `json:"prompt_chars,omitempty"`
	MemoriesRetrieved    int    `json:"memories_retrieved,omitempty"`
	DaydreamNudgeEmitted bool   `json:"daydream_nudge_emitted,omitempty"`
}

// HeartbeatOpts carries optional metadata for a heartbeat write.
type HeartbeatOpts struct {
	RuntimeHost          string
	Event                string
	PromptChars          int
	MemoriesRetrieved    int
	DaydreamNudgeEmitted bool
}

// ActiveSession summarizes a vault-touching session still inside the activity window.
type ActiveSession struct {
	SessionID   string
	RuntimeHost string
	Cwd         string
	LastSeen    time.Time
	Event       string
}

// VolleyCommitment tracks a hook-emitted daydream nudge awaiting agent spawn.
type VolleyCommitment struct {
	SessionID   string    `json:"session_id"`
	RuntimeHost string    `json:"runtime_host"`
	CommittedAt time.Time `json:"committed_at"`
	ExpiresAt   time.Time `json:"expires_at"`
	Status      string    `json:"status"` // pending | fulfilled | released
}

type volleyCommitmentStore struct {
	Commitments map[string]VolleyCommitment `json:"commitments"`
}

// DaydreamDispatchLogEntry audits dispatch decisions.
type DaydreamDispatchLogEntry struct {
	Timestamp      time.Time `json:"timestamp"`
	Channel        string    `json:"channel"`
	HostDetected   string    `json:"host_detected,omitempty"`
	HostPreferred  string    `json:"host_preferred,omitempty"`
	HostEffective  string    `json:"host_effective,omitempty"`
	Decision       string    `json:"decision"`
	Reason         string    `json:"reason,omitempty"`
	SessionID      string    `json:"session_id,omitempty"`
	SkipCategory   string    `json:"skip_category,omitempty"`
}

// DetectRuntimeHostFromEnv is an alias for tests that inject env vars.
func DetectRuntimeHostFromEnv() string {
	return DetectRuntimeHost()
}

// normalizeSchedulerHost maps empty config to claude-code default.
func normalizeSchedulerHost(host string) string {
	h := strings.TrimSpace(strings.ToLower(host))
	if h == "" {
		return HostClaudeCode
	}
	return h
}

// ReadActiveSessions returns the latest heartbeat per session_id within window.
// Any session touching this vault counts — not cwd-scoped.
func ReadActiveSessions(vaultRoot string, windowMinutes int, now time.Time) ([]ActiveSession, error) {
	if windowMinutes <= 0 {
		return nil, nil
	}
	path := filepath.Join(vaultRoot, "Metrics", "session_heartbeat.jsonl")
	records, err := readRecentHeartbeatRecords(path, 512*1024)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	cutoff := now.Add(-time.Duration(windowMinutes) * time.Minute)
	latest := make(map[string]ActiveSession)
	for _, rec := range records {
		ts, err := time.Parse(time.RFC3339, rec.Timestamp)
		if err != nil || ts.Before(cutoff) {
			continue
		}
		if rec.SessionID == "" {
			continue
		}
		host := rec.RuntimeHost
		if host == "" {
			host = HostUnknown
		}
		prev, ok := latest[rec.SessionID]
		if !ok || ts.After(prev.LastSeen) {
			latest[rec.SessionID] = ActiveSession{
				SessionID:   rec.SessionID,
				RuntimeHost: host,
				Cwd:         rec.Cwd,
				LastSeen:    ts,
				Event:       rec.Event,
			}
		}
	}
	out := make([]ActiveSession, 0, len(latest))
	for _, s := range latest {
		out = append(out, s)
	}
	return out, nil
}

func readRecentHeartbeatRecords(path string, maxBytes int64) ([]SessionHeartbeatRecord, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, nil
	}

	start := int64(0)
	if info.Size() > maxBytes {
		start = info.Size() - maxBytes
	}
	buf := make([]byte, info.Size()-start)
	if _, err := f.ReadAt(buf, start); err != nil && err != io.EOF {
		return nil, err
	}

	var records []SessionHeartbeatRecord
	scanner := bufio.NewScanner(strings.NewReader(string(buf)))
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec SessionHeartbeatRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		records = append(records, rec)
	}
	return records, scanner.Err()
}

func writeSessionHeartbeatEx(vaultRoot, sessionID, cwd string, ts time.Time, opts *HeartbeatOpts) error {
	if os.Getenv(autodreamInvocationEnvVar) == "1" || IsInternalInvocation() {
		return nil
	}

	dir := filepath.Join(vaultRoot, "Metrics")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir Metrics: %w", err)
	}
	path := filepath.Join(dir, "session_heartbeat.jsonl")

	if cfg := LoadConfig(vaultRoot); cfg.AutoDaydreamLogRotationThreshold > 0 {
		if err := rotateJSONLIfNeeded(path, cfg.AutoDaydreamLogRotationThreshold, ts); err != nil {
			fmt.Fprintf(os.Stderr, "[jm hook] heartbeat rotation: %v\n", err)
		}
	}

	rec := SessionHeartbeatRecord{
		Timestamp: ts.Format(time.RFC3339),
		SessionID: sessionID,
		Cwd:       cwd,
	}
	if opts != nil {
		rec.RuntimeHost = opts.RuntimeHost
		rec.Event = opts.Event
		rec.PromptChars = opts.PromptChars
		rec.MemoriesRetrieved = opts.MemoriesRetrieved
		rec.DaydreamNudgeEmitted = opts.DaydreamNudgeEmitted
	}
	if rec.RuntimeHost == "" {
		rec.RuntimeHost = DetectRuntimeHost()
	}

	line, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("marshal heartbeat: %w", err)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("open heartbeat file: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return fmt.Errorf("write heartbeat: %w", err)
	}
	return nil
}

func loadVolleyCommitments(vaultRoot string) (*volleyCommitmentStore, error) {
	path := filepath.Join(vaultRoot, daydreamVolleyCommitmentsPath)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &volleyCommitmentStore{Commitments: make(map[string]VolleyCommitment)}, nil
		}
		return nil, err
	}
	var st volleyCommitmentStore
	if err := json.Unmarshal(data, &st); err != nil {
		return nil, err
	}
	if st.Commitments == nil {
		st.Commitments = make(map[string]VolleyCommitment)
	}
	return &st, nil
}

func saveVolleyCommitments(vaultRoot string, st *volleyCommitmentStore) error {
	path := filepath.Join(vaultRoot, daydreamVolleyCommitmentsPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	writeAtomic(path, data, 0o644)
	return nil
}

// RecordVolleyCommitment marks a session as having received a daydream nudge.
func RecordVolleyCommitment(vaultRoot, sessionID, runtimeHost string, now time.Time, ttlMinutes int) error {
	if sessionID == "" || ttlMinutes <= 0 {
		return nil
	}
	st, err := loadVolleyCommitments(vaultRoot)
	if err != nil {
		return err
	}
	st.Commitments[sessionID] = VolleyCommitment{
		SessionID:   sessionID,
		RuntimeHost: runtimeHost,
		CommittedAt: now,
		ExpiresAt:   now.Add(time.Duration(ttlMinutes) * time.Minute),
		Status:      "pending",
	}
	return saveVolleyCommitments(vaultRoot, st)
}

// PendingVolleyCommitments returns commitments still blocking the scheduler.
func PendingVolleyCommitments(vaultRoot string, now time.Time) ([]VolleyCommitment, error) {
	st, err := loadVolleyCommitments(vaultRoot)
	if err != nil {
		return nil, err
	}
	var pending []VolleyCommitment
	for _, c := range st.Commitments {
		if c.Status != "pending" {
			continue
		}
		if now.After(c.ExpiresAt) {
			continue
		}
		pending = append(pending, c)
	}
	return pending, nil
}

func markVolleyCommitment(vaultRoot, sessionID, status string) error {
	st, err := loadVolleyCommitments(vaultRoot)
	if err != nil {
		return err
	}
	c, ok := st.Commitments[sessionID]
	if !ok || c.Status != "pending" {
		return nil
	}
	c.Status = status
	st.Commitments[sessionID] = c
	return saveVolleyCommitments(vaultRoot, st)
}

// VolleyFulfilledSince checks retrieval_sessions for a daydream agent prompt
// in the same conversation session after the commitment time.
func VolleyFulfilledSince(vaultRoot, conversationSessionID string, since time.Time) bool {
	if conversationSessionID == "" {
		return false
	}
	sessions, err := LoadRetrievalSessions(vaultRoot)
	if err != nil {
		return false
	}
	for i := len(sessions) - 1; i >= 0; i-- {
		s := &sessions[i]
		if s.ConversationSessionID != conversationSessionID {
			continue
		}
		if s.Timestamp.Before(since) {
			continue
		}
		if strings.Contains(s.QueryContext, daydreamAgentPromptMarker) {
			return true
		}
	}
	return false
}

// resolveVolleyCommitmentFromPreviousTurn runs at UserPromptSubmit before a new
// nudge may be recorded. Grok Stop fires before assistant text and tool calls
// are flushed to updates.jsonl — defer release to here, mirroring citation harvest.
func resolveVolleyCommitmentFromPreviousTurn(vaultRoot string, input *hookInput) {
	if input == nil || input.SessionID == "" {
		return
	}
	if err := resolveVolleyCommitment(vaultRoot, input.SessionID, time.Now(), input, true); err != nil {
		fmt.Fprintf(os.Stderr, "[jm hook] user-prompt-submit: volley commitment: %v\n", err)
	}
}

// tryFulfillVolleyCommitmentOnStop attempts fulfillment at Stop but does not
// release unfulfilled commitments — Grok transcript may not be ready yet.
func tryFulfillVolleyCommitmentOnStop(vaultRoot, sessionID string, now time.Time, input *hookInput) error {
	return resolveVolleyCommitment(vaultRoot, sessionID, now, input, false)
}

// resolveVolleyCommitment fulfills or releases a pending volley commitment.
// When releaseIfUnfulfilled is false (Stop on Grok), pending commitments stay
// open until the next UserPromptSubmit pass.
func resolveVolleyCommitment(vaultRoot, sessionID string, now time.Time, input *hookInput, releaseIfUnfulfilled bool) error {
	if sessionID == "" {
		return nil
	}
	st, err := loadVolleyCommitments(vaultRoot)
	if err != nil {
		return err
	}
	c, ok := st.Commitments[sessionID]
	if !ok || c.Status != "pending" {
		return nil
	}
	if now.After(c.ExpiresAt) {
		c.Status = "released"
		st.Commitments[sessionID] = c
		return saveVolleyCommitments(vaultRoot, st)
	}

	reason, fulfilled := volleyFulfillmentOracles(vaultRoot, sessionID, input, c.CommittedAt)
	if fulfilled {
		channel := "volley"
		decision := "fulfilled"
		if reason != "" {
			decision = reason
		}
		_ = appendDaydreamDispatchLog(vaultRoot, DaydreamDispatchLogEntry{
			Timestamp:    now,
			Channel:      channel,
			HostDetected: c.RuntimeHost,
			Decision:     decision,
			SessionID:    sessionID,
			Reason:       "daydream volley fulfillment oracle matched",
		})
		return markVolleyCommitment(vaultRoot, sessionID, "fulfilled")
	}
	if !releaseIfUnfulfilled {
		return nil
	}
	return markVolleyCommitment(vaultRoot, sessionID, "released")
}

func volleyFulfillmentOracles(vaultRoot, sessionID string, input *hookInput, since time.Time) (reason string, ok bool) {
	if VolleyFulfilledSince(vaultRoot, sessionID, since) {
		return "fulfilled_retrieval_session", true
	}
	if VolleySpawnedInTurn(input, since) {
		return "fulfilled_transcript", true
	}
	if VolleyFulfilledViaBreadcrumb(vaultRoot, since) {
		return "fulfilled_breadcrumb", true
	}
	return "", false
}

// VolleySpawnedInTurn checks the conversation transcript for a memory-daydream
// spawn in the prior assistant turn.
func VolleySpawnedInTurn(input *hookInput, since time.Time) bool {
	transcriptPath := resolveTranscriptPath(input)
	if transcriptPath == "" {
		return false
	}
	_ = since
	blob, err := lastTurnTranscriptBlob(transcriptPath)
	if err != nil || blob == "" {
		return false
	}
	return volleySpawnSignals(blob)
}

// VolleyFulfilledViaBreadcrumb reports whether a daydream buffer entry landed
// after the commitment — catches background subagents that finish after Stop.
func VolleyFulfilledViaBreadcrumb(vaultRoot string, since time.Time) bool {
	dir := filepath.Join(vaultRoot, "Buffer", "Daydream")
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(strings.ToLower(ent.Name()), ".md") {
			continue
		}
		info, err := ent.Info()
		if err != nil || info.ModTime().Before(since) {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, ent.Name()))
		if err != nil {
			continue
		}
		body := string(data)
		if strings.Contains(body, "source: daydream") {
			return true
		}
	}
	return false
}

func volleySpawnSignals(text string) bool {
	lower := strings.ToLower(text)
	signals := []string{
		"memory-daydream",
		"memory daydream agent",
		daydreamAgentPromptMarker,
		"daydream —",
		"daydream -",
		"spawn_subagent",
	}
	for _, s := range signals {
		if strings.Contains(lower, strings.ToLower(s)) {
			return true
		}
	}
	return false
}

func appendDaydreamDispatchLog(vaultRoot string, entry DaydreamDispatchLogEntry) error {
	path := filepath.Join(vaultRoot, daydreamDispatchLogPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	line, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}

func formatActiveSessionsReason(sessions []ActiveSession, now time.Time) string {
	if len(sessions) == 1 {
		s := sessions[0]
		age := int(now.Sub(s.LastSeen).Minutes())
		return fmt.Sprintf("active session %s (%s, %dm ago)", s.SessionID, s.RuntimeHost, age)
	}
	return fmt.Sprintf("active sessions (%d vault-touching)", len(sessions))
}