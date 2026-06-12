package main

import (
	"archive/tar"
	"compress/gzip"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// recover-provenance repairs the fields stripped by the 2026-06-11 round-trip
// bug (source_document/source_version/domain/verified, plus consolidation_source
// and contributing_sessions) by lifting them from the encrypted backup history.
//
// Principle (per user directive): KEEP the current on-disk revision — body and
// every present field — and graft ONLY the fields that are currently missing,
// using values from backups. Provenance is static (set at ingestion), so any
// pre-strip snapshot carries the authoritative value; scanning newest→oldest and
// taking the first non-empty value yields the newest pre-strip revision of each
// field. A field is recovered for an entry ONLY if some backup shows the entry
// HAD it — an entry that legitimately never had source_document is left alone.
//
// Default is dry-run: it reports the diff and coverage without writing.

// provenanceFields holds the recoverable fields lifted from backups for one entry.
type provenanceFields struct {
	SourceDocument       string   `json:"source_document,omitempty"`
	SourceVersion        string   `json:"source_version,omitempty"`
	Domain               string   `json:"domain,omitempty"`
	Verified             bool     `json:"verified,omitempty"`
	HasVerified          bool     `json:"-"` // a backup explicitly carried verified: true
	ConsolidationSource  []string `json:"consolidation_source,omitempty"`
	ContributingSessions []string `json:"contributing_sessions,omitempty"`
	FromBackup           string   `json:"from_backup,omitempty"`
}

type provChange struct {
	Key    string            `json:"key"`
	Fields []string          `json:"fields"`
	Values map[string]string `json:"values"`
	From   string            `json:"from_backup"`
}

func cmdRecoverProvenance(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("recover-provenance", flag.ExitOnError)
	apply := fs.Bool("apply", false, "Write recovered fields to memory files (default: dry-run)")
	nBackups := fs.Int("backups", 40, "Number of backup snapshots to scan, spread across the timeline")
	manifests := fs.Bool("manifests", false, "Build the recovery union from Ingestion/manifest_*.md (authoritative book provenance by prefix) instead of backups")
	format := fs.String("format", "text", "Output format: text | json")
	fs.Parse(args)

	cfg := LoadConfig(vaultRoot)
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] load memories: %v\n", err)
		os.Exit(1)
	}

	var union map[string]*provenanceFields
	if *manifests {
		union, err = buildManifestUnion(vaultRoot, memories)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] build manifest union: %v\n", err)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Built manifest union: %d knowledge entries mapped to a source book by prefix.\n", len(union))
	} else {
		identityPath, ierr := resolveBackupIdentityPath(cfg)
		if ierr != nil {
			fmt.Fprintf(os.Stderr, "[!] cannot resolve backup identity: %v\n", ierr)
			os.Exit(1)
		}
		dir := resolveBackupLocalTargetDir(cfg, vaultRoot)
		blobs, berr := selectBackupBlobs(dir, *nBackups)
		if berr != nil {
			fmt.Fprintf(os.Stderr, "[!] cannot list backups in %s: %v\n", dir, berr)
			os.Exit(1)
		}
		if len(blobs) == 0 {
			fmt.Fprintf(os.Stderr, "[!] no backup snapshots found in %s\n", dir)
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "Scanning %d backup snapshot(s) for pre-strip provenance...\n", len(blobs))
		union = make(map[string]*provenanceFields)
		for i, blob := range blobs { // newest → oldest; first non-empty wins
			if err := scanBackupProvenance(blob, identityPath, union); err != nil {
				fmt.Fprintf(os.Stderr, "  [warn] %s: %v\n", filepath.Base(blob), err)
				continue
			}
			fmt.Fprintf(os.Stderr, "  [%d/%d] %s\n", i+1, len(blobs), filepath.Base(blob))
		}
	}

	changes := graftProvenance(memories, union, *apply)

	if *apply {
		written := 0
		for _, c := range changes {
			for _, m := range memories {
				if normalizeKey(m) == c.Key {
					if err := WriteMemoryEntry(m); err != nil {
						fmt.Fprintf(os.Stderr, "  [!] write %s: %v\n", m.FileName, err)
					} else {
						written++
					}
					break
				}
			}
		}
		fmt.Printf("Applied provenance to %d file(s).\n\n", written)
	}

	report := buildRecoveryReport(memories, union, changes, *apply)
	if *format == "json" {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
		return
	}
	printRecoveryReport(report, changes, *apply)
}

// graftProvenance fills missing fields on current entries from the backup union.
// When apply is true the entry structs are mutated in place (caller persists);
// either way the change list is returned for reporting.
func graftProvenance(memories []*MemoryEntry, union map[string]*provenanceFields, apply bool) []provChange {
	var changes []provChange
	for _, m := range memories {
		key := normalizeKey(m)
		u := union[key]
		if u == nil {
			continue
		}
		var fields []string
		values := map[string]string{}

		if m.SourceDocument == "" && u.SourceDocument != "" {
			fields = append(fields, "source_document")
			values["source_document"] = u.SourceDocument
			if apply {
				m.SourceDocument = u.SourceDocument
			}
		}
		if m.SourceVersion == "" && u.SourceVersion != "" {
			fields = append(fields, "source_version")
			values["source_version"] = u.SourceVersion
			if apply {
				m.SourceVersion = u.SourceVersion
			}
		}
		if m.Domain == "" && u.Domain != "" {
			fields = append(fields, "domain")
			values["domain"] = u.Domain
			if apply {
				m.Domain = u.Domain
			}
		}
		if !m.Verified && u.HasVerified && u.Verified {
			fields = append(fields, "verified")
			values["verified"] = "true"
			if apply {
				m.Verified = true
			}
		}
		if len(m.ConsolidationSource) == 0 && len(u.ConsolidationSource) > 0 {
			fields = append(fields, "consolidation_source")
			values["consolidation_source"] = strings.Join(u.ConsolidationSource, "; ")
			if apply {
				m.ConsolidationSource = u.ConsolidationSource
			}
		}
		if len(m.ContributingSessions) == 0 && len(u.ContributingSessions) > 0 {
			fields = append(fields, "contributing_sessions")
			values["contributing_sessions"] = strings.Join(u.ContributingSessions, "; ")
			if apply {
				m.ContributingSessions = u.ContributingSessions
			}
		}

		if len(fields) > 0 {
			changes = append(changes, provChange{Key: key, Fields: fields, Values: values, From: u.FromBackup})
		}
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Key < changes[j].Key })
	return changes
}

// buildManifestUnion derives source_document/source_version for knowledge
// entries from the ingestion manifests — the authoritative record of what was
// ingested. Each Ingestion/manifest_*.md frontmatter carries a `prefix` and
// `book_title`/`book_year`; every knowledge entry whose filename starts with
// "<prefix>_" is mapped to that book. Longest-prefix wins so "eedr_*" maps to
// the eedr manifest rather than "ee". Used as the fallback for entries the
// backup history can't reach (stripped before the oldest snapshot).
func buildManifestUnion(vaultRoot string, memories []*MemoryEntry) (map[string]*provenanceFields, error) {
	dir := filepath.Join(vaultRoot, "Ingestion")
	dirEntries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	type book struct{ doc, ver string }
	byPrefix := make(map[string]book)
	for _, e := range dirEntries {
		if e.IsDir() || !strings.HasPrefix(e.Name(), "manifest_") || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			continue
		}
		yamlStr, _, err := splitFrontmatter(data)
		if err != nil {
			continue
		}
		fm := parseYAMLMap(yamlStr)
		prefix := strings.Trim(strings.TrimSpace(fm["prefix"]), "\"'")
		doc := extractQuotedValue(fm["book_title"])
		if prefix == "" || doc == "" {
			continue
		}
		byPrefix[prefix] = book{doc: doc, ver: strings.Trim(strings.TrimSpace(fm["book_year"]), "\"'")}
	}

	// Longest prefix first so eedr_ beats ee_, etc.
	prefixes := make([]string, 0, len(byPrefix))
	for p := range byPrefix {
		prefixes = append(prefixes, p)
	}
	sort.Slice(prefixes, func(i, j int) bool { return len(prefixes[i]) > len(prefixes[j]) })

	union := make(map[string]*provenanceFields)
	for _, m := range memories {
		if m.Type != TypeKnowledge {
			continue
		}
		base := strings.TrimSuffix(m.FileName, ".md")
		for _, p := range prefixes {
			if strings.HasPrefix(base, p+"_") {
				b := byPrefix[p]
				union[normalizeKey(m)] = &provenanceFields{
					SourceDocument: b.doc,
					SourceVersion:  b.ver,
					FromBackup:     "manifest_" + p,
				}
				break
			}
		}
	}
	return union, nil
}

// selectBackupBlobs returns up to n vault-*.age blob paths, newest first, evenly
// sampled across the full timeline (always including the newest and oldest).
func selectBackupBlobs(dir string, n int) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var blobs []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "vault-") && strings.HasSuffix(e.Name(), ".age") {
			blobs = append(blobs, filepath.Join(dir, e.Name()))
		}
	}
	// Names embed a sortable timestamp (vault-YYYYMMDDThhmmssZ-...): reverse-sort = newest first.
	sort.Sort(sort.Reverse(sort.StringSlice(blobs)))
	if n <= 0 || len(blobs) <= n {
		return blobs, nil
	}
	picked := make([]string, 0, n)
	seen := make(map[int]bool)
	step := float64(len(blobs)-1) / float64(n-1)
	for i := 0; i < n; i++ {
		idx := int(float64(i)*step + 0.5)
		if idx >= len(blobs) {
			idx = len(blobs) - 1
		}
		if !seen[idx] {
			seen[idx] = true
			picked = append(picked, blobs[idx])
		}
	}
	return picked, nil
}

// scanBackupProvenance streams one encrypted backup (decrypt → gunzip → tar),
// parses every Memory/**.md entry's frontmatter in memory (no extraction), and
// fills any not-yet-set fields in the union. Large non-memory files (e.g. the
// session log) are skipped without reading their bodies.
func scanBackupProvenance(blobPath, identityPath string, union map[string]*provenanceFields) error {
	in, err := os.Open(blobPath)
	if err != nil {
		return err
	}
	defer in.Close()
	dec, err := DecryptFromAge(in, identityPath)
	if err != nil {
		return fmt.Errorf("decrypt: %w", err)
	}
	gz, err := gzip.NewReader(dec)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	from := filepath.Base(blobPath)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := filepath.ToSlash(hdr.Name)
		if !strings.HasPrefix(name, "Memory/") || !strings.HasSuffix(name, ".md") {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(tr, hdr.Size))
		if err != nil {
			continue
		}
		yamlStr, _, err := splitFrontmatter(data)
		if err != nil {
			continue
		}
		fm := parseYAMLMap(yamlStr)
		key := strings.ToLower(strings.TrimSuffix(name, ".md")) // matches normalizeKey
		mergeProvenance(union, key, fm, from)
	}
	return nil
}

// mergeProvenance fills only not-yet-set fields for key (first-seen wins; callers
// scan newest→oldest so the newest pre-strip value is kept).
func mergeProvenance(union map[string]*provenanceFields, key string, fm map[string]string, from string) {
	u := union[key]
	if u == nil {
		u = &provenanceFields{}
		union[key] = u
	}
	set := false
	if u.SourceDocument == "" {
		if v := extractQuotedValue(fm["source_document"]); v != "" {
			u.SourceDocument = v
			set = true
		}
	}
	if u.SourceVersion == "" {
		if v := extractQuotedValue(fm["source_version"]); v != "" {
			u.SourceVersion = v
			set = true
		}
	}
	if u.Domain == "" {
		if v := strings.TrimSpace(fm["domain"]); v != "" {
			u.Domain = v
			set = true
		}
	}
	if !u.HasVerified {
		if v := strings.TrimSpace(fm["verified"]); v != "" {
			u.HasVerified = true
			u.Verified = v == "true"
			set = true
		}
	}
	if len(u.ConsolidationSource) == 0 {
		if l := parseStringList(fm["consolidation_source"]); len(l) > 0 {
			u.ConsolidationSource = l
			set = true
		}
	}
	if len(u.ContributingSessions) == 0 {
		if l := parseStringList(fm["contributing_sessions"]); len(l) > 0 {
			u.ContributingSessions = l
			set = true
		}
	}
	if set && u.FromBackup == "" {
		u.FromBackup = from
	}
}

type recoveryReport struct {
	Applied             bool   `json:"applied"`
	BackupUnionEntries  int    `json:"backup_union_entries"`
	EntriesChanged      int    `json:"entries_changed"`
	FieldsGrafted       int    `json:"fields_grafted"`
	KnowledgeStripped   int    `json:"knowledge_missing_source_document"`
	KnowledgeRecovered  int    `json:"knowledge_recoverable"`
	KnowledgeUnrecov    int    `json:"knowledge_unrecoverable"`
	Unrecoverable       []string `json:"unrecoverable_keys,omitempty"`
}

func buildRecoveryReport(memories []*MemoryEntry, union map[string]*provenanceFields, changes []provChange, applied bool) recoveryReport {
	r := recoveryReport{Applied: applied, BackupUnionEntries: len(union), EntriesChanged: len(changes)}
	for _, c := range changes {
		r.FieldsGrafted += len(c.Fields)
	}
	for _, m := range memories {
		if m.Type != TypeKnowledge {
			continue
		}
		if m.SourceDocument != "" {
			continue
		}
		// Knowledge entry missing source_document: recoverable iff union has it.
		r.KnowledgeStripped++
		key := normalizeKey(m)
		if u := union[key]; u != nil && u.SourceDocument != "" {
			r.KnowledgeRecovered++
		} else {
			r.KnowledgeUnrecov++
			r.Unrecoverable = append(r.Unrecoverable, key)
		}
	}
	sort.Strings(r.Unrecoverable)
	return r
}

func printRecoveryReport(r recoveryReport, changes []provChange, applied bool) {
	mode := "DRY-RUN"
	if applied {
		mode = "APPLIED"
	}
	fmt.Printf("# Provenance Recovery (%s)\n\n", mode)
	fmt.Printf("Backup union: %d entries carried recoverable fields\n", r.BackupUnionEntries)
	fmt.Printf("Entries %s: %d   Fields grafted: %d\n",
		map[bool]string{true: "updated", false: "to update"}[applied], r.EntriesChanged, r.FieldsGrafted)
	fmt.Printf("Knowledge missing source_document: %d  →  recoverable %d, UNRECOVERABLE %d\n",
		r.KnowledgeStripped, r.KnowledgeRecovered, r.KnowledgeUnrecov)

	if len(changes) > 0 {
		fmt.Println("\n## Changes" + map[bool]string{true: "", false: " (preview)"}[applied])
		shown := changes
		capped := false
		if !applied && len(changes) > 60 {
			shown = changes[:60]
			capped = true
		}
		for _, c := range shown {
			fmt.Printf("  %s  +[%s]  (from %s)\n", shortKey(c.Key), strings.Join(c.Fields, ", "), c.From)
		}
		if capped {
			fmt.Printf("  ... and %d more (use --format json for the full list)\n", len(changes)-60)
		}
	}

	if r.KnowledgeUnrecov > 0 {
		fmt.Printf("\n## Unrecoverable from backups (%d) — need manifest / body Source: fallback\n", r.KnowledgeUnrecov)
		for _, k := range r.Unrecoverable {
			fmt.Printf("  %s\n", shortKey(k))
		}
	}

	if !applied && r.EntriesChanged > 0 {
		fmt.Println("\nRun `jm recover-provenance --apply` to write these fields.")
	}
}
