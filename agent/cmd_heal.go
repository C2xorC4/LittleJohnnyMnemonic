package main

import (
	"flag"
	"fmt"
	"os"
)

// cmdHeal forces a load-and-write of every memory in the vault. This runs
// all entries through the parser's self-healing paths (link deduplication,
// explicit-decay-rate preservation, etc.) and flushes the healed version
// back to disk. Used after a parser fix to repair files corrupted by the
// previous buggy load-write cycle.
//
// This is a maintenance command. Safe to run anytime but not part of
// normal memory operations — it should not appear in the default workflow.
func cmdHeal(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("heal", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Report changes without writing")
	fs.Parse(args)

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Heal Pass (%d memories) ===\n\n", len(memories))

	rewritten := 0
	errors := 0

	for _, m := range memories {
		if *dryRun {
			rewritten++
			continue
		}
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "[!] %s: %v\n", m.FileName, err)
			errors++
			continue
		}
		rewritten++
	}

	fmt.Printf("Rewritten: %d | Errors: %d\n", rewritten, errors)
	if *dryRun {
		fmt.Println("[DRY RUN] No changes written.")
	}
}
