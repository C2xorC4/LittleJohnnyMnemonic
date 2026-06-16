package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestGenerateSessionID_FormatAndUniqueness(t *testing.T) {
	a := GenerateSessionID()
	b := GenerateSessionID()
	if a == b {
		t.Errorf("two consecutive session IDs collided: %s", a)
	}
	// UUID v4 form: 8-4-4-4-12 hex digits
	if got := len(a); got != 36 {
		t.Errorf("session ID length = %d, expected 36; got %s", got, a)
	}
	parts := strings.Split(a, "-")
	expectedLens := []int{8, 4, 4, 4, 12}
	if len(parts) != 5 {
		t.Fatalf("session ID has %d parts, expected 5: %s", len(parts), a)
	}
	for i, p := range parts {
		if len(p) != expectedLens[i] {
			t.Errorf("session ID part %d length = %d, expected %d: %s", i, len(p), expectedLens[i], a)
		}
	}
	// Version nibble: 13th hex digit (index 14 if you count dashes) is "4"
	if a[14] != '4' {
		t.Errorf("expected UUID v4 marker '4' at position 14, got %c in %s", a[14], a)
	}
	// Variant nibble: 19th hex digit (index 19 if you count dashes) is 8, 9, a, or b
	switch a[19] {
	case '8', '9', 'a', 'b':
		// ok
	default:
		t.Errorf("expected UUID variant marker [89ab] at position 19, got %c in %s", a[19], a)
	}
}

func TestRetrievalSession_AppendLoadFind(t *testing.T) {
	vault := t.TempDir()

	s1 := RetrievalSession{
		SessionID:    "id-1",
		Timestamp:    time.Now().Add(-1 * time.Hour),
		Loaded:       []string{"Memory/Project/argus", "Memory/Knowledge/abc"},
		QueryContext: "argus binary analysis",
		QueryTags:    []string{"argus", "binary"},
	}
	s2 := RetrievalSession{
		SessionID:    "id-2",
		Timestamp:    time.Now(),
		Loaded:       []string{"Memory/User/profile_expertise"},
		QueryContext: "user profile",
	}

	if err := AppendRetrievalSession(vault, s1); err != nil {
		t.Fatalf("append s1: %v", err)
	}
	if err := AppendRetrievalSession(vault, s2); err != nil {
		t.Fatalf("append s2: %v", err)
	}

	loaded, err := LoadRetrievalSessions(vault)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("loaded %d sessions, expected 2", len(loaded))
	}
	if loaded[0].SessionID != "id-1" || loaded[1].SessionID != "id-2" {
		t.Errorf("session order broken: %+v", loaded)
	}

	found, err := FindRetrievalSession(vault, "id-1")
	if err != nil {
		t.Fatalf("find: %v", err)
	}
	if found == nil {
		t.Fatalf("session id-1 not found")
	}
	if len(found.Loaded) != 2 || found.Loaded[0] != "Memory/Project/argus" {
		t.Errorf("found.Loaded mismatch: %+v", found.Loaded)
	}

	missing, err := FindRetrievalSession(vault, "no-such-id")
	if err != nil {
		t.Fatalf("find missing: %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil for missing id, got %+v", missing)
	}
}

func TestRetrievalSession_LoadEmptyOrAbsent(t *testing.T) {
	vault := t.TempDir()
	// No log file written yet.
	loaded, err := LoadRetrievalSessions(vault)
	if err != nil {
		t.Fatalf("load absent: %v", err)
	}
	if loaded != nil {
		t.Errorf("expected nil, got %+v", loaded)
	}
}

func TestRetrievalSession_PruneByRetention(t *testing.T) {
	vault := t.TempDir()

	old := RetrievalSession{
		SessionID: "old",
		Timestamp: time.Now().Add(-30 * 24 * time.Hour),
		Loaded:    []string{"Memory/X/a"},
	}
	recent := RetrievalSession{
		SessionID: "recent",
		Timestamp: time.Now().Add(-1 * time.Hour),
		Loaded:    []string{"Memory/X/b"},
	}
	for _, s := range []RetrievalSession{old, recent} {
		if err := AppendRetrievalSession(vault, s); err != nil {
			t.Fatalf("append %s: %v", s.SessionID, err)
		}
	}

	dropped, err := PruneRetrievalSessions(vault, 14)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if dropped != 1 {
		t.Errorf("dropped = %d, expected 1", dropped)
	}

	remaining, err := LoadRetrievalSessions(vault)
	if err != nil {
		t.Fatalf("load after prune: %v", err)
	}
	if len(remaining) != 1 || remaining[0].SessionID != "recent" {
		t.Errorf("remaining sessions: %+v", remaining)
	}

	// Re-running prune should be a no-op (already at retention).
	dropped2, err := PruneRetrievalSessions(vault, 14)
	if err != nil {
		t.Fatalf("prune2: %v", err)
	}
	if dropped2 != 0 {
		t.Errorf("dropped2 = %d, expected 0", dropped2)
	}
}

func TestRetrievalSession_PruneDisabled(t *testing.T) {
	vault := t.TempDir()
	s := RetrievalSession{
		SessionID: "x",
		Timestamp: time.Now().Add(-365 * 24 * time.Hour),
		Loaded:    []string{"Memory/X/a"},
	}
	if err := AppendRetrievalSession(vault, s); err != nil {
		t.Fatalf("append: %v", err)
	}

	// retentionDays = 0 → no pruning
	dropped, err := PruneRetrievalSessions(vault, 0)
	if err != nil {
		t.Fatalf("prune disabled: %v", err)
	}
	if dropped != 0 {
		t.Errorf("dropped with retention=0 = %d, expected 0", dropped)
	}
	loaded, err := LoadRetrievalSessions(vault)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(loaded) != 1 {
		t.Errorf("loaded %d, expected 1", len(loaded))
	}
}

func TestIsInternalEvalPrompt(t *testing.T) {
	if !IsInternalEvalPrompt("You evaluate whether a buffer entry for a memory system adds novel") {
		t.Error("expected buffer judge prefix to match")
	}
	if IsInternalEvalPrompt("What is the state of PR1 retrieval-session emission?") {
		t.Error("user query should not match internal eval")
	}
	t.Setenv(InternalInvocationEnvVar, "1")
	if !IsInternalEvalPrompt("anything") {
		t.Error("internal invocation env should force internal eval")
	}
}

func TestShouldLogHookRetrievalSession(t *testing.T) {
	t.Setenv(InternalInvocationEnvVar, "")
	if ShouldLogHookRetrievalSession("", "user question") {
		t.Error("empty conversation session should not log")
	}
	if ShouldLogHookRetrievalSession("conv-1", "You evaluate whether a buffer entry") {
		t.Error("judge prompt should not log")
	}
	if !ShouldLogHookRetrievalSession("conv-1", "<user_query>\nWhat is PR1 status?\n</user_query>") {
		t.Error("conversational prompt with session id should log")
	}
}

func TestCompactRetrievalSessionLog_DropsJudgePollution(t *testing.T) {
	vault := t.TempDir()
	cfgDir := filepath.Join(vault, "System")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cfgDir, "Config.md"),
		[]byte("```yaml\nretrieval_session_log_retention_days: 14\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	judge := RetrievalSession{
		SessionID:    "judge-1",
		Timestamp:    time.Now(),
		Loaded:       []string{"memory/x/a"},
		QueryContext: "You evaluate whether a buffer entry for a memory system adds novel information",
	}
	conv := RetrievalSession{
		SessionID:             "conv-1",
		Timestamp:             time.Now(),
		Loaded:                []string{"memory/project/johnny_mnemonic"},
		QueryContext:          "<user_query>\nstatus check\n</user_query>",
		ConversationSessionID: "host-session",
	}
	for _, s := range []RetrievalSession{judge, conv} {
		if err := AppendRetrievalSession(vault, s); err != nil {
			t.Fatalf("append: %v", err)
		}
	}

	kept, dropped, _, err := CompactRetrievalSessionLog(vault, 14, false)
	if err != nil {
		t.Fatalf("compact: %v", err)
	}
	if dropped != 1 || kept != 1 {
		t.Errorf("kept=%d dropped=%d, want kept=1 dropped=1", kept, dropped)
	}
	remaining, err := LoadRetrievalSessions(vault)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(remaining) != 1 || remaining[0].SessionID != "conv-1" {
		t.Errorf("remaining: %+v", remaining)
	}
}

func TestRetrievalSession_LogPathLocation(t *testing.T) {
	vault := t.TempDir()
	s := RetrievalSession{
		SessionID: "test",
		Timestamp: time.Now(),
		Loaded:    []string{"Memory/X/a"},
	}
	if err := AppendRetrievalSession(vault, s); err != nil {
		t.Fatalf("append: %v", err)
	}

	expected := filepath.Join(vault, "Metrics", "retrieval_sessions.jsonl")
	if _, err := os.Stat(expected); err != nil {
		t.Errorf("expected log at %s, got error: %v", expected, err)
	}
}
