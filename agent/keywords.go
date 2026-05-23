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

// isVowel reports whether c is an English vowel.
func isVowel(c byte) bool {
	return c == 'a' || c == 'e' || c == 'i' || c == 'o' || c == 'u'
}

// hasVowel reports whether s contains at least one vowel.
func hasVowel(s string) bool {
	for i := 0; i < len(s); i++ {
		if isVowel(s[i]) {
			return true
		}
	}
	return false
}

// fixStem applies Porter Step 1b fix-up rules to a stem produced by stripping
// -ed or -ing: collapses doubled consonants (running→run) and restores the
// silent-e where the CVC ending pattern applies (scor→score, activ→active).
func fixStem(stem string) string {
	n := len(stem)
	if n < 2 {
		return stem
	}
	// Remove doubled consonant — but NOT for -ll, -ss, -zz endings (Porter rule).
	if stem[n-1] == stem[n-2] && !isVowel(stem[n-1]) {
		last := stem[n-1]
		if last != 'l' && last != 's' && last != 'z' {
			return stem[:n-1]
		}
	}
	// Restore silent-e on CVC ending (consonant–vowel–consonant, excluding w/x/y).
	if n >= 3 && !isVowel(stem[n-1]) && isVowel(stem[n-2]) && !isVowel(stem[n-3]) {
		last := stem[n-1]
		if last != 'w' && last != 'x' && last != 'y' {
			return stem + "e"
		}
	}
	return stem
}

// Stem reduces an English word to an approximate base form using a targeted
// subset of Porter Step 1 rules. Input must be already lowercased. Covers the
// highest-frequency inflection patterns: plurals, gerunds, and past tense.
//
// Not a full stemmer — "retrieval" and "retrieve" remain distinct. Use for
// symmetric matching where both query tokens and corpus tokens are stemmed.
func Stem(word string) string {
	w := word
	if len(w) <= 3 {
		return w
	}

	// -ies → -y: memories→memory, entries→entry
	if strings.HasSuffix(w, "ies") && len(w) > 4 {
		return w[:len(w)-3] + "y"
	}

	// -sses → -ss: processes→process
	if strings.HasSuffix(w, "sses") {
		return w[:len(w)-2]
	}

	// -ing → base form: scoring→score, running→run, discussing→discuss
	if strings.HasSuffix(w, "ing") && len(w) > 5 {
		stem := w[:len(w)-3]
		if hasVowel(stem) {
			return fixStem(stem)
		}
	}

	// -ed → base form: scored→score, activated→activate
	if strings.HasSuffix(w, "ed") && len(w) > 4 {
		stem := w[:len(w)-2]
		if hasVowel(stem) {
			return fixStem(stem)
		}
	}

	// -es → remove trailing s (keep e): scores→score, retrieves→retrieve
	if strings.HasSuffix(w, "es") && len(w) > 4 {
		return w[:len(w)-1]
	}

	// -s → remove: topics→topic, patterns→pattern (skip -ss endings)
	if strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && len(w) > 3 {
		stem := w[:len(w)-1]
		if hasVowel(stem) {
			return stem
		}
	}

	return w
}

// stemTextSet tokenises text and returns the set of stemmed lowercased tokens.
// Hyphenated compounds (e.g. "kernel-level") contribute both the full token
// and each individual part, so a keyword "kernel" matches body text
// "kernel-level". Used for symmetric body/title matching against stemmed
// query keywords from ExtractKeywords.
func stemTextSet(text string) map[string]bool {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})
	set := make(map[string]bool, len(tokens)*2)
	for _, t := range tokens {
		t = strings.TrimFunc(t, func(r rune) bool { return r == '-' || r == '_' })
		if len(t) < 2 {
			continue
		}
		set[Stem(t)] = true
		// Also index each part of hyphenated/underscored compounds.
		if strings.ContainsAny(t, "-_") {
			for _, part := range strings.FieldsFunc(t, func(r rune) bool { return r == '-' || r == '_' }) {
				if len(part) >= 2 {
					set[Stem(part)] = true
				}
			}
		}
	}
	return set
}

// ExtractKeywords tokenizes free text into unique, lowercased, stemmed keywords
// with stop words and short tokens removed. The returned tokens are in stemmed
// form — pass them directly to the relevance and IDF functions.
func ExtractKeywords(text string) []string {
	tokens := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r) && r != '-' && r != '_'
	})

	seen := make(map[string]bool)
	var keywords []string
	for _, t := range tokens {
		t = strings.TrimFunc(t, func(r rune) bool { return r == '-' || r == '_' })
		if len(t) < 2 || stopWords[t] {
			continue
		}
		t = Stem(t)
		if seen[t] {
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
// Retained for ad-hoc unstemmed text search; relevance functions use stemTextSet.
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
// the memory corpus. Keywords are expected to be pre-stemmed (from ExtractKeywords).
// IDF = log(N / df) where N = total docs, df = docs containing term.
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

	// Pre-compute stemmed word sets once per memory to avoid redundant work.
	memStemSets := make([]map[string]bool, len(memories))
	for i, m := range memories {
		memStemSets[i] = stemTextSet(m.Title + " " + m.Body + " " + strings.Join(m.Tags, " "))
	}

	for _, kw := range keywords {
		df := 0
		for _, stemSet := range memStemSets {
			if stemSet[kw] {
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
// Keywords must be pre-stemmed (from ExtractKeywords).
func ComputeBodyRelevance(m *MemoryEntry, keywords []string) float64 {
	if len(keywords) == 0 {
		return 0.0
	}
	bodySet := stemTextSet(m.Title + " " + m.Body)
	hits := 0
	for _, kw := range keywords {
		if bodySet[kw] {
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
// Keywords must be pre-stemmed (from ExtractKeywords).
func ComputeWeightedBodyRelevance(m *MemoryEntry, keywords []string, idf IDFWeights) float64 {
	if len(keywords) == 0 {
		return 0.0
	}
	bodySet := stemTextSet(m.Title + " " + m.Body)
	weightedHits := 0.0
	totalWeight := 0.0

	for _, kw := range keywords {
		w := idf[kw]
		if w == 0 {
			w = 0.5 // fallback for unknown keywords
		}
		totalWeight += w
		if bodySet[kw] {
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
// Both keywords and memory tags are stemmed for symmetric matching.
// Keywords must be pre-stemmed (from ExtractKeywords).
func ComputeWeightedTagRelevance(m *MemoryEntry, keywords []string, idf IDFWeights) float64 {
	if len(keywords) == 0 {
		return 0.5 // neutral when no context
	}

	tagSet := make(map[string]bool, len(m.Tags))
	for _, t := range m.Tags {
		tagSet[Stem(strings.ToLower(t))] = true
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
// provide breadth. Keywords must be pre-stemmed (from ExtractKeywords).
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
// about the memory's topic. Keywords must be pre-stemmed (from ExtractKeywords).
func FindEnrichmentKeywords(m *MemoryEntry, keywords []string) []string {
	bodySet := stemTextSet(m.Title + " " + m.Body)
	tagSet := make(map[string]bool, len(m.Tags))
	for _, t := range m.Tags {
		tagSet[Stem(strings.ToLower(t))] = true
	}

	var novel []string
	for _, kw := range keywords {
		if !tagSet[kw] && !bodySet[kw] {
			novel = append(novel, kw)
		}
	}
	return novel
}

// FindWeightedEnrichmentKeywords returns novel keywords filtered by IDF weight.
// Generic terms that appear across most memories aren't useful as enrichment.
// Keywords must be pre-stemmed (from ExtractKeywords).
func FindWeightedEnrichmentKeywords(m *MemoryEntry, keywords []string, idf IDFWeights, minWeight float64) []string {
	bodySet := stemTextSet(m.Title + " " + m.Body)
	tagSet := make(map[string]bool, len(m.Tags))
	for _, t := range m.Tags {
		tagSet[Stem(strings.ToLower(t))] = true
	}

	var novel []string
	for _, kw := range keywords {
		if tagSet[kw] || bodySet[kw] {
			continue
		}
		// Only suggest as enrichment if the term is discriminating
		if idf[kw] >= minWeight {
			novel = append(novel, kw)
		}
	}
	return novel
}
