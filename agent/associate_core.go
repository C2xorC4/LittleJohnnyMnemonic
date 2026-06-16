package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"
)

// AssociateOpts controls the behavior of AssociateMemories.
type AssociateOpts struct {
	Limit               int
	Threshold           float64
	UpdateAccess        bool
	Enrichment          bool
	EnrichmentMinWeight float64
	// DiscriminatingMinIDF gates results to memories matching at least one
	// high-IDF keyword (tag or body). Zero uses DefaultDiscriminatingMinIDF.
	DiscriminatingMinIDF float64
	// SkipDiscriminatingGate disables the high-IDF match requirement (tests).
	SkipDiscriminatingGate bool
}

// AssociateMemories runs the free-text → scored memories pipeline.
// Shared by `jm associate` (CLI) and `jm hook user-prompt-submit`.
//
// Returns results already sorted by score descending and capped at opts.Limit.
// Archived memories are excluded. Access metadata and co-activation are
// updated only if opts.UpdateAccess is true.
func AssociateMemories(
	vaultRoot string,
	contextText string,
	opts AssociateOpts,
) ([]AssociatedMemory, []string, IDFWeights, error) {
	if opts.Limit <= 0 {
		opts.Limit = 10
	}
	if opts.Threshold <= 0 {
		opts.Threshold = 0.2
	}
	if opts.EnrichmentMinWeight <= 0 {
		opts.EnrichmentMinWeight = 0.3
	}

	cfg := LoadConfig(vaultRoot)
	now := time.Now()

	memories, err := LoadAllMemories(vaultRoot)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("load memories: %w", err)
	}
	if len(memories) == 0 {
		return nil, nil, nil, nil
	}

	keywords := ExtractKeywords(contextText)
	if len(keywords) == 0 {
		return nil, nil, nil, nil
	}

	scoringKeywords := ScoringKeywords(keywords)
	idf := ComputeIDF(scoringKeywords, memories)
	graph := BuildGraph(memories, cfg)

	discriminatingMinIDF := DefaultDiscriminatingMinIDF
	if opts.DiscriminatingMinIDF > 0 {
		discriminatingMinIDF = opts.DiscriminatingMinIDF
	}
	applyDiscriminatingGate := !opts.SkipDiscriminatingGate &&
		QueryHasDiscriminatingTerms(idf, scoringKeywords, discriminatingMinIDF)

	var results []AssociatedMemory
	for _, m := range memories {
		// Archived memories ARE considered for retrieval (B' semantics):
		// if a query matches an archived memory strongly enough to cross
		// threshold, the access will resurrect it via UnarchiveOnAccess
		// in the write-back loop below.

		activation := ActivationForType(m, now, cfg)
		tagRel := ComputeWeightedTagRelevance(m, scoringKeywords, idf)
		bodyRel := ComputeWeightedBodyRelevance(m, scoringKeywords, idf)
		combinedRel := tagRel*0.6 + bodyRel*0.4
		if combinedRel > 1.0 {
			combinedRel = 1.0
		}

		surprise := ComputeSurpriseBonus(m, cfg)
		score := activation*combinedRel*m.Confidence + surprise

		// Require at least some topical relevance
		if combinedRel < 0.01 {
			continue
		}
		if applyDiscriminatingGate && !HasDiscriminatingMatch(m, scoringKeywords, idf, discriminatingMinIDF) {
			continue
		}
		if score < opts.Threshold {
			continue
		}

		// Collect match explanations (scoring keywords only — operational terms excluded).
		var tagHits, bodyHits []string
		stemmedTags := make(map[string]bool, len(m.Tags))
		for _, t := range m.Tags {
			stemmedTags[Stem(strings.ToLower(t))] = true
		}
		bodySet := stemTextSet(m.Title + " " + m.Body)

		for _, kw := range scoringKeywords {
			if stemmedTags[kw] {
				tagHits = append(tagHits, kw)
			} else if bodySet[kw] {
				bodyHits = append(bodyHits, kw)
			}
		}

		var enrichWords []string
		if opts.Enrichment {
			enrichWords = FindWeightedEnrichmentKeywords(m, scoringKeywords, idf, opts.EnrichmentMinWeight)
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

	// Spreading activation boost via graph
	if len(results) > 0 {
		scoredForGraph := make([]ScoredMemory, len(results))
		for i, r := range results {
			scoredForGraph[i] = ScoredMemory{Memory: r.Memory, Total: r.Score}
		}
		boosted := ApplySpreadingActivation(scoredForGraph, graph, cfg)
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

	// Sort descending by final score
	sort.Slice(results, func(i, j int) bool {
		return results[i].Score > results[j].Score
	})

	// Cap
	if len(results) > opts.Limit {
		results = results[:opts.Limit]
	}

	// Update access metadata and record co-activations
	if opts.UpdateAccess && len(results) > 0 {
		var coactivatedKeys []string
		accessKeys := make([]string, 0, len(results))
		for _, r := range results {
			k := normalizeKey(r.Memory)
			accessKeys = append(accessKeys, k)
			coactivatedKeys = append(coactivatedKeys, k)
			// Un-archiving is a genuine content change → still writes the file.
			if UnarchiveOnAccess(r.Memory) {
				fmt.Fprintf(os.Stderr, "[jm associate] resurrected from archive: %s\n", r.Memory.FileName)
				if err := WriteMemoryEntry(r.Memory); err != nil {
					fmt.Fprintf(os.Stderr, "[!] Failed to update %s: %v\n", r.Memory.FileName, err)
				}
			}
		}
		if err := recordAccessBatch(vaultRoot, accessKeys, now); err != nil {
			fmt.Fprintf(os.Stderr, "[!] Failed to record access: %v\n", err)
		}

		if coLog, err := LoadCoactivation(vaultRoot); err == nil {
			RecordCoactivation(coLog, coactivatedKeys, contextText, 5)
			_ = SaveCoactivation(vaultRoot, coLog)
		}
	}

	return results, scoringKeywords, idf, nil
}
