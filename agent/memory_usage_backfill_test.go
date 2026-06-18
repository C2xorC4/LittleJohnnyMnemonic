package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseGrokUpdatesTurnPairs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "updates.jsonl")
	content := strings.Join([]string{
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Hello world"},"_meta":{"modelId":"grok-composer-2.5-fast"}}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Hi there"}}}}`,
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"Second question"},"_meta":{"modelId":"claude-opus-4-8"}}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"Second answer mentions repo_trust_protocol"}}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	turns, err := parseGrokUpdatesTurnPairs(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("turns = %d, want 2", len(turns))
	}
	if turns[0].Model != "grok-composer-2.5-fast" || turns[0].AssistantText != "Hi there" {
		t.Fatalf("turn0: %+v", turns[0])
	}
	if turns[1].Model != "claude-opus-4-8" || !strings.Contains(turns[1].AssistantText, "repo_trust_protocol") {
		t.Fatalf("turn1: %+v", turns[1])
	}
}

func TestMatchRetrievalSessionToTurn(t *testing.T) {
	rs := RetrievalSession{
		QueryContext: "<user_query>\nSecond question\n</user_query>",
	}
	turns := []transcriptTurnPair{
		{UserText: "Hello world", AssistantText: "Hi"},
		{UserText: "Second question with more detail", AssistantText: "Answer"},
	}
	turn, ok := matchRetrievalSessionToTurn(rs, turns)
	if !ok || turn.AssistantText != "Answer" {
		t.Fatalf("match failed: ok=%v turn=%+v", ok, turn)
	}
}

func TestBackfillMemoryUsage_DryRun(t *testing.T) {
	vault := t.TempDir()
	for _, dir := range []string{"Metrics", "System"} {
		if err := os.MkdirAll(filepath.Join(vault, dir), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(vault, "System", "Config.md"), []byte("```yaml\nmemory_usage_tracking_enabled: true\n```\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	convID := "019ed125-655b-7da0-aaaf-fedb9098effd"
	grokHome := t.TempDir()
	sessDir := filepath.Join(grokHome, "sessions", "encoded", convID)
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		t.Fatal(err)
	}
	transcript := strings.Join([]string{
		`{"params":{"update":{"sessionUpdate":"user_message_chunk","content":{"type":"text","text":"What is LJM status?"}}}}`,
		`{"params":{"update":{"sessionUpdate":"agent_message_chunk","content":{"type":"text","text":"See [[Memory/Project/johnny_mnemonic]] for status."}}}}`,
	}, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(sessDir, "updates.jsonl"), []byte(transcript), 0o644); err != nil {
		t.Fatal(err)
	}

	rs := RetrievalSession{
		SessionID:             "rs-1",
		Timestamp:             time.Now(),
		Loaded:                []string{"memory/project/johnny_mnemonic"},
		QueryContext:          "What is LJM status?",
		ConversationSessionID: convID,
	}
	if err := AppendRetrievalSession(vault, rs); err != nil {
		t.Fatal(err)
	}

	res, err := backfillMemoryUsage(vault, backfillUsageOpts{dryRun: true, grokHome: grokHome})
	if err != nil {
		t.Fatal(err)
	}
	if res.Matched != 1 {
		t.Fatalf("matched = %d, want 1 (res=%+v)", res.Matched, res)
	}
}