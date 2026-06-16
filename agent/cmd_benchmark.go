package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func cmdBenchmark(vaultRoot string, args []string) {
	if len(args) == 0 {
		printBenchmarkUsage()
		os.Exit(1)
	}
	sub := args[0]
	subArgs := args[1:]
	switch sub {
	case "validate":
		benchmarkValidate(subArgs)
	case "retrieve-check":
		benchmarkRetrieveCheck(subArgs)
	case "init-run":
		benchmarkInitRun(subArgs)
	case "parse-transcript":
		benchmarkParseTranscript(subArgs)
	case "grade":
		benchmarkGrade(subArgs)
	case "list":
		benchmarkList(subArgs)
	case "-h", "--help", "help":
		printBenchmarkUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown benchmark subcommand: %s\n", sub)
		printBenchmarkUsage()
		os.Exit(1)
	}
}

func printBenchmarkUsage() {
	fmt.Print(`jm benchmark — LJM comparative evaluation harness

Usage:
  jm benchmark <subcommand> [flags]

Subcommands:
  validate           Check fixture vault, repo, and task definitions
  retrieve-check     Verify associate() loads expected memories per task
  list               Print task catalog
  init-run           Scaffold a manual run directory (prompts + metadata)
  parse-transcript   Extract turn timing and tool counts from a transcript
  grade              Score an answer file against task ground truth

Environment:
  JM_BENCHMARK_ROOT   Path to benchmarks/ (default: <vault>/benchmarks)

Examples:
  jm benchmark validate
  jm benchmark retrieve-check --json
  jm benchmark init-run --host grok --arm grok-ljm-on
  jm benchmark parse-transcript --transcript runs/.../transcript.txt
  jm benchmark grade --task T01 --answer-file runs/.../T01/answer.txt
`)
}

func benchmarkRootFromFlag(fs *flag.FlagSet) string {
	root := defaultBenchmarkRoot()
	fs.StringVar(&root, "root", root, "Benchmark root directory")
	return root
}

func benchmarkValidate(args []string) {
	fs := flag.NewFlagSet("benchmark validate", flag.ExitOnError)
	root := benchmarkRootFromFlag(fs)
	fs.Parse(args)

	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(1)
	}
	if err := validateBenchmarkFixture(root, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(1)
	}
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "validate: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("benchmark validate OK — %d tasks, vault=%s\n", len(tasks), benchmarkVaultRoot(root, manifest))
}

func benchmarkRetrieveCheck(args []string) {
	fs := flag.NewFlagSet("benchmark retrieve-check", flag.ExitOnError)
	root := benchmarkRootFromFlag(fs)
	taskFilter := fs.String("task", "", "Run a single task id (e.g. T01)")
	asJSON := fs.Bool("json", false, "Emit JSON array")
	fs.Parse(args)

	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retrieve-check: %v\n", err)
		os.Exit(1)
	}
	vault := benchmarkVaultRoot(root, manifest)
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "retrieve-check: %v\n", err)
		os.Exit(1)
	}

	var results []BenchmarkRetrieveResult
	failures := 0
	for _, task := range tasks {
		if *taskFilter != "" && task.ID != *taskFilter {
			continue
		}
		assoc, _, _, err := AssociateMemories(vault, task.Prompt, AssociateOpts{
			Limit:        15,
			Threshold:    0.2,
			UpdateAccess: false,
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "retrieve-check %s: %v\n", task.ID, err)
			os.Exit(1)
		}
		keys := make([]string, 0, len(assoc))
		topScore := 0.0
		for _, a := range assoc {
			k := normalizeKey(a.Memory)
			keys = append(keys, k)
			if a.Score > topScore {
				topScore = a.Score
			}
		}
		res := checkBenchmarkRetrieval(task, keys)
		res.TopScore = topScore
		results = append(results, res)

		ok := res.ExpectedHit || res.AcceptableOnly
		if len(task.ExpectedMemoryKeys) == 0 {
			ok = true
		}
		// Forbidden keys may co-load; fail only when expected memory missed.
		if len(res.ForbiddenLoaded) > 0 && !res.ExpectedHit && !res.AcceptableOnly {
			ok = false
		}
		if !ok {
			failures++
		}
		if !*asJSON {
			status := "PASS"
			if !ok {
				status = "FAIL"
			}
			fmt.Printf("%s %s expected_hit=%v forbidden=%v top=%.3f loaded=%v\n",
				status, task.ID, res.ExpectedHit, res.ForbiddenLoaded, topScore, keys)
		}
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(results)
	}

	if failures > 0 {
		os.Exit(1)
	}
}

func benchmarkList(args []string) {
	fs := flag.NewFlagSet("benchmark list", flag.ExitOnError)
	root := benchmarkRootFromFlag(fs)
	fs.Parse(args)

	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list: %v\n", err)
		os.Exit(1)
	}
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list: %v\n", err)
		os.Exit(1)
	}
	for _, t := range tasks {
		fmt.Printf("%s tier=%d policy=%s — %s\n", t.ID, t.Tier, t.ToolPolicy, t.Title)
	}
	_ = manifest
}

func benchmarkInitRun(args []string) {
	fs := flag.NewFlagSet("benchmark init-run", flag.ExitOnError)
	root := benchmarkRootFromFlag(fs)
	host := fs.String("host", "grok", "Host label: grok | claude")
	arm := fs.String("arm", "", "Experimental arm (required)")
	toolPolicy := fs.String("tool-policy", "", "Optional tool policy override")
	notes := fs.String("notes", "", "Operator notes")
	fs.Parse(args)

	if *arm == "" {
		fmt.Fprintln(os.Stderr, "init-run: --arm is required")
		os.Exit(1)
	}

	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init-run: %v\n", err)
		os.Exit(1)
	}
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init-run: %v\n", err)
		os.Exit(1)
	}

	runID := fmt.Sprintf("%s_%s_%s", time.Now().UTC().Format("20060102T150405Z"), *host, *arm)
	runDir := filepath.Join(root, "runs", runID)
	if err := os.MkdirAll(runDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "init-run: %v\n", err)
		os.Exit(1)
	}

	meta := BenchmarkRunMeta{
		RunID:     runID,
		StartedAt: time.Now().UTC(),
		Host:      *host,
		Arm:       *arm,
		ToolPolicy: *toolPolicy,
		VaultRoot: benchmarkVaultRoot(root, manifest),
		RepoRoot:  benchmarkRepoRoot(root, manifest),
		Notes:     *notes,
	}
	metaBytes, _ := json.MarshalIndent(meta, "", "  ")
	if err := os.WriteFile(filepath.Join(runDir, "run.json"), metaBytes, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "init-run: %v\n", err)
		os.Exit(1)
	}

	for _, task := range tasks {
		taskDir := filepath.Join(runDir, task.ID)
		if err := os.MkdirAll(taskDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "init-run: %v\n", err)
			os.Exit(1)
		}
		_ = os.WriteFile(filepath.Join(taskDir, "prompt.txt"), []byte(task.Prompt), 0644)
		taskCopy, _ := json.MarshalIndent(task, "", "  ")
		_ = os.WriteFile(filepath.Join(taskDir, "task.json"), taskCopy, 0644)
		timingTemplate := fmt.Sprintf(`{
  "task_id": %q,
  "host": %q,
  "arm": %q,
  "model": "",
  "started_at": "",
  "finished_at": "",
  "wall_clock_s": 0,
  "turn_completed_s": 0,
  "turn_completed_source": "",
  "tool_policy": %q,
  "web_search_used": null,
  "memory_paths_cited": [],
  "operator_notes": ""
}
`, task.ID, *host, *arm, task.ToolPolicy)
		_ = os.WriteFile(filepath.Join(taskDir, "timing.json"), []byte(timingTemplate), 0644)
		readme := fmt.Sprintf(`# %s — %s

## Manual execution

1. Set JM_VAULT_ROOT=%s for LJM-on arms (omit for LJM-off).
2. Open cwd: %s
3. Tool policy: %s
4. Paste prompt.txt into a fresh session (read-only — no git push, deploy, or file mutations).
5. Save the final assistant answer to answer.txt.
6. Fill timing.json (see benchmarks/RUNBOOK.md and schema/timing.example.json).
7. Save Grok updates.jsonl path to transcript.path (one line).
8. Grade: jm benchmark grade --root %s --task %s --answer-file %s

## Timing

Grok: copy "Turn completed in ..." into timing.json → turn_completed_s, or parse updates.jsonl:
  jm benchmark parse-transcript --transcript <path> --out %s/%s/transcript_metrics.json

Full procedure: benchmarks/RUNBOOK.md
`, task.ID, task.Title, meta.VaultRoot, filepath.Join(meta.RepoRoot), task.ToolPolicy,
			root, task.ID, filepath.Join(taskDir, "answer.txt"),
			runDir, task.ID)
		_ = os.WriteFile(filepath.Join(taskDir, "README.md"), []byte(readme), 0644)
	}

	fmt.Printf("init-run: created %s (%d tasks)\n", runDir, len(tasks))
	fmt.Printf("  JM_VAULT_ROOT=%s  (LJM-on arms only)\n", meta.VaultRoot)
}

func benchmarkParseTranscript(args []string) {
	fs := flag.NewFlagSet("benchmark parse-transcript", flag.ExitOnError)
	transcript := fs.String("transcript", "", "Path to transcript file (required)")
	out := fs.String("out", "", "Write JSON metrics to this path")
	fs.Parse(args)

	if *transcript == "" {
		fmt.Fprintln(os.Stderr, "parse-transcript: --transcript is required")
		os.Exit(1)
	}

	metrics, err := parseBenchmarkTranscript(*transcript)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse-transcript: %v\n", err)
		os.Exit(1)
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(metrics)

	if *out != "" {
		data, _ := json.MarshalIndent(metrics, "", "  ")
		if err := os.WriteFile(*out, data, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "parse-transcript: write out: %v\n", err)
			os.Exit(1)
		}
	}
}

func benchmarkGrade(args []string) {
	fs := flag.NewFlagSet("benchmark grade", flag.ExitOnError)
	root := benchmarkRootFromFlag(fs)
	taskID := fs.String("task", "", "Task id (required)")
	answer := fs.String("answer", "", "Answer text")
	answerFile := fs.String("answer-file", "", "Path to answer file")
	out := fs.String("out", "", "Write grade JSON to path")
	fs.Parse(args)

	if *taskID == "" {
		fmt.Fprintln(os.Stderr, "grade: --task is required")
		os.Exit(1)
	}

	manifest, err := loadBenchmarkManifest(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grade: %v\n", err)
		os.Exit(1)
	}
	tasks, err := loadBenchmarkTasks(root, manifest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "grade: %v\n", err)
		os.Exit(1)
	}
	var task *BenchmarkTask
	for i := range tasks {
		if tasks[i].ID == *taskID {
			task = &tasks[i]
			break
		}
	}
	if task == nil {
		fmt.Fprintf(os.Stderr, "grade: unknown task %s\n", *taskID)
		os.Exit(1)
	}

	answerText := *answer
	if *answerFile != "" {
		data, err := os.ReadFile(*answerFile)
		if err != nil {
			fmt.Fprintf(os.Stderr, "grade: %v\n", err)
			os.Exit(1)
		}
		answerText = string(data)
	}
	if strings.TrimSpace(answerText) == "" {
		fmt.Fprintln(os.Stderr, "grade: provide --answer or --answer-file")
		os.Exit(1)
	}

	result := gradeBenchmarkAnswer(*task, answerText)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	_ = enc.Encode(result)

	if *out != "" {
		data, _ := json.MarshalIndent(result, "", "  ")
		_ = os.WriteFile(*out, data, 0644)
	}
	if !result.Passed {
		os.Exit(1)
	}
}