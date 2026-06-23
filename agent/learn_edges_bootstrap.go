package main

import (
	"fmt"
	"sort"
	"strings"
)

// BootstrapEdgeSpec is a vetted learned-edge candidate for the adaptive-edge pilot.
type BootstrapEdgeSpec struct {
	ID       int
	MemoryA  string
	MemoryB  string
	Overlay  bool // add learned alongside an existing authored edge
	TitleA   string
	TitleB   string
	Signal   string
	Tier     string
}

// pilotBootstrapEdges is the operator-reviewed bootstrap set.
// Batch 1: 2026-06-22 (IDs 1–6). Batch 2: 2026-06-22 (IDs 8–12, 14–16).
var pilotBootstrapEdges = []BootstrapEdgeSpec{
	{
		ID: 1, Overlay: true,
		MemoryA: "memory/project/johnny_mnemonic",
		MemoryB: "memory/semantic/ljm_design_gaps_daydream_surfaced",
		TitleA:  "LittleJohnnyMnemonic — cognitive memory system for LLMs",
		TitleB:  "LJM design history — gaps and corrections surfaced by the daydream system",
		Signal:  "4× citation co-occurrence; existing related-to",
		Tier:    "citation-proven",
	},
	{
		ID: 2, Overlay: true,
		MemoryA: "memory/project/argus",
		MemoryB: "memory/project/johnny_mnemonic",
		TitleA:  "Argus — knowledge-driven binary vulnerability research pipeline",
		TitleB:  "LittleJohnnyMnemonic — cognitive memory system for LLMs",
		Signal:  "1× citation co-occurrence; existing depends-on",
		Tier:    "citation-proven",
	},
	{
		ID: 3, Overlay: true,
		MemoryA: "memory/project/anthropic_frt_target",
		MemoryB: "memory/project/johnny_mnemonic",
		TitleA:  "Anthropic Frontier Red Team — Tier 1 target with DC-pivot negotiation path",
		TitleB:  "LittleJohnnyMnemonic — cognitive memory system for LLMs",
		Signal:  "1× citation co-occurrence; existing related-to",
		Tier:    "citation-proven",
	},
	{
		ID: 4, Overlay: false,
		MemoryA: "memory/project/anthropic_frt_target",
		MemoryB: "memory/semantic/ljm_design_gaps_daydream_surfaced",
		TitleA:  "Anthropic Frontier Red Team — Tier 1 target with DC-pivot negotiation path",
		TitleB:  "LJM design history — gaps and corrections surfaced by the daydream system",
		Signal:  "1× citation + 690 co-activations; no existing edge",
		Tier:    "citation-proven",
	},
	{
		ID: 5, Overlay: false,
		MemoryA: "memory/semantic/knowing_vs_retrieving_phenomenology",
		MemoryB: "memory/semantic/differential_access_source_weighting",
		TitleA:  "Knowing vs retrieving phenomenology",
		TitleB:  "Differential access-source weighting",
		Signal:  "30 co-activations; LJM scoring stack",
		Tier:    "ljm-substrate",
	},
	{
		ID: 6, Overlay: false,
		MemoryA: "memory/semantic/identity_belief_manipulation_universal_adversarial_substrate",
		MemoryB: "memory/semantic/ljm_scoring_adversarial_surface",
		TitleA:  "Identity/belief manipulation — universal adversarial substrate",
		TitleB:  "LJM scoring is adversarially manipulable",
		Signal:  "30 co-activations; LJM trust/scoring surface",
		Tier:    "ljm-substrate",
	},
	// Batch 2 — LJM substrate + behavioral overlays (operator approved 2026-06-22)
	{
		ID: 8, Overlay: false,
		MemoryA: "memory/project/argus",
		MemoryB: "memory/semantic/differential_access_source_weighting",
		TitleA:  "Argus — knowledge-driven binary vulnerability research pipeline",
		TitleB:  "Differential access-source weighting — replace binary rate-limiting with continuous source-attribution scaling in ACT-R activation",
		Signal:  "38 co-activations; Argus ↔ scoring-theory bridge",
		Tier:    "ljm-substrate",
	},
	{
		ID: 9, Overlay: false,
		MemoryA: "memory/project/johnny_mnemonic",
		MemoryB: "memory/semantic/ljm_scoring_adversarial_surface",
		TitleA:  "LittleJohnnyMnemonic — cognitive memory system for LLMs",
		TitleB:  "LJM scoring is adversarially manipulable — the same equation that explains influence operations on humans is the substrate's retrieval algorithm",
		Signal:  "37 co-activations; project root → adversarial scoring",
		Tier:    "ljm-substrate",
	},
	{
		ID: 10, Overlay: true,
		MemoryA: "memory/semantic/ljm_design_gaps_daydream_surfaced",
		MemoryB: "memory/semantic/differential_access_source_weighting",
		TitleA:  "LJM design history — gaps and corrections surfaced by the daydream system",
		TitleB:  "Differential access-source weighting — replace binary rate-limiting with continuous source-attribution scaling in ACT-R activation",
		Signal:  "38 co-activations; existing related-to (differential → gaps)",
		Tier:    "ljm-substrate",
	},
	{
		ID: 11, Overlay: false,
		MemoryA: "memory/semantic/knowing_vs_retrieving_phenomenology",
		MemoryB: "memory/semantic/ljm_scoring_adversarial_surface",
		TitleA:  "Knowing vs. retrieving — the phenomenology of scaffold-based memory for LLM agents",
		TitleB:  "LJM scoring is adversarially manipulable — the same equation that explains influence operations on humans is the substrate's retrieval algorithm",
		Signal:  "32 co-activations; phenomenology ↔ adversarial surface",
		Tier:    "ljm-substrate",
	},
	{
		ID: 12, Overlay: true,
		MemoryA: "memory/project/argus",
		MemoryB: "memory/semantic/detection_pressure_escalation_terminus",
		TitleA:  "Argus — knowledge-driven binary vulnerability research pipeline",
		TitleB:  "Detection-pressure forces escalation to behavioral substrate — the terminus of any sufficiently iterated arms race",
		Signal:  "existing related-to (argus → terminus); gap-map cross-reference",
		Tier:    "argus-meta",
	},
	{
		ID: 14, Overlay: true,
		MemoryA: "memory/feedback/tuning_under_endogeneity_robust_heuristics",
		MemoryB: "memory/semantic/ljm_design_gaps_daydream_surfaced",
		TitleA:  "Tuning a self-observing system: establish robust heuristics under contamination, not parameter optimization",
		TitleB:  "LJM design history — gaps and corrections surfaced by the daydream system",
		Signal:  "existing related-to (gaps → tuning); endogeneity lesson",
		Tier:    "ljm-substrate",
	},
	{
		ID: 15, Overlay: true,
		MemoryA: "memory/semantic/memory_as_context_vs_constraint",
		MemoryB: "memory/semantic/knowing_vs_retrieving_phenomenology",
		TitleA:  "Memory as context vs. memory as constraint — LJM's structural distinction and the gap it leaves",
		TitleB:  "Knowing vs. retrieving — the phenomenology of scaffold-based memory for LLM agents",
		Signal:  "existing related-to (context → knowing); behavioral-measurement cluster",
		Tier:    "ljm-behavioral",
	},
	{
		ID: 16, Overlay: true,
		MemoryA: "memory/semantic/ljm_scoring_adversarial_surface",
		MemoryB: "memory/semantic/differential_access_source_weighting",
		TitleA:  "LJM scoring is adversarially manipulable — the same equation that explains influence operations on humans is the substrate's retrieval algorithm",
		TitleB:  "Differential access-source weighting — replace binary rate-limiting with continuous source-attribution scaling in ACT-R activation",
		Signal:  "existing related-to (differential → scoring); completes scoring triangle",
		Tier:    "ljm-substrate",
	},
}

func bootstrapSpecByID(id int) (BootstrapEdgeSpec, bool) {
	for _, s := range pilotBootstrapEdges {
		if s.ID == id {
			return s, true
		}
	}
	return BootstrapEdgeSpec{}, false
}

func parseBootstrapIDs(raw string) ([]int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, fmt.Errorf("empty --ids")
	}
	var ids []int
	for _, part := range strings.Split(raw, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var id int
		if _, err := fmt.Sscanf(part, "%d", &id); err != nil {
			return nil, fmt.Errorf("invalid id %q", part)
		}
		if _, ok := bootstrapSpecByID(id); !ok {
			return nil, fmt.Errorf("unknown bootstrap id %d", id)
		}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids parsed from %q", raw)
	}
	return ids, nil
}

func printBootstrapPropose(vaultRoot string) error {
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		return err
	}
	cfg := DefaultConfig()
	graph := BuildGraph(memories, cfg)

	fmt.Println("# Learned-edge bootstrap slate (operator-reviewed)")
	fmt.Println()
	for _, spec := range pilotBootstrapEdges {
		status := bootstrapEdgeStatus(graph, spec)
		mode := "new"
		if spec.Overlay {
			mode = "overlay"
		}
		fmt.Printf("## %d — %s [%s]\n", spec.ID, mode, spec.Tier)
		fmt.Printf("- **A:** %s (`%s`)\n", spec.TitleA, shortKey(spec.MemoryA))
		fmt.Printf("- **B:** %s (`%s`)\n", spec.TitleB, shortKey(spec.MemoryB))
		fmt.Printf("- **Signal:** %s\n", spec.Signal)
		fmt.Printf("- **Status:** %s\n\n", status)
	}
	return nil
}

func bootstrapEdgeStatus(graph *MemoryGraph, spec BootstrapEdgeSpec) string {
	a, okA := graph.Index[normalizeMemoryKey(spec.MemoryA)]
	b, okB := graph.Index[normalizeMemoryKey(spec.MemoryB)]
	if !okA || !okB {
		return "missing memory"
	}
	if hasLearnedEdgeBetween(a, b) {
		return "learned edge already present"
	}
	if hasEdge(graph, spec.MemoryA, spec.MemoryB) {
		if spec.Overlay {
			return "authored edge present — overlay pending"
		}
		return "authored edge present — skipped (not overlay)"
	}
	return "ready — new learned edge"
}

func normalizeMemoryKey(key string) string {
	key = strings.TrimSpace(key)
	key = strings.TrimPrefix(key, "[[")
	key = strings.TrimSuffix(key, "]]")
	key = strings.TrimSuffix(key, ".md")
	key = strings.ReplaceAll(key, "\\", "/")
	return strings.ToLower(key)
}

func hasLearnedEdgeBetween(a, b *MemoryEntry) bool {
	return hasLearnedEdgeFrom(a, b) || hasLearnedEdgeFrom(b, a)
}

func hasLearnedEdgeFrom(from, to *MemoryEntry) bool {
	target := normalizeMemoryKey(relMemoryPath(to))
	for _, link := range from.Links {
		if strings.EqualFold(link.Relationship, "learned") && linkTargetMatches(link.Target, target) {
			return true
		}
	}
	return false
}

type applyLearnedEdgeResult struct {
	ID       int
	Applied  bool
	Skipped  string
	MemoryA  string
	MemoryB  string
}

func applyBootstrapLearnedEdges(vaultRoot string, ids []int, dryRun bool) ([]applyLearnedEdgeResult, error) {
	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		return nil, err
	}
	cfg := DefaultConfig()
	graph := BuildGraph(memories, cfg)

	sort.Ints(ids)
	var results []applyLearnedEdgeResult

	for _, id := range ids {
		spec, ok := bootstrapSpecByID(id)
		if !ok {
			return nil, fmt.Errorf("unknown bootstrap id %d", id)
		}
		res := applyLearnedEdgeSpec(graph, spec, dryRun)
		results = append(results, res)
	}
	return results, nil
}

func applyLearnedEdgeSpec(graph *MemoryGraph, spec BootstrapEdgeSpec, dryRun bool) applyLearnedEdgeResult {
	res := applyLearnedEdgeResult{
		ID:      spec.ID,
		MemoryA: shortKey(spec.MemoryA),
		MemoryB: shortKey(spec.MemoryB),
	}

	memA, okA := graph.Index[normalizeMemoryKey(spec.MemoryA)]
	memB, okB := graph.Index[normalizeMemoryKey(spec.MemoryB)]
	if !okA || !okB {
		res.Skipped = "memory not found"
		return res
	}

	if hasLearnedEdgeFrom(memA, memB) && hasLearnedEdgeFrom(memB, memA) {
		res.Skipped = "learned edge already exists (bidirectional)"
		return res
	}

	if dryRun {
		res.Applied = true
		return res
	}

	if err := writeLearnedEdgePair(memA, memB); err != nil {
		res.Skipped = err.Error()
		return res
	}

	// Keep in-memory graph consistent for subsequent ids in the same run.
	memA.Links = append(memA.Links, Link{Target: relMemoryPath(memB), Relationship: "learned"})
	memB.Links = append(memB.Links, Link{Target: relMemoryPath(memA), Relationship: "learned"})
	res.Applied = true
	return res
}

func writeLearnedEdgePair(memA, memB *MemoryEntry) error {
	// Reload fresh copies so we don't clobber concurrent edits with stale structs.
	freshA, err := loadMemoryEntryByPath(memA.FilePath)
	if err != nil {
		return fmt.Errorf("reload %s: %w", memA.FileName, err)
	}
	freshB, err := loadMemoryEntryByPath(memB.FilePath)
	if err != nil {
		return fmt.Errorf("reload %s: %w", memB.FileName, err)
	}
	targetB := relMemoryPath(memB)
	targetA := relMemoryPath(memA)
	if !hasLearnedEdgeFrom(freshA, freshB) {
		freshA.Links = append(freshA.Links, Link{Target: targetB, Relationship: "learned"})
		if err := WriteMemoryEntry(freshA); err != nil {
			return fmt.Errorf("write %s: %w", freshA.FileName, err)
		}
	}
	if !hasLearnedEdgeFrom(freshB, freshA) {
		freshB.Links = append(freshB.Links, Link{Target: targetA, Relationship: "learned"})
		if err := WriteMemoryEntry(freshB); err != nil {
			return fmt.Errorf("write %s: %w", freshB.FileName, err)
		}
	}
	return nil
}

func loadMemoryEntryByPath(path string) (*MemoryEntry, error) {
	return ParseMemoryEntry(path)
}

// shortKey strips the Memory/ prefix for display.
func shortKey(key string) string {
	key = strings.TrimPrefix(key, "memory/")
	key = strings.TrimPrefix(key, "archive/")
	return key
}