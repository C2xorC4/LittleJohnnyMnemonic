package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// `jm daydream` is the user-facing surface for triaging accumulated daydream
// findings — the explicit-attention escape hatch from the design discussion
// (see the 2026-04-30 buffer entry on user engagement during task focus).
// Real-time engagement signals during active work are unreliable; this is
// where the user opts in to triage when they actually have bandwidth.

func cmdDaydream(vaultRoot string, args []string) {
	if len(args) < 1 {
		printDaydreamUsage()
		return
	}
	switch args[0] {
	case "review":
		cmdDaydreamReview(vaultRoot, args[1:])
	case "list":
		cmdDaydreamList(vaultRoot, args[1:])
	case "-h", "--help", "help":
		printDaydreamUsage()
	default:
		fmt.Fprintf(os.Stderr, "[jm daydream] unknown subcommand: %s\n", args[0])
		printDaydreamUsage()
		os.Exit(1)
	}
}

func printDaydreamUsage() {
	fmt.Print(`jm daydream — Daydream finding triage

Usage:
  jm daydream review [flags]    Interactive batch review (designed for "I have 15 min")
  jm daydream list   [flags]    Print pending entries (non-interactive)

Flags (both subcommands):
  --kind <value>      Filter by daydream_kind (exploration|replay-refine|replay-contradict)
  --priority <value>  Filter by priority (high|critical)
  --max-age-days N    Only show entries newer than N days (0 = no limit)
`)
}

// DaydreamFilter is the shared filter spec for review/list. Empty fields
// match anything; MaxAgeDays=0 disables the age filter.
type DaydreamFilter struct {
	Kind       string
	Priority   string
	MaxAgeDays int
}

// loadDaydreamEntries returns all .md files under Buffer/Daydream/ as
// parsed BufferEntry pointers. Non-daydream Buffer/ entries are not
// included — the review CLI is scoped to daydream output.
func loadDaydreamEntries(vaultRoot string) ([]*BufferEntry, error) {
	all, err := LoadAllBufferEntries(vaultRoot)
	if err != nil {
		return nil, err
	}
	var out []*BufferEntry
	for _, e := range all {
		if !IsDaydreamSourced(e) && !strings.Contains(filepath.ToSlash(e.FilePath), "/Daydream/") {
			continue
		}
		out = append(out, e)
	}
	// Stable order by timestamp ascending so older entries surface first
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Timestamp.Before(out[j].Timestamp)
	})
	return out, nil
}

// filterDaydreamEntries applies the filter in-memory. Empty filter fields
// match everything. Time-based filter uses the entry's parsed Timestamp;
// entries with zero timestamp pass the age filter (we don't drop them).
func filterDaydreamEntries(entries []*BufferEntry, f DaydreamFilter, now time.Time) []*BufferEntry {
	var out []*BufferEntry
	cutoff := time.Time{}
	if f.MaxAgeDays > 0 {
		cutoff = now.Add(-time.Duration(f.MaxAgeDays) * 24 * time.Hour)
	}

	for _, e := range entries {
		if f.Kind != "" && !strings.EqualFold(e.DaydreamKind, f.Kind) {
			continue
		}
		if f.Priority != "" && !strings.EqualFold(e.Priority, f.Priority) {
			continue
		}
		if !cutoff.IsZero() && !e.Timestamp.IsZero() && e.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, e)
	}
	return out
}

func cmdDaydreamReview(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("review", flag.ExitOnError)
	kind := fs.String("kind", "", "Filter by daydream_kind")
	priority := fs.String("priority", "", "Filter by priority")
	maxAgeDays := fs.Int("max-age-days", 0, "Only show entries newer than N days (0 = no limit)")
	fs.Parse(args)

	entries, err := loadDaydreamEntries(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] %v\n", err)
		os.Exit(1)
	}
	pending := filterDaydreamEntries(entries, DaydreamFilter{
		Kind: *kind, Priority: *priority, MaxAgeDays: *maxAgeDays,
	}, time.Now())

	if len(pending) == 0 {
		fmt.Println("No pending daydream entries match the filter.")
		return
	}

	fmt.Printf("=== Daydream Review (%d pending) ===\n\n", len(pending))
	fmt.Println("Actions: (a)ccept  (r)efine  (x)reject  (p)romote-now  (s)kip  (q)uit  (?)help")
	fmt.Println()

	stats := runReviewLoop(vaultRoot, pending, os.Stdin, os.Stdout)
	fmt.Printf("\nReview complete: %d accepted, %d refined, %d rejected, %d promoted, %d skipped.\n",
		stats.Accepted, stats.Refined, stats.Rejected, stats.Promoted, stats.Skipped)
}

func cmdDaydreamList(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("list", flag.ExitOnError)
	kind := fs.String("kind", "", "Filter by daydream_kind")
	priority := fs.String("priority", "", "Filter by priority")
	maxAgeDays := fs.Int("max-age-days", 0, "Only show entries newer than N days (0 = no limit)")
	fs.Parse(args)

	entries, err := loadDaydreamEntries(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] %v\n", err)
		os.Exit(1)
	}
	pending := filterDaydreamEntries(entries, DaydreamFilter{
		Kind: *kind, Priority: *priority, MaxAgeDays: *maxAgeDays,
	}, time.Now())

	if len(pending) == 0 {
		fmt.Println("No pending daydream entries match the filter.")
		return
	}
	fmt.Printf("%-50s %-20s %-10s %s\n", "FILE", "KIND", "PRIORITY", "AGE")
	fmt.Println(strings.Repeat("─", 100))
	now := time.Now()
	for _, e := range pending {
		age := "—"
		if !e.Timestamp.IsZero() {
			age = formatAge(now.Sub(e.Timestamp))
		}
		fmt.Printf("%-50s %-20s %-10s %s\n",
			truncateField(e.FileName, 48), e.DaydreamKind, e.Priority, age)
	}
}

// reviewStats counts each action taken during a review pass for the
// summary line printed at the end.
type reviewStats struct {
	Accepted int
	Refined  int
	Rejected int
	Promoted int
	Skipped  int
}

// reviewActionLogFn is the writer used by runReviewLoop to record each user
// action to Metrics/daydream_review_log.jsonl. Production code uses
// AppendReviewActionLog; tests substitute an in-memory recorder.
var reviewActionLogFn = AppendReviewActionLog

// runReviewLoop processes the entries one by one, reading actions from r
// and writing prompts/results to w. Test code injects strings.Reader and
// bytes.Buffer to drive deterministic flows.
func runReviewLoop(vaultRoot string, entries []*BufferEntry, r io.Reader, w io.Writer) reviewStats {
	reader := bufio.NewReader(r)
	stats := reviewStats{}

	for i, entry := range entries {
		renderEntry(w, entry, i+1, len(entries))

		action := readActionChar(reader, w)
		actionName, success := "", false
		switch action {
		case 'a':
			actionName = "accept"
			if err := acceptEntry(entry); err != nil {
				fmt.Fprintf(w, "  [!] accept failed: %v\n", err)
			} else {
				fmt.Fprintln(w, "  → accepted (pinned for next consolidation)")
				stats.Accepted++
				success = true
			}
		case 'r':
			actionName = "refine"
			if err := refineEntry(entry, w); err != nil {
				fmt.Fprintf(w, "  [!] refine failed: %v\n", err)
			} else {
				fmt.Fprintln(w, "  → refined (saved)")
				stats.Refined++
				success = true
			}
		case 'x':
			actionName = "reject"
			if err := rejectEntry(entry); err != nil {
				fmt.Fprintf(w, "  [!] reject failed: %v\n", err)
			} else {
				fmt.Fprintln(w, "  → rejected (deleted)")
				stats.Rejected++
				success = true
			}
		case 'p':
			actionName = "promote"
			if err := promoteEntry(vaultRoot, entry, w); err != nil {
				fmt.Fprintf(w, "  [!] promote failed: %v\n", err)
			} else {
				stats.Promoted++
				success = true
			}
		case 's':
			actionName = "skip"
			fmt.Fprintln(w, "  → skipped")
			stats.Skipped++
			success = true
		case 'q':
			actionName = "quit"
			fmt.Fprintln(w, "  → quit")
			if reviewActionLogFn != nil {
				rec := buildReviewActionRecord(entry, actionName, true, time.Now())
				if err := reviewActionLogFn(vaultRoot, rec); err != nil {
					fmt.Fprintf(w, "  [warn] review log: %v\n", err)
				}
			}
			return stats
		default:
			actionName = "unknown"
			fmt.Fprintf(w, "  unknown action %q — skipping\n", string(action))
			stats.Skipped++
		}

		// Hook 7: audit log of the action. Independent of in-flight contradiction
		// marking — runs whether or not the entry was a replay-contradict, so the
		// stream captures every user decision for downstream tuning.
		if reviewActionLogFn != nil {
			rec := buildReviewActionRecord(entry, actionName, success, time.Now())
			if err := reviewActionLogFn(vaultRoot, rec); err != nil {
				fmt.Fprintf(w, "  [warn] review log: %v\n", err)
			}
		}

		// Mark matching contradiction entries as reviewed if this was a
		// replay-contradict and the user took a non-skip action.
		if action != 's' && action != 'q' && entry.DaydreamKind == "replay-contradict" {
			if n, err := markContradictionReviewed(vaultRoot, entry); err != nil {
				fmt.Fprintf(w, "  [warn] contradiction-mark: %v\n", err)
			} else if n > 0 {
				fmt.Fprintf(w, "  (marked %d contradiction(s) as reviewed)\n", n)
			}
		}
		fmt.Fprintln(w)
	}
	return stats
}

func renderEntry(w io.Writer, entry *BufferEntry, idx, total int) {
	fmt.Fprintf(w, "[%d/%d] %s\n", idx, total, entry.FileName)
	if entry.DaydreamKind != "" {
		fmt.Fprintf(w, "  kind: %s", entry.DaydreamKind)
		if entry.DaydreamMode != "" {
			fmt.Fprintf(w, " (%s)", entry.DaydreamMode)
		}
		if entry.Priority != "" {
			fmt.Fprintf(w, "  priority: %s", entry.Priority)
		}
		fmt.Fprintln(w)
	}
	if entry.Relationship != "" {
		fmt.Fprintf(w, "  relationship: %s\n", entry.Relationship)
	}
	if !entry.Timestamp.IsZero() {
		fmt.Fprintf(w, "  age: %s  surprise: %.2f\n", formatAge(time.Since(entry.Timestamp)), entry.Surprise)
	}
	if len(entry.Tags) > 0 {
		fmt.Fprintf(w, "  tags: %s\n", strings.Join(entry.Tags, ", "))
	}
	if len(entry.Related) > 0 {
		fmt.Fprintf(w, "  related: %s\n", strings.Join(entry.Related, ", "))
	}
	fmt.Fprintln(w)
	body := entry.Body
	if len(body) > 600 {
		body = body[:600] + " …"
	}
	for _, line := range strings.Split(body, "\n") {
		fmt.Fprintf(w, "    %s\n", line)
	}
	fmt.Fprintln(w)
}

func readActionChar(r *bufio.Reader, w io.Writer) byte {
	for {
		fmt.Fprint(w, "Action> ")
		line, err := r.ReadString('\n')
		if err != nil && line == "" {
			return 'q'
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		c := line[0]
		switch c {
		case '?':
			fmt.Fprintln(w, "  a=accept  r=refine  x=reject  p=promote-now  s=skip  q=quit")
			continue
		case 'a', 'r', 'x', 'p', 's', 'q':
			return c
		}
		fmt.Fprintf(w, "  (unknown — try a/r/x/p/s/q or ? for help)\n")
	}
}

func acceptEntry(entry *BufferEntry) error {
	entry.Pinned = true
	return WriteBufferEntry(entry)
}

func refineEntry(entry *BufferEntry, w io.Writer) error {
	editor := defaultEditor()
	cmd := exec.Command(editor, entry.FilePath)
	cmd.Stdin = os.Stdin
	cmd.Stdout = w
	cmd.Stderr = w
	return cmd.Run()
}

func rejectEntry(entry *BufferEntry) error {
	return os.Remove(entry.FilePath)
}

// promoteEntry writes a Memory/Semantic/ entry from a buffer entry, then
// removes the buffer file. Defaults are conservative: type=semantic,
// confidence=0.7, tags carried through, body unchanged. The user can move
// or rewrite in Obsidian afterward.
func promoteEntry(vaultRoot string, entry *BufferEntry, w io.Writer) error {
	targetDir := filepath.Join(vaultRoot, "Memory", "Semantic")
	if err := os.MkdirAll(targetDir, 0o755); err != nil {
		return fmt.Errorf("mkdir target: %w", err)
	}
	title := strings.TrimSuffix(entry.FileName, ".md")
	title = strings.ReplaceAll(title, "_", " ")
	title = strings.TrimPrefix(title, "daydream-")

	now := time.Now()
	mem := &MemoryEntry{
		Type:               TypeSemantic,
		Title:              title,
		Created:            now,
		LastAccessed:       now,
		AccessCount:        1,
		DecayRate:          0.2, // matches default semantic decay
		Confidence:         0.7,
		SurpriseAtEncoding: entry.Surprise,
		Tags:               entry.Tags,
		Body:               entry.Body,
		FileName:           entry.FileName,
		FilePath:           filepath.Join(targetDir, entry.FileName),
	}
	if err := WriteMemoryEntry(mem); err != nil {
		return fmt.Errorf("write memory: %w", err)
	}
	if err := os.Remove(entry.FilePath); err != nil {
		return fmt.Errorf("memory written but buffer not removed: %w", err)
	}
	fmt.Fprintf(w, "  → promoted to Memory/Semantic/%s\n", entry.FileName)
	return nil
}

func defaultEditor() string {
	if e := os.Getenv("EDITOR"); e != "" {
		return e
	}
	if runtime.GOOS == "windows" {
		return "notepad"
	}
	return "vi"
}

func formatAge(d time.Duration) string {
	if d < time.Hour {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours()/24))
}

func truncateField(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}

// markContradictionReviewed scans Metrics/replay_contradictions.jsonl, sets
// Reviewed=true on entries whose recent_seed_path matches the buffer
// entry's recent-seed wikilink (or whose timestamp is close to the buffer
// entry's timestamp), and rewrites the file. Returns the count flipped.
//
// Best-effort: matching is approximate because the buffer entry doesn't
// store a hard pointer back to the contradiction record. We match on the
// `related` wikilinks (which point to the recent seed) plus a 24h timestamp
// window. False positives just mean an extra entry gets marked reviewed —
// not catastrophic, and the user is the one driving the action.
func markContradictionReviewed(vaultRoot string, entry *BufferEntry) (int, error) {
	path := filepath.Join(vaultRoot, "Metrics", "replay_contradictions.jsonl")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}

	var hints []string
	for _, r := range entry.Related {
		hints = append(hints, normalizeRelatedRef(r))
	}

	// Decode + re-encode line by line; the file is small (rotated regularly).
	var newLines [][]byte
	flipped := 0
	for _, line := range bytes.Split(bytes.TrimRight(data, "\n"), []byte{'\n'}) {
		if len(line) == 0 {
			continue
		}
		var rec ReplayContradictionEntry
		if err := json.Unmarshal(line, &rec); err != nil {
			newLines = append(newLines, line)
			continue
		}
		if !rec.Reviewed && contradictionMatches(rec, entry, hints) {
			rec.Reviewed = true
			flipped++
		}
		out, err := json.Marshal(rec)
		if err != nil {
			newLines = append(newLines, line)
			continue
		}
		newLines = append(newLines, out)
	}

	if flipped == 0 {
		return 0, nil
	}

	var rewrite []byte
	for _, l := range newLines {
		rewrite = append(rewrite, l...)
		rewrite = append(rewrite, '\n')
	}
	if err := os.WriteFile(path, rewrite, 0o644); err != nil {
		return flipped, err
	}
	return flipped, nil
}

func contradictionMatches(rec ReplayContradictionEntry, entry *BufferEntry, hints []string) bool {
	for _, h := range hints {
		if h == "" {
			continue
		}
		if strings.Contains(filepath.ToSlash(rec.RecentSeedPath), h) {
			return true
		}
		if strings.Contains(filepath.ToSlash(rec.StableMemoryPath), h) {
			return true
		}
	}
	if !entry.Timestamp.IsZero() {
		dt := rec.Timestamp.Sub(entry.Timestamp)
		if dt < 0 {
			dt = -dt
		}
		if dt < 24*time.Hour {
			return true
		}
	}
	return false
}

// normalizeRelatedRef strips wikilink wrappers and Memory/Buffer prefixes
// for substring matching against an absolute file path. "[[Buffer/x]]"
// becomes "Buffer/x" so it matches "/vault/Buffer/x.md".
func normalizeRelatedRef(s string) string {
	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "[[")
	s = strings.TrimSuffix(s, "]]")
	s = strings.TrimSuffix(s, ".md")
	return s
}
