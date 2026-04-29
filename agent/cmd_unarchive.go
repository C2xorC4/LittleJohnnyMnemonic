package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdUnarchive is the explicit un-archive command. Complements B' auto-
// resurrection on retrieval-path access by providing a surgical path for
// memories that can't be reached organically (stale activation, never
// queried, etc.).
//
// Two modes:
//
//	jm unarchive --file <path>    # single file, relative or absolute
//	jm unarchive --all             # every soft-archived memory
//
// Both support --dry-run for preview.
//
// Scope: only operates on soft-archived memories (files with archived:
// frontmatter still living in Memory/<type>/). Hard-archived files in
// Archive/ are a separate concern and are NOT touched by this command.
func cmdUnarchive(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("unarchive", flag.ExitOnError)
	file := fs.String("file", "", "Memory file to unarchive (path relative to vault root or absolute)")
	all := fs.Bool("all", false, "Unarchive every soft-archived memory in Memory/")
	dryRun := fs.Bool("dry-run", false, "Report what would be unarchived without writing")
	fs.Parse(args)

	if *file == "" && !*all {
		fmt.Fprintln(os.Stderr, "Usage: jm unarchive (--file <path> | --all) [--dry-run]")
		os.Exit(1)
	}
	if *file != "" && *all {
		fmt.Fprintln(os.Stderr, "Use --file or --all, not both")
		os.Exit(1)
	}

	if *file != "" {
		unarchiveSingle(vaultRoot, *file, *dryRun)
		return
	}
	unarchiveAll(vaultRoot, *dryRun)
}

func unarchiveSingle(vaultRoot, path string, dryRun bool) {
	if !filepath.IsAbs(path) {
		path = filepath.Join(vaultRoot, path)
	}
	entry, err := ParseMemoryEntry(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Parse failed: %v\n", err)
		os.Exit(1)
	}
	relPath, _ := filepath.Rel(vaultRoot, path)

	if entry.Archived == nil {
		fmt.Printf("Already unarchived: %s\n", relPath)
		return
	}

	archivedAt := entry.Archived.Format("2006-01-02 15:04")
	reason := entry.ArchiveReason
	if reason == "" {
		reason = "(no reason)"
	}

	if dryRun {
		fmt.Printf("[DRY RUN] Would unarchive: %s\n", relPath)
		fmt.Printf("          Title: %s\n", entry.Title)
		fmt.Printf("          Archived: %s | Reason: %s\n", archivedAt, reason)
		return
	}

	UnarchiveOnAccess(entry)
	if err := WriteMemoryEntry(entry); err != nil {
		fmt.Fprintf(os.Stderr, "[!] Write failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Unarchived: %s\n", relPath)
	fmt.Printf("  Title: %s\n", entry.Title)
	fmt.Printf("  Was archived: %s | Reason: %s\n", archivedAt, reason)
}

func unarchiveAll(vaultRoot string, dryRun bool) {
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	var archived []*MemoryEntry
	for _, m := range memories {
		if m.Archived != nil {
			archived = append(archived, m)
		}
	}

	if len(archived) == 0 {
		fmt.Println("No soft-archived memories found.")
		return
	}

	if dryRun {
		fmt.Printf("[DRY RUN] %d soft-archived memories would be unarchived:\n\n", len(archived))
	} else {
		fmt.Printf("Unarchiving %d soft-archived memories:\n\n", len(archived))
	}

	ok := 0
	errors := 0
	for _, m := range archived {
		relPath, _ := filepath.Rel(vaultRoot, m.FilePath)
		archivedAt := m.Archived.Format("2006-01-02 15:04")
		reason := m.ArchiveReason
		if reason == "" {
			reason = "(no reason)"
		}

		if dryRun {
			fmt.Printf("  %-55s [%s, %s]\n", relPath, archivedAt, reason)
			fmt.Printf("  %-55s   %s\n", "", m.Title)
			ok++
			continue
		}

		UnarchiveOnAccess(m)
		if err := WriteMemoryEntry(m); err != nil {
			fmt.Fprintf(os.Stderr, "  [!] %s: %v\n", relPath, err)
			errors++
			continue
		}
		fmt.Printf("  ✓ %s\n", relPath)
		ok++
	}

	fmt.Println()
	if dryRun {
		fmt.Printf("[DRY RUN] %d would be unarchived. No changes written.\n", ok)
		fmt.Println("Re-run without --dry-run to apply.")
	} else {
		fmt.Printf("Unarchived: %d | Errors: %d\n", ok, errors)
	}
}
