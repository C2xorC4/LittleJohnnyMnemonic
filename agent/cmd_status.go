package main

import (
	"flag"
	"fmt"
	"strings"
	"time"
)

func cmdStatus(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	fs.Parse(args)

	cfg := LoadConfig(vaultRoot)
	now := time.Now()

	title := "LittleJohnnyMnemonic - System Status"
	boxWidth := 50
	pad := boxWidth - len(title)
	left := pad / 2
	right := pad - left
	fmt.Printf("╔%s╗\n", strings.Repeat("═", boxWidth))
	fmt.Printf("║%s%s%s║\n", strings.Repeat(" ", left), title, strings.Repeat(" ", right))
	fmt.Printf("╚%s╝\n", strings.Repeat("═", boxWidth))
	fmt.Println()

	// Buffer status
	bufferEntries, _ := LoadAllBufferEntries(vaultRoot)
	bufferFill := float64(len(bufferEntries)) / float64(cfg.BufferThreshold) * 100
	bufferBar := progressBar(bufferFill, 20)

	fmt.Printf("Buffer:  %s %d/%d entries (%.0f%%)\n",
		bufferBar, len(bufferEntries), cfg.BufferThreshold, bufferFill)
	if len(bufferEntries) >= cfg.BufferThreshold {
		fmt.Println("         ⚠  THRESHOLD REACHED — consolidation needed")
	}

	// Count buffer context states
	orphans, partial, pinned := 0, 0, 0
	for _, e := range bufferEntries {
		switch e.ContextIntegrity {
		case ContextOrphan:
			orphans++
		case ContextPartial:
			partial++
		}
		if e.Pinned {
			pinned++
		}
	}
	if orphans > 0 || partial > 0 {
		fmt.Printf("         Context: %d full, %d partial, %d orphan",
			len(bufferEntries)-orphans-partial, partial, orphans)
		if pinned > 0 {
			fmt.Printf(", %d pinned", pinned)
		}
		fmt.Println()
	}

	// Memory counts by type
	memories, _ := LoadAllMemories(vaultRoot)
	typeCounts := make(map[MemoryType]int)
	overrides := 0
	totalLinks := 0
	unlinked := 0
	var oldestAccess time.Time
	staleCount := 0

	for _, m := range memories {
		typeCounts[m.Type]++
		if m.TrainingOverride {
			overrides++
		}
		totalLinks += len(m.Links)
		if len(m.Links) == 0 {
			unlinked++
		}
		daysSince := now.Sub(m.LastAccessed).Hours() / 24
		if daysSince > float64(cfg.StaleThresholdDays) {
			staleCount++
		}
		if oldestAccess.IsZero() || m.LastAccessed.Before(oldestAccess) {
			oldestAccess = m.LastAccessed
		}
	}

	fmt.Printf("\nMemory:  %d total\n", len(memories))
	typeOrder := []MemoryType{TypeUser, TypeFeedback, TypeProject, TypeReference, TypeSemantic, TypeKnowledge}
	for _, t := range typeOrder {
		if c, ok := typeCounts[t]; ok {
			fmt.Printf("         %-12s %d\n", t, c)
		}
	}
	if overrides > 0 {
		fmt.Printf("         overrides    %d\n", overrides)
	}

	// Graph stats
	fmt.Printf("\nGraph:   %d links across %d memories", totalLinks, len(memories))
	if unlinked > 0 {
		fmt.Printf(" (%d unlinked)", unlinked)
	}
	fmt.Println()

	if len(memories) > 0 {
		graph := BuildGraph(memories, cfg)
		clusters := graph.FindClusters()
		if len(clusters) > 0 {
			fmt.Printf("         %d concept clusters detected\n", len(clusters))
		}
	}

	// Health indicators
	fmt.Println("\nHealth:")
	if staleCount > 0 {
		fmt.Printf("         ⚠  %d memories stale (>%d days without access)\n",
			staleCount, cfg.StaleThresholdDays)
	} else {
		fmt.Println("         ✓ No stale memories")
	}

	if unlinked > 0 && len(memories) > 3 {
		pct := float64(unlinked) / float64(len(memories)) * 100
		if pct > 50 {
			fmt.Printf("         ⚠  %.0f%% of memories have no links — consider deep consolidation\n", pct)
		}
	}

	// Archive count
	archived, _ := LoadArchived(vaultRoot)
	if len(archived) > 0 {
		fmt.Printf("\nArchive: %d memories\n", len(archived))
	}

	// Scoring preview
	if len(memories) > 0 {
		scored := ScoreAllMemories(memories, nil, "", cfg, now)
		aboveThreshold := 0
		for _, s := range scored {
			if s.Total >= cfg.RetrievalThreshold {
				aboveThreshold++
			}
		}
		fmt.Printf("\nRetrieval: %d/%d above τ (%.1f) with no context tags\n",
			aboveThreshold, len(memories), cfg.RetrievalThreshold)
		if len(scored) > 0 {
			fmt.Printf("           Highest: %.3f (%s)\n", scored[0].Total, scored[0].Memory.Title)
			fmt.Printf("           Lowest:  %.3f (%s)\n",
				scored[len(scored)-1].Total, scored[len(scored)-1].Memory.Title)
		}
	}

	// Adaptive edge weighting status — surfaces pilot state so the
	// operator can see at a glance whether reinforcement is active
	// and how many edges have non-base effective weight.
	fmt.Println()
	if cfg.AdaptiveEdgeWeightingEnabled {
		nonDefault, top := adaptiveEdgeStats(cfg, vaultRoot, 5)
		fmt.Printf("Adaptive edges: enabled (scope: %s; alpha=%.2f; cap=%.2fx)\n",
			strings.Join(cfg.AdaptiveEdgeScope, ","), cfg.AdaptiveEdgeAlpha, cfg.AdaptiveEdgeCap)
		fmt.Printf("                %d edges with non-default effective weight\n", nonDefault)
		if len(top) > 0 {
			fmt.Println("                Top reinforced:")
			for _, s := range top {
				fmt.Printf("                  [%3d×] %s → %s (%s)  base=%.2f → eff=%.3f\n",
					s.UsageCount, truncate(s.Source, 30), truncate(s.Target, 30), s.Relationship, s.Base, s.Effective)
			}
		}
	} else {
		fmt.Println("Adaptive edges: disabled (master toggle: adaptive_edge_weighting_enabled)")
	}

	fmt.Println()
}

func cmdConfig(vaultRoot string, args []string) {
	cfg := DefaultConfig()

	fmt.Println("=== Current Configuration ===")

	fmt.Println("Retrieval:")
	fmt.Printf("  threshold (τ):      %.1f\n", cfg.RetrievalThreshold)
	fmt.Printf("  max loaded:         %d\n", cfg.MaxMemoriesLoaded)

	fmt.Println("\nConsolidation:")
	fmt.Printf("  buffer threshold:   %d\n", cfg.BufferThreshold)
	fmt.Printf("  depth:              %s\n", cfg.ConsolidationDepth)
	fmt.Printf("  max holds:          %d\n", cfg.MaxHolds)

	fmt.Println("\nContext Integrity:")
	fmt.Printf("  partial penalty:    %.1f\n", cfg.ContextPenaltyPartial)
	fmt.Printf("  orphan penalty:     %.1f\n", cfg.ContextPenaltyOrphan)
	fmt.Printf("  discard ambiguous:  %t\n", cfg.DiscardAmbiguousOrphans)

	fmt.Println("\nDecay Rates:")
	for typ, rate := range cfg.DecayRates {
		fmt.Printf("  %-18s  %.2f\n", typ, rate)
	}

	fmt.Println("\nConfidence:")
	fmt.Printf("  reinforce:          +%.1f\n", cfg.ConfidenceReinforce)
	fmt.Printf("  contradict:         -%.1f\n", cfg.ConfidenceContradict)
	fmt.Printf("  stale factor:       ×%.1f\n", cfg.ConfidenceStaleFactor)
	fmt.Printf("  stale threshold:    %d days\n", cfg.StaleThresholdDays)

	fmt.Println("\nTraining Overrides:")
	fmt.Printf("  confidence floor:   %.1f\n", cfg.OverrideConfidenceFloor)
	fmt.Printf("  immune to archival: %t\n", cfg.OverrideImmuneToArchival)

	fmt.Println("\nSpreading Activation:")
	fmt.Printf("  factor:             %.1f\n", cfg.SpreadingActivationFactor)
	fmt.Printf("  max hops:           %d\n", cfg.MaxActivationHops)
	fmt.Println("  edge weights:")
	for rel, w := range cfg.EdgeWeights {
		fmt.Printf("    %-18s  %.1f\n", rel, w)
	}
}

func progressBar(pct float64, width int) string {
	if pct > 100 {
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	return "[" + bar + "]"
}
