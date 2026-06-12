package main

import (
	"flag"
	"fmt"
	"os"
)

// cmdMigrateAccess seeds the access sidecar (Metrics/access_base.json) from the
// current frontmatter access_count/last_accessed values. One-time cutover step:
// after this, the sidecar is authoritative and retrieval stops writing .md files.
func cmdMigrateAccess(vaultRoot string, args []string) {
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] load memories: %v\n", err)
		os.Exit(1)
	}
	if err := seedAccessIndex(vaultRoot, memories); err != nil {
		fmt.Fprintf(os.Stderr, "[!] seed access index: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("Seeded access index from %d memories → %s\n", len(memories), accessBasePath)
}

// cmdSyncAccess folds the event log and mirrors the current access values into
// each memory's frontmatter, so Obsidian/Dataview dashboards reflect them. The
// retrieval hot path never writes .md — this is the on-demand (or consolidation-
// time) reflection step. It also compacts the event log.
func cmdSyncAccess(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("sync-access", flag.ExitOnError)
	noFold := fs.Bool("no-fold", false, "Do not compact the event log into the base snapshot first")
	fs.Parse(args)

	if !*noFold {
		if err := foldAccessLog(vaultRoot); err != nil {
			fmt.Fprintf(os.Stderr, "[!] fold access log: %v\n", err)
		}
	}

	memories, err := LoadAllMemories(vaultRoot) // merge overlays current access
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] load memories: %v\n", err)
		os.Exit(1)
	}
	written := 0
	for _, m := range memories {
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "[!] write %s: %v\n", m.FileName, err)
			continue
		}
		written++
	}
	fmt.Printf("Mirrored access into frontmatter for %d memories (event log folded into base).\n", written)
}
