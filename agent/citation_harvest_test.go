package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExtractCitedMemoryKeys(t *testing.T) {
	text := `See [[Memory/Knowledge/argus_lifecycle_state_machine]] and Memory/Project/argus.md
for context. Bare path Memory/Feedback/no_auto_push also counts.`
	keys := extractCitedMemoryKeys(text)
	want := []string{
		"memory/knowledge/argus_lifecycle_state_machine",
		"memory/project/argus",
		"memory/feedback/no_auto_push",
	}
	if len(keys) != len(want) {
		t.Fatalf("got %d keys %v, want %d", len(keys), keys, len(want))
	}
	for i, w := range want {
		if keys[i] != w {
			t.Errorf("keys[%d] = %q, want %q", i, keys[i], w)
		}
	}
}

func TestFindLatestRetrievalSessionForConversation(t *testing.T) {
	vault := t.TempDir()
	conv := "conv-abc"

	older := RetrievalSession{
		SessionID:             "rs-old",
		Timestamp:             time.Now().Add(-2 * time.Hour),
		Loaded:                []string{"memory/project/alpha"},
		ConversationSessionID: conv,
	}
	newer := RetrievalSession{
		SessionID:             "rs-new",
		Timestamp:             time.Now().Add(-1 * time.Minute),
		Loaded:                []string{"memory/project/beta"},
		ConversationSessionID: conv,
	}
	other := RetrievalSession{
		SessionID:             "rs-other",
		Timestamp:             time.Now(),
		Loaded:                []string{"memory/project/gamma"},
		ConversationSessionID: "other-conv",
	}

	for _, s := range []RetrievalSession{older, newer, other} {
		if err := AppendRetrievalSession(vault, s); err != nil {
			t.Fatal(err)
		}
	}

	got, err := FindLatestRetrievalSessionForConversation(vault, conv)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.SessionID != "rs-new" {
		t.Fatalf("expected rs-new, got %+v", got)
	}
}

func TestHarvestCitationsFromStop_RecordsCitationAndReinforcesEdges(t *testing.T) {
	vault := t.TempDir()
	memDir := filepath.Join(vault, "Memory", "Project")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}

	alphaBody := `---
type: project
title: Alpha
tags: [alpha]
links:
  - target: "[[Memory/Project/beta]]"
    relationship: learned
---
alpha body`
	betaBody := `---
type: project
title: Beta
tags: [beta]
---
beta body`
	if err := os.WriteFile(filepath.Join(memDir, "alpha.md"), []byte(alphaBody), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(memDir, "beta.md"), []byte(betaBody), 0o644); err != nil {
		t.Fatal(err)
	}

	convID := "test-conv-harvest"
	rsID := "rs-harvest-1"
	session := RetrievalSession{
		SessionID:             rsID,
		Timestamp:             time.Now(),
		Loaded:                []string{"memory/project/alpha", "memory/project/beta"},
		ConversationSessionID: convID,
	}
	if err := AppendRetrievalSession(vault, session); err != nil {
		t.Fatal(err)
	}

	transcriptDir := t.TempDir()
	transcriptPath := filepath.Join(transcriptDir, "transcript.jsonl")
	assistantEvent := map[string]any{
		"type": "assistant",
		"message": map[string]any{
			"role":    "assistant",
			"content": "Applied guidance from Memory/Project/alpha per the lifecycle entry.",
		},
	}
	line, err := json.Marshal(assistantEvent)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(transcriptPath, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	// Enable retrieval session logging + adaptive edge pilot for harvest.
	cfgPath := filepath.Join(vault, "System", "Config.md")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	cfgContent := "# Config\n\n```yaml\nretrieval_session_log_enabled: true\nadaptive_edge_weighting_enabled: true\nadaptive_edge_scope: learned\n```\n"
	if err := os.WriteFile(cfgPath, []byte(cfgContent), 0o644); err != nil {
		t.Fatal(err)
	}

	harvestCitationsFromStop(vault, &hookInput{
		SessionID:      convID,
		TranscriptPath: transcriptPath,
	})

	cLog, err := LoadCitations(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(cLog.Citations) != 1 {
		t.Fatalf("expected 1 citation, got %d: %+v", len(cLog.Citations), cLog.Citations)
	}
	c := cLog.Citations[0]
	if c.MemoryKey != "memory/project/alpha" {
		t.Errorf("citation key = %q, want memory/project/alpha", c.MemoryKey)
	}
	if c.SessionID != rsID {
		t.Errorf("citation session = %q, want %s", c.SessionID, rsID)
	}
	if !c.Useful {
		t.Error("expected useful=true")
	}

	usage, err := LoadEdgeUsage(vault)
	if err != nil {
		t.Fatal(err)
	}
	key := edgeUsageKey("memory/project/alpha", "memory/project/beta", "learned")
	if u, ok := usage[key]; !ok || u.UsageCount < 1 {
		t.Errorf("expected reinforced learned edge %q, usage=%+v ok=%v", key, u, ok)
	}
}

func TestWriteRetrievalSessionID_EmitsTag(t *testing.T) {
	var buf strings.Builder
	writeRetrievalSessionID(&buf, "abcd-1234-5678-90ab-cdef12345678")
	out := buf.String()
	if !strings.Contains(out, `<retrieval-session id="abcd-1234-5678-90ab-cdef12345678"/>`) {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestLastAssistantTurn_GrokChatHistory(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "chat_history.jsonl")
	lines := []string{
		`{"type":"user","content":[{"type":"text","text":"hello"}]}`,
		`{"type":"assistant","content":"partial","tool_calls":[{"id":"x","name":"Read"}]}`,
		`{"type":"tool_result","tool_call_id":"x","content":"file body"}`,
		`{"type":"assistant","content":"Grounded in Memory/Project/alpha and [[Memory/Project/beta]]."}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := lastAssistantTurn(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Memory/Project/alpha") {
		t.Fatalf("expected final assistant text, got %q", got)
	}
}

func TestLastAssistantTurn_GrokUpdates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.jsonl")
	lines := []string{
		`{"params":{"update":{"sessionUpdate":"user_message_chunk"}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Part one "}}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Memory/Project/alpha"}}}}`,
		`{"params":{"update":{"sessionUpdate":"user_message_chunk"}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"New turn "}}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Memory/Project/beta"}}}}`,
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := lastAssistantTurn(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "Memory/Project/beta") {
		t.Fatalf("expected last turn text, got %q", got)
	}
	if strings.Contains(got, "Memory/Project/alpha") {
		t.Fatalf("did not expect prior turn text, got %q", got)
	}
}

func TestHarvestCitationsFromPreviousTurn_DedupesRetrievalSession(t *testing.T) {
	vault := t.TempDir()
	memDir := filepath.Join(vault, "Memory", "Project")
	if err := os.MkdirAll(memDir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := `---
type: project
title: Alpha
---
alpha`
	if err := os.WriteFile(filepath.Join(memDir, "alpha.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	convID := "conv-dedup"
	rsID := "rs-dedup-1"
	if err := AppendRetrievalSession(vault, RetrievalSession{
		SessionID:             rsID,
		Timestamp:             time.Now(),
		Loaded:                []string{"memory/project/alpha"},
		ConversationSessionID: convID,
	}); err != nil {
		t.Fatal(err)
	}

	transcriptDir := t.TempDir()
	transcriptPath := filepath.Join(transcriptDir, "chat_history.jsonl")
	line, _ := json.Marshal(map[string]any{
		"type":    "assistant",
		"content": "See Memory/Project/alpha for details.",
	})
	if err := os.WriteFile(transcriptPath, append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}

	cfgPath := filepath.Join(vault, "System", "Config.md")
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgPath, []byte("# Config\n\n```yaml\nretrieval_session_log_enabled: true\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	input := &hookInput{SessionID: convID, TranscriptPath: transcriptPath}
	harvestCitationsFromPreviousTurn(vault, input)
	harvestCitationsFromPreviousTurn(vault, input)

	cLog, err := LoadCitations(vault)
	if err != nil {
		t.Fatal(err)
	}
	if len(cLog.Citations) != 1 {
		t.Fatalf("expected 1 citation after duplicate harvest attempts, got %d", len(cLog.Citations))
	}
}