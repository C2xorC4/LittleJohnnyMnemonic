package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func cmdLearnEdges(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("learn-edges", flag.ExitOnError)
	threshold := fs.Int("threshold", 3, "Minimum co-activations before suggesting an edge")
	apply := fs.Bool("apply", false, "Actually write learned edges to memory files (default: dry-run)")
	fs.Parse(args)

	cfg := DefaultConfig()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	graph := BuildGraph(memories, cfg)

	coLog, err := LoadCoactivation(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load coactivation log: %v\n", err)
		os.Exit(1)
	}

	if len(coLog.Pairs) == 0 {
		fmt.Println("No co-activation data yet. Run `jm associate` to build up co-activation history.")
		return
	}

	candidates := FindLearnedEdgeCandidates(coLog, graph, *threshold)

	if len(candidates) == 0 {
		fmt.Printf("No edge candidates above threshold (%d co-activations).\n", *threshold)
		fmt.Printf("Total co-activation pairs tracked: %d\n", len(coLog.Pairs))

		// Show top pairs anyway
		top := coLog.Pairs
		// Sort by count descending
		for i := 0; i < len(top); i++ {
			for j := i + 1; j < len(top); j++ {
				if top[j].Count > top[i].Count {
					top[i], top[j] = top[j], top[i]
				}
			}
		}
		if len(top) > 5 {
			top = top[:5]
		}
		if len(top) > 0 {
			fmt.Println("\nTop co-activation pairs (may already have edges):")
			for _, p := range top {
				existing := ""
				if hasEdge(graph, p.MemoryA, p.MemoryB) {
					existing = " [linked]"
				}
				fmt.Printf("  %d× %s ↔ %s%s\n", p.Count, shortKey(p.MemoryA), shortKey(p.MemoryB), existing)
			}
		}
		return
	}

	fmt.Printf("# Learned Edge Candidates (%d)\n\n", len(candidates))
	fmt.Printf("Threshold: %d co-activations | Mode: %s\n\n",
		*threshold, map[bool]string{true: "APPLY", false: "dry-run"}[*apply])

	for i, c := range candidates {
		fmt.Printf("%d. **%s** ↔ **%s** (%d co-activations)\n",
			i+1, shortKey(c.MemoryA), shortKey(c.MemoryB), c.Count)
		if len(c.Contexts) > 0 {
			fmt.Printf("   Contexts: %s\n", strings.Join(c.Contexts, " | "))
		}

		if *apply {
			// Add a related-to link to the first memory's file
			memA, okA := graph.Index[c.MemoryA]
			memB, okB := graph.Index[c.MemoryB]

			if !okA || !okB {
				fmt.Printf("   ⚠ Could not resolve both memories — skipping\n")
				continue
			}

			// Add link to A pointing to B. Store the bracketless canonical path:
			// WriteMemoryEntry wraps Target in [[ ]], so passing an already-
			// bracketed value (the old toWikiLink) produced [[[[...]]]] on disk.
			memA.Links = append(memA.Links, Link{
				Target:       relMemoryPath(memB),
				Relationship: "learned",
			})
			if err := WriteMemoryEntry(memA); err != nil {
				fmt.Fprintf(os.Stderr, "   ⚠ Failed to write %s: %v\n", memA.FileName, err)
				continue
			}

			// Add reverse link to B pointing to A.
			memB.Links = append(memB.Links, Link{
				Target:       relMemoryPath(memA),
				Relationship: "learned",
			})
			if err := WriteMemoryEntry(memB); err != nil {
				fmt.Fprintf(os.Stderr, "   ⚠ Failed to write %s: %v\n", memB.FileName, err)
				continue
			}

			fmt.Printf("   ✓ Edge created: learned (bidirectional)\n")
		}
		fmt.Println()
	}

	if !*apply && len(candidates) > 0 {
		fmt.Println("Run with --apply to write these edges to memory files.")
	}
}

// shortKey strips the Memory/ prefix for display.
func shortKey(key string) string {
	key = strings.TrimPrefix(key, "memory/")
	key = strings.TrimPrefix(key, "archive/")
	return key
}

