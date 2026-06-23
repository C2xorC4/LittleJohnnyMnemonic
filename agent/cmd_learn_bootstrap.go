package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
)

func cmdLearnEdges(vaultRoot string, args []string) {
	if len(args) > 0 {
		switch args[0] {
		case "propose":
			if err := printBootstrapPropose(vaultRoot); err != nil {
				fmt.Fprintf(os.Stderr, "[!] learn-edges propose: %v\n", err)
				os.Exit(1)
			}
			return
		case "apply-bootstrap":
			cmdLearnEdgesApplyBootstrap(vaultRoot, args[1:])
			return
		case "-h", "--help", "help":
			printLearnEdgesUsage()
			return
		}
	}
	cmdLearnEdgesCoactivation(vaultRoot, args)
}

func printLearnEdgesUsage() {
	fmt.Print(`jm learn-edges — co-activation edge learning and pilot bootstrap

Usage:
  jm learn-edges [flags]                 Scan co-activation log for new edge candidates
  jm learn-edges propose                 Print operator-reviewed bootstrap slate
  jm learn-edges apply-bootstrap [flags] Apply approved bootstrap learned edges

Bootstrap flags:
  --ids <n,n,...>   Required. Bootstrap IDs from propose output (e.g. 1,2,3,4,5,6)
  --apply           Write edges (default: dry-run)
  --dry-run         Explicit dry-run (default when --apply omitted)

Co-activation flags:
  --threshold <n>   Minimum co-activations (default: 3)
  --apply           Write discovered edges (default: dry-run)
`)
}

func cmdLearnEdgesApplyBootstrap(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("learn-edges apply-bootstrap", flag.ExitOnError)
	idsFlag := fs.String("ids", "", "Comma-separated bootstrap IDs to apply (required)")
	apply := fs.Bool("apply", false, "Write learned edges to memory files")
	dryRun := fs.Bool("dry-run", false, "Preview without writing")
	fs.Parse(args)

	if *idsFlag == "" {
		fmt.Fprintln(os.Stderr, "[!] --ids is required (e.g. --ids 1,2,3,4,5,6)")
		os.Exit(1)
	}
	ids, err := parseBootstrapIDs(*idsFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] %v\n", err)
		os.Exit(1)
	}

	preview := !*apply || *dryRun
	mode := "DRY-RUN"
	if !preview {
		mode = "APPLY"
	}
	fmt.Printf("# Bootstrap learned edges (%s)\n\n", mode)

	results, err := applyBootstrapLearnedEdges(vaultRoot, ids, preview)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] apply-bootstrap: %v\n", err)
		os.Exit(1)
	}

	var applied, skipped int
	for _, r := range results {
		switch {
		case r.Applied && preview:
			fmt.Printf("%d. ✓ would apply  %s ↔ %s\n", r.ID, r.MemoryA, r.MemoryB)
			applied++
		case r.Applied:
			fmt.Printf("%d. ✓ applied    %s ↔ %s\n", r.ID, r.MemoryA, r.MemoryB)
			applied++
		default:
			fmt.Printf("%d. − skipped   %s ↔ %s (%s)\n", r.ID, r.MemoryA, r.MemoryB, r.Skipped)
			skipped++
		}
	}
	fmt.Printf("\nSummary: %d applied, %d skipped\n", applied, skipped)
	if preview && applied > 0 {
		fmt.Println("Re-run with --apply to write.")
	}
}

func cmdLearnEdgesCoactivation(vaultRoot string, args []string) {
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

		top := coLog.Pairs
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
			memA, okA := graph.Index[c.MemoryA]
			memB, okB := graph.Index[c.MemoryB]

			if !okA || !okB {
				fmt.Printf("   ⚠ Could not resolve both memories — skipping\n")
				continue
			}

			if err := writeLearnedEdgePair(memA, memB); err != nil {
				fmt.Fprintf(os.Stderr, "   ⚠ %v\n", err)
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