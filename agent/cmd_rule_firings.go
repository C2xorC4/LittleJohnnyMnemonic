package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// cmdRuleFirings aggregates the Metrics/rule_firings.jsonl log into a
// human-readable summary. The log has two-stage records per firing:
// a "pattern" stage (written synchronously by the Stop hook) and a
// "judge" stage (written asynchronously by the judge subprocess).
// Records are joined by firing_id.
func cmdRuleFirings(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("rule-firings", flag.ExitOnError)
	recent := fs.Int("recent", 0, "Show N most-recent firings per rule with excerpts (0 = summary only)")
	rule := fs.String("rule", "", "Filter to a single rule ID")
	since := fs.String("since", "", "Only include firings on/after this ISO date (e.g. 2026-04-14)")
	fs.Parse(args)

	logPath := ruleFiringsLogPath(vaultRoot)
	firings, err := loadFirings(logPath)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No rule firings log yet — Metrics/rule_firings.jsonl does not exist.")
			return
		}
		fmt.Fprintf(os.Stderr, "[!] Failed to read firings log: %v\n", err)
		os.Exit(1)
	}

	if *since != "" {
		cutoff, err := time.Parse("2006-01-02", *since)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] invalid --since date: %v\n", err)
			os.Exit(1)
		}
		firings = filterFirings(firings, cutoff, *rule)
	} else if *rule != "" {
		firings = filterFirings(firings, time.Time{}, *rule)
	}

	joined := joinFiringsByID(firings)
	if len(joined) == 0 {
		fmt.Println("No firings match the filter.")
		return
	}

	printRuleFiringsSummary(joined, *recent)
}

// loadFirings reads the JSONL log into a flat slice of records.
func loadFirings(path string) ([]RuleFiring, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var out []RuleFiring
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var f RuleFiring
		if err := json.Unmarshal([]byte(line), &f); err != nil {
			fmt.Fprintf(os.Stderr, "[!] line %d: parse error: %v\n", lineNum, err)
			continue
		}
		out = append(out, f)
	}
	return out, scanner.Err()
}

func filterFirings(firings []RuleFiring, since time.Time, rule string) []RuleFiring {
	var out []RuleFiring
	for _, f := range firings {
		if !since.IsZero() && f.Timestamp.Before(since) {
			continue
		}
		if rule != "" && f.RuleID != rule {
			continue
		}
		out = append(out, f)
	}
	return out
}

// joinedFiring combines pattern and judge records for a single firing_id.
type joinedFiring struct {
	FiringID              string
	RuleID                string
	SessionID             string
	PatternTimestamp      time.Time
	JudgeTimestamp        time.Time
	FireSignalsMatched    []string
	ContextSignalsMatched []string
	Excerpt               string
	Verdict               string // confirmed|rejected|uncertain|error|pending
	JudgeReason           string
	JudgeError            string
}

// joinFiringsByID groups two-stage records by firing_id. Returns a slice
// sorted by pattern timestamp descending (newest first).
func joinFiringsByID(firings []RuleFiring) []joinedFiring {
	byID := make(map[string]*joinedFiring)
	for _, f := range firings {
		j, ok := byID[f.FiringID]
		if !ok {
			j = &joinedFiring{
				FiringID:  f.FiringID,
				RuleID:    f.RuleID,
				SessionID: f.SessionID,
				Verdict:   "pending",
			}
			byID[f.FiringID] = j
		}
		switch f.Stage {
		case "pattern":
			j.PatternTimestamp = f.Timestamp
			j.FireSignalsMatched = f.FireSignalsMatched
			j.ContextSignalsMatched = f.ContextSignalsMatched
			j.Excerpt = f.Excerpt
		case "judge":
			j.JudgeTimestamp = f.Timestamp
			if f.Verdict != "" {
				j.Verdict = f.Verdict
			}
			j.JudgeReason = f.JudgeReason
			j.JudgeError = f.JudgeError
		}
	}

	out := make([]joinedFiring, 0, len(byID))
	for _, j := range byID {
		out = append(out, *j)
	}
	sort.Slice(out, func(i, k int) bool {
		return out[i].PatternTimestamp.After(out[k].PatternTimestamp)
	})
	return out
}

// printRuleFiringsSummary renders the summary report.
func printRuleFiringsSummary(firings []joinedFiring, recent int) {
	byRule := make(map[string][]joinedFiring)
	for _, f := range firings {
		byRule[f.RuleID] = append(byRule[f.RuleID], f)
	}

	ruleIDs := make([]string, 0, len(byRule))
	for id := range byRule {
		ruleIDs = append(ruleIDs, id)
	}
	sort.Strings(ruleIDs)

	fmt.Printf("=== Rule Firings (%d total across %d rules) ===\n\n", len(firings), len(ruleIDs))

	for _, id := range ruleIDs {
		list := byRule[id]
		counts := tallyVerdicts(list)
		total := len(list)

		fmt.Printf("Rule: %s\n", id)
		fmt.Printf("  Total firings: %d\n", total)
		printVerdictLine("Confirmed", counts["confirmed"], total)
		printVerdictLine("Rejected ", counts["rejected"], total)
		printVerdictLine("Uncertain", counts["uncertain"], total)
		if counts["error"] > 0 {
			printVerdictLine("Error    ", counts["error"], total)
		}
		if counts["pending"] > 0 {
			printVerdictLine("Pending  ", counts["pending"], total)
		}
		if len(list) > 0 {
			fmt.Printf("  Last firing:   %s\n", list[0].PatternTimestamp.Format("2006-01-02 15:04 UTC"))
		}
		fmt.Println()

		if recent > 0 {
			fmt.Printf("  Recent firings (up to %d):\n", recent)
			n := recent
			if n > len(list) {
				n = len(list)
			}
			for i := 0; i < n; i++ {
				f := list[i]
				fmt.Printf("    [%s] %s  verdict=%s\n",
					f.PatternTimestamp.Format("2006-01-02 15:04"),
					f.FiringID,
					f.Verdict)
				if len(f.FireSignalsMatched) > 0 {
					fmt.Printf("      signals: %s\n", strings.Join(f.FireSignalsMatched, ", "))
				}
				if f.JudgeReason != "" {
					fmt.Printf("      reason: %s\n", f.JudgeReason)
				}
				if f.JudgeError != "" {
					fmt.Printf("      error:  %s\n", f.JudgeError)
				}
				if f.Excerpt != "" {
					fmt.Printf("      excerpt: %s\n", excerptOneLine(f.Excerpt, 180))
				}
			}
			fmt.Println()
		}
	}
}

func tallyVerdicts(firings []joinedFiring) map[string]int {
	counts := make(map[string]int)
	for _, f := range firings {
		counts[f.Verdict]++
	}
	return counts
}

func printVerdictLine(label string, count, total int) {
	if count == 0 {
		fmt.Printf("  %s: %d\n", label, count)
		return
	}
	pct := float64(count) / float64(total) * 100
	fmt.Printf("  %s: %d (%.0f%%)\n", label, count, pct)
}

// excerptOneLine collapses an excerpt to a single line for recent-firing display.
func excerptOneLine(text string, max int) string {
	text = strings.ReplaceAll(strings.TrimSpace(text), "\n", " ")
	text = strings.Join(strings.Fields(text), " ")
	if len(text) <= max {
		return text
	}
	return text[:max] + "…"
}
