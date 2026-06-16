package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func benchmarkRoot(t *testing.T) string {
	t.Helper()
	root := filepath.Join(findVaultRoot(), "benchmarks")
	if _, err := os.Stat(root); err != nil {
		t.Skipf("benchmarks/ not present: %v", err)
	}
	return root
}

func TestBenchmarkValidate(t *testing.T) {
	root := benchmarkRoot(t)
	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	if err := validateBenchmarkFixture(root, manifest); err != nil {
		t.Fatal(err)
	}
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	if len(tasks) < 7 {
		t.Fatalf("expected >= 7 tasks, got %d", len(tasks))
	}
}

func TestBenchmarkRetrieveCheck_AllTasks(t *testing.T) {
	root := benchmarkRoot(t)
	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	vault := benchmarkVaultRoot(root, manifest)
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		t.Fatal(err)
	}

	for _, task := range tasks {
		t.Run(task.ID, func(t *testing.T) {
			assoc, _, _, err := AssociateMemories(vault, task.Prompt, AssociateOpts{
				Limit:        15,
				Threshold:    0.2,
				UpdateAccess: false,
			})
			if err != nil {
				t.Fatal(err)
			}
			keys := make([]string, 0, len(assoc))
			for _, a := range assoc {
				keys = append(keys, normalizeKey(a.Memory))
			}
			res := checkBenchmarkRetrieval(task, keys)
			// Decoys may co-activate; accuracy is graded from answer text.
			if len(task.ExpectedMemoryKeys) == 0 {
				return
			}
			if !res.ExpectedHit && !res.AcceptableOnly {
				t.Fatalf("expected one of %v in loaded %v", task.ExpectedMemoryKeys, keys)
			}
		})
	}
}

func TestGradeBenchmarkAnswer_T01(t *testing.T) {
	root := benchmarkRoot(t)
	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		t.Fatal(err)
	}
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		t.Fatal(err)
	}
	var task BenchmarkTask
	for _, tk := range tasks {
		if tk.ID == "T01" {
			task = tk
			break
		}
	}
	pass := gradeBenchmarkAnswer(task, "Kilcullen uses the fish trap metaphor.")
	if !pass.Passed {
		t.Fatalf("expected pass, got %+v", pass)
	}
	fail := gradeBenchmarkAnswer(task, "cage metaphor")
	if fail.Passed {
		t.Fatal("expected fail for wrong metaphor")
	}
}

func TestParseGrokTurnCompleted(t *testing.T) {
	text := "Here is the answer.\n\nTurn completed in 12.4s\n"
	m, err := parsePlainTranscript(writeTempTranscript(t, text))
	if err != nil {
		t.Fatal(err)
	}
	if m.TurnCount != 1 {
		t.Fatalf("turn count = %d", m.TurnCount)
	}
	if m.TotalDurationSecs < 12.3 || m.TotalDurationSecs > 12.5 {
		t.Fatalf("duration = %f", m.TotalDurationSecs)
	}
}

func TestParseGrokUpdatesToolCalls(t *testing.T) {
	lines := []string{
		`{"params":{"chunk_type":"user_message_chunk","text":"question"}}`,
		`{"params":{"chunk_type":"agent_message_chunk","text":"answer part 1"}}`,
		`{"params":{"chunk_type":"tool_call","tool_name":"WebSearch"}}`,
		`{"params":{"chunk_type":"agent_message_chunk","text":"Turn completed in 3s"}}`,
	}
	path := writeTempJSONL(t, lines)
	m, err := parseGrokUpdatesTranscript(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.TurnCount < 1 {
		t.Fatal("expected at least one turn")
	}
	if m.ToolTotals["WebSearch"] < 1 {
		t.Fatalf("tool totals: %+v", m.ToolTotals)
	}
}

func writeTempTranscript(t *testing.T, content string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "transcript.txt")
	if err := os.WriteFile(f, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func writeTempJSONL(t *testing.T, lines []string) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "updates.jsonl")
	if err := os.WriteFile(f, []byte(strings.Join(lines, "\n")), 0644); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestBenchmarkInitRunMetaShape(t *testing.T) {
	meta := BenchmarkRunMeta{
		RunID:   "test",
		Host:    "grok",
		Arm:     "grok-ljm-on",
	}
	data, err := json.Marshal(meta)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "grok-ljm-on") {
		t.Fatal("marshal failed")
	}
}