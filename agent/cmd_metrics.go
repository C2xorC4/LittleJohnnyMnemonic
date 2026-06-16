package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
)

func cmdMetrics(vaultRoot string, args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: jm metrics <subcommand> [flags]")
		fmt.Fprintln(os.Stderr, "subcommands:")
		fmt.Fprintln(os.Stderr, "  compact     compress per-prompt recall entries older than the retention window into daily aggregates")
		fmt.Fprintln(os.Stderr, "  compact-retrieval-sessions  archive and rewrite retrieval_sessions.jsonl (drop judge-prompt pollution)")
		fmt.Fprintln(os.Stderr, "  dashboard   generate a self-contained static HTML metrics dashboard")
		fmt.Fprintln(os.Stderr, "  serve       serve a live metrics dashboard over HTTP with SSE push updates")
		os.Exit(1)
	}
	switch args[0] {
	case "compact":
		cmdMetricsCompact(vaultRoot, args[1:])
	case "compact-retrieval-sessions":
		cmdMetricsCompactRetrievalSessions(vaultRoot, args[1:])
	case "dashboard":
		cmdMetricsDashboard(vaultRoot, args[1:])
	case "serve":
		cmdMetricsServe(vaultRoot, args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown metrics subcommand: %s\n", args[0])
		os.Exit(1)
	}
}

func cmdMetricsCompact(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("metrics compact", flag.ExitOnError)
	cfg := LoadConfig(vaultRoot)

	window := fs.Int("window", cfg.RecallLogRetentionDays, "retain granular entries for this many days; compress older entries into daily aggregates")
	dryRun := fs.Bool("dry-run", false, "report what would be compacted without writing")
	_ = fs.Parse(args)

	logPath := filepath.Join(vaultRoot, cfg.RecallTrackingLogPath)
	n, err := compactRecallLog(logPath, *window, *dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics compact: %v\n", err)
		os.Exit(1)
	}

	if n == 0 {
		fmt.Printf("nothing to compact (all entries within %d-day window)\n", *window)
		return
	}

	if *dryRun {
		fmt.Printf("[dry-run] would compact %d granular entries older than %d days into daily aggregates\n", n, *window)
	} else {
		fmt.Printf("compacted %d granular entries older than %d days into daily aggregates\n", n, *window)
	}
}

func cmdMetricsCompactRetrievalSessions(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("metrics compact-retrieval-sessions", flag.ExitOnError)
	cfg := LoadConfig(vaultRoot)
	retention := fs.Int("retention", cfg.RetrievalSessionLogRetentionDays, "drop conversational sessions older than N days (citation-linked sessions are kept)")
	dryRun := fs.Bool("dry-run", false, "report what would be kept/dropped without writing")
	_ = fs.Parse(args)

	kept, dropped, archive, err := CompactRetrievalSessionLog(vaultRoot, *retention, *dryRun)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics compact-retrieval-sessions: %v\n", err)
		os.Exit(1)
	}
	if dropped == 0 {
		fmt.Println("nothing to compact (no judge-prompt pollution or stale conversational entries)")
		return
	}
	if *dryRun {
		fmt.Printf("[dry-run] would keep %d sessions, drop %d\n", kept, dropped)
		return
	}
	fmt.Printf("kept %d sessions, dropped %d", kept, dropped)
	if archive != "" {
		fmt.Printf("; archived prior log to %s", archive)
	}
	fmt.Println()
}
