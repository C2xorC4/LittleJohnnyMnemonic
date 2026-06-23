package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func cmdBackfillEdgeUsage(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("backfill-edge-usage", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Preview reinforcement without writing edge_usage.jsonl")
	fs.Parse(args)

	cfg := LoadConfig(vaultRoot)
	if !cfg.AdaptiveEdgeWeightingEnabled {
		fmt.Fprintln(os.Stderr, "[!] adaptive_edge_weighting_enabled is false — enable in System/Config.md first")
		os.Exit(1)
	}
	if len(cfg.AdaptiveEdgeScope) == 0 {
		fmt.Fprintln(os.Stderr, "[!] adaptive_edge_scope is empty — nothing to reinforce")
		os.Exit(1)
	}

	cLog, err := LoadCitations(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] load citations: %v\n", err)
		os.Exit(1)
	}

	var eligible, reinforcedEdges, skipped int
	for _, c := range cLog.Citations {
		if !c.Useful || c.SessionID == "" {
			skipped++
			continue
		}
		key := normalizeMemoryKey(c.MemoryKey)
		if key == "" || !strings.HasPrefix(key, "memory/") {
			skipped++
			continue
		}
		eligible++

		if *dryRun {
			session, err := FindRetrievalSession(vaultRoot, c.SessionID)
			if err != nil || session == nil {
				fmt.Printf("− %s session=%s (session not found)\n", shortKey(key), c.SessionID[:8])
				continue
			}
			fmt.Printf("• %s session=%s loaded=%d\n", shortKey(key), c.SessionID[:8], len(session.Loaded))
			continue
		}

		edges, err := RecordEdgeUsageFromCitation(vaultRoot, c.SessionID, key, cfg.AdaptiveEdgeScope)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] reinforce %s: %v\n", key, err)
			continue
		}
		if len(edges) > 0 {
			reinforcedEdges += len(edges)
			fmt.Printf("✓ %s → %d edge(s) reinforced (session %s)\n", shortKey(key), len(edges), c.SessionID[:8])
		} else {
			fmt.Printf("− %s session=%s (no learned edges in loaded set)\n", shortKey(key), c.SessionID[:8])
		}
	}

	prefix := ""
	if *dryRun {
		prefix = "dry-run: "
	}
	fmt.Printf("\n%seligible=%d reinforced_edges=%d skipped=%d scope=%v\n",
		prefix, eligible, reinforcedEdges, skipped, cfg.AdaptiveEdgeScope)
}