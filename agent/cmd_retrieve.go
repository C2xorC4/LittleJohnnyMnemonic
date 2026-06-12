package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdRetrieve(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("retrieve", flag.ExitOnError)
	tags := fs.String("tags", "", "Comma-separated context tags")
	intent := fs.String("intent", "", "Query intent: guidance, who, what, where")
	format := fs.String("format", "full", "Output format: full, summary, json")
	noUpdate := fs.Bool("no-update", false, "Don't update access metadata")
	noSession := fs.Bool("no-session", false, "Don't write a retrieval session log entry even if enabled in Config")
	fs.Parse(args)

	cfg := LoadConfig(vaultRoot)
	now := time.Now()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	if len(memories) == 0 {
		fmt.Println("No memories to retrieve.")
		return
	}

	var contextTags []string
	if *tags != "" {
		for _, t := range strings.Split(*tags, ",") {
			contextTags = append(contextTags, strings.TrimSpace(t))
		}
	}

	// Score all memories
	scored := ScoreAllMemories(memories, contextTags, *intent, cfg, now)

	// Build graph and apply spreading activation. Use the vault-aware
	// constructor so adaptive edge weighting (citation-driven usage
	// multiplier) is layered in when enabled.
	graph := BuildGraphFromVault(memories, cfg, vaultRoot)
	scored = ApplySpreadingActivation(scored, graph, cfg)

	// Filter by threshold and cap
	var retrieved []ScoredMemory
	for _, s := range scored {
		if s.Total >= cfg.RetrievalThreshold {
			retrieved = append(retrieved, s)
		}
		if len(retrieved) >= cfg.MaxMemoriesLoaded {
			break
		}
	}

	if len(retrieved) == 0 {
		fmt.Println("No memories above retrieval threshold.")
		return
	}

	// Update access metadata unless --no-update
	if !*noUpdate {
		keys := make([]string, 0, len(retrieved))
		for _, s := range retrieved {
			keys = append(keys, normalizeKey(s.Memory))
			// Un-archiving is a genuine content change → still writes the file.
			if UnarchiveOnAccess(s.Memory) {
				fmt.Fprintf(os.Stderr, "[jm retrieve] resurrected from archive: %s\n", s.Memory.FileName)
				if err := WriteMemoryEntry(s.Memory); err != nil {
					fmt.Fprintf(os.Stderr, "[!] Failed to update %s: %v\n", s.Memory.FileName, err)
				}
			}
		}
		if err := recordAccessBatch(vaultRoot, keys, now); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to record access: %v\n", err)
		}
	}

	// Retrieval session logging — required substrate for adaptive edge
	// weighting reinforcement. When enabled, persist the loaded-together
	// set under a session_id that subsequent `jm associate --cite
	// --session <id>` calls can reference.
	sessionID := ""
	if cfg.RetrievalSessionLogEnabled && !*noSession && len(retrieved) > 0 {
		sessionID = GenerateSessionID()
		loaded := make([]string, 0, len(retrieved))
		for _, s := range retrieved {
			loaded = append(loaded, MemoryKey(s.Memory))
		}
		session := RetrievalSession{
			SessionID:    sessionID,
			Timestamp:    now,
			Loaded:       loaded,
			QueryContext: *intent,
			QueryTags:    contextTags,
		}
		if err := AppendRetrievalSession(vaultRoot, session); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to write retrieval session: %v\n", err)
			sessionID = "" // suppress session_id output if persistence failed
		} else {
			// Opportunistic pruning — keeps log bounded without a separate command.
			if cfg.RetrievalSessionLogRetentionDays > 0 {
				_, _ = PruneRetrievalSessions(vaultRoot, cfg.RetrievalSessionLogRetentionDays)
			}
		}
	}

	// Output
	switch *format {
	case "summary":
		outputSummary(retrieved)
	case "json":
		outputJSON(retrieved)
	default:
		outputFull(retrieved, graph)
	}

	// Surface session_id at the very end so it's easy to grab from
	// terminal output for a follow-on `jm associate --cite --session <id>`.
	if sessionID != "" {
		fmt.Printf("\nsession_id: %s\n", sessionID)
	}
}

// MemoryKey returns the canonical "memory/type/filename" identifier
// used in retrieval_sessions.jsonl and edge_usage.jsonl. Lower-cased
// to match the form produced by graph.normalizeKey() so that the
// adaptive-edge-weighting lookup in BuildGraph can match session-loaded
// keys against graph node keys without further normalisation.
func MemoryKey(m *MemoryEntry) string {
	if m.FilePath == "" {
		return strings.ToLower(m.FileName)
	}
	p := m.FilePath
	idx := indexOfMemoryRoot(p)
	if idx < 0 {
		return strings.ToLower(m.FileName)
	}
	rel := p[idx:]
	// Normalize separators and strip .md
	rel = normalizeSlash(rel)
	if len(rel) > 3 && rel[len(rel)-3:] == ".md" {
		rel = rel[:len(rel)-3]
	}
	return strings.ToLower(rel)
}

func indexOfMemoryRoot(p string) int {
	// Look for "Memory/" or "Memory\" as the path root marker.
	for _, marker := range []string{"Memory/", "Memory\\"} {
		if i := lastIndex(p, marker); i >= 0 {
			return i
		}
	}
	return -1
}

func lastIndex(s, sub string) int {
	for i := len(s) - len(sub); i >= 0; i-- {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func normalizeSlash(p string) string {
	out := make([]byte, 0, len(p))
	for i := 0; i < len(p); i++ {
		if p[i] == '\\' {
			out = append(out, '/')
		} else {
			out = append(out, p[i])
		}
	}
	return string(out)
}

func outputFull(retrieved []ScoredMemory, graph *MemoryGraph) {
	fmt.Printf("# Retrieved Memories (%d)\n\n", len(retrieved))

	for i, s := range retrieved {
		m := s.Memory
		fmt.Printf("## %d. %s (score: %.3f)\n", i+1, m.Title, s.Total)
		fmt.Printf("**Type:** %s | **Confidence:** %.2f | **Decay:** %.2f\n", m.Type, m.Confidence, m.DecayRate)

		if m.TrainingOverride {
			fmt.Printf("**⚠ Training Override:** %s\n", m.OverrideContext)
		}
		if m.Facet != "" {
			fmt.Printf("**Facet:** %s | **Observations:** %d\n", m.Facet, m.ObservationCount)
		}
		if len(m.Tags) > 0 {
			fmt.Printf("**Tags:** %s\n", strings.Join(m.Tags, ", "))
		}

		// Show score breakdown
		fmt.Printf("**Score breakdown:** activation=%.3f × relevance=%.3f × confidence=%.2f + surprise=%.3f",
			s.Activation, s.Relevance, s.Confidence, s.Surprise)
		if s.Boost > 0 {
			fmt.Printf(" + boost=%.3f", s.Boost)
		}
		fmt.Println()

		// Show links
		if len(m.Links) > 0 {
			fmt.Print("**Links:** ")
			var linkStrs []string
			for _, l := range m.Links {
				linkStrs = append(linkStrs, fmt.Sprintf("%s →(%s)", l.Target, l.Relationship))
			}
			fmt.Println(strings.Join(linkStrs, ", "))
		}

		fmt.Printf("\n%s\n\n---\n\n", m.Body)
	}
}

func outputSummary(retrieved []ScoredMemory) {
	for i, s := range retrieved {
		marker := ""
		if s.Memory.TrainingOverride {
			marker = " [OVERRIDE]"
		}
		fmt.Printf("%d. [%.3f] %s (%s)%s\n",
			i+1, s.Total, s.Memory.Title, s.Memory.Type, marker)
	}
}

func outputJSON(retrieved []ScoredMemory) {
	fmt.Println("[")
	for i, s := range retrieved {
		m := s.Memory
		comma := ","
		if i == len(retrieved)-1 {
			comma = ""
		}
		fmt.Printf(`  {"title": "%s", "type": "%s", "score": %.4f, "confidence": %.2f, `+
			`"activation": %.4f, "relevance": %.4f, "surprise": %.4f, "boost": %.4f, `+
			`"training_override": %t, "tags": ["%s"], "body": "%s"}%s`+"\n",
			escapeJSON(m.Title), m.Type, s.Total, m.Confidence,
			s.Activation, s.Relevance, s.Surprise, s.Boost,
			m.TrainingOverride, strings.Join(m.Tags, "\", \""),
			escapeJSON(truncate(m.Body, 200)), comma)
	}
	fmt.Println("]")
}

func escapeJSON(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	s = strings.ReplaceAll(s, "\t", `\t`)
	return s
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
