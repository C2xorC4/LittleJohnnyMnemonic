package main

import (
	"flag"
	"fmt"
	"os"
	"time"
)

func cmdMetricsBackfillUsage(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("metrics backfill-usage", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "report matches without writing")
	since := fs.String("since", "", "only sessions on/after date (YYYY-MM-DD)")
	zeroRecall := fs.Bool("zero-recall", true, "also backfill zero-recall prompts from session_heartbeat")
	grokHome := fs.String("grok-home", "", "Grok sessions root (default: $GROK_HOME or ~/.grok)")
	claudeHome := fs.String("claude-home", "", "Claude projects root (default: ~/.claude/projects)")
	fs.Usage = func() {
		fmt.Fprintln(fs.Output(), "Usage: jm metrics backfill-usage [flags]")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Replay retrieval_sessions.jsonl against saved host transcripts to backfill")
		fmt.Fprintln(fs.Output(), "Metrics/memory_usage_log.jsonl with historical usage rows (dated timestamps).")
		fmt.Fprintln(fs.Output(), "Requires retrieval session logs + transcripts still on disk.")
		fmt.Fprintln(fs.Output(), "")
		fmt.Fprintln(fs.Output(), "Flags:")
		fs.PrintDefaults()
	}
	_ = fs.Parse(args)

	opts := backfillUsageOpts{
		dryRun:     *dryRun,
		grokHome:   *grokHome,
		claudeHome: *claudeHome,
	}
	if *since != "" {
		t, err := time.Parse("2006-01-02", *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "metrics backfill-usage: invalid --since: %v\n", err)
			os.Exit(1)
		}
		opts.since = t.UTC()
	}

	res, err := backfillMemoryUsage(vaultRoot, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "metrics backfill-usage: %v\n", err)
		os.Exit(1)
	}

	prefix := ""
	if *dryRun {
		prefix = "[dry-run] "
	}
	fmt.Printf("%susage backfill: scanned=%d eligible=%d matched=%d written=%d skipped_existing=%d no_transcript=%d no_turn_match=%d no_assistant=%d\n",
		prefix, res.Scanned, res.Eligible, res.Matched, res.Written,
		res.SkippedExist, res.NoTranscript, res.NoTurnMatch, res.NoAssistant)

	if *zeroRecall {
		candidates, written, err := backfillZeroRecallFromHeartbeat(vaultRoot, *dryRun, opts.since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "metrics backfill-usage zero-recall: %v\n", err)
			os.Exit(1)
		}
		if *dryRun {
			fmt.Printf("%szero-recall backfill: would append %d recall_log rows\n", prefix, candidates)
		} else if written > 0 {
			fmt.Printf("zero-recall backfill: appended %d recall_log rows\n", written)
		} else {
			fmt.Println("zero-recall backfill: nothing to append")
		}
	}

	if !*dryRun && (res.Written > 0 || *zeroRecall) {
		MaybeRefreshDashboard(vaultRoot, dashReasonCompact)
	}
}