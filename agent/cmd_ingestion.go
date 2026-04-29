package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// cmdIngestion dispatches ingestion-management subcommands.
//
// Subcommands:
//   list  — print the master ingestion index
//   scan  — discover PDFs in references_dir that lack manifests
//   sync  — regenerate INDEX.md from the current set of manifests
func cmdIngestion(vaultRoot string, args []string) {
	if len(args) < 1 {
		printIngestionUsage()
		return
	}
	switch args[0] {
	case "list":
		cmdIngestionList(vaultRoot)
	case "scan":
		cmdIngestionScan(vaultRoot, args[1:])
	case "sync":
		cmdIngestionSync(vaultRoot)
	case "-h", "--help", "help":
		printIngestionUsage()
	default:
		fmt.Fprintf(os.Stderr, "unknown ingestion subcommand: %s\n\n", args[0])
		printIngestionUsage()
		os.Exit(1)
	}
}

func printIngestionUsage() {
	fmt.Print(`jm ingestion — book ingestion lifecycle management

Usage:
  jm ingestion list              Print the master ingestion index
  jm ingestion scan [--update]   Scan references_dir for PDFs without manifests
                                 (--update: write findings to INDEX.md)
  jm ingestion sync              Regenerate INDEX.md from current manifests

Configuration:
  References directory read from Config.md field 'references_dir'.

Protocol:
  See Ingestion/_README.md for ingestion protocol, signal-efficiency
  rules, and full-context preservation defaults.
`)
}

// IngestionManifest is the minimal parsed view of a manifest file.
// We read only the frontmatter fields we need for the index.
type IngestionManifest struct {
	Path          string
	Prefix        string
	BookTitle     string
	BookAuthor    string
	SourcePDF     string
	Status        string
	LastTouched   string
	NextAction    string // first "Next chapter" or similar pointer from Resume Instructions
}

func cmdIngestionList(vaultRoot string) {
	indexPath := filepath.Join(vaultRoot, "Ingestion", "INDEX.md")
	data, err := os.ReadFile(indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No INDEX.md found. Run `jm ingestion sync` to generate one.")
			return
		}
		fmt.Fprintf(os.Stderr, "[!] Failed to read INDEX.md: %v\n", err)
		os.Exit(1)
	}
	fmt.Print(string(data))
}

func cmdIngestionScan(vaultRoot string, args []string) {
	update := false
	for _, a := range args {
		if a == "--update" {
			update = true
		}
	}

	cfg := DefaultConfig()
	if cfg.ReferencesDir == "" {
		fmt.Fprintln(os.Stderr, "[!] Config.ReferencesDir not set. Set `references_dir` in Config or DefaultConfig().")
		os.Exit(1)
	}

	pdfs, err := listPDFs(cfg.ReferencesDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to scan %s: %v\n", cfg.ReferencesDir, err)
		os.Exit(1)
	}

	manifests, err := loadManifests(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load manifests: %v\n", err)
		os.Exit(1)
	}

	manifestedPDFs := make(map[string]bool)
	for _, m := range manifests {
		if m.SourcePDF != "" {
			manifestedPDFs[filepath.Clean(m.SourcePDF)] = true
		}
	}

	var unmanifested []string
	for _, pdf := range pdfs {
		if !manifestedPDFs[filepath.Clean(pdf)] {
			unmanifested = append(unmanifested, pdf)
		}
	}

	fmt.Printf("=== Ingestion Scan ===\n")
	fmt.Printf("References dir: %s\n", cfg.ReferencesDir)
	fmt.Printf("PDFs found:     %d\n", len(pdfs))
	fmt.Printf("Manifested:     %d\n", len(pdfs)-len(unmanifested))
	fmt.Printf("Unmanifested:   %d\n\n", len(unmanifested))

	if len(unmanifested) == 0 {
		fmt.Println("✓ All PDFs in references directory have manifests.")
		return
	}

	fmt.Println("Unmanifested PDFs (need a manifest created):")
	for _, pdf := range unmanifested {
		info, _ := os.Stat(pdf)
		size := int64(0)
		if info != nil {
			size = info.Size()
		}
		fmt.Printf("  • %s  (%s)\n", filepath.Base(pdf), humanBytes(size))
	}

	if update {
		if err := writeUnmanifestedSection(vaultRoot, unmanifested); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to update INDEX.md: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("\n✓ Updated Ingestion/INDEX.md with unmanifested PDFs.")
	} else {
		fmt.Println("\n(Run `jm ingestion scan --update` to record these in INDEX.md)")
		fmt.Println("To create manifests, copy an existing `manifest_*.md` as a template and fill in.")
	}
}

func cmdIngestionSync(vaultRoot string) {
	manifests, err := loadManifests(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load manifests: %v\n", err)
		os.Exit(1)
	}

	cfg := DefaultConfig()
	var unmanifested []string
	if cfg.ReferencesDir != "" {
		pdfs, _ := listPDFs(cfg.ReferencesDir)
		manifestedPDFs := make(map[string]bool)
		for _, m := range manifests {
			if m.SourcePDF != "" {
				manifestedPDFs[filepath.Clean(m.SourcePDF)] = true
			}
		}
		for _, pdf := range pdfs {
			if !manifestedPDFs[filepath.Clean(pdf)] {
				unmanifested = append(unmanifested, pdf)
			}
		}
	}

	if err := regenerateIndex(vaultRoot, manifests, unmanifested); err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to regenerate INDEX.md: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ INDEX.md regenerated. %d active manifests, %d unmanifested PDFs.\n",
		len(manifests), len(unmanifested))
}

// listPDFs returns every .pdf under dir (non-recursive by default;
// one level deep to catch simple subdirectories).
func listPDFs(dir string) ([]string, error) {
	var pdfs []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		full := filepath.Join(dir, e.Name())
		if e.IsDir() {
			// One level deep
			subEntries, subErr := os.ReadDir(full)
			if subErr != nil {
				continue
			}
			for _, se := range subEntries {
				if !se.IsDir() && strings.EqualFold(filepath.Ext(se.Name()), ".pdf") {
					pdfs = append(pdfs, filepath.Join(full, se.Name()))
				}
			}
			continue
		}
		if strings.EqualFold(filepath.Ext(e.Name()), ".pdf") {
			pdfs = append(pdfs, full)
		}
	}
	sort.Strings(pdfs)
	return pdfs, nil
}

// loadManifests reads all Ingestion/manifest_*.md files and extracts the
// fields we need for indexing. Best-effort — if a field is missing, the
// manifest still shows up in the index with empty values.
func loadManifests(vaultRoot string) ([]IngestionManifest, error) {
	ingestionDir := filepath.Join(vaultRoot, "Ingestion")
	entries, err := os.ReadDir(ingestionDir)
	if err != nil {
		return nil, err
	}

	var manifests []IngestionManifest
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "manifest_") || !strings.HasSuffix(name, ".md") {
			continue
		}
		full := filepath.Join(ingestionDir, name)
		m, err := parseManifest(full)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] parse %s: %v\n", name, err)
			continue
		}
		manifests = append(manifests, m)
	}
	sort.Slice(manifests, func(i, j int) bool {
		return manifests[i].Prefix < manifests[j].Prefix
	})
	return manifests, nil
}

var (
	reYamlKey   = regexp.MustCompile(`^(\w+)\s*:\s*(.*)$`)
	reNextChapter = regexp.MustCompile(`\*\*Next chapter.*?\*\*\s*(.+)`)
)

// parseManifest extracts frontmatter + next-action pointer.
func parseManifest(path string) (IngestionManifest, error) {
	f, err := os.Open(path)
	if err != nil {
		return IngestionManifest{}, err
	}
	defer f.Close()

	m := IngestionManifest{Path: path}
	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	state := "pre-frontmatter" // pre-frontmatter | frontmatter | body
	for scanner.Scan() {
		line := scanner.Text()
		switch state {
		case "pre-frontmatter":
			if strings.TrimSpace(line) == "---" {
				state = "frontmatter"
			}
		case "frontmatter":
			if strings.TrimSpace(line) == "---" {
				state = "body"
				continue
			}
			if match := reYamlKey.FindStringSubmatch(line); match != nil {
				key := match[1]
				val := strings.TrimSpace(strings.Trim(match[2], `"`))
				switch key {
				case "book_title":
					m.BookTitle = val
				case "book_author":
					m.BookAuthor = val
				case "source_pdf":
					m.SourcePDF = val
				case "prefix":
					m.Prefix = val
				case "status":
					m.Status = val
				case "last_touched":
					m.LastTouched = val
				}
			}
		case "body":
			// Look for "Next chapter" pointer in Resume Instructions
			if m.NextAction == "" {
				if match := reNextChapter.FindStringSubmatch(line); match != nil {
					m.NextAction = strings.TrimSpace(match[1])
				}
			}
		}
	}
	return m, scanner.Err()
}

// regenerateIndex rewrites INDEX.md from the given manifests.
// Preserves the "Previously Ingested" and "Non-Book" sections by reading
// them from the existing INDEX.md and carrying them forward unchanged.
func regenerateIndex(vaultRoot string, manifests []IngestionManifest, unmanifested []string) error {
	indexPath := filepath.Join(vaultRoot, "Ingestion", "INDEX.md")
	existing, _ := os.ReadFile(indexPath)

	preservedPrevSection := extractSection(string(existing), "## Previously Ingested (no active manifest)", "## Non-Book Knowledge Entries")
	preservedNonBook := extractSection(string(existing), "## Non-Book Knowledge Entries", "## Consistency Guarantee")
	preservedConsistency := extractSection(string(existing), "## Consistency Guarantee", "## Operational Notes")
	preservedOperational := extractSection(string(existing), "## Operational Notes", "")

	var b strings.Builder
	fmt.Fprintln(&b, "# Ingestion Index")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Master manifest-of-manifests. Every book intended for ingestion into")
	fmt.Fprintln(&b, "`Memory/Knowledge/` should appear here. Regeneratable from")
	fmt.Fprintln(&b, "`Ingestion/manifest_*.md` via `jm ingestion sync`.")
	fmt.Fprintln(&b)
	fmt.Fprintf(&b, "**Last updated:** %s\n\n", time.Now().UTC().Format("2006-01-02"))

	fmt.Fprintln(&b, "## Active Manifests")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "| Prefix | Book | Author | Status | Next action |")
	fmt.Fprintln(&b, "|--------|------|--------|--------|-------------|")
	for _, m := range manifests {
		next := m.NextAction
		if next == "" {
			next = "—"
		}
		fmt.Fprintf(&b, "| `%s` | %s | %s | %s | %s |\n",
			m.Prefix, m.BookTitle, m.BookAuthor, m.Status, next)
	}
	fmt.Fprintln(&b)

	fmt.Fprintln(&b, "## Books Discovered in References Directory (awaiting manifest)")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Unmanifested PDFs found at last scan. Run `jm ingestion scan` to refresh.")
	fmt.Fprintln(&b)
	if len(unmanifested) == 0 {
		fmt.Fprintln(&b, "| PDF filename | Size |")
		fmt.Fprintln(&b, "|--------------|------|")
		fmt.Fprintln(&b, "| *(none pending)* | |")
	} else {
		fmt.Fprintln(&b, "| PDF filename | Size |")
		fmt.Fprintln(&b, "|--------------|------|")
		for _, pdf := range unmanifested {
			info, _ := os.Stat(pdf)
			size := int64(0)
			if info != nil {
				size = info.Size()
			}
			fmt.Fprintf(&b, "| %s | %s |\n", filepath.Base(pdf), humanBytes(size))
		}
	}
	fmt.Fprintln(&b)

	if preservedPrevSection != "" {
		b.WriteString(preservedPrevSection)
	}
	if preservedNonBook != "" {
		b.WriteString(preservedNonBook)
	}
	if preservedConsistency != "" {
		b.WriteString(preservedConsistency)
	}
	if preservedOperational != "" {
		b.WriteString(preservedOperational)
	}

	return os.WriteFile(indexPath, []byte(b.String()), 0o644)
}

// writeUnmanifestedSection updates only the "Books Discovered" section of
// INDEX.md, preserving everything else.
func writeUnmanifestedSection(vaultRoot string, unmanifested []string) error {
	indexPath := filepath.Join(vaultRoot, "Ingestion", "INDEX.md")
	existing, err := os.ReadFile(indexPath)
	if err != nil {
		return err
	}

	var rebuilt strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(string(existing)))
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	inTargetSection := false
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "## Books Discovered") {
			inTargetSection = true
			rebuilt.WriteString(line + "\n\n")
			rebuilt.WriteString("Unmanifested PDFs found at last scan. Run `jm ingestion scan` to refresh.\n\n")
			if len(unmanifested) == 0 {
				rebuilt.WriteString("| PDF filename | Size |\n")
				rebuilt.WriteString("|--------------|------|\n")
				rebuilt.WriteString("| *(none pending)* | |\n\n")
			} else {
				rebuilt.WriteString("| PDF filename | Size |\n")
				rebuilt.WriteString("|--------------|------|\n")
				for _, pdf := range unmanifested {
					info, _ := os.Stat(pdf)
					size := int64(0)
					if info != nil {
						size = info.Size()
					}
					fmt.Fprintf(&rebuilt, "| %s | %s |\n", filepath.Base(pdf), humanBytes(size))
				}
				rebuilt.WriteString("\n")
			}
			continue
		}
		if inTargetSection && strings.HasPrefix(line, "## ") {
			inTargetSection = false
		}
		if !inTargetSection {
			rebuilt.WriteString(line + "\n")
		}
	}

	return os.WriteFile(indexPath, []byte(rebuilt.String()), 0o644)
}

// extractSection pulls text between two markdown headers (inclusive of the
// start header, exclusive of the end header). If endHeader is "", extracts
// from startHeader to end of document. Returns "" if startHeader not found.
func extractSection(doc, startHeader, endHeader string) string {
	startIdx := strings.Index(doc, startHeader)
	if startIdx < 0 {
		return ""
	}
	doc = doc[startIdx:]
	if endHeader != "" {
		endIdx := strings.Index(doc, endHeader)
		if endIdx >= 0 {
			return doc[:endIdx]
		}
	}
	return doc
}

// humanBytes formats a byte count as a short human-readable string.
func humanBytes(n int64) string {
	const (
		kb = 1024
		mb = 1024 * kb
		gb = 1024 * mb
	)
	switch {
	case n >= gb:
		return fmt.Sprintf("%.1f GB", float64(n)/float64(gb))
	case n >= mb:
		return fmt.Sprintf("%.1f MB", float64(n)/float64(mb))
	case n >= kb:
		return fmt.Sprintf("%.0f KB", float64(n)/float64(kb))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
