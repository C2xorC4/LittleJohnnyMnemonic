package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"
)

func cmdAssociate(vaultRoot string, args []string) {
	fs := flag.NewFlagSet("associate", flag.ExitOnError)
	format := fs.String("format", "full", "Output format: full, summary, json")
	limit := fs.Int("limit", 10, "Max memories to return")
	threshold := fs.Float64("threshold", 0.2, "Minimum combined score to surface (lower than retrieve τ for breadth)")
	enrichment := fs.Bool("enrichment", true, "Show enrichment candidates")
	noUpdate := fs.Bool("no-update", false, "Don't update access metadata")
	readTop := fs.Int("read", 0, "Output full body text for top N results (for LLM semantic evaluation)")
	cite := fs.String("cite", "", "Record citation for a memory key (comma-separated: key,context,useful)")
	fs.Parse(args)

	// Handle citation recording
	if *cite != "" {
		parts := strings.SplitN(*cite, ",", 3)
		if len(parts) < 2 {
			fmt.Fprintln(os.Stderr, "Usage: --cite 'memory_key,context[,true|false]'")
			os.Exit(1)
		}
		useful := true
		if len(parts) >= 3 && (parts[2] == "false" || parts[2] == "0") {
			useful = false
		}
		cLog, err := LoadCitations(vaultRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to load citations: %v\n", err)
			os.Exit(1)
		}
		RecordCitation(cLog, parts[0], parts[1], useful)
		if err := SaveCitations(vaultRoot, cLog); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to save citation: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Citation recorded: %s (%s)\n", parts[0], parts[1])
		return
	}

	// Remaining args are the free-text context
	contextText := strings.Join(fs.Args(), " ")
	if contextText == "" {
		fmt.Fprintln(os.Stderr, "Usage: jm associate [options] <context text>")
		fmt.Fprintln(os.Stderr, "  Provide free-text describing the current topic or activity.")
		fmt.Fprintln(os.Stderr, "  Example: jm associate \"discussing eBPF kernel hooks for network deception\"")
		os.Exit(1)
	}

	cfg := DefaultConfig()
	now := time.Now()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		fmt.Fprintf(os.Stderr, "[!] Failed to load memories: %v\n", err)
		os.Exit(1)
	}

	if len(memories) == 0 {
		fmt.Println("No memories to associate against.")
		return
	}

	// Extract keywords from free text
	keywords := ExtractKeywords(contextText)
	if len(keywords) == 0 {
		fmt.Println("No meaningful keywords extracted from context.")
		return
	}

	// Compute IDF weights for keyword specificity
	idf := ComputeIDF(keywords, memories)

	// Score with IDF-weighted relevance (tags + body matching)
	var results []AssociatedMemory

	// Build graph for spreading activation
	graph := BuildGraph(memories, cfg)

	for _, m := range memories {
		var activation float64
		if m.Type == TypeKnowledge {
			activation = 1.0 // Knowledge entries don't decay with time
		} else {
			activation = ComputeActivation(m, now)
		}
		tagRel := ComputeWeightedTagRelevance(m, keywords, idf)
		bodyRel := ComputeWeightedBodyRelevance(m, keywords, idf)
		combinedRel := tagRel*0.6 + bodyRel*0.4
		if combinedRel > 1.0 {
			combinedRel = 1.0
		}

		surprise := ComputeSurpriseBonus(m, cfg)
		score := activation*combinedRel*m.Confidence + surprise

		// Require at least some topical relevance — pure activation without
		// any keyword match isn't a meaningful association
		if combinedRel < 0.01 {
			continue
		}
		if score < *threshold {
			continue
		}

		// Find specific matches for explanation (with IDF weight annotation)
		var tagHits, bodyHits []string
		tagSet := make(map[string]bool)
		for _, t := range m.Tags {
			tagSet[strings.ToLower(t)] = true
		}
		searchable := strings.ToLower(m.Title + " " + m.Body)

		for _, kw := range keywords {
			if tagSet[kw] {
				tagHits = append(tagHits, kw)
			} else if containsWord(searchable, kw) {
				bodyHits = append(bodyHits, kw)
			}
		}

		var enrichWords []string
		if *enrichment {
			// Only suggest discriminating terms as enrichment (IDF ≥ 0.3)
			enrichWords = FindWeightedEnrichmentKeywords(m, keywords, idf, 0.3)
		}

		results = append(results, AssociatedMemory{
			Memory:          m,
			Score:           score,
			Activation:      activation,
			Relevance:       combinedRel,
			BodyRelevance:   bodyRel,
			TagMatches:      tagHits,
			BodyKeywordHits: bodyHits,
			EnrichmentWords: enrichWords,
		})
	}

	// Apply spreading activation boost
	if len(results) > 0 {
		// Convert to ScoredMemory for graph boosting, then merge back
		scoredForGraph := make([]ScoredMemory, len(results))
		for i, r := range results {
			scoredForGraph[i] = ScoredMemory{
				Memory: r.Memory,
				Total:  r.Score,
			}
		}
		boosted := ApplySpreadingActivation(scoredForGraph, graph, cfg)

		// Merge boost back and re-sort
		boostMap := make(map[string]float64)
		for _, b := range boosted {
			boostMap[b.Memory.FilePath] = b.Boost
		}
		for i := range results {
			if boost, ok := boostMap[results[i].Memory.FilePath]; ok {
				results[i].Score += boost
			}
		}
	}

	// Sort descending by score
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			if results[j].Score > results[i].Score {
				results[i], results[j] = results[j], results[i]
			}
		}
	}

	// Cap
	if len(results) > *limit {
		results = results[:*limit]
	}

	if len(results) == 0 {
		fmt.Printf("No memories associated with: %s\n", contextText)
		return
	}

	// Update access metadata and record co-activations
	if !*noUpdate {
		var coactivatedKeys []string
		for _, r := range results {
			r.Memory.LastAccessed = now
			r.Memory.AccessCount++
			if err := WriteMemoryEntry(r.Memory); err != nil {
				fmt.Fprintf(os.Stderr, "[!] Failed to update %s: %v\n", r.Memory.FileName, err)
			}
			coactivatedKeys = append(coactivatedKeys, normalizeKey(r.Memory))
		}

		// Record co-activation for edge learning
		coLog, err := LoadCoactivation(vaultRoot)
		if err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to load coactivation log: %v\n", err)
		} else {
			RecordCoactivation(coLog, coactivatedKeys, contextText, 5)
			if err := SaveCoactivation(vaultRoot, coLog); err != nil {
				fmt.Fprintf(os.Stderr, "[!] Failed to save coactivation log: %v\n", err)
			}
		}
	}

	// Output
	switch *format {
	case "summary":
		associateOutputSummary(results, keywords)
	case "json":
		associateOutputJSON(results, keywords)
	default:
		associateOutputFull(results, keywords, contextText, *enrichment, idf)
	}

	// Output full body text for top N results if --read is specified
	if *readTop > 0 {
		n := *readTop
		if n > len(results) {
			n = len(results)
		}
		fmt.Printf("\n## Full content for top %d results\n\n", n)
		for i := 0; i < n; i++ {
			m := results[i].Memory
			fmt.Printf("### %d. %s (%.3f)\n\n", i+1, m.Title, results[i].Score)
			fmt.Printf("%s\n\n---\n\n", m.Body)
		}
	}
}

func associateOutputFull(results []AssociatedMemory, keywords []string, context string, showEnrichment bool, idf IDFWeights) {
	fmt.Printf("# Associations (context: %q)\n", truncate(context, 80))

	// Show keywords with IDF weights
	var kwParts []string
	for _, kw := range keywords {
		w := idf[kw]
		kwParts = append(kwParts, fmt.Sprintf("%s(%.2f)", kw, w))
	}
	fmt.Printf("**Keywords (IDF weight):** %s\n\n", strings.Join(kwParts, ", "))

	// Split into relevant and enrichment candidates
	var relevant, enrichable []AssociatedMemory
	for _, r := range results {
		relevant = append(relevant, r)
		// Only flag enrichment for memories with meaningful topical overlap
		// (relevance driven by discriminating terms, not just generic matches)
		// and that have discriminating novel concepts to absorb
		if showEnrichment && r.Relevance >= 0.10 && len(r.EnrichmentWords) > 0 && r.Score >= 0.3 {
			enrichable = append(enrichable, r)
		}
	}

	fmt.Printf("## Relevant memories (%d)\n\n", len(relevant))
	for i, r := range relevant {
		m := r.Memory
		override := ""
		if m.TrainingOverride {
			override = " [OVERRIDE]"
		}
		fmt.Printf("%d. **[%.3f]** %s (%s)%s\n", i+1, r.Score, m.Title, m.Type, override)

		var matchParts []string
		if len(r.TagMatches) > 0 {
			matchParts = append(matchParts, fmt.Sprintf("tags: %s", strings.Join(r.TagMatches, ", ")))
		}
		if len(r.BodyKeywordHits) > 0 {
			matchParts = append(matchParts, fmt.Sprintf("body: %s", strings.Join(r.BodyKeywordHits, ", ")))
		}
		if len(matchParts) > 0 {
			fmt.Printf("   Matched: %s\n", strings.Join(matchParts, " | "))
		}
		fmt.Printf("   Score: activation=%.3f × relevance=%.3f × confidence=%.2f\n",
			r.Activation, r.Relevance, m.Confidence)
		fmt.Println()
	}

	if showEnrichment && len(enrichable) > 0 {
		fmt.Printf("## Enrichment candidates (%d)\n", len(enrichable))
		fmt.Println("_Current context contains concepts not yet in these memories:_")
		for _, r := range enrichable {
			fmt.Printf("- **%s**: novel concepts → %s\n", r.Memory.Title, strings.Join(r.EnrichmentWords, ", "))
		}
		fmt.Println()
	}
}

func associateOutputSummary(results []AssociatedMemory, keywords []string) {
	fmt.Printf("keywords: %s\n\n", strings.Join(keywords, ", "))
	for i, r := range results {
		override := ""
		if r.Memory.TrainingOverride {
			override = " [OVERRIDE]"
		}
		matches := append(r.TagMatches, r.BodyKeywordHits...)
		matchStr := ""
		if len(matches) > 0 {
			matchStr = fmt.Sprintf(" {%s}", strings.Join(matches, ","))
		}
		fmt.Printf("%d. [%.3f] %s (%s)%s%s\n",
			i+1, r.Score, r.Memory.Title, r.Memory.Type, override, matchStr)
	}
}

func associateOutputJSON(results []AssociatedMemory, keywords []string) {
	fmt.Printf(`{"keywords": ["%s"], "associations": [`+"\n", strings.Join(keywords, `", "`))
	for i, r := range results {
		comma := ","
		if i == len(results)-1 {
			comma = ""
		}
		m := r.Memory
		fmt.Printf(`  {"title": "%s", "type": "%s", "score": %.4f, `+
			`"tag_matches": ["%s"], "body_matches": ["%s"], `+
			`"enrichment": ["%s"]}%s`+"\n",
			escapeJSON(m.Title), m.Type, r.Score,
			strings.Join(r.TagMatches, `", "`),
			strings.Join(r.BodyKeywordHits, `", "`),
			strings.Join(r.EnrichmentWords, `", "`), comma)
	}
	fmt.Println("]}")
}

type AssociatedMemory struct {
	Memory          *MemoryEntry
	Score           float64
	Activation      float64
	Relevance       float64
	BodyRelevance   float64
	TagMatches      []string
	BodyKeywordHits []string
	EnrichmentWords []string
}
