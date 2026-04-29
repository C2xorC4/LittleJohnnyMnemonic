package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// cmdMigrateArchive is a one-off maintenance command that reorganizes
// the flat Archive/ layout (legacy) into per-type subdirectories mirroring
// the Memory/ structure. Files already in subdirs are left alone.
//
// Safe to run repeatedly — idempotent.
func cmdMigrateArchive(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("migrate-archive", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Show what would be moved without actually moving")
	fs.Parse(args)

	archiveDir := filepath.Join(vaultRoot, "Archive")
	entries, err := os.ReadDir(archiveDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to read %s: %v\n", archiveDir, err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("=== Archive Migration (DRY RUN) ===\n\n")
	} else {
		fmt.Printf("=== Archive Migration ===\n\n")
	}

	type move struct {
		src  string
		dst  string
		typ  string
	}
	var moves []move
	skipped := 0
	errors := 0

	for _, e := range entries {
		if e.IsDir() {
			continue // already in subdir structure
		}
		if !strings.HasSuffix(e.Name(), ".md") {
			continue
		}

		srcPath := filepath.Join(archiveDir, e.Name())

		// Parse to determine type
		entry, err := ParseMemoryEntry(srcPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [!] Parse error for %s: %v\n", e.Name(), err)
			errors++
			continue
		}

		if entry.Type == "" {
			fmt.Fprintf(os.Stderr, "  [!] %s has no type — skipping\n", e.Name())
			skipped++
			continue
		}

		typeDir := archiveTypeDir(string(entry.Type))
		dstPath := filepath.Join(archiveDir, typeDir, e.Name())
		moves = append(moves, move{src: srcPath, dst: dstPath, typ: typeDir})
	}

	if len(moves) == 0 {
		fmt.Println("No flat archive files to migrate.")
		return
	}

	ok := 0
	for _, mv := range moves {
		relSrc, _ := filepath.Rel(vaultRoot, mv.src)
		relDst, _ := filepath.Rel(vaultRoot, mv.dst)
		fmt.Printf("  %s  →  %s\n", relSrc, relDst)

		if *dryRun {
			ok++
			continue
		}

		dstDir := filepath.Dir(mv.dst)
		if err := os.MkdirAll(dstDir, 0755); err != nil {
			fmt.Fprintf(os.Stderr, "    [!] mkdir %s: %v\n", dstDir, err)
			errors++
			continue
		}

		if err := os.Rename(mv.src, mv.dst); err != nil {
			fmt.Fprintf(os.Stderr, "    [!] rename: %v\n", err)
			errors++
			continue
		}
		ok++
	}

	fmt.Println()
	if *dryRun {
		fmt.Printf("[DRY RUN] Would move: %d | Skipped: %d | Errors: %d\n", ok, skipped, errors)
	} else {
		fmt.Printf("Moved: %d | Skipped: %d | Errors: %d\n", ok, skipped, errors)
	}
}

// archiveTypeDir returns the canonical subdirectory name under Archive/
// for a given memory type string. Mirrors the Memory/ subdir names.
func archiveTypeDir(memoryType string) string {
	switch memoryType {
	case "user":
		return "User"
	case "feedback":
		return "Feedback"
	case "project":
		return "Project"
	case "reference":
		return "Reference"
	case "semantic":
		return "Semantic"
	case "episodic":
		return "Episodic"
	case "knowledge":
		return "Knowledge"
	default:
		return "Misc"
	}
}
