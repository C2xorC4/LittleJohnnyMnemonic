package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// cmdCompress is the Claude-driven progressive compression command. It
// has two modes:
//
//	jm compress                           → list pending compressions
//	jm compress --list                    → same
//	jm compress --file <path> --apply     → atomic update from stdin
//
// The apply flow:
//  1. The caller (Claude) reads the current memory via a Read tool call
//  2. Generates a compressed body appropriate for the target fidelity
//  3. Pipes the new body to this command via stdin
//  4. This command atomically updates body + fidelity + clears target
//
// For target == "gist", this command also snapshots the pre-compression
// version of the memory file to Archive/<type>/<filename> and sets the
// ArchiveRef field so restoration can find it later.
func cmdCompress(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("compress", flag.ExitOnError)
	list := fs.Bool("list", false, "List memories with pending compressions (default if no other flags)")
	file := fs.String("file", "", "Path to the memory file to compress (relative to vault root or absolute)")
	apply := fs.Bool("apply", false, "Apply compression using body read from stdin (requires --file)")
	fs.Parse(args)

	// Validation: --apply requires --file
	if *apply && *file == "" {
		fmt.Fprintln(os.Stderr, "[!] --apply requires --file <path>")
		os.Exit(1)
	}

	// Default to list mode if no action flags given
	if !*apply {
		runCompressList(vaultRoot)
		return
	}

	runCompressApply(vaultRoot, *file)
	_ = list // silence unused warning; --list is an alias for default mode
}

// runCompressList shows all memories with pending compressions.
func runCompressList(vaultRoot string) {
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	now := time.Now()
	type pending struct {
		relPath  string
		title    string
		current  string
		target   string
		ageDays  float64
		imp      string
	}
	var pendings []pending

	for _, m := range memories {
		if m.TargetFidelity == "" {
			continue
		}
		cur := currentFidelity(m)
		if !FidelityIsHigherThan(m.TargetFidelity, cur) {
			continue
		}
		rel, _ := filepath.Rel(vaultRoot, m.FilePath)
		pendings = append(pendings, pending{
			relPath: rel,
			title:   m.Title,
			current: cur,
			target:  m.TargetFidelity,
			ageDays: now.Sub(m.LastAccessed).Hours() / 24,
			imp:     m.Importance,
		})
	}

	if len(pendings) == 0 {
		fmt.Println("No compressions pending.")
		return
	}

	// Sort by target severity (gist first), then by age descending
	sort.Slice(pendings, func(i, j int) bool {
		oi := fidelityOrder[pendings[i].target]
		oj := fidelityOrder[pendings[j].target]
		if oi != oj {
			return oi > oj
		}
		return pendings[i].ageDays > pendings[j].ageDays
	})

	fmt.Printf("=== Pending Compressions (%d) ===\n\n", len(pendings))
	fmt.Printf("%-55s %-11s → %-10s %8s  %-11s\n",
		"PATH", "CURRENT", "TARGET", "AGE_DAYS", "IMPORTANCE")
	fmt.Println(strings.Repeat("─", 110))

	for _, p := range pendings {
		fmt.Printf("%-55s %-11s → %-10s %8.1f  %-11s\n",
			truncate(p.relPath, 55), p.current, p.target, p.ageDays, p.imp)
		fmt.Printf("%-55s   %s\n", "", truncate(p.title, 70))
	}

	fmt.Println()
	fmt.Println("To apply a compression, generate the new body and pipe it to:")
	fmt.Println("  jm compress --file <path> --apply")
	fmt.Println()
	fmt.Println("Soft budgets: detailed ≤ 2000, summary ≤ 800, gist ≤ 400 chars")
}

// runCompressApply performs the atomic update: reads new body from stdin,
// validates, and writes the memory with updated fidelity.
func runCompressApply(vaultRoot, file string) {
	// Resolve path
	path := file
	if !filepath.IsAbs(path) {
		path = filepath.Join(vaultRoot, path)
	}

	entry, err := ParseMemoryEntry(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to parse %s: %v\n", path, err)
		os.Exit(1)
	}

	if entry.TargetFidelity == "" {
		fmt.Fprintf(os.Stderr, "[!] %s is not queued for compression (no target_fidelity set)\n", path)
		os.Exit(1)
	}

	cur := currentFidelity(entry)
	if !FidelityIsHigherThan(entry.TargetFidelity, cur) {
		fmt.Fprintf(os.Stderr, "[!] target_fidelity (%s) is not more compressed than current (%s) — nothing to do\n",
			entry.TargetFidelity, cur)
		os.Exit(1)
	}

	// Read new body from stdin
	newBodyBytes, err := io.ReadAll(os.Stdin)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to read stdin: %v\n", err)
		os.Exit(1)
	}
	newBody := strings.TrimSpace(string(newBodyBytes))
	if newBody == "" {
		fmt.Fprintln(os.Stderr, "[!] Empty body from stdin — aborting to avoid data loss")
		os.Exit(1)
	}

	// Soft budget warning
	if budget := FidelityBudget(entry.TargetFidelity); budget > 0 && len(newBody) > budget {
		fmt.Fprintf(os.Stderr, "[!] warning: new body is %d chars, soft budget for %s is %d\n",
			len(newBody), entry.TargetFidelity, budget)
	}

	targetFid := entry.TargetFidelity
	relPath, _ := filepath.Rel(vaultRoot, path)

	// Gist-boundary: snapshot pre-compression version to Archive/<type>/
	if targetFid == FidelityGist {
		archiveRel, err := snapshotToArchive(vaultRoot, entry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to snapshot to archive: %v\n", err)
			os.Exit(1)
		}
		entry.ArchiveRef = archiveRel
		fmt.Printf("  Snapshotted pre-compression version → %s\n", archiveRel)
	}

	// Apply the compression atomically via WriteMemoryEntry
	entry.Body = newBody
	entry.Fidelity = targetFid
	entry.TargetFidelity = ""

	if err := WriteMemoryEntry(entry); err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to write %s: %v\n", path, err)
		os.Exit(1)
	}

	fmt.Printf("✓ Compressed: %s\n", relPath)
	fmt.Printf("  %s → %s (%d chars)\n", cur, targetFid, len(newBody))
}

// snapshotToArchive copies the current file content to
// Archive/<Type>/<filename> and returns the archive path relative to
// vault root. This is called at the gist boundary so the pre-compression
// version is preserved for restoration.
//
// The snapshot reads the file raw (not via ParseMemoryEntry+WriteMemoryEntry)
// so every byte of the original, including comments and formatting
// idiosyncrasies, is preserved.
func snapshotToArchive(vaultRoot string, entry *MemoryEntry) (string, error) {
	typeDir := archiveTypeDir(string(entry.Type))
	archiveSubdir := filepath.Join(vaultRoot, "Archive", typeDir)
	if err := os.MkdirAll(archiveSubdir, 0755); err != nil {
		return "", fmt.Errorf("mkdir %s: %w", archiveSubdir, err)
	}

	archivePath := filepath.Join(archiveSubdir, entry.FileName)
	rawContent, err := os.ReadFile(entry.FilePath)
	if err != nil {
		return "", fmt.Errorf("read source: %w", err)
	}

	if err := os.WriteFile(archivePath, rawContent, 0644); err != nil {
		return "", fmt.Errorf("write archive: %w", err)
	}

	// Return archive path relative to vault root (forward-slash form for
	// portability in the archive_ref frontmatter field).
	rel, err := filepath.Rel(vaultRoot, archivePath)
	if err != nil {
		return archivePath, nil
	}
	return filepath.ToSlash(rel), nil
}
