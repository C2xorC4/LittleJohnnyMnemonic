package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDetectRuntimeHost_Grok(t *testing.T) {
	t.Setenv("GROK_HOOK_EVENT", "UserPromptSubmit")
	t.Setenv("GROK_SESSION_ID", "grok-1")
	if got := DetectRuntimeHost(); got != HostGrokBuild {
		t.Fatalf("DetectRuntimeHost() = %q, want %q", got, HostGrokBuild)
	}
}

func TestReadActiveSessions_MultiSessionVaultScoped(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)

	if err := writeSessionHeartbeatEx(vault, "session-a", "/vault/a", now.Add(-10*time.Minute), &HeartbeatOpts{
		RuntimeHost: HostGrokBuild,
		Event:       "user-prompt-submit",
	}); err != nil {
		t.Fatal(err)
	}
	if err := writeSessionHeartbeatEx(vault, "session-b", "/other/cwd", now.Add(-5*time.Minute), &HeartbeatOpts{
		RuntimeHost: HostClaudeCode,
		Event:       "user-prompt-submit",
	}); err != nil {
		t.Fatal(err)
	}

	sessions, err := ReadActiveSessions(vault, 45, now)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 2 {
		t.Fatalf("active sessions = %d, want 2", len(sessions))
	}
}

func TestVolleyCommitment_StopDefersReleaseWhenUnfulfilled(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	sessionID := "conv-1"

	if err := RecordVolleyCommitment(vault, sessionID, HostGrokBuild, now, 20); err != nil {
		t.Fatal(err)
	}
	if err := tryFulfillVolleyCommitmentOnStop(vault, sessionID, now.Add(time.Minute), nil); err != nil {
		t.Fatal(err)
	}
	pending, err := PendingVolleyCommitments(vault, now.Add(time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected commitment still pending after Stop, got: %+v", pending)
	}
	if err := resolveVolleyCommitment(vault, sessionID, now.Add(2*time.Minute), nil, true); err != nil {
		t.Fatal(err)
	}
	pending, err = PendingVolleyCommitments(vault, now.Add(2*time.Minute))
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected commitment released on deferred UPS pass, still pending: %+v", pending)
	}
}

func TestVolleyCommitment_FulfilledWhenDaydreamPromptLogged(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	sessionID := "conv-fulfill"

	if err := RecordVolleyCommitment(vault, sessionID, HostGrokBuild, now, 20); err != nil {
		t.Fatal(err)
	}
	session := RetrievalSession{
		SessionID:             "rs-1",
		Timestamp:             now.Add(2 * time.Minute),
		Loaded:                []string{"memory/project/johnny_mnemonic"},
		QueryContext:          "# Memory Daydream Agent\nexplore",
		ConversationSessionID: sessionID,
	}
	if err := AppendRetrievalSession(vault, session); err != nil {
		t.Fatal(err)
	}
	if err := resolveVolleyCommitment(vault, sessionID, now.Add(3*time.Minute), nil, true); err != nil {
		t.Fatal(err)
	}
	st, err := loadVolleyCommitments(vault)
	if err != nil {
		t.Fatal(err)
	}
	if st.Commitments[sessionID].Status != "fulfilled" {
		t.Fatalf("status = %q, want fulfilled", st.Commitments[sessionID].Status)
	}
}

func TestVolleyCommitment_FulfilledFromTranscript(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	sessionID := "conv-transcript"

	if err := RecordVolleyCommitment(vault, sessionID, HostGrokBuild, now, 20); err != nil {
		t.Fatal(err)
	}

	dir := filepath.Join(vault, "transcript")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "chat_history.jsonl")
	line := `{"type":"assistant","content":"Spawned spawn_subagent memory-daydream for topic seed."}` + "\n"
	if err := os.WriteFile(path, []byte(line), 0o644); err != nil {
		t.Fatal(err)
	}

	input := &hookInput{SessionID: sessionID, TranscriptPath: path}
	if err := resolveVolleyCommitment(vault, sessionID, now.Add(time.Minute), input, true); err != nil {
		t.Fatal(err)
	}
	st, err := loadVolleyCommitments(vault)
	if err != nil {
		t.Fatal(err)
	}
	if st.Commitments[sessionID].Status != "fulfilled" {
		t.Fatalf("status = %q, want fulfilled", st.Commitments[sessionID].Status)
	}
}

func TestVolleyCommitment_FulfilledViaBreadcrumb(t *testing.T) {
	vault := t.TempDir()
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	sessionID := "conv-crumb"

	if err := RecordVolleyCommitment(vault, sessionID, HostGrokBuild, now, 20); err != nil {
		t.Fatal(err)
	}

	daydreamDir := filepath.Join(vault, "Buffer", "Daydream")
	if err := os.MkdirAll(daydreamDir, 0o755); err != nil {
		t.Fatal(err)
	}
	crumb := `---
type: buffer
source: daydream
---
finding`
	path := filepath.Join(daydreamDir, "2026-06-16_test-crumb.md")
	if err := os.WriteFile(path, []byte(crumb), 0o644); err != nil {
		t.Fatal(err)
	}

	if err := resolveVolleyCommitment(vault, sessionID, now.Add(time.Minute), nil, true); err != nil {
		t.Fatal(err)
	}
	st, err := loadVolleyCommitments(vault)
	if err != nil {
		t.Fatal(err)
	}
	if st.Commitments[sessionID].Status != "fulfilled" {
		t.Fatalf("status = %q, want fulfilled", st.Commitments[sessionID].Status)
	}
}

func TestLastTurnBlobFromGrokUpdates_IncludesToolCalls(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.jsonl")
	lines := []string{
		`{"params":{"update":{"sessionUpdate":"user_message_chunk"}}}`,
		`{"params":{"update":{"sessionUpdate":"tool_call","title":"Daydream — topic seed","rawInput":{"subagent_type":"memory-daydream"}}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	blob, err := lastTurnTranscriptBlob(path)
	if err != nil {
		t.Fatal(err)
	}
	if !volleySpawnSignals(blob) {
		t.Fatalf("expected volley signals in blob, got %q", blob)
	}
}

func TestSchedulerHostAvailable_GrokHardSkip(t *testing.T) {
	if SchedulerHostAvailable(HostGrokBuild) {
		t.Fatal("grok-build should not have headless scheduler invoker yet")
	}
}

func TestRunAutodream_SkipsWhenVolleyCommitmentPending(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	if err := RecordVolleyCommitment(vault, "sess", HostGrokBuild, now, 20); err != nil {
		t.Fatal(err)
	}
	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       enabledCfg(),
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped || res.SkipCategory != SkipVolleyCommitment {
		t.Fatalf("Decision=%s SkipCategory=%q Reason=%q", res.Decision, res.SkipCategory, res.Reason)
	}
}

func TestRunAutodream_SkipsWhenSchedulerHostUnavailable(t *testing.T) {
	vault := runVault(t)
	now := time.Date(2026, 6, 16, 12, 0, 0, 0, time.UTC)
	cfg := enabledCfg()
	cfg.DaydreamSchedulerHost = HostGrokBuild
	cfg.AutoDaydreamActiveSkipWindowMinutes = 0
	in := AutodreamRunInputs{
		VaultRoot: vault,
		Cfg:       cfg,
		Now:       now,
		Rand:      seededRand(1, 2),
		Invoker:   fakeInvoker("ok"),
	}
	res := RunAutodream(in)
	if res.Decision != decisionSkipped || res.SkipCategory != SkipSchedulerHostUnavailable {
		t.Fatalf("Decision=%s SkipCategory=%q Reason=%q", res.Decision, res.SkipCategory, res.Reason)
	}
}

