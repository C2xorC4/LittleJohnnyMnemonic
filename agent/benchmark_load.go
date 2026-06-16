package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func defaultBenchmarkRoot() string {
	root := os.Getenv("JM_BENCHMARK_ROOT")
	if root != "" {
		return root
	}
	vault := findVaultRoot()
	return filepath.Join(vault, "benchmarks")
}

func loadBenchmarkManifest(root string) (*BenchmarkManifest, error) {
	data, err := os.ReadFile(filepath.Join(root, "manifest.json"))
	if err != nil {
		return nil, fmt.Errorf("read manifest: %w", err)
	}
	var m BenchmarkManifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse manifest: %w", err)
	}
	if m.FixtureVault == "" {
		m.FixtureVault = "fixture-vault"
	}
	if m.FixtureRepo == "" {
		m.FixtureRepo = "fixture-repo"
	}
	if m.TasksDir == "" {
		m.TasksDir = "tasks"
	}
	return &m, nil
}

func loadBenchmarkTasks(root string, manifest *BenchmarkManifest) ([]BenchmarkTask, error) {
	tasksDir := filepath.Join(root, manifest.TasksDir)
	entries, err := os.ReadDir(tasksDir)
	if err != nil {
		return nil, fmt.Errorf("read tasks dir: %w", err)
	}
	var tasks []BenchmarkTask
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(tasksDir, e.Name()))
		if err != nil {
			return nil, fmt.Errorf("read task %s: %w", e.Name(), err)
		}
		var t BenchmarkTask
		if err := json.Unmarshal(data, &t); err != nil {
			return nil, fmt.Errorf("parse task %s: %w", e.Name(), err)
		}
		if t.ID == "" || t.Prompt == "" {
			return nil, fmt.Errorf("task %s missing id or prompt", e.Name())
		}
		if t.Grading.PassThreshold <= 0 {
			t.Grading.PassThreshold = 1.0
		}
		tasks = append(tasks, t)
	}
	if len(tasks) == 0 {
		return nil, fmt.Errorf("no tasks found in %s", tasksDir)
	}
	sort.Slice(tasks, func(i, j int) bool { return tasks[i].ID < tasks[j].ID })
	return tasks, nil
}

func validateBenchmarkFixture(root string, manifest *BenchmarkManifest) error {
	vault := filepath.Join(root, manifest.FixtureVault)
	repo := filepath.Join(root, manifest.FixtureRepo)
	for _, p := range []string{vault, repo} {
		if _, err := os.Stat(p); err != nil {
			return fmt.Errorf("missing fixture path %s: %w", p, err)
		}
	}
	claude := filepath.Join(vault, "CLAUDE.md")
	system := filepath.Join(vault, "System")
	if _, err := os.Stat(claude); err != nil {
		return fmt.Errorf("fixture vault missing CLAUDE.md: %w", err)
	}
	if _, err := os.Stat(system); err != nil {
		return fmt.Errorf("fixture vault missing System/: %w", err)
	}
	memories, err := LoadAllMemories(vault)
	if err != nil {
		return fmt.Errorf("load fixture memories: %w", err)
	}
	if len(memories) < 5 {
		return fmt.Errorf("fixture vault has only %d memories; need at least 5", len(memories))
	}
	return nil
}

func benchmarkVaultRoot(root string, manifest *BenchmarkManifest) string {
	return filepath.Join(root, manifest.FixtureVault)
}

func benchmarkRepoRoot(root string, manifest *BenchmarkManifest) string {
	return filepath.Join(root, manifest.FixtureRepo)
}