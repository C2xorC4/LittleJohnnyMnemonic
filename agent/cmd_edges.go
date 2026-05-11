package main

import (
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
)

// cmdEdges dispatches the `jm edges` subcommands. Currently supports
// `--inspect <memory_key>` which lists the outgoing edges of a memory
// with their base/authored/effective weights and usage counts.
//
// Used as the v0 audit surface for the adaptive-edge-weighting pilot:
// operators can see exactly why retrieval prioritised one neighbor
// over another, and confirm that out-of-scope relationships are
// unaffected by the adaptive layer.
func cmdEdges(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("edges", flag.ExitOnError)
	inspect := fs.String("inspect", "", "Inspect outgoing edges of a memory (path under vault root, e.g. Memory/Project/argus)")
	fs.Parse(args)

	if *inspect == "" {
		fmt.Fprintln(os.Stderr, "Usage: jm edges --inspect <memory_key>")
		fmt.Fprintln(os.Stderr, "  Example: jm edges --inspect Memory/Project/argus")
		os.Exit(1)
	}

	cfg := LoadConfig(vaultRoot)
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	// Locate the memory by normalised key.
	targetKey := strings.ToLower(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(*inspect), "[["), "]]"))
	targetKey = strings.TrimSuffix(targetKey, ".md")

	var target *MemoryEntry
	for _, m := range memories {
		if normalizeKey(m) == targetKey {
			target = m
			break
		}
	}
	if target == nil {
		fmt.Fprintf(os.Stderr, "Memory not found: %s\n", *inspect)
		fmt.Fprintln(os.Stderr, "  Hint: use the form 'Memory/Type/filename' as it appears under the vault root.")
		os.Exit(1)
	}

	// Load edge usage data if adaptive weighting is enabled (otherwise
	// the lookup is pointless and we want the column to show "n/a"
	// rather than silently zero).
	var usage map[string]EdgeUsage
	if cfg.AdaptiveEdgeWeightingEnabled {
		if u, err := LoadEdgeUsage(vaultRoot); err == nil {
			usage = u
		} else {
			fmt.Fprintf(os.Stderr, "[!] Warning: failed to load edge usage: %v\n", err)
		}
	}

	fmt.Printf("Memory: %s\n", target.Title)
	fmt.Printf("Key:    %s\n", normalizeKey(target))
	fmt.Printf("Type:   %s\n", target.Type)
	fmt.Printf("Adaptive weighting: %s", boolEnabledLabel(cfg.AdaptiveEdgeWeightingEnabled))
	if cfg.AdaptiveEdgeWeightingEnabled {
		fmt.Printf(" (scope: %s; alpha=%.2f; cap=%.2fx)\n",
			strings.Join(cfg.AdaptiveEdgeScope, ","), cfg.AdaptiveEdgeAlpha, cfg.AdaptiveEdgeCap)
	} else {
		fmt.Println()
	}
	fmt.Println()

	if len(target.Links) == 0 {
		fmt.Println("  (no outgoing links)")
		return
	}

	// Column headers
	fmt.Printf("%-50s %-14s %-7s %-9s %-8s %-9s\n",
		"TARGET", "RELATIONSHIP", "BASE", "OVERRIDE", "USAGE", "EFFECTIVE")
	fmt.Printf("%s\n", strings.Repeat("-", 100))

	sourceKey := normalizeKey(target)
	for _, link := range target.Links {
		targetK := normalizeLinkTarget(link.Target)
		baseWeight := cfg.EdgeWeights[link.Relationship]
		if baseWeight == 0 {
			baseWeight = 0.5
		}
		overrideStr := "-"
		if link.Weight != nil {
			overrideStr = fmt.Sprintf("%.2f", *link.Weight)
		}
		usageStr := "-"
		if cfg.AdaptiveEdgeWeightingEnabled && inAdaptiveScope(link.Relationship, cfg.AdaptiveEdgeScope) {
			if u, ok := usage[edgeUsageKey(sourceKey, targetK, link.Relationship)]; ok {
				usageStr = fmt.Sprintf("%d", u.UsageCount)
			} else {
				usageStr = "0"
			}
		}
		effective := effectiveEdgeWeight(sourceKey, targetK, link, cfg, usage)

		fmt.Printf("%-50s %-14s %-7.2f %-9s %-8s %-9.3f\n",
			truncate(targetK, 50), link.Relationship, baseWeight, overrideStr, usageStr, effective)
	}
}

func boolEnabledLabel(b bool) string {
	if b {
		return "enabled"
	}
	return "disabled"
}

// adaptiveEdgeStats returns summary information for the status command:
// how many edges currently have non-default effective weight, and the
// top N most-reinforced learned (or other in-scope) edges. The function
// is split out so cmd_status.go can call it without duplicating logic.
type adaptiveEdgeStat struct {
	Source     string
	Target     string
	Relationship string
	UsageCount int
	Effective  float64
	Base       float64
}

func adaptiveEdgeStats(cfg Config, vaultRoot string, topN int) (nonDefaultCount int, top []adaptiveEdgeStat) {
	if !cfg.AdaptiveEdgeWeightingEnabled {
		return 0, nil
	}
	usage, err := LoadEdgeUsage(vaultRoot)
	if err != nil || len(usage) == 0 {
		return 0, nil
	}

	for _, u := range usage {
		if !inAdaptiveScope(u.Relationship, cfg.AdaptiveEdgeScope) {
			continue
		}
		if u.UsageCount <= 0 {
			continue
		}
		base := cfg.EdgeWeights[u.Relationship]
		if base == 0 {
			base = 0.5
		}
		// We don't have the original Link object here, so no authored
		// override is applied — this is the multiplier-only view, which
		// is what the status display needs.
		link := Link{Target: u.Target, Relationship: u.Relationship}
		eff := effectiveEdgeWeight(u.Source, u.Target, link, cfg, usage)
		if eff != base {
			nonDefaultCount++
		}
		top = append(top, adaptiveEdgeStat{
			Source: u.Source, Target: u.Target, Relationship: u.Relationship,
			UsageCount: u.UsageCount, Effective: eff, Base: base,
		})
	}

	sort.Slice(top, func(i, j int) bool {
		if top[i].UsageCount != top[j].UsageCount {
			return top[i].UsageCount > top[j].UsageCount
		}
		return top[i].Effective > top[j].Effective
	})
	if topN > 0 && len(top) > topN {
		top = top[:topN]
	}
	return nonDefaultCount, top
}
