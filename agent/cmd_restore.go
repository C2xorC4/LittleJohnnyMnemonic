package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

// cmdRestore is the explicit un-compression command. It pulls a memory
// back from gist fidelity to full by reading the archive snapshot and
// replacing the in-Memory/ gist body with the archived full body.
//
// Semantics:
//   - Non-destructive: the Archive/ copy is preserved so subsequent decay
//     passes can re-compress without losing the source.
//   - Only works on memories with archive_ref set (i.e., memories that
//     crossed the gist boundary via jm compress).
//   - Resets fidelity to "full", clears target_fidelity, clears archive_ref.
//
// This is the explicit counterpart to B' auto-resurrection: if organic
// retrieval can't reach an over-compressed memory, `jm restore` is the
// surgical escape hatch.
func cmdRestore(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("restore", flag.ExitOnError)
	file := fs.String("file", "", "Memory file to restore (path relative to vault root or absolute)")
	dryRun := fs.Bool("dry-run", false, "Show what would be restored without writing")
	fs.Parse(args)

	if *file == "" {
		fmt.Fprintln(os.Stderr, "Usage: jm restore --file <path> [--dry-run]")
		os.Exit(1)
	}

	path := *file
	if !filepath.IsAbs(path) {
		path = filepath.Join(vaultRoot, path)
	}

	entry, err := ParseMemoryEntry(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to parse %s: %v\n", path, err)
		os.Exit(1)
	}

	relPath, _ := filepath.Rel(vaultRoot, path)

	if entry.ArchiveRef == "" {
		fmt.Fprintf(os.Stderr, "[!] %s has no archive_ref — nothing to restore from\n", relPath)
		fmt.Fprintln(os.Stderr, "    (restore only works on memories that crossed the gist boundary)")
		os.Exit(1)
	}

	// Resolve archive path (stored as forward-slash relative)
	archiveRel := filepath.FromSlash(entry.ArchiveRef)
	archivePath := filepath.Join(vaultRoot, archiveRel)

	// Parse archive entry to extract the full body
	archiveEntry, err := ParseMemoryEntry(archivePath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to parse archive file %s: %v\n", archivePath, err)
		os.Exit(1)
	}

	if *dryRun {
		fmt.Printf("[DRY RUN] Would restore: %s\n", relPath)
		fmt.Printf("          Title: %s\n", entry.Title)
		fmt.Printf("          Current fidelity: %s (%d chars)\n", currentFidelity(entry), len(entry.Body))
		fmt.Printf("          Archive source: %s\n", entry.ArchiveRef)
		fmt.Printf("          Restored body: %d chars (full fidelity)\n", len(archiveEntry.Body))
		return
	}

	// Copy archive body into target memory, reset fidelity state
	entry.Body = archiveEntry.Body
	entry.Fidelity = FidelityFull
	entry.TargetFidelity = ""
	entry.ArchiveRef = ""

	if err := WriteMemoryEntry(entry); err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to write %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("✓ Restored: %s\n", relPath)
	fmt.Printf("  Title: %s\n", entry.Title)
	fmt.Printf("  Fidelity: gist → full (%d chars)\n", len(archiveEntry.Body))
	fmt.Printf("  Archive copy preserved: %s\n", archiveRel)
}
