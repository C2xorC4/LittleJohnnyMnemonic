package main

import (
	"math"
	"strings"
	"unicode"
)

// stopWords are common English words filtered from keyword extraction.
var stopWords = map[string]bool{
	"a": true, "an": true, "and": true, "are": true, "as": true, "at": true,
	"be": true, "by": true, "but": true, "do": true, "for": true, "from": true,
	"had": true, "has": true, "have": true, "he": true, "her": true, "his": true,
	"how": true, "i": true, "if": true, "in": true, "into": true, "is": true,
	"it": true, "its": true, "just": true, "me": true, "my": true, "no": true,
	"not": true, "now": true, "of": true, "on": true, "or": true, "our": true,
	"out": true, "so": true, "some": true, "than": true, "that": true,
	"the": true, "their": true, "them": true, "then": true, "there": true,
	"these": true, "they": true, "this": true, "to": true, "up": true,
	"us": true, "was": true, "we": true, "were": true, "what": true,
	"when": true, "which": true, "who": true, "will": true, "with": true,
	"would": true, "you": true, "your": true, "been": true, "being": true,
	"could": true, "did": true, "does": true, "doing": true, "each": true,
	"get": true, "got": true, "may": true, "might": true, "more": true,
	"most": true, "much": true, "must": true, "only": true, "other": true,
	"over": true, "own": true, "said": true, "shall": true, "she": true,
	"should": true, "such": true, "very": true, "about": true, "also": true,
	"back": true, "can": true, "like": true, "make": true, "one": true,
	"way": true, "where": true, "all": true, "any": true,
}

// ExtractKeywords tokenizes free text into unique, lowercased keywords
// with stop words and short tokens removed.
func ExtractKeywords(text string) []string {
	// Split on non-alphanumeric boundaries
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})

	seen := make(map[string]bool)
	var keywords []string
	for _, t := range tokens {
		t = strings.TrimFunc(t, func(r rune) bool { return r == '-' || r == '_' })
		if len(t) < 2 || stopWords[t] || seen[t] {
			continue
		}
		seen[t] = true
		keywords = append(keywords, t)
	}
	return keywords
}

// IDFWeights maps keywords to their inverse document frequency weight.
// Higher weight = more discriminating (appears in fewer documents).
type IDFWeights map[string]float64

// containsWord checks if text contains keyword as a whole word or tag,
// not as a substring of a larger word. Uses simple word-boundary detection.
func containsWord(text, keyword string) bool {
	idx := 0
	for {
		pos := strings.Index(text[idx:], keyword)
		if pos < 0 {
			return false
		}
		absPos := idx + pos
		endPos := absPos + len(keyword)

		// Check word boundaries
		startOK := absPos == 0 || !unicode.IsLetter(rune(text[absPos-1]))
		endOK := endPos >= len(text) || !unicode.IsLetter(rune(text[endPos]))

		if startOK && endOK {
			return true
		}
		idx = absPos + 1
		if idx >= len(text) {
			return false
		}
	}
}

// ComputeIDF calculates inverse document frequency for each keyword across
// the memory corpus. IDF = log(N / df) where N = total docs, df = docs containing term.
// Keywords appearing in most documents get low weight; rare terms get high weight.
func ComputeIDF(keywords []string, memories []*MemoryEntry) IDFWeights {
	weights := make(IDFWeights)
	n := float64(len(memories))
	if n == 0 {
		for _, kw := range keywords {
			weights[kw] = 1.0
		}
		return weights
	}

	for _, kw := range keywords {
		df := 0
		for _, m := range memories {
			searchable := strings.ToLower(m.Title + " " + m.Body + " " + strings.Join(m.Tags, " "))
			if containsWord(searchable, kw) {
				df++
			}
		}

		if df == 0 {
			// Term not in corpus — max weight (novel concept)
			weights[kw] = math.Log(n+1) + 1.0
		} else {
			// Standard IDF with smoothing
			weights[kw] = math.Log(n/float64(df)) + 0.1
		}
	}

	// Normalize to 0.0–1.0 range
	maxW := 0.0
	for _, w := range weights {
		if w > maxW {
			maxW = w
		}
	}
	if maxW > 0 {
		for kw := range weights {
			weights[kw] /= maxW
		}
	}

	return weights
}

// ComputeBodyRelevance scores how well a set of keywords match against a memory's
// body text and title. Returns 0.0–1.0. Flat weighting (no IDF).
func ComputeBodyRelevance(m *MemoryEntry, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0.0
	}

	searchable := strings.ToLower(m.Title + " " + m.Body)
	hits := 0
	for _, kw := range keywords {
		if containsWord(searchable, kw) {
			hits++
		}
	}

	ratio := float64(hits) / float64(len(keywords))
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// ComputeWeightedBodyRelevance scores keyword matches weighted by IDF.
// Discriminating terms (high IDF) contribute more than generic ones (low IDF).
func ComputeWeightedBodyRelevance(m *MemoryEntry, keywords []string, idf IDFWeights) float64 {
	if len(keywords) == 0 {
		return 0.0
	}

	searchable := strings.ToLower(m.Title + " " + m.Body)
	weightedHits := 0.0
	totalWeight := 0.0

	for _, kw := range keywords {
		w := idf[kw]
		if w == 0 {
			w = 0.5 // fallback for unknown keywords
		}
		totalWeight += w
		if containsWord(searchable, kw) {
			weightedHits += w
		}
	}

	if totalWeight == 0 {
		return 0.0
	}
	ratio := weightedHits / totalWeight
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// ComputeWeightedTagRelevance scores tag matches weighted by IDF.
// Tags are exact matches (not substring), so no word-boundary check needed.
func ComputeWeightedTagRelevance(m *MemoryEntry, keywords []string, idf IDFWeights) float64 {
	if len(keywords) == 0 {
		return 0.5 // neutral when no context
	}

	tagSet := make(map[string]bool)
	for _, t := range m.Tags {
		tagSet[strings.ToLower(t)] = true
	}

	weightedHits := 0.0
	totalWeight := 0.0

	for _, kw := range keywords {
		w := idf[kw]
		if w == 0 {
			w = 0.5
		}
		totalWeight += w
		if tagSet[kw] {
			weightedHits += w
		}
	}

	if totalWeight == 0 {
		return 0.0
	}
	ratio := weightedHits / totalWeight
	if ratio > 1.0 {
		ratio = 1.0
	}
	return ratio
}

// ComputeCombinedRelevance merges tag-based and body-based relevance.
// Tag matches are weighted higher (exact semantic markers) while body matches
// provide breadth.
func ComputeCombinedRelevance(m *MemoryEntry, contextTags []string, keywords []string) float64 {
	tagRel := ComputeRelevance(m, contextTags)
	bodyRel := ComputeBodyRelevance(m, keywords)

	// Weighted combination: tags 60%, body 40%
	combined := tagRel*0.6 + bodyRel*0.4
	if combined > 1.0 {
		combined = 1.0
	}
	return combined
}

// FindEnrichmentKeywords identifies keywords present in the context but NOT
// in the memory's tags or body — these represent potential new knowledge
// about the memory's topic.
func FindEnrichmentKeywords(m *MemoryEntry, keywords []string) []string {
	searchable := strings.ToLower(m.Title + " " + m.Body + " " + strings.Join(m.Tags, " "))
	tagSet := make(map[string]bool)
	for _, t := range m.Tags {
		tagSet[strings.ToLower(t)] = true
	}

	var novel []string
	for _, kw := range keywords {
		// Check tags (exact) and body (word-boundary)
		if !tagSet[kw] && !containsWord(searchable, kw) {
			novel = append(novel, kw)
		}
	}
	return novel
}

// FindWeightedEnrichmentKeywords returns novel keywords filtered by IDF weight.
// Generic terms that appear across most memories aren't useful as enrichment.
func FindWeightedEnrichmentKeywords(m *MemoryEntry, keywords []string, idf IDFWeights, minWeight float64) []string {
	searchable := strings.ToLower(m.Title + " " + m.Body + " " + strings.Join(m.Tags, " "))
	tagSet := make(map[string]bool)
	for _, t := range m.Tags {
		tagSet[strings.ToLower(t)] = true
	}

	var novel []string
	for _, kw := range keywords {
		if tagSet[kw] || containsWord(searchable, kw) {
			continue
		}
		// Only suggest as enrichment if the term is discriminating
		if idf[kw] >= minWeight {
			novel = append(novel, kw)
		}
	}
	return novel
}
