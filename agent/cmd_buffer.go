package main

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

func cmdBuffer(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("buffer", flag.ExitOnError)
	verbose := fs.Bool("v", false, "Show full buffer entry details")
	assess := fs.Bool("assess", false, "Assess buffer density and recommend consolidation timing")
	fs.Parse(args)

	cfg := DefaultConfig()

	entries, err := LoadAllBufferEntries(vaultRoot)
	if err != nil {
		fmt.Printf("Buffer is empty or not accessible.\n")
		return
	}

	if len(entries) == 0 {
		fmt.Println("Buffer is empty.")
		return
	}

	if *assess {
		assessBufferDensity(entries, cfg)
		return
	}

	fmt.Printf("=== Buffer Status (%d/%d entries) ===\n\n",
		len(entries), cfg.BufferThreshold)

	if len(entries) >= cfg.BufferThreshold {
		fmt.Println("⚠  BUFFER THRESHOLD REACHED — consolidation recommended")
	}

	now := time.Now()

	fmt.Printf("%-35s %10s %6s %8s %5s %5s\n",
		"ENTRY", "AGE", "SURP", "CONTEXT", "PINS", "HOLDS")
	fmt.Println(strings.Repeat("─", 80))

	pinned := 0
	held := 0
	orphans := 0

	for _, e := range entries {
		age := now.Sub(e.Timestamp)
		ageStr := formatDuration(age)

		pin := ""
		if e.Pinned {
			pin = "📌"
			pinned++
		}

		holdStr := ""
		if e.HoldCount > 0 {
			holdStr = fmt.Sprintf("%d/%d", e.HoldCount, cfg.MaxHolds)
			held++
		}

		if e.ContextIntegrity == ContextOrphan {
			orphans++
		}

		name := strings.TrimSuffix(e.FileName, ".md")
		if len(name) > 33 {
			name = name[:30] + "..."
		}

		fmt.Printf("%-35s %10s %6.1f %8s %5s %5s\n",
			name, ageStr, e.Surprise, e.ContextIntegrity, pin, holdStr)

		if *verbose {
			fmt.Printf("  Tags: %s\n", strings.Join(e.Tags, ", "))
			body := e.Body
			if len(body) > 120 {
				body = body[:117] + "..."
			}
			fmt.Printf("  %s\n\n", body)
		}
	}

	fmt.Println(strings.Repeat("─", 80))
	fmt.Printf("Total: %d | Pinned: %d | Held: %d | Orphans: %d | Threshold: %d\n",
		len(entries), pinned, held, orphans, cfg.BufferThreshold)
}

func assessBufferDensity(entries []*BufferEntry, cfg Config) {
	now := time.Now()

	// Compute metrics
	totalSurprise := 0.0
	oldestAge := time.Duration(0)
	daydreamCount := 0
	sourceMap := make(map[string]int)
	tagCounts := make(map[string]int)

	for _, e := range entries {
		totalSurprise += e.Surprise
		age := now.Sub(e.Timestamp)
		if age > oldestAge {
			oldestAge = age
		}
		sourceMap[e.Source]++
		for _, t := range e.Tags {
			tagCounts[strings.ToLower(t)]++
		}
		for _, t := range e.Tags {
			if t == "daydream" {
				daydreamCount++
				break
			}
		}
	}

	avgSurprise := totalSurprise / float64(len(entries))

	// Find tag clusters (tags appearing in 3+ entries = topic cluster)
	var clusters []string
	for tag, count := range tagCounts {
		if count >= 3 {
			clusters = append(clusters, fmt.Sprintf("%s(%d)", tag, count))
		}
	}

	// Score density (0-100)
	score := 0.0

	// Entry count pressure (linear up to threshold)
	countPressure := float64(len(entries)) / float64(cfg.BufferThreshold) * 30
	if countPressure > 30 {
		countPressure = 30
	}
	score += countPressure

	// Age pressure (oldest entry > 24h is significant)
	agePressure := oldestAge.Hours() / 24.0 * 20
	if agePressure > 25 {
		agePressure = 25
	}
	score += agePressure

	// Surprise weight (high-surprise entries need consolidation sooner)
	surprisePressure := avgSurprise * 20
	score += surprisePressure

	// Topic clustering (clustered entries are ready to consolidate together)
	clusterPressure := float64(len(clusters)) * 8
	if clusterPressure > 25 {
		clusterPressure = 25
	}
	score += clusterPressure

	// Output
	fmt.Printf("=== Buffer Density Assessment ===\n\n")
	fmt.Printf("Entries:          %d / %d\n", len(entries), cfg.BufferThreshold)
	fmt.Printf("Oldest entry:     %s\n", formatDuration(oldestAge))
	fmt.Printf("Avg surprise:     %.2f\n", avgSurprise)
	fmt.Printf("Daydream entries: %d\n", daydreamCount)

	if len(clusters) > 0 {
		fmt.Printf("Topic clusters:   %s\n", strings.Join(clusters, ", "))
	} else {
		fmt.Printf("Topic clusters:   none (entries are dispersed)\n")
	}

	fmt.Printf("\nDensity score:    %.0f / 100\n", score)
	fmt.Printf("  Count pressure: %.0f / 30\n", countPressure)
	fmt.Printf("  Age pressure:   %.0f / 25\n", agePressure)
	fmt.Printf("  Surprise:       %.0f / 20\n", surprisePressure)
	fmt.Printf("  Clustering:     %.0f / 25\n", clusterPressure)

	fmt.Println()
	if score >= 70 {
		fmt.Println("⚠  CONSOLIDATION RECOMMENDED — high density, entries at risk of context decay")
	} else if score >= 45 {
		fmt.Println("●  Consolidation advisable — buffer is accumulating meaningful content")
	} else if score >= 25 {
		fmt.Println("◇  Buffer is healthy — consolidate at natural break or threshold")
	} else {
		fmt.Println("○  Buffer is light — no consolidation pressure")
	}

	// Specific recommendations
	if oldestAge.Hours() > 48 {
		fmt.Println("\n⚠  Oldest entry is >48h old — context integrity may be degrading")
	}
	if len(clusters) >= 2 {
		fmt.Println("\n●  Multiple topic clusters detected — narrative synthesis opportunity")
	}
	if daydreamCount > 0 {
		fmt.Printf("\n◇  %d daydream entries pending review\n", daydreamCount)
	}
}

func formatDuration(d time.Duration) string {
	hours := d.Hours()
	if hours < 1 {
		return fmt.Sprintf("%.0fm", d.Minutes())
	}
	if hours < 24 {
		return fmt.Sprintf("%.0fh", hours)
	}
	return fmt.Sprintf("%.0fd", hours/24)
}
