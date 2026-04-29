package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdDecay(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("decay", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "Show decay effects without applying")
	fs.Parse(args)

	cfg := LoadConfig(vaultRoot)
	ApplyConfigCompressionThresholds(cfg)
	now := time.Now()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("=== Decay Pass (%d memories) ===\n\n", len(memories))
	fmt.Printf("%-50s %8s %8s %10s %6s → %-10s %s\n",
		"MEMORY", "CONF", "NEW_CONF", "AGE_DAYS", "FID", "TARGET", "ACTION")
	fmt.Println(strings.Repeat("─", 110))

	confidenceModified := 0
	compressionQueued := 0

	for _, m := range memories {
		daysSinceAccess := now.Sub(m.LastAccessed).Hours() / 24
		isStale := daysSinceAccess > float64(cfg.StaleThresholdDays)
		oldConf := m.Confidence
		action := ""
		changed := false

		// --- Confidence adjustments (independent of fidelity) ---

		// Staleness decay for project memories
		if isStale && m.Type == TypeProject {
			m.Confidence *= cfg.ConfidenceStaleFactor
			action = "conf-decayed"
			confidenceModified++
			changed = true
		}

		// Training override confidence floor
		if m.TrainingOverride && m.Confidence < cfg.OverrideConfidenceFloor {
			m.Confidence = cfg.OverrideConfidenceFloor
			action = "floor-override"
			confidenceModified++
			changed = true
		}

		// Profile confidence floor
		if m.Profile && m.Confidence < cfg.ProfileConfidenceFloor {
			m.Confidence = cfg.ProfileConfidenceFloor
			action = "floor-profile"
			confidenceModified++
			changed = true
		}

		// --- Progressive fidelity classification (replaces archival) ---

		currentFid := currentFidelity(m)
		targetFid := ClassifyTargetFidelity(m, now)

		fidelityAction := ""
		if IsCompressionImmune(m) {
			fidelityAction = "immune"
		} else if FidelityIsHigherThan(targetFid, currentFid) {
			// Compression is needed — mark the target fidelity for a later
			// compress pass. The decay command itself never transforms content.
			if m.TargetFidelity != targetFid {
				m.TargetFidelity = targetFid
				compressionQueued++
				changed = true
			}
			fidelityAction = "queue→" + targetFid
		} else if m.TargetFidelity != "" && !FidelityIsHigherThan(m.TargetFidelity, currentFid) {
			// Target fidelity is no longer ahead of current — clear stale mark.
			// This happens when a compress pass brings the memory up to its
			// target, or when the target was reset by access.
			m.TargetFidelity = ""
			changed = true
			fidelityAction = "clear"
		}

		// Merge action labels for display
		if fidelityAction != "" && action != "" {
			action = action + ", " + fidelityAction
		} else if fidelityAction != "" {
			action = fidelityAction
		}

		// Only display rows where something happened or the memory is aging
		shouldShow := changed || isStale || fidelityAction != "immune"
		if !shouldShow {
			continue
		}

		fmt.Printf("%-50s %8.2f %8.2f %10.1f %6s → %-10s %s\n",
			truncate(m.Title, 50), oldConf, m.Confidence,
			daysSinceAccess, currentFid, targetFid, action)

		if !*dryRun && changed {
			if err := WriteMemoryEntry(m); err != nil {
				fmt.Fprintf(os.Stderr, "[!] %s: %v\n", m.FileName, err)
			}
		}
	}

	fmt.Println(strings.Repeat("─", 110))
	fmt.Printf("Confidence adjustments: %d | Compression queued: %d\n",
		confidenceModified, compressionQueued)
	if compressionQueued > 0 {
		fmt.Println("\nNext step: run `jm compress` to apply pending fidelity transitions.")
		fmt.Println("           (decay only classifies; compression is a separate Claude-driven pass)")
	}
	if *dryRun {
		fmt.Println("[DRY RUN] No changes written.")
	}
}
