package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func writeTestMemory(t *testing.T, dir, relPath, frontmatter, body string) {
	t.Helper()
	path := filepath.Join(dir, relPath)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\n" + frontmatter + "\n---\n\n" + body
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestAssociateMemories_FiltersGenericOnlyMatches(t *testing.T) {
	vault := t.TempDir()
	now := time.Now().Format(time.RFC3339)

	writeTestMemory(t, vault, "Memory/Project/ljm.md", `type: project
title: LittleJohnnyMnemonic
created: `+now+`
last_accessed: `+now+`
access_count: 100
decay_rate: 0.2
confidence: 0.95
tags: [project, ljm, adaptive-edge]
`, `edge_usage.jsonl adaptive_edge_scope learn-edge PR3 gate retrieval_sessions.jsonl`)

	writeTestMemory(t, vault, "Memory/Project/mimic.md", `type: project
title: Mimic
created: `+now+`
last_accessed: `+now+`
access_count: 800
decay_rate: 0.2
confidence: 0.9
tags: [project, mimic]
`, `apply vet set validation config memory filter process — OS deception tooling`)

	writeTestMemory(t, vault, "Memory/Semantic/apt_cc.md", `type: semantic
title: Competitive control analogues
created: `+now+`
last_accessed: `+now+`
access_count: 50000
decay_rate: 0.2
confidence: 0.85
tags: [semantic, competitive-control]
`, `methodology address show block apply small edge beyond validation change project memory without`)

	query := `What is the recommended methodology for addressing edge_usage.jsonl must show non-zero reinforcement — blocked upstream: Either apply a small, vetted set of learned edges (jm learn-edges --apply on a filtered subset), or Temporarily widen adaptive_edge_scope beyond learned for pilot validation (config change; not recommended in project memory without maturity criteria), or Accept that PR3's learned-edge creation is a prerequisite for meaningful reinforcement and adjust the gate accordingly.`

	opts := AssociateOpts{
		Limit:        8,
		Threshold:    0.2,
		UpdateAccess: false,
		Enrichment:   false,
	}
	results, _, _, err := AssociateMemories(vault, query, opts)
	if err != nil {
		t.Fatal(err)
	}

	loaded := make(map[string]bool)
	for _, r := range results {
		loaded[strings.ToLower(r.Memory.FileName)] = true
	}
	if !loaded["ljm.md"] {
		t.Errorf("expected LJM project in results, got %d memories", len(results))
	}
	for _, noise := range []string{"mimic.md", "apt_cc.md"} {
		if loaded[noise] {
			t.Errorf("%s should be filtered by operational stopwords + discriminating gate", noise)
		}
	}
}